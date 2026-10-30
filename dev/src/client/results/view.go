package results

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"hash"
	"io"
	"os"
	"sync"

	"p11.local/compute/client/supervisor"
)

// A view is a serial, original-result capability, not execution authority.
// Close releases this reader; explicit Discard/retention destroys the result.
type view struct {
	mu                        sync.Mutex
	record                    *record
	file                      *os.File
	at                        int64
	sequence                  uint32
	hash                      hash.Hash
	total                     uint64
	options                   options
	start                     supervisor.TextStart
	result                    supervisor.TextResult
	started, finished, closed bool
	err                       error
}

func (v *view) Descriptor() supervisor.TextDescriptor { return v.record.descriptor }

func (v *view) next(ctx context.Context) (byte, []byte, error) {
	r := v.record
	for {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		select {
		case <-r.revoked:
			return 0, nil, ErrUnavailable
		default:
		}
		r.mu.Lock()
		written, done, notice := r.written, r.sealed, r.notify
		aead := r.aead
		bad := r.retired || r.err != nil || aead == nil
		r.mu.Unlock()
		if bad {
			return 0, nil, ErrUnavailable
		}
		if written-v.at < frameBytes {
			if done {
				return 0, nil, ErrUnavailable
			}
			select {
			case <-notice:
				continue
			case <-r.revoked:
				return 0, nil, ErrUnavailable
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			}
		}
		var prefix [frameBytes]byte
		if n, e := v.file.ReadAt(prefix[:], v.at); e != nil || n != frameBytes {
			return 0, nil, ErrUnavailable
		}
		seq := binary.BigEndian.Uint32(prefix[1:5])
		size := binary.BigEndian.Uint32(prefix[5:])
		if seq != v.sequence || seq >= maxRecords || size > maxPiece {
			return 0, nil, ErrUnavailable
		}
		full := int64(frameBytes) + int64(size) + int64(aead.Overhead())
		if full > written-v.at {
			return 0, nil, ErrUnavailable
		}
		ciphertext := make([]byte, int(size)+aead.Overhead())
		if n, e := v.file.ReadAt(ciphertext, v.at+frameBytes); e != nil || n != len(ciphertext) {
			clear(ciphertext)
			return 0, nil, ErrUnavailable
		}
		var nonce [12]byte
		copy(nonce[:8], r.header[40:])
		binary.BigEndian.PutUint32(nonce[8:], seq)
		aad := append(append(make([]byte, 0, headerBytes+frameBytes), r.header[:]...), prefix[:]...)
		plain, e := aead.Open(nil, nonce[:], ciphertext, aad)
		clear(ciphertext)
		if e != nil {
			return 0, nil, ErrUnavailable
		}
		if ctx.Err() != nil {
			clear(plain)
			return 0, nil, ctx.Err()
		}
		select {
		case <-r.revoked:
			clear(plain)
			return 0, nil, ErrUnavailable
		default:
		}
		v.at += full
		v.sequence++
		return prefix[0], plain, nil
	}
}

func (v *view) startLocked(ctx context.Context) (supervisor.TextStart, error) {
	if v.closed || v.err != nil {
		return supervisor.TextStart{}, ErrUnavailable
	}
	if v.started {
		return v.start, nil
	}
	kind, b, e := v.next(ctx)
	if e != nil {
		return supervisor.TextStart{}, e
	}
	defer clear(b)
	if kind != kOptions || len(b) > 2048 {
		return supervisor.TextStart{}, ErrUnavailable
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&v.options); e != nil || dec.Decode(new(any)) != io.EOF || v.options.IncludeUsage != v.record.descriptor.IncludeUsage || len(v.options.Stops) > 4 {
		return supervisor.TextStart{}, ErrUnavailable
	}
	for _, s := range v.options.Stops {
		if len(s) < 1 || len(s) > 256 {
			return supervisor.TextStart{}, ErrUnavailable
		}
	}
	kind, b2, e := v.next(ctx)
	defer clear(b2)
	if e != nil {
		return supervisor.TextStart{}, e
	}
	if kind != kStarted || len(b2) != 4 {
		return supervisor.TextStart{}, ErrUnavailable
	}
	v.start.InputTokens = binary.BigEndian.Uint32(b2)
	if v.start.InputTokens == 0 || uint64(v.start.InputTokens) > uint64(v.record.descriptor.Manifest.InputTokenLimit) {
		return supervisor.TextStart{}, ErrUnavailable
	}
	v.started = true
	return v.start, nil
}

func (v *view) Started(ctx context.Context) (supervisor.TextStart, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, e := v.startLocked(ctx)
	if e != nil {
		v.err = ErrUnavailable
	}
	return s, e
}

func (v *view) Read(ctx context.Context) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, e := v.startLocked(ctx); e != nil {
		v.err = ErrUnavailable
		return nil, e
	}
	if v.finished {
		return nil, io.EOF
	}
	kind, b, e := v.next(ctx)
	if e != nil {
		v.err = ErrUnavailable
		return nil, e
	}
	if kind == kText && len(b) > 0 {
		v.total += uint64(len(b))
		_, _ = v.hash.Write(b)
		return b, nil
	}
	defer clear(b)
	if kind != kFinal || len(b) != 56 || b[13] != 0 || b[14] != 0 || b[15] != 0 || binary.BigEndian.Uint64(b[16:]) != v.total || !bytes.Equal(b[24:], v.hash.Sum(nil)) {
		v.err = ErrUnavailable
		return nil, ErrUnavailable
	}
	result := supervisor.TextResult{InputTokens: binary.BigEndian.Uint32(b), OutputTokens: binary.BigEndian.Uint32(b[4:]), StopIndex: binary.BigEndian.Uint32(b[8:])}
	switch b[12] {
	case 1:
		result.Reason = "eog"
	case 2:
		result.Reason = "limit"
	case 3:
		result.Reason = "stop"
	default:
		v.err = ErrUnavailable
		return nil, ErrUnavailable
	}
	m := v.record.descriptor.Manifest
	if result.InputTokens != v.start.InputTokens || uint64(result.OutputTokens) > uint64(m.Spec.MaxOutputTokens) || (result.Reason == "stop" && uint64(result.StopIndex) >= uint64(len(v.options.Stops))) || (result.Reason != "stop" && result.StopIndex != supervisor.NoStopIndex) {
		v.err = ErrUnavailable
		return nil, ErrUnavailable
	}
	// END is not visible as success until the producer synced the complete
	// encrypted file. Authenticated counts/hash alone do not prove durability.
	select {
	case <-v.record.done:
	case <-ctx.Done():
		v.err = ErrUnavailable
		return nil, ctx.Err()
	}
	v.record.mu.Lock()
	ok := v.record.sealed && v.record.err == nil && v.record.written == v.at
	v.record.mu.Unlock()
	info, e := v.file.Stat()
	select {
	case <-v.record.revoked:
		ok = false
	default:
	}
	if !ok || e != nil || info.Size() != v.at {
		v.err = ErrUnavailable
		return nil, ErrUnavailable
	}
	v.result = result
	v.finished = true
	return nil, io.EOF
}

func (v *view) Result(ctx context.Context) (supervisor.TextResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	select {
	case <-v.record.revoked:
		return supervisor.TextResult{}, ErrUnavailable
	default:
	}
	if ctx.Err() != nil {
		return supervisor.TextResult{}, ctx.Err()
	}
	if v.closed || v.err != nil || !v.finished {
		return supervisor.TextResult{}, ErrUnavailable
	}
	return v.result, nil
}

func (v *view) StopSequence(ctx context.Context, index uint32) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	select {
	case <-v.record.revoked:
		return nil, ErrUnavailable
	default:
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if v.closed || v.err != nil || !v.started || uint64(index) >= uint64(len(v.options.Stops)) {
		return nil, ErrUnavailable
	}
	return []byte(v.options.Stops[index]), nil
}

func (v *view) Close(context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil
	}
	v.closed = true
	v.options = options{}
	v.hash = sha256.New()
	_ = v.file.Close()
	v.record.mu.Lock()
	v.record.reader = false
	retired := v.record.retired
	v.record.mu.Unlock()
	select {
	case <-v.record.revoked:
		retired = true
	default:
	}
	if retired {
		return v.record.destroy()
	}
	return nil
}
