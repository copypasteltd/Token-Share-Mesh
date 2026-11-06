package payments

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"sync"
	"time"

	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const receiptCipherProfile = "P11_MACHINE_PAY_SIGNED_RECEIPT_1"

type receiptState struct {
	mu     sync.Mutex
	raw    []byte
	closed bool
}

func newReceiptState(raw []byte) *receiptState { return &receiptState{raw: append([]byte{}, raw...)} }

// Close revokes every copy's access to the owned original signed envelope.
// Financial metadata remains a fact; closing is not a reversal or refund.
func (o Observation) Close() {
	if o.receipt != nil {
		o.receipt.mu.Lock()
		defer o.receipt.mu.Unlock()
		o.receipt.closed = true
		clear(o.receipt.raw)
		o.receipt.raw = nil
	}
}

type receiptMetadata struct {
	Scope        Scope     `json:"scope"`
	Kind         string    `json:"kind"`
	Amount       money.Fen `json:"amount_fen"`
	Trade        string    `json:"trade,omitempty"`
	Prepay       string    `json:"prepay,omitempty"`
	EvidenceHash string    `json:"evidence_hash"`
	Observed     time.Time `json:"observed_at"`
}
type encryptedReceipt struct {
	Profile    string          `json:"profile"`
	KeyID      string          `json:"key_id"`
	Metadata   receiptMetadata `json:"metadata"`
	Nonce      []byte          `json:"nonce"`
	Ciphertext []byte          `json:"ciphertext"`
}

func receiptAAD(key string, m receiptMetadata) []byte {
	raw, _ := json.Marshal(struct {
		Profile, KeyID string
		Metadata       receiptMetadata
	}{receiptCipherProfile, key, m})
	return raw
}

// SealReceipt retains the COMPLETE signed response, not a reserialized node or
// a hash alone. It cannot seal an arbitrary caller-created financial fact.
func (v *Vault) SealReceipt(o Observation) ([]byte, error) {
	if v == nil || !o.scope.Valid() || !hashPattern.MatchString(o.evidenceHash) || o.observed.IsZero() || o.receipt == nil ||
		(o.kind != "PRECREATED" && o.kind != "VERIFIED_PAID") {
		return nil, ErrProof
	}
	o.receipt.mu.Lock()
	defer o.receipt.mu.Unlock()
	if o.receipt.closed || len(o.receipt.raw) < 2 || len(o.receipt.raw) > MaximumChannelResponse {
		return nil, ErrProof
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return nil, ErrProof
	}
	block, e := aes.NewCipher(v.keys[v.active])
	if e != nil {
		return nil, ErrProof
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrProof
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, ErrProof
	}
	m := receiptMetadata{o.scope, o.kind, o.amount, o.trade, o.prepay, o.evidenceHash, o.observed.UTC()}
	aad := receiptAAD(v.active, m)
	defer clear(aad)
	ciphertext := gcm.Seal(nil, nonce, o.receipt.raw, aad)
	defer clear(ciphertext)
	return json.Marshal(encryptedReceipt{receiptCipherProfile, v.active, m, nonce, ciphertext})
}

// OpenReceipt is for a trusted financial archive reader. It authenticates the
// stored envelope and its exact original scope; it does not create a new paid
// Observation or grant execution. Callers must clear the returned owned bytes.
func (v *Vault) OpenReceipt(scope Scope, encoded []byte) ([]byte, error) {
	if v == nil || !scope.Valid() || len(encoded) > 100000 {
		return nil, ErrProof
	}
	var r encryptedReceipt
	if strictjson.Decode(encoded, &r, 100000) != nil || r.Profile != receiptCipherProfile || r.Metadata.Scope != scope || !reference.MatchString(r.KeyID) ||
		len(r.Nonce) != 12 || len(r.Ciphertext) < 18 || len(r.Ciphertext) > MaximumChannelResponse+16 {
		return nil, ErrProof
	}
	defer clear(r.Ciphertext)
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return nil, ErrProof
	}
	block, e := aes.NewCipher(v.keys[r.KeyID])
	if e != nil {
		return nil, ErrProof
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrProof
	}
	aad := receiptAAD(r.KeyID, r.Metadata)
	defer clear(aad)
	raw, e := gcm.Open(nil, r.Nonce, r.Ciphertext, aad)
	if e != nil {
		return nil, ErrProof
	}
	return raw, nil
}
