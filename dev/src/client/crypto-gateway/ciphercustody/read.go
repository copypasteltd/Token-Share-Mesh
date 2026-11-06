package ciphercustody

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"p11.local/compute/client/protocol/cipherstream"
)

func (s *Store) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.readyLocked() {
		return Snapshot{}, ErrClosed
	}
	out := Snapshot{Profile: Profile, Policy: s.state.Policy, Reserved: decimal(s.reservedLocked()), Objects: []PublicState{}}
	for _, r := range s.state.Objects {
		hash := ""
		if r.Manifest != nil {
			hash = r.Manifest.Hash()
		}
		out.Objects = append(out.Objects, PublicState{r.ID, r.Descriptor.Hash(), hash, r.State, r.Bytes, r.Descriptor.ManifestExpiry})
	}
	return out, nil
}

// WithOriginal authenticates original signed ciphertext before exposing any
// file bytes. The synchronous trusted callback must not retain the reader or
// start asynchronous use. No content key, plaintext or execution grant exists
// here. The callback's I/O must obey its context.
func (s *Store) WithOriginal(parent context.Context, d Descriptor, h Handle, now time.Time, fn func(context.Context, io.Reader) error) error {
	if parent == nil || fn == nil || d.Validate(now) != nil || d.Hash() != h.DescriptorHash {
		return ErrScope
	}
	s.mu.Lock()
	if !s.readyLocked() {
		s.mu.Unlock()
		return ErrClosed
	}
	r, e := s.lookupLocked(h)
	if e != nil {
		s.mu.Unlock()
		return e
	}
	if r.State != "READY" {
		s.mu.Unlock()
		return ErrInterrupted
	}
	stored := *r
	m := *r.Manifest
	expiry, _ := cipherstream.CanonicalTime(d.ManifestExpiry)
	ctx, done, e := s.beginUseLocked(parent, r.ID, expiry)
	s.mu.Unlock()
	if e != nil {
		return e
	}
	defer done()
	f, e := s.files.Open(stored.File)
	if e != nil {
		return ErrState
	}
	closeFile := ownFile(ctx, f)
	defer closeFile()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != dSize(stored.Bytes) {
		return ErrState
	}
	expected, e := d.Expected()
	if e != nil {
		return e
	}
	if _, e = cipherstream.InspectOpaque(f, expected, m, time.Now().UTC()); e != nil || ctx.Err() != nil {
		return ErrState
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return ErrState
	}
	if e = fn(ctx, &guardedReader{ctx: ctx, reader: io.LimitReader(f, info.Size())}); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ErrClosed
	}
	return nil
}
func dSize(raw string) int64 { n, _ := cipherstream.Counter(raw, true, 63); return int64(n) }

type guardedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *guardedReader) Read(raw []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, ErrClosed
	}
	n, e := r.reader.Read(raw)
	if r.ctx.Err() != nil {
		return n, ErrClosed
	}
	return n, e
}

// CopyRange retransmits the same already signed ciphertext, never a seek into
// secretstream plaintext. It does not recreate a cipher state or run inference.
func (s *Store) CopyRange(parent context.Context, d Descriptor, h Handle, offset, length int64, destination io.Writer, now time.Time) (int64, error) {
	if destination == nil || offset < 0 || length < 1 || offset > d.maximum() || length > d.maximum()-offset {
		return 0, ErrScope
	}
	var copied int64
	e := s.WithOriginal(parent, d, h, now, func(ctx context.Context, in io.Reader) error {
		s.mu.Lock()
		r, e := s.lookupLocked(h)
		var size int64
		if e == nil {
			size = dSize(r.Bytes)
		}
		s.mu.Unlock()
		if e != nil || offset > size || length > size-offset {
			return ErrScope
		}
		buffer := make([]byte, CopyBufferBytes)
		defer clear(buffer)
		for left := offset; left > 0; {
			n := int(min(int64(len(buffer)), left))
			read, re := io.ReadFull(in, buffer[:n])
			left -= int64(read)
			if re != nil {
				return ErrState
			}
		}
		for copied < length {
			if ctx.Err() != nil {
				return ErrClosed
			}
			n := int(min(int64(len(buffer)), length-copied))
			read, re := io.ReadFull(in, buffer[:n])
			if re != nil || read != n {
				return ErrState
			}
			for at := 0; at < read; {
				if ctx.Err() != nil {
					return ErrClosed
				}
				written, we := destination.Write(buffer[at:read])
				if written < 0 || written > read-at {
					return ErrState
				}
				at += written
				copied += int64(written)
				if we != nil || written == 0 {
					return ErrState
				}
			}
		}
		return nil
	})
	return copied, e
}

// Expire first journals RETIRING and cancels/joins actual readers or producers,
// then removes only the original registered file. Expired tombstones can be
// compacted: replaying the original descriptor is already past its fixed
// manifest deadline and cannot create another valid stream.
// No caller-supplied "ACK" string can cause early deletion or buyer acceptance.
func (s *Store) Expire(parent context.Context, now time.Time) (int, error) {
	if parent == nil || parent.Err() != nil {
		return 0, ErrClosed
	}
	s.mu.Lock()
	// One reserved control owner can cancel both payload slots even at full
	// capacity. Concurrent cleanup must not race file removal or journal compaction.
	if s.maintenance {
		s.mu.Unlock()
		return 0, ErrPending
	}
	ctx, done, e := s.beginUseLocked(parent, maintenanceID, time.Now().UTC().Add(time.Minute))
	if e != nil {
		s.mu.Unlock()
		return 0, e
	}
	s.maintenance = true
	var expired []string
	for _, r := range s.state.Objects {
		until, _ := cipherstream.CanonicalTime(r.Descriptor.ManifestExpiry)
		if !now.Before(until) {
			expired = append(expired, r.ID)
		}
	}
	s.mu.Unlock()
	defer done()
	defer func() { s.mu.Lock(); s.maintenance = false; s.mu.Unlock() }()
	count := 0
	for _, id := range expired {
		if ctx.Err() != nil {
			return count, ErrClosed
		}
		s.mu.Lock()
		var target *entry
		for i := range s.state.Objects {
			if s.state.Objects[i].ID == id {
				target = &s.state.Objects[i]
				break
			}
		}
		if target == nil {
			s.mu.Unlock()
			continue
		}
		name := target.File
		if target.State != "RETIRED" {
			target.State = "RETIRING"
			if s.saveLocked() != nil {
				s.mu.Unlock()
				return count, ErrState
			}
		}
		for _, u := range s.uses {
			if u.id == id {
				u.cancel()
			}
		}
		wg := s.owners[id]
		s.mu.Unlock()
		if wg != nil {
			wg.Wait()
		}
		if ctx.Err() != nil {
			return count, ErrClosed
		}
		if e = s.files.Remove(name); e != nil && !errors.Is(e, os.ErrNotExist) {
			return count, ErrState
		}
		s.mu.Lock()
		for i := range s.state.Objects {
			if s.state.Objects[i].ID == id {
				s.state.Objects[i].State = "RETIRED"
				s.state.Objects[i].Bytes = "0"
				break
			}
		}
		if s.saveLocked() != nil {
			s.mu.Unlock()
			return count, ErrState
		}
		s.mu.Unlock()
		count++
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining := make([]entry, 0, len(s.state.Objects))
	for _, r := range s.state.Objects {
		until, _ := cipherstream.CanonicalTime(r.Descriptor.ManifestExpiry)
		if r.State == "RETIRED" && !now.Before(until) {
			delete(s.owners, r.ID)
			continue
		}
		remaining = append(remaining, r)
	}
	s.state.Objects = remaining
	if s.saveLocked() != nil {
		return count, ErrState
	}
	return count, nil
}
