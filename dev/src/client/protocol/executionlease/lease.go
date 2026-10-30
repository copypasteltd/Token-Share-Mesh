// Package executionlease verifies metadata-only execution authority. It does not
// establish platform trust, approve an engine, decrypt content or reserve funds.
package executionlease

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"slices"
	"strconv"
	"strings"
	"time"
)

const Profile = "P11_EXECUTION_LEASE_ED25519_1"
const MaximumBody = 16384
const MaximumTerm = 45 * time.Second

var ErrLease = errors.New("execution lease is invalid, untrusted, expired or outside its approved plan")

// Lease implements the existing control OpenAPI Lease, not an alternative job
// contract. References are opaque IDs; no prompt, URL, path or content key fits.
type Lease struct {
	ID           string   `json:"lease_id"`
	Job          string   `json:"job_id"`
	Attempt      string   `json:"attempt_id"`
	Order        string   `json:"order_id"`
	Seller       string   `json:"seller_id"`
	Device       string   `json:"device_id"`
	Resource     string   `json:"resource_id"`
	Generation   string   `json:"resource_lock_generation"`
	Capability   string   `json:"capability_id"`
	Manifest     string   `json:"execution_manifest_ref"`
	ManifestHash string   `json:"execution_manifest_hash"`
	Environment  string   `json:"kernel_environment_digest"`
	Revision     int64    `json:"lease_revision"`
	Issued       string   `json:"issued_at"`
	NotBefore    string   `json:"not_before"`
	HardDeadline string   `json:"hard_deadline"`
	Fence        string   `json:"fencing_token"`
	Expires      string   `json:"expires_at"`
	ContractHash string   `json:"contract_hash"`
	InputAssets  []string `json:"input_asset_refs,omitempty"`
	Envelope     string   `json:"envelope_ref,omitempty"`
	Profile      string   `json:"signature_profile_id"`
	Signature    string   `json:"signature"`
}

// Plan must come from independently approved order/manifest/kernel state, never
// be constructed by copying the incoming lease. Trust pin provisioning and the
// platform issuer are separate, still closed integration steps.
type Plan struct {
	Job, Attempt, Order, Seller, Device, Resource, Generation, Capability string
	Manifest, ManifestHash, Environment, Fence, ContractHash              string
	HardDeadline                                                          time.Time
}

// Verified cannot be populated by an untrusted JSON decoder. Its accessors
// return copies, including the only slice, so verification cannot be mutated.
type Verified struct {
	lease Lease
	valid bool
}

func Counter(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != s {
		return 0, ErrLease
	}
	return n, nil
}
func Hash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}
func instant(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Location() != time.UTC || t.Format(time.RFC3339Nano) != s || t.Year() < 2000 || t.Year() > 2100 {
		return time.Time{}, ErrLease
	}
	return t, nil
}
func (l Lease) validate() error {
	for _, id := range []string{l.ID, l.Job, l.Attempt, l.Order, l.Seller, l.Device, l.Resource, l.Capability, l.Manifest} {
		if !ids.Valid(id) || len(id) > 128 {
			return ErrLease
		}
	}
	if l.Profile != Profile || !Hash(l.ManifestHash) || !Hash(l.Environment) || !Hash(l.ContractHash) || l.Revision < 1 || l.Revision > 2147483647 || len(l.InputAssets) > 50 || l.Envelope != "" && !ids.Valid(l.Envelope) {
		return ErrLease
	}
	seen := map[string]bool{}
	for _, id := range l.InputAssets {
		if !ids.Valid(id) || len(id) > 128 || seen[id] {
			return ErrLease
		}
		seen[id] = true
	}
	g, e := Counter(l.Generation)
	f, e2 := Counter(l.Fence)
	if e != nil || e2 != nil || g == 0 || f == 0 {
		return ErrLease
	}
	i, e := instant(l.Issued)
	n, e2 := instant(l.NotBefore)
	x, e3 := instant(l.Expires)
	h, e4 := instant(l.HardDeadline)
	if e != nil || e2 != nil || e3 != nil || e4 != nil || n.After(i) || !i.Before(x) || x.After(h) || x.Sub(i) > MaximumTerm {
		return ErrLease
	}
	return nil
}

// Message uses a domain-separated, fixed-order ASCII field encoding. Each
// field is one LF-terminated line; validated IDs/digests/times contain no LF.
// The asset count precedes individually ordered assets; absent/empty assets
// both mean zero. This is not JCS or the customer-content envelope signature.
func Message(l Lease) ([]byte, error) {
	if l.validate() != nil {
		return nil, ErrLease
	}
	fields := []string{Profile, l.ID, l.Job, l.Attempt, l.Order, l.Seller, l.Device, l.Resource, l.Generation, l.Capability, l.Manifest, l.ManifestHash, l.Environment, strconv.FormatInt(l.Revision, 10), l.Issued, l.NotBefore, l.HardDeadline, l.Fence, l.Expires, l.ContractHash, strconv.Itoa(len(l.InputAssets))}
	fields = append(fields, l.InputAssets...)
	fields = append(fields, l.Envelope)
	return []byte(strings.Join(fields, "\n") + "\n"), nil
}

func Verify(raw []byte, public ed25519.PublicKey, plan Plan, now time.Time) (Verified, error) {
	var l Lease
	if len(public) != ed25519.PublicKeySize || strictjson.Decode(raw, &l, MaximumBody) != nil || l.validate() != nil {
		return Verified{}, ErrLease
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || strings.TrimSpace(string(fields["input_asset_refs"])) == "null" {
		return Verified{}, ErrLease
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(l.Signature)
	message, e := Message(l)
	if err != nil || e != nil || len(sig) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(sig) != l.Signature || !ed25519.Verify(public, message, sig) {
		return Verified{}, ErrLease
	}
	if l.Job != plan.Job || l.Attempt != plan.Attempt || l.Order != plan.Order || l.Seller != plan.Seller || l.Device != plan.Device || l.Resource != plan.Resource || l.Generation != plan.Generation || l.Capability != plan.Capability || l.Manifest != plan.Manifest || l.ManifestHash != plan.ManifestHash || l.Environment != plan.Environment || l.Fence != plan.Fence || l.ContractHash != plan.ContractHash || l.HardDeadline != plan.HardDeadline.UTC().Format(time.RFC3339Nano) {
		return Verified{}, ErrLease
	}
	v := Verified{lease: l, valid: true}
	if !v.ValidAt(now) {
		return Verified{}, ErrLease
	}
	return v, nil
}
func (v Verified) ValidAt(now time.Time) bool {
	if !v.valid {
		return false
	}
	i, _ := instant(v.lease.Issued)
	n, _ := instant(v.lease.NotBefore)
	x, _ := instant(v.lease.Expires)
	h, _ := instant(v.lease.HardDeadline)
	return !now.Before(i) && !now.Before(n) && now.Before(x) && now.Before(h)
}
func (v Verified) Details() Lease {
	l := v.lease
	l.InputAssets = slices.Clone(l.InputAssets)
	return l
}
func (v Verified) Metadata() []byte {
	if !v.valid {
		return nil
	}
	b, _ := json.Marshal(v.lease)
	return b
}
func (v Verified) Digest() string {
	h := sha256.Sum256(v.Metadata())
	return hex.EncodeToString(h[:])
}

// SameOwner excludes only the revision/issue/expiry/signature fields. A renewal
// cannot change recipient, terms, assets, envelope, hard deadline or either fence.
func SameOwner(a, b Lease) bool {
	a.Revision, b.Revision = 1, 1
	a.Issued, b.Issued, a.Expires, b.Expires = "", "", "", ""
	a.Signature, b.Signature = "", ""
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
