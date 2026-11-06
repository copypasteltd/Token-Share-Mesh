package ciphercustody

import (
	"context"
	"io"
	"sync"
	"time"

	"p11.local/compute/client/protocol/cipherstream"
)

type boundedSink struct {
	mu               sync.Mutex
	ctx              context.Context
	file             File
	maximum, written int64
	failed           bool
}

func (w *boundedSink) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed || w.ctx.Err() != nil {
		w.failed = true
		return 0, ErrClosed
	}
	if int64(len(raw)) > w.maximum-w.written {
		w.failed = true
		return 0, ErrCapacity
	}
	total := 0
	for len(raw) > 0 {
		if w.ctx.Err() != nil {
			w.failed = true
			return total, ErrClosed
		}
		piece := raw[:min(len(raw), CopyBufferBytes)]
		n, e := w.file.Write(piece)
		if n < 0 || n > len(piece) {
			w.failed = true
			return total, ErrState
		}
		w.written += int64(n)
		total += n
		raw = raw[n:]
		if e != nil || n == 0 {
			w.failed = true
			return total, ErrState
		}
	}
	return total, nil
}

// File close interrupts owned file I/O on cancellation and is joined before
// metadata/root/key/lock can be released. It does not stop an arbitrary producer
// that ignores context while blocked on its own external input.
func ownFile(ctx context.Context, f File) func() error {
	closed := make(chan struct{})
	var closeError error
	stop := context.AfterFunc(ctx, func() { closeError = f.Close(); close(closed) })
	var once sync.Once
	return func() error {
		once.Do(func() {
			if stop() {
				closeError = f.Close()
			} else {
				<-closed
			}
		})
		return closeError
	}
}
func result(r entry, replayed bool) Result {
	m := *r.Manifest
	return Result{Handle: Handle{r.ID, r.Descriptor.Hash(), m.Hash()}, Manifest: m, Bytes: r.Bytes, Replayed: replayed}
}
func (s *Store) lookupLocked(h Handle) (*entry, error) {
	if !h.Validate() {
		return nil, ErrScope
	}
	for i := range s.state.Objects {
		r := &s.state.Objects[i]
		if r.ID != h.ID {
			continue
		}
		if r.Descriptor.Hash() != h.DescriptorHash || r.Manifest == nil || r.Manifest.Hash() != h.ManifestHash {
			return nil, ErrScope
		}
		return r, nil
	}
	return nil, ErrState
}

// Produce reserves its complete disk bound and journals one original intent
// BEFORE invoking the producer. Retrying READY returns its exact old manifest;
// WRITING/INTERRUPTED never invoke the producer again. A crash cannot resume a
// secretstream push state or silently repeat inference.
func (s *Store) Produce(parent context.Context, d Descriptor, now time.Time, produce func(context.Context, io.Writer) (cipherstream.Manifest, error)) (_ Result, answer error) {
	if parent == nil || parent.Err() != nil || produce == nil || d.Validate(now) != nil {
		return Result{}, ErrScope
	}
	s.mu.Lock()
	if !s.readyLocked() {
		s.mu.Unlock()
		return Result{}, ErrClosed
	}
	for _, r := range s.state.Objects {
		if r.Descriptor.Context.Object != d.Context.Object {
			continue
		}
		if r.Descriptor.Hash() != d.Hash() {
			s.mu.Unlock()
			return Result{}, ErrScope
		}
		s.mu.Unlock()
		switch r.State {
		case "READY":
			return result(r, true), nil
		case "WRITING":
			return Result{}, ErrPending
		default:
			return Result{}, ErrInterrupted
		}
	}
	capacity, _ := cipherstream.Counter(s.state.Policy.Bytes, false, 63)
	if len(s.state.Objects) >= s.state.Policy.Objects || s.payloadUsesLocked() >= MaximumActiveUses || d.maximum() > int64(capacity)-s.reservedLocked() {
		s.mu.Unlock()
		return Result{}, ErrCapacity
	}
	random, e := NewID()
	if e != nil {
		s.mu.Unlock()
		return Result{}, e
	}
	r := entry{ID: "custody_" + random, File: "cipher-object-" + random + ".bin", Descriptor: d, Created: now.UTC().Format(time.RFC3339Nano), State: "WRITING", Bytes: "0"}
	s.state.Objects = append(s.state.Objects, r)
	if s.saveLocked() != nil {
		s.mu.Unlock()
		return Result{}, ErrState
	}
	expires, _ := cipherstream.CanonicalTime(d.ManifestExpiry)
	ctx, done, e := s.beginUseLocked(parent, r.ID, expires)
	if e != nil {
		s.state.Objects[len(s.state.Objects)-1].State = "INTERRUPTED"
		_ = s.saveLocked()
		s.mu.Unlock()
		return Result{}, e
	}
	s.mu.Unlock()
	defer done()
	var written int64
	defer func() {
		if answer == nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.state.Objects {
			stored := &s.state.Objects[i]
			if stored.ID != r.ID {
				continue
			}
			if stored.State == "WRITING" {
				stored.State = "INTERRUPTED"
			}
			stored.Bytes = decimal(written)
			if s.saveLocked() != nil {
				answer = ErrState
			}
			break
		}
	}()
	f, e := s.files.Create(r.File)
	if e != nil {
		return Result{}, ErrState
	}
	closeFile := ownFile(ctx, f)
	defer closeFile()
	w := &boundedSink{ctx: ctx, file: f, maximum: d.maximum()}
	m, e := produce(ctx, w)
	w.mu.Lock()
	written = w.written
	bad := w.failed
	w.mu.Unlock()
	if e != nil || bad || ctx.Err() != nil {
		return Result{}, ErrInterrupted
	}
	if m.Expires != d.ManifestExpiry || d.Origin == "INBOUND" && m.Hash() != d.InboundManifest {
		return Result{}, ErrScope
	}
	if f.Sync() != nil || closeFile() != nil {
		return Result{}, ErrState
	}
	expected, e := d.Expected()
	if e != nil {
		return Result{}, e
	}
	verify, e := s.files.Open(r.File)
	if e != nil {
		return Result{}, ErrState
	}
	closeVerify := ownFile(ctx, verify)
	summary, e := cipherstream.InspectOpaque(verify, expected, m, time.Now().UTC())
	ce := closeVerify()
	if e != nil || ce != nil || ctx.Err() != nil || summary.CipherBytes != written {
		return Result{}, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.readyLocked() || ctx.Err() != nil {
		return Result{}, ErrClosed
	}
	for i := range s.state.Objects {
		stored := &s.state.Objects[i]
		if stored.ID != r.ID {
			continue
		}
		if stored.State != "WRITING" {
			return Result{}, ErrInterrupted
		}
		stored.Manifest = &m
		stored.Bytes = decimal(written)
		stored.State = "READY"
		if s.saveLocked() != nil {
			return Result{}, ErrState
		}
		return result(*stored, false), nil
	}
	return Result{}, ErrState
}

// Import stores only bounded original ciphertext. Opaque validation does NOT
// establish authenticated plaintext/END or make INPUT executable.
func (s *Store) Import(parent context.Context, d Descriptor, m cipherstream.Manifest, source io.Reader, now time.Time) (Result, error) {
	if source == nil || d.Origin != "INBOUND" || d.InboundManifest != m.Hash() || m.Expires != d.ManifestExpiry {
		return Result{}, ErrScope
	}
	return s.Produce(parent, d, now, func(ctx context.Context, out io.Writer) (cipherstream.Manifest, error) {
		buffer := make([]byte, CopyBufferBytes)
		defer clear(buffer)
		var total int64
		empty := 0
		for {
			if ctx.Err() != nil {
				return cipherstream.Manifest{}, ErrClosed
			}
			n, e := source.Read(buffer)
			if n < 0 || n > len(buffer) || int64(n) > d.maximum()-total {
				return cipherstream.Manifest{}, ErrCapacity
			}
			if n > 0 {
				wn, we := out.Write(buffer[:n])
				total += int64(wn)
				empty = 0
				if we != nil || wn != n {
					return cipherstream.Manifest{}, ErrState
				}
			} else if e == nil {
				empty++
				if empty > 8 {
					return cipherstream.Manifest{}, io.ErrNoProgress
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return cipherstream.Manifest{}, ErrState
			}
		}
		return m, nil
	})
}
