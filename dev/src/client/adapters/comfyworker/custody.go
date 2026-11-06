package comfyworker

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/mediaworkspace"
	"p11.local/compute/client/protocol/rentalaccess"
)

// MediaCustody holds a private RESULT key. It does not confirm buyer delivery,
// authenticated network END, usage, money or physical cleanup. Closing a
// receipt/reader alone never releases an original rental resource.
type MediaCustody struct{ state *custodyState }
type custodyState struct {
	mu             sync.Mutex
	ledger         *execution.Store
	hold           execution.Hold
	expected       rentalaccess.Expected
	workspace      mediaworkspace.Workspace
	scope          Scope
	receipt        Receipt
	reader         *MediaReader
	opened, closed bool
}

func (c MediaCustody) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private media result custody redacted]")
}
func (c MediaCustody) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// Called exclusively with an original owned process's private RESULT frame.
// Callers may not bind a worker-selected scope, path, grant or media policy.
func newMediaCustody(raw []byte, kind string, ledger *execution.Store, h execution.Hold, expected rentalaccess.Expected, w mediaworkspace.Workspace) (MediaCustody, error) {
	record := w.Record()
	if ledger == nil || record.Phase != "ACTIVE" || record.Session != h.Owner() || record.Resource != h.Resource() || record.LocalGeneration != h.LocalGeneration() || record.Grant != expected.Grant || record.GrantDigest != expected.TokenDigest || record.ManifestHash != expected.ManifestHash || record.EnvironmentHash != expected.EnvironmentHash {
		return MediaCustody{}, ErrOutput
	}
	scope := Scope{Session: record.Session, Invocation: record.Invocation, LocalGeneration: record.LocalGeneration, Environment: record.EnvironmentHash, Recipe: record.RecipeHash}
	receipt, e := DecodeReceipt(raw, scope, kind)
	if e != nil {
		return MediaCustody{}, ErrOutput
	}
	return MediaCustody{state: &custodyState{ledger: ledger, hold: h, expected: expected, workspace: w, scope: scope, receipt: receipt}}, nil
}

// Open is one-shot, including on failure. Read/seek recheck the SAME grant and
// original physical owner. The lease owns initialization and actual reader.
func (c MediaCustody) Open(ctx context.Context) (*MediaReader, error) {
	if c.state == nil || ctx == nil {
		return nil, ErrOutput
	}
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.opened {
		return nil, ErrOutput
	}
	s.opened = true
	if _, e := s.ledger.CheckRentalExpected(ctx, s.hold, s.expected, "EXPORT_SELF"); e != nil {
		s.receipt.Close()
		return nil, ErrOutput
	}
	lease, e := s.workspace.AcquireRead(ctx)
	if e != nil {
		s.receipt.Close()
		return nil, ErrOutput
	}
	var reader *Reader
	e = lease.Open(func(root *os.Root) (io.Closer, error) {
		var e error
		reader, e = OpenCipher(ctx, root, s.scope, &s.receipt)
		if e != nil {
			return nil, e
		}
		return reader, nil
	})
	if e != nil {
		lease.Close()
		s.receipt.Close()
		return nil, ErrOutput
	}
	result := &MediaReader{state: &mediaReaderState{reader: reader, lease: lease, ledger: s.ledger, hold: s.hold, expected: s.expected}}
	s.reader = result
	return result, nil
}
func (c MediaCustody) Close() {
	if c.state == nil {
		return
	}
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.receipt.Close()
	if s.reader != nil {
		_ = s.reader.Close()
		s.reader = nil
	}
}

type MediaReader struct{ state *mediaReaderState }
type mediaReaderState struct {
	mu       sync.Mutex
	reader   *Reader
	lease    *mediaworkspace.ReadLease
	ledger   *execution.Store
	hold     execution.Hold
	expected rentalaccess.Expected
	closed   bool
}

func (r MediaReader) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[original media result reader redacted]")
}
func (r MediaReader) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (r *MediaReader) Read(ctx context.Context, dst []byte) (int, error) {
	if r == nil || r.state == nil || ctx == nil {
		return 0, ErrOutput
	}
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrOutput
	}
	if _, e := s.ledger.CheckRentalExpected(ctx, s.hold, s.expected, "EXPORT_SELF"); e != nil {
		return 0, ErrOutput
	}
	n, e := s.reader.ReadContext(ctx, dst)
	if ctx.Err() != nil {
		clear(dst[:n])
		return 0, ErrOutput
	}
	return n, e
}
func (r *MediaReader) Seek(ctx context.Context, offset int64, whence int) (int64, error) {
	if r == nil || r.state == nil || ctx == nil {
		return 0, ErrOutput
	}
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrOutput
	}
	if _, e := s.ledger.CheckRentalExpected(ctx, s.hold, s.expected, "EXPORT_SELF"); e != nil {
		return 0, ErrOutput
	}
	return s.reader.Seek(offset, whence)
}
func (r *MediaReader) Close() error {
	if r == nil || r.state == nil {
		return nil
	}
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lease.Close()
}
