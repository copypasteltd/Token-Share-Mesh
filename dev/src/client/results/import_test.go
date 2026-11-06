package results

// SOURCE_ONLY: receiver cache definitions, not executed runtime evidence.
import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/resultwire"
)

type piecesSource struct {
	pieces [][]byte
	empty  bool
}

func (s *piecesSource) Read(context.Context) ([]byte, error) {
	if s.empty {
		return nil, nil
	}
	if len(s.pieces) == 0 {
		return nil, io.EOF
	}
	b := bytes.Clone(s.pieces[0])
	s.pieces = s.pieces[1:]
	return b, nil
}
func importSpec(text []byte) ImportSpec {
	w, m := scope("readonly_import")
	return ImportSpec{Descriptor: supervisor.TextDescriptor{Work: w, Manifest: m, ResultID: "result_original", CreatedUnix: time.Now().Unix()}, Start: supervisor.TextStart{InputTokens: 3}, Result: supervisor.TextResult{InputTokens: 3, OutputTokens: 2, Reason: "eog", StopIndex: supervisor.NoStopIndex}, OutputHash: resultwire.BytesDigest(text), OutputBytes: int64(len(text))}
}
func TestImportVerifiesBeforeExposingAndStoresOnlyCiphertext(t *testing.T) {
	s := store(t, nil)
	text := []byte("private original result with enough bytes to scan")
	v, e := s.Import(context.Background(), importSpec(text), &piecesSource{pieces: [][]byte{text}})
	if e != nil {
		t.Fatal(e)
	}
	out, e := v.Open(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	got, e := drain(t, out)
	_ = out.Close(context.Background())
	if e != nil || got != string(text) {
		t.Fatal("original import changed", e)
	}
	entries, e := os.ReadDir(s.c.Root)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(s.c.Root, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(raw, text) {
			t.Fatal("plain output entered cache")
		}
	}
	if e = v.Discard(); e != nil {
		t.Fatal(e)
	}
	if _, e = v.Open(context.Background()); e == nil {
		t.Fatal("discarded content reopened")
	}
}
func TestImportTruncationOrWrongDigestReturnsNoCapability(t *testing.T) {
	for _, change := range []func(*ImportSpec){func(s *ImportSpec) { s.OutputBytes++ }, func(s *ImportSpec) { s.OutputHash = resultwire.BytesDigest([]byte("wrong")) }} {
		s := store(t, nil)
		spec := importSpec([]byte("text"))
		change(&spec)
		if v, e := s.Import(context.Background(), spec, &piecesSource{pieces: [][]byte{[]byte("text")}}); e == nil || v != nil {
			t.Fatal("partial output exposed")
		}
	}
}
func TestImportValidatesUTF8AcrossChunkBoundary(t *testing.T) {
	s := store(t, nil)
	text := []byte("原结果")
	v, e := s.Import(context.Background(), importSpec(text), &piecesSource{pieces: [][]byte{text[:1], text[1:4], text[4:]}})
	if e != nil || v == nil {
		t.Fatal("split UTF8 lost", e)
	}
	_ = v.Discard()
	invalid := []byte{0xe4, 0x80}
	if v, e = s.Import(context.Background(), importSpec(invalid), &piecesSource{pieces: [][]byte{invalid}}); e == nil || v != nil {
		t.Fatal("incomplete UTF8 exposed")
	}
}
func TestImportCancellationAndNonProgressNeverIssueCapability(t *testing.T) {
	s := store(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v, e := s.Import(ctx, importSpec([]byte("text")), &piecesSource{pieces: [][]byte{[]byte("text")}}); e == nil || v != nil {
		t.Fatal("canceled import exposed")
	}
	if v, e := s.Import(context.Background(), importSpec([]byte("text")), &piecesSource{empty: true}); e == nil || v != nil {
		t.Fatal("nonprogress source accepted")
	}
}
func TestImportCannotInventCountsOrOverflowOutputLimit(t *testing.T) {
	for _, change := range []func(*ImportSpec){func(s *ImportSpec) { s.Result.InputTokens++ }, func(s *ImportSpec) { s.Result.OutputTokens = ^uint32(0) }, func(s *ImportSpec) { s.Descriptor.Manifest.Spec.MaxOutputTokens = -1 }, func(s *ImportSpec) { s.OutputBytes = 33 << 20 }} {
		s := store(t, nil)
		spec := importSpec([]byte("text"))
		change(&spec)
		if v, e := s.Import(context.Background(), spec, &piecesSource{pieces: [][]byte{[]byte("text")}}); e == nil || v != nil {
			t.Fatal("invalid original usage exposed")
		}
	}
}
