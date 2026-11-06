package results

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"unicode/utf8"

	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/resultwire"
)

type ImportSource interface {
	Read(context.Context) ([]byte, error)
}
type ImportSpec struct {
	Descriptor  supervisor.TextDescriptor
	Start       supervisor.TextStart
	Result      supervisor.TextResult
	Stops       []string
	OutputHash  string
	OutputBytes int64
}

// Imported is a content capability only, with no execution or acceptance API.
// The existing encrypted quota/retention store owns every ciphertext and key.
type Imported struct{ record *record }

func (i *Imported) Open(ctx context.Context) (supervisor.TextOutput, error) {
	return i.record.open(ctx)
}
func (i *Imported) Discard() error { i.record.revoke(); return i.record.destroy() }

// Import exposes no output until exact length, UTF-8, digest and terminal have
// all been checked and the authenticated encrypted file has been synced.
func (s *Store) Import(ctx context.Context, spec ImportSpec, source ImportSource) (*Imported, error) {
	if source == nil || spec.OutputBytes < 0 || spec.OutputBytes > resultwire.MaximumContentBytes || spec.Start.InputTokens == 0 || spec.Start.InputTokens != spec.Result.InputTokens || int64(spec.Start.InputTokens) > spec.Descriptor.Manifest.InputTokenLimit || int64(spec.Result.OutputTokens) > int64(spec.Descriptor.Manifest.Spec.MaxOutputTokens) || len(spec.OutputHash) != 64 || (spec.Result.Reason != "eog" && spec.Result.Reason != "limit" && spec.Result.Reason != "stop") || spec.Result.Reason == "stop" && int(spec.Result.StopIndex) >= len(spec.Stops) || spec.Result.Reason != "stop" && spec.Result.StopIndex != supervisor.NoStopIndex {
		return nil, ErrBinding
	}
	r, e := s.beginDescriptor(spec.Descriptor.Work, spec.Descriptor.Manifest, options{IncludeUsage: spec.Descriptor.IncludeUsage, Stops: append([]string(nil), spec.Stops...)}, nil, &spec.Descriptor)
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			r.complete(ErrUnavailable)
			r.revoke()
			_ = r.destroy()
		}
	}()
	var started [4]byte
	binary.BigEndian.PutUint32(started[:], spec.Start.InputTokens)
	if e = r.append(kStarted, started[:]); e != nil {
		return nil, e
	}
	sum := sha256.New()
	var total int64
	var tail []byte
	defer func() { clear(tail) }()
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		b, re := source.Read(ctx)
		if len(b) > maxPiece || len(b) == 0 && re == nil {
			clear(b)
			return nil, ErrBinding
		}
		if len(b) > 0 {
			total += int64(len(b))
			if total > spec.OutputBytes {
				clear(b)
				return nil, ErrBinding
			}
			_, _ = sum.Write(b)
			joined := append(tail, b...)
			tail = nil
			at := 0
			for at < len(joined) && utf8.FullRune(joined[at:]) {
				runeValue, n := utf8.DecodeRune(joined[at:])
				if runeValue == utf8.RuneError && n == 1 {
					clear(b)
					clear(joined)
					return nil, ErrBinding
				}
				at += n
			}
			tail = append([]byte(nil), joined[at:]...)
			clear(joined)
			if e = r.append(kText, b); e != nil {
				clear(b)
				return nil, e
			}
			clear(b)
		}
		if re != nil {
			if re != io.EOF {
				return nil, re
			}
			break
		}
	}
	if total != spec.OutputBytes || len(tail) != 0 || hex.EncodeToString(sum.Sum(nil)) != spec.OutputHash {
		return nil, ErrBinding
	}
	terminal := encodeResult(spec.Result, uint64(total), sum.Sum(nil))
	e = r.append(kFinal, terminal)
	clear(terminal)
	if e != nil {
		return nil, e
	}
	r.mu.Lock()
	e = r.file.Sync()
	r.mu.Unlock()
	if e != nil {
		return nil, ErrUnavailable
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.complete(nil)
	success = true
	return &Imported{record: r}, nil
}
