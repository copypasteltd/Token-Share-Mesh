package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The issuer/authority and backend here are explicit synthetic fixtures. HTTP,
// signed device proofs, native files/AEAD, both SQLite journals and the actual
// supervisor run loop are real. There is no model, GPU or payment invocation.
const hostContentMarker = "CUSTOMER_CONTENT_SENTINEL_HOST_R020"

type hostFixture struct {
	node                                                             *Store
	host                                                             *Host
	config                                                           HostConfig
	server                                                           *httptest.Server
	key                                                              ed25519.PrivateKey
	body                                                             []byte
	lease                                                            executionlease.Lease
	manifest                                                         executionlease.Manifest
	mu                                                               sync.Mutex
	requests, nonces                                                 [][]byte
	seen                                                             map[string]bool
	claims, inputs                                                   atomic.Int32
	dropClaim, oversizeInput, oversizeManifest, wrongPin, wrongOrder atomic.Bool
	blockInput                                                       atomic.Bool
	inputStarted                                                     chan struct{}
	inputOnce                                                        sync.Once
}

func newHostFixture(t *testing.T) *hostFixture {
	t.Helper()
	f := &hostFixture{key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{91}, 32)), seen: map[string]bool{}, inputStarted: make(chan struct{})}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	t.Cleanup(f.server.Close)
	f.node = testStore(t, f.server.URL)
	f.node.s.DeviceID = "device_synthetic"
	f.node.s.SellerID = "seller_synthetic"
	f.node.s.Version = 1
	f.node.s.RemoteState = "PAIRED"
	if e := f.node.save(); e != nil {
		t.Fatal(e)
	}
	if _, e := f.node.InitExecution(context.Background()); e != nil {
		t.Fatal(e)
	}
	a := devicecontrol.Approval{Order: "order_test", Route: "route_test", Seller: "seller_synthetic", Device: "device_synthetic", Resource: "resource_test", Capability: "cap_test", ContractHash: strings.Repeat("c", 64), Mode: "PLAINTEXT", Privacy: "STANDARD", EnvironmentRef: "kernel_environment_test", EnvironmentDigest: strings.Repeat("d", 64), EngineKind: "LLAMA_CPP_NATIVE", EngineRelease: "llama_test_1", ModelDigest: strings.Repeat("e", 64), Spec: executionlease.Spec{Protocol: relay.ChatText, Model: "mesh-local-text", MaxOutputTokens: 32}, InputTokenLimit: 1024}
	f.config = HostConfig{Profile: HostProfile, IssuerPin: base64.RawURLEncoding.EncodeToString(f.key.Public().(ed25519.PublicKey)), Rules: []HostRule{{Approval: a, PhysicalDigest: strings.Repeat("f", 64)}}}
	f.body = []byte("{\n  \"model\":\"mesh-local-text\", \"messages\":[{\"role\":\"user\",\"content\":\"" + hostContentMarker + "\"}],\n  \"max_completion_tokens\":8,\"stream\":false\n}\n")
	f.issue(t, time.Now().UTC())
	var e error
	f.host, e = f.node.InitHost(context.Background(), f.config)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *hostFixture) issue(t *testing.T, now time.Time) {
	t.Helper()
	a := f.config.Rules[0].Approval
	f.manifest = executionlease.Manifest{Profile: executionlease.ManifestProfile, ID: "manifest_test", Job: "job_test", Attempt: "attempt_test", Order: a.Order, Route: a.Route, Seller: a.Seller, Device: a.Device, Resource: a.Resource, Capability: a.Capability, Generation: "1", Fence: "1", ContractHash: a.ContractHash, EnvironmentRef: a.EnvironmentRef, EnvironmentDigest: a.EnvironmentDigest, EngineKind: a.EngineKind, EngineRelease: a.EngineRelease, ModelDigest: a.ModelDigest, RecipeDigest: a.RecipeDigest, Spec: a.Spec, InputTokenLimit: a.InputTokenLimit, InputAsset: "input_asset_test", InputDigest: strings.Repeat("a", 64), InputBytes: int64(len(f.body)), Mode: a.Mode, Privacy: a.Privacy, HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano)}
	f.manifest.Spec.MaxOutputTokens = 8
	f.lease = executionlease.Lease{ID: "lease_test", Job: f.manifest.Job, Attempt: f.manifest.Attempt, Order: a.Order, Seller: a.Seller, Device: a.Device, Resource: a.Resource, Generation: "1", Capability: a.Capability, Manifest: f.manifest.ID, ManifestHash: f.manifest.Digest(), Environment: a.EnvironmentDigest, Revision: 1, Issued: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), HardDeadline: f.manifest.HardDeadline, Fence: "1", Expires: now.Add(40 * time.Second).Format(time.RFC3339Nano), ContractHash: a.ContractHash, InputAssets: []string{f.manifest.InputAsset}, Profile: executionlease.Profile}
	message, e := executionlease.Message(f.lease)
	if e != nil {
		t.Fatal(e)
	}
	f.lease.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, message))
}
func (f *hostFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	body, e := io.ReadAll(r.Body)
	if e != nil {
		t.Error(e)
		return
	}
	nonce := verify(t, f.node, r, body)
	f.mu.Lock()
	if f.seen[nonce] {
		t.Error("proof nonce replayed")
	}
	f.seen[nonce] = true
	f.nonces = append(f.nonces, []byte(nonce))
	if r.Method == "POST" {
		f.requests = append(f.requests, bytes.Clone(body))
	}
	f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/p11/runner/leases/claim":
		f.claims.Add(1)
		var in devicecontrol.LeaseClaim
		if json.Unmarshal(body, &in) != nil || in.Validate(r.Header.Get("Idempotency-Key")) != nil || in.Device != "device_synthetic" || in.Capability != "cap_test" || in.Version != 1 {
			writeReply(w, 409, map[string]string{"code": "FIXTURE_ORIGINAL_CLAIM_REQUIRED"})
			return
		}
		if f.dropClaim.Swap(false) {
			drop(w)
			return
		}
		lease := f.lease
		if f.wrongPin.Load() {
			lease.Signature = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64))
		}
		writeReply(w, 200, lease)
	case "/v1/p11/runner/leases/lease_test/manifest":
		if f.oversizeManifest.Load() {
			writeReply(w, 200, map[string]string{"oversize": strings.Repeat("x", 17000)})
			return
		}
		manifest := f.manifest
		if f.wrongOrder.Load() {
			manifest.Order = "other_order"
		}
		writeReply(w, 200, manifest)
	case "/v1/p11/runner/leases/lease_test/input":
		f.inputs.Add(1)
		f.inputOnce.Do(func() { close(f.inputStarted) })
		if f.blockInput.Load() {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if f.oversizeInput.Load() {
			_ = json.NewEncoder(w).Encode(map[string]string{"oversize": strings.Repeat("x", 256<<10)})
			return
		}
		_, _ = w.Write(f.body)
	default:
		writeReply(w, 404, map[string]string{"code": "FIXTURE_NOT_FOUND"})
	}
}
func (f *hostFixture) begin(t *testing.T, id string) {
	t.Helper()
	if e := f.host.Begin(context.Background(), id, "cap_test", 1); e != nil {
		t.Fatal(e)
	}
}
func (f *hostFixture) reopen(t *testing.T) {
	t.Helper()
	f.node = reopen(t, f.node)
	var e error
	f.host, e = f.node.OpenHost(context.Background())
	if e != nil {
		t.Fatal(e)
	}
}

type contentBackend struct {
	nodeBackend
	mu                 sync.Mutex
	inputCalls         atomic.Int32
	captured, borrowed []byte
	lost               bool
}

func (b *contentBackend) SubmitInput(ctx context.Context, w supervisor.Work, m executionlease.Manifest, body []byte) (supervisor.Observation, error) {
	b.inputCalls.Add(1)
	b.mu.Lock()
	b.captured = bytes.Clone(body)
	b.borrowed = body
	b.mu.Unlock()
	o, e := b.nodeBackend.Submit(ctx, w)
	if b.lost {
		return supervisor.Observation{}, errors.New("synthetic lost input acknowledgement")
	}
	return o, e
}
func (f *hostFixture) watch(t *testing.T, b *contentBackend) *supervisor.Supervisor {
	t.Helper()
	a := f.config.Rules[0].Approval
	w, e := f.host.NewSupervisor(context.Background(), []HostKernel{{Environment: a.EnvironmentDigest, Capability: a.Capability, EngineKind: a.EngineKind, EngineRelease: a.EngineRelease, ModelDigest: a.ModelDigest, RecipeDigest: a.RecipeDigest, Backend: b}}, supervisor.Options{Poll: 10 * time.Millisecond, CallTimeout: time.Second, MetadataTimeout: 2 * time.Second, ShutdownTimeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	return w
}
func runHostWatch(t *testing.T, w *supervisor.Supervisor) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case <-w.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("host watchdog did not start")
	}
	t.Cleanup(cancel)
	return cancel, done
}
func stopHostWatch(t *testing.T, cancel context.CancelFunc, done <-chan error, want error) {
	t.Helper()
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, want) {
			t.Fatal("host shutdown", e, want)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("host watchdog did not stop")
	}
}
func hostDiskHas(t *testing.T, s *Store, needle []byte) bool {
	t.Helper()
	entries, e := os.ReadDir(s.path)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		// The OS lock and SQLite's fixed WAL index are byte-locked on Windows.
		// They contain synchronization/index metadata, not SQL payload bytes.
		// Scan the actual DB/WAL, encrypted state and other ordinary files.
		if !entry.IsDir() && entry.Name() != "control.lock" && !strings.HasSuffix(entry.Name(), "-shm") {
			body, e := os.ReadFile(filepath.Join(s.path, entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(body, needle) {
				return true
			}
		}
	}
	return false
}

func TestHostOriginalInputDispatchOnceAndCleanupProjection(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	f.begin(t, "command_original")
	p, e := f.host.Prepare(ctx, "command_original")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if p.Manifest().Spec.MaxOutputTokens != 8 || f.config.Rules[0].Approval.Spec.MaxOutputTokens != 32 {
		t.Fatal("independent maximum was not specialized safely")
	}
	b := &contentBackend{}
	watch := f.watch(t, b)
	cancel, done := runHostWatch(t, watch)
	hold, e := p.Start(ctx, watch)
	if e != nil || hold.Owner() != f.lease.ID {
		t.Fatal(hold, e)
	}
	if b.inputCalls.Load() != 1 || !bytes.Equal(b.captured, f.body) || !bytes.Equal(b.borrowed, make([]byte, len(b.borrowed))) || len(p.body) != 0 {
		t.Fatal("original bytes or owned-buffer clearing failed")
	}
	if _, e = p.Start(ctx, watch); !errors.Is(e, ErrHostPolicy) {
		t.Fatal("ticket replay", e)
	}
	if e = f.host.ReconcileCleanup(ctx, "command_original"); !errors.Is(e, ErrHostPending) {
		t.Fatal("cancel/submit ack was treated as cleanup", e)
	}
	if hostDiskHas(t, f.node, []byte(hostContentMarker)) {
		t.Fatal("customer content persisted")
	}
	stopHostWatch(t, cancel, done, nil)
	if e = f.host.ReconcileCleanup(ctx, "command_original"); e != nil {
		t.Fatal(e)
	}
	status, e := f.host.Snapshot(ctx)
	if e != nil || len(status.Commands) != 1 || status.Commands[0].Phase != "CLEANED" || status.ExecutionEnabled {
		t.Fatal(status, e)
	}
	f.reopen(t)
	if _, e = f.host.Prepare(ctx, "command_original"); !errors.Is(e, ErrHostPending) {
		t.Fatal("cleaned original resubmitted", e)
	}
	if e = f.host.Begin(ctx, "command_next", "cap_test", 1); e != nil {
		t.Fatal(e)
	}
}

func TestHostUnknownClaimRecoversExactCommandAcrossReopen(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original")
	f.dropClaim.Store(true)
	if _, e := f.host.Prepare(context.Background(), "command_original"); e == nil {
		t.Fatal("lost claim reply accepted")
	}
	f.reopen(t)
	p, e := f.host.Prepare(context.Background(), "command_original")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claims.Load() != 2 || len(f.requests) != 2 || !bytes.Equal(f.requests[0], f.requests[1]) || bytes.Equal(f.nonces[0], f.nonces[1]) {
		t.Fatal("unknown result changed original key/body or reused proof")
	}
}

func TestHostIndependentApprovalAndPinRejectBeforeInput(t *testing.T) {
	for _, kind := range []string{"pin", "order", "model", "output-limit", "stream", "physical-alias", "purpose-mode"} {
		t.Run(kind, func(t *testing.T) {
			f := newHostFixture(t)
			switch kind {
			case "pin":
				f.wrongPin.Store(true)
			case "order":
				f.wrongOrder.Store(true)
			case "model":
				f.manifest.Spec.Model = "other_model"
			case "output-limit":
				f.manifest.Spec.MaxOutputTokens = 33
			case "stream":
				f.manifest.Spec.Stream = true
			case "physical-alias":
				c := cloneHostConfig(f.config)
				r := c.Rules[0]
				r.Approval.Order = "other_order"
				r.Approval.Route = "other_route"
				r.Approval.Resource = "other_resource"
				c.Rules = append(c.Rules, r)
				if c.validate(f.host.device, f.host.seller) == nil {
					t.Fatal("physical alias permitted")
				}
				return
			case "purpose-mode":
				c := cloneHostConfig(f.config)
				c.Rules[0].Approval.Mode = "E2E"
				if c.validate(f.host.device, f.host.seller) == nil {
					t.Fatal("mode silently downgraded")
				}
				return
			}
			f.begin(t, "command_original")
			if _, e := f.host.Prepare(context.Background(), "command_original"); !errors.Is(e, ErrHostPolicy) {
				t.Fatal(kind, e)
			}
			if f.inputs.Load() != 0 {
				t.Fatal("content fetched before independent approval")
			}
		})
	}
}

func TestHostReopenKeepsOriginalOwnerQueryOnly(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	f.begin(t, "command_original")
	p, e := f.host.Prepare(ctx, "command_original")
	if e != nil {
		t.Fatal(e)
	}
	b := &contentBackend{lost: true}
	watch := f.watch(t, b)
	cancel, done := runHostWatch(t, watch)
	if _, e = p.Start(ctx, watch); !errors.Is(e, supervisor.ErrUnknown) {
		t.Fatal(e)
	}
	p.Close()
	stopHostWatch(t, cancel, done, nil)
	// Even after original cleanup, the durable native receipt cannot grant a
	// second Submit; the host projection deliberately has not yet reconciled.
	f.reopen(t)
	again, e := f.host.Prepare(ctx, "command_original")
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	watch = f.watch(t, b)
	cancel, done = runHostWatch(t, watch)
	if _, e = again.Start(ctx, watch); !errors.Is(e, supervisor.ErrUnknown) {
		t.Fatal("original receipt became a new submit", e)
	}
	stopHostWatch(t, cancel, done, nil)
	if b.inputCalls.Load() != 1 {
		t.Fatal("input replayed after reopening")
	}
}

func TestHostCommandsFreezeParametersAndSerializeCapability(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	var won atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Go(func() {
			if f.host.Begin(ctx, fmt.Sprintf("command_%02d", i), "cap_test", 1) == nil {
				won.Add(1)
			}
		})
	}
	group.Wait()
	if won.Load() != 1 {
		t.Fatal("multiple unfinished commands for one capability", won.Load())
	}
	s, e := f.host.Snapshot(ctx)
	if e != nil || len(s.Commands) != 1 {
		t.Fatal(s, e)
	}
	id := s.Commands[0].ID
	if e = f.host.Begin(ctx, id, "cap_test", 1); e != nil {
		t.Fatal(e)
	}
	if e = f.host.Begin(ctx, id, "cap_test", 2); !errors.Is(e, ErrHostPolicy) {
		t.Fatal("original version changed", e)
	}
	if f.claims.Load() != 0 {
		t.Fatal("Begin sent before a durable original command existed")
	}
}

func TestHostMissingCorruptAndCrossNodeStateNeverReinitializes(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt", "binding", "config"} {
		t.Run(kind, func(t *testing.T) {
			f := newHostFixture(t)
			path := f.node.path
			switch kind {
			case "binding":
				_, e := f.host.db.Exec(`UPDATE metadata SET value='other_binding' WHERE name='binding_hash'`)
				if e != nil {
					t.Fatal(e)
				}
			case "config":
				_, e := f.host.db.Exec(`UPDATE configuration SET sealed=zeroblob(40)`)
				if e != nil {
					t.Fatal(e)
				}
			}
			f.node.Close()
			if kind == "missing" {
				if e := os.Remove(filepath.Join(path, hostFile)); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "corrupt" {
				if e := os.WriteFile(filepath.Join(path, hostFile), []byte("not sqlite"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			s, e := openWith(context.Background(), path, testLoader)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if _, e = s.OpenHost(context.Background()); !errors.Is(e, ErrHost) {
				t.Fatal(kind, e)
			}
			if _, e = s.InitHost(context.Background(), f.config); e == nil {
				t.Fatal("registered host recreated")
			}
		})
	}
}

func TestHostBoundedResponsesAndExpiredTicketNeverSubmit(t *testing.T) {
	for _, kind := range []string{"manifest", "input", "expiry"} {
		t.Run(kind, func(t *testing.T) {
			f := newHostFixture(t)
			f.begin(t, "command_original")
			if kind == "manifest" {
				f.oversizeManifest.Store(true)
			}
			if kind == "input" {
				f.oversizeInput.Store(true)
			}
			p, e := f.host.Prepare(context.Background(), "command_original")
			if kind != "expiry" {
				if e == nil {
					p.Close()
					t.Fatal("oversized native reply accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			// Verify exactly at the signed expiry without relaxing a runtime timer.
			expiry, _ := time.Parse(time.RFC3339Nano, f.lease.Expires)
			if _, e = p.accepted.AcceptInput(devicecontrol.Reply{Status: 200, Body: f.body}, expiry); e == nil {
				t.Fatal("expiry boundary accepted")
			}
			p.Close()
			b := &contentBackend{}
			watch := f.watch(t, b)
			if _, e = p.Start(context.Background(), watch); !errors.Is(e, ErrHostPolicy) || b.inputCalls.Load() != 0 {
				t.Fatal("closed/expired ticket reached backend", e)
			}
		})
	}
}

func TestHostConcurrentPrepareAndCloseCancelOriginalRead(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original")
	f.blockInput.Store(true)
	done := make(chan error, 1)
	go func() {
		p, e := f.host.Prepare(context.Background(), "command_original")
		if p != nil {
			p.Close()
		}
		done <- e
	}()
	select {
	case <-f.inputStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("input request never started")
	}
	if _, e := f.host.Prepare(context.Background(), "command_original"); !errors.Is(e, ErrHostPending) {
		t.Fatal("parallel original command admitted", e)
	}
	f.host.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled input accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host close did not cancel owned read")
	}
	if f.claims.Load() != 1 {
		t.Fatal("concurrent prepare created another claim")
	}
}

func TestHostCloseWipesIdleTicketsAndStatusHasNoContent(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original")
	p, e := f.host.Prepare(context.Background(), "command_original")
	if e != nil {
		t.Fatal(e)
	}
	borrowed := p.body
	status, e := f.host.Snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte(hostContentMarker)) || hostDiskHas(t, f.node, []byte(hostContentMarker)) {
		t.Fatal("content in metadata or status")
	}
	f.host.Close()
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) || len(p.body) != 0 {
		t.Fatal("idle ticket not cleared on close")
	}
	p.Close()
}

func TestHostCatalogRejectsSubstitutionAndForeignSupervisor(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original")
	p, e := f.host.Prepare(context.Background(), "command_original")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	a := f.config.Rules[0].Approval
	b := &contentBackend{}
	bad := HostKernel{Environment: a.EnvironmentDigest, Capability: a.Capability, EngineKind: a.EngineKind, EngineRelease: "other_release", ModelDigest: a.ModelDigest, Backend: b}
	if _, e = f.host.NewSupervisor(context.Background(), []HostKernel{bad}, supervisor.Options{}); !errors.Is(e, ErrHostPolicy) {
		t.Fatal("kernel substitution accepted", e)
	}
	foreign, e := supervisor.New(f.node.execution, []supervisor.Registration{{Environment: a.EnvironmentDigest, Capability: a.Capability, ManifestHash: f.manifest.Digest(), Backend: b}}, supervisor.Options{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Start(context.Background(), foreign); !errors.Is(e, ErrHostPolicy) || b.inputCalls.Load() != 0 {
		t.Fatal("foreign supervisor accepted", e)
	}
	valid := bad
	valid.EngineRelease = a.EngineRelease
	if _, e = f.host.NewSupervisor(context.Background(), []HostKernel{valid}, supervisor.Options{}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.host.NewSupervisor(context.Background(), []HostKernel{valid}, supervisor.Options{}); !errors.Is(e, ErrHostPending) {
		t.Fatal("second frozen watchdog admitted before the first finished", e)
	}
}

func TestHostCorruptCommandStopsAdmissionAndCannotProjectCleanup(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original")
	if e := f.host.ReconcileCleanup(context.Background(), "command_original"); !errors.Is(e, ErrHostPending) {
		t.Fatal(e)
	}
	if _, e := f.host.db.Exec(`UPDATE commands SET phase='CLEANED' WHERE command_id='command_original'`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.host.Snapshot(context.Background()); !errors.Is(e, ErrHost) {
		t.Fatal("unauthenticated phase accepted", e)
	}
	if e := f.host.Begin(context.Background(), "command_other", "cap_test", 1); !errors.Is(e, ErrHost) {
		t.Fatal("corruption did not close admission", e)
	}
}

func TestHostSelectorsCannotHideUnfinishedCommand(t *testing.T) {
	for _, kind := range []string{"phase", "capability"} {
		t.Run(kind, func(t *testing.T) {
			f := newHostFixture(t)
			f.begin(t, "command_original")
			query := `UPDATE commands SET phase='CLEANED' WHERE command_id='command_original'`
			if kind == "capability" {
				query = `UPDATE commands SET capability_id='cap_other' WHERE command_id='command_original'`
			}
			if _, e := f.host.db.Exec(query); e != nil {
				t.Fatal(e)
			}
			if e := f.host.Begin(context.Background(), "command_replacement", "cap_test", 1); !errors.Is(e, ErrHost) {
				t.Fatal("unauthenticated selector hid original command", e)
			}
			if f.claims.Load() != 0 {
				t.Fatal("corrupt state caused a replacement claim")
			}
		})
	}
}

func TestHostCapacityRetainsOriginalAndRefusesNew(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	// Synthetic journal saturation only: no receive, owner, model or cleanup
	// proof is manufactured. Each retained claim still needs original recovery.
	if e := f.host.transaction(ctx, func(tx *sql.Tx) error {
		for i := 0; i < MaximumHostCommands; i++ {
			id := fmt.Sprintf("command_capacity_%04d", i)
			c := hostCommand{ID: id, Claim: devicecontrol.LeaseClaim{Device: f.host.device, Capability: fmt.Sprintf("cap_capacity_%04d", i), ClaimKey: id, Version: 1}, Phase: "CLAIM_PENDING"}
			if e := f.host.saveCommand(ctx, tx, c, true); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.host.Begin(ctx, "command_over_capacity", "cap_test", 1); !errors.Is(e, ErrHostPending) {
		t.Fatal("bounded journal did not refuse new admission", e)
	}
	s, e := f.host.Snapshot(ctx)
	if e != nil || len(s.Commands) != MaximumHostCommands || f.claims.Load() != 0 {
		t.Fatal("capacity limit discarded originals or sent a claim", e)
	}
}

func TestHostLegacyMetadataAdapterCannotConsumeOriginalContent(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	f.begin(t, "command_original")
	p, e := f.host.Prepare(ctx, "command_original")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	a := f.config.Rules[0].Approval
	b := &nodeBackend{}
	w, e := supervisor.New(f.node.execution, []supervisor.Registration{{Environment: a.EnvironmentDigest, Capability: a.Capability, ManifestHash: f.manifest.Digest(), Backend: b}}, supervisor.Options{Poll: 10 * time.Millisecond, CallTimeout: time.Second, MetadataTimeout: 2 * time.Second, ShutdownTimeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := runHostWatch(t, w)
	if _, e = w.SubmitInput(ctx, "command_original", p.accepted, p.body); !errors.Is(e, supervisor.ErrAdapter) {
		t.Fatal("legacy metadata adapter consumed content", e)
	}
	status, e := f.node.execution.CommandStatus(ctx, "command_original")
	if e != nil || status.Found {
		t.Fatal("unsupported content submission wrote a receive receipt", e)
	}
	stopHostWatch(t, cancel, done, nil)
}
