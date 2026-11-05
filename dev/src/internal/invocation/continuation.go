package invocation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/identity"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// Continuations owns only an explicitly configured execution signing key.
// Claims remains verifier-only; neither service obtains input/content keys.
type Continuations struct {
	claims *Claims
	keyID  string
	mu     sync.RWMutex
	signer ed25519.PrivateKey
}

func NewContinuations(claims *Claims, keyID string, signer ed25519.PrivateKey) (*Continuations, error) {
	if claims == nil || claims.db == nil || !ids.Valid(keyID) || len(signer) != ed25519.PrivateKeySize {
		return nil, fault.New(fault.Invalid, "Explicit continuation verifier and signing identity required")
	}
	derived := ed25519.NewKeyFromSeed(signer.Seed())
	defer clear(derived)
	claims.mu.RLock()
	matched := bytes.Equal(derived, signer) && bytes.Equal(derived.Public().(ed25519.PublicKey), claims.verifiers[keyID])
	claims.mu.RUnlock()
	if !matched {
		return nil, fault.New(fault.Invalid, "Continuation signer must match the independently configured verifier")
	}
	return &Continuations{claims: claims, keyID: keyID, signer: append(ed25519.PrivateKey(nil), signer...)}, nil
}

func (s *Continuations) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.signer)
	s.signer = nil
}

func (s *Continuations) UsesClaims(claims *Claims) bool { return s != nil && s.claims == claims }

func minDeadline(values ...time.Time) time.Time {
	var end time.Time
	for _, value := range values {
		if end.IsZero() || value.Before(end) {
			end = value
		}
	}
	return end
}

func (s *Continuations) RecordProgress(ctx context.Context, p identity.Principal, attempt string, in devicecontrol.ProgressWrite, key string) (ReportResult, error) {
	if s == nil || s.claims == nil {
		return ReportResult{}, fault.New(fault.Unavailable, "Continuation authority unavailable")
	}
	return s.claims.recordProgress(ctx, p, attempt, in, key, s.renewTx)
}

func (s *Continuations) renewTx(ctx context.Context, tx pgx.Tx, p identity.Principal, v reportAuthority, ack devicecontrol.Ack) (*executionlease.Lease, error) {
	// Avoid issuing a new revision on every metadata sample. This does not
	// accept a late reply: the current signed window must still be live.
	if v.expires.Sub(v.now) > 20*time.Second {
		return nil, nil
	}
	var version int64
	var signerID string
	if err := tx.QueryRow(ctx, `SELECT d.version,l.issuer_key_id FROM p11.devices d JOIN p11.execution_leases l ON l.claim_device_id=d.id WHERE l.id=$1 AND d.id=$2`, v.lease.ID, p.DeviceID).Scan(&version, &signerID); err != nil {
		return nil, err
	}
	if signerID != s.keyID {
		return nil, fault.New(fault.Unavailable, "Original execution signer is unavailable for continuation")
	}
	// Fresh admission rechecks the original buyer key/route, settled funding,
	// seller/device qualification, input, full reserved budget and both fences.
	// Failure rolls back the report; it cannot extend time or mint a new task.
	original, err := s.claims.authorizeTx(ctx, tx, p, v.lease.ID, v.lease.Capability, version)
	if err != nil {
		return nil, err
	}
	a, err := executionlease.SignedIdentity(original.Lease)
	if err != nil {
		return nil, err
	}
	b, err := executionlease.SignedIdentity(v.lease)
	if err != nil || a != b || original.Lease.Revision >= math.MaxInt32 {
		return nil, fault.New(fault.Conflict, "Original continuation revision changed")
	}
	next := original.Lease
	expires := minDeadline(original.now.Add(executionlease.MaximumTerm), original.deadline)
	if !expires.After(v.expires) {
		return nil, nil
	}
	next.Revision++
	next.Issued, next.Expires = original.now.UTC().Format(time.RFC3339Nano), expires.UTC().Format(time.RFC3339Nano)
	// NotBefore, hard deadline and every original contract/engine/asset field
	// stay fixed. The old signed payload is retained in the append-only ledger.
	message, err := executionlease.Message(next)
	if err != nil || !executionlease.SameOwner(v.lease, next) {
		return nil, fault.New(fault.Conflict, "Continuation cannot replace original authority")
	}
	s.mu.RLock()
	if len(s.signer) != ed25519.PrivateKeySize {
		s.mu.RUnlock()
		return nil, fault.New(fault.Unavailable, "Continuation signer is closed")
	}
	next.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.signer, message))
	s.mu.RUnlock()
	identity, err := executionlease.SignedIdentity(next)
	if err != nil {
		return nil, err
	}
	previous, err := json.Marshal(v.lease)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.native_lease_continuations(id,report_id,lease_id,attempt_id,job_id,device_id,seller_id,resource_id,fencing_token,resource_lock_generation,previous_revision,lease_revision,previous_identity,signed_identity,previous_payload,signed_payload,previous_expires_at,expires_at,recorded_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, ids.New("continuation"), ack.ID, next.ID, next.Attempt, next.Job, next.Device, next.Seller, next.Resource, next.Fence, next.Generation, v.lease.Revision, next.Revision, a, identity, previous, payload, v.expires, expires, original.now)
	if err != nil {
		return nil, err
	}
	updated, err := tx.Exec(ctx, `UPDATE p11.execution_leases SET lease_revision=$2,issued_at=$3,expires_at=$4,signed_payload=$5,signature=$6 WHERE id=$1 AND lease_revision=$7 AND expires_at=$8 AND state='ACKED'`, next.ID, next.Revision, original.now, expires, payload, next.Signature, v.lease.Revision, v.expires)
	if err != nil {
		return nil, err
	}
	if updated.RowsAffected() != 1 {
		return nil, fault.New(fault.Conflict, "Original live lease continuation was not applied")
	}
	updated, err = tx.Exec(ctx, `UPDATE p11.resource_locks SET expires_at=$2,version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1 AND phase='ACTIVE' AND owner_attempt_id=$3 AND owner_session_id IS NULL AND lock_generation=$4 AND expires_at=$5`, next.Resource, expires, next.Attempt, next.Generation, v.expires)
	if err != nil {
		return nil, err
	}
	if updated.RowsAffected() != 1 {
		return nil, fault.New(fault.Conflict, "Original physical owner cannot be extended")
	}
	return &next, nil
}
