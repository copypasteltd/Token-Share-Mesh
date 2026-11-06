package node

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"os"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"time"
)

const endpointKeyFile = "endpoint-keys.enc"

type endpointJournal struct{ node *Store }

func (j endpointJournal) Read() ([]byte, error) {
	s := j.node
	if s.root == nil || s.poisoned || s.checkFile(endpointKeyFile, true) != nil {
		return nil, endpoint.ErrState
	}
	if _, e := s.root.Lstat(endpointKeyFile); errors.Is(e, os.ErrNotExist) {
		return nil, os.ErrNotExist
	} else if e != nil {
		return nil, endpoint.ErrState
	}
	return s.read(endpointKeyFile, endpoint.MaximumJournalBytes)
}
func (j endpointJournal) Commit(sealed []byte) error {
	s := j.node
	if s.root == nil || s.poisoned || len(sealed) < 28 || len(sealed) > endpoint.MaximumJournalBytes {
		return endpoint.ErrState
	}
	id, e := randomHex(16)
	if e != nil {
		return endpoint.ErrState
	}
	name := "endpoint-next-" + id
	if s.writeNew(name, sealed) != nil {
		return endpoint.ErrState
	}
	defer s.root.Remove(name)
	if s.checkFile(endpointKeyFile, true) != nil || replaceFile(s.path, s.root, name, endpointKeyFile) != nil || syncDirectory(s.path) != nil {
		return endpoint.ErrState
	}
	return nil
}
func (s *Store) endpointBinding() string {
	// Pairing/seller state may change independently. The content key root is
	// bound to the original installation/anchor; each bundle binds its owner,
	// role and endpoint independently and is never rewritten on re-pairing.
	return hostDigest([]byte(endpoint.Profile + "\x00" + s.s.Installation + "\x00" + s.s.CryptoID))
}
func (s *Store) canOpenEndpointKeys() bool { return s.root != nil && !s.poisoned && !s.closingNative }
func (s *Store) InitEndpointKeys() (*endpoint.Vault, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenEndpointKeys() || s.s.CryptoID != "" || s.cryptoKeys != nil || s.s.Pending != nil || s.host != nil || len(s.nativeRentals)+len(s.comfyRentals) != 0 {
		return nil, endpoint.ErrState
	}
	if _, e := (endpointJournal{s}).Read(); !errors.Is(e, os.ErrNotExist) {
		return nil, endpoint.ErrState
	}
	id, e := randomHex(32)
	if e != nil {
		return nil, endpoint.ErrState
	}
	s.s.CryptoID = id
	// Commit the original anchor first. If creation is interrupted, missing
	// state fails closed; it cannot silently mint replacement endpoint keys.
	if s.save() != nil {
		return nil, endpoint.ErrState
	}
	return s.openEndpointKeysLocked(true)
}
func (s *Store) OpenEndpointKeys() (*endpoint.Vault, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenEndpointKeys() || s.s.CryptoID == "" {
		return nil, endpoint.ErrState
	}
	if s.cryptoKeys != nil {
		return s.cryptoKeys, nil
	}
	return s.openEndpointKeysLocked(false)
}
func (s *Store) openEndpointKeysLocked(create bool) (*endpoint.Vault, error) {
	binding := s.endpointBinding()
	mac := hmac.New(sha256.New, s.master)
	_, _ = mac.Write([]byte("P11_ENDPOINT_WRAP_KEY_1\x00" + binding))
	key := mac.Sum(nil)
	defer clear(key)
	v, e := endpoint.OpenOwned(endpointJournal{s}, key, binding, create)
	if e != nil {
		return nil, e
	}
	s.cryptoKeys = v
	return v, nil
}

// Metadata management is performed before starting the mutable node control
// loop. A seller identity cannot be claimed for another registered seller/device.
func (s *Store) CreateLocalEndpoint(owner, recipient, role string, now, expires time.Time) (endpoint.Bundle, error) {
	if role == "SELLER" && (s.s.SellerID == "" || s.s.SellerID != owner || s.s.DeviceID != recipient || s.s.RemoteState == "REVOKED") {
		return endpoint.Bundle{}, endpoint.ErrTrust
	}
	v, e := s.OpenEndpointKeys()
	if e != nil {
		return endpoint.Bundle{}, e
	}
	return v.CreateLocal(owner, recipient, role, now, expires)
}
