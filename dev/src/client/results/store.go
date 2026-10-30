// Package results transfers original native text into a separate encrypted,
// quota-bounded local result scope. It creates no HTTP listener, execution
// authority, trusted usage or buyer-acceptance/payment evidence.
package results

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
)

const Profile = "P11_LOCAL_RESULT_1"
const maxPiece = 65536
const maxRecords = 131072
const headerBytes = 48
const frameBytes = 9
const (
	kOptions byte = 1
	kStarted byte = 2
	kText    byte = 3
	kFinal   byte = 4
)

var (
	ErrConfig      = errors.New("local result configuration rejected")
	ErrBinding     = errors.New("local result original scope rejected")
	ErrCapacity    = errors.New("local result capacity unavailable")
	ErrUnavailable = errors.New("original local result unavailable")
	ErrPending     = errors.New("original local result custody pending")
)

// Keys live only in this process. Restart/key loss makes the ciphertext
// unavailable; this store NEVER reconstructs a key, submits work or re-runs it.
// This local AES-GCM format is not BINARY_STREAM_1, E2E or an LP/TEE claim.
type Config struct {
	Root        string
	MaxResults  int
	ResultBytes int64
	TotalBytes  int64
	Retention   time.Duration
	DrainGrace  time.Duration
}

func DefaultConfig(root string) Config {
	return Config{root, 16, 8 << 20, 128 << 20, 5 * time.Minute, 5 * time.Second}
}

type Store struct {
	mu       sync.Mutex
	c        Config
	root     string
	live     map[*record]bool
	closed   bool
	lock     *os.File
	lockInfo os.FileInfo
}

type options struct {
	IncludeUsage bool     `json:"include_usage"`
	Stops        []string `json:"stops"`
}

type record struct {
	mu                      sync.Mutex
	store                   *Store
	descriptor              supervisor.TextDescriptor
	path                    string
	file                    *os.File
	identity                os.FileInfo
	key                     []byte
	aead                    cipher.AEAD
	header                  [headerBytes]byte
	written                 int64
	sequence                uint32
	notify                  chan struct{}
	done                    chan struct{}
	revoked                 chan struct{}
	endOnce                 sync.Once
	revokeOnce              sync.Once
	sealed, retired, reader bool
	err                     error
	timer                   *time.Timer
	stop                    func()
	life                    context.Context
	abort                   context.CancelFunc
}

func Open(c Config) (*Store, error) {
	if !filepath.IsAbs(c.Root) || c.MaxResults < 1 || c.MaxResults > 32 || c.ResultBytes < 65536 || c.ResultBytes > 32<<20 || c.TotalBytes < c.ResultBytes || c.TotalBytes > 512<<20 || int64(c.MaxResults) > c.TotalBytes/c.ResultBytes || c.Retention < time.Second || c.Retention > 30*time.Minute || c.DrainGrace < 100*time.Millisecond || c.DrainGrace > 10*time.Second {
		return nil, ErrConfig
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return nil, ErrConfig
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, ErrConfig
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(real) != filepath.Clean(root) {
		return nil, ErrConfig
	}
	// An abandoned root needs explicit recovery. Never ignore orphan ciphertext,
	// reconstruct lost keys or share a quota with another process.
	dir, err := os.Open(root)
	if err != nil {
		return nil, ErrConfig
	}
	items, scanErr := dir.ReadDir(1)
	_ = dir.Close()
	if len(items) != 0 || scanErr != io.EOF {
		return nil, ErrConfig
	}
	lock, err := os.OpenFile(filepath.Join(root, ".p11-result-owner"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrConfig
	}
	info, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, ErrConfig
	}
	return &Store{c: c, root: root, live: map[*record]bool{}, lock: lock, lockInfo: info}, nil
}

func original(w supervisor.Work, m executionlease.Manifest) bool {
	return w.RequestID != "" && len(w.RequestID) <= 192 && !strings.ContainsRune(w.RequestID, 0) && utf8.ValidString(w.RequestID) && w.LocalGeneration != "" && len(w.LocalGeneration) <= 192 && w.Resource != "" && w.Resource == m.Resource && executionlease.Hash(w.Environment) && w.Environment == m.EnvironmentDigest && m.Profile == executionlease.ManifestProfile && m.Mode == "PLAINTEXT" && m.Privacy == "STANDARD" && m.Spec.Model != "" && m.Spec.MaxOutputTokens > 0 && m.InputTokenLimit > 0
}

func (s *Store) begin(w supervisor.Work, m executionlease.Manifest, o options, stop func()) (*record, error) {
	if !original(w, m) {
		return nil, ErrBinding
	}
	if len(o.Stops) > 4 {
		return nil, ErrBinding
	}
	for _, v := range o.Stops {
		if len(v) < 1 || len(v) > 256 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return nil, ErrBinding
		}
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, ErrUnavailable
	}
	r := &record{store: s, key: make([]byte, 32), notify: make(chan struct{}), done: make(chan struct{}), revoked: make(chan struct{}), stop: stop}
	r.life, r.abort = context.WithCancel(context.Background())
	r.descriptor = supervisor.TextDescriptor{Work: w, Manifest: m, ResultID: "result_" + hex.EncodeToString(nonce[:16]), CreatedUnix: time.Now().Unix(), IncludeUsage: o.IncludeUsage}
	encoded, _ := json.Marshal(r.descriptor)
	binding := sha256.Sum256(encoded)
	clear(encoded)
	copy(r.header[:8], "P11RES01")
	copy(r.header[8:40], binding[:])
	copy(r.header[40:], nonce[16:])
	if _, err := rand.Read(r.key); err != nil {
		clear(r.key)
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(r.key)
	if err != nil {
		clear(r.key)
		return nil, ErrUnavailable
	}
	r.aead, err = cipher.NewGCM(block)
	if err != nil {
		clear(r.key)
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	if s.closed || len(s.live) >= s.c.MaxResults {
		s.mu.Unlock()
		clear(r.key)
		r.aead = nil
		return nil, ErrCapacity
	}
	s.live[r] = true
	s.mu.Unlock()
	failed := true
	defer func() {
		if failed {
			r.complete(ErrUnavailable)
			r.destroy()
		}
	}()
	r.file, err = os.CreateTemp(s.root, "result-*.sealed")
	if err != nil {
		return nil, ErrUnavailable
	}
	r.path = r.file.Name()
	if err = r.file.Chmod(0600); err != nil {
		return nil, ErrUnavailable
	}
	r.identity, err = r.file.Stat()
	if err != nil || !r.identity.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	if n, e := r.file.WriteAt(r.header[:], 0); e != nil || n != headerBytes {
		return nil, ErrUnavailable
	}
	r.written = headerBytes
	b, err := json.Marshal(o)
	if err != nil {
		return nil, ErrBinding
	}
	err = r.append(kOptions, b)
	clear(b)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.timer = time.AfterFunc(s.c.Retention, func() {
		r.revoke()
		if r.stop != nil {
			r.stop()
		}
		go func() { <-r.done; r.destroy() }()
	})
	r.mu.Unlock()
	failed = false
	return r, nil
}

func (r *record) signal() { close(r.notify); r.notify = make(chan struct{}) }
func (r *record) revoke() { r.revokeOnce.Do(func() { close(r.revoked); r.abort() }) }

func (r *record) append(kind byte, plain []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.revoked:
		return ErrUnavailable
	default:
	}
	if r.retired || r.err != nil || r.sealed || len(plain) > maxPiece || r.sequence >= maxRecords || r.written+int64(frameBytes+len(plain)+r.aead.Overhead()) > r.store.c.ResultBytes {
		return ErrCapacity
	}
	var prefix [frameBytes]byte
	prefix[0] = kind
	binary.BigEndian.PutUint32(prefix[1:5], r.sequence)
	binary.BigEndian.PutUint32(prefix[5:], uint32(len(plain)))
	var nonce [12]byte
	copy(nonce[:8], r.header[40:])
	binary.BigEndian.PutUint32(nonce[8:], r.sequence)
	aad := make([]byte, 0, headerBytes+frameBytes)
	aad = append(aad, r.header[:]...)
	aad = append(aad, prefix[:]...)
	sealed := r.aead.Seal(nil, nonce[:], plain, aad)
	frame := append(prefix[:], sealed...)
	n, err := r.file.WriteAt(frame, r.written)
	if err != nil || n != len(frame) {
		return ErrUnavailable
	}
	r.written += int64(n)
	r.sequence++
	r.signal()
	return nil
}

func (r *record) complete(err error) {
	r.mu.Lock()
	if err != nil {
		r.err = ErrUnavailable
		r.revoke()
	} else {
		r.sealed = true
	}
	r.signal()
	r.endOnce.Do(func() { close(r.done) })
	r.mu.Unlock()
}

func (r *record) destroy() error {
	r.mu.Lock()
	r.retired = true
	r.revoke()
	if r.timer != nil {
		r.timer.Stop()
	}
	if r.reader {
		r.mu.Unlock()
		return ErrPending
	}
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
	clear(r.key)
	r.key = nil
	r.aead = nil
	r.signal()
	path := r.path
	r.mu.Unlock()
	// Failed removal keeps the quota slot reserved. Repeated Discard/Close may
	// retry it; disk failure never manufactures available storage capacity.
	if path != "" {
		if e := os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) {
			return ErrUnavailable
		}
	}
	r.store.mu.Lock()
	delete(r.store.live, r)
	r.store.mu.Unlock()
	return nil
}

func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	all := make([]*record, 0, len(s.live))
	for r := range s.live {
		all = append(all, r)
	}
	s.mu.Unlock()
	for _, r := range all {
		r.revoke()
		if r.stop != nil {
			r.stop()
		}
	}
	for _, r := range all {
		select {
		case <-r.done:
			if err := r.destroy(); err != nil {
				return err
			}
		case <-ctx.Done():
			return ErrPending
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		path := s.lock.Name()
		info, e := os.Lstat(path)
		if e != nil || !os.SameFile(info, s.lockInfo) {
			return ErrUnavailable
		}
		_ = s.lock.Close()
		s.lock = nil
		if os.Remove(path) != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func encodeResult(v supervisor.TextResult, total uint64, sum []byte) []byte {
	b := make([]byte, 24+32)
	binary.BigEndian.PutUint32(b, v.InputTokens)
	binary.BigEndian.PutUint32(b[4:], v.OutputTokens)
	binary.BigEndian.PutUint32(b[8:], v.StopIndex)
	switch v.Reason {
	case "eog":
		b[12] = 1
	case "limit":
		b[12] = 2
	case "stop":
		b[12] = 3
	}
	binary.BigEndian.PutUint64(b[16:], total)
	copy(b[24:], sum)
	return b
}

func (r *record) open(ctx context.Context) (*view, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired || r.err != nil || r.reader {
		return nil, ErrUnavailable
	}
	select {
	case <-r.revoked:
		return nil, ErrUnavailable
	default:
	}
	info, err := os.Lstat(r.path)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, r.identity) {
		return nil, ErrUnavailable
	}
	f, err := os.Open(r.path)
	if err != nil {
		return nil, ErrUnavailable
	}
	var header [headerBytes]byte
	if _, err = io.ReadFull(f, header[:]); err != nil || header != r.header {
		f.Close()
		return nil, ErrUnavailable
	}
	info, err = f.Stat()
	if err != nil || !os.SameFile(info, r.identity) {
		f.Close()
		return nil, ErrUnavailable
	}
	r.reader = true
	return &view{record: r, file: f, at: headerBytes, hash: sha256.New()}, nil
}
