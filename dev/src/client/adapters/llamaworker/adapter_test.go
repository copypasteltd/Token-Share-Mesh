package llamaworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

func textCase(profile relay.Profile, raw string) (Config, supervisor.Work, executionlease.Manifest, []byte) {
	c := Config{Environment: strings.Repeat("c", 64), Capability: "cap_native_text", Resource: "physical_gpu_0", Model: FilePin{SHA256: strings.Repeat("a", 64)}, Template: FilePin{SHA256: strings.Repeat("b", 64)}, ContextTokens: 2048, BatchTokens: 32, Threads: 1}
	w := supervisor.Work{RequestID: "original_request", Resource: "physical_gpu_0", LocalGeneration: "local_generation_1", Environment: c.Environment}
	b := []byte(raw)
	h := sha256.Sum256(b)
	m := executionlease.Manifest{Profile: executionlease.ManifestProfile, ID: "manifest_original", Resource: w.Resource, EnvironmentDigest: c.Environment, Capability: c.Capability, EngineKind: "LLAMA_CPP_NATIVE", EngineRelease: EngineRelease, ModelDigest: c.Model.SHA256, RecipeDigest: c.Template.SHA256, InputTokenLimit: 1024, InputBytes: int64(len(b)), InputDigest: hex.EncodeToString(h[:]), Mode: "PLAINTEXT", Privacy: "STANDARD", HardDeadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Spec: executionlease.Spec{Protocol: profile, Model: "public_model", MaxOutputTokens: 32, Stream: true}}
	return c, w, m, b
}

const chatRequest = `{"model":"public_model","messages":[{"role":"user","content":"sensitive fixture only"}],"max_tokens":32,"stream":true}`

func TestNativePipeThreeProfilesKeepTextAndRejectUnimplementedSemantics(t *testing.T) {
	for _, tc := range []struct {
		profile   relay.Profile
		raw, want string
	}{
		{relay.ChatText, chatRequest, "sensitive fixture only"},
		{relay.ResponsesText, `{"model":"public_model","input":"response text","instructions":"system rule","max_output_tokens":32,"store":false,"stream":true}`, "response text"},
		{relay.MessagesText, `{"model":"public_model","system":[{"type":"text","text":"system rule"}],"messages":[{"role":"user","content":[{"type":"text","text":"anthropic text"}]}],"max_tokens":32,"stream":true}`, "anthropic text"},
	} {
		c, w, m, raw := textCase(tc.profile, tc.raw)
		b, _, e := normalize(w, m, raw, c)
		if e != nil || !bytes.Contains(b, []byte(tc.want)) {
			t.Fatalf("profile %s: %v", tc.profile, e)
		}
		clear(b)
	}
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	for _, mutate := range []func(*executionlease.Manifest){func(m *executionlease.Manifest) { m.Resource = "another_gpu" }, func(m *executionlease.Manifest) { m.EngineKind = "LM_STUDIO" }, func(m *executionlease.Manifest) { m.Privacy = "LP" }, func(m *executionlease.Manifest) { m.InputBytes++ }, func(m *executionlease.Manifest) { m.Spec.MaxOutputTokens++ }, func(m *executionlease.Manifest) {
		m.HardDeadline = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	}} {
		copy := m
		mutate(&copy)
		if b, _, e := normalize(w, copy, raw, c); e == nil {
			clear(b)
			t.Fatal("mismatched authority admitted")
		}
	}
	other := w
	other.Resource = "another_physical_gpu"
	companion := m
	companion.Resource = other.Resource
	if b, _, e := normalize(other, companion, raw, c); e == nil {
		clear(b)
		t.Fatal("companion manifest selected an unapproved physical resource")
	}

	for _, raw := range []string{strings.Replace(chatRequest, `"max_tokens":32`, `"max_tokens":32,"tools":[]`, 1), strings.Replace(chatRequest, `"max_tokens":32`, `"max_tokens":32,"top_p":0`, 1), strings.Replace(chatRequest, "sensitive fixture only", `has\u0000nul`, 1)} {
		c, w, m, b := textCase(relay.ChatText, raw)
		if out, _, e := normalize(w, m, b, c); e == nil {
			clear(out)
			t.Fatal("unsupported native semantics silently accepted")
		}
	}
}

func TestNativePipeFramesBoundLengthBeforeAllocation(t *testing.T) {
	for _, n := range []uint32{0, maxFrame + 1, 0xffffffff} {
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], n)
		if _, _, e := readFrame(bytes.NewReader(h[:])); e == nil {
			t.Fatal("unbounded frame accepted")
		}
	}
	if _, _, e := readFrame(bytes.NewReader([]byte{0, 0, 0, 3, 1, 2})); e == nil {
		t.Fatal("truncated frame accepted")
	}
	var b bytes.Buffer
	if writeFrame(&b, kData, make([]byte, maxFrame)) == nil {
		t.Fatal("oversize data accepted")
	}
	if e := writeFrame(&b, kData, []byte("ok")); e != nil {
		t.Fatal(e)
	}
	kind, body, e := readFrame(&b)
	if e != nil || kind != kData || string(body) != "ok" {
		t.Fatal("bounded round trip failed")
	}
}

type syntheticPipe struct {
	input   *io.PipeReader
	output  *io.PipeWriter
	control chan string
	mu      sync.Mutex
	quit    func()
}

func synthetic(t *testing.T, c Config) (*Adapter, *syntheticPipe) {
	t.Helper()
	ir, iw := io.Pipe()
	or, ow := io.Pipe()
	cr, cw := io.Pipe()
	wait := make(chan error, 1)
	var once sync.Once
	quit := func() {
		once.Do(func() {
			ir.Close()
			iw.Close()
			or.Close()
			ow.Close()
			cr.Close()
			cw.Close()
			wait <- nil
		})
	}
	p := &syntheticPipe{input: ir, output: ow, control: make(chan string, 4), quit: quit}
	a := newAdapter(c, child{iw, cw, or, quit, wait})
	go func() {
		for {
			kind, b, e := readFrame(cr)
			if e != nil {
				return
			}
			d := decoder{raw: b}
			id := d.str(256)
			if kind == kCancel && d.end() {
				p.control <- id
			}
			clear(b)
		}
	}()
	go func() { var b encoder; b.str(Profile); b.str(UpstreamCommit); p.frame(kReady, b.Bytes()) }()
	select {
	case e := <-a.ready:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("synthetic pipe not ready")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	return a, p
}
func (p *syntheticPipe) frame(kind byte, b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = writeFrame(p.output, kind, b)
}
func (p *syntheticPipe) request(t *testing.T) (string, []byte) {
	t.Helper()
	kind, b, e := readFrame(p.input)
	if e != nil || kind != kBegin {
		t.Fatal("no original begin")
	}
	d := decoder{raw: b}
	id := d.str(256)
	d.take(8)
	size := d.u32()
	if !d.end() {
		t.Fatal("bad begin")
	}
	clear(b)
	var body []byte
	for len(body) < int(size) {
		kind, b, e = readFrame(p.input)
		if e != nil || kind != kData {
			t.Fatal("missing bounded data")
		}
		body = append(body, b...)
		clear(b)
	}
	kind, b, e = readFrame(p.input)
	if e != nil || kind != kEnd || len(b) != 0 {
		t.Fatal("missing body end")
	}
	var started encoder
	started.str(id)
	started.u32(12)
	p.frame(kStarted, started.Bytes())
	return id, body
}
func (p *syntheticPipe) done(id string, reason uint32) {
	var b encoder
	b.str(id)
	b.u32(12)
	b.u32(1)
	b.u32(reason)
	b.u32(1)
	b.u32(supervisor.NoStopIndex)
	p.frame(kDone, b.Bytes())
}
func awaitState(t *testing.T, a *Adapter, w supervisor.Work, state string) supervisor.Observation {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		o, _ := a.Query(context.Background(), w)
		if o.State == state {
			return o
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected native state %s", state)
	return supervisor.Observation{}
}

func TestNativePipeOriginalOwnerOnceAndCleanupReceipt(t *testing.T) {
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	a, p := synthetic(t, c)
	o, e := a.SubmitInput(context.Background(), w, m, raw)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.SubmitInput(context.Background(), w, m, raw); e == nil {
		t.Fatal("duplicate submission")
	}
	id, body := p.request(t)
	if id != o.BackendID || !bytes.Contains(body, []byte("sensitive fixture only")) {
		t.Fatal("original input lost")
	}
	clear(body)
	if _, e = a.Cleanup(context.Background(), w, o); e == nil {
		t.Fatal("running cleanup released owner")
	}
	wrong := w
	wrong.LocalGeneration = "stale_generation"
	if _, e = a.Query(context.Background(), wrong); e == nil {
		t.Fatal("cross generation query")
	}
	var chunk encoder
	chunk.str(id)
	chunk.u32(0)
	chunk.WriteString("reply")
	p.frame(kOutput, chunk.Bytes())
	got, e := a.Read(context.Background(), w)
	if e != nil || string(got) != "reply" {
		t.Fatal("output handoff failed")
	}
	clear(got)
	p.done(id, 1)
	o = awaitState(t, a, w, "STOPPED")
	clean, e := a.Cleanup(context.Background(), w, o)
	if e != nil || !clean.WorkStopped || !clean.TokensRevoked || !clean.WorkspaceCleared || clean.ManifestHash != m.Digest() || clean.LocalGeneration != w.LocalGeneration {
		t.Fatal("original cleanup proof lost")
	}
	if _, e = a.SubmitInput(context.Background(), w, m, raw); e == nil {
		t.Fatal("cleaned request resubmitted")
	}
}

func TestNativePipeCancelIndependentOfBlockedContent(t *testing.T) {
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	a, p := synthetic(t, c)
	o, e := a.SubmitInput(context.Background(), w, m, raw)
	if e != nil {
		t.Fatal(e)
	}
	// Nobody reads the work pipe yet: the writer is blocked on BEGIN.
	if e = a.Cancel(context.Background(), w); e != nil {
		t.Fatal(e)
	}
	select {
	case id := <-p.control:
		if id != o.BackendID {
			t.Fatal("cancel rebound")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked behind content")
	}
	id, b := p.request(t)
	clear(b)
	p.done(id, 1) // Native completion raced a parent cancel; it must not undo it.
	o = awaitState(t, a, w, "STOPPED")
	if _, e = a.Result(w); e != context.Canceled {
		t.Fatal("native completion undid parent revocation")
	}
	if _, e = a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
}

func TestNativePipeExitKeepsAmbiguousOwner(t *testing.T) {
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	a, p := synthetic(t, c)
	if _, e := a.SubmitInput(context.Background(), w, m, raw); e != nil {
		t.Fatal(e)
	}
	_, b := p.request(t)
	clear(b)
	p.quit()
	o := awaitState(t, a, w, "UNKNOWN")
	if _, e := a.Cleanup(context.Background(), w, o); e == nil {
		t.Fatal("exit fabricated stop proof")
	}
	if _, e := a.SubmitInput(context.Background(), w, m, raw); e == nil {
		t.Fatal("ambiguous work retried")
	}
}

func TestNativePipeSlowOutputCancelsWithinFixedBuffer(t *testing.T) {
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	a, p := synthetic(t, c)
	if _, e := a.SubmitInput(context.Background(), w, m, raw); e != nil {
		t.Fatal(e)
	}
	id, b := p.request(t)
	clear(b)
	for i := uint32(0); i < 9; i++ {
		var frame encoder
		frame.str(id)
		frame.u32(i)
		frame.WriteString(strings.Repeat("x", 16384))
		p.frame(kOutput, frame.Bytes())
	}
	select {
	case <-p.control:
	case <-time.After(time.Second):
		t.Fatal("slow consumer never cancelled")
	}
	p.done(id, 4)
	o := awaitState(t, a, w, "STOPPED")
	if _, e := a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Read(context.Background(), w); e != ErrOutput {
		t.Fatal("discarded output reported successful")
	}
}

func TestNativePipeStaleCallbackCannotReleaseNextOwner(t *testing.T) {
	c, w, m, raw := textCase(relay.ChatText, chatRequest)
	a, p := synthetic(t, c)
	if _, e := a.SubmitInput(context.Background(), w, m, raw); e != nil {
		t.Fatal(e)
	}
	old, b := p.request(t)
	clear(b)
	p.done(old, 1)
	o := awaitState(t, a, w, "STOPPED")
	if _, e := a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
	w.RequestID = "next_original_request"
	w.LocalGeneration = "new_generation"
	if _, e := a.SubmitInput(context.Background(), w, m, raw); e != nil {
		t.Fatal(e)
	}
	_, b = p.request(t)
	clear(b)
	p.done(old, 1)
	o = awaitState(t, a, w, "UNKNOWN")
	if _, e := a.Cleanup(context.Background(), w, o); e == nil {
		t.Fatal("old native receipt released new owner")
	}
}

func TestNativePipeRejectsMismatchedStartAndStopMetadata(t *testing.T) {
	for _, attack := range []string{"input_count", "unknown_stop", "unexpected_stop"} {
		t.Run(attack, func(t *testing.T) {
			c, w, m, raw := textCase(relay.ChatText, chatRequest)
			a, p := synthetic(t, c)
			if _, e := a.SubmitInput(context.Background(), w, m, raw); e != nil {
				t.Fatal(e)
			}
			id, body := p.request(t)
			clear(body)
			var end encoder
			end.str(id)
			input, reason, index := uint32(12), uint32(1), supervisor.NoStopIndex
			switch attack {
			case "input_count":
				input++
			case "unknown_stop":
				reason = 3
				index = 0
			case "unexpected_stop":
				index = 0
			}
			end.u32(input)
			end.u32(1)
			end.u32(reason)
			end.u32(1)
			end.u32(index)
			p.frame(kDone, end.Bytes())
			o := awaitState(t, a, w, "UNKNOWN")
			if _, e := a.Cleanup(context.Background(), w, o); e == nil {
				t.Fatal("inconsistent native metadata released original")
			}
		})
	}
}

func TestNativePackPinsPathsAndCompleteLibraryAllowlist(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pack")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	makePin := func(name, text string) FilePin {
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256([]byte(text))
		return FilePin{p, hex.EncodeToString(h[:])}
	}
	c, _, _, _ := textCase(relay.ChatText, chatRequest)
	c.RuntimeRoot = root
	c.Executable = makePin("worker.exe", "not an executable")
	c.Model = makePin("one.gguf", "not a model")
	c.Template = makePin("template.txt", "chatml")
	lib := makePin("llama.dll", "synthetic library bytes")
	c.Libraries = []FilePin{lib}
	c.Library = lib.Path
	c.BackendRegistry = lib.Path
	if _, e := c.validate(context.Background()); e != nil {
		t.Fatal(e)
	}
	_ = makePin("unapproved.dll", "unapproved")
	if _, e := c.validate(context.Background()); e == nil {
		t.Fatal("unapproved native library accepted")
	}
	_ = os.Remove(filepath.Join(dir, "unapproved.dll"))
	if e := os.WriteFile(lib.Path, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := c.validate(context.Background()); e == nil {
		t.Fatal("changed native library accepted")
	}
	copy := c
	copy.Model.Path = filepath.Join(root, "..", "outside.gguf")
	if _, e := copy.validate(context.Background()); e == nil {
		t.Fatal("outside runtime accepted")
	}
}
