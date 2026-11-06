package node

import (
	"context"
	"sync"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalruntime"
)

// NativeRental owns one explicit local installation, the existing original
// physical RENTAL hold, its watchdog, pending sealed inputs and native worker.
// No listener/model auto-download, payment or remote readiness proof is added.
type NativeRental struct {
	mu             sync.Mutex
	provider       *ProviderRental
	policy         rentalaccess.TextPolicy
	backend        *llamaworker.RentalAdapter
	runtime        *rentalruntime.Runtime
	info           llamaworker.LocalRuntimeInfo
	life           context.Context
	cancel         context.CancelFunc
	openDone, done chan struct{}
	inputs         map[*rentalaccess.TextInput]bool
	closing        bool
	requestOnce    sync.Once
	result         error
}

// OpenNative loads only an independently approved package while the SAME
// physical session is PREPARING. It cannot acquire another maintenance/job
// owner, select another GPU, or silently fall back to a CPU/model/LM Studio.
func (r *ProviderRental) OpenNative(ctx context.Context, install NativeInstallation, policy rentalaccess.TextPolicy, options rentalruntime.Options) (_ *NativeRental, answer error) {
	if r == nil || r.node == nil || !policy.Binds(r.prepared) || !r.journal.matchesPreparation(r.prepared) {
		return nil, ErrRentalJournal
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return nil, ErrRentalJournal
	}
	node := r.node
	life, cancel := context.WithCancel(context.Background())
	s := &NativeRental{provider: r, policy: policy, life: life, cancel: cancel, openDone: make(chan struct{}), done: make(chan struct{}), inputs: map[*rentalaccess.TextInput]bool{}}
	node.nativeMu.Lock()
	if node.closingNative || node.root == nil || node.poisoned || node.nativeRentals[r.hold.Resource()] != nil || node.comfyRentals[r.hold.Resource()] != nil || len(node.nativeRentals)+len(node.comfyRentals) >= execution.MaximumResources {
		node.nativeMu.Unlock()
		cancel()
		return nil, ErrHostPending
	}
	if node.nativeRentals == nil {
		node.nativeRentals = map[string]*NativeRental{}
	}
	node.nativeRentals[r.hold.Resource()] = s
	node.nativeMu.Unlock()
	watchStarted := false
	defer close(s.openDone)
	defer func() {
		if answer == nil {
			return
		}
		s.requestClose()
		if watchStarted {
			return
		} // The running watchdog owns Done and cleanup.
		stop, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		r.Stop(stop)
		if s.backend != nil {
			s.backend.CloseSession(stop, r.hold)
			s.backend.Join(context.Background())
		}
		s.mu.Lock()
		s.result = ErrRentalJournal
		s.mu.Unlock()
		close(s.done)
	}()
	meta, end := context.WithTimeout(ctx, 2*time.Second)
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	end()
	if e != nil || state.Phase != "PREPARING" || state.ManifestHash != r.prepared.Digest() || state.ApprovalHash != r.prepared.ApprovalDigest() || state.Deadline.IsZero() || !time.Now().Before(state.Deadline) {
		return nil, ErrRentalJournal
	}
	opening, finish := context.WithDeadline(ctx, state.Deadline)
	defer finish()
	stopLife := context.AfterFunc(life, finish)
	defer stopLife()
	c, e := llamaworker.LoadPackage(opening, install.RuntimeRoot, install.Package, install.Selection, install.Environment)
	if e != nil {
		return nil, e
	}
	backend, e := llamaworker.OpenRental(opening, c, r.prepared, policy)
	if e != nil {
		return nil, e
	}
	s.backend = backend
	s.info = backend.LocalInfo()
	watch, e := rentalruntime.New(r.ledger, r, r.prepared, policy, backend, options)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	s.runtime = watch
	stopped := s.closing || life.Err() != nil
	s.mu.Unlock()
	if stopped {
		return nil, ErrRentalJournal
	}
	watchStarted = true
	go func() {
		e := watch.Run(life)
		s.mu.Lock()
		s.closing = true
		for input := range s.inputs {
			input.Close()
		}
		clear(s.inputs)
		s.result = e
		s.mu.Unlock()
		if e == nil {
			node.nativeMu.Lock()
			if node.nativeRentals[r.hold.Resource()] == s {
				delete(node.nativeRentals, r.hold.Resource())
			}
			node.nativeMu.Unlock()
		}
		close(s.done)
	}()
	// No inference is possible before this original watchdog starts. Ordinary
	// preparation may finish before grant/first ALLOW, and remains unbilled.
	select {
	case <-watch.Ready():
	case <-s.done:
		return nil, ErrRentalJournal
	case <-opening.Done():
		s.requestClose()
		<-s.done
		return nil, ErrRentalJournal
	}
	return s, nil
}

func (s *NativeRental) Info() llamaworker.LocalRuntimeInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}
func (s *NativeRental) Done() <-chan struct{} { return s.done }
func (s *NativeRental) requestClose() {
	if s == nil {
		return
	}
	s.requestOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		for input := range s.inputs {
			input.Close()
		}
		clear(s.inputs)
		s.cancel()
		s.mu.Unlock()
	})
}

// SealTextInput checks the current original owner AND Bearer independently for
// every call. The local AES-GCM input key never becomes a journal/SQL field.
// At most eight not-yet-consumed input envelopes are owned by this session.
func (s *NativeRental) SealTextInput(ctx context.Context, verifier *rentalaccess.Verifier, credential, id string, raw []byte) (*rentalaccess.TextInput, error) {
	if s == nil || ctx.Err() != nil {
		return nil, ErrRentalJournal
	}
	s.mu.Lock()
	closed := s.closing || s.runtime == nil || len(s.inputs) >= 8
	s.mu.Unlock()
	if closed {
		return nil, ErrRentalJournal
	}
	if e := s.provider.CheckScope(ctx, "INVOKE"); e != nil {
		return nil, e
	}
	current, e := s.provider.current(ctx)
	if e != nil {
		return nil, e
	}
	input, e := rentalaccess.SealTextInput(s.provider.prepared, verifier, credential, current, id, s.provider.hold.LocalGeneration(), s.policy, raw, time.Now())
	if e != nil {
		return nil, e
	}
	if e = s.provider.CheckScope(ctx, "INVOKE"); e != nil {
		input.Close()
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || len(s.inputs) >= 8 {
		input.Close()
		return nil, ErrRentalJournal
	}
	s.inputs[input] = true
	return input, nil
}
func (s *NativeRental) Invoke(ctx context.Context, input *rentalaccess.TextInput) (execution.RentalInvocation, error) {
	if s == nil || input == nil {
		return execution.RentalInvocation{}, ErrRentalJournal
	}
	s.mu.Lock()
	owned := s.inputs[input]
	admitted := owned && !s.closing && s.runtime != nil
	if owned {
		delete(s.inputs, input)
	}
	watch := s.runtime
	s.mu.Unlock()
	if !admitted {
		if owned {
			input.Close()
		}
		return execution.RentalInvocation{}, ErrRentalJournal
	}
	defer input.Close()
	return watch.Invoke(ctx, input)
}

// DiscardInput returns an unused envelope's capacity as well as clearing its
// owned key. Closing an envelope alone does not remove it from this owner map.
func (s *NativeRental) DiscardInput(input *rentalaccess.TextInput) {
	if s == nil || input == nil {
		return
	}
	s.mu.Lock()
	owned := s.inputs[input]
	delete(s.inputs, input)
	s.mu.Unlock()
	if owned {
		input.Close()
	}
}
func (s *NativeRental) access(ctx context.Context, verifier *rentalaccess.Verifier, credential, scope string) (*rentalruntime.Runtime, error) {
	if s == nil || verifier == nil || ctx.Err() != nil {
		return nil, ErrRentalJournal
	}
	s.mu.Lock()
	watch, closed := s.runtime, s.closing
	s.mu.Unlock()
	if closed || watch == nil {
		return nil, ErrRentalJournal
	}
	if e := s.provider.CheckScope(ctx, scope); e != nil {
		return nil, e
	}
	current, e := s.provider.current(ctx)
	if e != nil {
		return nil, e
	}
	grant, e := verifier.Verify(credential, current, time.Now())
	if e != nil || !grant.Allows(scope, time.Now()) {
		return nil, ErrRentalJournal
	}
	return watch, nil
}
func (s *NativeRental) Query(ctx context.Context, verifier *rentalaccess.Verifier, credential, id string) (execution.RentalInvocation, error) {
	watch, e := s.access(ctx, verifier, credential, "QUEUE_SELF")
	if e != nil {
		return execution.RentalInvocation{}, e
	}
	return watch.Query(ctx, id)
}
func (s *NativeRental) Cancel(ctx context.Context, verifier *rentalaccess.Verifier, credential, id string) error {
	watch, e := s.access(ctx, verifier, credential, "CANCEL_SELF")
	if e != nil {
		return e
	}
	return watch.Cancel(ctx, id)
}
func (s *NativeRental) Output(ctx context.Context, verifier *rentalaccess.Verifier, credential, id string) (*rentalruntime.Output, error) {
	watch, e := s.access(ctx, verifier, credential, "INVOKE")
	if e != nil {
		return nil, e
	}
	return watch.Output(ctx, id)
}
func (s *NativeRental) Close(ctx context.Context) error {
	if s == nil {
		return ErrRentalJournal
	}
	s.requestClose()
	select {
	case <-s.openDone:
	case <-ctx.Done():
		return ErrRentalJournal
	}
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	case <-ctx.Done():
		return ErrRentalJournal
	}
}

// Close may return before a stuck owned operation has joined. The original OS
// lock/key/ledger must then remain owned; the parent arranges final closure only
// after every native/text and Comfy/media Done has actually closed. No process
// is orphaned, and a timed-out caller cannot release the original node master.
func (s *Store) closeNativeRentals() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	return s.closeNativeRentalsWithin(ctx)
}
func (s *Store) closeNativeRentalsWithin(ctx context.Context) bool {
	s.nativeMu.Lock()
	s.closingNative = true
	all := make([]ownedRental, 0, len(s.nativeRentals)+len(s.comfyRentals))
	for _, n := range s.nativeRentals {
		all = append(all, n)
	}
	for _, n := range s.comfyRentals {
		all = append(all, n)
	}
	s.nativeMu.Unlock()
	for _, n := range all {
		n.requestClose()
	}
	complete := true
	for _, n := range all {
		n.Close(ctx)
		select {
		case <-n.Done():
		default:
			complete = false
		}
	}
	if complete {
		return true
	}
	s.poisoned = true
	go func() {
		for _, n := range all {
			<-n.Done()
		}
		s.finishClose()
	}()
	return false
}
