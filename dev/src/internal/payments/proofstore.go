package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ProofStore holds ONLY Vault ciphertext beneath an explicitly supplied project
// runtime root. It owns native filesystem operations; it does not own the Vault
// keyring, create a payment, grant recovery jobs or change financial state.
// Capacity is tracked by this instance. The trusted enclosing application must
// establish exclusive process ownership before opening it; independent stores
// or out-of-band writers are not coordinated by this package.
type ProofStore struct {
	mu         sync.Mutex
	root       *os.Root
	vault      *Vault
	known      map[string]bool
	operations sync.WaitGroup
	closed     bool
	closeErr   error
	done       chan struct{}
}

func (s *ProofStore) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[payment proof store redacted]")
}
func (s *ProofStore) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func OpenProofStore(runtimeRoot string, vault *Vault) (*ProofStore, error) {
	if runtimeRoot == "" || vault == nil {
		return nil, ErrProof
	}
	parent, e := os.OpenRoot(runtimeRoot)
	if e != nil {
		return nil, ErrProof
	}
	defer parent.Close()
	if parent.MkdirAll("payments/proofs", 0700) != nil {
		return nil, ErrProof
	}
	root, e := parent.OpenRoot("payments/proofs")
	if e != nil {
		return nil, ErrProof
	}
	f, e := root.Open(".")
	if e != nil {
		root.Close()
		return nil, ErrProof
	}
	entries, e := f.Readdirnames(4097)
	f.Close()
	if e != nil && e != io.EOF || len(entries) > 4096 {
		root.Close()
		return nil, ErrProof
	}
	known := map[string]bool{}
	for _, name := range entries {
		known[name] = true
	}
	return &ProofStore{root: root, vault: vault, known: known, done: make(chan struct{})}, nil
}
func (s *ProofStore) enter(ctx context.Context) bool {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.operations.Add(1)
	return true
}
func proofFilePrefix(scope Scope) string { return "ppr_" + scope.Digest() + "_" }
func proofFileMatches(scope Scope, ref string) bool {
	prefix := proofFilePrefix(scope)
	if !scope.Valid() || len(ref) != len(prefix)+32 || !strings.HasPrefix(ref, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(ref, prefix)
	decoded, e := hex.DecodeString(suffix)
	return e == nil && hex.EncodeToString(decoded) == suffix
}

// Put returns a nonempty ref even on an uncertain *file* write so the original
// owner can retain and retire it. Caller must not announce proof persistence or
// request a new payment when err != nil. Database registration is still separate.
func (s *ProofStore) Put(ctx context.Context, scope Scope, proof *Proof, expires, now time.Time) (string, error) {
	if !s.enter(ctx) {
		return "", ErrProof
	}
	defer s.operations.Done()
	encrypted, e := s.vault.Seal(scope, proof, expires, now)
	if e != nil {
		return "", e
	}
	defer clear(encrypted)
	if len(encrypted) > 8192 {
		return "", ErrProof
	}
	random := make([]byte, 16)
	defer clear(random)
	if _, e = rand.Read(random); e != nil {
		return "", ErrProof
	}
	ref := proofFilePrefix(scope) + hex.EncodeToString(random)
	s.mu.Lock()
	if len(s.known) >= 4096 || s.known[ref] {
		s.mu.Unlock()
		return "", ErrProof
	}
	s.known[ref] = true
	s.mu.Unlock()
	created := false
	defer func() {
		if !created {
			s.mu.Lock()
			delete(s.known, ref)
			s.mu.Unlock()
		}
	}()
	if ctx.Err() != nil {
		return "", ErrProof
	}
	f, e := s.root.OpenFile(ref, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		if os.IsExist(e) {
			created = true
			return ref, ErrUnknown
		}
		return "", ErrProof
	}
	created = true
	n, writeErr := f.Write(encrypted)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(encrypted) || ctx.Err() != nil {
		return ref, ErrUnknown
	}
	return ref, nil
}
func (s *ProofStore) Get(ctx context.Context, scope Scope, ref string, now time.Time) (*Proof, error) {
	if !proofFileMatches(scope, ref) || !s.enter(ctx) {
		return nil, ErrProof
	}
	defer s.operations.Done()
	f, e := s.root.Open(ref)
	if e != nil {
		return nil, ErrProof
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 8192 {
		return nil, ErrProof
	}
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	defer clear(raw)
	if e != nil || len(raw) > 8192 || ctx.Err() != nil {
		return nil, ErrProof
	}
	return s.vault.Open(scope, raw, now)
}

// Retire removes only an original scope's exact encrypted artifact. The trusted
// caller must first persist its tombstone/cleanup intent. This is not financial
// release, payment closure, proof of refund or an authorization to erase history.
func (s *ProofStore) Retire(ctx context.Context, scope Scope, ref string) error {
	if !proofFileMatches(scope, ref) || !s.enter(ctx) {
		return ErrProof
	}
	defer s.operations.Done()
	e := s.root.Remove(ref)
	if e != nil && !os.IsNotExist(e) {
		return ErrUnknown
	}
	s.mu.Lock()
	delete(s.known, ref)
	s.mu.Unlock()
	return nil
}
func (s *ProofStore) Close(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrProof
	}
	s.mu.Lock()
	first := !s.closed
	if first {
		s.closed = true
	}
	s.mu.Unlock()
	if first {
		go func() {
			s.operations.Wait()
			e := s.root.Close()
			s.mu.Lock()
			if e != nil {
				s.closeErr = ErrUnknown
			}
			s.mu.Unlock()
			close(s.done)
		}()
	}
	select {
	case <-ctx.Done():
		return ErrUnknown
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.closeErr
	}
}
