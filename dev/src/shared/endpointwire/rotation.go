package endpointwire

import (
	"crypto/ed25519"
	"p11.local/compute/shared/canonicaljson"
	"time"
)

type Transition struct {
	Profile   string `json:"profile_id"`
	Previous  string `json:"previous_bundle_hash"`
	Next      Bundle `json:"next_bundle"`
	Recorded  string `json:"recorded_at"`
	Signature string `json:"old_identity_signature"`
}
type unsignedTransition struct {
	Profile  string `json:"profile_id"`
	Previous string `json:"previous_bundle_hash"`
	Next     Bundle `json:"next_bundle"`
	Recorded string `json:"recorded_at"`
}

func (t Transition) Hash() string {
	raw, e := canonicaljson.Encode(t, 12000)
	if e != nil {
		return ""
	}
	return Hash(raw)
}
func cloneTransition(t Transition) Transition { t.Next = cloneBundle(t.Next); return t }
func (t Transition) SigningMessage() ([]byte, error) {
	raw, e := canonicaljson.Encode(unsignedTransition{t.Profile, t.Previous, t.Next, t.Recorded}, 12000)
	if e != nil {
		return nil, ErrTrust
	}
	return append([]byte("P11/E2E/ENDPOINT/ROTATION_SIGNATURE\x00"), raw...), nil
}
func (t Transition) Verify(old Bundle, now time.Time) error {
	a, e := version(old.Version)
	b, ee := version(t.Next.Version)
	recorded, re := stamp(t.Recorded)
	start, _ := stamp(old.NotBefore)
	end, _ := stamp(old.Expires)
	if t.Profile != "P11_ENDPOINT_ROTATION_1" || old.Validate() != nil || t.Next.Current(now) != nil || t.Previous != old.Hash() ||
		t.Next.Owner != old.Owner || t.Next.Endpoint != old.Endpoint || t.Next.Role != old.Role || !distinctKeys(old, t.Next) || e != nil || ee != nil || a == uint64(1<<63-1) || b != a+1 || re != nil || recorded.Before(start) || !recorded.Before(end) || recorded.After(now.Add(30*time.Second)) {
		return ErrTrust
	}
	p, _ := old.Identity.Bytes("Ed25519")
	sig, se := decode(t.Signature, ed25519.SignatureSize)
	msg, me := t.SigningMessage()
	if se != nil || me != nil || !ed25519.Verify(ed25519.PublicKey(p), msg, sig) {
		return ErrTrust
	}
	return nil
}

func distinctKeys(a, b Bundle) bool {
	old := []PublicKey{a.Identity, a.Content}
	next := []PublicKey{b.Identity, b.Content}
	if a.Receipt != nil {
		old = append(old, *a.Receipt)
	}
	if b.Receipt != nil {
		next = append(next, *b.Receipt)
	}
	for _, x := range old {
		for _, y := range next {
			if x.ID == y.ID || x.Public == y.Public || x.Fingerprint == y.Fingerprint {
				return false
			}
		}
	}
	return true
}
