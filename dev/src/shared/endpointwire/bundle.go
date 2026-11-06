// Package endpointwire validates public endpoint metadata only. It holds no
// private key, vault, native cipher, decryptor or inference dependency.
package endpointwire

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/ids"
	"strconv"
	"time"
)

const BundleProfile = "P11_ENDPOINT_PUBLIC_1"
const SignatureProfile = "ED25519_JCS_1"

var ErrTrust = errors.New("independent endpoint identity required")

type PublicKey struct {
	ID          string `json:"key_id"`
	Algorithm   string `json:"algorithm"`
	Public      string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}
type Bundle struct {
	Profile          string     `json:"profile_id"`
	Owner            string     `json:"owner_id"`
	Endpoint         string     `json:"endpoint_id"`
	Role             string     `json:"role"`
	Version          string     `json:"version"`
	Identity         PublicKey  `json:"identity_signing_key"`
	Content          PublicKey  `json:"content_receiving_key"`
	Receipt          *PublicKey `json:"buyer_receipt_key,omitempty"`
	NotBefore        string     `json:"not_before"`
	Expires          string     `json:"expires_at"`
	SignatureProfile string     `json:"signature_profile_id"`
	Signature        string     `json:"signature"`
}
type unsignedBundle struct {
	Profile          string     `json:"profile_id"`
	Owner            string     `json:"owner_id"`
	Endpoint         string     `json:"endpoint_id"`
	Role             string     `json:"role"`
	Version          string     `json:"version"`
	Identity         PublicKey  `json:"identity_signing_key"`
	Content          PublicKey  `json:"content_receiving_key"`
	Receipt          *PublicKey `json:"buyer_receipt_key,omitempty"`
	NotBefore        string     `json:"not_before"`
	Expires          string     `json:"expires_at"`
	SignatureProfile string     `json:"signature_profile_id"`
}

func (b Bundle) unsigned() unsignedBundle {
	return unsignedBundle{b.Profile, b.Owner, b.Endpoint, b.Role, b.Version, b.Identity, b.Content, b.Receipt, b.NotBefore, b.Expires, b.SignatureProfile}
}
func cloneBundle(b Bundle) Bundle {
	if b.Receipt != nil {
		p := *b.Receipt
		b.Receipt = &p
	}
	return b
}
func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func hashValid(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == v
}
func decode(v string, n int) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(v)
	if e != nil || len(b) != n || base64.RawURLEncoding.EncodeToString(b) != v {
		clear(b)
		return nil, ErrTrust
	}
	return b, nil
}
func stamp(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil || t.UTC().Format(time.RFC3339Nano) != v {
		return time.Time{}, ErrTrust
	}
	return t, nil
}
func version(v string) (uint64, error) {
	n, e := strconv.ParseUint(v, 10, 63)
	if e != nil || n < 1 || strconv.FormatUint(n, 10) != v {
		return 0, ErrTrust
	}
	return n, nil
}
func key(id, algorithm string, public []byte) PublicKey {
	return PublicKey{id, algorithm, base64.RawURLEncoding.EncodeToString(public), Hash(public)}
}
func (k PublicKey) Bytes(algorithm string) ([]byte, error) {
	p, e := decode(k.Public, 32)
	if e != nil || !ids.Valid(k.ID) || k.Algorithm != algorithm || Hash(p) != k.Fingerprint {
		return nil, ErrTrust
	}
	return p, nil
}
func (b Bundle) SigningMessage() ([]byte, error) {
	raw, e := canonicaljson.Encode(b.unsigned(), 8192)
	if e != nil {
		return nil, ErrTrust
	}
	return append([]byte("P11/E2E/ENDPOINT/BUNDLE_SIGNATURE\x00"), raw...), nil
}
func (b Bundle) Validate() error {
	start, e := stamp(b.NotBefore)
	expiry, ee := stamp(b.Expires)
	if b.Profile != BundleProfile || !ids.Valid(b.Owner) || !ids.Valid(b.Endpoint) || b.SignatureProfile != SignatureProfile || e != nil || ee != nil || !expiry.After(start) || expiry.Sub(start) > 90*24*time.Hour {
		return ErrTrust
	}
	if _, e = version(b.Version); e != nil {
		return ErrTrust
	}
	ip, e := b.Identity.Bytes("Ed25519")
	if e != nil {
		return ErrTrust
	}
	cp, e := b.Content.Bytes("X25519_SEALED_BOX")
	if e != nil || string(ip) == string(cp) || b.Identity.ID == b.Content.ID {
		return ErrTrust
	}
	switch b.Role {
	case "BUYER":
		if b.Receipt == nil {
			return ErrTrust
		}
		rp, re := b.Receipt.Bytes("Ed25519")
		if re != nil || string(rp) == string(ip) || string(rp) == string(cp) || b.Receipt.ID == b.Identity.ID || b.Receipt.ID == b.Content.ID {
			return ErrTrust
		}
	case "SELLER":
		if b.Receipt != nil {
			return ErrTrust
		}
	default:
		return ErrTrust
	}
	msg, e := b.SigningMessage()
	sig, se := decode(b.Signature, ed25519.SignatureSize)
	if e != nil || se != nil || !ed25519.Verify(ed25519.PublicKey(ip), msg, sig) {
		return ErrTrust
	}
	return nil
}
func (b Bundle) Current(now time.Time) error {
	if b.Validate() != nil {
		return ErrTrust
	}
	start, _ := stamp(b.NotBefore)
	expiry, _ := stamp(b.Expires)
	if now.Before(start) || !now.Before(expiry) {
		return ErrTrust
	}
	return nil
}
func (b Bundle) Bytes() ([]byte, error) { return canonicaljson.Encode(b, 8192) }
func (b Bundle) Hash() string {
	raw, e := b.Bytes()
	if e != nil {
		return ""
	}
	return Hash(raw)
}
