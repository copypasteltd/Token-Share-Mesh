package invocation

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

const InputProfile = "P11_NATIVE_TEXT_INPUT_AESGCM_1"
const AdmissionProfile = "P11_NATIVE_TEXT_ADMISSION_1"

// ExecutionConfig has no generated/default production keys. Input encryption
// protects PLAINTEXT-domain storage; it is NOT customer E2E or provider blindness.
// The host provisions the issuer pin independently on devices before delivery.
type ExecutionConfig struct {
	Invocations       *Service
	InputVersion      string
	InputKeys         map[string][]byte
	SigningKeyID      string
	SigningPrivateKey ed25519.PrivateKey
	VerificationKeys  map[string]ed25519.PublicKey
}

type Execution struct {
	invocations       *Service
	version, signerID string
	mu                sync.RWMutex
	keys              map[string][]byte
	signer            ed25519.PrivateKey
	verifiers         map[string]ed25519.PublicKey
}

func NewExecution(c ExecutionConfig) (*Execution, error) {
	if c.Invocations == nil || c.Invocations.db == nil || !ids.Valid(c.InputVersion) || !ids.Valid(c.SigningKeyID) ||
		len(c.InputKeys) < 1 || len(c.InputKeys) > 16 || len(c.InputKeys[c.InputVersion]) != 32 || len(c.VerificationKeys) > 16 ||
		len(c.SigningPrivateKey) != ed25519.PrivateKeySize {
		return nil, fault.New(fault.Invalid, "Explicit input keys and native execution issuer required")
	}
	derived := ed25519.NewKeyFromSeed(c.SigningPrivateKey.Seed())
	defer clear(derived)
	if !bytes.Equal(derived, c.SigningPrivateKey) {
		return nil, fault.New(fault.Invalid, "Invalid execution issuer key")
	}
	e := &Execution{invocations: c.Invocations, version: c.InputVersion, signerID: c.SigningKeyID,
		keys: map[string][]byte{}, signer: append(ed25519.PrivateKey(nil), c.SigningPrivateKey...), verifiers: map[string]ed25519.PublicKey{}}
	c.Invocations.mu.RLock()
	defer c.Invocations.mu.RUnlock()
	if len(c.Invocations.keys) == 0 {
		e.Close()
		return nil, fault.New(fault.Unavailable, "Invocation keys unavailable")
	}
	for version, key := range c.InputKeys {
		if !ids.Valid(version) || len(key) != 32 || bytes.Equal(key, c.SigningPrivateKey.Seed()) {
			e.Close()
			return nil, fault.New(fault.Invalid, "Invalid or reused input key")
		}
		for _, fingerprint := range c.Invocations.keys {
			if bytes.Equal(fingerprint, c.SigningPrivateKey.Seed()) || bytes.Equal(fingerprint, c.SigningPrivateKey) {
				e.Close()
				return nil, fault.New(fault.Invalid, "Issuer and fingerprint keys require separate purposes")
			}
			if bytes.Equal(key, fingerprint) {
				e.Close()
				return nil, fault.New(fault.Invalid, "Input and fingerprint keys require separate purposes")
			}
		}
		e.keys[version] = append([]byte(nil), key...)
	}
	for id, pub := range c.VerificationKeys {
		if !ids.Valid(id) || len(pub) != ed25519.PublicKeySize {
			e.Close()
			return nil, fault.New(fault.Invalid, "Invalid historical issuer pin")
		}
		e.verifiers[id] = append(ed25519.PublicKey(nil), pub...)
	}
	pub := c.SigningPrivateKey.Public().(ed25519.PublicKey)
	if old, ok := e.verifiers[c.SigningKeyID]; ok && !bytes.Equal(old, pub) {
		e.Close()
		return nil, fault.New(fault.Invalid, "Issuer ID pin conflict")
	}
	e.verifiers[c.SigningKeyID] = append(ed25519.PublicKey(nil), pub...)
	if len(e.verifiers) > 16 {
		e.Close()
		return nil, fault.New(fault.Invalid, "Too many issuer verification pins")
	}
	return e, nil
}

// Close clears only owned copies, not all Go/runtime memory, budgets or locks.
func (e *Execution) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for version, key := range e.keys {
		clear(key)
		delete(e.keys, version)
	}
	clear(e.signer)
	e.signer = nil
	for id, pub := range e.verifiers {
		clear(pub)
		delete(e.verifiers, id)
	}
}

type InputReady struct {
	Preparation           Preparation
	AssetID, CipherDigest string
	Bytes                 int64
	Expires               time.Time
	Replayed              bool
}

type storedInput struct {
	id, owner, order, job, version, digest, state string
	expires                                       time.Time
	bytes                                         int64
	ciphertext                                    []byte
}

func (a storedInput) aad() []byte {
	b, _ := json.Marshal(struct{ Domain, Buyer, Order, Job, Asset, KeyVersion, Expires string }{InputProfile, a.owner, a.order, a.job, a.id, a.version, a.expires.UTC().Format(time.RFC3339Nano)})
	return b
}

func (e *Execution) aead(version string) (cipher.AEAD, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	key := e.keys[version]
	if len(key) != 32 || len(e.signer) != ed25519.PrivateKeySize {
		return nil, fault.New(fault.Unavailable, "Original input key or execution issuer unavailable")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func verifyBody(in Request, body []byte) error {
	m, err := relay.Preflight(in.Protocol, in.Model, body)
	if err != nil {
		return fault.New(fault.Invalid, "Unsupported bounded native text input")
	}
	if m.Digest != in.RequestDigest || m.MaxOutputTokens != in.MaxOutputTokens || m.Stream != in.Stream {
		return fault.New(fault.Conflict, "Preflight differs from original native input")
	}
	return nil
}

func (e *Execution) loadInput(ctx context.Context, tx pgx.Tx, p Preparation) (storedInput, error) {
	var a storedInput
	err := tx.QueryRow(ctx, `SELECT a.id,a.owner_id,a.order_id,a.job_id,a.input_key_version,a.storage_digest,a.state,a.expires_at,a.bytes,a.input_ciphertext
FROM p11.jobs j JOIN p11.assets a ON a.id=j.input_asset_id AND a.job_id=j.id AND a.order_id=j.order_id
JOIN p11.orders o ON o.id=j.order_id AND o.buyer_id=a.owner_id
WHERE j.id=$1 AND a.input_profile=$2 AND a.visibility='PLAINTEXT' AND a.media_type='application/json' FOR SHARE OF a`, p.JobID, InputProfile).
		Scan(&a.id, &a.owner, &a.order, &a.job, &a.version, &a.digest, &a.state, &a.expires, &a.bytes, &a.ciphertext)
	if err == pgx.ErrNoRows {
		return a, fault.New(fault.Conflict, "Original validated input unavailable")
	}
	return a, err
}

func (e *Execution) open(a storedInput, in Request) error {
	defer clear(a.ciphertext)
	if a.state != "READY" || a.bytes < 1 || a.bytes > 256<<10 || len(a.ciphertext) != int(a.bytes)+28 || outbox.Digest(a.ciphertext) != a.digest {
		return fault.New(fault.Conflict, "Original input requires recovery")
	}
	gcm, err := e.aead(a.version)
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, a.ciphertext[:gcm.NonceSize()], a.ciphertext[gcm.NonceSize():], a.aad())
	if err != nil {
		return fault.New(fault.Conflict, "Original input authentication failed")
	}
	defer clear(plain)
	return verifyBody(in, plain)
}

func (e *Execution) Ready(ctx context.Context, p identity.Principal, in Request, body []byte) (InputReady, error) {
	var result InputReady
	if e == nil || e.invocations == nil {
		return result, fault.New(fault.Unavailable, "Native input unavailable")
	}
	err := e.invocations.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error { var err error; result, err = e.ReadyTx(ctx, tx, p, in, body); return err })
	if err != nil {
		return InputReady{}, err
	}
	return result, nil
}

// ReadyTx commits an actual encrypted bounded body, the original hold, QUEUED
// and a safe event together. No filesystem, network, tokenizer or GPU work.
func (e *Execution) ReadyTx(ctx context.Context, tx pgx.Tx, p identity.Principal, in Request, body []byte) (InputReady, error) {
	var result InputReady
	if e == nil || e.invocations == nil || tx == nil {
		return result, fault.New(fault.Unavailable, "Native input unavailable")
	}
	if len(body) > 256<<10 {
		return result, fault.New(fault.Invalid, "Native input exceeds bound")
	}
	body = append([]byte(nil), body...)
	defer clear(body)
	if err := verifyBody(in, body); err != nil {
		return result, err
	}
	prepared, err := e.invocations.PrepareTx(ctx, tx, p, in)
	if err != nil {
		return result, err
	}
	var existing *string
	if err = tx.QueryRow(ctx, `SELECT input_asset_id FROM p11.jobs WHERE id=$1`, prepared.JobID).Scan(&existing); err != nil {
		return result, err
	}
	if existing != nil {
		a, err := e.loadInput(ctx, tx, prepared)
		if err != nil {
			return result, err
		}
		if err = e.open(a, in); err != nil {
			return result, err
		}
		return InputReady{prepared, a.id, a.digest, a.bytes, a.expires, true}, nil
	}
	c, err := e.invocations.routes.LockForPreparationTx(ctx, tx, p, in.Model, in.Protocol)
	if err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, err
	}
	if !c.ExistingAdmission || prepared.State != "INPUT_PENDING" || prepared.BudgetState != "RESERVED" || !prepared.Deadline.After(now) {
		return result, fault.New(fault.Conflict, "Original call cannot accept new input")
	}
	gcm, err := e.aead(e.version)
	if err != nil {
		return result, err
	}
	a := storedInput{id: ids.New("ast"), owner: p.UserID, order: prepared.OrderID, job: prepared.JobID, version: e.version, bytes: int64(len(body)), expires: prepared.Deadline, state: "READY"}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return result, err
	}
	a.ciphertext = gcm.Seal(nonce, nonce, body, a.aad())
	defer clear(a.ciphertext)
	a.digest = outbox.Digest(a.ciphertext)
	_, err = tx.Exec(ctx, `INSERT INTO p11.assets(id,order_id,owner_id,job_id,visibility,state,storage_key,storage_digest,bytes,media_type,expires_at,input_profile,input_key_version,input_ciphertext)
VALUES($1::text,$2,$3,$4,'PLAINTEXT','READY',$1::text,$5,$6,'application/json',$7,$8,$9,$10)`, a.id, a.order, a.owner, a.job, a.digest, a.bytes, a.expires, InputProfile, a.version, a.ciphertext)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE p11.jobs SET input_asset_id=$2,state='QUEUED',version=version+1 WHERE id=$1`, prepared.JobID, a.id)
	if err != nil {
		return result, err
	}
	payload, _ := json.Marshal(map[string]string{"job_id": a.job, "asset_id": a.id, "order_id": a.order, "contract_hash": c.ContractHash})
	_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'ROUTE_INVOCATION',$2,2,'RouteInvocationInputReady','3.0',$3,'PENDING')`, ids.New("evt"), a.job, payload)
	if err != nil {
		return result, err
	}
	prepared.State = "QUEUED"
	return InputReady{prepared, a.id, a.digest, a.bytes, a.expires, false}, nil
}
