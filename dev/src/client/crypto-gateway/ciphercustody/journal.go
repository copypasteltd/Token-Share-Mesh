package ciphercustody

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"p11.local/compute/client/protocol/cipherstream"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

type entry struct {
	ID         string                 `json:"custody_id"`
	File       string                 `json:"cipher_file"`
	Descriptor Descriptor             `json:"descriptor"`
	Created    string                 `json:"created_at"`
	State      string                 `json:"state"`
	Bytes      string                 `json:"cipher_bytes"`
	Manifest   *cipherstream.Manifest `json:"manifest,omitempty"`
}
type diskState struct {
	Profile string  `json:"profile_id"`
	Binding string  `json:"original_binding_digest"`
	Policy  Policy  `json:"policy"`
	Objects []entry `json:"objects"`
}
type use struct {
	id     string
	cancel context.CancelFunc
}
type Store struct {
	mu                sync.Mutex
	journal           Journal
	files             Files
	key               []byte
	state             diskState
	uses              map[uint64]use
	owners            map[string]*sync.WaitGroup
	next              uint64
	active            sync.WaitGroup
	closing, poisoned bool
	maintenance       bool
	closeOnce         sync.Once
	done              chan struct{}
}

func FileNameValid(name string) bool {
	const prefix = "cipher-object-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".bin") {
		return false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".bin")
	decoded, e := hex.DecodeString(raw)
	return e == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == raw
}
func NewID() (string, error) {
	var raw [16]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return "", ErrState
	}
	return hex.EncodeToString(raw[:]), nil
}
func OpenOwned(j Journal, files Files, key []byte, binding string, create *Policy) (_ *Store, answer error) {
	if j == nil || files == nil || len(key) != 32 || !cipherstream.ValidHash(binding) {
		return nil, ErrState
	}
	s := &Store{journal: j, files: files, key: append([]byte(nil), key...), uses: map[uint64]use{}, owners: map[string]*sync.WaitGroup{}, done: make(chan struct{})}
	defer func() {
		if answer != nil {
			clear(s.key)
		}
	}()
	raw, e := j.Read()
	if create != nil {
		if create.Validate() != nil || !errors.Is(e, os.ErrNotExist) || len(raw) != 0 {
			return nil, ErrState
		}
		names, ne := files.Names()
		if ne != nil || len(names) != 0 {
			return nil, ErrState
		}
		s.state = diskState{Profile: Profile, Binding: binding, Policy: *create, Objects: []entry{}}
		if s.saveLocked() != nil {
			return nil, ErrState
		}
		return s, nil
	}
	if e != nil || len(raw) < 28 || len(raw) > MaximumJournalBytes {
		return nil, ErrState
	}
	s.state.Binding = binding
	g, e := s.aead()
	if e != nil {
		return nil, ErrState
	}
	plain, e := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], s.aad())
	if e != nil {
		return nil, ErrState
	}
	defer clear(plain)
	if strictjson.Decode(plain, &s.state, MaximumJournalBytes-28) != nil || s.state.Binding != binding || s.validateLocked() != nil {
		return nil, ErrState
	}
	// Only original registered ciphertext may be adopted. Unknown files are
	// retained for explicit recovery rather than erased or charged to no quota.
	names, e := files.Names()
	if e != nil {
		return nil, ErrState
	}
	registered := map[string]bool{}
	for _, record := range s.state.Objects {
		registered[record.File] = true
	}
	for _, name := range names {
		if !registered[name] {
			return nil, ErrState
		}
	}
	changed := false
	for i := range s.state.Objects {
		r := &s.state.Objects[i]
		s.owners[r.ID] = &sync.WaitGroup{}
		if r.State == "WRITING" {
			r.State = "INTERRUPTED"
			f, fe := files.Open(r.File)
			if fe == nil {
				info, se := f.Stat()
				ce := f.Close()
				if se != nil || ce != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > r.Descriptor.maximum() {
					return nil, ErrState
				}
				r.Bytes = decimal(info.Size())
			} else if !errors.Is(fe, os.ErrNotExist) {
				return nil, ErrState
			}
			changed = true
		}
	}
	if changed && s.saveLocked() != nil {
		return nil, ErrState
	}
	return s, nil
}
func (s *Store) aad() []byte { return []byte(Profile + "\x00" + s.state.Binding) }
func (s *Store) aead() (cipher.AEAD, error) {
	b, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func (s *Store) validateLocked() error {
	if s.state.Profile != Profile || !cipherstream.ValidHash(s.state.Binding) || s.state.Policy.Validate() != nil || len(s.state.Objects) > s.state.Policy.Objects {
		return ErrState
	}
	idsSeen, filesSeen, objects := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var reserved int64
	for _, r := range s.state.Objects {
		at, e := cipherstream.CanonicalTime(r.Created)
		n, ne := cipherstream.Counter(r.Bytes, true, 63)
		if !ids.Valid(r.ID) || idsSeen[r.ID] || !FileNameValid(r.File) || r.ID != "custody_"+strings.TrimSuffix(strings.TrimPrefix(r.File, "cipher-object-"), ".bin") || filesSeen[r.File] || objects[r.Descriptor.Context.Object] || e != nil || ne != nil || n > uint64(r.Descriptor.maximum()) || r.Descriptor.Validate(at) != nil {
			return ErrState
		}
		idsSeen[r.ID] = true
		filesSeen[r.File] = true
		objects[r.Descriptor.Context.Object] = true
		switch r.State {
		case "WRITING", "INTERRUPTED":
			if r.Manifest != nil {
				return ErrState
			}
		case "READY":
			if r.Manifest == nil || r.Manifest.Hash() == "" || r.Manifest.CipherBytes != r.Bytes || r.Manifest.Expires != r.Descriptor.ManifestExpiry || r.Descriptor.Origin == "INBOUND" && r.Descriptor.InboundManifest != r.Manifest.Hash() {
				return ErrState
			}
		case "RETIRING":
		case "RETIRED":
			if n != 0 {
				return ErrState
			}
		default:
			return ErrState
		}
		if r.State != "RETIRED" {
			reserved += r.Descriptor.maximum()
		}
	}
	cap, _ := cipherstream.Counter(s.state.Policy.Bytes, false, 63)
	if reserved < 0 || uint64(reserved) > cap {
		return ErrState
	}
	return nil
}
func (s *Store) failLocked() {
	s.poisoned = true
	for _, u := range s.uses {
		u.cancel()
	}
}
func (s *Store) saveLocked() error {
	if s.poisoned || s.validateLocked() != nil {
		s.failLocked()
		return ErrState
	}
	raw, e := json.Marshal(s.state)
	defer clear(raw)
	if e != nil || len(raw) > MaximumJournalBytes-28 {
		s.failLocked()
		return ErrState
	}
	g, e := s.aead()
	if e != nil {
		s.failLocked()
		return ErrState
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		s.failLocked()
		return ErrState
	}
	sealed := g.Seal(nonce, nonce, raw, s.aad())
	if s.journal.Commit(sealed) != nil {
		s.failLocked()
		return ErrState
	}
	return nil
}
func (s *Store) readyLocked() bool { return !s.closing && !s.poisoned }

const maintenanceID = "custody_maintenance"

func (s *Store) payloadUsesLocked() int {
	n := 0
	for _, u := range s.uses {
		if u.id != maintenanceID {
			n++
		}
	}
	return n
}
func (s *Store) reservedLocked() int64 {
	var total int64
	for _, r := range s.state.Objects {
		if r.State != "RETIRED" {
			total += r.Descriptor.maximum()
		}
	}
	return total
}
func (s *Store) beginUseLocked(parent context.Context, id string, deadline time.Time) (context.Context, func(), error) {
	if !s.readyLocked() || parent == nil || parent.Err() != nil || s.next == ^uint64(0) {
		return nil, nil, ErrClosed
	}
	if id != maintenanceID && s.payloadUsesLocked() >= MaximumActiveUses {
		return nil, nil, ErrCapacity
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	if ctx.Err() != nil {
		cancel()
		return nil, nil, ErrClosed
	}
	wg := s.owners[id]
	if wg == nil {
		wg = &sync.WaitGroup{}
		s.owners[id] = wg
	}
	s.next++
	seq := s.next
	s.uses[seq] = use{id, cancel}
	s.active.Add(1)
	wg.Add(1)
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.uses, seq)
		s.mu.Unlock()
		wg.Done()
		s.active.Done()
	}, nil
}
func (s *Store) BeginClose() {
	s.mu.Lock()
	s.closing = true
	for _, u := range s.uses {
		u.cancel()
	}
	s.mu.Unlock()
}
func (s *Store) Close() {
	s.closeOnce.Do(func() {
		s.BeginClose()
		s.active.Wait()
		s.mu.Lock()
		clear(s.key)
		s.key = nil
		s.mu.Unlock()
		close(s.done)
	})
	<-s.done
}
