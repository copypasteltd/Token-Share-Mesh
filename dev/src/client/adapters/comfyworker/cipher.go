// Package comfyworker implements the private ComfyUI engine boundary. The
// local cipher format is neither BINARY_STREAM_1 nor network E2E/TEE evidence.
package comfyworker

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sync"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const Profile = "P11_COMFY_PRIVATE_PIPE_2"
const Release = "comfy-private-2"
const MaximumMediaBytes int64 = 64 << 20
const chunkBytes = 65536
const headerBytes = 48
const recordBytes = 9

// END is <QQ32s>: record count, plaintext size and SHA-256 (8+8+32).
const endBytes = 48

var ErrOutput = errors.New("original ComfyUI encrypted media unavailable or unconfirmed")
var basenamePattern = regexp.MustCompile(`^cipher-[a-f0-9]{32}\.bin$`)

// Scope must come from the original independently authorized host Work. The
// receipt cannot select a different session, invocation, environment or recipe.
type Scope struct {
	Session         string `json:"session_id"`
	Invocation      string `json:"invocation_id"`
	LocalGeneration string `json:"local_generation"`
	Environment     string `json:"environment_digest"`
	Recipe          string `json:"recipe_digest"`
}

func (s Scope) bytes() ([]byte, error) {
	for _, v := range []string{s.Session, s.Invocation} {
		if len(v) > 128 || !ids.Valid(v) {
			return nil, ErrOutput
		}
	}
	if n, e := executionlease.Counter(s.LocalGeneration); e != nil || n == 0 {
		return nil, ErrOutput
	}
	if !executionlease.Hash(s.Environment) || !executionlease.Hash(s.Recipe) {
		return nil, ErrOutput
	}
	// encoding/json sorts these ASCII map keys, matching Python canonical().
	return json.Marshal(map[string]string{"session_id": s.Session, "invocation_id": s.Invocation, "local_generation": s.LocalGeneration, "environment_digest": s.Environment, "recipe_digest": s.Recipe})
}

type receiptWire struct {
	Basename   string `json:"basename"`
	Key        string `json:"key"`
	ScopeHash  string `json:"scope_sha256"`
	Bytes      int64  `json:"plaintext_bytes"`
	Records    uint32 `json:"data_records"`
	Digest     string `json:"plaintext_sha256"`
	Kind       string `json:"media_kind"`
	Invocation string `json:"invocation_id"`
}
type receiptState struct {
	mu        sync.Mutex
	consumed  bool
	scope     Scope
	wire      receiptWire
	key       []byte
	scopeHash [32]byte
}
type Receipt struct{ state *receiptState }

func (r Receipt) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private ComfyUI receipt redacted]")
}
func (r Receipt) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func mediaKind(kind string) bool {
	return kind == "PNG" || kind == "MP4" || kind == "WAV" || kind == "GLB"
}

// DecodeReceipt accepts only an authenticated ORIGINAL worker's private result
// frame; expected scope/kind are independent. It grants no execution, buyer
// acceptance, trusted usage, availability, cleanup or payment qualification.
func DecodeReceipt(raw []byte, expected Scope, kind string) (Receipt, error) {
	b, e := expected.bytes()
	if e != nil || !mediaKind(kind) {
		return Receipt{}, ErrOutput
	}
	var w receiptWire
	if strictjson.Decode(raw, &w, 65536) != nil || !basenamePattern.MatchString(w.Basename) || w.Invocation != expected.Invocation || w.Kind != kind || !executionlease.Hash(w.ScopeHash) || !executionlease.Hash(w.Digest) || w.Bytes < 1 || w.Bytes > MaximumMediaBytes || w.Records != uint32((w.Bytes+chunkBytes-1)/chunkBytes) {
		return Receipt{}, ErrOutput
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != w.ScopeHash {
		return Receipt{}, ErrOutput
	}
	key, e := base64.RawURLEncoding.Strict().DecodeString(w.Key)
	if e != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != w.Key {
		clear(key)
		return Receipt{}, ErrOutput
	}
	w.Key = ""
	return Receipt{&receiptState{scope: expected, wire: w, key: key, scopeHash: h}}, nil
}
func (r *Receipt) Close() {
	if r == nil || r.state == nil {
		return
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.consumed = true
	clear(r.state.key)
	r.state.key = nil
}

type Reader struct{ state *readerState }
type readerState struct {
	mu         sync.Mutex
	file       *os.File
	identity   os.FileInfo
	aead       cipher.AEAD
	key        []byte
	header     [headerBytes]byte
	w          receiptWire
	position   int64
	cache      []byte
	cacheIndex int64
	closed     bool
}

func (r Reader) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private ComfyUI media reader redacted]")
}
func (r Reader) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// OpenCipher owns only this file descriptor, not root. The caller must retain
// its original anchored private workspace/root until Close. No arbitrary path,
// directory creation, key reconstruction, process launch or rerun is provided.
// The single-use receipt is consumed even on failure.
func OpenCipher(ctx context.Context, root *os.Root, expected Scope, receipt *Receipt) (*Reader, error) {
	if receipt == nil || receipt.state == nil {
		return nil, ErrOutput
	}
	original := receipt.state
	original.mu.Lock()
	if original.consumed {
		original.mu.Unlock()
		return nil, ErrOutput
	}
	original.consumed = true
	state := &receiptState{scope: original.scope, wire: original.wire, key: append([]byte(nil), original.key...), scopeHash: original.scopeHash}
	clear(original.key)
	original.key = nil
	original.mu.Unlock()
	defer clear(state.key)
	if ctx == nil || ctx.Err() != nil || root == nil || state.scope != expected {
		return nil, ErrOutput
	}
	info, e := root.Lstat(state.wire.Basename)
	if e != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeIrregular|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || runtime.GOOS == "linux" && info.Mode().Perm() != 0600 {
		return nil, ErrOutput
	}
	f, e := root.Open(state.wire.Basename)
	if e != nil {
		return nil, ErrOutput
	}
	r := &Reader{state: &readerState{file: f, identity: info, key: append([]byte(nil), state.key...), w: state.wire, cacheIndex: -1}}
	failed := true
	defer func() {
		if failed {
			r.Close()
		}
	}()
	actual, e := f.Stat()
	expectedSize := int64(headerBytes) + r.state.w.Bytes + int64(r.state.w.Records)*(recordBytes+16) + recordBytes + endBytes + 16
	if e != nil || !os.SameFile(info, actual) || actual.Size() != expectedSize {
		return nil, ErrOutput
	}
	if n, e := f.ReadAt(r.state.header[:], 0); e != nil || n != headerBytes || string(r.state.header[:8]) != "P11CS01\x00" || string(r.state.header[16:]) != string(state.scopeHash[:]) {
		return nil, ErrOutput
	}
	block, e := aes.NewCipher(r.state.key)
	if e != nil {
		return nil, ErrOutput
	}
	r.state.aead, e = cipher.NewGCM(block)
	if e != nil {
		return nil, ErrOutput
	}
	plain, e := r.record(ctx, r.state.w.Records, 2, endBytes, expectedSize-recordBytes-endBytes-16)
	if e != nil {
		return nil, e
	}
	defer clear(plain)
	if binary.LittleEndian.Uint64(plain[:8]) != uint64(r.state.w.Records) || binary.LittleEndian.Uint64(plain[8:16]) != uint64(r.state.w.Bytes) || hex.EncodeToString(plain[16:]) != r.state.w.Digest {
		return nil, ErrOutput
	}
	failed = false
	return r, nil
}
func (r *Reader) record(ctx context.Context, sequence uint32, kind byte, size int, offset int64) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || r.state.closed || r.state.file == nil || size < 0 || size > chunkBytes {
		return nil, ErrOutput
	}
	var header [recordBytes]byte
	binary.LittleEndian.PutUint32(header[:4], sequence)
	header[4] = kind
	binary.LittleEndian.PutUint32(header[5:], uint32(size))
	raw := make([]byte, recordBytes+size+16)
	defer clear(raw)
	if n, e := r.state.file.ReadAt(raw, offset); e != nil || n != len(raw) || string(raw[:recordBytes]) != string(header[:]) {
		return nil, ErrOutput
	}
	nonce := make([]byte, 12)
	copy(nonce, r.state.header[8:16])
	binary.LittleEndian.PutUint32(nonce[8:], sequence)
	aad := make([]byte, 0, headerBytes+recordBytes)
	aad = append(aad, r.state.header[:]...)
	aad = append(aad, header[:]...)
	plain, e := r.state.aead.Open(nil, nonce, raw[recordBytes:], aad)
	if e != nil || len(plain) != size {
		clear(plain)
		return nil, ErrOutput
	}
	return plain, nil
}
func (r *Reader) readAt(ctx context.Context, p []byte, offset int64) (int, error) {
	if ctx == nil || ctx.Err() != nil || r.state.closed || offset < 0 || offset > r.state.w.Bytes {
		return 0, ErrOutput
	}
	if len(p) == 0 {
		return 0, nil
	}
	if offset == r.state.w.Bytes {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && offset < r.state.w.Bytes {
		if ctx.Err() != nil {
			return n, ErrOutput
		}
		index := offset / chunkBytes
		if r.state.cacheIndex != index {
			clear(r.state.cache)
			r.state.cache = nil
			r.state.cacheIndex = -1
			size := int64(chunkBytes)
			if tail := r.state.w.Bytes - index*chunkBytes; tail < size {
				size = tail
			}
			data, e := r.record(ctx, uint32(index), 1, int(size), headerBytes+index*(chunkBytes+recordBytes+16))
			if e != nil {
				return n, e
			}
			r.state.cache = data
			r.state.cacheIndex = index
		}
		now := copy(p[n:], r.state.cache[offset%chunkBytes:])
		if now == 0 {
			return n, ErrOutput
		}
		n += now
		offset += int64(now)
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (r *Reader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if r == nil || r.state == nil {
		return 0, ErrOutput
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	n, e := r.readAt(ctx, p, r.state.position)
	r.state.position += int64(n)
	return n, e
}
func (r *Reader) Read(p []byte) (int, error) { return r.ReadContext(context.Background(), p) }
func (r *Reader) ReadAtContext(ctx context.Context, p []byte, offset int64) (int, error) {
	if r == nil || r.state == nil {
		return 0, ErrOutput
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	return r.readAt(ctx, p, offset)
}
func (r *Reader) ReadAt(p []byte, offset int64) (int, error) {
	return r.ReadAtContext(context.Background(), p, offset)
}
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	if r == nil || r.state == nil {
		return 0, ErrOutput
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if r.state.closed {
		return 0, ErrOutput
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.state.position
	case io.SeekEnd:
		base = r.state.w.Bytes
	default:
		return 0, ErrOutput
	}
	if offset < -base || offset > r.state.w.Bytes-base {
		return 0, ErrOutput
	}
	r.state.position = base + offset
	return r.state.position, nil
}
func (r *Reader) Close() error {
	if r == nil || r.state == nil {
		return nil
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if r.state.closed {
		return nil
	}
	r.state.closed = true
	clear(r.state.cache)
	r.state.cache = nil
	clear(r.state.key)
	r.state.key = nil
	r.state.aead = nil
	if r.state.file != nil {
		e := r.state.file.Close()
		r.state.file = nil
		if e != nil {
			return ErrOutput
		}
	}
	return nil
}
