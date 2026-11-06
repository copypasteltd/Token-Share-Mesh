package rentalmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
)

type entry struct {
	descriptor                                   rentalaccess.MediaDescriptor
	expected                                     rentalaccess.Expected
	child                                        Child
	submitted                                    chan struct{}
	retireAt                                     time.Time
	stopping, cleaned, opening, opened, released bool
	output                                       *Output
}
type Runtime struct {
	mu                                          sync.Mutex
	ledger                                      *execution.Store
	control                                     Control
	backend                                     Backend
	inputOwner                                  InputOwner
	hold                                        execution.Hold
	prepared                                    rentalaccess.Prepared
	policy                                      rentalaccess.MediaPolicy
	options                                     Options
	entries                                     map[string]*entry
	live                                        *entry
	deadline, prepareDeadline, shutdown         time.Time
	expected                                    rentalaccess.Expected
	authorized, closed, uncertain, stopRecorded bool
	admission                                   context.Context
	cancelAdmission                             context.CancelFunc
	ready, done, wake, deadlineWake             chan struct{}
	invokes, outputs                            sync.WaitGroup
	used                                        atomic.Bool
	result                                      error
}

func New(ledger *execution.Store, control Control, prepared rentalaccess.Prepared, policy rentalaccess.MediaPolicy, backend Backend, o Options) (*Runtime, error) {
	o, e := options(o)
	if e != nil || ledger == nil || control == nil || backend == nil || !policy.Binds(prepared) {
		return nil, ErrClosed
	}
	h, p, mapping := backend.Original()
	m := prepared.Details()
	if h != control.Hold() || h.Owner() != m.Session || h.Resource() != m.Resource || h.LocalGeneration() == "" || p.Digest() != prepared.Digest() || p.ApprovalDigest() != prepared.ApprovalDigest() || mapping != policy {
		return nil, ErrUnknown
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.MetadataTimeout)
	state, e := ledger.RentalCurrent(ctx, h)
	cancel()
	if e != nil || state.ManifestHash != p.Digest() || state.ApprovalHash != p.ApprovalDigest() || (state.Phase != "PREPARING" && state.Phase != "ACTIVE") || state.Deadline.IsZero() || !time.Now().Before(state.Deadline) {
		return nil, ErrUnknown
	}
	admission, end := context.WithCancel(context.Background())
	r := &Runtime{ledger: ledger, control: control, backend: backend, hold: h, prepared: p, policy: policy, options: o, entries: map[string]*entry{}, deadline: state.Deadline, prepareDeadline: state.Deadline, ready: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), deadlineWake: make(chan struct{}, 1), admission: admission, cancelAdmission: end}
	if state.Authorized {
		if !p.BindsGrant(state.Expected) {
			end()
			return nil, ErrUnknown
		}
		r.authorized = true
		r.expected = state.Expected
	}
	return r, nil
}
func (r *Runtime) Ready() <-chan struct{}   { return r.ready }
func (r *Runtime) Done() <-chan struct{}    { return r.done }
func (r *Runtime) Context() context.Context { return r.admission }
func (r *Runtime) RequestStop() {
	if r != nil {
		r.withdraw(false)
	}
}
func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runtime) metadata() (context.Context, context.CancelFunc) {
	r.mu.Lock()
	shutdown := r.shutdown
	r.mu.Unlock()
	limit := time.Now().Add(r.options.MetadataTimeout)
	if !shutdown.IsZero() && shutdown.Before(limit) {
		limit = shutdown
	}
	return context.WithDeadline(context.Background(), limit)
}
func (r *Runtime) call() (context.Context, context.CancelFunc) {
	r.mu.Lock()
	shutdown := r.shutdown
	r.mu.Unlock()
	limit := time.Now().Add(r.options.CallTimeout)
	if !shutdown.IsZero() && shutdown.Before(limit) {
		limit = shutdown
	}
	return context.WithDeadline(context.Background(), limit)
}

// No SQL, result close or native wait precedes the independent stop request.
func (r *Runtime) withdraw(uncertain bool) {
	r.mu.Lock()
	first := !r.closed
	if first {
		r.closed = true
		r.shutdown = time.Now().Add(r.options.ShutdownTimeout)
	}
	r.uncertain = r.uncertain || uncertain
	if r.live != nil {
		r.live.stopping = true
	}
	r.mu.Unlock()
	if first {
		r.cancelAdmission()
		r.backend.RequestStop()
		r.mu.Lock()
		owner := r.inputOwner
		r.mu.Unlock()
		if owner != nil {
			owner.StopInputs()
		}
	}
	r.signal()
}
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
		_ = r.control.RefreshControl(call)
		cancel()
		r.signal()
		timer.Reset(r.options.ControlPoll)
	}
}
func (r *Runtime) observe(state execution.RentalStatus) error {
	if state.Hold != r.hold || state.ManifestHash != r.prepared.Digest() || state.ApprovalHash != r.prepared.ApprovalDigest() {
		return ErrUnknown
	}
	if state.Phase != "PREPARING" && state.Phase != "ACTIVE" {
		r.withdraw(false)
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if state.Deadline.IsZero() || !time.Now().Before(r.deadline) || !time.Now().Before(state.Deadline) {
		return ErrClosed
	}
	if !state.Authorized {
		// Binding a grant without first signed ALLOW cannot extend preparation or
		// admit work. A previously active ALLOW cannot fall back to preparation.
		if r.authorized || !time.Now().Before(r.prepareDeadline) {
			return ErrClosed
		}
		return nil
	}
	if state.Phase != "ACTIVE" || !r.prepared.BindsGrant(state.Expected) || r.authorized && r.expected != state.Expected {
		return ErrUnknown
	}
	r.authorized = true
	r.expected = state.Expected
	r.deadline = state.Deadline
	select {
	case r.deadlineWake <- struct{}{}:
	default:
	}
	return nil
}
func (r *Runtime) inspect() error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil
	}
	meta, cancel := r.metadata()
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	cancel()
	if e != nil {
		return ErrUnknown
	}
	if e = r.observe(state); e != nil {
		return e
	}
	if state.Authorized {
		meta, cancel = r.metadata()
		e = r.control.CheckScope(meta, "INVOKE")
		cancel()
		if e != nil {
			return ErrClosed
		}
	}
	return nil
}

// Run owns the control exchange, independent monotonic deadline and actual
// joins. A caller's timeout never manufactures Done or releases the node lock.
func (r *Runtime) Run(ctx context.Context) (answer error) {
	if r == nil || ctx == nil || !r.used.CompareAndSwap(false, true) {
		return ErrClosed
	}
	background, stop := context.WithCancel(context.Background())
	controlDone, deadlineDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(controlDone); r.refreshLoop(background) }()
	go func() { defer close(deadlineDone); r.deadlineLoop(background) }()
	close(r.ready)
	defer func() {
		r.withdraw(answer != nil)
		stop()
		r.mu.Lock()
		live := r.live
		var output *Output
		if live != nil {
			output = live.output
		}
		r.mu.Unlock()
		if output != nil {
			_ = output.Close()
		}
		r.invokes.Wait()
		if e := r.backend.Join(context.Background()); e != nil {
			answer = ErrUnknown
		}
		r.outputs.Wait()
		<-controlDone
		<-deadlineDone
		r.mu.Lock()
		owner := r.inputOwner
		r.mu.Unlock()
		if owner != nil {
			if owner.JoinInputs(context.Background()) != nil {
				answer = ErrUnknown
			}
		}
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
			r.withdraw(!errors.Is(e, ErrClosed))
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
		if e := r.stepChild(); e != nil {
			r.withdraw(true)
		}
		r.mu.Lock()
		closed, uncertain, live, limit := r.closed, r.uncertain, r.live, r.shutdown
		r.mu.Unlock()
		if closed && live == nil {
			stop()
			<-controlDone
			<-deadlineDone
			if uncertain {
				return ErrUnknown
			}
			return r.finishSession()
		}
		if closed && !time.Now().Before(limit) {
			return ErrUnknown
		}
	}
}
func (r *Runtime) bound(d rentalaccess.MediaDescriptor, current rentalaccess.Expected) bool {
	m := r.prepared.Details()
	return d.Profile == rentalaccess.MediaInputProfile && ids.Valid(d.ID) && d.Session == r.hold.Owner() && d.Resource == r.hold.Resource() && d.LocalGeneration == r.hold.LocalGeneration() && d.Generation == m.Generation && d.Grant == current.Grant && d.GrantDigest == current.TokenDigest && d.ManifestHash == r.prepared.Digest() && d.EnvironmentHash == m.EnvironmentHash && d.RecipeHash == m.RecipeHash && d.PolicyHash == r.policy.Digest() && d.Kind == r.policy.Kind && executionlease.Hash(d.InputCiphertextDigest)
}
func (r *Runtime) Invoke(ctx context.Context, input *rentalaccess.MediaInput) (execution.RentalInvocation, error) {
	if r == nil || ctx == nil || input == nil {
		return execution.RentalInvocation{}, ErrClosed
	}
	defer input.Close()
	select {
	case <-r.ready:
	default:
		return execution.RentalInvocation{}, ErrClosed
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
	if e != nil || !state.Authorized || !r.bound(d, state.Expected) {
		return execution.RentalInvocation{}, ErrClosed
	}
	if e = r.observe(state); e != nil {
		r.withdraw(!errors.Is(e, ErrClosed))
		return execution.RentalInvocation{}, e
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return execution.RentalInvocation{}, ErrClosed
	}
	if r.entries[d.ID] != nil {
		r.mu.Unlock()
		return execution.RentalInvocation{}, ErrQuery
	}
	if r.live != nil || len(r.entries) >= 4096 {
		r.mu.Unlock()
		return execution.RentalInvocation{}, ErrClosed
	}
	j := &entry{descriptor: d, expected: state.Expected, submitted: make(chan struct{})}
	r.entries[d.ID] = j
	r.live = j
	r.invokes.Add(1)
	deadline := r.deadline
	r.mu.Unlock()
	defer r.invokes.Done()
	defer close(j.submitted)
	defer r.signal()
	invoke, end := context.WithDeadline(ctx, deadline)
	defer end()
	stop := context.AfterFunc(r.admission, end)
	defer stop()
	child, e := r.backend.Submit(invoke, input, state.Expected)
	if child != nil {
		v, descriptor, expected := child.Original()
		if descriptor != d || expected != state.Expected || v.ID != d.ID || v.Session != d.Session || v.Resource != d.Resource || v.LocalGeneration != d.LocalGeneration || v.Grant != d.Grant || v.GrantDigest != d.GrantDigest || v.InputCiphertextDigest != d.InputCiphertextDigest || v.RecipeDigest != d.RecipeHash {
			r.withdraw(true)
			return execution.RentalInvocation{}, ErrUnknown
		}
		r.mu.Lock()
		j.child = child
		r.mu.Unlock()
		if e != nil {
			r.withdraw(true)
			return v, ErrQuery
		}
		return v, nil
	}
	// An adapter may have reserved durable inbox/workspace before refusing. Do
	// not drop live or infer ABSENT from the missing in-memory child handle.
	r.withdraw(true)
	return execution.RentalInvocation{}, ErrQuery
}
func (r *Runtime) stepChild() error {
	r.mu.Lock()
	j, closed := r.live, r.closed
	r.mu.Unlock()
	if j == nil {
		return nil
	}
	select {
	case <-j.submitted:
	default:
		return nil
	}
	r.mu.Lock()
	child, stopping, output, opening := j.child, j.stopping, j.output, j.opening
	r.mu.Unlock()
	if child == nil {
		return ErrUnknown
	}
	observation := child.Observation()
	v, _, _ := child.Original()
	if observation.RequestID != j.descriptor.ID || observation.BackendID == "" || v.ID != j.descriptor.ID || observation.State == "UNKNOWN" || observation.State == "ABSENT" {
		return ErrUnknown
	}
	if observation.State != "STOPPED" {
		return nil
	}
	select {
	case <-child.Done():
	default:
		return nil
	}
	r.mu.Lock()
	if j.retireAt.IsZero() {
		j.retireAt = time.Now().Add(r.options.ResultRetention)
	}
	expired := !time.Now().Before(j.retireAt)
	retain := !closed && !stopping && !expired && child.Succeeded() && !j.released
	r.mu.Unlock()
	if retain {
		return nil
	}
	if opening {
		return nil
	}
	if output != nil {
		if e := output.Close(); e != nil {
			return ErrUnknown
		}
	}
	call, cancel := r.call()
	e := child.Cleanup(call)
	cancel()
	if e != nil {
		return ErrUnknown
	}
	meta, cancel := r.metadata()
	remaining, e := r.ledger.RentalInvocation(meta, r.hold, j.descriptor.ID)
	cancel()
	if e != nil || remaining.Phase != "CLEANED" || remaining.Grant != j.expected.Grant || remaining.GrantDigest != j.expected.TokenDigest {
		return ErrUnknown
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
	r.mu.Lock()
	deadline := r.shutdown
	r.mu.Unlock()
	if deadline.IsZero() {
		return ErrUnknown
	}
	call, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	r.mu.Lock()
	owner := r.inputOwner
	r.mu.Unlock()
	if owner != nil && owner.JoinInputs(call) != nil {
		return ErrUnknown
	}
	proof, e := r.backend.End(call)
	if e != nil || proof.Session != r.hold.Owner() || proof.Resource != r.hold.Resource() || proof.LocalGeneration != r.hold.LocalGeneration() || !executionlease.Hash(proof.ManifestHash) || !proof.WorkStopped || !proof.FutureStartsRevoked || !proof.WorkspaceCleared || !proof.OutputKeysHandled {
		return ErrUnknown
	}
	meta, cancel := r.metadata()
	e = r.ledger.ObserveRentalSessionStopped(meta, r.hold, execution.RentalSessionStop{Session: proof.Session, LocalGeneration: proof.LocalGeneration, ScopeDigest: proof.ManifestHash, AllRequestsStopped: true, FutureStartsRevoked: true})
	cancel()
	if e != nil {
		return ErrUnknown
	}
	meta, cancel = r.metadata()
	e = r.ledger.FinishRentalSessionCleanup(meta, r.hold, execution.RentalSessionCleanup{Session: proof.Session, LocalGeneration: proof.LocalGeneration, ManifestHash: proof.ManifestHash, WorkStopped: true, AllGrantsRevoked: true, WorkspaceCleared: true, OutputKeysHandled: true})
	cancel()
	if e != nil {
		return ErrUnknown
	}
	return nil
}
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrClosed
	}
	r.withdraw(false)
	select {
	case <-ctx.Done():
		return ErrUnknown
	case <-r.done:
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.result
	}
}
func (r *Runtime) originalChild(ctx context.Context, id, scope string) (*entry, error) {
	if r == nil || ctx == nil || ctx.Err() != nil || !ids.Valid(id) {
		return nil, ErrClosed
	}
	meta, cancel := r.metadata()
	e := r.control.CheckScope(meta, scope)
	cancel()
	if e != nil {
		return nil, ErrClosed
	}
	meta, cancel = r.metadata()
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	cancel()
	if e != nil || !state.Authorized {
		return nil, ErrClosed
	}
	if e = r.observe(state); e != nil {
		r.withdraw(!errors.Is(e, ErrClosed))
		return nil, e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.entries[id]
	if r.closed || j == nil || j.expected != state.Expected {
		return nil, ErrClosed
	}
	return j, nil
}
func (r *Runtime) Query(ctx context.Context, id string) (execution.RentalInvocation, error) {
	j, e := r.originalChild(ctx, id, "QUEUE_SELF")
	if e != nil {
		return execution.RentalInvocation{}, e
	}
	meta, cancel := r.metadata()
	v, e := r.ledger.RentalInvocation(meta, r.hold, id)
	cancel()
	if e != nil || v.Grant != j.expected.Grant || v.GrantDigest != j.expected.TokenDigest {
		return execution.RentalInvocation{}, ErrQuery
	}
	return v, nil
}
func (r *Runtime) Cancel(ctx context.Context, id string) error {
	j, e := r.originalChild(ctx, id, "CANCEL_SELF")
	if e != nil {
		return e
	}
	r.mu.Lock()
	if j.cleaned {
		r.mu.Unlock()
		return nil
	}
	j.stopping = true
	r.mu.Unlock()
	call, cancel := r.call()
	e = r.backend.CancelOriginal(call, id)
	cancel()
	r.signal()
	meta, cancel := r.metadata()
	record := r.ledger.StopRentalInvocation(meta, r.hold, id)
	cancel()
	if e != nil || record != nil {
		r.withdraw(true)
		return ErrUnknown
	}
	return nil
}

// Output is a single original authenticated result reader. Close does not
// assert delivery/ACK/usage. It only releases local custody for child cleanup.
type Output struct{ state *outputState }
type outputState struct {
	mu      sync.Mutex
	runtime *Runtime
	entry   *entry
	reader  Reader
	closed  bool
}

func (o Output) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original rental media output redacted]")
}
func (o Output) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (r *Runtime) Output(ctx context.Context, id string) (*Output, error) {
	j, e := r.originalChild(ctx, id, "EXPORT_SELF")
	if e != nil {
		return nil, e
	}
	r.mu.Lock()
	if r.closed || j.cleaned || j.stopping || j.child == nil || j.opening || j.opened || j.released {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	j.opening = true
	r.outputs.Add(1)
	child := j.child
	r.mu.Unlock()
	var output *Output
	defer func() {
		r.mu.Lock()
		j.opening = false
		r.mu.Unlock()
		if output == nil {
			r.outputs.Done()
		}
		r.signal()
	}()
	if !child.Succeeded() {
		return nil, ErrClosed
	}
	custody, e := child.Result()
	if e != nil {
		return nil, ErrClosed
	}
	opening, cancel := context.WithTimeout(ctx, r.options.MetadataTimeout)
	reader, e := custody.Open(opening)
	cancel()
	if e != nil {
		custody.Close()
		return nil, ErrUnknown
	}
	r.mu.Lock()
	if r.closed || j.stopping || j.cleaned {
		r.mu.Unlock()
		reader.Close()
		custody.Close()
		return nil, ErrClosed
	}
	output = &Output{state: &outputState{runtime: r, entry: j, reader: reader}}
	j.output = output
	j.opened = true
	r.mu.Unlock()
	return output, nil
}
func (o *Output) Read(ctx context.Context, dst []byte) (int, error) {
	if o == nil || o.state == nil || ctx == nil {
		return 0, ErrClosed
	}
	s := o.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	bounded, cancel := context.WithTimeout(ctx, s.runtime.options.MetadataTimeout)
	defer cancel()
	if _, e := s.runtime.originalChild(bounded, s.entry.descriptor.ID, "EXPORT_SELF"); e != nil {
		return 0, ErrClosed
	}
	return s.reader.Read(bounded, dst)
}
func (o *Output) Seek(ctx context.Context, offset int64, whence int) (int64, error) {
	if o == nil || o.state == nil || ctx == nil {
		return 0, ErrClosed
	}
	s := o.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	bounded, cancel := context.WithTimeout(ctx, s.runtime.options.MetadataTimeout)
	defer cancel()
	if _, e := s.runtime.originalChild(bounded, s.entry.descriptor.ID, "EXPORT_SELF"); e != nil {
		return 0, ErrClosed
	}
	return s.reader.Seek(bounded, offset, whence)
}
func (o *Output) Close() error {
	if o == nil || o.state == nil {
		return nil
	}
	s := o.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	e := s.reader.Close()
	r := s.runtime
	r.mu.Lock()
	s.entry.released = true
	r.mu.Unlock()
	r.outputs.Done()
	r.signal()
	return e
}
func (r *Runtime) Discard(ctx context.Context, id string) error {
	j, e := r.originalChild(ctx, id, "EXPORT_SELF")
	if e != nil {
		return e
	}
	r.mu.Lock()
	if j.opening {
		r.mu.Unlock()
		return ErrClosed
	}
	j.released = true
	output := j.output
	r.mu.Unlock()
	if output != nil {
		if output.Close() != nil {
			return ErrUnknown
		}
	}
	r.signal()
	return nil
}

func (r *Runtime) BindInputOwner(owner InputOwner) error {
	if r == nil || owner == nil {
		return ErrClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used.Load() || r.closed || r.inputOwner != nil {
		return ErrClosed
	}
	r.inputOwner = owner
	return nil
}
