package payments

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"time"

	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const refundReceiptCipherProfile = "P11_ORIGINAL_REFUND_SIGNED_RECEIPT_1"

type refundReceiptMetadata struct {
	Scope        RefundScope `json:"scope"`
	IntentHash   string      `json:"intent_hash"`
	Kind         string      `json:"kind"`
	Method       string      `json:"method"`
	Amount       money.Fen   `json:"amount_fen"`
	Cumulative   money.Fen   `json:"cumulative_fen"`
	EvidenceHash string      `json:"evidence_hash"`
	Observed     time.Time   `json:"observed_at"`
}
type encryptedRefundReceipt struct {
	Profile    string                `json:"profile"`
	KeyID      string                `json:"key_id"`
	Metadata   refundReceiptMetadata `json:"metadata"`
	Nonce      []byte                `json:"nonce"`
	Ciphertext []byte                `json:"ciphertext"`
}

func refundReceiptAAD(key string, m refundReceiptMetadata) []byte {
	raw, _ := json.Marshal(struct {
		Profile, KeyID string
		Metadata       refundReceiptMetadata
	}{refundReceiptCipherProfile, key, m})
	return raw
}
func validRefundReceiptMetadata(m refundReceiptMetadata) bool {
	if !m.Scope.Valid() || !hashPattern.MatchString(m.IntentHash) || m.Amount < 1 || m.Amount > 5000 || m.Cumulative < 0 || m.Cumulative > 5000 ||
		!hashPattern.MatchString(m.EvidenceHash) || m.Observed.IsZero() {
		return false
	}
	switch m.Kind {
	case "REFUND_SUBMITTED":
		return m.Method == refundMethod
	case "REFUND_QUERY_UNRESOLVED", "REFUND_CONFIRMED":
		return m.Method == refundQueryMethod && m.Cumulative == 0
	default:
		return false
	}
}

// SealRefundReceipt uses the financial archive keyring, separate from proof
// credentials. The complete original signed envelope (including signatures,
// spacing and vendor optional fields) is authenticated under original refund,
// intent, decision, route, method, amount and observation metadata.
func (v *Vault) SealRefundReceipt(o RefundObservation) ([]byte, error) {
	m := refundReceiptMetadata{o.scope, o.intentHash, o.kind, o.method, o.amount, o.cumulative, o.evidenceHash, o.observed.UTC()}
	if v == nil || !validRefundReceiptMetadata(m) || o.receipt == nil {
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
	aad := refundReceiptAAD(v.active, m)
	defer clear(aad)
	ciphertext := gcm.Seal(nil, nonce, o.receipt.raw, aad)
	defer clear(ciphertext)
	return json.Marshal(encryptedRefundReceipt{refundReceiptCipherProfile, v.active, m, nonce, ciphertext})
}

// OpenRefundReceipt authenticates an archived original blob only. It returns
// owned bytes for a trusted archive reader, NOT an Observation, execution
// authority, refund success or fresh RSA reverification. Clear owned bytes.
func (v *Vault) OpenRefundReceipt(i RefundIntent, encoded []byte) ([]byte, error) {
	if v == nil || i.Profile != RefundProfile || !i.Scope.Valid() || i.Amount < 1 || i.Amount > 5000 || len(encoded) > 100000 {
		return nil, ErrProof
	}
	var r encryptedRefundReceipt
	if strictjson.Decode(encoded, &r, 100000) != nil {
		return nil, ErrProof
	}
	defer clear(r.Ciphertext)
	if r.Profile != refundReceiptCipherProfile || !reference.MatchString(r.KeyID) || !validRefundReceiptMetadata(r.Metadata) ||
		r.Metadata.Scope != i.Scope || r.Metadata.IntentHash != i.Digest() || r.Metadata.Amount != i.Amount || r.Metadata.Cumulative > i.Collected ||
		r.Metadata.Observed.Before(i.CreatedAt) || len(r.Nonce) != 12 || len(r.Ciphertext) < 18 || len(r.Ciphertext) > MaximumChannelResponse+16 {
		return nil, ErrProof
	}
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
	aad := refundReceiptAAD(r.KeyID, r.Metadata)
	defer clear(aad)
	raw, e := gcm.Open(nil, r.Nonce, r.Ciphertext, aad)
	if e != nil {
		return nil, ErrProof
	}
	return raw, nil
}
