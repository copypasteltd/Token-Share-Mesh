// Package endpoint owns separate content, identity and receipt keys under the
// enclosing node's OS-protected master and original single-writer lock.
package endpoint

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/shared/endpointwire"
	"strconv"
	"time"
)

const Profile = "P11_ENDPOINT_KEYS_1"
const BundleProfile = endpointwire.BundleProfile
const MaximumJournalBytes = 256 << 10
const MaximumLocalKeys = 32
const MaximumPeerKeys = 128
const MaximumActiveUses = 2
const SignatureProfile = endpointwire.SignatureProfile

var ErrState = errors.New("original endpoint key state unavailable; do not recreate")
var ErrTrust = endpointwire.ErrTrust
var ErrClosed = errors.New("endpoint key use unavailable")

type PublicKey = endpointwire.PublicKey
type Bundle = endpointwire.Bundle
type Transition = endpointwire.Transition

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
		return nil, ErrState
	}
	return b, nil
}
func stamp(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil || t.UTC().Format(time.RFC3339Nano) != v {
		return time.Time{}, ErrState
	}
	return t, nil
}
func version(v string) (uint64, error) {
	n, e := strconv.ParseUint(v, 10, 63)
	if e != nil || n < 1 || strconv.FormatUint(n, 10) != v {
		return 0, ErrState
	}
	return n, nil
}
func key(id, algorithm string, public []byte) PublicKey {
	return PublicKey{id, algorithm, base64.RawURLEncoding.EncodeToString(public), Hash(public)}
}
func randomID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", ErrState
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

type secretBytes []byte

func (b *secretBytes) UnmarshalJSON(raw []byte) error {
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ErrState
	}
	out, e := base64.StdEncoding.Strict().DecodeString(v)
	if e != nil || base64.StdEncoding.EncodeToString(out) != v {
		clear(out)
		return ErrState
	}
	*b = out
	return nil
}

type localKey struct {
	Bundle        Bundle      `json:"bundle"`
	Status        string      `json:"status"`
	Rotation      *Transition `json:"original_rotation,omitempty"`
	IdentitySeed  secretBytes `json:"identity_seed,omitempty"`
	ContentSecret secretBytes `json:"content_secret,omitempty"`
	ReceiptSeed   secretBytes `json:"receipt_seed,omitempty"`
}

func (k *localKey) clear() {
	clear(k.IdentitySeed)
	clear(k.ContentSecret)
	clear(k.ReceiptSeed)
	k.IdentitySeed = nil
	k.ContentSecret = nil
	k.ReceiptSeed = nil
}
func generate(owner, endpoint, role, v string, now, expires time.Time) (_ localKey, answer error) {
	if sodium.Available() != nil {
		return localKey{}, sodium.ErrUnavailable
	}
	k := localKey{Status: "ACTIVE", IdentitySeed: make([]byte, 32)}
	defer func() {
		if answer != nil {
			k.clear()
		}
	}()
	if _, e := rand.Read(k.IdentitySeed); e != nil {
		return localKey{}, ErrState
	}
	ip := ed25519.NewKeyFromSeed(k.IdentitySeed)
	defer clear(ip)
	cp, cs, e := sodium.Keypair()
	if e != nil {
		return localKey{}, e
	}
	k.ContentSecret = cs
	ik, e := randomID("sign")
	if e != nil {
		return localKey{}, e
	}
	ck, e := randomID("recv")
	if e != nil {
		return localKey{}, e
	}
	k.Bundle = Bundle{Profile: BundleProfile, Owner: owner, Endpoint: endpoint, Role: role, Version: v, Identity: key(ik, "Ed25519", ip.Public().(ed25519.PublicKey)), Content: key(ck, "X25519_SEALED_BOX", cp), NotBefore: now.UTC().Format(time.RFC3339Nano), Expires: expires.UTC().Format(time.RFC3339Nano), SignatureProfile: SignatureProfile}
	if role == "BUYER" {
		k.ReceiptSeed = make([]byte, 32)
		if _, e = rand.Read(k.ReceiptSeed); e != nil {
			return localKey{}, ErrState
		}
		rp := ed25519.NewKeyFromSeed(k.ReceiptSeed)
		defer clear(rp)
		rid, e := randomID("receipt")
		if e != nil {
			return localKey{}, e
		}
		rkey := key(rid, "Ed25519", rp.Public().(ed25519.PublicKey))
		k.Bundle.Receipt = &rkey
	}
	msg, e := k.Bundle.SigningMessage()
	if e != nil {
		return localKey{}, e
	}
	k.Bundle.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(ip, msg))
	if k.Bundle.Current(now) != nil {
		return localKey{}, ErrTrust
	}
	return k, nil
}
