package comfyenv

// SOURCE_ONLY metadata fixtures. The installed Python is deliberately NOT an
// executable; none of these definitions qualifies an actual runtime or GPU.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/contractspec"
)

type runtimeUseFixture struct {
	registry *Registry
	plan     Plan
	gate     *execution.Store
	hold     execution.Hold
	prepared rentalaccess.Prepared
}

func newRuntimeUseFixture(t *testing.T) runtimeUseFixture {
	t.Helper()
	_, p, r, j, _, _ := fixtureInstall(t)
	ctx := context.Background()
	name := filepath.Join(j.root.Name(), "execution.db")
	if e := os.WriteFile(name, nil, 0600); e != nil {
		t.Fatal(e)
	}
	gate, e := execution.Open(ctx, name, execution.Binding{Installation: strings.Repeat("a", 64), LedgerID: strings.Repeat("c", 64), Device: "device_test", Seller: "seller_test"}, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(gate.Close)
	if e = gate.RegisterResource(ctx, "resource_test", strings.Repeat("e", 64)); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	m := rentalaccess.Manifest{Profile: rentalaccess.ManifestProfile, Reference: "manifest_test", Session: "session_test", Order: "order_test", Buyer: "buyer_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "1", Capability: "capability_test", Environment: "environment_test", EnvironmentHash: p.Digest(), ContractHash: strings.Repeat("c", 64), Mode: "GPU_RENTAL", Privacy: "STANDARD", Engine: "COMFYUI", Release: "comfy-private-1", ModelHash: strings.Repeat("d", 64), RecipeHash: strings.Repeat("f", 64), Protocol: "openai_nb1", PublicSpec: contractspec.Selection{WorkflowID: "workflow_test", WorkflowRevision: 1, DurationMS: 120000}}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := rentalaccess.VerifyPreparation(raw, rentalaccess.LocalApproval{Profile: rentalaccess.ApprovalProfile, Manifest: m, ManifestHash: sum(raw), PhysicalHash: strings.Repeat("e", 64), Connection: "connection_test", Issuer: "issuer_test", PrepareUntil: now.Add(time.Minute)}, now)
	if e != nil {
		t.Fatal(e)
	}
	h, _, e := gate.ReceiveRentalPreparation(ctx, "prepare_test", prepared)
	if e != nil {
		t.Fatal(e)
	}
	// Synthetic already-approved prior maintenance selection. The published CE
	// activation tests independently exercise real maintenance transactions.
	r.mu.Lock()
	r.s.Active = []Selection{{Resource: h.Resource(), LocalGeneration: "1", Owner: "maintenance_prior", Current: p.Digest()}}
	e = r.persistLocked()
	r.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	return runtimeUseFixture{r, p, gate, h, prepared}
}
func (f runtimeUseFixture) open(t *testing.T) Use {
	t.Helper()
	u, e := f.registry.OpenRentalUse(context.Background(), f.gate, f.hold, f.prepared, f.plan)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(u.Close)
	return u
}
func TestComfyRuntimeUseRequiresSelectionAndOriginalPreparationGate(t *testing.T) {
	f := newRuntimeUseFixture(t)
	f.registry.mu.Lock()
	f.registry.s.Active = nil
	f.registry.mu.Unlock()
	if _, e := f.registry.OpenRentalUse(context.Background(), f.gate, f.hold, f.prepared, f.plan); e == nil {
		t.Fatal("unselected environment adopted")
	}
	if _, e := f.registry.OpenRentalUse(context.Background(), f.gate, execution.Hold{}, f.prepared, f.plan); e == nil {
		t.Fatal("forged zero physical hold adopted")
	}
}
func TestComfyRuntimeUseCopiesShareSingleClaimAndRejectConcurrentResource(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	alias := u
	joined := make(chan struct{})
	defer close(joined)
	if e := u.Claim(joined); e != nil {
		t.Fatal(e)
	}
	if u.state.plan.state != nil {
		t.Fatal("claimed reference retained large preparation inventory")
	}
	if alias.Claim(make(chan struct{})) == nil {
		t.Fatal("copied reference claimed twice")
	}
	if _, e := f.registry.OpenRentalUse(context.Background(), f.gate, f.hold, f.prepared, f.plan); e == nil {
		t.Fatal("same original physical resource opened twice")
	}
	alias.Close()
	if u.Context().Err() == nil {
		t.Fatal("alias close did not cancel common reference")
	}
	f.registry.mu.Lock()
	held := f.registry.uses[f.hold.Resource()] != nil
	f.registry.mu.Unlock()
	if !held {
		t.Fatal("cancel released directory before actual join")
	}
}
func TestComfyRuntimeUseCloseWaitsForActualClaimJoinBeforeKeyRelease(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	joined := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(joined) }) }
	defer finish()
	if e := u.Claim(joined); e != nil {
		t.Fatal(e)
	}
	closed := make(chan struct{})
	go func() { f.registry.Close(); close(closed) }()
	select {
	case <-u.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("registry did not revoke process lifetime")
	}
	select {
	case <-closed:
		t.Fatal("registry released original node key before native join")
	default:
	}
	f.registry.mu.Lock()
	retained := len(f.registry.key) == 32
	f.registry.mu.Unlock()
	if !retained {
		t.Fatal("original key prematurely erased")
	}
	finish()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("actual join did not release original registry")
	}
}
func TestComfyRuntimeDriftCancelsExistingClaimWithoutEarlyDirectoryRelease(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	joined := make(chan struct{})
	defer close(joined)
	if e := u.Claim(joined); e != nil {
		t.Fatal(e)
	}
	if e := writeNew(f.registry.journal.Root(), bundleRoot+"/environments/"+f.plan.Digest()+"/comfy/extra.py", []byte("UNAPPROVED")); e != nil {
		t.Fatal(e)
	}
	if f.registry.VerifyInstalled(context.Background(), f.plan) == nil || u.Context().Err() == nil {
		t.Fatal("drift left active environment usable")
	}
	if _, e := u.Layout(); e == nil {
		t.Fatal("cancelled claim exported runtime layout")
	}
	f.registry.mu.Lock()
	held := f.registry.uses[f.hold.Resource()] != nil
	f.registry.mu.Unlock()
	if !held {
		t.Fatal("drift released original directory before native join")
	}
}
func TestComfyRuntimeFileInventoryAndLayoutAreCopiesAndRedacted(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	layout, e := u.Layout()
	if e != nil {
		t.Fatal(e)
	}
	files := f.plan.RuntimeFiles()
	if len(files) != len(f.plan.state.files)+2 {
		t.Fatal("exact private metadata absent from inventory")
	}
	files[0].Path = "injected"
	layout.CoreClasses[0] = "Injected"
	next, e := u.Layout()
	if e != nil || next.CoreClasses[0] == "Injected" || f.plan.RuntimeFiles()[0].Path == "injected" {
		t.Fatal("exported copy mutated trusted plan")
	}
	raw, _ := json.Marshal(layout)
	if strings.Contains(fmt.Sprintf("%+v %#v", layout, u), layout.Root) || strings.Contains(string(raw), layout.Root) {
		t.Fatal("formatting exposed private runtime paths")
	}
}
