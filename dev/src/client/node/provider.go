package node

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/ids"
)

const NativeProviderProfile = "P11_NATIVE_PROVIDER_1"

// NativeProviderConfig is explicit LOCAL stdin configuration. It neither
// downloads packages nor edits approvals, listing qualification or payment state.
type NativeProviderConfig struct {
	Profile              string             `json:"profile_id"`
	Installation         NativeInstallation `json:"installation"`
	ClaimMilliseconds    int                `json:"claim_interval_milliseconds,omitempty"`
	ProgressMilliseconds int                `json:"progress_interval_milliseconds,omitempty"`
	RequestMilliseconds  int                `json:"request_timeout_milliseconds,omitempty"`
}

func (c NativeProviderConfig) Normalize() (NativeProviderConfig, error) {
	if c.ClaimMilliseconds == 0 {
		c.ClaimMilliseconds = 2000
	}
	if c.ProgressMilliseconds == 0 {
		c.ProgressMilliseconds = 5000
	}
	if c.RequestMilliseconds == 0 {
		c.RequestMilliseconds = 5000
	}
	i := c.Installation
	environment, selectionError := llamaworker.PackageEnvironment(i.Package.SHA256, i.Selection)
	relative, relativeError := filepath.Rel(i.RuntimeRoot, i.Package.Path)
	if c.Profile != NativeProviderProfile || !filepath.IsAbs(i.RuntimeRoot) || filepath.Clean(i.RuntimeRoot) != i.RuntimeRoot || !filepath.IsAbs(i.Package.Path) || relativeError != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || !executionlease.Hash(i.Package.SHA256) || !executionlease.Hash(i.Environment) || selectionError != nil || environment != i.Environment || !ids.Valid(i.Selection.Capability) || !ids.Valid(i.Selection.Resource) || i.Selection.GPULayers == 0 || c.ClaimMilliseconds < 500 || c.ClaimMilliseconds > 30000 || c.ProgressMilliseconds < 1000 || c.ProgressMilliseconds > 10000 || c.RequestMilliseconds < 500 || c.RequestMilliseconds > 5000 {
		return c, ErrHostPolicy
	}
	return c, nil
}

type ProviderStatus struct {
	Profile          string `json:"profile_id"`
	Phase            string `json:"phase"`
	Command          string `json:"command_id,omitempty"`
	Device           string `json:"device_id"`
	Resource         string `json:"resource_id"`
	Capability       string `json:"capability_id"`
	Model            string `json:"model_name"`
	Version          int64  `json:"device_version"`
	AdmissionEnabled bool   `json:"admission_enabled"`
}

type providerControl struct {
	status  Status
	healthy bool
}

func (s *NativeSession) heartbeatState(ctx context.Context) (string, error) {
	s.mu.Lock()
	ready := s.runStarted && !s.closing && !s.closed
	s.mu.Unlock()
	if !ready || s.host.poisoned.Load() {
		return "UNKNOWN", nil
	}
	select {
	case <-s.runDone:
		return "UNKNOWN", nil
	default:
	}
	select {
	case <-s.watch.Ready():
	default:
		return "UNKNOWN", nil
	}
	snapshot, err := s.host.ledger.Snapshot(ctx)
	if err != nil {
		return "UNKNOWN", err
	}
	found := false
	for _, r := range snapshot.Resources {
		if r.Resource == s.resource {
			found = true
		}
		// Device-wide heartbeat is conservative when another local mode/resource
		// is owned. An IDLE report never releases a local or platform owner.
		if r.Phase != "FREE" || r.Recovery {
			return "BUSY", nil
		}
	}
	if !found {
		return "UNKNOWN", ErrHost
	}
	return "IDLE", nil
}

func (h *Host) providerCommand(ctx context.Context, capability string, version int64) (hostCommand, error) {
	var selected hostCommand
	err := h.transaction(ctx, func(tx *sql.Tx) error {
		return h.visitCommands(ctx, tx, func(c hostCommand) error {
			complete := c.Phase == "CLEANED" && c.CleanupAck != nil &&
				(c.ResultContent != nil && c.ResultContent.Ready || c.Progress != nil && c.Progress.Applied && (c.Progress.Write.Phase == "FAILED" || c.Progress.Write.Phase == "CANCELED"))
			if complete {
				return nil
			}
			if c.Claim.Capability != capability || selected.ID != "" {
				return ErrHostPending
			}
			selected = c
			return nil
		})
	})
	if err != nil || selected.ID != "" {
		return selected, err
	}
	nonce, err := randomHex(24)
	if err != nil {
		return hostCommand{}, ErrHost
	}
	id := "host_claim_" + nonce
	if err = h.Begin(ctx, id, capability, version); err != nil {
		return hostCommand{}, err
	}
	return h.command(ctx, id)
}

func (h *Host) recoverProviderCommand(ctx context.Context, c hostCommand) error {
	status, err := h.ledger.CommandStatus(ctx, c.ID)
	if err != nil {
		return err
	}
	if !status.Found || status.MetadataHash != hostDigest(c.Lease) || status.Phase != "CLEANED" {
		return ErrHostPending
	}
	if _, err = h.ReportCleanup(ctx, c.ID); err != nil {
		return err
	}
	if c.ResultContent != nil {
		ack, e := h.RecoverResultPublication(ctx, c.ID)
		if e != nil {
			return e
		}
		if ack.State == "READY" {
			return nil
		}
	}
	if c.Progress != nil && c.Progress.Applied && (c.Progress.Write.Phase == "FAILED" || c.Progress.Write.Phase == "CANCELED") {
		return nil
	}
	// Process restart loses the ephemeral result scope. Query can confirm a
	// remotely sealed result; absent/incomplete output cannot justify a rerun.
	return ErrHostPending
}

type providerRun struct {
	host     *Host
	native   *NativeSession
	config   NativeProviderConfig
	controls <-chan providerControl
	current  providerControl
	report   func(ProviderStatus)
}

func (r *providerRun) emit(phase, command string) {
	if r.report != nil {
		r.report(ProviderStatus{Profile: NativeProviderProfile, Phase: phase, Command: command, Device: r.host.device, Resource: r.native.resource, Capability: r.native.capability, Model: r.current.status.Model, Version: r.current.status.Version, AdmissionEnabled: r.current.healthy && phase == "READY"})
	}
}

func (r *providerRun) latest() {
	for {
		select {
		case v := <-r.controls:
			r.current = v
		default:
			return
		}
	}
}

func (r *providerRun) wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case value := <-r.controls:
			r.current = value
		case <-timer.C:
			return nil
		}
	}
}

func (r *providerRun) request(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(r.config.RequestMilliseconds)*time.Millisecond)
}

func providerRetry(err error) bool { return retryable(err) || errors.Is(err, ErrHostPending) }

func (r *providerRun) execute(ctx context.Context, c hostCommand) error {
	if c.Phase == "SUBMITTING" || c.Phase == "SUBMITTED" || c.Phase == "CLEANED" {
		request, cancel := r.request(ctx)
		defer cancel()
		return r.host.recoverProviderCommand(request, c)
	}
	r.emit("CLAIMING", c.ID)
	request, cancel := r.request(ctx)
	p, err := r.host.Prepare(request, c.ID)
	cancel()
	if err != nil {
		return err
	}
	defer p.Close()
	// Recheck actual caller cancellation/current control before once-only Submit.
	r.latest()
	if !r.current.healthy || ctx.Err() != nil {
		return ErrHostPending
	}
	request, cancel = r.request(ctx)
	hold, err := r.native.Start(request, p)
	cancel()
	if err != nil {
		if hold.Owner() != "" {
			stop, done := r.request(context.Background())
			_ = r.host.ledger.RequestStop(stop, hold)
			_, _ = r.host.ReportProgress(stop, c.ID, "FAILED")
			done()
		}
		return err // no second Start, including ambiguous acknowledgement
	}
	r.emit("EXECUTING", c.ID)
	hard, err := time.Parse(time.RFC3339Nano, p.Manifest().HardDeadline)
	if err != nil {
		return ErrHost
	}
	// Compute authority still ends at the signed deadline. Output rights are
	// separate and get a bounded transfer window after compute cleanup.
	active, end := context.WithDeadline(ctx, hard.Add(2*time.Minute))
	defer end()
	publication := make(chan error, 1)
	var publishers sync.WaitGroup
	publishers.Add(1)
	go func() {
		defer publishers.Done()
		for {
			_, e := p.PublishResult(active)
			if e == nil || !providerRetry(e) || active.Err() != nil {
				publication <- e
				return
			}
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-active.Done():
				timer.Stop()
				publication <- active.Err()
				return
			case <-timer.C:
			}
		}
	}()
	defer func() { end(); publishers.Wait() }()
	progress := time.NewTicker(time.Duration(r.config.ProgressMilliseconds) * time.Millisecond)
	defer progress.Stop()
	cleanup := time.NewTicker(500 * time.Millisecond)
	defer cleanup.Stop()
	published, cleaned, stopped := false, false, false
	sendProgress := func() error {
		request, cancel := r.request(active)
		defer cancel()
		v, e := r.host.ReportProgress(request, c.ID, "RUNNING")
		if e == nil {
			stopped = v.Stopped
		}
		if providerRetry(e) {
			return nil
		}
		return e
	}
	if err = sendProgress(); err != nil {
		return err
	}
	for {
		if published && cleaned {
			request, cancel := r.request(active)
			err := r.native.DiscardPublished(request, c.ID)
			cancel()
			if err != nil {
				if providerRetry(err) {
					if e := r.wait(active, 500*time.Millisecond); e != nil {
						return e
					}
					continue
				}
				return err
			}
			r.emit("DELIVERED_AND_CLEANED", c.ID)
			return nil
		}
		select {
		case <-active.Done():
			return active.Err()
		case control := <-r.controls:
			r.current = control
		case e := <-publication:
			if e != nil {
				stop, done := r.request(context.Background())
				_ = r.host.ledger.RequestStop(stop, hold)
				_, _ = r.host.ReportProgress(stop, c.ID, "FAILED")
				done()
				return e
			}
			published = true
			r.emit("OUTPUT_READY", c.ID)
		case <-progress.C:
			if !cleaned && !stopped {
				if e := sendProgress(); e != nil {
					return e
				}
			}
		case <-cleanup.C:
			if cleaned {
				continue
			}
			request, cancel := r.request(active)
			_, e := r.host.ReportCleanup(request, c.ID)
			cancel()
			if e == nil {
				cleaned = true
			} else if !providerRetry(e) {
				return e
			}
		}
	}
}

// RunNativeProvider owns all mutable Store control calls in one goroutine,
// starts exactly one resident watchdog, and joins control/output/worker lifetimes
// before returning. It is foreground/outbound-only and never accepts payment,
// creates trusted usage, accepts buyer delivery, changes a seller or downgrades
// privacy. Caller must keep Store open until this method returns.
func (s *Store) RunNativeProvider(ctx context.Context, config NativeProviderConfig, report func(ProviderStatus)) (answer error) {
	c, err := config.Normalize()
	if err != nil {
		return err
	}
	if s.s.Pending != nil || s.s.DeviceID == "" {
		return ErrPending
	}
	if err = s.Refresh(ctx); err != nil {
		return err
	}
	if s.s.RemoteState != "ONLINE" {
		return ErrAuthority
	}
	host, err := s.OpenHost(ctx)
	if err != nil {
		return err
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	native, err := host.OpenNative(owned, c.Installation, supervisor.Options{})
	if err != nil {
		return err
	}
	nativeDone := make(chan error, 1)
	go func() { nativeDone <- native.Run(owned); cancel() }()
	var controlDone chan error
	defer func() {
		cancel()
		if controlDone != nil {
			if e := <-controlDone; e != nil && answer == nil {
				answer = e
			}
		}
		closeCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		if e := native.Close(closeCtx); e != nil && answer == nil {
			answer = e
		}
		done()
		// Close's bounded error is not a lifecycle join. In the early-cancel
		// race Close can own the canceled watchdog while Run returns early, so
		// join both the actual watchdog and the Run wrapper before returning.
		<-native.runDone
		if native.native != nil {
			if e := native.native.Join(context.Background()); e != nil && answer == nil {
				answer = e
			}
		}
		if e := <-nativeDone; e != nil && answer == nil {
			answer = e
		}
		// Closing the watchdog can finish actual cleanup after cancellation.
		// Drain at most the bounded original pending reports, never mint leases.
		flush, finish := context.WithTimeout(context.Background(), 5*time.Second)
		if e := host.flushProviderCleanup(flush); e != nil && answer == nil {
			answer = e
		}
		finish()
	}()
	select {
	case <-native.Ready():
	case <-owned.Done():
		return ErrHostPending
	}
	controls := make(chan providerControl, 1)
	controlDone = make(chan error, 1)
	go func() {
		err := s.runControl(owned, func(ctx context.Context) error { return s.stepResource(ctx, native.heartbeatState) }, func(v Status, e error) {
			value := providerControl{status: v, healthy: e == nil && v.Pending == "" && v.RemoteState == "ONLINE"}
			select {
			case controls <- value:
			default:
				select {
				case <-controls:
				default:
				}
				controls <- value
			}
			if v.RemoteState == "DRAINING" || v.RemoteState == "REVOKED" || e != nil && !retryable(e) {
				cancel()
			}
		})
		controlDone <- err
		cancel()
	}()
	r := &providerRun{host: host, native: native, config: c, controls: controls, report: report}
	select {
	case r.current = <-controls:
	case <-owned.Done():
		return ErrHostPending
	}
	for owned.Err() == nil {
		r.latest()
		if !r.current.healthy {
			r.emit("ADMISSION_PAUSED", "")
			if r.wait(owned, time.Duration(c.ClaimMilliseconds)*time.Millisecond) != nil {
				break
			}
			continue
		}
		r.emit("READY", "")
		request, done := r.request(owned)
		command, e := host.providerCommand(request, native.capability, r.current.status.Version)
		done()
		if e != nil {
			return e
		}
		e = r.execute(owned, command)
		if errors.Is(e, ErrHostPending) {
			original, readErr := host.command(owned, command.ID)
			if readErr != nil {
				return readErr
			}
			// Transient control loss may pause a still-unsubmitted original input.
			// Once SUBMITTING is durable, only recovery/cleanup is permitted.
			if original.Phase != "CLAIM_PENDING" && original.Phase != "CLAIMED" && original.Phase != "VERIFIED" {
				return e
			}
		} else if e != nil && !errors.Is(e, ErrHostNoOffer) && !retryable(e) {
			return e
		}
		if r.wait(owned, time.Duration(c.ClaimMilliseconds)*time.Millisecond) != nil {
			break
		}
	}
	return nil
}

func (h *Host) flushProviderCleanup(ctx context.Context) error {
	var ids []string
	err := h.transaction(ctx, func(tx *sql.Tx) error {
		return h.visitCommands(ctx, tx, func(c hostCommand) error {
			if len(c.Lease) != 0 && c.CleanupAck == nil {
				ids = append(ids, c.ID)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		status, e := h.ledger.CommandStatus(ctx, id)
		if e != nil {
			return e
		}
		if !status.Found || status.Phase != "CLEANED" {
			return ErrHostPending
		}
		if _, e = h.ReportCleanup(ctx, id); e != nil {
			return e
		}
	}
	return nil
}
