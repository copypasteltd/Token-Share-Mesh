package endpoint

import (
	"crypto/ed25519"
	"encoding/base64"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/ids"
	"strconv"
	"time"
)

type Verification struct {
	Source      string `json:"source"`
	Fingerprint string `json:"independently_verified_identity_fingerprint"`
	Reference   string `json:"verification_reference"`
}

func validReference(v string) bool { return ids.Valid(v) }
func (v *Vault) PinInitial(b Bundle, proof Verification, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() || b.Current(now) != nil || proof.Source != "OUT_OF_BAND" || proof.Fingerprint != b.Identity.Fingerprint || !validReference(proof.Reference) {
		return ErrTrust
	}
	for _, p := range v.state.Peers {
		if p.Bundle.Hash() == b.Hash() && p.Status == "ACTIVE" {
			return nil
		}
		if p.Bundle.Endpoint == b.Endpoint {
			return ErrTrust
		}
	}
	if len(v.state.Peers) >= MaximumPeerKeys {
		return ErrState
	}
	v.state.Peers = append(v.state.Peers, peerKey{Bundle: cloneBundle(b), Status: "ACTIVE", Verified: now.UTC().Format(time.RFC3339Nano), Reference: proof.Reference})
	return v.saveLocked()
}

type PeerScope struct {
	Owner, Endpoint, Role, BundleHash string
	AllowRetired                      bool
}

func (v *Vault) Peer(s PeerScope, now time.Time) (Bundle, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return Bundle{}, ErrClosed
	}
	for _, p := range v.state.Peers {
		b := p.Bundle
		if b.Hash() == s.BundleHash && b.Owner == s.Owner && b.Endpoint == s.Endpoint && b.Role == s.Role &&
			(p.Status == "ACTIVE" || s.AllowRetired && p.Status == "RETIRED") && b.Current(now) == nil {
			return cloneBundle(b), nil
		}
	}
	return Bundle{}, ErrTrust
}
func (v *Vault) RevokePeer(bundleHash string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return ErrClosed
	}
	for i := range v.state.Peers {
		p := &v.state.Peers[i]
		if p.Bundle.Hash() == bundleHash {
			if p.Status == "REVOKED" {
				return nil
			}
			p.Status = "REVOKED"
			if v.saveLocked() != nil {
				return ErrState
			}
			for _, u := range v.uses {
				if u.peer == bundleHash {
					u.cancel()
				}
			}
			return nil
		}
	}
	return ErrTrust
}

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
func (t Transition) message() ([]byte, error) {
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
		t.Next.Owner != old.Owner || t.Next.Endpoint != old.Endpoint || t.Next.Role != old.Role || e != nil || ee != nil || a == uint64(1<<63-1) || b != a+1 || re != nil || recorded.Before(start) || !recorded.Before(end) || recorded.After(now.Add(30*time.Second)) {
		return ErrTrust
	}
	p, _ := old.Identity.Bytes("Ed25519")
	sig, se := decode(t.Signature, ed25519.SignatureSize)
	msg, me := t.message()
	if se != nil || me != nil || !ed25519.Verify(ed25519.PublicKey(p), msg, sig) {
		return ErrTrust
	}
	return nil
}
func (v *Vault) RotateLocal(oldHash string, now, expires time.Time) (Transition, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return Transition{}, ErrState
	}
	for _, k := range v.state.Local {
		if k.Rotation != nil && k.Rotation.Previous == oldHash && k.Status == "ACTIVE" {
			if k.Bundle.Expires != expires.UTC().Format(time.RFC3339Nano) {
				return Transition{}, ErrTrust
			}
			return cloneTransition(*k.Rotation), nil
		}
	}
	if len(v.state.Local) >= MaximumLocalKeys {
		return Transition{}, ErrState
	}
	for i := range v.state.Local {
		old := &v.state.Local[i]
		if old.Bundle.Hash() != oldHash || old.Status != "ACTIVE" || old.Bundle.Current(now) != nil {
			continue
		}
		n, e := version(old.Bundle.Version)
		if e != nil || n == uint64(1<<63-1) {
			return Transition{}, ErrState
		}
		next, e := generate(old.Bundle.Owner, old.Bundle.Endpoint, old.Bundle.Role, strconv.FormatUint(n+1, 10), now, expires)
		if e != nil {
			return Transition{}, e
		}
		t := Transition{Profile: "P11_ENDPOINT_ROTATION_1", Previous: oldHash, Next: cloneBundle(next.Bundle), Recorded: now.UTC().Format(time.RFC3339Nano)}
		msg, e := t.message()
		if e != nil {
			next.clear()
			return Transition{}, e
		}
		key := ed25519.NewKeyFromSeed(old.IdentitySeed)
		t.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, msg))
		clear(key)
		if t.Verify(old.Bundle, now) != nil {
			next.clear()
			return Transition{}, ErrTrust
		}
		stored := cloneTransition(t)
		next.Rotation = &stored
		old.Status = "RETIRED"
		v.state.Local = append(v.state.Local, next)
		if v.saveLocked() != nil {
			return Transition{}, ErrState
		}
		return cloneTransition(t), nil
	}
	return Transition{}, ErrTrust
}
func (v *Vault) RotatePeer(t Transition, now time.Time) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return ErrState
	}
	for _, p := range v.state.Peers {
		if p.Bundle.Hash() == t.Next.Hash() && p.Previous == t.Previous && p.Status == "ACTIVE" {
			if p.RotationHash != t.Hash() {
				return ErrTrust
			}
			for _, old := range v.state.Peers {
				if old.Bundle.Hash() == t.Previous && (old.Status == "ACTIVE" || old.Status == "RETIRED") {
					return t.Verify(old.Bundle, now)
				}
			}
			return ErrTrust
		}
	}
	if len(v.state.Peers) >= MaximumPeerKeys {
		return ErrState
	}
	for i := range v.state.Peers {
		p := &v.state.Peers[i]
		if p.Bundle.Hash() != t.Previous || p.Status != "ACTIVE" {
			continue
		}
		if t.Verify(p.Bundle, now) != nil {
			return ErrTrust
		}
		reference := p.Reference
		p.Status = "RETIRED"
		v.state.Peers = append(v.state.Peers, peerKey{Bundle: cloneBundle(t.Next), Status: "ACTIVE", Verified: now.UTC().Format(time.RFC3339Nano), Reference: reference, Previous: t.Previous, RotationHash: t.Hash()})
		return v.saveLocked()
	}
	return ErrTrust
}
