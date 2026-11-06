package llamaworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalruntime"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/gpuid"
)

type rentalEntry struct {
	work                                         rentalruntime.Work
	life                                         context.Context
	cancel                                       context.CancelFunc
	done                                         chan struct{}
	pending, entered, revoked, rejected, cleaned bool
	cleanupHash                                  string
}

// RentalAdapter exclusively owns its worker for one original RENTAL session.
// No fabricated executionlease.Manifest/JOB is used; only normalized encoding
// and native metadata keys are shared with the original T1 adapter.
type RentalAdapter struct {
	mu       sync.Mutex
	closing  sync.Mutex
	native   *Adapter
	prepared rentalaccess.Prepared
	policy   rentalaccess.TextPolicy
	identity rentalruntime.Identity
	jobs     map[string]*rentalEntry
	closed   bool
	proof    rentalruntime.SessionProof
}

var _ rentalruntime.Backend = (*RentalAdapter)(nil)

// OpenRental is an explicit locally approved preparation action. The caller
// must already own the original shared physical RENTAL slot and its bounded
// preparation context. No public request may manufacture c/p/policy approval.
func OpenRental(ctx context.Context, c Config, p rentalaccess.Prepared, policy rentalaccess.TextPolicy) (*RentalAdapter, error) {
	m := p.Details()
	physical, e := gpuid.Digest(c.GPUUUID)
	if e != nil || !p.ValidAt(time.Now()) || !policy.Binds(p) || physical != p.Approval().PhysicalHash || c.GPULayers == 0 ||
		m.Engine != "LLAMA_CPP_NATIVE" || m.Release != EngineRelease || m.Resource != c.Resource || m.Capability != c.Capability || m.EnvironmentHash != c.Environment || m.ModelHash != c.Model.SHA256 || m.RecipeHash != c.Template.SHA256 ||
		uint64(policy.InputTokenLimit)+uint64(policy.OutputTokenLimit) > uint64(c.ContextTokens) {
		return nil, ErrConfig
	}
	a, e := Open(ctx, c)
	if e != nil {
		return nil, e
	}
	i := a.LocalInfo()
	if i.PhysicalDigest != physical || i.ObservedGPU.UUID != c.GPUUUID || i.ObservedGPU.PCI != c.GPUPCI || i.ObservedGPU.Device != c.GPUDevice || i.SelectedGPULayers != c.GPULayers || i.Environment != m.EnvironmentHash || i.EngineRelease != m.Release || i.ModelSHA256 != m.ModelHash || i.TemplateSHA256 != m.RecipeHash {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.Close(stop)
		cancel()
		a.Join(context.Background())
		return nil, ErrUnknown
	}
	return newRentalAdapter(a, p, policy, physical), nil
}
func newRentalAdapter(a *Adapter, p rentalaccess.Prepared, policy rentalaccess.TextPolicy, physical string) *RentalAdapter {
	m := p.Details()
	return &RentalAdapter{native: a, prepared: p, policy: policy, jobs: map[string]*rentalEntry{}, identity: rentalruntime.Identity{ManifestHash: p.Digest(), PhysicalHash: physical, EnvironmentHash: m.EnvironmentHash, ModelHash: m.ModelHash, RecipeHash: m.RecipeHash, PolicyHash: policy.Digest()}}
}
func (a *RentalAdapter) Identity() rentalruntime.Identity { return a.identity }
func (a *RentalAdapter) LocalInfo() LocalRuntimeInfo      { return a.native.LocalInfo() }
func (a *RentalAdapter) Join(ctx context.Context) error   { return a.native.Join(ctx) }
func rentalNativeWork(w rentalruntime.Work) supervisor.Work {
	return supervisor.Work{RequestID: w.Invocation.ID, Resource: w.Invocation.Resource, LocalGeneration: w.Invocation.LocalGeneration, Environment: w.Descriptor.EnvironmentHash}
}
func (a *RentalAdapter) bound(w rentalruntime.Work) bool {
	return rentalruntime.BoundWork(w, a.prepared, a.policy)
}

func (a *RentalAdapter) SubmitText(ctx context.Context, w rentalruntime.Work, input *rentalaccess.TextInput) (out rentalruntime.Observation, result error) {
	if input == nil || !a.bound(w) || input.Descriptor() != w.Descriptor || ctx.Err() != nil {
		return out, ErrInput
	}
	defer input.Close()
	a.mu.Lock()
	if a.closed || a.jobs[w.Invocation.ID] != nil || len(a.jobs) >= maxJobs {
		a.mu.Unlock()
		return out, ErrWork
	}
	life, cancel := context.WithCancel(context.Background())
	j := &rentalEntry{work: w, life: life, cancel: cancel, done: make(chan struct{}), pending: true}
	a.jobs[w.Invocation.ID] = j
	a.mu.Unlock()
	stopCaller := context.AfterFunc(ctx, cancel)
	defer stopCaller()
	defer func() {
		input.Close()
		a.mu.Lock()
		j.pending = false
		if !j.entered {
			j.rejected = true
			j.revoked = true
			cancel()
		}
		close(j.done)
		a.mu.Unlock()
	}()
	raw, e := input.Take(life, a.prepared, w.Expected, time.Now())
	if e != nil {
		return out, ErrInput
	}
	defer clear(raw)
	d := w.Descriptor
	normal, e := normalizeText(life, d.Spec.Protocol, d.Spec.PublicModel, d.InputTokenLimit, d.Spec.MaxOutputTokens, d.Spec.Stream, raw)
	if e != nil {
		return out, e
	}
	hard, e := time.Parse(time.RFC3339Nano, w.Expected.HardDeadline)
	if e != nil || !time.Now().Before(hard) {
		clear(normal)
		return out, ErrInput
	}
	a.mu.Lock()
	if a.closed || j.revoked || life.Err() != nil || ctx.Err() != nil {
		a.mu.Unlock()
		clear(normal)
		return out, ErrInput
	}
	j.entered = true
	a.mu.Unlock()
	// This context is owned for the whole invocation. A cancellation persists
	// even if it wins before native entry; the native control pipe also retains
	// original-ID cancellation tombstones for a not-yet-started request.
	o, e := a.native.submitNormalized(life, rentalNativeWork(w), d.Digest(), d.InputTokenLimit, uint32(d.Spec.MaxOutputTokens), normal, hard, true)
	if errors.Is(e, ErrCapacity) {
		a.mu.Lock()
		j.entered = false
		j.rejected = true
		j.revoked = true
		cancel()
		a.mu.Unlock()
	}
	return rentalruntime.Observation{RequestID: o.RequestID, BackendID: o.BackendID, State: o.State}, e
}

func (a *RentalAdapter) Query(ctx context.Context, w rentalruntime.Work) (rentalruntime.Observation, error) {
	if ctx.Err() != nil || !a.bound(w) {
		return rentalruntime.Observation{}, ErrWork
	}
	a.mu.Lock()
	j := a.jobs[w.Invocation.ID]
	if j == nil || j.work != w || j.pending {
		a.mu.Unlock()
		return rentalruntime.Observation{RequestID: w.Invocation.ID, State: "UNKNOWN"}, ErrUnknown
	}
	absent := !j.entered && (j.revoked || j.rejected)
	a.mu.Unlock()
	if absent {
		return rentalruntime.Observation{RequestID: w.Invocation.ID, State: "ABSENT"}, nil
	}
	o, e := a.native.Query(ctx, rentalNativeWork(w))
	return rentalruntime.Observation{RequestID: o.RequestID, BackendID: o.BackendID, State: o.State}, e
}
func (a *RentalAdapter) Cancel(ctx context.Context, w rentalruntime.Work) error {
	if ctx.Err() != nil || !a.bound(w) {
		return ErrWork
	}
	a.mu.Lock()
	j := a.jobs[w.Invocation.ID]
	if j == nil {
		if len(a.jobs) >= maxJobs {
			a.mu.Unlock()
			return ErrUnknown
		}
		done := make(chan struct{})
		close(done)
		a.jobs[w.Invocation.ID] = &rentalEntry{work: w, done: done, revoked: true, rejected: true}
		a.mu.Unlock()
		return nil
	}
	if j.work != w {
		a.mu.Unlock()
		return ErrWork
	}
	j.revoked = true
	if j.cancel != nil {
		j.cancel()
	}
	entered := j.entered
	a.mu.Unlock()
	if entered {
		return a.native.Cancel(ctx, rentalNativeWork(w))
	}
	return nil
}

func rentalProofHash(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (a *RentalAdapter) Cleanup(ctx context.Context, w rentalruntime.Work, o rentalruntime.Observation) (rentalruntime.ChildProof, error) {
	if !a.bound(w) {
		return rentalruntime.ChildProof{}, ErrWork
	}
	if e := a.Cancel(ctx, w); e != nil {
		return rentalruntime.ChildProof{}, e
	}
	actual, e := a.Query(ctx, w)
	if e != nil || actual != o || (o.State != "STOPPED" && o.State != "ABSENT") {
		return rentalruntime.ChildProof{}, ErrUnknown
	}
	a.mu.Lock()
	j := a.jobs[w.Invocation.ID]
	entered := j.entered
	a.mu.Unlock()
	if entered {
		c, e := a.native.Cleanup(ctx, rentalNativeWork(w), supervisor.Observation{RequestID: o.RequestID, BackendID: o.BackendID, State: o.State})
		if e != nil || !c.WorkStopped || !c.TokensRevoked || !c.WorkspaceCleared || c.ManifestHash != w.Descriptor.Digest() {
			return rentalruntime.ChildProof{}, ErrUnknown
		}
	}
	proof := rentalruntime.ChildProof{Session: w.Invocation.Session, Invocation: w.Invocation.ID, LocalGeneration: w.Invocation.LocalGeneration, BackendID: o.BackendID, WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}
	proof.ManifestHash = rentalProofHash(struct {
		Work            rentalruntime.Work
		Observation     rentalruntime.Observation
		NativeInputHash string
	}{w, o, w.Descriptor.Digest()})
	a.mu.Lock()
	j.cleaned = true
	j.cleanupHash = proof.ManifestHash
	a.mu.Unlock()
	return proof, nil
}

func (a *RentalAdapter) readable(w rentalruntime.Work) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	j := a.jobs[w.Invocation.ID]
	return j != nil && j.work == w && j.entered
}
func (a *RentalAdapter) Started(ctx context.Context, w rentalruntime.Work) (supervisor.TextStart, error) {
	if !a.readable(w) {
		return supervisor.TextStart{}, ErrWork
	}
	return a.native.Started(ctx, rentalNativeWork(w))
}
func (a *RentalAdapter) Read(ctx context.Context, w rentalruntime.Work) ([]byte, error) {
	if !a.readable(w) {
		return nil, ErrWork
	}
	return a.native.Read(ctx, rentalNativeWork(w))
}
func (a *RentalAdapter) Result(w rentalruntime.Work) (supervisor.TextResult, error) {
	if !a.readable(w) {
		return supervisor.TextResult{}, ErrWork
	}
	return a.native.Result(rentalNativeWork(w))
}

// CloseSession prevents all future submissions before inspecting its exact
// original entries. Only synchronized STOPPED/clear receipts or an authoritative
// pre-entry rejection qualify. Process kill/exit without them remains UNKNOWN.
func (a *RentalAdapter) CloseSession(ctx context.Context, h execution.Hold) (rentalruntime.SessionProof, error) {
	a.closing.Lock()
	defer a.closing.Unlock()
	m := a.prepared.Details()
	if h.Owner() != m.Session || h.Resource() != m.Resource || h.LocalGeneration() == "" {
		return rentalruntime.SessionProof{}, ErrWork
	}
	a.mu.Lock()
	a.closed = true
	if a.proof.ManifestHash != "" {
		proof := a.proof
		a.mu.Unlock()
		if proof.LocalGeneration != h.LocalGeneration() {
			return rentalruntime.SessionProof{}, ErrWork
		}
		return proof, nil
	}
	entries := make([]*rentalEntry, 0, len(a.jobs))
	for _, j := range a.jobs {
		j.revoked = true
		if j.cancel != nil {
			j.cancel()
		}
		entries = append(entries, j)
	}
	a.mu.Unlock()
	for _, j := range entries {
		select {
		case <-j.done:
		case <-ctx.Done():
			a.native.Close(ctx)
			return rentalruntime.SessionProof{}, ErrUnknown
		}
	}
	a.mu.Lock()
	hashes := make([]string, 0, len(entries))
	valid := true
	for _, j := range entries {
		if j.pending || j.work.Invocation.LocalGeneration != h.LocalGeneration() || j.entered && !j.cleaned {
			valid = false
		}
		hashes = append(hashes, j.work.Descriptor.Digest()+":"+j.cleanupHash)
	}
	a.mu.Unlock()
	e := a.native.Close(ctx)
	if !valid || e != nil || a.native.Join(ctx) != nil {
		return rentalruntime.SessionProof{}, ErrUnknown
	}
	sort.Strings(hashes)
	proof := rentalruntime.SessionProof{Session: m.Session, Resource: m.Resource, LocalGeneration: h.LocalGeneration(), WorkStopped: true, FutureStartsRevoked: true, WorkspaceCleared: true, OutputKeysHandled: true}
	proof.ManifestHash = rentalProofHash(struct {
		Profile, Session, Resource, Local, Manifest string
		Identity                                    rentalruntime.Identity
		Children                                    []string
	}{"P11_NATIVE_RENTAL_CLEANUP_1", m.Session, m.Resource, h.LocalGeneration(), a.prepared.Digest(), a.identity, hashes})
	a.mu.Lock()
	a.proof = proof
	a.mu.Unlock()
	return proof, nil
}
