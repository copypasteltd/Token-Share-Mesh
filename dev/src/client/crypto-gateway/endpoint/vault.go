package endpoint

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"p11.local/compute/shared/strictjson"
	"sync"
	"time"
)

// Journal belongs to the enclosing OS-locked, private native directory.
// Commit atomically replaces and flushes ciphertext; it must not be a cloud
// key store or an independently opened second writer.
type Journal interface {
	Read() ([]byte, error)
	Commit([]byte) error
}
type peerKey struct {
	Bundle       Bundle `json:"bundle"`
	Status       string `json:"status"`
	Verified     string `json:"verified_at"`
	Reference    string `json:"verification_reference"`
	Previous     string `json:"previous_bundle_hash,omitempty"`
	RotationHash string `json:"original_rotation_hash,omitempty"`
}
type vaultState struct {
	Profile string     `json:"profile_id"`
	Binding string     `json:"binding_digest"`
	Local   []localKey `json:"local_keys"`
	Peers   []peerKey  `json:"peer_keys"`
}
type keyUse struct {
	cancel       context.CancelFunc
	bundle, peer string
}
type Vault struct {
	mu                sync.Mutex
	journal           Journal
	wrap              []byte
	state             vaultState
	uses              map[uint64]keyUse
	next              uint64
	active            sync.WaitGroup
	closing, poisoned bool
	closeOnce         sync.Once
	done              chan struct{}
}

func OpenOwned(j Journal, wrap []byte, binding string, create bool) (_ *Vault, answer error) {
	if j == nil || len(wrap) != 32 || !hashValid(binding) {
		return nil, ErrState
	}
	v := &Vault{journal: j, wrap: append([]byte(nil), wrap...), uses: map[uint64]keyUse{}, done: make(chan struct{}), state: vaultState{Profile: Profile, Binding: binding, Local: []localKey{}, Peers: []peerKey{}}}
	defer func() {
		if answer != nil {
			v.clearLocked()
		}
	}()
	sealed, e := j.Read()
	if create {
		if !errors.Is(e, os.ErrNotExist) || len(sealed) != 0 {
			return nil, ErrState
		}
		if v.saveLocked() != nil {
			return nil, ErrState
		}
		return v, nil
	}
	if e != nil || len(sealed) < 28 || len(sealed) > MaximumJournalBytes {
		return nil, ErrState
	}
	g, e := v.aead()
	if e != nil {
		return nil, ErrState
	}
	plain, e := g.Open(nil, sealed[:g.NonceSize()], sealed[g.NonceSize():], v.aad())
	if e != nil {
		return nil, ErrState
	}
	defer clear(plain)
	if strictjson.Decode(plain, &v.state, MaximumJournalBytes-28) != nil || v.state.Binding != binding || v.validateLocked() != nil {
		return nil, ErrState
	}
	return v, nil
}
func (v *Vault) aad() []byte { return []byte(Profile + "\x00" + v.state.Binding) }
func (v *Vault) aead() (cipher.AEAD, error) {
	b, e := aes.NewCipher(v.wrap)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func (v *Vault) validateLocked() error {
	if v.state.Profile != Profile || !hashValid(v.state.Binding) || len(v.state.Local) > MaximumLocalKeys || len(v.state.Peers) > MaximumPeerKeys {
		return ErrState
	}
	seen, active := map[string]bool{}, map[string]bool{}
	for _, k := range v.state.Local {
		b := k.Bundle
		h := b.Hash()
		if b.Validate() != nil || seen[h] {
			return ErrState
		}
		seen[h] = true
		switch k.Status {
		case "ACTIVE", "RETIRED":
			if len(k.IdentitySeed) != 32 || len(k.ContentSecret) != 32 || b.Role == "BUYER" && len(k.ReceiptSeed) != 32 || b.Role == "SELLER" && len(k.ReceiptSeed) != 0 {
				return ErrState
			}
			priv := ed25519.NewKeyFromSeed(k.IdentitySeed)
			ok := Hash(priv.Public().(ed25519.PublicKey)) == b.Identity.Fingerprint
			clear(priv)
			if !ok {
				return ErrState
			}
			if b.Receipt != nil {
				p := ed25519.NewKeyFromSeed(k.ReceiptSeed)
				ok = Hash(p.Public().(ed25519.PublicKey)) == b.Receipt.Fingerprint
				clear(p)
				if !ok {
					return ErrState
				}
			}
		case "REVOKED":
			if len(k.IdentitySeed)+len(k.ContentSecret)+len(k.ReceiptSeed) != 0 {
				return ErrState
			}
		default:
			return ErrState
		}
		if k.Status == "ACTIVE" {
			a := b.Owner + "\x00" + b.Endpoint + "\x00" + b.Role
			if active[a] {
				return ErrState
			}
			active[a] = true
		}
		if k.Rotation != nil {
			if k.Rotation.Next.Hash() != b.Hash() {
				return ErrState
			}
			matched := false
			at, e := stamp(k.Rotation.Recorded)
			if e != nil {
				return ErrState
			}
			for _, old := range v.state.Local {
				if old.Bundle.Hash() == k.Rotation.Previous && k.Rotation.Verify(old.Bundle, at) == nil {
					matched = true
					break
				}
			}
			if !matched {
				return ErrState
			}
		}
	}
	seen, active = map[string]bool{}, map[string]bool{}
	for _, p := range v.state.Peers {
		b := p.Bundle
		h := b.Hash()
		if b.Validate() != nil || seen[h] || !validReference(p.Reference) {
			return ErrState
		}
		seen[h] = true
		if _, e := stamp(p.Verified); e != nil {
			return ErrState
		}
		if p.Previous != "" && (!hashValid(p.Previous) || !hashValid(p.RotationHash)) || p.Previous == "" && p.RotationHash != "" {
			return ErrState
		}
		switch p.Status {
		case "ACTIVE", "RETIRED", "REVOKED":
		default:
			return ErrState
		}
		if p.Status == "ACTIVE" {
			a := b.Owner + "\x00" + b.Endpoint + "\x00" + b.Role
			if active[a] {
				return ErrState
			}
			active[a] = true
		}
	}
	return nil
}
func (v *Vault) failLocked() {
	v.poisoned = true
	for _, u := range v.uses {
		u.cancel()
	}
}
func (v *Vault) saveLocked() error {
	if v.closing || v.poisoned || v.validateLocked() != nil {
		v.failLocked()
		return ErrState
	}
	raw, e := json.Marshal(v.state)
	defer clear(raw)
	if e != nil || len(raw) > MaximumJournalBytes-28 {
		v.failLocked()
		return ErrState
	}
	g, e := v.aead()
	if e != nil {
		v.failLocked()
		return ErrState
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		v.failLocked()
		return ErrState
	}
	sealed := g.Seal(nonce, nonce, raw, v.aad())
	if v.journal.Commit(sealed) != nil {
		v.failLocked()
		return ErrState
	}
	return nil
}
func (v *Vault) readyLocked() bool { return !v.closing && !v.poisoned }
func (v *Vault) CreateLocal(owner, endpoint, role string, now, expires time.Time) (Bundle, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() || len(v.state.Local) >= MaximumLocalKeys {
		return Bundle{}, ErrState
	}
	for _, k := range v.state.Local {
		if k.Bundle.Endpoint == endpoint {
			return Bundle{}, ErrState
		}
	}
	k, e := generate(owner, endpoint, role, "1", now, expires)
	if e != nil {
		return Bundle{}, e
	}
	v.state.Local = append(v.state.Local, k)
	if v.saveLocked() != nil {
		return Bundle{}, ErrState
	}
	return cloneBundle(k.Bundle), nil
}

// Public returns only public bundles and status; private material is never
// serializable through the local control interface.
type PublicStatus struct {
	Bundle Bundle `json:"bundle"`
	Status string `json:"status"`
	Hash   string `json:"bundle_hash"`
}

func (v *Vault) Public() ([]PublicStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return nil, ErrClosed
	}
	out := make([]PublicStatus, 0, len(v.state.Local))
	for _, k := range v.state.Local {
		out = append(out, PublicStatus{Bundle: cloneBundle(k.Bundle), Status: k.Status, Hash: k.Bundle.Hash()})
	}
	return out, nil
}

type Scope struct {
	Owner, Endpoint, Role, BundleHash, Purpose, PeerBundleHash string
	AllowRetired, PeerAllowRetired                             bool
}
type Material struct {
	Bundle        Bundle
	Signing       ed25519.PrivateKey
	ContentSecret []byte
}

func (m *Material) clear() {
	clear(m.Signing)
	clear(m.ContentSecret)
	m.Signing = nil
	m.ContentSecret = nil
}

// Use does not grant model/execution authority. The caller must first verify
// its original signed business scope. The key use has its own cancellation
// lifetime; Close/revocation join it before the node can drop its OS lock.
func (v *Vault) Use(parent context.Context, s Scope, now time.Time, fn func(context.Context, *Material) error) error {
	v.mu.Lock()
	if !v.readyLocked() || parent == nil || parent.Err() != nil || fn == nil || len(v.uses) >= MaximumActiveUses || v.next == ^uint64(0) {
		v.mu.Unlock()
		return ErrClosed
	}
	var selected *localKey
	for i := range v.state.Local {
		k := &v.state.Local[i]
		b := k.Bundle
		if b.Hash() == s.BundleHash && b.Owner == s.Owner && b.Endpoint == s.Endpoint && b.Role == s.Role {
			selected = k
			break
		}
	}
	if selected == nil || selected.Status != "ACTIVE" && !(s.AllowRetired && selected.Status == "RETIRED") || selected.Bundle.Current(now) != nil {
		v.mu.Unlock()
		return ErrTrust
	}
	until, _ := stamp(selected.Bundle.Expires)
	if s.PeerBundleHash != "" {
		matched := false
		for _, p := range v.state.Peers {
			if p.Bundle.Hash() != s.PeerBundleHash || p.Status != "ACTIVE" && !(s.PeerAllowRetired && p.Status == "RETIRED") || p.Bundle.Current(now) != nil {
				continue
			}
			expiry, _ := stamp(p.Bundle.Expires)
			if expiry.Before(until) {
				until = expiry
			}
			matched = true
			break
		}
		if !matched {
			v.mu.Unlock()
			return ErrTrust
		}
	}
	m := &Material{Bundle: cloneBundle(selected.Bundle)}
	switch s.Purpose {
	case "SIGN_STREAM":
		m.Signing = ed25519.NewKeyFromSeed(selected.IdentitySeed)
	case "OPEN_CONTENT":
		m.ContentSecret = append([]byte(nil), selected.ContentSecret...)
	case "CIPHER_CUSTODY":
		// Public original-bundle ownership only. Ciphertext retransmission
		// requires no content secret, receipt key or signing-key copy.
	case "SIGN_RECEIPT":
		if selected.Bundle.Role != "BUYER" {
			v.mu.Unlock()
			return ErrTrust
		}
		m.Signing = ed25519.NewKeyFromSeed(selected.ReceiptSeed)
	default:
		v.mu.Unlock()
		return ErrTrust
	}
	ctx, cancel := context.WithDeadline(parent, until)
	v.next++
	id := v.next
	v.uses[id] = keyUse{cancel: cancel, bundle: s.BundleHash, peer: s.PeerBundleHash}
	v.active.Add(1)
	v.mu.Unlock()
	defer func() {
		cancel()
		m.clear()
		v.mu.Lock()
		delete(v.uses, id)
		v.mu.Unlock()
		v.active.Done()
	}()
	if ctx.Err() != nil {
		return ErrClosed
	}
	return fn(ctx, m)
}
func (v *Vault) RevokeLocal(bundleHash string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.readyLocked() {
		return ErrClosed
	}
	for i := range v.state.Local {
		k := &v.state.Local[i]
		if k.Bundle.Hash() != bundleHash {
			continue
		}
		if k.Status == "REVOKED" {
			return nil
		}
		k.Status = "REVOKED"
		k.clear()
		if v.saveLocked() != nil {
			return ErrState
		}
		for _, u := range v.uses {
			if u.bundle == bundleHash {
				u.cancel()
			}
		}
		return nil
	}
	return ErrTrust
}
func (v *Vault) clearLocked() {
	clear(v.wrap)
	v.wrap = nil
	for i := range v.state.Local {
		v.state.Local[i].clear()
	}
}
func (v *Vault) BeginClose() {
	v.mu.Lock()
	v.closing = true
	for _, u := range v.uses {
		u.cancel()
	}
	v.mu.Unlock()
}
func (v *Vault) Close() {
	v.closeOnce.Do(func() {
		v.BeginClose()
		v.active.Wait()
		v.mu.Lock()
		v.clearLocked()
		v.mu.Unlock()
		close(v.done)
	})
	<-v.done
}
