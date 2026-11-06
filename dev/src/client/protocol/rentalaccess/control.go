package rentalaccess

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
)

const ControlProfile = "P11_RENTAL_CONTROL_ED25519_1"
const MaximumControlBytes = 8192
const MaximumControlTerm = 45 * time.Second

// Control is an authenticated answer to one original, durable request sequence.
// It conveys liveness/revocation only. Neither ALLOW nor an unsigned HTTP Ack
// extends the grant, terms, hard deadline, money or physical session ownership.
// These metadata signatures are not Bearer credentials or content keys.
type Control struct {
	Profile         string `json:"profile_id"`
	Grant           string `json:"grant_id"`
	GrantDigest     string `json:"grant_digest"`
	Order           string `json:"order_id"`
	Session         string `json:"session_id"`
	Seller          string `json:"seller_id"`
	Device          string `json:"device_id"`
	Resource        string `json:"resource_id"`
	Generation      string `json:"resource_lock_generation"`
	Terms           string `json:"terms_revision"`
	TermsHash       string `json:"terms_hash"`
	EnvironmentHash string `json:"kernel_environment_digest"`
	ManifestHash    string `json:"manifest_hash"`
	HardDeadline    string `json:"hard_deadline"`
	Sequence        string `json:"sequence"`
	Decision        string `json:"decision"`
	Issued          string `json:"issued_at"`
	Expires         string `json:"expires_at"`
	KeyID           string `json:"issuer_key_id"`
	Signature       string `json:"signature"`
}

func (c Control) shape() error {
	for _, v := range []string{c.Grant, c.Order, c.Session, c.Seller, c.Device, c.Resource, c.KeyID} {
		if !identifier(v) {
			return ErrAccess
		}
	}
	i, e := utc(c.Issued)
	if e != nil {
		return ErrAccess
	}
	x, e := utc(c.Expires)
	if e != nil {
		return ErrAccess
	}
	h, e := utc(c.HardDeadline)
	if e != nil {
		return ErrAccess
	}
	if c.Profile != ControlProfile || !positive(c.Generation) || !positive(c.Terms) || !positive(c.Sequence) ||
		!executionlease.Hash(c.GrantDigest) || !executionlease.Hash(c.TermsHash) || !executionlease.Hash(c.EnvironmentHash) || !executionlease.Hash(c.ManifestHash) ||
		(c.Decision != "ALLOW" && c.Decision != "STOP") || !x.After(i) || x.Sub(i) > MaximumControlTerm || x.After(h) {
		return ErrAccess
	}
	return nil
}

// Message has fixed field order and exactly one LF after every ASCII field.
// shape rejects arbitrary text/newlines. Signature bytes are never self-signed.
func (c Control) Message() ([]byte, error) {
	if c.shape() != nil {
		return nil, ErrAccess
	}
	return []byte(strings.Join([]string{c.Profile, c.Grant, c.GrantDigest, c.Order, c.Session, c.Seller, c.Device, c.Resource, c.Generation, c.Terms, c.TermsHash, c.EnvironmentHash, c.ManifestHash, c.HardDeadline, c.Sequence, c.Decision, c.Issued, c.Expires, c.KeyID}, "\n") + "\n"), nil
}
func (c Control) matches(v Verified) bool {
	a := v.claims
	x, e := utc(c.Expires)
	return e == nil && v.valid && c.Grant == a.ID && c.GrantDigest == v.digest && c.Order == a.Order && c.Session == a.Session && c.Seller == a.Seller && c.Device == a.Device && c.Resource == a.Resource && c.Generation == a.Generation && c.Terms == a.Terms && c.TermsHash == a.TermsHash && c.EnvironmentHash == a.EnvironmentHash && c.ManifestHash == a.ManifestHash && c.HardDeadline == a.HardDeadline && c.KeyID == v.keyID && !x.After(time.Unix(a.Expires, 0))
}

// SignControl requires the original persisted safe grant metadata. A platform
// caller must reauthorize its current session/funding/owner in the transaction
// that freezes this response; this method alone does not do that admission.
func (s *Signer) SignControl(m Metadata, sequence uint64, decision string, now, expires time.Time) (Control, error) {
	if s == nil || m.Profile != Profile || m.KeyID != s.keyID || m.Claims.Issuer != s.issuer || !m.Claims.validAt(now) || !executionlease.Hash(m.Digest) || sequence == 0 {
		return Control{}, ErrAccess
	}
	a := m.Claims
	c := Control{Profile: ControlProfile, Grant: a.ID, GrantDigest: m.Digest, Order: a.Order, Session: a.Session, Seller: a.Seller, Device: a.Device, Resource: a.Resource, Generation: a.Generation, Terms: a.Terms, TermsHash: a.TermsHash, EnvironmentHash: a.EnvironmentHash, ManifestHash: a.ManifestHash, HardDeadline: a.HardDeadline, Sequence: strconv.FormatUint(sequence, 10), Decision: decision, Issued: now.UTC().Format(time.RFC3339Nano), Expires: expires.UTC().Format(time.RFC3339Nano), KeyID: s.keyID}
	if expires.After(time.Unix(a.Expires, 0)) {
		return Control{}, ErrAccess
	}
	message, e := c.Message()
	if e != nil {
		return Control{}, ErrAccess
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.key) != ed25519.PrivateKeySize {
		return Control{}, ErrAccess
	}
	c.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.key, message))
	return c, nil
}

// VerifiedControl has no public constructor. Layout-independent Identity is
// used for duplicate detection, independently of a transport JSON body's hash.
type VerifiedControl struct {
	value    Control
	identity string
	valid    bool
}

func (c VerifiedControl) Details() Control {
	if !c.valid {
		return Control{}
	}
	return c.value
}
func (c VerifiedControl) Identity() string {
	if !c.valid {
		return ""
	}
	return c.identity
}

func (v *Verifier) VerifyControl(raw []byte, grant Verified, current Expected, now time.Time) (VerifiedControl, error) {
	var c Control
	if v == nil || !grant.ValidAt(now) || !current.binds(grant) || strictjson.Decode(raw, &c, MaximumControlBytes) != nil || !c.matches(grant) {
		return VerifiedControl{}, ErrAccess
	}
	message, e := c.Message()
	if e != nil {
		return VerifiedControl{}, ErrAccess
	}
	i, _ := utc(c.Issued)
	x, _ := utc(c.Expires)
	if now.Before(i) || !now.Before(x) {
		return VerifiedControl{}, ErrAccess
	}
	signature, e := segment(c.Signature, ed25519.SignatureSize)
	if e != nil || len(signature) != ed25519.SignatureSize {
		return VerifiedControl{}, ErrAccess
	}
	v.mu.RLock()
	key := append(ed25519.PublicKey(nil), v.keys[c.KeyID]...)
	v.mu.RUnlock()
	defer clear(key)
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, message, signature) {
		return VerifiedControl{}, ErrAccess
	}
	h := sha256.New()
	h.Write(message)
	h.Write([]byte(c.Signature))
	h.Write([]byte("\n"))
	return VerifiedControl{value: c, identity: hex.EncodeToString(h.Sum(nil)), valid: true}, nil
}
