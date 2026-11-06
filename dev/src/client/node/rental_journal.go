package node

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const RentalJournalProfile = "P11_NATIVE_RENTAL_JOURNAL_1"
const rentalJournalFile = "rental-control.enc"
const rentalJournalLimit = 262144
const MaximumRentalAnchors = 64

var ErrRentalJournal = errors.New("original encrypted rental control state unavailable; retain state and stop owned work")

type rentalEntry struct {
	Expected rentalaccess.Expected `json:"original_expected"`
	KeyID    string                `json:"original_issuer_key_id"`
	Version  int64                 `json:"grant_session_version"`
	Anchor   json.RawMessage       `json:"anchor"`
}
type rentalJournalState struct {
	Profile string        `json:"profile_id"`
	Binding string        `json:"binding_digest"`
	Entries []rentalEntry `json:"anchors"`
}

// RentalJournal shares the original node's OS process lock and private root;
// it creates no GPU/resource lock. Its separate AEAD key is derived from the
// OS-protected node master and an explicitly initialized random journal ID.
// The parent Store must outlive it. No Bearer, prompt or result is persisted.
type RentalJournal struct {
	mu        sync.Mutex
	node      *Store
	key       []byte
	s         rentalJournalState
	active    map[string]bool
	controls  map[*RentalControl]bool
	closeOnce sync.Once
	closing   bool
	closed    bool
	poisoned  bool
}

func (s *Store) rentalBinding() string {
	return hostDigest([]byte(RentalJournalProfile + "\n" + s.s.Installation + "\n" + s.s.RentalID + "\n" + s.s.DeviceID + "\n" + s.s.SellerID + "\n" + s.s.Config.Base + "\n"))
}

// InitRentalJournal is an explicit metadata-only initialization before other
// node loops/Host start. Missing/corrupt state after initialization is never
// silently recreated. An interrupted create remains closed for manual recovery.
func (s *Store) InitRentalJournal() (*RentalJournal, error) {
	if s.root == nil || s.poisoned || s.rentals != nil || s.s.RentalID != "" || s.s.DeviceID == "" || s.s.Config.Kind != "GPU_DEVICE" || s.s.RemoteState == "REVOKED" || s.s.Pending != nil || s.host != nil {
		return nil, ErrRentalJournal
	}
	if s.checkFile(rentalJournalFile, true) != nil {
		return nil, ErrRentalJournal
	}
	if _, err := s.root.Lstat(rentalJournalFile); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRentalJournal
	}
	id, err := randomHex(32)
	if err != nil {
		return nil, ErrRentalJournal
	}
	s.s.RentalID = id
	if s.save() != nil {
		return nil, ErrRentalJournal
	}
	j := s.newRentalJournal()
	if j.persistLocked() != nil {
		j.Close()
		return nil, ErrRentalJournal
	}
	s.rentals = j
	return j, nil
}

func (s *Store) newRentalJournal() *RentalJournal {
	binding := s.rentalBinding()
	mac := hmac.New(sha256.New, s.master)
	mac.Write([]byte("P11_NATIVE_RENTAL_METADATA_KEY_1\n" + binding))
	return &RentalJournal{node: s, key: mac.Sum(nil), s: rentalJournalState{Profile: RentalJournalProfile, Binding: binding, Entries: []rentalEntry{}}, active: map[string]bool{}, controls: map[*RentalControl]bool{}}
}

// OpenRentalJournal decrypts exact original metadata. It never restores an
// ALLOW deadline; OpenControl requires a newly verified Bearer/current context,
// and Window.Restore requires a higher durable sequence before live control.
func (s *Store) OpenRentalJournal() (*RentalJournal, error) {
	if s.rentals != nil {
		return s.rentals, nil
	}
	if s.root == nil || s.poisoned || s.s.RentalID == "" || s.s.RemoteState == "REVOKED" {
		return nil, ErrRentalJournal
	}
	j := s.newRentalJournal()
	sealed, err := s.read(rentalJournalFile, rentalJournalLimit)
	if err != nil {
		j.Close()
		return nil, ErrRentalJournal
	}
	aead, err := makeCipher(j.key)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		j.Close()
		return nil, ErrRentalJournal
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(RentalJournalProfile+"\n"+j.s.Binding))
	defer clear(plain)
	if err != nil || strictjson.Decode(plain, &j.s, rentalJournalLimit) != nil || j.s.Profile != RentalJournalProfile || j.s.Binding != s.rentalBinding() || j.s.Entries == nil || len(j.s.Entries) > MaximumRentalAnchors {
		j.Close()
		return nil, ErrRentalJournal
	}
	seen := map[string]bool{}
	for _, e := range j.s.Entries {
		_, anchorErr := rentalaccess.DecodeStoredAnchor(e.Anchor, e.Expected)
		if seen[e.Expected.Grant] || anchorErr != nil || !ids.Valid(e.KeyID) || len(e.KeyID) > 128 || e.Version < 1 || e.Expected.Device != s.s.DeviceID || e.Expected.Seller != s.s.SellerID {
			j.Close()
			return nil, ErrRentalJournal
		}
		seen[e.Expected.Grant] = true
	}
	s.rentals = j
	return j, nil
}

func (j *RentalJournal) persistLocked() error {
	if j.closed || j.poisoned || j.node.root == nil || len(j.s.Entries) > MaximumRentalAnchors {
		return ErrRentalJournal
	}
	plain, err := json.Marshal(j.s)
	if err != nil || len(plain) > rentalJournalLimit-64 {
		return ErrRentalJournal
	}
	defer clear(plain)
	aead, err := makeCipher(j.key)
	if err != nil {
		return ErrRentalJournal
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return ErrRentalJournal
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(RentalJournalProfile+"\n"+j.s.Binding))
	name, err := randomHex(16)
	if err != nil {
		return ErrRentalJournal
	}
	name = "rental-next-" + name
	if err = j.node.writeNew(name, sealed); err != nil {
		j.poisoned = true
		return ErrRentalJournal
	}
	defer j.node.root.Remove(name)
	if j.node.checkFile(rentalJournalFile, true) != nil || replaceFile(j.node.path, j.node.root, name, rentalJournalFile) != nil || syncDirectory(j.node.path) != nil {
		j.poisoned = true
		return ErrRentalJournal
	}
	return nil
}

func (j *RentalJournal) Close() {
	if j == nil {
		return
	}
	j.closeOnce.Do(func() {
		j.mu.Lock()
		j.closing = true
		controls := make([]*RentalControl, 0, len(j.controls))
		for control := range j.controls {
			controls = append(controls, control)
		}
		j.mu.Unlock()
		// Do not hold journal.mu while a window flushes its permanent STOP. The
		// original root/process lock remains held until every child key is closed.
		for _, control := range controls {
			control.Close()
		}
		j.mu.Lock()
		j.closed = true
		clear(j.key)
		j.key = nil
		j.mu.Unlock()
	})
}

// openWindow enforces one live Window per original grant in this process. A
// second wrapper cannot race a previously accepted sequence. Closed windows
// remain stopped; original high-water/tombstones are retained at bounded size.
func (j *RentalJournal) openWindow(verifier *rentalaccess.Verifier, credential string, expected rentalaccess.Expected, version int64, now time.Time) (*rentalaccess.Window, rentalaccess.Verified, error) {
	if j == nil || verifier == nil {
		return nil, rentalaccess.Verified{}, ErrRentalJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	grant, err := verifier.Verify(credential, expected, now)
	if err != nil || j.closed || j.closing || j.poisoned || j.active[expected.Grant] || version < 1 || expected.Device != j.node.s.s.DeviceID || expected.Seller != j.node.s.s.SellerID {
		return nil, rentalaccess.Verified{}, ErrRentalJournal
	}
	index := -1
	for i, e := range j.s.Entries {
		if e.Expected.Grant == expected.Grant {
			if e.Expected != expected || e.Version != version || e.KeyID != grant.KeyID() {
				return nil, rentalaccess.Verified{}, ErrRentalJournal
			}
			index = i
		}
	}
	create := index == -1
	if create {
		if len(j.s.Entries) == MaximumRentalAnchors {
			return nil, rentalaccess.Verified{}, ErrRentalJournal
		}
		index = len(j.s.Entries)
		j.s.Entries = append(j.s.Entries, rentalEntry{Expected: expected, KeyID: grant.KeyID(), Version: version})
	}
	// NewWindow performs the initial save synchronously. Initialization owns
	// j.mu, so use a distinct initial writer before replacing it with the normal
	// mutex-protected writer (all calls after construction are concurrent-safe).
	initializing := true
	save := func(a rentalaccess.Anchor) error {
		if !initializing {
			j.mu.Lock()
			defer j.mu.Unlock()
		}
		raw, err := json.Marshal(a)
		if err != nil {
			j.poisoned = true
			return ErrRentalJournal
		}
		j.s.Entries[index].Anchor = raw
		if err := j.persistLocked(); err != nil {
			j.poisoned = true
			return ErrRentalJournal
		}
		return nil
	}
	var window *rentalaccess.Window
	if create {
		window, err = rentalaccess.NewWindow(grant, expected, now, save)
	} else {
		anchor, e := rentalaccess.DecodeAnchor(j.s.Entries[index].Anchor, grant, expected)
		if e != nil {
			err = e
		} else {
			window, err = rentalaccess.RestoreWindow(grant, expected, anchor, now, save)
		}
	}
	initializing = false
	if err != nil {
		j.poisoned = true
		return nil, rentalaccess.Verified{}, ErrRentalJournal
	}
	j.active[expected.Grant] = true
	return window, grant, nil
}
