// Package supervisor drives signed JOB ownership through a trusted backend
// adapter. It neither qualifies hardware nor configures an engine or trust key.
package supervisor

import (
	"context"
	"errors"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrClosed   = errors.New("supervisor admission is closed")
	ErrAdapter  = errors.New("owned backend state is unverified; retain resource")
	ErrUnknown  = errors.New("original execution outcome requires query and cleanup")
	ErrShutdown = errors.New("shutdown cleanup is unconfirmed; retain resource")
)

// Work deliberately carries only fixed execution metadata. A content adapter
// must later resolve separately authorized input inside the execution identity;
// prompts, paths, URLs, argv and keys are not accepted through this interface.
type Work struct{ RequestID, Resource, LocalGeneration, Environment string }
type Observation struct{ RequestID, BackendID, State string }
type Cleaned struct {
	RequestID, BackendID, LocalGeneration, ManifestHash string
	WorkStopped, TokensRevoked, WorkspaceCleared        bool
}

// Adapter is trusted code, independently approved for the exact registry key.
// All calls MUST honor ctx, be bounded and return no customer content in errors.
// Query must cover the ORIGINAL request even while Submit is pending. ABSENT
// is authoritative only when that request cannot subsequently start. Cancel
// revokes that request's future start as well as its current owned computation.
// An HTTP reply, cancel ACK, process exit or missing PID is not STOPPED proof.
// Cleanup is idempotent for the full owner generation and must not touch other
// owners. Repeated Query/Cancel/Cleanup must never submit new work.
type Adapter interface {
	Submit(context.Context, Work) (Observation, error)
	Query(context.Context, Work) (Observation, error)
	Cancel(context.Context, Work) error
	Cleanup(context.Context, Work, Observation) (Cleaned, error)
}

// InputAdapter receives content only inside the once-reserved original Submit
// call. It must enforce its model tokenizer and private content lifecycle.
// The byte slice is valid only until return and is cleared by the supervisor;
// recovery calls use the metadata-only Adapter methods and never replay input.
type InputAdapter interface {
	Adapter
	SubmitInput(context.Context, Work, executionlease.Manifest, []byte) (Observation, error)
}

type inputSubmission struct {
	manifest executionlease.Manifest
	body     []byte
}

type Registration struct {
	Environment, Capability, ManifestHash string
	Backend                               Adapter
}
type Options struct{ Poll, CallTimeout, MetadataTimeout, ShutdownTimeout time.Duration }
type registryKey struct{ environment, capability, manifest string }
type slot struct {
	busy     bool
	stopping bool
	owner    execution.Hold
	cancel   context.CancelFunc
	backend  Adapter
	work     Work
}
type Supervisor struct {
	store      *execution.Store
	registry   map[registryKey]Adapter
	options    Options
	mu         sync.Mutex
	slots      map[string]*slot
	runContext context.Context
	ready      chan struct{}
	done       chan struct{}
	completed  chan struct{}
	used       atomic.Bool
	closed     atomic.Bool
}

// New freezes a COPY of registry bindings. This is not KernelRegistry package
// installation/signature validation and provides no sale or funding approval.
func New(store *execution.Store, registrations []Registration, o Options) (*Supervisor, error) {
	if store == nil || len(registrations) == 0 || len(registrations) > 128 {
		return nil, ErrAdapter
	}
	if o.Poll == 0 {
		o.Poll = 50 * time.Millisecond
	}
	if o.CallTimeout == 0 {
		o.CallTimeout = time.Second
	}
	if o.MetadataTimeout == 0 {
		o.MetadataTimeout = 2 * time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 5 * time.Second
	}
	if o.Poll < 10*time.Millisecond || o.Poll > time.Second || o.CallTimeout < 20*time.Millisecond || o.CallTimeout > 5*time.Second || o.MetadataTimeout < 20*time.Millisecond || o.MetadataTimeout > 5*time.Second || o.ShutdownTimeout < o.CallTimeout || o.ShutdownTimeout > 30*time.Second {
		return nil, ErrAdapter
	}
	s := &Supervisor{store: store, registry: map[registryKey]Adapter{}, options: o, slots: map[string]*slot{}, ready: make(chan struct{}), done: make(chan struct{}), completed: make(chan struct{}, 1)}
	for _, r := range registrations {
		key := registryKey{r.Environment, r.Capability, r.ManifestHash}
		if !executionlease.Hash(r.Environment) || !executionlease.Hash(r.ManifestHash) || !ids.Valid(r.Capability) || r.Backend == nil || s.registry[key] != nil {
			return nil, ErrAdapter
		}
		s.registry[key] = r.Backend
	}
	return s, nil
}
func (s *Supervisor) Ready() <-chan struct{} { return s.ready }
func (s *Supervisor) Done() <-chan struct{}  { return s.done }

// BoundTo lets the enclosing node retain one OS lock for this exact ledger.
// It is an ownership check only, not a platform/device authorization grant.
func (s *Supervisor) BoundTo(store *execution.Store) bool { return s.store == store }

// Drain latches admission closed but keeps the watchdog available to recover,
// stop and clean existing owners. Pending/revoked control state must not make
// original-owner cleanup depend on a fresh execution authorization.
func (s *Supervisor) Drain() {
	s.closed.Store(true)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.slots {
		if r.busy && !r.stopping {
			r.cancel()
		}
	}
}
func (s *Supervisor) adapter(a execution.Activity) Adapter {
	return s.registry[registryKey{a.Environment, a.Capability, a.ManifestHash}]
}
func work(a execution.Activity) Work {
	return Work{a.Hold.Owner(), a.Hold.Resource(), a.Hold.LocalGeneration(), a.Environment}
}

func (s *Supervisor) reserve(a execution.Activity, b Adapter, parent context.Context) (context.Context, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Drain may win after Receive but before this slot is reserved. It must
	// still permit stop recovery, while refusing any new live-work slot.
	if a.Authorized && s.closed.Load() {
		return nil, nil, false
	}
	r := s.slots[a.Hold.Resource()]
	if r == nil {
		if len(s.slots) >= execution.MaximumResources {
			return nil, nil, false
		}
		r = &slot{}
		s.slots[a.Hold.Resource()] = r
	}
	if r.busy {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	r.busy = true
	r.stopping = !a.Authorized
	r.owner = a.Hold
	r.cancel = cancel
	r.backend = b
	r.work = work(a)
	return ctx, func() { cancel(); s.mu.Lock(); r.busy = false; r.cancel = nil; s.mu.Unlock() }, true
}
func (s *Supervisor) cancelOwner(h execution.Hold) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.slots[h.Resource()]; r != nil && r.busy && r.owner == h && !r.stopping {
		r.cancel()
	}
}
func (s *Supervisor) runningContext() (context.Context, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runContext, s.runContext != nil && s.runContext.Err() == nil && !s.closed.Load()
}

// Submit must run with an active watchdog. The durable store reserves the
// external side effect once before the adapter receives the original key.
// Duplicates and unknown outcomes return query-only; no caller can retry Submit.
func (s *Supervisor) Submit(ctx context.Context, command string, v executionlease.Verified) (execution.Hold, error) {
	return s.submit(ctx, command, v, nil)
}

// SubmitInput preserves the original inbox/BeginSubmit fencing. A legacy
// metadata adapter cannot silently accept content via Adapter.Submit.
func (s *Supervisor) SubmitInput(ctx context.Context, command string, accepted devicecontrol.AcceptedDelivery, body []byte) (execution.Hold, error) {
	copy, e := accepted.AcceptInput(devicecontrol.Reply{Status: 200, Body: body}, time.Now())
	if e != nil {
		return execution.Hold{}, ErrAdapter
	}
	defer clear(copy)
	return s.submit(ctx, command, accepted.Verified(), &inputSubmission{accepted.Manifest(), copy})
}

func (s *Supervisor) submit(ctx context.Context, command string, v executionlease.Verified, input *inputSubmission) (execution.Hold, error) {
	parent, ok := s.runningContext()
	if !ok {
		return execution.Hold{}, ErrClosed
	}
	l := v.Details()
	b := s.registry[registryKey{l.Environment, l.Capability, l.ManifestHash}]
	if b == nil {
		return execution.Hold{}, ErrAdapter
	}
	if input != nil {
		if _, ok := b.(InputAdapter); !ok {
			return execution.Hold{}, ErrAdapter
		}
	}
	if ctx.Err() != nil {
		return execution.Hold{}, ErrClosed
	}
	meta, done := s.metadataContext()
	defer func() { done() }()
	refresh := func() { done(); meta, done = s.metadataContext() }
	h, disposition, e := s.store.Receive(meta, command, v)
	if e != nil {
		s.storageFailure(e)
		return h, e
	}
	if disposition != "RECEIVED" {
		return h, ErrUnknown
	}
	refresh()
	a, e := s.store.Current(meta, h)
	if e != nil {
		s.storageFailure(e)
		return h, e
	}
	run, release, ok := s.reserve(a, b, parent)
	if !ok {
		return h, ErrUnknown
	}
	defer release()
	// A caller cancellation also closes this submission, without ending stop
	// recovery or silently retrying under another caller's context.
	stop := context.AfterFunc(ctx, func() { s.cancelOwner(h) })
	defer stop()
	refresh()
	key, e := s.store.BeginSubmit(meta, h)
	if e != nil {
		s.storageFailure(e)
		return h, e
	}
	refresh()
	a, e = s.store.Current(meta, h)
	if e != nil || !a.Authorized {
		s.storageFailure(e)
		return h, ErrUnknown
	}
	call, cancel := context.WithDeadline(run, a.Deadline)
	defer cancel()
	bounded, bound := context.WithTimeout(call, s.options.CallTimeout)
	defer bound()
	if bounded.Err() != nil || ctx.Err() != nil || key != h.Owner() {
		return h, ErrUnknown
	}
	var o Observation
	if input != nil {
		o, e = b.(InputAdapter).SubmitInput(bounded, work(a), input.manifest, input.body)
	} else {
		o, e = b.Submit(bounded, work(a))
	}
	if e != nil || !valid(a, o) || (o.State != "RUNNING" && o.State != "STOPPED") {
		s.quarantine(h)
		return h, ErrUnknown
	}
	// Persist even a late positive mapping using a fresh bounded metadata
	// context. This never reinstates authority that expired during submission.
	late, finish := s.metadataContext()
	defer finish()
	if e = s.store.RecordBackend(late, h, o.BackendID); e != nil {
		s.storageFailure(e)
		return h, ErrUnknown
	}
	if o.State == "STOPPED" {
		refresh()
		s.storageFailure(s.store.RequestStop(meta, h))
	}
	return h, nil
}

func valid(a execution.Activity, o Observation) bool {
	if o.RequestID != a.Hold.Owner() || (a.BackendID != "" && o.BackendID != a.BackendID) {
		return false
	}
	switch o.State {
	case "RUNNING", "STOPPED":
		return ids.Valid(o.BackendID)
	case "ABSENT":
		return o.BackendID == "" && a.BackendID == ""
	case "UNKNOWN":
		return o.BackendID == "" || ids.Valid(o.BackendID)
	default:
		return false
	}
}
func (s *Supervisor) quarantine(h execution.Hold) {
	ctx, cancel := s.metadataContext()
	defer cancel()
	s.storageFailure(s.store.Quarantine(ctx, h))
}
func (s *Supervisor) metadataContext(parents ...context.Context) (context.Context, context.CancelFunc) {
	// Caller/control cancellation cannot interrupt a safety-state transaction.
	// Backend work retains its independently cancellable, bounded context.
	deadline := time.Now().Add(s.options.MetadataTimeout)
	for _, parent := range parents {
		if until, ok := parent.Deadline(); ok && until.Before(deadline) {
			deadline = until
		}
	}
	return context.WithDeadline(context.Background(), deadline)
}
func (s *Supervisor) storageFailure(e error) {
	if !errors.Is(e, execution.ErrStorage) {
		return
	}
	s.closed.Store(true)
	// The database cannot authorize release. Best-effort cancel every owned
	// in-flight request, retaining the original metadata and occupied resource.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.slots {
		if r.busy {
			r.cancel()
		}
	}
}

// Stop is owner-scoped and first persists STOPPING, then interrupts an in-flight
// submission/query. The watchdog performs backend cancellation and cleanup.
func (s *Supervisor) Stop(ctx context.Context, h execution.Hold) error {
	_ = ctx
	meta, done := s.metadataContext()
	defer done()
	e := s.store.RequestStop(meta, h)
	s.storageFailure(e)
	if e == nil {
		s.cancelOwner(h)
	}
	return e
}

func (s *Supervisor) query(ctx context.Context, b Adapter, a execution.Activity) (Observation, error) {
	call, cancel := context.WithTimeout(ctx, s.options.CallTimeout)
	defer cancel()
	o, e := b.Query(call, work(a))
	if e != nil || !valid(a, o) || o.State == "UNKNOWN" {
		return Observation{}, ErrAdapter
	}
	return o, nil
}
func (s *Supervisor) reconcile(ctx context.Context, a execution.Activity, b Adapter) bool {
	meta, done := s.metadataContext(ctx)
	defer func() { done() }()
	refresh := func() { done(); meta, done = s.metadataContext(ctx) }
	current, e := s.store.Current(meta, a.Hold)
	if e != nil {
		s.storageFailure(e)
		return false
	}
	a = current
	if a.Phase == "CLEANING" {
		return s.cleanup(ctx, a, b, Observation{a.Hold.Owner(), a.BackendID, stoppedState(a)})
	}
	if !a.Authorized {
		refresh()
		if e = s.store.RequestStop(meta, a.Hold); e != nil {
			s.storageFailure(e)
			return false
		}
		call, cancel := context.WithTimeout(ctx, s.options.CallTimeout)
		e = b.Cancel(call, work(a))
		cancel()
		if e != nil {
			return false
		}
		refresh()
	}
	o, e := s.query(ctx, b, a)
	if e != nil {
		s.quarantine(a.Hold)
		return false
	}
	refresh()
	if o.BackendID != "" && a.BackendID != o.BackendID {
		if e = s.store.RecordBackend(meta, a.Hold, o.BackendID); e != nil {
			s.storageFailure(e)
			return false
		}
		a.BackendID = o.BackendID
	}
	refresh()
	current, e = s.store.Current(meta, a.Hold)
	if e != nil {
		s.storageFailure(e)
		return false
	}
	a = current
	if a.Authorized && o.State == "RUNNING" {
		return false
	}
	// An authoritative ABSENT after SUBMITTING is terminal for THIS original
	// request. It never causes a fresh execution or a replacement request key.
	if a.Phase != "STOPPING" && a.Phase != "CLEANING" {
		refresh()
		if e = s.store.RequestStop(meta, a.Hold); e != nil {
			s.storageFailure(e)
			return false
		}
	}
	if o.State == "RUNNING" {
		call, cancel := context.WithTimeout(ctx, s.options.CallTimeout)
		e = b.Cancel(call, work(a))
		cancel()
		if e != nil {
			return false
		}
		o, e = s.query(ctx, b, a)
		if e != nil || o.State == "RUNNING" {
			return false
		}
		refresh()
	}
	refresh()
	if e = s.store.ObserveStopped(meta, a.Hold, execution.Observation{RequestID: o.RequestID, BackendID: o.BackendID, State: o.State}); e != nil {
		s.storageFailure(e)
		return false
	}
	return s.cleanup(ctx, a, b, o)
}
func stoppedState(a execution.Activity) string {
	if a.BackendID == "" {
		return "ABSENT"
	}
	return "STOPPED"
}
func (s *Supervisor) cleanup(ctx context.Context, a execution.Activity, b Adapter, o Observation) bool {
	call, cancel := context.WithTimeout(ctx, s.options.CallTimeout)
	defer cancel()
	c, e := b.Cleanup(call, work(a), o)
	if e != nil || c.RequestID != a.Hold.Owner() || c.BackendID != o.BackendID || c.LocalGeneration != a.Hold.LocalGeneration() {
		return false
	}
	meta, done := s.metadataContext(ctx)
	defer done()
	e = s.store.FinishCleanup(meta, a.Hold, execution.Cleanup{WorkStopped: c.WorkStopped, TokensRevoked: c.TokensRevoked, WorkspaceCleared: c.WorkspaceCleared, ManifestHash: c.ManifestHash})
	s.storageFailure(e)
	return e == nil
}

func (s *Supervisor) sweep(ctx context.Context, shutdown bool) (bool, error) {
	meta, done := s.metadataContext(ctx)
	defer done()
	activities, e := s.store.Activities(meta)
	if e != nil {
		s.storageFailure(e)
		return false, e
	}
	return len(activities) == 0, s.sweepActivities(ctx, activities, shutdown)
}

func (s *Supervisor) sweepActivities(ctx context.Context, activities []execution.Activity, shutdown bool) error {
	unavailable := false
	for _, a := range activities {
		if (shutdown || s.closed.Load()) && a.Authorized {
			meta, done := s.metadataContext(ctx)
			e := s.store.RequestStop(meta, a.Hold)
			done()
			if e != nil {
				// Cleanup may finish after Activities took this snapshot. Its
				// owner CAS correctly refuses the stale hold; a later sweep
				// reads current state. Never stop a replacement owner or turn
				// this expected race into a failed shutdown/storage fault.
				if errors.Is(e, execution.ErrFence) {
					continue
				}
				s.storageFailure(e)
				return e
			}
			a.Authorized = false
		}
		if !a.Authorized {
			s.cancelOwner(a.Hold)
		}
		b := s.adapter(a)
		if b == nil {
			// An absent frozen binding cannot recover during this supervisor's
			// lifetime. Preserve an already safe occupied state without repeatedly
			// rewriting it until the shutdown metadata deadline is exhausted.
			if a.Phase != "QUARANTINED" && a.Phase != "STOPPING" && a.Phase != "CLEANING" {
				s.quarantine(a.Hold)
			}
			unavailable = true
			continue
		}
		// A freshly RECEIVED command may still be entering Submit. A recovery
		// process cannot have its guard, so that case must be queried/stopped.
		if a.Authorized && a.InboxPhase == "RECEIVED" {
			continue
		}
		run, release, ok := s.reserve(a, b, ctx)
		if ok {
			go func() {
				completed := false
				defer func() {
					release()
					if completed {
						// Only durable cleanup completion wakes shutdown. Ordinary
						// queries/retries keep their poll interval and cannot spin.
						select {
						case s.completed <- struct{}{}:
						default:
						}
					}
				}()
				completed = s.reconcile(run, a, b)
			}()
		}
	}
	// Visit every owner first so a missing binding cannot prevent cancellation
	// of another resource with a matching original backend. Run then performs
	// owner-scoped emergency cancellation and reports incomplete shutdown.
	if shutdown && unavailable {
		return ErrAdapter
	}
	return nil
}

// Run is single-use. Polling never renews a lease. Shutdown closes admission,
// interrupts submissions, and queries/stops/cleans original owners under a
// separate bounded context. Unconfirmed cleanup remains occupied on disk.
func (s *Supervisor) Run(ctx context.Context) error {
	if !s.used.CompareAndSwap(false, true) {
		return ErrClosed
	}
	defer close(s.done)
	s.mu.Lock()
	s.runContext = ctx
	s.mu.Unlock()
	close(s.ready)
	ticker := time.NewTicker(s.options.Poll)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if _, e := s.sweep(ctx, false); e != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	s.closed.Store(true)
	stop, cancel := context.WithTimeout(context.Background(), s.options.ShutdownTimeout)
	defer cancel()
	for stop.Err() == nil {
		empty, e := s.sweep(stop, true)
		if e != nil {
			s.emergencyCancel(stop)
			return ErrShutdown
		}
		if empty && !s.busy() {
			return nil
		}
		select {
		case <-stop.Done():
		case <-s.completed:
		case <-ticker.C:
		}
	}
	s.emergencyCancel(stop)
	return ErrShutdown
}
func (s *Supervisor) busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.slots {
		if r.busy {
			return true
		}
	}
	return false
}
func (s *Supervisor) emergencyCancel(parent context.Context) {
	type owned struct {
		b Adapter
		w Work
	}
	var pending []owned
	s.mu.Lock()
	for _, r := range s.slots {
		if r.cancel != nil {
			r.cancel()
		}
		if r.backend != nil {
			pending = append(pending, owned{r.backend, r.work})
		}
	}
	s.mu.Unlock()
	// No cleanup/free is possible without durable state. Cancel has its own
	// fresh bound even if the shutdown context has expired.
	_ = parent
	ctx, cancel := context.WithTimeout(context.Background(), s.options.CallTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, p := range pending {
		wg.Go(func() { _ = p.b.Cancel(ctx, p.w) })
	}
	wg.Wait()
}
