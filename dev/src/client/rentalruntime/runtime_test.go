package rentalruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalruntime"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

// A synthetic backend supplies no actual STOPPED, GPU, funding or cleanup
// qualification. The tests combine its explicitly synthetic observations with
// the actual local SQLite fence and typed input/crypto implementation.
type backend struct {
	mu               sync.Mutex
	f                rentaltest.Fixture
	states           map[string]string
	revoked          map[string]bool
	submits, cancels int
	unknown          bool
	joined           chan struct{}
	canceled         chan struct{}
}

func (b *backend) Identity() rentalruntime.Identity {
	p := b.f.Prepared
	m := p.Details()
	return rentalruntime.Identity{ManifestHash: p.Digest(), PhysicalHash: p.Approval().PhysicalHash, EnvironmentHash: m.EnvironmentHash, ModelHash: m.ModelHash, RecipeHash: m.RecipeHash, PolicyHash: b.f.Policy.Digest()}
}
func (b *backend) SubmitText(ctx context.Context, w rentalruntime.Work, i *rentalaccess.TextInput) (rentalruntime.Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revoked[w.Invocation.ID] {
		return rentalruntime.Observation{RequestID: w.Invocation.ID, State: "ABSENT"}, rentalruntime.ErrClosed
	}
	raw, e := i.Take(ctx, b.f.Prepared, w.Expected, time.Now())
	clear(raw)
	if e != nil {
		return rentalruntime.Observation{}, e
	}
	b.submits++
	b.states[w.Invocation.ID] = "RUNNING"
	return rentalruntime.Observation{RequestID: w.Invocation.ID, BackendID: "backend_" + w.Invocation.ID, State: "RUNNING"}, nil
}
func (b *backend) Query(ctx context.Context, w rentalruntime.Work) (rentalruntime.Observation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil || b.unknown {
		return rentalruntime.Observation{}, rentalruntime.ErrBackend
	}
	state := b.states[w.Invocation.ID]
	if state == "" {
		if b.revoked[w.Invocation.ID] {
			return rentalruntime.Observation{RequestID: w.Invocation.ID, State: "ABSENT"}, nil
		}
		return rentalruntime.Observation{}, rentalruntime.ErrBackend
	}
	return rentalruntime.Observation{RequestID: w.Invocation.ID, BackendID: "backend_" + w.Invocation.ID, State: state}, nil
}
func (b *backend) Cancel(ctx context.Context, w rentalruntime.Work) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancels++
	select {
	case b.canceled <- struct{}{}:
	default:
	}
	b.revoked[w.Invocation.ID] = true
	if b.states[w.Invocation.ID] != "" && !b.unknown {
		b.states[w.Invocation.ID] = "STOPPED"
	}
	return ctx.Err()
}
func (b *backend) Cleanup(ctx context.Context, w rentalruntime.Work, o rentalruntime.Observation) (rentalruntime.ChildProof, error) {
	q, e := b.Query(ctx, w)
	if e != nil || q != o || o.State != "STOPPED" && o.State != "ABSENT" {
		return rentalruntime.ChildProof{}, rentalruntime.ErrBackend
	}
	return rentalruntime.ChildProof{Session: w.Invocation.Session, Invocation: w.Invocation.ID, LocalGeneration: w.Invocation.LocalGeneration, BackendID: o.BackendID, ManifestHash: strings.Repeat("7", 64), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}, nil
}
func (b *backend) Started(context.Context, rentalruntime.Work) (supervisor.TextStart, error) {
	return supervisor.TextStart{InputTokens: 12}, nil
}
func (b *backend) Read(ctx context.Context, w rentalruntime.Work) ([]byte, error) {
	q, e := b.Query(ctx, w)
	if e != nil {
		return nil, e
	}
	if q.State == "STOPPED" {
		return nil, io.EOF
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *backend) Result(rentalruntime.Work) (supervisor.TextResult, error) {
	return supervisor.TextResult{InputTokens: 12, OutputTokens: 1, Reason: "eog", StopIndex: supervisor.NoStopIndex}, nil
}
func (b *backend) CloseSession(ctx context.Context, h execution.Hold) (rentalruntime.SessionProof, error) {
	return rentalruntime.SessionProof{Session: h.Owner(), Resource: h.Resource(), LocalGeneration: h.LocalGeneration(), ManifestHash: strings.Repeat("8", 64), WorkStopped: true, FutureStartsRevoked: true, WorkspaceCleared: true, OutputKeysHandled: true}, ctx.Err()
}
func (b *backend) Join(ctx context.Context) error {
	select {
	case <-b.joined:
		return nil
	case <-ctx.Done():
		return rentalruntime.ErrBackend
	}
}

type control struct {
	s *execution.Store
	h execution.Hold
}

func (c control) Hold() execution.Hold { return c.h }
func (c control) CheckScope(ctx context.Context, scope string) error {
	return c.s.CheckRental(ctx, c.h, scope)
}
func (c control) RefreshControl(context.Context) error { return nil }
func (c control) Stop(ctx context.Context) error       { return c.s.StopRental(ctx, c.h) }

func start(t *testing.T, allow, join bool) (rentaltest.Fixture, *execution.Store, execution.Hold, *backend, *rentalruntime.Runtime) {
	t.Helper()
	f := rentaltest.Text(t, relay.ChatText)
	s, h := f.Start(t, allow)
	b := &backend{f: f, states: map[string]string{}, revoked: map[string]bool{}, joined: make(chan struct{}), canceled: make(chan struct{}, 4)}
	if join {
		close(b.joined)
	}
	r, e := rentalruntime.New(s, control{s, h}, f.Prepared, f.Policy, b, rentalruntime.Options{Poll: 20 * time.Millisecond, ControlPoll: time.Second, CallTimeout: 50 * time.Millisecond, MetadataTimeout: time.Second, DrainGrace: 100 * time.Millisecond, ShutdownTimeout: 500 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	select {
	case <-r.Ready():
	case <-time.After(time.Second):
		t.Fatal("watchdog never started")
	}
	t.Cleanup(func() {
		cancel()
		stop, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		r.Close(stop)
	})
	return f, s, h, b, r
}
func waitChild(t *testing.T, r *rentalruntime.Runtime, id, phase string) {
	t.Helper()
	limit := time.Now().Add(2 * time.Second)
	for time.Now().Before(limit) {
		v, e := r.Query(context.Background(), id)
		if e == nil && v.Phase == phase {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("original child never reached", phase)
}

func TestRentalRuntimeNoInitialAllowNeverEntersBackend(t *testing.T) {
	f, _, h, b, r := start(t, false, true)
	if _, e := r.Invoke(context.Background(), f.Input(t, "invocation_no_allow", h.LocalGeneration())); e == nil {
		t.Fatal("initial grant became execution authority without control")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.submits != 0 {
		t.Fatal("unapproved inference entered backend")
	}
}
func TestRentalRuntimeChildCleanupRetainsParentAndOriginalQuery(t *testing.T) {
	f, s, h, b, r := start(t, true, true)
	id := "invocation_first"
	if _, e := r.Invoke(context.Background(), f.Input(t, id, h.LocalGeneration())); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Invoke(context.Background(), f.Input(t, "invocation_busy", h.LocalGeneration())); e == nil {
		t.Fatal("parallel rental child admitted")
	}
	q, e := r.Query(context.Background(), id)
	if e != nil || q.BackendID != "backend_"+id {
		t.Fatal("query rebound original metadata", e)
	}
	b.mu.Lock()
	b.states[id] = "STOPPED"
	b.mu.Unlock()
	o, e := r.Output(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	if data, e := o.Read(context.Background()); e != io.EOF || len(data) != 0 {
		clear(data)
		t.Fatal("terminal output not drained", e)
	}
	waitChild(t, r, id, "CLEANED")
	if _, e = s.AcquireLocal(context.Background(), h.Resource(), "local_intruder", "LOCAL"); !errors.Is(e, execution.ErrBusy) {
		t.Fatal("child cleanup released parent physical slot", e)
	}
	if _, e = r.Invoke(context.Background(), f.Input(t, "invocation_second", h.LocalGeneration())); e != nil {
		t.Fatal("next serial invocation denied after cleanup", e)
	}
	if e = r.Cancel(context.Background(), "invocation_second"); e != nil {
		t.Fatal(e)
	}
	waitChild(t, r, "invocation_second", "CLEANED")
}
func TestRentalRuntimeDisconnectedReaderStopsOnlyOriginalChild(t *testing.T) {
	f, s, h, b, r := start(t, true, true)
	id := "invocation_disconnect"
	if _, e := r.Invoke(context.Background(), f.Input(t, id, h.LocalGeneration())); e != nil {
		t.Fatal(e)
	}
	o, e := r.Output(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, e := o.Read(ctx); e == nil {
		clear(data)
		t.Fatal("disconnected reader still returned text")
	}
	waitChild(t, r, id, "CLEANED")
	b.mu.Lock()
	n := b.cancels
	b.mu.Unlock()
	if n == 0 {
		t.Fatal("reader cancellation did not reach backend")
	}
	if _, e = s.AcquireLocal(context.Background(), h.Resource(), "local_intruder", "LOCAL"); !errors.Is(e, execution.ErrBusy) {
		t.Fatal("disconnect released rental parent", e)
	}
}
func TestRentalRuntimeUnknownStopRetainsOriginalPhysicalOwner(t *testing.T) {
	f, s, h, b, r := start(t, true, true)
	if _, e := r.Invoke(context.Background(), f.Input(t, "invocation_unknown", h.LocalGeneration())); e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	b.unknown = true
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if r.Close(ctx) == nil {
		t.Fatal("unknown native outcome reported clean")
	}
	status, e := s.Snapshot(context.Background())
	if e != nil || status.Resources[0].Mode != "RENTAL" || status.Resources[0].Phase == "FREE" {
		t.Fatal("unconfirmed original physical slot released", e)
	}
}
func TestRentalRuntimeCloseWaitsForOwnedJoinBeforeRelease(t *testing.T) {
	_, s, h, b, r := start(t, true, false)
	defer close(b.joined)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if r.Close(ctx) == nil {
		t.Fatal("incomplete native join reported completion")
	}
	select {
	case <-r.Done():
		t.Fatal("Done closed while an owned operation still exists")
	default:
	}
	if _, e := s.AcquireLocal(context.Background(), h.Resource(), "local_intruder", "LOCAL"); !errors.Is(e, execution.ErrBusy) {
		t.Fatal("process join timeout released physical owner", e)
	}
}

type blockedControl struct {
	control
	blocked          atomic.Bool
	entered, release chan struct{}
}

func (c *blockedControl) CheckScope(ctx context.Context, scope string) error {
	if c.blocked.Load() {
		select {
		case c.entered <- struct{}{}:
		default:
		}
		<-c.release // A synthetic stuck local call, deliberately ignoring ctx.
	}
	return c.control.CheckScope(ctx, scope)
}

func TestRentalRuntimeLocalDeadlineCancelsWhileMetadataCheckIsStuck(t *testing.T) {
	f := rentaltest.Text(t, relay.ChatText)
	s, h := f.Start(t, true)
	now := time.Now().UTC()
	signed, e := f.Signer.SignControl(f.Metadata, 2, "ALLOW", now, now.Add(800*time.Millisecond))
	if e != nil {
		t.Fatal(e)
	}
	wire, e := json.Marshal(signed)
	if e != nil {
		t.Fatal(e)
	}
	proof, e := f.Verifier.VerifyControl(wire, f.Grant, f.Expected, now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ApplyRentalControl(context.Background(), h, proof); e != nil {
		t.Fatal(e)
	}
	b := &backend{f: f, states: map[string]string{}, revoked: map[string]bool{}, joined: make(chan struct{}), canceled: make(chan struct{}, 4)}
	close(b.joined)
	c := &blockedControl{control: control{s, h}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	r, e := rentalruntime.New(s, c, f.Prepared, f.Policy, b, rentalruntime.Options{Poll: 20 * time.Millisecond, MetadataTimeout: time.Second, CallTimeout: 50 * time.Millisecond, ShutdownTimeout: 500 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	t.Cleanup(func() {
		cancel()
		stop, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		r.Close(stop)
	})
	defer close(c.release)
	select {
	case <-r.Ready():
	case <-time.After(time.Second):
		t.Fatal("watchdog not started")
	}
	if _, e = r.Invoke(context.Background(), f.Input(t, "invocation_deadline", h.LocalGeneration())); e != nil {
		t.Fatal(e)
	}
	c.blocked.Store(true)
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("synthetic metadata barrier not entered")
	}
	select {
	case <-b.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("local deadline waited for blocked metadata")
	}
	// The wait is a test timeout, not a claim about measured GPU stop latency.
}
