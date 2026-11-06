package rentalruntime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
)

type invocation struct {
	work                                                 Work
	input                                                *rentalaccess.TextInput
	submitDone                                           chan struct{}
	stopping, cleaned, reader, outputDone, outputAborted bool
	stopSeen                                             time.Time
}

type Runtime struct {
	mu                              sync.Mutex
	invokes                         sync.WaitGroup
	ledger                          *execution.Store
	control                         Control
	backend                         Backend
	prepared                        rentalaccess.Prepared
	policy                          rentalaccess.TextPolicy
	hold                            execution.Hold
	options                         Options
	entries                         map[string]*invocation
	live                            *invocation
	closed, uncertain, stopRecorded bool
	shutdown                        time.Time
	deadline                        time.Time
	ready, done, wake               chan struct{}
	deadlineWake                    chan struct{}
	used                            atomic.Bool
	result                          error
}

func New(ledger *execution.Store, control Control, p rentalaccess.Prepared, policy rentalaccess.TextPolicy, backend Backend, o Options) (*Runtime, error) {
	o, e := defaults(o)
	if e != nil || ledger == nil || control == nil || backend == nil || !policy.Binds(p) {
		return nil, ErrBackend
	}
	m, i, h := p.Details(), backend.Identity(), control.Hold()
	if h.Owner() != m.Session || h.Resource() != m.Resource || h.LocalGeneration() == "" || i != (Identity{ManifestHash: p.Digest(), PhysicalHash: p.Approval().PhysicalHash, EnvironmentHash: m.EnvironmentHash, ModelHash: m.ModelHash, RecipeHash: m.RecipeHash, PolicyHash: policy.Digest()}) {
		return nil, ErrBackend
	}
	ctx, end := context.WithTimeout(context.Background(), o.MetadataTimeout)
	state, e := ledger.RentalCurrent(ctx, h)
	end()
	if e != nil || state.ManifestHash != p.Digest() || state.ApprovalHash != p.ApprovalDigest() || state.Phase != "PREPARING" && state.Phase != "ACTIVE" || state.Deadline.IsZero() || !time.Now().Before(state.Deadline) {
		return nil, ErrBackend
	}
	return &Runtime{ledger: ledger, control: control, backend: backend, prepared: p, policy: policy, hold: h, options: o, entries: map[string]*invocation{}, ready: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), deadline: state.Deadline, deadlineWake: make(chan struct{}, 1)}, nil
}
func (r *Runtime) Ready() <-chan struct{} { return r.ready }
func (r *Runtime) Done() <-chan struct{}  { return r.done }
func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// cacheDeadline accepts only an independently checked current ledger window.
// Once the previously accepted monotonic deadline expires, a late refreshed
// window cannot reopen admission, even if a metadata operation was blocked.
func (r *Runtime) cacheDeadline(state execution.RentalStatus) {
	if state.ManifestHash != r.prepared.Digest() || state.ApprovalHash != r.prepared.ApprovalDigest() || state.Deadline.IsZero() {
		r.withdraw(true)
		return
	}
	r.mu.Lock()
	closed, expired := r.closed, !time.Now().Before(r.deadline) || !time.Now().Before(state.Deadline)
	if !closed && !expired {
		r.deadline = state.Deadline
	}
	r.mu.Unlock()
	if !closed && expired {
		r.withdraw(false)
		return
	}
	select {
	case r.deadlineWake <- struct{}{}:
	default:
	}
}

// deadlineLoop never reads SQL, waits for the page or refreshes a grant. It
// cancels using the separate native control pipe before any slow metadata.
func (r *Runtime) deadlineLoop(ctx context.Context) {
	for {
		r.mu.Lock()
		deadline, closed := r.deadline, r.closed
		r.mu.Unlock()
		if closed {
			return
		}
		timer := time.NewTimer(max(time.Until(deadline), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-r.deadlineWake:
			timer.Stop()
		case <-timer.C:
			r.mu.Lock()
			expired := !r.closed && r.deadline.Equal(deadline) && !time.Now().Before(r.deadline)
			r.mu.Unlock()
			if expired {
				r.withdraw(false)
				return
			}
		}
	}
}
func (r *Runtime) metadata() (context.Context, context.CancelFunc) {
	r.mu.Lock()
	deadline := r.shutdown
	r.mu.Unlock()
	if deadline.IsZero() {
		return context.WithTimeout(context.Background(), r.options.MetadataTimeout)
	}
	limit := time.Now().Add(r.options.MetadataTimeout)
	if deadline.Before(limit) {
		limit = deadline
	}
	return context.WithDeadline(context.Background(), limit)
}
func (r *Runtime) call() (context.Context, context.CancelFunc) {
	r.mu.Lock()
	deadline := r.shutdown
	r.mu.Unlock()
	limit := time.Now().Add(r.options.CallTimeout)
	if !deadline.IsZero() && deadline.Before(limit) {
		limit = deadline
	}
	return context.WithDeadline(context.Background(), limit)
}

// withdraw is a one-way latch. Closing the owner or its transport cannot
// silently reopen it. Native cancellation precedes potentially slow metadata.
func (r *Runtime) withdraw(uncertain bool) {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.shutdown = time.Now().Add(r.options.ShutdownTimeout)
	}
	r.uncertain = r.uncertain || uncertain
	j := r.live
	if j != nil {
		j.stopping = true
		j.outputAborted = true
		j.input.Close()
	}
	r.mu.Unlock()
	if j != nil {
		ctx, cancel := r.call()
		r.backend.Cancel(ctx, j.work)
		cancel()
	}
	r.signal()
}

// Run owns a continuous watchdog and a separate bounded control exchange loop.
// A blocked/closed buyer stream cannot prevent cancellation through the native
// independent control pipe. Preparation permits model warm-up, never inference.
func (r *Runtime) Run(ctx context.Context) (answer error) {
	if !r.used.CompareAndSwap(false, true) {
		return ErrClosed
	}
	controlCtx, stopControl := context.WithCancel(context.Background())
	controlDone := make(chan struct{})
	deadlineDone := make(chan struct{})
	go func() { defer close(controlDone); r.refreshLoop(controlCtx) }()
	go func() { defer close(deadlineDone); r.deadlineLoop(controlCtx) }()
	if ctx.Err() != nil {
		r.withdraw(false)
	}
	close(r.ready)
	defer func() {
		stopControl()
		// CloseSession is also the fail-closed owned-process termination path;
		// a missing STOPPED receipt never authorizes a ledger release.
		if answer != nil {
			stop, cancel := context.WithTimeout(context.Background(), r.options.CallTimeout)
			r.backend.CloseSession(stop, r.hold)
			cancel()
		}
		// Admission is already withdrawn before reaching here. Never report
		// Done while an entered SubmitText still owns input/normalization RAM.
		r.invokes.Wait()
		// A timeout/kill is not a cleanup proof. Keep Done open until the owned
		// worker and private pipe goroutines actually return; a parent waiting
		// with its own deadline retains the lock/key on timeout.
		if e := r.backend.Join(context.Background()); e != nil {
			answer = ErrBackend
		}
		// Done means every owned control exchange has actually returned. A
		// caller may time out waiting; the enclosing node retains its OS lock
		// and key until this join completes instead of orphaning that exchange.
		<-controlDone
		<-deadlineDone
		r.mu.Lock()
		r.result = answer
		r.mu.Unlock()
		close(r.done)
	}()
	ticker := time.NewTicker(r.options.Poll)
	defer ticker.Stop()
	parent := ctx.Done()
	for {
		select {
		case <-parent:
			r.withdraw(false)
			parent = nil
		case <-ticker.C:
		case <-r.wake:
		}
		if e := r.inspect(); e != nil {
			r.withdraw(true)
		}
		r.mu.Lock()
		closed, recorded := r.closed, r.stopRecorded
		r.mu.Unlock()
		if closed && !recorded {
			meta, cancel := r.metadata()
			e := r.control.Stop(meta)
			cancel()
			r.mu.Lock()
			r.stopRecorded = true
			r.uncertain = r.uncertain || e != nil
			r.mu.Unlock()
		}
		if e := r.stepChild(); e != nil && errors.Is(e, execution.ErrStorage) {
			r.withdraw(true)
		}
		r.mu.Lock()
		closed = r.closed
		live := r.live
		deadline := r.shutdown
		uncertain := r.uncertain
		r.mu.Unlock()
		if closed && live == nil {
			if uncertain {
				return ErrBackend
			}
			return r.finishSession()
		}
		if closed && !time.Now().Before(deadline) {
			return ErrBackend
		}
	}
}

func (r *Runtime) refreshLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return
		}
		call, cancel := context.WithTimeout(ctx, r.options.CallTimeout)
		r.control.RefreshControl(call)
		cancel() // Errors cannot manufacture ALLOW.
		r.signal()
		timer.Reset(r.options.ControlPoll)
	}
}

func (r *Runtime) inspect() error {
	r.mu.Lock()
	closed := r.closed
	live := r.live
	r.mu.Unlock()
	if closed {
		return nil
	}
	meta, cancel := r.metadata()
	e := r.ledger.Tick(meta)
	cancel()
	if e != nil {
		return e
	}
	meta, cancel = r.metadata()
	status, e := r.ledger.RentalCurrent(meta, r.hold)
	cancel()
	if e != nil || status.ManifestHash != r.prepared.Digest() || status.ApprovalHash != r.prepared.ApprovalDigest() {
		return ErrBackend
	}
	if status.Phase == "PREPARING" {
		if live != nil || status.Deadline.IsZero() || !time.Now().Before(status.Deadline) {
			r.withdraw(false)
		}
		r.cacheDeadline(status)
		return nil
	}
	if status.Phase != "ACTIVE" || status.Deadline.IsZero() || !time.Now().Before(status.Deadline) {
		r.withdraw(false)
		return nil
	}
	if !status.Authorized {
		// The first control reply may still be pending; no child may enter in
		// this grace period. Once a child exists, loss immediately withdraws.
		if live != nil {
			r.withdraw(false)
		}
		return nil
	}
	meta, cancel = r.metadata()
	e = r.control.CheckScope(meta, "INVOKE")
	cancel()
	if e != nil {
		r.withdraw(false)
	} else {
		r.cacheDeadline(status)
	}
	return nil
}

func (r *Runtime) Invoke(ctx context.Context, input *rentalaccess.TextInput) (execution.RentalInvocation, error) {
	if input == nil {
		return execution.RentalInvocation{}, ErrClosed
	}
	defer input.Close()
	select {
	case <-r.ready:
	default:
		return execution.RentalInvocation{}, ErrClosed
	}
	select {
	case <-r.done:
		return execution.RentalInvocation{}, ErrClosed
	default:
	}
	d := input.Descriptor()
	meta, cancel := r.metadata()
	e := r.control.CheckScope(meta, "INVOKE")
	cancel()
	if e != nil {
		return execution.RentalInvocation{}, ErrClosed
	}
	meta, cancel = r.metadata()
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	cancel()
	if e != nil || !state.Authorized || d.Grant != state.Expected.Grant || d.GrantDigest != state.Expected.TokenDigest {
		return execution.RentalInvocation{}, ErrClosed
	}
	r.cacheDeadline(state)
	v := execution.RentalInvocation{ID: d.ID, Session: d.Session, Resource: d.Resource, LocalGeneration: d.LocalGeneration, Grant: d.Grant, GrantDigest: d.GrantDigest, InputCiphertextDigest: d.InputCiphertextDigest, RecipeDigest: d.RecipeHash, Phase: "SUBMITTING"}
	w := Work{Invocation: v, Descriptor: d, Expected: state.Expected}
	if !BoundWork(w, r.prepared, r.policy) || ctx.Err() != nil {
		return execution.RentalInvocation{}, ErrBackend
	}
	r.mu.Lock()
	if old := r.entries[d.ID]; old != nil {
		r.mu.Unlock()
		if old.work.Descriptor != d {
			return old.work.Invocation, execution.ErrFence
		}
		return old.work.Invocation, ErrQuery
	}
	if r.closed || r.live != nil || len(r.entries) >= 1024 {
		r.mu.Unlock()
		return execution.RentalInvocation{}, ErrClosed
	}
	j := &invocation{work: w, input: input, submitDone: make(chan struct{})}
	r.entries[d.ID] = j
	r.live = j
	r.invokes.Add(1)
	r.mu.Unlock()
	defer r.invokes.Done()
	defer func() { input.Close(); close(j.submitDone); r.signal() }()
	meta, cancel = r.metadata()
	ticket, actual, disposition, e := r.ledger.BeginRentalInvocation(meta, r.hold, d.ID, d.InputCiphertextDigest, d.RecipeHash)
	cancel()
	if e != nil || disposition != "SUBMISSION_RESERVED" || ticket == nil {
		r.withdraw(errors.Is(e, execution.ErrStorage))
		return actual, ErrQuery
	}
	reserved, e := ticket.Take()
	if e != nil || reserved != v {
		r.withdraw(true)
		return actual, ErrBackend
	}
	meta, cancel = r.metadata()
	e = r.control.CheckScope(meta, "INVOKE")
	cancel()
	if e != nil || ctx.Err() != nil {
		r.stopChild(j)
		return v, ErrClosed
	}
	r.mu.Lock()
	stopping := r.closed || j.stopping
	r.mu.Unlock()
	if stopping {
		r.stopChild(j)
		return v, ErrClosed
	}
	call, end := context.WithTimeout(ctx, r.options.CallTimeout)
	o, e := r.backend.SubmitText(call, w, input)
	end()
	if o.RequestID == v.ID && ids.Valid(o.BackendID) {
		meta, cancel = r.metadata()
		persisted := r.ledger.MarkRentalSubmitted(meta, r.hold, v.ID, o.BackendID)
		cancel()
		if persisted != nil {
			r.withdraw(true)
			return v, ErrQuery
		}
	}
	if e != nil || o.RequestID != v.ID || !ids.Valid(o.BackendID) || (o.State != "RUNNING" && o.State != "STOPPED") {
		r.stopChild(j)
		return v, ErrQuery
	}
	return v, nil
}

func (r *Runtime) stopChild(j *invocation) {
	r.mu.Lock()
	j.stopping = true
	j.outputAborted = true
	j.input.Close()
	r.mu.Unlock()
	call, cancel := r.call()
	r.backend.Cancel(call, j.work)
	cancel()
	r.signal()
}

func (r *Runtime) stepChild() error {
	r.mu.Lock()
	j := r.live
	stopping := j != nil && j.stopping
	r.mu.Unlock()
	if j == nil {
		return nil
	}
	if stopping {
		call, cancel := r.call()
		r.backend.Cancel(call, j.work)
		cancel()
	}
	select {
	case <-j.submitDone:
	default:
		return nil
	}
	call, cancel := r.call()
	o, e := r.backend.Query(call, j.work)
	cancel()
	if e != nil || o.RequestID != j.work.Invocation.ID || (o.State != "STOPPED" && o.State != "ABSENT") {
		return ErrBackend
	}
	if o.State == "STOPPED" && !ids.Valid(o.BackendID) || o.State == "ABSENT" && o.BackendID != "" {
		return ErrBackend
	}
	r.mu.Lock()
	if j.stopSeen.IsZero() {
		j.stopSeen = time.Now()
	}
	waitOutput := !j.stopping && !j.outputDone && time.Now().Before(j.stopSeen.Add(r.options.DrainGrace))
	if !waitOutput {
		j.stopping = true
		if !j.outputDone {
			j.outputAborted = true
		}
	}
	r.mu.Unlock()
	if waitOutput {
		return nil
	}
	meta, end := r.metadata()
	stored, e := r.ledger.RentalInvocation(meta, r.hold, j.work.Invocation.ID)
	end()
	if e != nil {
		return e
	}
	if stored.BackendID != "" && stored.BackendID != o.BackendID {
		return ErrBackend
	}
	if stored.Phase != "CLEANING" && stored.Phase != "CLEANED" {
		meta, end = r.metadata()
		e = r.ledger.StopRentalInvocation(meta, r.hold, j.work.Invocation.ID)
		end()
		if e != nil {
			return e
		}
		meta, end = r.metadata()
		e = r.ledger.ObserveRentalInvocationStopped(meta, r.hold, j.work.Invocation.ID, execution.Observation{RequestID: o.RequestID, BackendID: o.BackendID, State: o.State})
		end()
		if e != nil {
			return e
		}
	}
	call, end = r.call()
	proof, e := r.backend.Cleanup(call, j.work, o)
	end()
	if e != nil || proof.Session != r.hold.Owner() || proof.Invocation != j.work.Invocation.ID || proof.LocalGeneration != r.hold.LocalGeneration() || proof.BackendID != o.BackendID || !proof.WorkStopped || !proof.FutureStartRevoked || !proof.InputCleared || !proof.OutputKeysHandled {
		return ErrBackend
	}
	if stored.Phase != "CLEANED" {
		meta, end = r.metadata()
		e = r.ledger.FinishRentalInvocationCleanup(meta, r.hold, j.work.Invocation.ID, execution.RentalChildCleanup{Session: proof.Session, Invocation: proof.Invocation, LocalGeneration: proof.LocalGeneration, BackendID: proof.BackendID, ManifestHash: proof.ManifestHash, WorkStopped: proof.WorkStopped, FutureStartRevoked: proof.FutureStartRevoked, InputCleared: proof.InputCleared, OutputKeysHandled: proof.OutputKeysHandled})
		end()
		if e != nil {
			return e
		}
	}
	r.mu.Lock()
	j.cleaned = true
	if r.live == j {
		r.live = nil
	}
	r.mu.Unlock()
	return nil
}

func (r *Runtime) finishSession() error {
	call, cancel := context.WithDeadline(context.Background(), r.shutdown)
	defer cancel()
	proof, e := r.backend.CloseSession(call, r.hold)
	if e != nil || proof.Session != r.hold.Owner() || proof.Resource != r.hold.Resource() || proof.LocalGeneration != r.hold.LocalGeneration() || !proof.WorkStopped || !proof.FutureStartsRevoked || !proof.WorkspaceCleared || !proof.OutputKeysHandled {
		return ErrBackend
	}
	if r.backend.Join(call) != nil {
		return ErrBackend
	}
	meta, end := r.metadata()
	e = r.ledger.ObserveRentalSessionStopped(meta, r.hold, execution.RentalSessionStop{Session: proof.Session, LocalGeneration: proof.LocalGeneration, ScopeDigest: proof.ManifestHash, AllRequestsStopped: proof.WorkStopped, FutureStartsRevoked: proof.FutureStartsRevoked})
	end()
	if e != nil {
		return ErrBackend
	}
	// Local grant revocation was durably required by Stop; the ledger checks
	// that no CURRENT grant remains again in the actual release transaction.
	meta, end = r.metadata()
	e = r.ledger.FinishRentalSessionCleanup(meta, r.hold, execution.RentalSessionCleanup{Session: proof.Session, LocalGeneration: proof.LocalGeneration, ManifestHash: proof.ManifestHash, WorkStopped: proof.WorkStopped, AllGrantsRevoked: true, WorkspaceCleared: proof.WorkspaceCleared, OutputKeysHandled: proof.OutputKeysHandled})
	end()
	if e != nil {
		return ErrBackend
	}
	return nil
}

func (r *Runtime) Close(ctx context.Context) error {
	r.withdraw(false)
	select {
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.result
	case <-ctx.Done():
		return ErrBackend
	}
}

// originalChild checks the independently current grant before returning any
// child's metadata. No caller-supplied token digest can redefine Expected.
func (r *Runtime) originalChild(ctx context.Context, id, scope string) (*invocation, error) {
	if ctx.Err() != nil || !ids.Valid(id) {
		return nil, ErrClosed
	}
	meta, end := r.metadata()
	e := r.control.CheckScope(meta, scope)
	end()
	if e != nil {
		return nil, ErrClosed
	}
	meta, end = r.metadata()
	current, e := r.ledger.RentalCurrent(meta, r.hold)
	end()
	if e != nil || !current.Authorized {
		return nil, ErrClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.entries[id]
	if r.closed || j == nil || j.work.Expected != current.Expected {
		return nil, ErrClosed
	}
	return j, nil
}

// Query reads the original committed inbox only. It never renews authority,
// opens an input envelope, allocates another ID or invokes the backend again.
func (r *Runtime) Query(ctx context.Context, id string) (execution.RentalInvocation, error) {
	j, e := r.originalChild(ctx, id, "QUEUE_SELF")
	if e != nil {
		return execution.RentalInvocation{}, e
	}
	meta, end := r.metadata()
	v, e := r.ledger.RentalInvocation(meta, r.hold, id)
	end()
	if e != nil || v.Grant != j.work.Expected.Grant || v.GrantDigest != j.work.Expected.TokenDigest {
		return execution.RentalInvocation{}, ErrQuery
	}
	r.mu.Lock()
	cleaned := j.cleaned
	r.mu.Unlock()
	if v.Phase == "CLEANED" && !cleaned {
		return v, ErrQuery
	}
	return v, nil
}
func (r *Runtime) Cancel(ctx context.Context, id string) error {
	j, e := r.originalChild(ctx, id, "CANCEL_SELF")
	if e != nil {
		return e
	}
	r.stopChild(j)
	return nil
}

// Output is a single owned bounded reader. Raw slices must be cleared by the
// caller. Local measurements are not trusted usage, ACK or an invoice.
type Output struct {
	runtime    *Runtime
	invocation *invocation
	closed     atomic.Bool
}

func (r *Runtime) Output(ctx context.Context, id string) (*Output, error) {
	r.mu.Lock()
	j := r.entries[id]
	if j == nil || j.reader || j.outputAborted || r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	j.reader = true
	r.mu.Unlock()
	o := &Output{runtime: r, invocation: j}
	if e := o.authorize(ctx); e != nil {
		o.Close()
		return nil, e
	}
	return o, nil
}
func (o *Output) authorize(ctx context.Context) error {
	if o == nil || o.closed.Load() {
		return ErrClosed
	}
	r, j := o.runtime, o.invocation
	r.mu.Lock()
	forbidden := r.closed || j.outputAborted
	r.mu.Unlock()
	if forbidden {
		return ErrClosed
	}
	meta, cancel := r.metadata()
	defer cancel()
	if ctx.Err() != nil || r.control.CheckScope(meta, "INVOKE") != nil {
		return ErrClosed
	}
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	if e != nil || !state.Authorized || state.Expected != j.work.Expected {
		return ErrClosed
	}
	return nil
}
func (o *Output) Descriptor() rentalaccess.TextDescriptor { return o.invocation.work.Descriptor }
func (o *Output) Read(ctx context.Context) ([]byte, error) {
	if e := o.authorize(ctx); e != nil {
		o.Close()
		return nil, e
	}
	stop := context.AfterFunc(ctx, o.Close)
	defer stop()
	data, e := o.runtime.backend.Read(ctx, o.invocation.work)
	if e == io.EOF {
		o.runtime.mu.Lock()
		o.invocation.outputDone = true
		o.runtime.mu.Unlock()
		o.runtime.signal()
	}
	if e2 := o.authorize(ctx); e2 != nil {
		clear(data)
		return nil, e2
	}
	return data, e
}
func (o *Output) Started(ctx context.Context) (TextStart, error) {
	if e := o.authorize(ctx); e != nil {
		o.Close()
		return TextStart{}, e
	}
	stop := context.AfterFunc(ctx, o.Close)
	defer stop()
	v, e := o.runtime.backend.Started(ctx, o.invocation.work)
	if guard := o.authorize(ctx); guard != nil {
		return TextStart{}, guard
	}
	return v, e
}
func (o *Output) Result(ctx context.Context) (TextResult, error) {
	if e := o.authorize(ctx); e != nil {
		o.Close()
		return TextResult{}, e
	}
	v, e := o.runtime.backend.Result(o.invocation.work)
	if guard := o.authorize(ctx); guard != nil {
		return TextResult{}, guard
	}
	return v, e
}
func (o *Output) Close() {
	if o == nil || !o.closed.CompareAndSwap(false, true) {
		return
	}
	o.runtime.stopChild(o.invocation)
}
