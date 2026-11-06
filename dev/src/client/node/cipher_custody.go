package node

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	cryptogateway "p11.local/compute/client/crypto-gateway"
	"p11.local/compute/client/crypto-gateway/ciphercustody"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/protocol/cipherstream"
)

const cipherCustodyFile = "cipher-custody.enc"

type cipherJournal struct{ node *Store }

func (j cipherJournal) Read() ([]byte, error) {
	s := j.node
	if s.root == nil {
		return nil, ciphercustody.ErrState
	}
	if _, e := s.root.Lstat(cipherCustodyFile); errors.Is(e, os.ErrNotExist) {
		return nil, os.ErrNotExist
	} else if e != nil {
		return nil, ciphercustody.ErrState
	}
	if s.checkFile(cipherCustodyFile, false) != nil {
		return nil, ciphercustody.ErrState
	}
	return s.read(cipherCustodyFile, ciphercustody.MaximumJournalBytes)
}
func (j cipherJournal) Commit(raw []byte) error {
	s := j.node
	if s.root == nil || len(raw) < 28 || len(raw) > ciphercustody.MaximumJournalBytes {
		return ciphercustody.ErrState
	}
	id, e := randomHex(16)
	if e != nil {
		return ciphercustody.ErrState
	}
	name := "cipher-next-" + id
	defer s.root.Remove(name)
	if s.writeNew(name, raw) != nil {
		return ciphercustody.ErrState
	}
	if s.checkFile(cipherCustodyFile, true) != nil || replaceFile(s.path, s.root, name, cipherCustodyFile) != nil || syncDirectory(s.path) != nil {
		return ciphercustody.ErrState
	}
	return nil
}

type cipherFiles struct{ node *Store }

func (b cipherFiles) Create(name string) (ciphercustody.File, error) {
	s := b.node
	if !ciphercustody.FileNameValid(name) || s.root == nil {
		return nil, ciphercustody.ErrState
	}
	f, e := createPrivateFile(s, name)
	if e != nil {
		return nil, ciphercustody.ErrState
	}
	if s.checkFile(name, false) != nil || syncDirectory(s.path) != nil {
		f.Close()
		return nil, ciphercustody.ErrState
	}
	return f, nil
}
func (b cipherFiles) Open(name string) (ciphercustody.File, error) {
	s := b.node
	if !ciphercustody.FileNameValid(name) || s.root == nil {
		return nil, ciphercustody.ErrState
	}
	before, e := s.root.Lstat(name)
	if errors.Is(e, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if e != nil || s.checkFile(name, false) != nil {
		return nil, ciphercustody.ErrState
	}
	f, e := s.root.Open(name)
	if e != nil {
		return nil, ciphercustody.ErrState
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		f.Close()
		return nil, ciphercustody.ErrState
	}
	return f, nil
}
func (b cipherFiles) Remove(name string) error {
	s := b.node
	if !ciphercustody.FileNameValid(name) || s.root == nil {
		return ciphercustody.ErrState
	}
	if _, e := s.root.Lstat(name); errors.Is(e, os.ErrNotExist) {
		return os.ErrNotExist
	} else if e != nil {
		return ciphercustody.ErrState
	}
	if s.checkFile(name, false) != nil || s.root.Remove(name) != nil || syncDirectory(s.path) != nil {
		return ciphercustody.ErrState
	}
	return nil
}
func (b cipherFiles) Names() ([]string, error) {
	s := b.node
	if s.root == nil {
		return nil, ciphercustody.ErrState
	}
	directory, e := s.root.Open(".")
	if e != nil {
		return nil, ciphercustody.ErrState
	}
	defer directory.Close()
	names := []string{}
	scanned := 0
	for {
		batch, re := directory.ReadDir(64)
		scanned += len(batch)
		if scanned > 8192 {
			return nil, ciphercustody.ErrState
		}
		for _, item := range batch {
			// A leftover encrypted replacement has an unknown commit outcome.
			// Retain it and require explicit recovery rather than silently
			// adopting it, deleting it or excluding it from disk ownership.
			if strings.HasPrefix(item.Name(), "cipher-next-") {
				return nil, ciphercustody.ErrState
			}
			if !strings.HasPrefix(item.Name(), "cipher-object-") {
				continue
			}
			if !ciphercustody.FileNameValid(item.Name()) || item.IsDir() || s.checkFile(item.Name(), false) != nil {
				return nil, ciphercustody.ErrState
			}
			names = append(names, item.Name())
			if len(names) > ciphercustody.MaximumObjects {
				return nil, ciphercustody.ErrState
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return nil, ciphercustody.ErrState
		}
	}
	return names, nil
}
func (s *Store) cipherBinding() string {
	return hostDigest([]byte(ciphercustody.Profile + "\x00" + s.s.Installation + "\x00" + s.s.CryptoID + "\x00" + s.s.CipherID))
}
func (s *Store) InitCipherCustody(policy ciphercustody.Policy) (*ciphercustody.Store, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenEndpointKeys() || s.s.CryptoID == "" || s.s.CipherID != "" || s.cipherCustody != nil || s.s.Pending != nil || s.host != nil || len(s.nativeRentals)+len(s.comfyRentals) != 0 || policy.Validate() != nil {
		return nil, ciphercustody.ErrState
	}
	if s.cryptoKeys == nil {
		if _, e := s.openEndpointKeysLocked(false); e != nil {
			return nil, e
		}
	}
	if _, e := (cipherJournal{s}).Read(); !errors.Is(e, os.ErrNotExist) {
		return nil, ciphercustody.ErrState
	}
	names, e := (cipherFiles{s}).Names()
	if e != nil || len(names) != 0 {
		return nil, ciphercustody.ErrState
	}
	id, e := randomHex(32)
	if e != nil {
		return nil, ciphercustody.ErrState
	}
	s.s.CipherID = id
	if s.save() != nil {
		return nil, ciphercustody.ErrState
	}
	return s.openCipherCustodyLocked(&policy)
}
func (s *Store) OpenCipherCustody() (*ciphercustody.Store, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenEndpointKeys() || s.s.CryptoID == "" || s.s.CipherID == "" {
		return nil, ciphercustody.ErrState
	}
	if s.cipherCustody != nil {
		return s.cipherCustody, nil
	}
	return s.openCipherCustodyLocked(nil)
}
func (s *Store) openCipherCustodyLocked(create *ciphercustody.Policy) (*ciphercustody.Store, error) {
	binding := s.cipherBinding()
	mac := hmac.New(sha256.New, s.master)
	_, _ = mac.Write([]byte("P11_CIPHER_CUSTODY_WRAP_KEY_1\x00" + binding))
	key := mac.Sum(nil)
	defer clear(key)
	store, e := ciphercustody.OpenOwned(cipherJournal{s}, cipherFiles{s}, key, binding, create)
	if e != nil {
		return nil, e
	}
	s.cipherCustody = store
	return store, nil
}
func (s *Store) cipherOwners() (*endpoint.Vault, *ciphercustody.Store, error) {
	keys, e := s.OpenEndpointKeys()
	if e != nil {
		return nil, nil, e
	}
	store, e := s.OpenCipherCustody()
	if e != nil {
		return nil, nil, e
	}
	return keys, store, nil
}
func (s *Store) SealCipher(ctx context.Context, b cryptogateway.Binding, headers cipherstream.PrivateHeaders, source io.Reader, outcome string, expires, now time.Time) (ciphercustody.Result, error) {
	keys, store, e := s.cipherOwners()
	if e != nil {
		return ciphercustody.Result{}, e
	}
	return cryptogateway.SealStored(ctx, keys, store, b, headers, source, outcome, expires, now)
}
func (s *Store) ImportCipher(ctx context.Context, b cryptogateway.Binding, source io.Reader, m cipherstream.Manifest, now time.Time) (ciphercustody.Result, error) {
	keys, store, e := s.cipherOwners()
	if e != nil {
		return ciphercustody.Result{}, e
	}
	return cryptogateway.ImportStored(ctx, keys, store, b, source, m, now)
}
func (s *Store) ConsumeCipher(ctx context.Context, b cryptogateway.Binding, h ciphercustody.Handle, m cipherstream.Manifest, now time.Time, consume func(context.Context, cipherstream.Record) error) error {
	keys, store, e := s.cipherOwners()
	if e != nil {
		return e
	}
	return cryptogateway.ConsumeStored(ctx, keys, store, b, h, m, now, consume)
}
func (s *Store) CopyCipherRange(ctx context.Context, b cryptogateway.Binding, origin string, h ciphercustody.Handle, m cipherstream.Manifest, offset, length int64, destination io.Writer, now time.Time) (int64, error) {
	keys, store, e := s.cipherOwners()
	if e != nil {
		return 0, e
	}
	return cryptogateway.CopyStoredRange(ctx, keys, store, b, origin, h, m, offset, length, destination, now)
}
