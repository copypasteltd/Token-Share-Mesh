package supervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Synthetic owned-engine fixture: it performs no inference, file deletion,
// key revocation or GPU work. Tests exercise the real loop and SQLite journal.
type backend struct {
	mu                                                                                        sync.Mutex
	state                                                                                     string
	work                                                                                      Work
	submits, queries, cancels, cleans                                                         int
	lost, blockSubmit, blockQuery, queryError, cancelNoStop, badOwner, badCleanup, cleanError bool
	queryStarted                                                                              chan struct{}
	started                                                                                   chan struct{}
	startOnce, queryOnce                                                                      sync.Once
}

func newBackend() *backend {
	return &backend{state: "ABSENT", started: make(chan struct{}), queryStarted: make(chan struct{})}
}
func (b *backend) obs(w Work) Observation {
	id := ""
	if b.state != "ABSENT" {
		id = "backend_" + w.RequestID
	}
	request := w.RequestID
	if b.badOwner {
		request = "other_request"
	}
	return Observation{request, id, b.state}
}
func (b *backend) Submit(ctx context.Context, w Work) (Observation, error) {
	b.mu.Lock()
	b.submits++
	b.work = w
	b.state = "RUNNING"
	block, lost := b.blockSubmit, b.lost
	o := b.obs(w)
	b.mu.Unlock()
	b.startOnce.Do(func() { close(b.started) })
	if block {
		<-ctx.Done()
		return Observation{}, ctx.Err()
	}
	if lost {
		return Observation{}, errors.New("synthetic lost acknowledgement")
	}
	return o, nil
}
func (b *backend) Query(ctx context.Context, w Work) (Observation, error) {
	b.mu.Lock()
	b.queries++
	block, fail := b.blockQuery, b.queryError
	o := b.obs(w)
	b.mu.Unlock()
	b.queryOnce.Do(func() { close(b.queryStarted) })
	if block {
		<-ctx.Done()
		return Observation{}, ctx.Err()
	}
	if fail {
		return Observation{}, errors.New("synthetic query unavailable")
	}
	return o, nil
}
func (b *backend) Cancel(ctx context.Context, w Work) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancels++
	if b.state == "RUNNING" && !b.cancelNoStop {
		b.state = "STOPPED"
	}
	return nil
}
func (b *backend) Cleanup(ctx context.Context, w Work, o Observation) (Cleaned, error) {
	if ctx.Err() != nil {
		return Cleaned{}, ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleans++
	if b.cleanError {
		return Cleaned{}, errors.New("synthetic cleanup unavailable")
	}
	c := Cleaned{RequestID: w.RequestID, BackendID: o.BackendID, LocalGeneration: w.LocalGeneration, ManifestHash: strings.Repeat("f", 64), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}
	if b.badCleanup {
		c.LocalGeneration = "0"
		c.TokensRevoked = false
	}
	return c, nil
}
func (b *backend) counts() (int, int, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.submits, b.queries, b.cancels, b.cleans
}

type manualClock struct {
	mu    sync.Mutex
	stamp execution.Stamp
}

func (c *manualClock) Sample() execution.Stamp { c.mu.Lock(); defer c.mu.Unlock(); return c.stamp }
func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stamp.Wall = c.stamp.Wall.Add(d)
	c.stamp.Elapsed += d
}

type fixture struct {
	store   *execution.Store
	clock   *manualClock
	path    string
	binding execution.Binding
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{path: filepath.Join(t.TempDir(), "execution.db"), binding: execution.Binding{Installation: strings.Repeat("a", 64), LedgerID: strings.Repeat("d", 64), Device: "device_test", Seller: "seller_test"}, clock: &manualClock{stamp: execution.Stamp{Wall: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}}}
	if e := os.WriteFile(f.path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	var e error
	f.store, e = execution.Open(context.Background(), f.path, f.binding, true, f.clock)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.store.Close() })
	if e = f.store.RegisterResource(context.Background(), "resource_test", strings.Repeat("e", 64)); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *fixture) lease(t *testing.T, id string) executionlease.Verified {
	t.Helper()
	now := f.clock.Sample().Wall
	l := executionlease.Lease{ID: id, Job: "job_" + id, Attempt: "attempt_" + id, Order: "order_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "1", Capability: "cap_test", Manifest: "manifest_test", ManifestHash: strings.Repeat("1", 64), Environment: strings.Repeat("2", 64), Revision: 1, Issued: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano), Fence: "1", Expires: now.Add(45 * time.Second).Format(time.RFC3339Nano), ContractHash: strings.Repeat("3", 64), Profile: executionlease.Profile}
	return sign(t, l, now)
}
func sign(t *testing.T, l executionlease.Lease, now time.Time) executionlease.Verified {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{47}, 32))
	m, e := executionlease.Message(l)
	if e != nil {
		t.Fatal(e)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, m))
	raw, _ := json.Marshal(l)
	hard, _ := time.Parse(time.RFC3339Nano, l.HardDeadline)
	p := executionlease.Plan{Job: l.Job, Attempt: l.Attempt, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Capability: l.Capability, Manifest: l.Manifest, ManifestHash: l.ManifestHash, Environment: l.Environment, Fence: l.Fence, ContractHash: l.ContractHash, HardDeadline: hard}
	v, e := executionlease.Verify(raw, key.Public().(ed25519.PublicKey), p, now)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func options() Options {
	return Options{Poll: 10 * time.Millisecond, CallTimeout: 150 * time.Millisecond, ShutdownTimeout: 400 * time.Millisecond}
}
func (f *fixture) supervisor(t *testing.T, b Adapter) *Supervisor {
	t.Helper()
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}, options())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func launch(t *testing.T, s *Supervisor) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-s.Ready():
	case <-time.After(time.Second):
		t.Fatal("watchdog not ready")
	}
	t.Cleanup(func() { cancel() })
	return cancel, done
}
func finish(t *testing.T, cancel context.CancelFunc, done <-chan error, want error) {
	t.Helper()
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, want) {
			t.Fatal("wrong shutdown", e, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog failed to stop")
	}
}
func await(t *testing.T, condition func() bool) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func (f *fixture) phase() string {
	s, e := f.store.Snapshot(context.Background())
	if e != nil || len(s.Resources) != 1 {
		return "ERROR"
	}
	return s.Resources[0].Phase
}

func TestSupervisorRequiresWatchdogAndFrozenRegistry(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	v := f.lease(t, "lease_one")
	if _, e := s.Submit(context.Background(), "command_one", v); !errors.Is(e, ErrClosed) {
		t.Fatal("submitted without watchdog", e)
	}
	for _, r := range [][]Registration{nil, {{"bad", "cap_test", strings.Repeat("1", 64), b}}, {{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), nil}}} {
		if _, e := New(f.store, r, options()); !errors.Is(e, ErrAdapter) {
			t.Fatal("bad registry accepted", e)
		}
	}
	r := []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}
	s, e := New(f.store, r, options())
	if e != nil {
		t.Fatal(e)
	}
	r[0].Backend = newBackend()
	cancel, done := launch(t, s)
	if _, e = s.Submit(context.Background(), "command_one", v); e != nil {
		t.Fatal(e)
	}
	if n, _, _, _ := b.counts(); n != 1 {
		t.Fatal("caller mutated registry")
	}
	finish(t, cancel, done, nil)
	if e = s.Run(context.Background()); !errors.Is(e, ErrClosed) {
		t.Fatal("watchdog reused", e)
	}
}
func TestSupervisorOriginalCommandSubmittedOnlyOnce(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	v := f.lease(t, "lease_one")
	h, e := s.Submit(context.Background(), "command_one", v)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 12; i++ {
		if same, e := s.Submit(context.Background(), "command_one", v); same != h || !errors.Is(e, ErrUnknown) {
			t.Fatal("duplicate not query-only", e)
		}
	}
	await(t, func() bool { _, q, _, _ := b.counts(); return q > 0 })
	if n, _, _, _ := b.counts(); n != 1 {
		t.Fatal("resubmitted original")
	}
	// A second, non-running wrapper models a drain winning between the
	// durable Receive and the slot reservation. It cannot reserve live work.
	late := f.supervisor(t, b)
	late.Drain()
	a, e := f.store.Current(context.Background(), h)
	if e != nil || !a.Authorized {
		t.Fatal("missing live owner for drain race", e)
	}
	if _, release, ok := late.reserve(a, b, context.Background()); ok {
		release()
		t.Fatal("drain allowed late live reservation")
	}
	finish(t, cancel, done, nil)
	if f.phase() != "FREE" {
		t.Fatal("confirmed cleanup retained resource")
	}
}
func TestSupervisorExpiryCancelsAndCleansWithoutRenewal(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	f.clock.advance(46 * time.Second)
	await(t, func() bool { return f.phase() == "FREE" })
	if n, _, c, k := b.counts(); n != 1 || c < 1 || k < 1 {
		t.Fatal("expiry not stopped and cleaned", n, c, k)
	}
	finish(t, cancel, done, nil)
}
func TestSupervisorAmbiguousSubmitQueriesOriginalAndStops(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.lost = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); !errors.Is(e, ErrUnknown) {
		t.Fatal("lost ack not unknown", e)
	}
	await(t, func() bool { return f.phase() == "FREE" })
	if n, q, c, k := b.counts(); n != 1 || q < 1 || c < 1 || k < 1 {
		t.Fatal("unknown caused resubmit or skipped cleanup", n, q, c, k)
	}
	finish(t, cancel, done, nil)
}
func TestSupervisorStopInterruptsInFlightSubmit(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.blockSubmit = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	v := f.lease(t, "lease_one")
	submitted := make(chan error, 1)
	go func() { _, e := s.Submit(context.Background(), "command_one", v); submitted <- e }()
	<-b.started
	r, e := f.store.Recovery(context.Background(), "lease_one")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stop(context.Background(), r.Hold); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-submitted:
		if !errors.Is(e, ErrUnknown) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("submission not interrupted")
	}
	await(t, func() bool { return f.phase() == "FREE" })
	finish(t, cancel, done, nil)
}
func TestSupervisorCallerCancellationDoesNotAbandonCleanup(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.blockSubmit = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	ctx, stop := context.WithCancel(context.Background())
	submitted := make(chan error, 1)
	v := f.lease(t, "lease_one")
	go func() { _, e := s.Submit(ctx, "command_one", v); submitted <- e }()
	<-b.started
	stop()
	select {
	case e := <-submitted:
		if !errors.Is(e, ErrUnknown) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation stuck")
	}
	await(t, func() bool { return f.phase() == "FREE" })
	finish(t, cancel, done, nil)
}
func TestSupervisorCancelAcknowledgementNeverProvesStopped(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.cancelNoStop = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	h, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stop(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { _, _, c, _ := b.counts(); return c >= 2 })
	if f.phase() == "FREE" {
		t.Fatal("cancel ACK freed running work")
	}
	if _, _, _, n := b.counts(); n != 0 {
		t.Fatal("cleaned running backend")
	}
	finish(t, cancel, done, ErrShutdown)
	if f.phase() == "FREE" {
		t.Fatal("shutdown TTL freed running resource")
	}
}
func TestSupervisorQueryFailureStillCancelsAndRetainsOwner(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.queryError = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	f.clock.advance(46 * time.Second)
	await(t, func() bool { _, _, c, _ := b.counts(); return c > 0 })
	if f.phase() == "FREE" {
		t.Fatal("query failure freed resource")
	}
	finish(t, cancel, done, ErrShutdown)
}
func TestSupervisorWrongOwnerAndIncompleteCleanupRetainResource(t *testing.T) {
	for _, kind := range []string{"owner", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			b := newBackend()
			s := f.supervisor(t, b)
			cancel, done := launch(t, s)
			if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
				t.Fatal(e)
			}
			b.mu.Lock()
			if kind == "owner" {
				b.badOwner = true
			} else {
				b.badCleanup = true
			}
			b.mu.Unlock()
			f.clock.advance(46 * time.Second)
			await(t, func() bool { _, q, c, k := b.counts(); return q > 0 && c > 0 && (kind == "owner" || k > 0) })
			if f.phase() == "FREE" {
				t.Fatal("invalid owner/cleanup released resource")
			}
			finish(t, cancel, done, ErrShutdown)
		})
	}
}
func TestSupervisorReopenQueriesWithoutSubmissionOrAuthority(t *testing.T) {
	f := newFixture(t)
	v := f.lease(t, "lease_one")
	h, _, e := f.store.Receive(context.Background(), "command_one", v)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.BeginSubmit(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	f.store.Close()
	f.store, e = execution.Open(context.Background(), f.path, f.binding, false, f.clock)
	if e != nil {
		t.Fatal(e)
	}
	b := newBackend()
	b.state = "RUNNING"
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	await(t, func() bool { return f.phase() == "FREE" })
	if n, q, c, k := b.counts(); n != 0 || q < 1 || c < 1 || k < 1 {
		t.Fatal("reopen resumed/submitted", n, q, c, k)
	}
	finish(t, cancel, done, nil)
}
func TestSupervisorUnknownRegistryQuarantinesWithoutFallback(t *testing.T) {
	f := newFixture(t)
	v := f.lease(t, "lease_one")
	h, _, e := f.store.Receive(context.Background(), "command_one", v)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.BeginSubmit(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	f.store.Close()
	f.store, e = execution.Open(context.Background(), f.path, f.binding, false, f.clock)
	if e != nil {
		t.Fatal(e)
	}
	b := newBackend()
	s, e := New(f.store, []Registration{{strings.Repeat("4", 64), "cap_test", strings.Repeat("1", 64), b}}, options())
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	if _, e = s.Submit(context.Background(), "other_command", v); !errors.Is(e, ErrAdapter) {
		t.Fatal("unbound adapter fallback", e)
	}
	finish(t, cancel, done, ErrShutdown)
	if n, q, c, k := b.counts(); n+q+c+k != 0 {
		t.Fatal("called unrelated adapter")
	}
	if f.phase() != "QUARANTINED" && f.phase() != "STOPPING" {
		t.Fatal("lost unknown owner")
	}
}
func TestSupervisorDatabaseFailureClosesAdmissionAndCancelsOwnedWork(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	_, done := launch(t, s)
	v := f.lease(t, "lease_one")
	if _, e := s.Submit(context.Background(), "command_one", v); e != nil {
		t.Fatal(e)
	}
	f.store.Close()
	select {
	case e := <-done:
		if !errors.Is(e, ErrShutdown) {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("storage failure loop did not stop")
	}
	if _, e := s.Submit(context.Background(), "another_command", v); !errors.Is(e, ErrClosed) {
		t.Fatal("accepted after storage loss", e)
	}
	if _, _, c, _ := b.counts(); c < 1 {
		t.Fatal("storage loss did not cancel owned backend")
	}
}
func TestSupervisorCleanupRetryDoesNotResubmitOrRepeatStop(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.cleanError = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	h, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stop(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { _, _, _, k := b.counts(); return k >= 2 })
	if f.phase() != "CLEANING" {
		t.Fatal("cleanup retry lost stage")
	}
	n, _, c, _ := b.counts()
	b.mu.Lock()
	b.cleanError = false
	b.mu.Unlock()
	await(t, func() bool { return f.phase() == "FREE" })
	if nn, _, cc, _ := b.counts(); nn != n || cc != c {
		t.Fatal("cleanup retried execution or cancellation")
	}
	finish(t, cancel, done, nil)
}
func TestSupervisorQueryTimeoutDoesNotBlockExpiryLoop(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	b.blockQuery = true
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	<-b.queryStarted
	f.clock.advance(46 * time.Second)
	await(t, func() bool { _, _, c, _ := b.counts(); return c > 0 })
	if f.phase() == "FREE" {
		t.Fatal("timeout released resource")
	}
	finish(t, cancel, done, ErrShutdown)
}
func TestSupervisorSignedRenewalKeepsOriginalBackend(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	v := f.lease(t, "lease_one")
	h, e := s.Submit(context.Background(), "command_one", v)
	if e != nil {
		t.Fatal(e)
	}
	f.clock.advance(20 * time.Second)
	l := v.Details()
	l.Revision = 2
	l.Issued = f.clock.Sample().Wall.Format(time.RFC3339Nano)
	l.Expires = f.clock.Sample().Wall.Add(40 * time.Second).Format(time.RFC3339Nano)
	if e = f.store.Renew(context.Background(), "renew_original", h, sign(t, l, f.clock.Sample().Wall)); e != nil {
		t.Fatal(e)
	}
	f.clock.advance(26 * time.Second)
	await(t, func() bool { _, q, _, _ := b.counts(); return q >= 2 })
	a, e := f.store.Current(context.Background(), h)
	if e != nil || !a.Authorized || a.BackendID != "backend_lease_one" {
		t.Fatal("renewal changed owner or expired early", e)
	}
	if n, _, c, _ := b.counts(); n != 1 || c != 0 {
		t.Fatal("renewal restarted/stopped backend")
	}
	finish(t, cancel, done, nil)
}

func TestSupervisorShortRealLeaseNeverExecutesPastExpiry(t *testing.T) {
	f := newFixture(t)
	f.store.Close()
	var e error
	f.store, e = execution.Open(context.Background(), f.path, f.binding, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	b := &deadlineBackend{backend: newBackend()}
	b.blockSubmit = true
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}, Options{Poll: 10 * time.Millisecond, CallTimeout: time.Second, ShutdownTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	// Start the watchdog and SQLite before issuing the short lease. A slow
	// host can still exhaust the 150ms lease during admission; that must reject
	// with zero Submit calls rather than count as a failed engine cancellation.
	f.clock.mu.Lock()
	f.clock.stamp.Wall = time.Now().UTC()
	f.clock.mu.Unlock()
	l := f.lease(t, "lease_one").Details()
	expires := f.clock.Sample().Wall.Add(150 * time.Millisecond)
	l.Expires = expires.Format(time.RFC3339Nano)
	started := time.Now()
	_, e = s.Submit(context.Background(), "command_one", sign(t, l, f.clock.Sample().Wall))
	n, _, _, _ := b.counts()
	if n == 0 {
		if !errors.Is(e, execution.ErrRecovery) && !errors.Is(e, ErrUnknown) && !errors.Is(e, executionlease.ErrLease) {
			t.Fatal(e)
		}
		if errors.Is(e, executionlease.ErrLease) && time.Now().Before(expires) {
			t.Fatal("valid original lease rejected before its expiry", e)
		}
		await(t, func() bool { return !time.Now().Before(expires) })
		t.Log("ZERO_SUBMIT_SAFE_REJECTION: short lease could not authorize backend work")
	} else {
		if n != 1 || !errors.Is(e, ErrUnknown) {
			t.Fatal("unexpected side effects", n, e)
		}
		if b.deadline.IsZero() || b.deadline.After(expires) {
			t.Fatal("backend deadline exceeded signed expiry")
		}
		if time.Since(started) > 800*time.Millisecond {
			t.Fatal("submission used call timeout beyond signed expiry")
		}
		t.Log("ONE_SUBMIT_BOUNDED_BY_SIGNED_EXPIRY")
	}
	await(t, func() bool { return f.phase() == "FREE" })
	if after, _, _, _ := b.counts(); after != n {
		t.Fatal("late or repeated backend Submit after expiry")
	}
	finish(t, cancel, done, nil)
}

func TestSupervisorKnownBackendAbsentCannotFreeResource(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	h, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one"))
	if e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	b.state = "ABSENT"
	b.mu.Unlock()
	if e = s.Stop(context.Background(), h); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { _, q, c, _ := b.counts(); return q > 0 && c > 0 })
	if f.phase() == "FREE" {
		t.Fatal("missing previously known backend treated as stopped")
	}
	finish(t, cancel, done, ErrShutdown)
}

func TestSupervisorSlowResourceDoesNotBlockAnotherExpiry(t *testing.T) {
	f := newFixture(t)
	if e := f.store.RegisterResource(context.Background(), "resource_second", strings.Repeat("9", 64)); e != nil {
		t.Fatal(e)
	}
	slow, fast := newBackend(), newBackend()
	slow.blockQuery = true
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), slow}, {strings.Repeat("4", 64), "cap_test", strings.Repeat("1", 64), fast}}, options())
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	if _, e = s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	l := f.lease(t, "lease_two").Details()
	l.Resource = "resource_second"
	l.Environment = strings.Repeat("4", 64)
	h, e := s.Submit(context.Background(), "command_two", sign(t, l, f.clock.Sample().Wall))
	if e != nil {
		t.Fatal(e)
	}
	<-slow.queryStarted
	f.clock.advance(46 * time.Second)
	await(t, func() bool { _, e := f.store.Current(context.Background(), h); return errors.Is(e, execution.ErrFence) })
	if n, _, c, k := fast.counts(); n != 1 || c < 1 || k < 1 {
		t.Fatal("slow resource blocked another", n, c, k)
	}
	finish(t, cancel, done, ErrShutdown)
}

func TestSupervisorClockRollbackStopsWithoutFreeByTime(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	if _, e := s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	f.clock.advance(-time.Second)
	await(t, func() bool { return f.phase() == "FREE" })
	if _, _, c, k := b.counts(); c < 1 || k < 1 {
		t.Fatal("rollback bypassed stop/cleanup")
	}
	finish(t, cancel, done, nil)
}
