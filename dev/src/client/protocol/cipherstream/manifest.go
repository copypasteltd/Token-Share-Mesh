package cipherstream

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"p11.local/compute/shared/canonicaljson"
	"time"
)

// Manifest contains only cipher hashes. The DATA/plaintext digest remains
// inside encrypted END and must never be copied into a platform record.
type Manifest struct {
	Profile          string `json:"profile_id"`
	ContextHash      string `json:"context_hash"`
	HeaderHash       string `json:"header_hash"`
	CipherBytes      string `json:"cipher_bytes"`
	CipherHash       string `json:"cipher_sha256"`
	Signer           string `json:"signer_key_id"`
	Expires          string `json:"expires_at"`
	SignatureProfile string `json:"signature_profile_id"`
	Signature        string `json:"signature"`
}
type unsignedManifest struct {
	Profile          string `json:"profile_id"`
	ContextHash      string `json:"context_hash"`
	HeaderHash       string `json:"header_hash"`
	CipherBytes      string `json:"cipher_bytes"`
	CipherHash       string `json:"cipher_sha256"`
	Signer           string `json:"signer_key_id"`
	Expires          string `json:"expires_at"`
	SignatureProfile string `json:"signature_profile_id"`
}

func (m Manifest) unsigned() unsignedManifest {
	return unsignedManifest{m.Profile, m.ContextHash, m.HeaderHash, m.CipherBytes, m.CipherHash, m.Signer, m.Expires, m.SignatureProfile}
}
func (m Manifest) Verify(c Context, headerHash, cipherHash string, cipherBytes int64, public ed25519.PublicKey, now time.Time) error {
	expiry, e := CanonicalTime(m.Expires)
	deadline, de := CanonicalTime(c.Deadline)
	if m.Profile != "P11_CIPHER_MANIFEST_1" || m.SignatureProfile != SignatureProfile || m.ContextHash != contextHash(c) || m.HeaderHash != headerHash || m.CipherHash != cipherHash || m.Signer != c.SenderKey ||
		!ValidHash(m.HeaderHash) || !ValidHash(m.CipherHash) || len(public) != ed25519.PublicKeySize || e != nil || de != nil || !now.Before(expiry) || expiry.After(deadline) {
		return ErrEnvelope
	}
	n, e := Counter(m.CipherBytes, false, 63)
	if e != nil || cipherBytes < 1 || n != uint64(cipherBytes) || cipherBytes > MaximumObjectBytes {
		return ErrEnvelope
	}
	if c.Purpose == "INPUT" && expiry.After(deadline) {
		return ErrEnvelope
	}
	sig, e := decode64(m.Signature, ed25519.SignatureSize)
	if e != nil {
		return e
	}
	msg, e := signedBytes("MANIFEST_SIGNATURE", m.unsigned(), 8192)
	if e != nil || !ed25519.Verify(public, msg, sig) {
		return ErrEnvelope
	}
	return nil
}
func (m Manifest) Bytes() ([]byte, error) { return canonicaljson.Encode(m, 8192) }
func (m Manifest) Hash() string {
	raw, e := m.Bytes()
	if e != nil {
		return ""
	}
	return Hash(raw)
}
func (w *Writer) Manifest(expires time.Time, now time.Time) (Manifest, error) {
	if w == nil || w.closed || !w.state.ended || w.manifested {
		return Manifest{}, ErrClosed
	}
	m := Manifest{Profile: "P11_CIPHER_MANIFEST_1", ContextHash: contextHash(w.context), HeaderHash: hex.EncodeToString(w.headerHash[:]), CipherBytes: decimalBytes(w.wireBytes), CipherHash: hex.EncodeToString(w.wire.Sum(nil)), Signer: w.context.SenderKey, Expires: expires.UTC().Format(time.RFC3339Nano), SignatureProfile: SignatureProfile}
	msg, e := signedBytes("MANIFEST_SIGNATURE", m.unsigned(), 8192)
	if e != nil {
		return Manifest{}, e
	}
	m.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(w.signer, msg))
	if m.Verify(w.context, m.HeaderHash, m.CipherHash, w.wireBytes, w.signer.Public().(ed25519.PublicKey), now) != nil {
		return Manifest{}, ErrEnvelope
	}
	w.manifested = true
	w.Close()
	return m, nil
}
