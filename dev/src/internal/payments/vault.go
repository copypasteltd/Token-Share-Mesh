package payments

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"p11.local/compute/shared/strictjson"
)

const proofCipherProfile = "P11_MACHINE_PAY_PROOF_CIPHER_1"

// Vault encrypts recovery credentials before a future durable store receives
// them. The keyring is independently supplied; no key is stored in the blob,
// source tree, payment snapshot, URL, safe_result or ordinary job payload.
type Vault struct {
	mu     sync.RWMutex
	active string
	keys   map[string][]byte
	closed bool
}
type cipherProof struct {
	Profile    string    `json:"profile"`
	KeyID      string    `json:"key_id"`
	Scope      Scope     `json:"scope"`
	Expires    time.Time `json:"expires_at"`
	Nonce      []byte    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
}

func (v *Vault) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[payment proof vault redacted]")
}
func (v *Vault) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func NewVault(active string, keys map[string][]byte) (*Vault, error) {
	if !reference.MatchString(active) || len(keys) < 1 || len(keys) > 8 {
		return nil, ErrProof
	}
	for id, key := range keys {
		if !reference.MatchString(id) || len(key) != 32 {
			return nil, ErrProof
		}
	}
	if keys[active] == nil {
		return nil, ErrProof
	}
	copied := map[string][]byte{}
	for id, key := range keys {
		copied[id] = append([]byte{}, key...)
	}
	return &Vault{active: active, keys: copied}, nil
}
func (v *Vault) Close() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	for _, key := range v.keys {
		clear(key)
	}
	v.keys = nil
}
func proofAAD(scope Scope, key string, expires time.Time) []byte {
	raw, _ := json.Marshal(struct {
		Profile, KeyID string
		Scope          Scope
		Expires        time.Time
	}{proofCipherProfile, key, scope, expires})
	return raw
}
func (v *Vault) Seal(scope Scope, p *Proof, expires, now time.Time) ([]byte, error) {
	if v == nil || !scope.Valid() || now.IsZero() || !expires.After(now) || expires.After(now.Add(30*time.Minute)) {
		return nil, ErrProof
	}
	raw, e := p.copyRaw()
	if e != nil {
		return nil, ErrProof
	}
	defer clear(raw)
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
	expires = expires.UTC()
	aad := proofAAD(scope, v.active, expires)
	defer clear(aad)
	sealed := gcm.Seal(nil, nonce, raw, aad)
	defer clear(sealed)
	return json.Marshal(cipherProof{proofCipherProfile, v.active, scope, expires, nonce, sealed})
}
func (v *Vault) Open(scope Scope, encrypted []byte, now time.Time) (*Proof, error) {
	if v == nil || !scope.Valid() || now.IsZero() || len(encrypted) > 8192 {
		return nil, ErrProof
	}
	var record cipherProof
	if strictjson.Decode(encrypted, &record, 8192) != nil || record.Profile != proofCipherProfile || record.Scope != scope || !now.Before(record.Expires) ||
		!reference.MatchString(record.KeyID) || len(record.Nonce) != 12 || len(record.Ciphertext) < 16 || len(record.Ciphertext) > 4112 {
		return nil, ErrProof
	}
	defer clear(record.Ciphertext)
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return nil, ErrProof
	}
	block, e := aes.NewCipher(v.keys[record.KeyID])
	if e != nil {
		return nil, ErrProof
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrProof
	}
	aad := proofAAD(scope, record.KeyID, record.Expires)
	defer clear(aad)
	raw, e := gcm.Open(nil, record.Nonce, record.Ciphertext, aad)
	if e != nil {
		return nil, ErrProof
	}
	defer clear(raw)
	return decodeProof(raw)
}
