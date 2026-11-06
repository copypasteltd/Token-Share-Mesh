package media

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Staged multipart assets never spill plaintext to disk. Cipher blocks are
// cleared as the original rental input consumes them, avoiding a second full
// retained media buffer. This is local staging, not network E2E qualification.
type stagedAsset struct {
	mu                       sync.Mutex
	key, prefix, plain, head []byte
	blocks                   [][]byte
	index                    int
	size                     int64
	closed                   bool
}

func (s *stagedAsset) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[encrypted local media asset redacted]")
}
func (s *stagedAsset) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func nonce(prefix []byte, n int) []byte {
	out := make([]byte, 12)
	copy(out, prefix)
	binary.LittleEndian.PutUint32(out[8:], uint32(n))
	return out
}
func stageAsset(ctx context.Context, source io.Reader, maximum int64) (out *stagedAsset, err error) {
	s := &stagedAsset{key: make([]byte, 32), prefix: make([]byte, 8)}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if _, err = rand.Read(s.key); err != nil {
		return nil, ErrRequest
	}
	if _, err = rand.Read(s.prefix); err != nil {
		return nil, ErrRequest
	}
	block, _ := aes.NewCipher(s.key)
	aead, _ := cipher.NewGCM(block)
	scratch := make([]byte, 65536)
	defer clear(scratch)
	used := 0
	for {
		if ctx.Err() != nil {
			return nil, ErrRequest
		}
		n, e := source.Read(scratch[used:])
		if n < 0 || n > len(scratch)-used || n == 0 && e == nil {
			return nil, ErrRequest
		}
		if n > 0 {
			if s.size+int64(n) > maximum {
				return nil, ErrRequest
			}
			s.size += int64(n)
			used += n
		}
		if used == len(scratch) || e == io.EOF && used > 0 {
			if len(s.head) == 0 {
				s.head = append([]byte{}, scratch[:min(used, 32)]...)
			}
			s.blocks = append(s.blocks, aead.Seal(nil, nonce(s.prefix, len(s.blocks)), scratch[:used], []byte("P11_LOCAL_HTTP_ASSET_1")))
			clear(scratch[:used])
			used = 0
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, ErrRequest
		}
	}
	if s.size < 1 {
		return nil, ErrRequest
	}
	return s, nil
}
func (s *stagedAsset) Read(dst []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrAccess
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if len(s.plain) == 0 {
		if s.index >= len(s.blocks) {
			return 0, io.EOF
		}
		block, _ := aes.NewCipher(s.key)
		aead, _ := cipher.NewGCM(block)
		raw, e := aead.Open(nil, nonce(s.prefix, s.index), s.blocks[s.index], []byte("P11_LOCAL_HTTP_ASSET_1"))
		clear(s.blocks[s.index])
		s.blocks[s.index] = nil
		s.index++
		if e != nil {
			return 0, ErrAccess
		}
		s.plain = raw
	}
	n := copy(dst, s.plain)
	clear(s.plain[:n])
	s.plain = s.plain[n:]
	return n, nil
}
func (s *stagedAsset) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		clear(s.key)
		clear(s.prefix)
		clear(s.plain)
		clear(s.head)
		for _, b := range s.blocks {
			clear(b)
		}
		s.blocks = nil
		s.plain = nil
	}
	return nil
}
func (s *stagedAsset) png() bool {
	if len(s.head) < 24 || !bytes.Equal(s.head[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) || string(s.head[12:16]) != "IHDR" {
		return false
	}
	w, h := binary.BigEndian.Uint32(s.head[16:20]), binary.BigEndian.Uint32(s.head[20:24])
	return w > 0 && h > 0 && w <= 8192 && h <= 8192 && uint64(w)*uint64(h) <= 16777216
}

// Cache reads keep ciphertext intact for authenticated repeated/range reads.
// At most one 64KiB plaintext block exists per reader; no plaintext file/cache.
func (s *stagedAsset) readAt(ctx context.Context, dst []byte, offset int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || ctx.Err() != nil || offset < 0 || offset > s.size {
		return 0, ErrAccess
	}
	if offset == s.size {
		return 0, io.EOF
	}
	index := int(offset / 65536)
	inside := int(offset % 65536)
	if index >= len(s.blocks) || s.blocks[index] == nil {
		return 0, ErrAccess
	}
	block, _ := aes.NewCipher(s.key)
	aead, _ := cipher.NewGCM(block)
	raw, e := aead.Open(nil, nonce(s.prefix, index), s.blocks[index], []byte("P11_LOCAL_HTTP_ASSET_1"))
	if e != nil {
		return 0, ErrAccess
	}
	defer clear(raw)
	n := copy(dst, raw[inside:])
	return n, nil
}
func (s *stagedAsset) hashInto(ctx, owner context.Context, dst io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrAccess
	}
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(s.size))
	if _, e := dst.Write(length[:]); e != nil {
		return e
	}
	block, _ := aes.NewCipher(s.key)
	aead, _ := cipher.NewGCM(block)
	for i, sealed := range s.blocks {
		if ctx.Err() != nil || owner.Err() != nil {
			return ErrAccess
		}
		raw, e := aead.Open(nil, nonce(s.prefix, i), sealed, []byte("P11_LOCAL_HTTP_ASSET_1"))
		if e != nil {
			return ErrAccess
		}
		_, e = dst.Write(raw)
		clear(raw)
		if e != nil {
			return e
		}
	}
	return nil
}
