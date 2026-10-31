//go:build windows && p11_native_real

package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/textoutput"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/shared/gpuid"
)

// Real hardware/process/inference, with an explicitly SYNTHETIC platform issuer,
// order/payment authority, HTTP delivery and local owner approval fixture.
// These tests do not qualify the platform, public SDK, privacy or funds.
func nativeRealHost(t *testing.T, profile relay.Profile, stream bool, device string) (*hostFixture, NativeInstallation) {
	t.Helper()
	install := NativeInstallation{RuntimeRoot: os.Getenv("P11_NATIVE_RUNTIME_ROOT"), Package: llamaworker.FilePin{Path: os.Getenv("P11_REAL_PACKAGE"), SHA256: os.Getenv("P11_REAL_PACKAGE_SHA256")}, Selection: llamaworker.OwnerSelection{Capability: "cap_test", Resource: "resource_test", GPUDevice: device, GPUUUID: os.Getenv("P11_REAL_GPU_UUID"), GPUPCI: os.Getenv("P11_REAL_GPU_PCI"), GPULayers: 999, ContextTokens: 2048, BatchTokens: 128, Threads: 2}}
	var err error
	install.Environment, err = llamaworker.PackageEnvironment(install.Package.SHA256, install.Selection)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := llamaworker.LoadPackage(ctx, install.RuntimeRoot, install.Package, install.Selection, install.Environment)
	if err != nil {
		t.Fatal("explicit package required", err)
	}
	digest, err := gpuid.Digest(c.GPUUUID)
	if err != nil {
		t.Fatal(err)
	}
	f := newHostFixtureConfigured(t, func(f *hostFixture) {
		a := &f.config.Rules[0].Approval
		a.EnvironmentDigest = c.Environment
		a.EngineRelease = llamaworker.EngineRelease
		a.ModelDigest = c.Model.SHA256
		a.RecipeDigest = c.Template.SHA256
		a.Spec.Protocol = profile
		a.Spec.Stream = stream
		f.config.Rules[0].PhysicalDigest = digest
		prompt := "Reply with one brief greeting. Reference: " + hostContentMarker
		body := map[string]any{"model": "mesh-local-text", "stream": stream}
		switch profile {
		case relay.ChatText:
			body["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
			body["max_completion_tokens"] = 8
		case relay.ResponsesText:
			body["store"] = false
			body["input"] = prompt
			body["max_output_tokens"] = 8
		case relay.MessagesText:
			body["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
			body["max_tokens"] = 8
		}
		f.body, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	})
	return f, install
}

func nativeRealPrepare(t *testing.T, f *hostFixture, id string) *PreparedExecution {
	t.Helper()
	f.begin(t, id)
	p, err := f.host.Prepare(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func nativeRealReconcile(t *testing.T, ctx context.Context, h *Host, id string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := h.ReconcileCleanup(ctx, id)
		if err == nil {
			return
		}
		if !errors.Is(err, ErrHostPending) {
			t.Fatal("original cleanup projection", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("original cleanup not proved")
		case <-ticker.C:
		}
	}
}

type nativeHostSink struct{ bytes.Buffer }

func (s *nativeHostSink) Write(ctx context.Context, p []byte) error {
	if ctx.Err() != nil || s.Len()+len(p) > 2<<20 {
		return io.ErrShortWrite
	}
	_, err := s.Buffer.Write(p)
	return err
}
func nativeHostText(t *testing.T, profile relay.Profile, stream bool, body string) string {
	t.Helper()
	var text strings.Builder
	read := func(raw string) {
		var o map[string]any
		if json.Unmarshal([]byte(raw), &o) != nil {
			return
		}
		if stream {
			switch profile {
			case relay.ChatText:
				if choices, ok := o["choices"].([]any); ok && len(choices) > 0 {
					choice, _ := choices[0].(map[string]any)
					delta, _ := choice["delta"].(map[string]any)
					s, _ := delta["content"].(string)
					text.WriteString(s)
				}
			case relay.ResponsesText:
				if o["type"] == "response.output_text.delta" {
					s, _ := o["delta"].(string)
					text.WriteString(s)
				}
			case relay.MessagesText:
				if o["type"] == "content_block_delta" {
					delta, _ := o["delta"].(map[string]any)
					s, _ := delta["text"].(string)
					text.WriteString(s)
				}
			}
			return
		}
		switch profile {
		case relay.ChatText:
			choices, _ := o["choices"].([]any)
			if len(choices) > 0 {
				choice, _ := choices[0].(map[string]any)
				message, _ := choice["message"].(map[string]any)
				s, _ := message["content"].(string)
				text.WriteString(s)
			}
		case relay.ResponsesText:
			output, _ := o["output"].([]any)
			for _, v := range output {
				item, _ := v.(map[string]any)
				content, _ := item["content"].([]any)
				for _, c := range content {
					part, _ := c.(map[string]any)
					s, _ := part["text"].(string)
					text.WriteString(s)
				}
			}
		case relay.MessagesText:
			content, _ := o["content"].([]any)
			for _, c := range content {
				part, _ := c.(map[string]any)
				s, _ := part["text"].(string)
				text.WriteString(s)
			}
		}
	}
	if stream {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "data: ") {
				read(strings.TrimPrefix(line, "data: "))
			}
		}
	} else {
		read(body)
	}
	if text.Len() == 0 {
		t.Fatal("no actual text in protocol output")
	}
	return text.String()
}

func TestHostNativeRealThreeProfilesOriginalOrderAndCustody(t *testing.T) {
	for _, profile := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		for _, stream := range []bool{false, true} {
			t.Run(string(profile)+map[bool]string{false: "_json", true: "_sse"}[stream], func(t *testing.T) {
				f, install := nativeRealHost(t, profile, stream, "CUDA0")
				p := nativeRealPrepare(t, f, "command_native_real")
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				s, err := f.host.OpenNative(ctx, install, supervisor.Options{})
				if err != nil {
					t.Fatal("owned native session", err)
				}
				t.Cleanup(func() {
					closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := s.Close(closeCtx); err != nil {
						t.Error("owned session close", err)
					}
				})
				go func() { _ = s.Run(ctx) }()
				select {
				case <-s.Ready():
				case <-ctx.Done():
					t.Fatal("native session not ready")
				}
				if _, err = s.Start(ctx, p); err != nil {
					t.Fatal("original submit", err)
				}
				nativeRealReconcile(t, ctx, f.host, "command_native_real")
				out, err := p.Output(ctx)
				if err != nil {
					t.Fatal(err)
				}
				sink := &nativeHostSink{}
				receipt, err := textoutput.Render(ctx, sink, out, textoutput.DefaultLimits())
				if err != nil || receipt.InputTokens < 1 || receipt.OutputTokens < 1 || receipt.OutputTokens > 8 || receipt.ResultID == "" || receipt.Profile != profile {
					t.Fatal("original output", receipt, err)
				}
				text := nativeHostText(t, profile, stream, sink.String())
				if _, err = s.Start(ctx, p); err == nil {
					t.Fatal("original execution replayed")
				}
				if f.claims.Load() != 1 || f.inputs.Load() != 1 {
					t.Fatal("claim or original input replayed")
				}
				if hostDiskHas(t, f.node, []byte(hostContentMarker)) {
					t.Fatal("customer prompt persisted in node journals")
				}
				info := s.Info()
				if info.ObservedGPU.UUID != install.Selection.GPUUUID || info.Resource != "resource_test" || info.Capability != "cap_test" {
					t.Fatal("original hardware binding lost")
				}
				t.Logf("profile=%s stream=%t owned_pid=%d local_original_result=%s input=%d output=%d text_bytes=%d text_sha256=%x", profile, stream, info.ProcessID, receipt.ResultID, receipt.InputTokens, receipt.OutputTokens, len(text), sha256.Sum256([]byte(text)))
			})
		}
	}
}

func TestHostNativeRealCloseWaitsWorkerAndRevokesResult(t *testing.T) {
	f, install := nativeRealHost(t, relay.ChatText, false, "CUDA0")
	p := nativeRealPrepare(t, f, "command_native_close")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := f.host.OpenNative(ctx, install, supervisor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Run(ctx) }()
	select {
	case <-s.Ready():
	case <-ctx.Done():
		t.Fatal("native session not ready")
	}
	if _, err = s.Start(ctx, p); err != nil {
		t.Fatal(err)
	}
	nativeRealReconcile(t, ctx, f.host, "command_native_close")
	out, err := p.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(s.Info().ProcessID))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	f.host.Close()
	if state, err := windows.WaitForSingleObject(process, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("exact owned process still live", err)
	}
	if _, err = out.Read(ctx); err == nil {
		t.Fatal("closed result scope retained readable plaintext")
	}
	_ = out.Close(ctx)
	if err = s.Close(ctx); err != nil {
		t.Fatal("idempotent native session close", err)
	}
	t.Log("owned_worker_exit_observed=true result_scope_revoked=true")
}

func TestHostNativeRealBusyAndUnknownWarmupRetainOwnership(t *testing.T) {
	t.Run("busy_original_owner", func(t *testing.T) {
		f, install := nativeRealHost(t, relay.ChatText, false, "CUDA0")
		nativeRealPrepare(t, f, "command_native_busy")
		ctx := context.Background()
		other, err := f.host.ledger.AcquireLocal(ctx, "resource_test", "local_user_original", "LOCAL")
		if err != nil {
			t.Fatal(err)
		}
		if s, err := f.host.OpenNative(ctx, install, supervisor.Options{}); s != nil || !errors.Is(err, execution.ErrBusy) {
			t.Fatal("busy original resource substituted", err)
		}
		status, err := f.host.ledger.Snapshot(ctx)
		if err != nil || len(status.Resources) != 1 || status.Resources[0].LocalHigh != other.LocalGeneration() || status.Resources[0].Mode != "LOCAL" || status.Resources[0].Phase != "PREPARING" {
			t.Fatal("other owner changed", err)
		}
	})
	t.Run("missing_gpu_unknown_warmup", func(t *testing.T) {
		f, install := nativeRealHost(t, relay.ChatText, false, "CUDA_DOES_NOT_EXIST_999")
		nativeRealPrepare(t, f, "command_native_unknown")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s, err := f.host.OpenNative(ctx, install, supervisor.Options{})
		var startup llamaworker.StartupError
		if s != nil || !errors.As(err, &startup) || startup.Stage != "SELECT_DEVICE" || !f.host.poisoned.Load() {
			t.Fatal("unknown startup acquired execution authority", err)
		}
		status, err := f.host.ledger.Snapshot(ctx)
		if err != nil || len(status.Resources) != 1 || status.Resources[0].Mode != "MAINTENANCE" || status.Resources[0].Phase == "FREE" || status.Resources[0].RemoteHigh != "0" {
			t.Fatal("unknown warmup released resource", err)
		}
		f.reopen(t)
		reopened, err := f.host.ledger.Snapshot(ctx)
		if err != nil || reopened.Resources[0].Mode != "MAINTENANCE" || reopened.Resources[0].Phase == "FREE" || reopened.Resources[0].LocalHigh != status.Resources[0].LocalHigh {
			t.Fatal("restart erased unknown owner", err)
		}
		if s, err = f.host.OpenNative(ctx, install, supervisor.Options{}); s != nil || !errors.Is(err, execution.ErrBusy) {
			t.Fatal("restart reissued native warmup", err)
		}
	})
}
