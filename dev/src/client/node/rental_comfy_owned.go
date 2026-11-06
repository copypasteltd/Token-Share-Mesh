package node

import (
	"context"
	"sync"
	"time"

	"p11.local/compute/client/adapters/comfyworker"
	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalmedia"
	mediagateway "p11.local/compute/gateway/media"
)

// ComfyRental is the ORIGINAL node-owned prepared media session. Its watchdog
// starts before any buyer authorization; no local preparation implies paid
// readiness, real hardware qualification, E2E or permission to execute.
type ComfyRental struct {
	mu                                        sync.Mutex
	provider                                  *ProviderRental
	use                                       comfyenv.Use
	process                                   *comfyworker.Process
	adapter                                   *comfyworker.RentalMediaAdapter
	runtime                                   *rentalmedia.Runtime
	gateway                                   *mediagateway.Handler
	openingGateway                            bool
	policy                                    rentalaccess.MediaPolicy
	life                                      context.Context
	cancel                                    context.CancelFunc
	openDone, done, inputsDone                chan struct{}
	inputs                                    map[*rentalaccess.MediaInput]bool
	sealing, authorizing, authorized, closing bool
	operations                                sync.WaitGroup
	closeOnce                                 sync.Once
	result                                    error
}

func (r *ProviderRental) OpenComfy(ctx context.Context, plan comfyenv.Plan, selection comfyworker.RuntimeSelection, policy rentalaccess.MediaPolicy, options rentalmedia.Options) (_ *ComfyRental, answer error) {
	if r == nil || r.node == nil || ctx == nil || ctx.Err() != nil || r.prepared.Details().Engine != "COMFYUI" || !policy.Binds(r.prepared) {
		return nil, ErrRentalJournal
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed || !r.journal.matchesPreparation(r.prepared) {
		return nil, ErrRentalJournal
	}
	node := r.node
	life, cancel := context.WithCancel(context.Background())
	session := &ComfyRental{provider: r, policy: policy, life: life, cancel: cancel, openDone: make(chan struct{}), done: make(chan struct{}), inputsDone: make(chan struct{}), inputs: map[*rentalaccess.MediaInput]bool{}}
	node.nativeMu.Lock()
	if node.closingNative || node.root == nil || node.poisoned || node.media == nil || node.nativeRentals[r.hold.Resource()] != nil || node.comfyRentals[r.hold.Resource()] != nil || len(node.nativeRentals)+len(node.comfyRentals) >= execution.MaximumResources {
		node.nativeMu.Unlock()
		cancel()
		return nil, ErrHostPending
	}
	if node.comfyRentals == nil {
		node.comfyRentals = map[string]*ComfyRental{}
	}
	node.comfyRentals[r.hold.Resource()] = session
	node.nativeMu.Unlock()
	watching := false
	defer close(session.openDone)
	defer func() {
		if answer == nil {
			return
		}
		session.requestClose()
		if watching {
			return
		}
		// If opening fails, actual process/input joins still own Done and the node
		// lock. A caller cancellation must not leave an untracked prepared worker.
		go func() {
			stop, end := context.WithTimeout(context.Background(), 2*time.Second)
			_ = r.Stop(stop)
			end()
			session.mu.Lock()
			process, adapter := session.process, session.adapter
			session.mu.Unlock()
			if adapter != nil {
				_ = adapter.Join(context.Background())
			} else if process != nil {
				_ = process.Close(context.Background())
			}
			session.use.Close()
			_ = session.use.Join(context.Background())
			session.operations.Wait()
			session.mu.Lock()
			session.result = ErrRentalJournal
			session.mu.Unlock()
			close(session.done)
		}()
	}()
	// The caller controls opening only. After success the node owns life until
	// explicit stop/withdrawal, not until the HTTP request used for opening ends.
	stopOpening := context.AfterFunc(ctx, session.requestClose)
	defer stopOpening()
	meta, end := context.WithTimeout(ctx, 2*time.Second)
	state, e := r.ledger.RentalCurrent(meta, r.hold)
	end()
	if e != nil || state.Phase != "PREPARING" || state.ManifestHash != r.prepared.Digest() || state.ApprovalHash != r.prepared.ApprovalDigest() || state.Deadline.IsZero() || !time.Now().Before(state.Deadline) {
		return nil, ErrRentalJournal
	}
	use, e := node.OpenComfyUse(life, r.hold, r.prepared, plan)
	if e != nil {
		return nil, e
	}
	session.mu.Lock()
	session.use = use
	session.mu.Unlock()
	process, e := comfyworker.OpenPrepared(life, use, selection)
	if e != nil {
		use.Close()
		return nil, e
	}
	session.mu.Lock()
	session.process = process
	stopped := session.closing
	session.mu.Unlock()
	if stopped {
		process.RequestStop()
		return nil, ErrRentalJournal
	}
	adapter, e := r.BindPreparedComfyMedia(process, policy)
	if e != nil {
		return nil, e
	}
	session.mu.Lock()
	session.adapter = adapter
	session.mu.Unlock()
	watch, e := rentalmedia.New(r.ledger, r, r.prepared, policy, rentalmedia.Comfy(adapter), options)
	if e != nil {
		return nil, e
	}
	if e = watch.BindInputOwner(session); e != nil {
		return nil, e
	}
	session.mu.Lock()
	session.runtime = watch
	stopped = session.closing
	session.mu.Unlock()
	if stopped {
		watch.RequestStop()
		return nil, ErrRentalJournal
	}
	watching = true
	go func() {
		result := watch.Run(life)
		session.requestClose()
		session.operations.Wait()
		_ = session.use.Join(context.Background())
		session.mu.Lock()
		session.result = result
		session.mu.Unlock()
		if result == nil {
			node.nativeMu.Lock()
			if node.comfyRentals[r.hold.Resource()] == session {
				delete(node.comfyRentals, r.hold.Resource())
			}
			node.nativeMu.Unlock()
		}
		close(session.done)
	}()
	select {
	case <-watch.Ready():
	case <-session.done:
		return nil, ErrRentalJournal
	case <-ctx.Done():
		session.requestClose()
		return nil, ErrRentalJournal
	}
	session.mu.Lock()
	stopped = session.closing
	session.mu.Unlock()
	if stopped {
		return nil, ErrRentalJournal
	}
	return session, nil
}
func (s *ComfyRental) Done() <-chan struct{} { return s.done }
func (s *ComfyRental) requestClose() {
	if s == nil {
		return
	}
	first := false
	var watch *rentalmedia.Runtime
	var process *comfyworker.Process
	var gateway *mediagateway.Handler
	var list []*rentalaccess.MediaInput
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		watch, process = s.runtime, s.process
		gateway = s.gateway
		list = make([]*rentalaccess.MediaInput, 0, len(s.inputs))
		for input := range s.inputs {
			list = append(list, input)
		}
		clear(s.inputs)
		s.mu.Unlock()
		first = true
	})
	if !first {
		return
	}
	// Invoke lifecycle hooks only AFTER the once-latch returns. The runtime may
	// call this same owner back while withdrawing, without reentering sync.Once.
	if watch != nil {
		watch.RequestStop()
	} else if process != nil {
		process.RequestStop()
	}
	s.cancel()
	if gateway != nil {
		gateway.RequestStop()
	}
	for _, input := range list {
		input.Close()
	}
	go func() {
		s.operations.Wait()
		if gateway != nil {
			_ = gateway.Join(context.Background())
		}
		if s.inputsDone != nil {
			close(s.inputsDone)
		}
	}()
}

// Authorize resolves the platform's original digest, commits first signed
// ALLOW, then waits the SAME private process AUTHORIZE/ACK. It is once-only;
// it cannot replace or extend a paid term. Paid renewal remains a separate gate.
func (s *ComfyRental) Authorize(ctx context.Context, verifier *rentalaccess.Verifier, credential string, approved rentalaccess.Expected) error {
	if s == nil || ctx == nil || verifier == nil {
		return ErrRentalJournal
	}
	s.mu.Lock()
	if s.closing || s.authorizing || s.authorized || s.runtime == nil || s.process == nil {
		s.mu.Unlock()
		return ErrRentalJournal
	}
	s.authorizing = true
	s.operations.Add(1)
	process := s.process
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.authorizing = false; s.mu.Unlock(); s.operations.Done() }()
	call, end := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(s.life, end)
	defer end()
	defer stop()
	if e := s.provider.AttachControl(call, verifier, credential, approved); e != nil {
		s.requestClose()
		return e
	}
	if e := s.provider.RefreshControl(call); e != nil {
		s.requestClose()
		return e
	}
	current, e := s.provider.current(call)
	if e != nil || current != approved {
		s.requestClose()
		return ErrRentalJournal
	}
	grant, e := verifier.Verify(credential, current, time.Now().UTC())
	if e != nil || !grant.Allows("INVOKE", time.Now().UTC()) {
		s.requestClose()
		return ErrRentalJournal
	}
	if e = process.Authorize(call, grant); e != nil {
		s.requestClose()
		return e
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return ErrRentalJournal
	}
	s.authorized = true
	s.mu.Unlock()
	return nil
}

// SealMediaInput owns at most one unsubmitted envelope or one active seal.
// It takes ownership of every asset Source even on refusal. The runtime's
// admission context interrupts local input reads on expiry/withdrawal.
func (s *ComfyRental) SealMediaInput(ctx context.Context, verifier *rentalaccess.Verifier, credential, id string, parameters []byte, assets []rentalaccess.MediaAsset) (*rentalaccess.MediaInput, error) {
	accepted := false
	defer func() {
		if !accepted {
			for _, asset := range assets {
				if asset.Source != nil {
					_ = asset.Source.Close()
				}
			}
		}
	}()
	if s == nil || ctx == nil || verifier == nil || ctx.Err() != nil {
		return nil, ErrRentalJournal
	}
	s.mu.Lock()
	if s.closing || !s.authorized || s.sealing || len(s.inputs) != 0 || s.runtime == nil {
		s.mu.Unlock()
		return nil, ErrRentalJournal
	}
	s.sealing = true
	s.operations.Add(1)
	watch := s.runtime
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.sealing = false; s.mu.Unlock(); s.operations.Done() }()
	seal, end := context.WithCancel(ctx)
	stop := context.AfterFunc(watch.Context(), end)
	defer end()
	defer stop()
	if e := s.provider.CheckScope(seal, "INVOKE"); e != nil {
		return nil, e
	}
	current, e := s.provider.current(seal)
	if e != nil {
		return nil, e
	}
	accepted = true // the local envelope constructor now owns/joins every source
	input, e := rentalaccess.SealMediaInput(seal, s.provider.prepared, verifier, credential, current, id, s.provider.hold.LocalGeneration(), s.policy, parameters, assets, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	if e = s.provider.CheckScope(seal, "INVOKE"); e != nil {
		input.Close()
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || len(s.inputs) != 0 {
		input.Close()
		return nil, ErrRentalJournal
	}
	s.inputs[input] = true
	return input, nil
}
func (s *ComfyRental) DiscardInput(input *rentalaccess.MediaInput) {
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
func (s *ComfyRental) Invoke(ctx context.Context, input *rentalaccess.MediaInput) (execution.RentalInvocation, error) {
	if s == nil || ctx == nil || input == nil {
		return execution.RentalInvocation{}, ErrRentalJournal
	}
	s.mu.Lock()
	owned := s.inputs[input]
	if owned {
		delete(s.inputs, input)
	}
	watch := s.runtime
	allowed := owned && !s.closing && s.authorized && watch != nil
	if allowed {
		s.operations.Add(1)
	}
	s.mu.Unlock()
	if !allowed {
		if owned {
			input.Close()
		}
		return execution.RentalInvocation{}, ErrRentalJournal
	}
	defer s.operations.Done()
	return watch.Invoke(ctx, input)
}
func (s *ComfyRental) access(ctx context.Context, verifier *rentalaccess.Verifier, credential, scope string) (*rentalmedia.Runtime, error) {
	if s == nil || ctx == nil || verifier == nil || ctx.Err() != nil {
		return nil, ErrRentalJournal
	}
	s.mu.Lock()
	watch, closed, authorized := s.runtime, s.closing, s.authorized
	s.mu.Unlock()
	if closed || !authorized || watch == nil {
		return nil, ErrRentalJournal
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if e := s.provider.CheckScope(bounded, scope); e != nil {
		return nil, e
	}
	current, e := s.provider.current(bounded)
	if e != nil {
		return nil, e
	}
	grant, e := verifier.Verify(credential, current, time.Now().UTC())
	if e != nil || !grant.Allows(scope, time.Now().UTC()) {
		return nil, ErrRentalJournal
	}
	return watch, nil
}
func (s *ComfyRental) Query(ctx context.Context, v *rentalaccess.Verifier, credential, id string) (execution.RentalInvocation, error) {
	if !s.enterAccess() {
		return execution.RentalInvocation{}, ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "QUEUE_SELF")
	if e != nil {
		return execution.RentalInvocation{}, e
	}
	return watch.Query(ctx, id)
}
func (s *ComfyRental) Cancel(ctx context.Context, v *rentalaccess.Verifier, credential, id string) error {
	if !s.enterAccess() {
		return ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "CANCEL_SELF")
	if e != nil {
		return e
	}
	return watch.Cancel(ctx, id)
}
func (s *ComfyRental) Output(ctx context.Context, v *rentalaccess.Verifier, credential, id string) (*rentalmedia.Output, error) {
	if !s.enterAccess() {
		return nil, ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "EXPORT_SELF")
	if e != nil {
		return nil, e
	}
	return watch.Output(ctx, id)
}
func (s *ComfyRental) DiscardResult(ctx context.Context, v *rentalaccess.Verifier, credential, id string) error {
	if !s.enterAccess() {
		return ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "EXPORT_SELF")
	if e != nil {
		return e
	}
	return watch.Discard(ctx, id)
}
func (s *ComfyRental) Close(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrRentalJournal
	}
	s.requestClose()
	select {
	case <-ctx.Done():
		return ErrRentalJournal
	case <-s.openDone:
	}
	select {
	case <-ctx.Done():
		return ErrRentalJournal
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	}
}

// Both original desktop rental types participate in the same node close audit.
type ownedRental interface {
	requestClose()
	Close(context.Context) error
	Done() <-chan struct{}
}

func (s *ComfyRental) enterAccess() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.operations.Add(1)
	return true
}

func (s *ComfyRental) StopInputs() { s.requestClose() }
func (s *ComfyRental) JoinInputs(ctx context.Context) error {
	if s == nil || ctx == nil || s.inputsDone == nil {
		return ErrRentalJournal
	}
	select {
	case <-ctx.Done():
		return ErrRentalJournal
	case <-s.inputsDone:
		return nil
	}
}
