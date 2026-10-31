package node

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"p11.local/compute/client/protocol/executionlease"
	"path/filepath"
	"sync"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/gpuid"
)

// NativeInstallation is independent LOCAL owner configuration, never a task,
// downloaded manifest or a source of approval. One resident native environment
// is owned by a Host at a time; switching requires closing the previous session.
type NativeInstallation struct {
	RuntimeRoot string
	Package     llamaworker.FilePin
	Selection   llamaworker.OwnerSelection
	Environment string
}

type NativeSession struct {
	host                           *Host
	mu                             sync.Mutex
	native                         *llamaworker.Adapter
	spool                          *results.Store
	backend                        *results.Adapter
	outputs                        map[*nativeOutput]bool
	runCancel                      context.CancelFunc
	watch                          *supervisor.Supervisor
	info                           llamaworker.LocalRuntimeInfo
	resource, capability, physical string
	openDone, runDone, closeDone   chan struct{}
	runStarted, closing, closed    bool
	runErr, closeErr               error
}

func nativeRuleMatches(c llamaworker.Config, physical string, r HostRule) bool {
	a := r.Approval
	return physical != "" && r.PhysicalDigest == physical && a.Resource == c.Resource &&
		a.Capability == c.Capability && a.EnvironmentDigest == c.Environment &&
		a.EngineKind == "LLAMA_CPP_NATIVE" && a.EngineRelease == llamaworker.EngineRelease &&
		a.ModelDigest == c.Model.SHA256 && a.RecipeDigest == c.Template.SHA256 &&
		a.Mode == "PLAINTEXT" && a.Privacy == "STANDARD" &&
		uint64(a.InputTokenLimit)+uint64(a.Spec.MaxOutputTokens) <= uint64(c.ContextTokens)
}

// approveNative binds ALL rules for this environment/capability, including
// resource, model, recipe and physical gate. A conflicting rule cannot be
// ignored merely because another rule happens to match.
func (h *Host) approveNative(c llamaworker.Config) (string, error) {
	physical, err := gpuid.Digest(c.GPUUUID)
	if err != nil || c.GPULayers == 0 {
		return "", ErrHostPolicy
	}
	matched := false
	for _, r := range h.config.Rules {
		if r.Approval.EnvironmentDigest == c.Environment && r.Approval.Capability == c.Capability {
			if !nativeRuleMatches(c, physical, r) {
				return "", ErrHostPolicy
			}
			matched = true
		}
	}
	if !matched {
		return "", ErrHostPolicy
	}
	return physical, nil
}

func nativeIdentityMatches(c llamaworker.Config, physical string, i llamaworker.LocalRuntimeInfo) bool {
	return i.ProcessID > 0 && i.Profile == llamaworker.Profile && i.UpstreamCommit == llamaworker.UpstreamCommit &&
		i.EngineRelease == llamaworker.EngineRelease && i.Environment == c.Environment &&
		i.Resource == c.Resource && i.Capability == c.Capability && i.ModelSHA256 == c.Model.SHA256 &&
		i.TemplateSHA256 == c.Template.SHA256 && i.SelectedGPUDevice == c.GPUDevice &&
		i.SelectedGPULayers == c.GPULayers && i.ObservedGPU.Validate() == nil &&
		i.ObservedGPU.Device == c.GPUDevice && i.ObservedGPU.UUID == c.GPUUUID && i.ObservedGPU.PCI == c.GPUPCI &&
		i.PhysicalDigest == physical
}

// OpenNative composes the owned native worker, encrypted result scope and
// ORIGINAL Host supervisor. It must follow Prepare of an approved command.
// No public listener, device qualification, lease or payment is created here.
func (h *Host) OpenNative(ctx context.Context, install NativeInstallation, options supervisor.Options) (_ *NativeSession, answer error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, ErrHost
	}
	id := "native_open_" + hex.EncodeToString(nonce[:])
	owned, finish, err := h.beginWork(ctx, id)
	if err != nil {
		return nil, err
	}
	defer finish()
	s := &NativeSession{host: h, openDone: make(chan struct{}), runDone: make(chan struct{}), closeDone: make(chan struct{})}
	h.mu.Lock()
	for w := range h.watchdogs {
		select {
		case <-w.Done():
			delete(h.watchdogs, w)
		default:
		}
	}
	if h.closed || h.nativeSession != nil || len(h.watchdogs) != 0 {
		h.mu.Unlock()
		return nil, ErrHostPending
	}
	h.nativeSession = s
	h.mu.Unlock()
	defer close(s.openDone)
	defer func() {
		if answer == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.native != nil && s.native.Close(cleanup) != nil {
			h.fail()
		}
		if s.spool != nil && s.spool.Close(cleanup) != nil {
			h.fail()
		}
		h.mu.Lock()
		if h.nativeSession == s {
			h.nativeSession = nil
		}
		h.mu.Unlock()
	}()
	c, err := llamaworker.LoadPackage(owned, install.RuntimeRoot, install.Package, install.Selection, install.Environment)
	if err != nil {
		return nil, ErrHostPolicy
	}
	physical, err := h.approveNative(c)
	if err != nil {
		return nil, err
	}
	// Validate the original command registry before mapping a model or taking a
	// maintenance generation. NewSupervisor repeats it after actual READY.
	if err = h.hasNativeCommand(owned, c); err != nil {
		return nil, err
	}
	s.resource, s.capability, s.physical = c.Resource, c.Capability, physical
	hold, err := h.ledger.AcquireLocal(owned, c.Resource, id, "MAINTENANCE")
	if err != nil {
		return nil, err
	}
	s.native, err = llamaworker.Open(owned, c)
	if err != nil {
		h.fail()
		return nil, err
	} // uncertain warm-up NEVER releases its owner
	s.info = s.native.LocalInfo()
	if !nativeIdentityMatches(c, physical, s.info) {
		h.fail()
		return nil, ErrHostPolicy
	}
	// READY, before publishing this session, proves the initialization request
	// submitted no JOB/body/context. This finishes ONLY this maintenance owner;
	// it does not assert model unload or cleanup of any inference request.
	if err = h.finishNativeWarmup(owned, hold, c.Environment); err != nil {
		h.fail()
		return nil, err
	}
	s.spool, err = results.Open(results.DefaultConfig(filepath.Join(c.RuntimeRoot, "native-host-results", id)))
	if err != nil {
		return nil, err
	}
	backend, err := results.New(s.native, s.spool)
	if err != nil {
		return nil, err
	}
	s.backend = backend
	k := HostKernel{Environment: c.Environment, Capability: c.Capability, EngineKind: "LLAMA_CPP_NATIVE", EngineRelease: llamaworker.EngineRelease,
		ModelDigest: c.Model.SHA256, RecipeDigest: c.Template.SHA256, Backend: backend, native: s}
	s.watch, err = h.NewSupervisor(owned, []HostKernel{k}, options)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (h *Host) hasNativeCommand(ctx context.Context, c llamaworker.Config) error {
	found := false
	err := h.transaction(ctx, func(tx *sql.Tx) error {
		return h.visitCommands(ctx, tx, func(command hostCommand) error {
			if command.Phase == "CLAIM_PENDING" || command.Phase == "CLAIMED" || command.Phase == "CLEANED" {
				return nil
			}
			var lease executionlease.Lease
			if json.Unmarshal(command.Lease, &lease) != nil {
				return ErrHost
			}
			issued, err := time.Parse(time.RFC3339Nano, lease.Issued)
			if err != nil {
				return ErrHost
			}
			// Historical authority can recover only original metadata. Recheck
			// the signature and independent approval before any model warm-up;
			// current expiry is independently enforced for actual SubmitInput.
			accepted, err := h.accept(command, devicecontrol.Reply{Status: 200, Body: command.Lease}, devicecontrol.Reply{Status: 200, Body: command.Manifest}, issued)
			if err != nil {
				return ErrHost
			}
			m := accepted.Manifest()
			if m.EnvironmentDigest == c.Environment && m.Capability == c.Capability && m.Resource == c.Resource {
				found = true
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrHostPending
	}
	return nil
}

func (h *Host) finishNativeWarmup(ctx context.Context, hold execution.Hold, environment string) error {
	if err := h.ledger.RequestStop(ctx, hold); err != nil {
		return err
	}
	if err := h.ledger.ObserveStopped(ctx, hold, execution.Observation{RequestID: hold.Owner(), State: "ABSENT"}); err != nil {
		return err
	}
	return h.ledger.FinishCleanup(ctx, hold, execution.Cleanup{WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, ManifestHash: environment})
}

func (s *NativeSession) matches(a devicecontrol.Approval) bool {
	return s.resource == a.Resource && s.capability == a.Capability && s.physical != "" &&
		s.info.PhysicalDigest == s.physical && s.info.Environment == a.EnvironmentDigest &&
		s.info.ModelSHA256 == a.ModelDigest && s.info.TemplateSHA256 == a.RecipeDigest
}
func (s *NativeSession) Ready() <-chan struct{} { return s.watch.Ready() }

func (s *NativeSession) Info() llamaworker.LocalRuntimeInfo { return s.info }

func (s *NativeSession) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.watch == nil || s.closing || s.closed || s.runStarted {
		s.mu.Unlock()
		return ErrHostPending
	}
	s.runStarted = true
	run, cancel := context.WithCancel(ctx)
	s.runCancel = cancel
	s.mu.Unlock()
	defer cancel()
	err := s.watch.Run(run)
	s.mu.Lock()
	s.runErr = err
	close(s.runDone)
	s.mu.Unlock()
	return err
}
func (s *NativeSession) Start(ctx context.Context, p *PreparedExecution) (execution.Hold, error) {
	s.mu.Lock()
	ready := s.runStarted && !s.closing && !s.closed
	s.mu.Unlock()
	if !ready || p == nil || p.host != s.host {
		return execution.Hold{}, ErrHostPolicy
	}
	select {
	case <-s.runDone:
		return execution.Hold{}, ErrHostPending
	default:
	}
	select {
	case <-s.watch.Ready():
	case <-s.runDone:
		return execution.Hold{}, ErrHostPending
	case <-ctx.Done():
		return execution.Hold{}, ErrHostPending
	}
	return p.Start(ctx, s.watch)
}

// Close drains original work before terminating the resident owned model. It
// revokes this result scope, does not ACK/bill, and never releases UNKNOWN jobs.
func (s *NativeSession) Close(ctx context.Context) error {
	select {
	case <-s.openDone:
	case <-ctx.Done():
		return ErrHostPending
	}
	s.mu.Lock()
	if s.closing {
		done := s.closeDone
		s.mu.Unlock()
		select {
		case <-done:
			return s.closeErr
		case <-ctx.Done():
			return ErrHostPending
		}
	}
	s.closing = true
	start := !s.runStarted
	s.runStarted = true
	cancel := s.runCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.watch.Drain()
	if start {
		go func() {
			stop, cancel := context.WithCancel(context.Background())
			cancel()
			err := s.watch.Run(stop)
			s.mu.Lock()
			s.runErr = err
			close(s.runDone)
			s.mu.Unlock()
		}()
	}
	var err error
	select {
	case <-s.runDone:
		s.mu.Lock()
		err = s.runErr
		s.mu.Unlock()
	case <-ctx.Done():
		err = ErrHostPending
	}
	// Model termination is not permission to manufacture cleanup proof.
	if s.native.Close(ctx) != nil {
		err = ErrHost
	}
	resultErr := s.spool.Close(ctx) // revoke readers before closing their handles
	if s.closeOutputs(ctx) != nil {
		err = ErrHost
	} else if resultErr == results.ErrPending {
		// An open original reader retains keys/quota until its owned Close.
		// Retry only that same scope after every owned handle was closed.
		resultErr = s.spool.Close(ctx)
	}
	if resultErr != nil {
		err = ErrHost
	}
	if err != nil {
		s.host.fail()
	}
	s.host.mu.Lock()
	if s.host.nativeSession == s && err == nil {
		s.host.nativeSession = nil
	}
	s.host.mu.Unlock()
	s.mu.Lock()
	s.closed = true
	s.closeErr = err
	close(s.closeDone)
	s.mu.Unlock()
	return err
}
