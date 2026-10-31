package node

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/gpuid"
	"p11.local/compute/shared/ids"
)

// These are policy fixtures, not observed native hardware or qualification.
func nativePolicyFixture() (llamaworker.Config, HostRule, llamaworker.LocalRuntimeInfo) {
	c := llamaworker.Config{Environment: strings.Repeat("d", 64), Capability: "cap_test", Resource: "resource_test", GPUDevice: "CUDA0", GPUUUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", GPUPCI: "0000:01:00.0", GPULayers: 999, ContextTokens: 2048,
		Model: llamaworker.FilePin{SHA256: strings.Repeat("e", 64)}, Template: llamaworker.FilePin{SHA256: strings.Repeat("b", 64)}}
	digest, _ := gpuid.Digest(c.GPUUUID)
	r := HostRule{PhysicalDigest: digest, Approval: devicecontrol.Approval{Resource: c.Resource, Capability: c.Capability, EnvironmentDigest: c.Environment, EngineKind: "LLAMA_CPP_NATIVE", EngineRelease: llamaworker.EngineRelease, ModelDigest: c.Model.SHA256, RecipeDigest: c.Template.SHA256, Mode: "PLAINTEXT", Privacy: "STANDARD", InputTokenLimit: 1024, Spec: executionlease.Spec{MaxOutputTokens: 32}}}
	i := llamaworker.LocalRuntimeInfo{ProcessID: 123, Profile: llamaworker.Profile, UpstreamCommit: llamaworker.UpstreamCommit, EngineRelease: llamaworker.EngineRelease, Environment: c.Environment, Resource: c.Resource, Capability: c.Capability, ModelSHA256: c.Model.SHA256, TemplateSHA256: c.Template.SHA256, SelectedGPUDevice: c.GPUDevice, SelectedGPULayers: c.GPULayers, PhysicalDigest: digest, ObservedGPU: gpuid.Identity{Device: c.GPUDevice, UUID: c.GPUUUID, PCI: c.GPUPCI, Name: "NVIDIA GeForce RTX 3060", TotalBytes: 12 << 30}}
	return c, r, i
}

type nativeReaderFixture struct {
	supervisor.TextOutput
	unblock <-chan struct{}
	calls   atomic.Int32
}

func (r *nativeReaderFixture) Close(context.Context) error {
	r.calls.Add(1)
	<-r.unblock
	return nil
}

func TestHostNativeResultReadersKeepBoundedOwnedLifetime(t *testing.T) {
	s := &NativeSession{}
	unblock := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	raw := &nativeReaderFixture{unblock: unblock}
	out, err := s.ownOutput(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if out.Close(canceled) != ErrHostPending {
		t.Fatal("blocked result handle manufactured close completion")
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = out.Close(ctx); err != nil || raw.calls.Load() != 1 {
		t.Fatal("original reader close was replayed or not awaited", err)
	}
	s.mu.Lock()
	remaining := len(s.outputs)
	s.closing = true
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatal("closed reader retained by its native session")
	}
	late := &nativeReaderFixture{unblock: unblock}
	if out, err = s.ownOutput(ctx, late); out != nil || err != ErrHostPending || late.calls.Load() != 1 {
		t.Fatal("late result escaped a closing native session")
	}
}

func TestHostNativeApprovalBindsEveryOriginalKernelFact(t *testing.T) {
	c, r, _ := nativePolicyFixture()
	h := &Host{config: HostConfig{Rules: []HostRule{r}}}
	if _, err := h.approveNative(c); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*HostRule){
		func(r *HostRule) { r.PhysicalDigest = strings.Repeat("0", 64) }, func(r *HostRule) { r.Approval.Resource = "other_gpu" }, func(r *HostRule) { r.Approval.ModelDigest = strings.Repeat("a", 64) }, func(r *HostRule) { r.Approval.RecipeDigest = strings.Repeat("a", 64) }, func(r *HostRule) { r.Approval.EngineKind = "LM_STUDIO" }, func(r *HostRule) { r.Approval.EngineRelease = "other_release" }, func(r *HostRule) { r.Approval.Mode = "E2E" }, func(r *HostRule) { r.Approval.Privacy = "PE" }, func(r *HostRule) { r.Approval.InputTokenLimit = 2048 }, func(r *HostRule) { r.Approval.Spec.MaxOutputTokens = 2048 },
	} {
		bad := r
		change(&bad)
		h.config.Rules = []HostRule{bad}
		if _, err := h.approveNative(c); !errors.Is(err, ErrHostPolicy) {
			t.Fatal("foreign kernel fact approved", err)
		}
	}
	for _, change := range []func(*llamaworker.Config){func(c *llamaworker.Config) { c.Capability = "other_cap" }, func(c *llamaworker.Config) { c.Environment = strings.Repeat("f", 64) }, func(c *llamaworker.Config) { c.GPULayers = 0 }, func(c *llamaworker.Config) { c.GPUUUID = "" }} {
		bad := c
		change(&bad)
		h.config.Rules = []HostRule{r}
		if _, err := h.approveNative(bad); err == nil {
			t.Fatal("unapproved environment")
		}
	}
}

func TestHostNativeApprovalRejectsConflictingSameEnvironment(t *testing.T) {
	c, r, _ := nativePolicyFixture()
	bad := r
	bad.Approval.Resource = "other_gpu"
	bad.Approval.Order = "other_order"
	h := &Host{config: HostConfig{Rules: []HostRule{r, bad}}}
	if _, err := h.approveNative(c); err == nil {
		t.Fatal("one matching rule hid conflicting original resource")
	}
	h.config.Rules = []HostRule{bad, r}
	if _, err := h.approveNative(c); err == nil {
		t.Fatal("rule order changed approval")
	}
}

func TestHostNativeObservedIdentityCannotBorrowAnotherWorker(t *testing.T) {
	c, r, i := nativePolicyFixture()
	if !nativeIdentityMatches(c, r.PhysicalDigest, i) {
		t.Fatal("matching identity rejected")
	}
	for _, change := range []func(*llamaworker.LocalRuntimeInfo){func(i *llamaworker.LocalRuntimeInfo) { i.ProcessID = 0 }, func(i *llamaworker.LocalRuntimeInfo) { i.Profile = "P11_LLAMA_PIPE_3" }, func(i *llamaworker.LocalRuntimeInfo) { i.UpstreamCommit = "other" }, func(i *llamaworker.LocalRuntimeInfo) { i.Resource = "other_gpu" }, func(i *llamaworker.LocalRuntimeInfo) { i.Capability = "other_cap" }, func(i *llamaworker.LocalRuntimeInfo) { i.Environment = strings.Repeat("a", 64) }, func(i *llamaworker.LocalRuntimeInfo) { i.TemplateSHA256 = strings.Repeat("a", 64) }, func(i *llamaworker.LocalRuntimeInfo) { i.ModelSHA256 = strings.Repeat("a", 64) }, func(i *llamaworker.LocalRuntimeInfo) { i.SelectedGPULayers = 1 }, func(i *llamaworker.LocalRuntimeInfo) { i.ObservedGPU.UUID = "GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21" }, func(i *llamaworker.LocalRuntimeInfo) { i.ObservedGPU.PCI = "0000:02:00.0" }, func(i *llamaworker.LocalRuntimeInfo) { i.ObservedGPU.Device = "CUDA1" }, func(i *llamaworker.LocalRuntimeInfo) { i.PhysicalDigest = strings.Repeat("0", 64) }} {
		bad := i
		change(&bad)
		if nativeIdentityMatches(c, r.PhysicalDigest, bad) {
			t.Fatal("borrowed worker identity accepted")
		}
	}
}

func TestHostNativeUnapprovedPackageConsumesNoResource(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	before, err := f.host.ledger.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := f.host.OpenNative(ctx, NativeInstallation{}, supervisor.Options{}); s != nil || !errors.Is(err, ErrHostPolicy) {
		t.Fatal("unapproved package opened", err)
	}
	after, err := f.host.ledger.Snapshot(ctx)
	if err != nil || !reflect.DeepEqual(before, after) || f.host.nativeSession != nil {
		t.Fatal("rejection changed physical ownership", err)
	}
}

func TestHostNativeMountReservationRejectsConcurrentOpen(t *testing.T) {
	f := newHostFixture(t)
	reserved := &NativeSession{host: f.host}
	f.host.nativeSession = reserved
	defer func() { f.host.nativeSession = nil }()
	if s, err := f.host.OpenNative(context.Background(), NativeInstallation{}, supervisor.Options{}); s != nil || !errors.Is(err, ErrHostPending) {
		t.Fatal("second resident environment opened", err)
	}
	if f.host.nativeSession != reserved {
		t.Fatal("second opener stole reservation")
	}
}

func TestHostNativeUnpreparedCommandCannotWarmModel(t *testing.T) {
	f := newHostFixture(t)
	a := f.config.Rules[0].Approval
	c := llamaworker.Config{Environment: a.EnvironmentDigest, Capability: a.Capability, Resource: a.Resource}
	if err := f.host.hasNativeCommand(context.Background(), c); !errors.Is(err, ErrHostPending) {
		t.Fatal("model warmup without original command", err)
	}
	f.begin(t, "command_native_prepared")
	p, err := f.host.Prepare(context.Background(), "command_native_prepared")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = f.host.hasNativeCommand(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.Resource = "other_gpu"
	if err = f.host.hasNativeCommand(context.Background(), c); !errors.Is(err, ErrHostPending) {
		t.Fatal("foreign command warmed model", err)
	}
}

func TestHostNativeSessionRequiresRunningOriginalHostTicket(t *testing.T) {
	s := &NativeSession{}
	if _, err := s.Start(context.Background(), nil); !errors.Is(err, ErrHostPolicy) {
		t.Fatal("unstarted session submitted")
	}
	s.runStarted = true
	s.host = &Host{}
	if _, err := s.Start(context.Background(), &PreparedExecution{host: &Host{}}); !errors.Is(err, ErrHostPolicy) {
		t.Fatal("foreign host submitted")
	}
	s.closing = true
	if _, err := s.Start(context.Background(), &PreparedExecution{host: s.host}); !errors.Is(err, ErrHostPolicy) {
		t.Fatal("closing session submitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, ErrHostPending) {
		t.Fatal("unfinished mount claimed shutdown")
	}
}

func TestHostNativeWarmupCASPreservesOtherOwnersAndFences(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	h, err := f.host.ledger.AcquireLocal(ctx, "resource_test", "native_warmup_original", "MAINTENANCE")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.host.finishNativeWarmup(ctx, h, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	other, err := f.host.ledger.AcquireLocal(ctx, "resource_test", "local_other_owner", "LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.host.finishNativeWarmup(ctx, h, strings.Repeat("d", 64)); !errors.Is(err, execution.ErrFence) {
		t.Fatal("old warmup cleared another owner", err)
	}
	if other.LocalGeneration() == h.LocalGeneration() {
		t.Fatal("local generation reused")
	}
}

func TestHostNativeReleaseLabelsRemainBoundedAndNotPaths(t *testing.T) {
	f := newHostFixture(t)
	c := f.config
	for _, label := range []string{llamaworker.EngineRelease, "llama_test_1"} {
		c.Rules[0].Approval.EngineRelease = label
		if c.validate("device_synthetic", "seller_synthetic") != nil {
			t.Fatal("native release rejected")
		}
	}
	if ids.Valid(llamaworker.EngineRelease) {
		t.Fatal("general identifier grammar widened")
	}
	for _, label := range []string{".llama", "llama.", "llama..cpp", "llama/cpp", "llama\\cpp", "llama:cpp", "llama cpp", "llama\ncpp", strings.Repeat("a", 129)} {
		c.Rules[0].Approval.EngineRelease = label
		if c.validate("device_synthetic", "seller_synthetic") == nil {
			t.Fatal("ambiguous release label accepted")
		}
	}
}
