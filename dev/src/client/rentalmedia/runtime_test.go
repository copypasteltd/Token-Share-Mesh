package rentalmedia

// SOURCE_ONLY. Synthetic metadata/backends exercise supervision contracts;
// they never qualify a GPU, native media, ready, paid time, E2E or cleanup.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
)

type mediaControlFixture struct {
	ledger                         *execution.Store
	hold                           execution.Hold
	fixture                        rentaltest.Fixture
	sequence                       atomic.Uint64
	refreshEntered, refreshRelease chan struct{}
	scopeEntered, scopeRelease     chan struct{}
	stopped                        *atomic.Int32
}

func (c *mediaControlFixture) Hold() execution.Hold { return c.hold }
func (c *mediaControlFixture) CheckScope(ctx context.Context, scope string) error {
	if c.scopeRelease != nil {
		select {
		case c.scopeEntered <- struct{}{}:
		default:
		}
		<-c.scopeRelease
	}
	return c.ledger.CheckRental(ctx, c.hold, scope)
}
func (c *mediaControlFixture) RefreshControl(ctx context.Context) error {
	if c.refreshRelease != nil {
		select {
		case c.refreshEntered <- struct{}{}:
		default:
		}
		<-c.refreshRelease
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	now := time.Now().UTC()
	reply, e := c.fixture.Signer.SignControl(c.fixture.Metadata, c.sequence.Add(1), "ALLOW", now, now.Add(30*time.Second))
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(reply)
	proof, e := c.fixture.Verifier.VerifyControl(raw, c.fixture.Grant, c.fixture.Expected, now)
	if e != nil {
		return e
	}
	return c.ledger.ApplyRentalControl(ctx, c.hold, proof)
}
func (c *mediaControlFixture) Stop(ctx context.Context) error {
	if c.stopped.Load() == 0 {
		return ErrUnknown
	}
	return c.ledger.StopRental(ctx, c.hold)
}

type mediaBackendFixture struct {
	mu                                                        sync.Mutex
	ledger                                                    *execution.Store
	hold                                                      execution.Hold
	fixture                                                   rentaltest.Fixture
	policy                                                    rentalaccess.MediaPolicy
	stopped                                                   atomic.Int32
	submits                                                   atomic.Int32
	child                                                     *mediaChildFixture
	autoStop, success, cancelStops, hiddenReservation, badEnd bool
	submitEntered, submitRelease, joinRelease                 chan struct{}
}

func (b *mediaBackendFixture) Original() (execution.Hold, rentalaccess.Prepared, rentalaccess.MediaPolicy) {
	return b.hold, b.fixture.Prepared, b.policy
}
func (b *mediaBackendFixture) Submit(ctx context.Context, input *rentalaccess.MediaInput, current rentalaccess.Expected) (Child, error) {
	b.submits.Add(1)
	d := input.Descriptor()
	plain, e := input.Take(ctx, b.fixture.Prepared, current, time.Now())
	if e != nil {
		return nil, e
	}
	plain.Close()
	ticket, v, disposition, e := b.ledger.BeginRentalInvocation(ctx, b.hold, d.ID, d.InputCiphertextDigest, d.RecipeHash)
	if e != nil || disposition != "SUBMISSION_RESERVED" {
		return nil, ErrQuery
	}
	if _, e = ticket.Take(); e != nil {
		return nil, e
	}
	if b.submitRelease != nil {
		select {
		case b.submitEntered <- struct{}{}:
		default:
		}
		<-b.submitRelease
	}
	if b.hiddenReservation {
		return nil, ErrUnknown
	}
	child := &mediaChildFixture{backend: b, value: v, descriptor: d, expected: current, done: make(chan struct{}), success: b.success, result: &mediaResultFixture{}}
	if e = b.ledger.MarkRentalSubmitted(context.Background(), b.hold, v.ID, "media_synthetic"); e != nil {
		return nil, e
	}
	b.mu.Lock()
	b.child = child
	b.mu.Unlock()
	if b.autoStop {
		child.stop()
	}
	return child, nil
}
func (b *mediaBackendFixture) CancelOriginal(context.Context, string) error {
	b.mu.Lock()
	c := b.child
	b.mu.Unlock()
	if b.cancelStops && c != nil {
		c.stop()
	}
	return nil
}
func (b *mediaBackendFixture) RequestStop() {
	b.stopped.Add(1)
	b.mu.Lock()
	c := b.child
	b.mu.Unlock()
	if b.cancelStops && c != nil {
		c.stop()
	}
}
func (b *mediaBackendFixture) End(ctx context.Context) (SessionProof, error) {
	out, e := b.ledger.RentalOutstanding(ctx, b.hold)
	if e != nil || out.Invocations != 0 || out.Workspaces != 0 {
		return SessionProof{}, ErrUnknown
	}
	return SessionProof{Session: b.hold.Owner(), Resource: b.hold.Resource(), LocalGeneration: b.hold.LocalGeneration(), ManifestHash: strings.Repeat("a", 64), WorkStopped: true, FutureStartsRevoked: !b.badEnd, WorkspaceCleared: true, OutputKeysHandled: true}, nil
}
func (b *mediaBackendFixture) Join(context.Context) error {
	if b.joinRelease != nil {
		<-b.joinRelease
	}
	return nil
}

type mediaChildFixture struct {
	mu               sync.Mutex
	backend          *mediaBackendFixture
	value            execution.RentalInvocation
	descriptor       rentalaccess.MediaDescriptor
	expected         rentalaccess.Expected
	done             chan struct{}
	once             sync.Once
	stopped, success bool
	result           *mediaResultFixture
}

func (c *mediaChildFixture) Original() (execution.RentalInvocation, rentalaccess.MediaDescriptor, rentalaccess.Expected) {
	return c.value, c.descriptor, c.expected
}
func (c *mediaChildFixture) Done() <-chan struct{} { return c.done }
func (c *mediaChildFixture) stop() {
	c.once.Do(func() { c.mu.Lock(); c.stopped = true; c.mu.Unlock(); close(c.done) })
}
func (c *mediaChildFixture) Observation() execution.Observation {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := "EXECUTING"
	if c.stopped {
		state = "STOPPED"
	}
	return execution.Observation{RequestID: c.value.ID, BackendID: "media_synthetic", State: state}
}
func (c *mediaChildFixture) Succeeded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped && c.success
}
func (c *mediaChildFixture) Result() (Result, error) {
	if !c.Succeeded() {
		return nil, ErrClosed
	}
	return c.result, nil
}
func (c *mediaChildFixture) Cleanup(ctx context.Context) error {
	c.result.Close()
	b := c.backend
	if b.ledger.StopRentalInvocation(ctx, b.hold, c.value.ID) != nil {
		return ErrUnknown
	}
	if b.ledger.ObserveRentalInvocationStopped(ctx, b.hold, c.value.ID, c.Observation()) != nil {
		return ErrUnknown
	}
	return b.ledger.FinishRentalInvocationCleanup(ctx, b.hold, c.value.ID, execution.RentalChildCleanup{Session: c.value.Session, Invocation: c.value.ID, LocalGeneration: c.value.LocalGeneration, BackendID: "media_synthetic", ManifestHash: strings.Repeat("b", 64), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true})
}

type mediaResultFixture struct {
	mu             sync.Mutex
	opened, closed bool
	reader         *mediaReaderFixture
}

func (r *mediaResultFixture) Open(context.Context) (Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.opened || r.closed {
		return nil, ErrClosed
	}
	r.opened = true
	r.reader = &mediaReaderFixture{data: bytes.NewReader([]byte("synthetic-media"))}
	return r.reader, nil
}
func (r *mediaResultFixture) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.reader != nil {
		r.reader.Close()
	}
}

type mediaReaderFixture struct {
	mu     sync.Mutex
	data   *bytes.Reader
	closed bool
	closes atomic.Int32
}

func (r *mediaReaderFixture) Read(_ context.Context, b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, ErrClosed
	}
	return r.data.Read(b)
}
func (r *mediaReaderFixture) Seek(_ context.Context, o int64, w int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, ErrClosed
	}
	return r.data.Seek(o, w)
}
func (r *mediaReaderFixture) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.closes.Add(1)
	}
	return nil
}

type runtimeFixture struct {
	runtime *Runtime
	backend *mediaBackendFixture
	control *mediaControlFixture
	fixture rentaltest.Fixture
	ledger  *execution.Store
	hold    execution.Hold
	cancel  context.CancelFunc
}

func newMediaRuntime(t *testing.T, start bool) runtimeFixture {
	t.Helper()
	f := rentaltest.Media(t)
	ledger, h := f.Start(t, true)
	m := f.Prepared.Details()
	policy := rentalaccess.MediaPolicy{Profile: rentalaccess.MediaPolicyProfile, Workflow: m.PublicSpec.WorkflowID, Revision: m.PublicSpec.WorkflowRevision, Protocol: m.Protocol, Kind: "PNG", MaximumOutputBytes: 4096}
	b := &mediaBackendFixture{ledger: ledger, hold: h, fixture: f, policy: policy, autoStop: true, success: true, cancelStops: true}
	c := &mediaControlFixture{ledger: ledger, hold: h, fixture: f, stopped: &b.stopped}
	c.sequence.Store(1)
	watch, e := New(ledger, c, f.Prepared, policy, b, Options{Poll: 10 * time.Millisecond, MetadataTimeout: 200 * time.Millisecond, CallTimeout: 200 * time.Millisecond, ShutdownTimeout: time.Second, ResultRetention: time.Minute})
	if e != nil {
		t.Fatal(e)
	}
	life, cancel := context.WithCancel(context.Background())
	out := runtimeFixture{watch, b, c, f, ledger, h, cancel}
	if start {
		go func() { _ = watch.Run(life) }()
		<-watch.Ready()
	}
	t.Cleanup(func() {
		cancel()
		if watch.used.Load() {
			ctx, end := context.WithTimeout(context.Background(), 3*time.Second)
			defer end()
			_ = watch.Close(ctx)
		}
	})
	return out
}
func (f runtimeFixture) input(t *testing.T, id string) *rentalaccess.MediaInput {
	t.Helper()
	i, e := rentalaccess.SealMediaInput(context.Background(), f.fixture.Prepared, f.fixture.Verifier, f.fixture.Credential, f.fixture.Expected, id, f.hold.LocalGeneration(), f.backend.policy, []byte(`{"prompt":"synthetic private media"}`), nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(i.Close)
	return i
}
func waitMedia(t *testing.T, predicate func() bool) {
	t.Helper()
	limit := time.NewTimer(2 * time.Second)
	defer limit.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-limit.C:
			t.Fatal("synthetic media condition not reached")
		case <-tick.C:
		}
	}
}
func (f runtimeFixture) invoke(t *testing.T, id string) execution.RentalInvocation {
	t.Helper()
	v, e := f.runtime.Invoke(context.Background(), f.input(t, id))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestMediaSupervisorRejectsForeignLocalPolicyBeforeAdmission(t *testing.T) {
	f := newMediaRuntime(t, false)
	policy := f.backend.policy
	policy.Workflow = "foreign_workflow"
	if _, e := New(f.ledger, f.control, f.fixture.Prepared, policy, f.backend, Options{}); e == nil {
		t.Fatal("foreign policy admitted")
	}
}
func TestMediaSupervisorPreservesOriginalRentalAndQueryOnlyReplay(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	if v.Session != f.hold.Owner() || v.LocalGeneration != f.hold.LocalGeneration() {
		t.Fatal("original rental identity lost")
	}
	if _, e := f.runtime.Invoke(context.Background(), f.input(t, "invoke_media")); e == nil {
		t.Fatal("original input replay submitted")
	}
	if f.backend.submits.Load() != 1 {
		t.Fatal("backend called twice")
	}
	got, e := f.runtime.Query(context.Background(), v.ID)
	if e != nil || got.GrantDigest != f.fixture.Expected.TokenDigest {
		t.Fatal("original query changed grant", e)
	}
}
func TestMediaSupervisorRetainsParentUntilResultIsExplicitlyReleased(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	time.Sleep(30 * time.Millisecond)
	current, e := f.ledger.RentalCurrent(context.Background(), f.hold)
	if e != nil || current.Phase != "ACTIVE" {
		t.Fatal("child stopped released parent", e)
	}
	if e = f.runtime.Discard(context.Background(), v.ID); e != nil {
		t.Fatal(e)
	}
	waitMedia(t, func() bool {
		got, e := f.ledger.RentalInvocation(context.Background(), f.hold, v.ID)
		return e == nil && got.Phase == "CLEANED"
	})
	current, e = f.ledger.RentalCurrent(context.Background(), f.hold)
	if e != nil || current.Phase != "ACTIVE" {
		t.Fatal("child cleanup released parent", e)
	}
}
func TestMediaSupervisorCopiedOutputClosesOneOwnedReader(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	output, e := f.runtime.Output(context.Background(), v.ID)
	if e != nil {
		t.Fatal(e)
	}
	alias := *output
	if _, e = output.Read(context.Background(), make([]byte, 8)); e != nil {
		t.Fatal(e)
	}
	if output.Close() != nil || alias.Close() != nil {
		t.Fatal("shared output close")
	}
	if _, e = alias.Read(context.Background(), make([]byte, 1)); e == nil {
		t.Fatal("copied reader revived")
	}
	if f.backend.child.result.reader.closes.Load() != 1 {
		t.Fatal("actual reader closed more than once")
	}
}
func TestMediaSupervisorEOFDoesNotForgeBuyerAcceptanceOrRelease(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	output, e := f.runtime.Output(context.Background(), v.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer output.Close()
	buffer := make([]byte, 64)
	if _, e = output.Read(context.Background(), buffer); e != nil {
		t.Fatal(e)
	}
	if _, e = output.Read(context.Background(), buffer); e != io.EOF {
		t.Fatal("synthetic EOF")
	}
	f.runtime.mu.Lock()
	released := f.runtime.entries[v.ID].released
	f.runtime.mu.Unlock()
	if released {
		t.Fatal("EOF fabricated local close/ACK")
	}
}
func TestMediaSupervisorExpiryRevokesOpenOutputBeforeCleanup(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	output, e := f.runtime.Output(context.Background(), v.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.runtime.mu.Lock()
	f.runtime.entries[v.ID].retireAt = time.Now().Add(-time.Second)
	f.runtime.mu.Unlock()
	f.runtime.signal()
	waitMedia(t, func() bool { _, e := output.Read(context.Background(), make([]byte, 1)); return e != nil })
	waitMedia(t, func() bool {
		got, e := f.ledger.RentalInvocation(context.Background(), f.hold, v.ID)
		return e == nil && got.Phase == "CLEANED"
	})
}
func TestMediaSupervisorFailedStoppedChildNeedsNoInventedOutput(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.success = false
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.runtime.Run(life)
	<-f.runtime.Ready()
	defer func() {
		ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		f.runtime.Close(ctx)
	}()
	v := f.invoke(t, "invoke_media")
	waitMedia(t, func() bool {
		got, e := f.ledger.RentalInvocation(context.Background(), f.hold, v.ID)
		return e == nil && got.Phase == "CLEANED"
	})
	if _, e := f.runtime.Output(context.Background(), v.ID); e == nil {
		t.Fatal("failed child manufactured output")
	}
}
func TestMediaSupervisorCancelAcknowledgementAloneCannotClean(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.autoStop = false
	f.backend.cancelStops = false
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.runtime.Run(life)
	<-f.runtime.Ready()
	defer func() {
		f.backend.child.stop()
		ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		f.runtime.Close(ctx)
	}()
	v := f.invoke(t, "invoke_media")
	if e := f.runtime.Cancel(context.Background(), v.ID); e != nil {
		t.Fatal(e)
	}
	got, e := f.ledger.RentalInvocation(context.Background(), f.hold, v.ID)
	if e != nil || got.Phase != "STOPPING" {
		t.Fatal("cancel ACK implied stopped work", e)
	}
	f.backend.child.stop()
	f.runtime.signal()
	waitMedia(t, func() bool {
		got, e := f.ledger.RentalInvocation(context.Background(), f.hold, v.ID)
		return e == nil && got.Phase == "CLEANED"
	})
}
func TestMediaSupervisorDeadlineStopsBeforeBlockedControlExchangeJoins(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.control.refreshEntered = make(chan struct{}, 1)
	f.control.refreshRelease = make(chan struct{})
	f.control.scopeEntered = make(chan struct{}, 1)
	f.control.scopeRelease = make(chan struct{})
	releaseControl := sync.OnceFunc(func() { close(f.control.refreshRelease) })
	releaseScope := sync.OnceFunc(func() { close(f.control.scopeRelease) })
	defer releaseControl()
	defer releaseScope()
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.runtime.Run(life)
	<-f.runtime.Ready()
	<-f.control.refreshEntered
	// Wait until the main supervisor has observed the original metadata and
	// is blocked on an entered scope check. Its independent clock must stop
	// the native backend without this fake control exchange returning first.
	<-f.control.scopeEntered
	f.runtime.mu.Lock()
	f.runtime.deadline = time.Now().Add(40 * time.Millisecond)
	f.runtime.mu.Unlock()
	select {
	case f.runtime.deadlineWake <- struct{}{}:
	default:
	}
	waitMedia(t, func() bool { return f.backend.stopped.Load() != 0 })
	select {
	case <-f.runtime.Done():
		t.Fatal("reported done with original control still blocked")
	default:
	}
	releaseControl()
	releaseScope()
	select {
	case <-f.runtime.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("control exchange did not join")
	}
}
func TestMediaSupervisorLateControlCannotReviveExpiredMonotonicWindow(t *testing.T) {
	f := newMediaRuntime(t, false)
	state, e := f.ledger.RentalCurrent(context.Background(), f.hold)
	if e != nil {
		t.Fatal(e)
	}
	f.runtime.mu.Lock()
	f.runtime.deadline = time.Now().Add(-time.Second)
	f.runtime.mu.Unlock()
	if f.runtime.observe(state) == nil {
		t.Fatal("fresh metadata revived already expired window")
	}
}
func TestMediaSupervisorMissingChildAfterReservationRetainsOriginalOwner(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.hiddenReservation = true
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.runtime.Run(life)
	<-f.runtime.Ready()
	if _, e := f.runtime.Invoke(context.Background(), f.input(t, "invoke_media")); e == nil {
		t.Fatal("hidden reservation treated as success")
	}
	<-f.runtime.Done()
	snapshot, e := f.ledger.Snapshot(context.Background())
	if e != nil || len(snapshot.Resources) != 1 || snapshot.Resources[0].Phase == "FREE" {
		t.Fatal("missing child handle freed original owner", e)
	}
}
func TestMediaSupervisorBadSessionProofCannotReleaseParent(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.badEnd = true
	go f.runtime.Run(context.Background())
	<-f.runtime.Ready()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if f.runtime.Close(ctx) == nil {
		t.Fatal("incomplete native session proof accepted")
	}
	snapshot, e := f.ledger.Snapshot(context.Background())
	if e != nil || snapshot.Resources[0].Phase == "FREE" {
		t.Fatal("bad proof released owner", e)
	}
}
func TestMediaSupervisorSuccessfulSyntheticSessionUsesOriginalCleanupCAS(t *testing.T) {
	f := newMediaRuntime(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e := f.runtime.Close(ctx); e != nil {
		t.Fatal(e)
	}
	history, e := f.ledger.RentalHistory(context.Background(), f.hold)
	if e != nil || history.Phase != "CLEANED" {
		t.Fatal("original session metadata not cleaned", e)
	}
}
func TestMediaSupervisorWaitsActualBackendJoinAfterCallerTimeout(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.joinRelease = make(chan struct{})
	go f.runtime.Run(context.Background())
	<-f.runtime.Ready()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if f.runtime.Close(ctx) == nil {
		t.Fatal("timed out backend reported closed")
	}
	select {
	case <-f.runtime.Done():
		t.Fatal("Done before actual native join")
	default:
	}
	close(f.backend.joinRelease)
	select {
	case <-f.runtime.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("native join not observed")
	}
}

type inputOwnerFixture struct {
	stopped chan struct{}
	release chan struct{}
	once    sync.Once
}

func (o *inputOwnerFixture) StopInputs() { o.once.Do(func() { close(o.stopped) }) }
func (o *inputOwnerFixture) JoinInputs(ctx context.Context) error {
	select {
	case <-o.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestMediaSupervisorUnsubmittedInputOwnerMustJoinBeforeParentRelease(t *testing.T) {
	f := newMediaRuntime(t, false)
	owner := &inputOwnerFixture{stopped: make(chan struct{}), release: make(chan struct{})}
	if f.runtime.BindInputOwner(owner) != nil {
		t.Fatal("bind owner")
	}
	go f.runtime.Run(context.Background())
	<-f.runtime.Ready()
	f.runtime.RequestStop()
	<-owner.stopped
	snapshot, e := f.ledger.Snapshot(context.Background())
	if e != nil || snapshot.Resources[0].Phase == "FREE" {
		t.Fatal("released parent with input owner still active", e)
	}
	close(owner.release)
	select {
	case <-f.runtime.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("input owner join not observed")
	}
}
func TestMediaSupervisorCannotReplaceInputOwnerAfterWatchStarts(t *testing.T) {
	f := newMediaRuntime(t, true)
	owner := &inputOwnerFixture{stopped: make(chan struct{}), release: make(chan struct{})}
	if f.runtime.BindInputOwner(owner) == nil {
		t.Fatal("replaced ownership after admission started")
	}
}

func TestMediaSupervisorEnteredSubmitKeepsDoneAndOwnerUntilActualReturn(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.submitEntered = make(chan struct{}, 1)
	f.backend.submitRelease = make(chan struct{})
	release := sync.OnceFunc(func() { close(f.backend.submitRelease) })
	defer release()
	go f.runtime.Run(context.Background())
	<-f.runtime.Ready()
	input := f.input(t, "invoke_media")
	returned := make(chan struct{})
	go func() { defer close(returned); _, _ = f.runtime.Invoke(context.Background(), input) }()
	<-f.backend.submitEntered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if f.runtime.Close(ctx) == nil {
		t.Fatal("entered sender cancellation reported actual join")
	}
	select {
	case <-f.runtime.Done():
		t.Fatal("Done before entered submit returned")
	default:
	}
	snapshot, e := f.ledger.Snapshot(context.Background())
	if e != nil || snapshot.Resources[0].Phase == "FREE" {
		t.Fatal("pending original submission released resource", e)
	}
	release()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("entered submit did not return")
	}
	select {
	case <-f.runtime.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("entered submit was not joined")
	}
}
