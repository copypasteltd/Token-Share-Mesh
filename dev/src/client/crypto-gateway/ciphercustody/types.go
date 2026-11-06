// Package ciphercustody retains immutable original BINARY_STREAM_1 bytes under
// the enclosing native node's OS root and single-writer lock. It contains no
// plaintext, content key, model invocation, acceptance or payment authority.
package ciphercustody

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"p11.local/compute/client/protocol/cipherstream"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/ids"
)

const Profile = "P11_ORIGINAL_CIPHER_CUSTODY_1"
const MaximumJournalBytes = 2 << 20
const MaximumObjects = 64
const MaximumActiveUses = 2
const CopyBufferBytes = 65536

var ErrState = errors.New("original cipher custody unavailable; retain original state")
var ErrScope = errors.New("original cipher custody scope rejected")
var ErrCapacity = errors.New("original cipher custody budget unavailable")
var ErrPending = errors.New("original cipher creation is still owned")
var ErrInterrupted = errors.New("original cipher creation interrupted; do not recreate or reexecute")
var ErrClosed = errors.New("original cipher custody closed")

// Journal and Files are supplied by the already OS-locked native directory.
// No second lock, arbitrary external path or replacement secret root is opened.
type Journal interface {
	Read() ([]byte, error)
	Commit([]byte) error
}
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	Stat() (os.FileInfo, error)
	Sync() error
	Close() error
}
type Files interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	Remove(name string) error
	Names() ([]string, error)
}

type Policy struct {
	Profile string `json:"profile_id"`
	Bytes   string `json:"capacity_bytes"`
	Objects int    `json:"maximum_objects"`
}

func (p Policy) Validate() error {
	n, e := cipherstream.Counter(p.Bytes, false, 63)
	if p.Profile != Profile || e != nil || n < uint64(cipherstream.MaximumObjectBytes) || n > 512<<20 || p.Objects < 1 || p.Objects > MaximumObjects {
		return ErrScope
	}
	return nil
}

// Descriptor is independently derived from the original confirmed contract and
// public endpoint bundles. It never takes authority from a received header.
// OUTBOUND owns one original encryption attempt; INBOUND retains received bytes
// and records their already known original manifest hash.
type Descriptor struct {
	Profile         string               `json:"profile_id"`
	Origin          string               `json:"origin"`
	Context         cipherstream.Context `json:"original_context"`
	SenderPublic    string               `json:"sender_public_key"`
	RecipientPublic string               `json:"recipient_public_key"`
	LocalBundle     string               `json:"original_local_bundle_hash"`
	PeerBundle      string               `json:"original_peer_bundle_hash"`
	PublicModel     string               `json:"original_public_model"`
	DataBytes       string               `json:"data_limit_bytes"`
	WireBytes       string               `json:"wire_limit_bytes"`
	Records         string               `json:"record_limit"`
	ManifestExpiry  string               `json:"original_manifest_expires_at"`
	InboundManifest string               `json:"inbound_manifest_hash,omitempty"`
}

func publicBytes(raw string) ([]byte, error) {
	p, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	if e != nil || len(p) != 32 || base64.RawURLEncoding.EncodeToString(p) != raw {
		return nil, ErrScope
	}
	return p, nil
}
func (d Descriptor) Expected() (cipherstream.Expected, error) {
	sp, e := publicBytes(d.SenderPublic)
	rp, re := publicBytes(d.RecipientPublic)
	data, de := cipherstream.Counter(d.DataBytes, true, 63)
	wire, we := cipherstream.Counter(d.WireBytes, false, 63)
	records, ce := cipherstream.Counter(d.Records, false, 64)
	if e != nil || re != nil || de != nil || we != nil || ce != nil || d.Context.StreamHeader != "" {
		return cipherstream.Expected{}, ErrScope
	}
	l := cipherstream.Limits{DataBytes: int64(data), WireBytes: int64(wire), Records: records}
	if l.Validate(d.Context.Purpose) != nil || cipherstream.Hash(rp) != d.Context.ReceiverFingerprint {
		return cipherstream.Expected{}, ErrScope
	}
	return cipherstream.Expected{Context: d.Context, SenderPublic: ed25519.PublicKey(sp), RecipientPublic: rp, Limits: l}, nil
}
func (d Descriptor) Validate(now time.Time) error {
	if d.Profile != Profile || !cipherstream.ValidHash(d.LocalBundle) || !cipherstream.ValidHash(d.PeerBundle) || d.LocalBundle == d.PeerBundle || len(d.PublicModel) < 1 || len(d.PublicModel) > 256 || !utf8.ValidString(d.PublicModel) || strings.IndexFunc(d.PublicModel, unicode.IsControl) >= 0 {
		return ErrScope
	}
	if d.Origin == "OUTBOUND" {
		if d.InboundManifest != "" {
			return ErrScope
		}
	} else if d.Origin != "INBOUND" || !cipherstream.ValidHash(d.InboundManifest) {
		return ErrScope
	}
	expected, e := d.Expected()
	if e != nil {
		return e
	}
	c := expected.Context
	// The random wire header is supplied only when sealing. A fixed-size dummy
	// validates the original pre-header context without minting a real stream.
	c.StreamHeader = base64.RawURLEncoding.EncodeToString(make([]byte, 24))
	expiry, ee := cipherstream.CanonicalTime(d.ManifestExpiry)
	deadline, de := cipherstream.CanonicalTime(c.Deadline)
	if c.Validate(now) != nil || ee != nil || de != nil || !now.Before(expiry) || expiry.After(deadline) {
		return ErrScope
	}
	return nil
}
func (d Descriptor) Hash() string {
	raw, e := canonicaljson.Encode(d, 16384)
	if e != nil {
		return ""
	}
	return canonicaljson.Digest("P11/E2E/CIPHER_CUSTODY/DESCRIPTOR\x00", raw)
}
func (d Descriptor) maximum() int64 {
	n, _ := cipherstream.Counter(d.WireBytes, false, 63)
	return int64(n)
}

type Handle struct {
	ID             string `json:"custody_id"`
	DescriptorHash string `json:"descriptor_hash"`
	ManifestHash   string `json:"manifest_hash"`
}

func (h Handle) Validate() bool {
	return ids.Valid(h.ID) && cipherstream.ValidHash(h.DescriptorHash) && cipherstream.ValidHash(h.ManifestHash)
}

type Result struct {
	Handle   Handle                `json:"handle"`
	Manifest cipherstream.Manifest `json:"manifest"`
	Bytes    string                `json:"cipher_bytes"`
	Replayed bool                  `json:"replayed"`
}
type PublicState struct {
	ID             string `json:"custody_id"`
	DescriptorHash string `json:"descriptor_hash"`
	ManifestHash   string `json:"manifest_hash,omitempty"`
	State          string `json:"state"`
	Bytes          string `json:"cipher_bytes"`
	Expires        string `json:"expires_at"`
}
type Snapshot struct {
	Profile  string        `json:"profile_id"`
	Policy   Policy        `json:"policy"`
	Reserved string        `json:"reserved_bytes"`
	Objects  []PublicState `json:"objects"`
}

func decimal(n int64) string { return strconv.FormatInt(n, 10) }
