package comfyworker

// SOURCE_ONLY, not compiled or executed during CODE_FIRST.
import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type stalledWriter struct{ writes int }

func (w *stalledWriter) Write(p []byte) (int, error) { w.writes++; return 0, nil }

type shortReader struct{ source *bytes.Reader }

func (r shortReader) Read(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return r.source.Read(p)
}

type headerOnly struct {
	header       *bytes.Reader
	payloadReads int
}

func (r *headerOnly) Read(p []byte) (int, error) {
	if r.header.Len() == 0 {
		r.payloadReads++
		return 0, errors.New("no payload permitted")
	}
	return r.header.Read(p)
}

type enteredWriter struct {
	writer  io.Writer
	entered chan struct{}
	first   bool
}

func (w *enteredWriter) Write(p []byte) (int, error) {
	if !w.first {
		w.first = true
		close(w.entered)
	}
	return w.writer.Write(p)
}

func TestComfyWorkerFramesBoundLengthBeforePayloadAndRejectInputKinds(t *testing.T) {
	for _, test := range []struct {
		size uint32
		kind Kind
	}{{0, Ready}, {65538, Ready}, {0xffffffff, Result}, {3, Invoke}, {3, Cancel}, {3, 108}} {
		var h [5]byte
		binary.LittleEndian.PutUint32(h[:4], test.size)
		h[4] = byte(test.kind)
		r := &headerOnly{header: bytes.NewReader(h[:])}
		if _, e := ReadWorkerFrame(context.Background(), r); e == nil || r.payloadReads != 0 {
			t.Fatal("invalid header reached private payload")
		}
	}
}
func TestComfyPrivateFramePartialReadsSingleUseAliasesAndRedaction(t *testing.T) {
	body := []byte(`{"key":"never-log-this-key","parameters":{"prompt":"secret original prompt"}}`)
	raw := binary.LittleEndian.AppendUint32(nil, uint32(len(body)+1))
	raw = append(raw, byte(Result))
	raw = append(raw, body...)
	f, e := ReadWorkerFrame(context.Background(), shortReader{bytes.NewReader(raw)})
	if e != nil || f.Kind() != Result {
		t.Fatal("private frame unavailable")
	}
	alias := f
	for _, v := range []any{f, &f} {
		encoded, _ := json.Marshal(v)
		if strings.Contains(fmt.Sprintf("%#v", v), "secret") || string(encoded) != `{"redacted":true}` {
			t.Fatal("private frame exposed")
		}
	}
	got, e := f.Take()
	if e != nil || !bytes.Equal(got, body) {
		t.Fatal("private frame changed")
	}
	clear(got)
	if _, e := alias.Take(); e == nil {
		t.Fatal("private frame alias reused")
	}
	f.Close()
	alias.Close()
}
func TestComfyPrivateFrameCloseRevokesAliasesAndRejectsTruncation(t *testing.T) {
	raw := []byte{3, 0, 0, 0, byte(Ready), 'a', 'b'}
	f, e := ReadWorkerFrame(context.Background(), bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	alias := f
	f.Close()
	if _, e := alias.Take(); e == nil {
		t.Fatal("closed private frame reused")
	}
	if _, e := ReadWorkerFrame(context.Background(), bytes.NewReader(raw[:len(raw)-1])); e == nil {
		t.Fatal("truncated private frame accepted")
	}
}
func TestComfyIndependentControlWriterNotBlockedByContentMutex(t *testing.T) {
	// The content writer is genuinely blocked on its own OS-style pipe while
	// the independently owned control writer must still make progress.
	read, write := io.Pipe()
	entered := make(chan struct{})
	done := make(chan error, 1)
	defer read.Close()
	defer write.Close()
	go func() {
		done <- WritePrivateFrame(context.Background(), &enteredWriter{writer: write, entered: entered}, Invoke, []byte(`{}`))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("content writer never entered")
	}
	var control bytes.Buffer
	body := []byte(`{"session_id":"session_original","local_generation":"19","invocation_id":"invoke_original"}`)
	if e := WritePrivateFrame(context.Background(), &control, Cancel, body); e != nil {
		t.Fatal("independent control write unavailable")
	}
	if control.Bytes()[4] != byte(Cancel) || !bytes.Equal(control.Bytes()[5:], body) {
		t.Fatal("original cancel changed")
	}
	read.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("closed content pipe reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("content pipe writer not joined")
	}
}
func TestComfyPrivateWriterRejectsEmptyOversizeCancelledAndWrongKinds(t *testing.T) {
	stalled := &stalledWriter{}
	if e := WritePrivateFrame(context.Background(), stalled, Invoke, []byte(`{}`)); e == nil || stalled.writes != 1 {
		t.Fatal("zero-progress private writer accepted")
	}
	for _, test := range []struct {
		kind Kind
		body []byte
	}{{Cancel, nil}, {Cancel, make([]byte, 4097)}, {AssetData, make([]byte, 65537)}, {Invoke, make([]byte, (2<<20)+1)}, {Ready, []byte(`{}`)}} {
		var w bytes.Buffer
		if e := WritePrivateFrame(context.Background(), &w, test.kind, test.body); e == nil || w.Len() != 0 {
			t.Fatal("invalid private body written")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := WritePrivateFrame(ctx, io.Discard, StopSession, []byte(`{}`)); e == nil {
		t.Fatal("cancelled private write succeeded")
	}
}
