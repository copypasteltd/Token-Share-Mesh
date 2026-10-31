//go:build windows && p11_native_real

package llamaworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/protocol/textoutput"
	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

func realConfig(t *testing.T, device string) Config {
	t.Helper()
	document := FilePin{Path: os.Getenv("P11_REAL_PACKAGE"), SHA256: os.Getenv("P11_REAL_PACKAGE_SHA256")}
	s := OwnerSelection{Capability: "cap_real_cuda_test", Resource: "physical_gpu_0", GPUDevice: device, GPUUUID: os.Getenv("P11_REAL_GPU_UUID"), GPUPCI: os.Getenv("P11_REAL_GPU_PCI"), GPULayers: 999, ContextTokens: 2048, BatchTokens: 128, Threads: 2}
	env, err := PackageEnvironment(document.SHA256, s)
	if err != nil {
		t.Fatal("explicit pinned real package required", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := LoadPackage(ctx, os.Getenv("P11_NATIVE_RUNTIME_ROOT"), document, s, env)
	if err != nil {
		t.Fatal("real package verification failed", err)
	}
	return c
}
func realOpen(t *testing.T, c Config) *Adapter {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a, err := Open(ctx, c)
	if err != nil {
		t.Fatal("real CUDA model warm-up failed", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Close(ctx); err != nil {
			t.Error("real worker close", err)
		}
		select {
		case <-a.exited:
		case <-ctx.Done():
			t.Error("owned real worker exit not observed")
		}
	})
	info := a.LocalInfo()
	if info.ProcessID < 1 || info.SelectedGPUDevice != "CUDA0" || info.SelectedGPULayers != 999 || info.ModelSHA256 != c.Model.SHA256 || info.Environment != c.Environment || info.ObservedGPU.UUID != c.GPUUUID || info.ObservedGPU.PCI != c.GPUPCI || info.PhysicalDigest == "" {
		t.Fatal("original local metadata lost")
	}
	// Observe modules only in this exact owned process; inspect no memory/content.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, uint32(info.ProcessID))
	if err != nil {
		t.Fatal("owned module snapshot", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ModuleEntry32{Size: uint32(unsafe.Sizeof(windows.ModuleEntry32{}))}
	seen := map[string]bool{}
	for err = windows.Module32First(snapshot, &entry); err == nil; err = windows.Module32Next(snapshot, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.Module[:]))
		if name == "ggml-cuda.dll" || name == "cublas64_12.dll" || name == "cublaslt64_12.dll" || name == "cudart64_12.dll" {
			path := windows.UTF16ToString(entry.ExePath[:])
			if !strings.EqualFold(filepath.Dir(path), filepath.Dir(c.Library)) {
				t.Fatal("CUDA module loaded outside pinned package")
			}
			seen[name] = true
		}
	}
	if err != windows.ERROR_NO_MORE_FILES {
		t.Fatal("module enumeration incomplete", err)
	}
	for _, name := range []string{"ggml-cuda.dll", "cublas64_12.dll", "cublaslt64_12.dll", "cudart64_12.dll"} {
		if !seen[name] {
			t.Fatal("selected CUDA library not loaded", name)
		}
	}
	t.Logf("owned_pid=%d selected_device=%s selected_layers=%d CUDA_modules=4", info.ProcessID, info.SelectedGPUDevice, info.SelectedGPULayers)
	return a
}
func realInput(c Config, id string, p relay.Profile, stream bool, maximum uint32) (supervisor.Work, executionlease.Manifest, []byte) {
	v := map[string]any{"model": "public_model", "stream": stream}
	if p == relay.ChatText {
		v["temperature"] = 0
	}
	prompt := "Write the numbers from one to one hundred in order. Continue until the list is complete."
	if p == relay.ResponsesText {
		v["input"] = prompt
		v["max_output_tokens"] = maximum
		v["store"] = false
	} else {
		v["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
		v["max_tokens"] = maximum
	}
	b, _ := json.Marshal(v)
	_, w, m, _ := textCase(p, string(b))
	w.RequestID = id
	w.LocalGeneration = id
	w.Environment = c.Environment
	m.EnvironmentDigest = c.Environment
	m.ModelDigest = c.Model.SHA256
	m.RecipeDigest = c.Template.SHA256
	m.Capability = c.Capability
	m.Spec.Stream = stream
	m.Spec.MaxOutputTokens = int(maximum)
	return w, m, b
}
func realStopped(t *testing.T, a *Adapter, w supervisor.Work) supervisor.Observation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		o, err := a.Query(ctx, w)
		if err != nil {
			t.Fatal("real original owner query", err)
		}
		if o.State == "STOPPED" {
			return o
		}
		if o.State == "UNKNOWN" {
			t.Fatal("real work became UNKNOWN; release refused")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("real native stop proof deadline")
	return supervisor.Observation{}
}

type realSink struct{ bytes.Buffer }

func (s *realSink) Write(ctx context.Context, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.Buffer.Write(b)
	return err
}

func realText(t *testing.T, p relay.Profile, stream bool, raw string) string {
	t.Helper()
	var output strings.Builder
	object := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	array := func(v any) []any { a, _ := v.([]any); return a }
	text := func(v any) string { s, _ := v.(string); return s }
	read := func(raw string) {
		var v map[string]any
		if json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal("invalid actual model protocol JSON")
		}
		if p == relay.ChatText {
			for _, choice := range array(v["choices"]) {
				key := "message"
				if stream {
					key = "delta"
				}
				output.WriteString(text(object(object(choice)[key])["content"]))
			}
			return
		}
		if stream {
			if p == relay.ResponsesText && v["type"] == "response.output_text.delta" {
				output.WriteString(text(v["delta"]))
			}
			if p == relay.MessagesText && v["type"] == "content_block_delta" {
				output.WriteString(text(object(v["delta"])["text"]))
			}
			return
		}
		if p == relay.ResponsesText {
			for _, item := range array(v["output"]) {
				for _, content := range array(object(item)["content"]) {
					output.WriteString(text(object(content)["text"]))
				}
			}
		} else {
			for _, content := range array(v["content"]) {
				output.WriteString(text(object(content)["text"]))
			}
		}
	}
	if !stream {
		read(raw)
	} else {
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "data: ") && line != "data: [DONE]" {
				read(strings.TrimPrefix(line, "data: "))
			}
		}
	}
	return output.String()
}
func TestNativeRealCUDAThreeProfilesEncryptedCustodyAndModelReuse(t *testing.T) {
	c := realConfig(t, "CUDA0")
	a := realOpen(t, c)
	store, err := results.Open(results.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := store.Close(ctx); e != nil {
			t.Error(e)
		}
	}()
	custody, err := results.New(a, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		for _, stream := range []bool{false, true} {
			name := string(p) + "_json"
			if stream {
				name = string(p) + "_sse"
			}
			t.Run(name, func(t *testing.T) {
				w, m, b := realInput(c, "real_"+name, p, stream, 48)
				defer clear(b)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if _, err := custody.SubmitInput(nativeInputContext(t, ctx, w, m, b), w, m, b); err != nil {
					t.Fatal("real submit", err)
				}
				o := realStopped(t, a, w)
				if _, err = custody.Cleanup(ctx, w, o); err != nil {
					t.Fatal("real cleanup/custody", err)
				}
				out, err := custody.Output(ctx, w)
				if err != nil {
					t.Fatal(err)
				}
				sink := &realSink{}
				receipt, err := textoutput.Render(ctx, sink, out, textoutput.DefaultLimits())
				if err != nil || receipt.InputTokens < 1 || receipt.OutputTokens < 1 || receipt.OutputTokens > 48 || receipt.ResultID == "" || receipt.Terminal != "COMPLETED" && receipt.Terminal != "INCOMPLETE" {
					t.Fatal("real protocol result failed", receipt, err)
				}
				content := realText(t, p, stream, sink.String())
				if len(content) == 0 || !utf8.ValidString(content) {
					t.Fatal("real model generated no valid text")
				}
				if _, err = custody.SubmitInput(nativeInputContext(t, ctx, w, m, b), w, m, b); err == nil {
					t.Fatal("original real work replay accepted")
				}
				t.Logf("profile=%s stream=%t input_tokens=%d output_tokens=%d bytes=%d text_sha256=%x terminal=%s", p, stream, receipt.InputTokens, receipt.OutputTokens, len(content), sha256.Sum256([]byte(content)), receipt.Terminal)
			})
		}
	}
}
func TestNativeRealCUDATokenLimitCancelAndHardDeadline(t *testing.T) {
	c := realConfig(t, "CUDA0")
	a := realOpen(t, c)
	for _, mode := range []string{"token_limit", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			w, m, b := realInput(c, "real_"+mode, relay.ChatText, true, 1024)
			defer clear(b)
			if mode == "token_limit" {
				m.InputTokenLimit = 1
			}
			if mode == "deadline" {
				m.HardDeadline = time.Now().Add(200 * time.Millisecond).UTC().Format(time.RFC3339Nano)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := a.SubmitInput(nativeInputContext(t, ctx, w, m, b), w, m, b); err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				if _, err := a.Started(ctx, w); err != nil {
					t.Fatal(err)
				}
				if err := a.Cancel(ctx, w); err != nil {
					t.Fatal(err)
				}
			}
			o := realStopped(t, a, w)
			_, err := a.Result(w)
			if mode == "token_limit" && err != ErrInput || mode != "token_limit" && err != context.Canceled {
				t.Fatal("real terminal cause lost", err)
			}
			clean, err := a.Cleanup(ctx, w, o)
			if err != nil || !clean.WorkStopped || !clean.WorkspaceCleared || clean.LocalGeneration != w.LocalGeneration {
				t.Fatal("real original cleanup proof missing", err)
			}
		})
	}
}
func TestNativeRealCUDAUnavailableDeviceNeverFallsBack(t *testing.T) {
	t.Run("metadata_only_discovery", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := DiscoverPackage(ctx, os.Getenv("P11_NATIVE_RUNTIME_ROOT"), FilePin{Path: os.Getenv("P11_REAL_PACKAGE"), SHA256: os.Getenv("P11_REAL_PACKAGE_SHA256")})
		if err != nil || len(items) != 1 || items[0].UUID != os.Getenv("P11_REAL_GPU_UUID") || items[0].PCI != os.Getenv("P11_REAL_GPU_PCI") || items[0].Device != "CUDA0" {
			t.Fatal("explicit test board identity missing", err)
		}
		t.Logf("metadata_only_devices=%d model_loaded=false", len(items))
	})
	for _, mode := range []string{"missing", "wrong_uuid", "wrong_pci"} {
		t.Run(mode, func(t *testing.T) {
			c := realConfig(t, "CUDA0")
			stage := "IDENTIFY_DEVICE"
			switch mode {
			case "missing":
				c.GPUDevice = "CUDA_DOES_NOT_EXIST_999"
				stage = "SELECT_DEVICE"
			case "wrong_uuid":
				c.GPUUUID = "GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21"
			case "wrong_pci":
				c.GPUPCI = "0000:02:00.0"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if a, err := Open(ctx, c); err == nil {
				_ = a.Close(ctx)
				t.Fatal("physical GPU mismatch silently accepted")
			} else {
				var startup StartupError
				if !errors.As(err, &startup) || startup.Stage != stage {
					t.Fatal("GPU rejected for another cause", err)
				}
			}
		})
	}
}
