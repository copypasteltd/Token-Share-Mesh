package node

// SOURCE_ONLY: no engine/model is started by these policy definitions.
import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/strictjson"
)

func providerConfigFixture(t *testing.T) NativeProviderConfig {
	t.Helper()
	root := t.TempDir()
	c, _, _ := nativePolicyFixture()
	selection := llamaworker.OwnerSelection{Capability: c.Capability, Resource: c.Resource, GPUDevice: c.GPUDevice, GPUUUID: c.GPUUUID, GPUPCI: c.GPUPCI, GPULayers: c.GPULayers, ContextTokens: 2048, BatchTokens: 32, Threads: 1}
	pin := llamaworker.FilePin{Path: filepath.Join(root, "approved-package.json"), SHA256: strings.Repeat("a", 64)}
	env, err := llamaworker.PackageEnvironment(pin.SHA256, selection)
	if err != nil {
		t.Fatal(err)
	}
	return NativeProviderConfig{Profile: NativeProviderProfile, Installation: NativeInstallation{RuntimeRoot: root, Package: pin, Selection: selection, Environment: env}}
}

func TestProviderConfigEnforcesBudgetsAndPinnedLocalGPUSelection(t *testing.T) {
	c := providerConfigFixture(t)
	normalized, err := c.Normalize()
	if err != nil || normalized.ClaimMilliseconds != 2000 || normalized.ProgressMilliseconds != 5000 || normalized.RequestMilliseconds != 5000 {
		t.Fatal("explicit defaults unavailable", err)
	}
	for name, change := range map[string]func(*NativeProviderConfig){
		"profile":          func(c *NativeProviderConfig) { c.Profile = "other" },
		"relative_runtime": func(c *NativeProviderConfig) { c.Installation.RuntimeRoot = "relative" },
		"outside_package": func(c *NativeProviderConfig) {
			c.Installation.Package.Path = filepath.Join(filepath.Dir(c.Installation.RuntimeRoot), "outside.json")
		},
		"package_digest": func(c *NativeProviderConfig) { c.Installation.Package.SHA256 = "none" },
		"environment":    func(c *NativeProviderConfig) { c.Installation.Environment = strings.Repeat("b", 64) },
		"gpu_fallback":   func(c *NativeProviderConfig) { c.Installation.Selection.GPULayers = 0 },
		"uuid":           func(c *NativeProviderConfig) { c.Installation.Selection.GPUUUID = "other" },
		"pci":            func(c *NativeProviderConfig) { c.Installation.Selection.GPUPCI = "other" },
		"capability":     func(c *NativeProviderConfig) { c.Installation.Selection.Capability = "bad/value" },
		"resource":       func(c *NativeProviderConfig) { c.Installation.Selection.Resource = "bad/value" },
		"claim_poll":     func(c *NativeProviderConfig) { c.ClaimMilliseconds = 499 },
		"progress_poll":  func(c *NativeProviderConfig) { c.ProgressMilliseconds = 10001 },
		"request_bound":  func(c *NativeProviderConfig) { c.RequestMilliseconds = 5001 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			change(&bad)
			if _, e := bad.Normalize(); !errors.Is(e, ErrHostPolicy) {
				t.Fatal("unsafe resident configuration accepted", e)
			}
		})
	}
}

func TestProviderConfigDoesNotAcceptTaskSuppliedExecutableOrUnknownFields(t *testing.T) {
	c := providerConfigFixture(t)
	raw, _ := json.Marshal(c)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	fields["executable"] = json.RawMessage(`"incoming-task.exe"`)
	raw, _ = json.Marshal(fields)
	var decoded NativeProviderConfig
	if strictjson.Decode(raw, &decoded, 16384) == nil {
		t.Fatal("task supplied engine field accepted")
	}
	if err := (&Store{}).RunNativeProvider(context.Background(), NativeProviderConfig{}, nil); !errors.Is(err, ErrHostPolicy) {
		t.Fatal("invalid config touched unopened node", err)
	}
}

func TestResidentHeartbeatUsesActualLedgerAndKeepsOtherModesBusy(t *testing.T) {
	f := newHostFixture(t)
	w := f.watch(t, &contentBackend{}) // zero task manifests, independently approved kernel
	cancel, done := runHostWatch(t, w)
	defer stopHostWatch(t, cancel, done, nil)
	n := &NativeSession{host: f.host, resource: "resource_test", watch: w, runDone: make(chan struct{}), runStarted: true}
	state, err := n.heartbeatState(context.Background())
	if err != nil || state != "IDLE" {
		t.Fatal("resident free gate was not sampled", state, err)
	}
	if _, err = f.host.ledger.AcquireLocal(context.Background(), "resource_test", "personal_use_original", "LOCAL"); err != nil {
		t.Fatal(err)
	}
	state, err = n.heartbeatState(context.Background())
	if err != nil || state != "BUSY" {
		t.Fatal("personal use advertised available", state, err)
	}
	n.mu.Lock()
	n.closing = true
	n.mu.Unlock()
	state, err = n.heartbeatState(context.Background())
	if err != nil || state != "UNKNOWN" {
		t.Fatal("closing native runtime advertised available", state, err)
	}
}

func TestProviderControlFreezesSampledHeartbeatAcrossUnknownResponse(t *testing.T) {
	var s *Store
	var current devicecontrol.Device
	var mu sync.Mutex
	var bodies [][]byte
	var samples atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		verify(t, s, r, body)
		if r.Method == "GET" {
			writeReply(w, 200, current)
			return
		}
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		count := len(bodies)
		mu.Unlock()
		if count == 1 {
			drop(w)
			return
		}
		writeReply(w, 200, devicecontrol.Ack{ID: current.ID, Version: current.Version, Status: "RECORDED", Recorded: time.Now().UTC()})
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	current = view(s)
	current.State = "ONLINE"
	if err := s.adopt(current); err != nil {
		t.Fatal(err)
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	sample := func(context.Context) (string, error) { samples.Add(1); return "IDLE", nil }
	if err := s.stepResource(context.Background(), sample); !retryable(err) {
		t.Fatal("unknown heartbeat discarded", err)
	}
	if err := s.stepResource(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || string(bodies[0]) != string(bodies[1]) || samples.Load() != 1 {
		t.Fatal("unknown heartbeat resampled and changed original body")
	}
	var h devicecontrol.Heartbeat
	_ = json.Unmarshal(bodies[0], &h)
	if h.State != "IDLE" || s.Status().ExecutionEnabled {
		t.Fatal("owned resource sample became global execution qualification")
	}
}

func TestProviderControlCancellationJoinsItsSingleOwner(t *testing.T) {
	var s *Store
	var current devicecontrol.Device
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		verify(t, s, r, body)
		calls.Add(1)
		if r.Method == "GET" {
			writeReply(w, 200, current)
			return
		}
		writeReply(w, 200, devicecontrol.Ack{ID: current.ID, Version: 1, Status: "RECORDED", Recorded: time.Now().UTC()})
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	current = view(s)
	current.State = "ONLINE"
	if err := s.adopt(current); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err := s.runControl(ctx, s.Step, func(v Status, e error) {
		if e != nil || v.Pending != "" {
			t.Error("unexpected control result")
		}
		cancel()
	})
	if err != nil || calls.Load() != 2 {
		t.Fatal("control did not return after joined cancellation", err)
	}
}

func TestHostResidentBindsSecondOriginalTaskWithoutReplacingBackend(t *testing.T) {
	f := newHostFixture(t)
	backend := &custodyBackend{}
	a := f.config.Rules[0].Approval
	w, err := f.host.NewSupervisor(context.Background(), []HostKernel{{Environment: a.EnvironmentDigest, Capability: a.Capability, EngineKind: a.EngineKind, EngineRelease: a.EngineRelease, ModelDigest: a.ModelDigest, RecipeDigest: a.RecipeDigest, Backend: backend}}, supervisor.Options{Poll: 10 * time.Millisecond, CallTimeout: time.Second, MetadataTimeout: 2 * time.Second, ShutdownTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := runHostWatch(t, w)
	defer stopHostWatch(t, cancel, done, nil)
	for index, id := range []string{"resident_original_one", "resident_original_two"} {
		if index == 1 {
			f.mu.Lock()
			f.manifest.ID, f.manifest.Job, f.manifest.Attempt = "manifest_two", "job_two", "attempt_two"
			f.manifest.Generation, f.manifest.Fence = "2", "1"
			f.lease.ID, f.lease.Job, f.lease.Attempt = "lease_two", f.manifest.Job, f.manifest.Attempt
			f.lease.Manifest, f.lease.ManifestHash = f.manifest.ID, f.manifest.Digest()
			f.lease.Generation, f.lease.Fence = "2", "1"
			message, _ := executionlease.Message(f.lease)
			f.lease.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, message))
			f.mu.Unlock()
		}
		f.begin(t, id)
		p, e := f.host.Prepare(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		hold, e := p.Start(context.Background(), w)
		if e != nil {
			p.Close()
			t.Fatal("new original task failed resident binding", e)
		}
		if e = f.host.ledger.RequestStop(context.Background(), hold); e != nil {
			p.Close()
			t.Fatal(e)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			e = f.host.ReconcileCleanup(context.Background(), id)
			if e == nil {
				break
			}
			if !errors.Is(e, ErrHostPending) || time.Now().After(deadline) {
				p.Close()
				t.Fatal("original cleanup incomplete", e)
			}
			time.Sleep(10 * time.Millisecond)
		}
		p.Close()
	}
	if backend.inputCalls.Load() != 2 {
		t.Fatal("resident binding reran or skipped an original submission")
	}
}

func TestProviderRecoveryDoesNotInventOutputOrAnotherAttempt(t *testing.T) {
	f, id := receivedProgressFixture(t)
	c, err := f.host.command(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.host.recoverProviderCommand(context.Background(), c); !errors.Is(err, ErrHostPending) {
		t.Fatal("unfinished original converted into success", err)
	}
	other, err := f.host.providerCommand(context.Background(), "cap_test", 2)
	if err != nil || other.ID != id {
		t.Fatal("recovery replaced original command", err)
	}
	status, err := f.host.ledger.Snapshot(context.Background())
	if err != nil || status.Resources[0].Phase == "FREE" {
		t.Fatal("metadata recovery freed unconfirmed work", err)
	}
}

func TestProviderRunPublishesAndCleansOriginalWithoutBuyerAcceptance(t *testing.T) {
	f := newHostFixture(t)
	authority := &contentAuthority{}
	f.resultHandler = func(w http.ResponseWriter, r *http.Request, body []byte) { authority.serve(t, f, w, r, body) }
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeReply(w, 200, progressAck("NO_CHANGE", nil))
	}
	f.cleanupHandler = func(w http.ResponseWriter, _ *http.Request, body []byte) {
		var in devicecontrol.CleanupWrite
		if json.Unmarshal(body, &in) != nil || in.Validate() != nil || in.Lease != f.lease.ID {
			t.Error("foreign cleanup report")
			writeReply(w, 409, map[string]string{"code": "CONFLICT"})
			return
		}
		writeReply(w, 200, devicecontrol.Ack{ID: "cleanup_original", Version: 1, Status: "CLEANED", Recorded: time.Now().UTC()})
	}
	spool, err := results.Open(results.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close(context.Background()) })
	backend := &custodyBackend{}
	adapter, err := results.New(backend, spool)
	if err != nil {
		t.Fatal(err)
	}
	approved := f.config.Rules[0].Approval
	watch, err := f.host.NewSupervisor(context.Background(), []HostKernel{{Environment: approved.EnvironmentDigest, Capability: approved.Capability, EngineKind: approved.EngineKind, EngineRelease: approved.EngineRelease, ModelDigest: approved.ModelDigest, RecipeDigest: approved.RecipeDigest, Backend: adapter}}, supervisor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cancel, done := runHostWatch(t, watch)
	defer stopHostWatch(t, cancel, done, nil)
	native := &NativeSession{host: f.host, watch: watch, backend: adapter, resource: approved.Resource, capability: approved.Capability, runStarted: true, runDone: make(chan struct{})}
	native.info.Environment = approved.EnvironmentDigest
	c, err := f.host.providerCommand(context.Background(), approved.Capability, 1)
	if err != nil {
		t.Fatal(err)
	}
	config, err := providerConfigFixture(t).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	run := &providerRun{host: f.host, native: native, config: config, controls: make(chan providerControl), current: providerControl{status: f.node.Status(), healthy: true}}
	ctx, finish := context.WithTimeout(context.Background(), 10*time.Second)
	defer finish()
	if err = run.execute(ctx, c); err != nil {
		t.Fatal("original provider orchestration incomplete", err)
	}
	c, err = f.host.command(ctx, c.ID)
	if err != nil || c.Phase != "CLEANED" || c.CleanupAck == nil || c.ResultContent == nil || !c.ResultContent.Ready || !c.ResultRetired || c.UsageStatement != nil || backend.inputCalls.Load() != 1 {
		t.Fatal("provider invented acceptance/metering or did not retire cache", err)
	}
	authority.mu.Lock()
	ready := authority.ready
	authority.mu.Unlock()
	if !ready {
		t.Fatal("local retirement removed remote result")
	}
	if err = native.DiscardPublished(ctx, c.ID); err != nil {
		t.Fatal("original retirement not idempotent", err)
	}
}

func TestProviderRetirementRequiresReadyAndActualCleanup(t *testing.T) {
	f, id := receivedProgressFixture(t)
	spool, err := results.Open(results.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close(context.Background())
	adapter, err := results.New(&custodyBackend{}, spool)
	if err != nil {
		t.Fatal(err)
	}
	native := &NativeSession{host: f.host, backend: adapter}
	if err = native.DiscardPublished(context.Background(), id); !errors.Is(err, ErrHostPending) {
		t.Fatal("unfinished original cache retired", err)
	}
	c, err := f.host.command(context.Background(), id)
	if err != nil || c.ResultRetired {
		t.Fatal("retirement manufactured cleanup/READY", err)
	}
}
