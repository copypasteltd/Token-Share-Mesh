//go:build windows && p11_native_capi_fixture

package llamaworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"p11.local/compute/client/protocol/textoutput"
	"p11.local/compute/client/results"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

func capiPack(t *testing.T) Config {
	t.Helper()
	root := os.Getenv("P11_NATIVE_RUNTIME_ROOT")
	exe := os.Getenv("P11_NATIVE_WORKER_EXE")
	dll := os.Getenv("P11_NATIVE_FIXTURE_DLL")
	if root == "" || exe == "" || dll == "" {
		t.Fatal("explicit synthetic native fixture tool required")
	}
	dir := t.TempDir()
	pin := func(path string) FilePin {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		clear(b)
		return FilePin{path, hex.EncodeToString(h[:])}
	}
	copyDLL := filepath.Join(dir, "llama-fixture.dll")
	b, e := os.ReadFile(dll)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(copyDLL, b, 0600); e != nil {
		t.Fatal(e)
	}
	clear(b)
	model := filepath.Join(dir, "synthetic-only.gguf")
	template := filepath.Join(dir, "template.txt")
	if e = os.WriteFile(model, []byte("SYNTHETIC_C_ABI_NO_MODEL_NO_GPU"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(template, []byte("chatml"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(model+".audit.allow-clear", nil, 0600); e != nil {
		t.Fatal(e)
	}
	c, _, _, _ := textCase(relay.ChatText, chatRequest)
	c.RuntimeRoot = root
	c.Executable = pin(exe)
	c.Model = pin(model)
	c.Template = pin(template)
	lib := pin(copyDLL)
	c.Libraries = []FilePin{lib}
	c.Library = copyDLL
	c.BackendRegistry = copyDLL
	return c
}
func capiOpen(t *testing.T, c Config) *Adapter {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a, e := Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Close(ctx)
		select {
		case <-a.exited:
		case <-ctx.Done():
			t.Error("owned synthetic native process exit not observed")
		}
	})
	return a
}
func capiInput(c Config, raw string) (supervisor.Work, executionlease.Manifest, []byte) {
	_, w, m, b := textCase(relay.ChatText, raw)
	w.Environment = c.Environment
	m.EnvironmentDigest = c.Environment
	m.ModelDigest = c.Model.SHA256
	m.RecipeDigest = c.Template.SHA256
	return w, m, b
}
func capiState(t *testing.T, a *Adapter, w supervisor.Work, state string) supervisor.Observation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if ctx.Err() != nil {
			t.Fatalf("native C ABI did not reach %s", state)
		}
		o, _ := a.Query(ctx, w)
		if state == "UNKNOWN" && o.State == "STOPPED" {
			t.Fatal("native failure manufactured STOPPED")
		}
		if o.State == state {
			return o
		}
		time.Sleep(2 * time.Millisecond)
	}
}
func capiAudit(t *testing.T, c Config, stage string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(c.Model.Path + ".audit")
		if strings.Contains(string(b), stage) {
			return string(b)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("synthetic native audit did not reach %s", stage)
	return ""
}

func TestNativeCAPISyntheticRoundTripStopAndModelReuse(t *testing.T) {
	c := capiPack(t)
	a := capiOpen(t, c)
	for _, id := range []string{"first_native_original", "second_native_original"} {
		raw := strings.Replace(chatRequest, `"max_tokens":32`, `"max_tokens":32,"stop":"<END>"`, 1)
		w, m, b := capiInput(c, raw)
		w.RequestID = id
		if _, e := a.SubmitInput(context.Background(), w, m, b); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		var out strings.Builder
		for {
			piece, e := a.Read(ctx, w)
			out.Write(piece)
			clear(piece)
			if e == io.EOF {
				break
			}
			if e != nil {
				cancel()
				t.Fatal(e)
			}
		}
		cancel()
		if out.String() != "hello" {
			t.Fatal("stop string or following content leaked")
		}
		o := capiState(t, a, w, "STOPPED")
		r, e := a.Result(w)
		if e != nil || r.Reason != "stop" || r.StopIndex != 0 || r.InputTokens == 0 || r.OutputTokens != 10 {
			t.Fatal("synthetic tokenizer/sample counters mismatch")
		}
		if _, e = a.Cleanup(context.Background(), w, o); e != nil {
			t.Fatal(e)
		}
	}
	audit := capiAudit(t, c, "FREE_SYNTHETIC")
	if strings.Count(audit, "MODEL_LOAD_SYNTHETIC") != 1 || strings.Count(audit, "FREE_SYNTHETIC") != 2 || strings.Contains(audit, "sensitive") {
		t.Fatal("model reloaded or content logged")
	}
}

type capiResultSink struct{ bytes.Buffer }

func (s *capiResultSink) Write(ctx context.Context, b []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, e := s.Buffer.Write(b)
	return e
}

func capiProtocolText(t *testing.T, p relay.Profile, stream bool, raw string) string {
	t.Helper()
	var text strings.Builder
	decode := func(raw string) map[string]any {
		var v map[string]any
		if json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal("invalid native protocol JSON")
		}
		return v
	}
	if stream {
		for _, line := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
				continue
			}
			v := decode(strings.TrimPrefix(line, "data: "))
			if p == relay.ChatText {
				if choices, ok := v["choices"].([]any); ok && len(choices) == 1 {
					choice, _ := choices[0].(map[string]any)
					delta, _ := choice["delta"].(map[string]any)
					if s, ok := delta["content"].(string); ok {
						text.WriteString(s)
					}
				}
			}
			if p == relay.ResponsesText && v["type"] == "response.output_text.delta" {
				s, _ := v["delta"].(string)
				text.WriteString(s)
			}
			if p == relay.MessagesText && v["type"] == "content_block_delta" {
				delta, _ := v["delta"].(map[string]any)
				s, _ := delta["text"].(string)
				text.WriteString(s)
			}
		}
		return text.String()
	}
	v := decode(raw)
	switch p {
	case relay.ChatText:
		choices, _ := v["choices"].([]any)
		if len(choices) != 1 {
			t.Fatal("missing native choice")
		}
		choice, _ := choices[0].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		s, _ := message["content"].(string)
		return s
	case relay.ResponsesText:
		items, _ := v["output"].([]any)
		if len(items) != 1 {
			t.Fatal("missing native item")
		}
		v, _ = items[0].(map[string]any)
	}
	content, _ := v["content"].([]any)
	if len(content) != 1 {
		t.Fatal("missing native text content")
	}
	part, _ := content[0].(map[string]any)
	s, _ := part["text"].(string)
	return s
}

func TestNativeCAPIEncryptedCustodyAndThreeProtocolDelivery(t *testing.T) {
	c := capiPack(t)
	a := capiOpen(t, c)
	spool, e := results.Open(results.DefaultConfig(t.TempDir()))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := spool.Close(ctx); e != nil {
			t.Error(e)
		}
	}()
	custodian, e := results.New(a, spool)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		for _, stream := range []bool{false, true} {
			raw := `{"model":"public_model","messages":[{"role":"user","content":"synthetic input"}],"max_tokens":32,"stop":["unused","<END>"],"stream":STREAM}`
			if p == relay.ResponsesText {
				raw = `{"model":"public_model","input":"synthetic input","max_output_tokens":32,"store":false,"stream":STREAM}`
			}
			if p == relay.MessagesText {
				raw = `{"model":"public_model","messages":[{"role":"user","content":"synthetic input"}],"max_tokens":32,"stop_sequences":["unused","<END>"],"stream":STREAM}`
			}
			flag := "false"
			if stream {
				flag = "true"
			}
			raw = strings.Replace(raw, "STREAM", flag, 1)
			w, m, b := capiInput(c, raw)
			w.RequestID = "custody_" + string(p) + "_" + flag
			w.LocalGeneration = w.RequestID
			m.Spec.Protocol = p
			m.Spec.Stream = stream
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if _, e = custodian.SubmitInput(ctx, w, m, b); e != nil {
				cancel()
				t.Fatal(e)
			}
			o := capiState(t, a, w, "STOPPED")
			if _, e = custodian.Cleanup(ctx, w, o); e != nil {
				cancel()
				t.Fatal(e)
			}
			out, e := custodian.Output(ctx, w)
			if e != nil {
				cancel()
				t.Fatal(e)
			}
			sink := &capiResultSink{}
			receipt, e := textoutput.Render(ctx, sink, out, textoutput.DefaultLimits())
			cancel()
			want := "hello"
			if p == relay.ResponsesText {
				want = "hello<END>hidden"
			}
			if e != nil || receipt.Terminal != "COMPLETED" || capiProtocolText(t, p, stream, sink.String()) != want || receipt.InputTokens == 0 || receipt.OutputTokens == 0 {
				t.Fatal("actual native -> ciphertext -> protocol failed", e)
			}
			if p == relay.MessagesText && (!strings.Contains(sink.String(), "stop_sequence") || !strings.Contains(sink.String(), "<END>") && !strings.Contains(sink.String(), `\u003cEND\u003e`)) {
				t.Fatal("matched original second stop string lost")
			}
			clear(b)
		}
	}
}

func TestNativeCAPITokenizerRejectsBeforeDecode(t *testing.T) {
	c := capiPack(t)
	a := capiOpen(t, c)
	w, m, b := capiInput(c, chatRequest)
	m.InputTokenLimit = 1
	if _, e := a.SubmitInput(context.Background(), w, m, b); e != nil {
		t.Fatal(e)
	}
	o := capiState(t, a, w, "STOPPED")
	audit := capiAudit(t, c, "TOKENIZE_SYNTHETIC")
	if strings.Contains(audit, "CONTEXT_SYNTHETIC") || strings.Contains(audit, "DECODE_SYNTHETIC") {
		t.Fatal("oversize prompt reached native decode")
	}
	if _, e := a.Result(w); e != ErrInput {
		t.Fatal("tokenizer rejection hidden")
	}
	if _, e := a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
}

func TestNativeCAPICancelRequiresSynchronizationAndClear(t *testing.T) {
	c := capiPack(t)
	if e := os.Remove(c.Model.Path + ".audit.allow-clear"); e != nil {
		t.Fatal(e)
	}
	a := capiOpen(t, c)
	w, m, b := capiInput(c, strings.Replace(chatRequest, "sensitive fixture only", "WAIT_NATIVE_FIXTURE", 1))
	if _, e := a.SubmitInput(context.Background(), w, m, b); e != nil {
		t.Fatal(e)
	}
	capiAudit(t, c, "DECODE_SYNTHETIC")
	if e := a.Cancel(context.Background(), w); e != nil {
		t.Fatal(e)
	}
	capiAudit(t, c, "SYNCHRONIZE_SYNTHETIC")
	o, e := a.Query(context.Background(), w)
	if e != nil || o.State != "RUNNING" {
		t.Fatal("cancel acknowledgement became stop proof")
	}
	if _, e = a.Cleanup(context.Background(), w, o); e == nil {
		t.Fatal("unsynchronized native work cleaned")
	}
	if e = os.WriteFile(c.Model.Path+".audit.allow-clear", nil, 0600); e != nil {
		t.Fatal(e)
	}
	o = capiState(t, a, w, "STOPPED")
	audit := capiAudit(t, c, "FREE_SYNTHETIC")
	if strings.Index(audit, "SYNCHRONIZE_SYNTHETIC") > strings.Index(audit, "CLEAR_SYNTHETIC") || strings.Index(audit, "CLEAR_SYNTHETIC") > strings.Index(audit, "FREE_SYNTHETIC") {
		t.Fatal("native cleanup order invalid")
	}
	if _, e = a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
}

func TestNativeCAPIUnexpectedExitNeverReleasesAmbiguousOwner(t *testing.T) {
	for _, marker := range []string{"CRASH_NATIVE_FIXTURE", "CONTEXT_NULL_FIXTURE"} {
		t.Run(marker, func(t *testing.T) {
			c := capiPack(t)
			a := capiOpen(t, c)
			w, m, b := capiInput(c, strings.Replace(chatRequest, "sensitive fixture only", marker, 1))
			if _, e := a.SubmitInput(context.Background(), w, m, b); e != nil {
				t.Fatal(e)
			}
			o := capiState(t, a, w, "UNKNOWN")
			if _, e := a.Cleanup(context.Background(), w, o); e == nil {
				t.Fatal("native exit/init failure became stop proof")
			}
			if _, e := a.SubmitInput(context.Background(), w, m, b); e == nil {
				t.Fatal("ambiguous native request resubmitted")
			}
		})
	}
}

func TestNativeCAPIPackTamperAndUnavailableDeviceFailClosed(t *testing.T) {
	c := capiPack(t)
	if e := os.WriteFile(c.Library, []byte("tampered fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if a, e := Open(ctx, c); e == nil {
		_ = a.Close(ctx)
		t.Fatal("tampered pack started")
	}
	c = capiPack(t)
	c.GPULayers = 1
	c.GPUDevice = "CUDA0"
	if a, e := Open(ctx, c); e == nil {
		_ = a.Close(ctx)
		t.Fatal("unavailable GPU silently fell back")
	}
}
