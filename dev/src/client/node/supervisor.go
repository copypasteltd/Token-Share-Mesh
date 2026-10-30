package node

import (
	"context"
	"p11.local/compute/client/supervisor"
)

// NewSupervisor binds an explicitly initialized execution ledger to the same
// node's OS process lock. Registrations are trusted embedding code, not remote
// buyer input. No CLI engine/trust configuration or task receiver is enabled.
func (s *Store) NewSupervisor(ctx context.Context, registrations []supervisor.Registration, options supervisor.Options) (*supervisor.Supervisor, error) {
	ledger, e := s.OpenExecution(ctx)
	if e != nil {
		return nil, e
	}
	watchdog, e := supervisor.New(ledger, registrations, options)
	if e != nil {
		return nil, e
	}
	if s.s.Pending != nil || s.s.RemoteState == "REVOKED" || s.s.RemoteState == "DRAINING" {
		watchdog.Drain()
	}
	return watchdog, nil
}

// RunSupervised couples the existing outbound control agent to the watchdog.
// Drain/revocation or a terminal control failure closes execution admission and
// waits for bounded original-backend stop/cleanup before returning. A transport
// outage cannot extend signed leases. Heartbeats remain UNKNOWN; no GPU probe
// or real engine qualification is inferred from these library hooks.
func (s *Store) RunSupervised(ctx context.Context, watchdog *supervisor.Supervisor, report func(Status)) error {
	if watchdog == nil || s.execution == nil || !watchdog.BoundTo(s.execution) {
		return ErrState
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	controlDone := make(chan error, 1)
	executionDone := make(chan error, 1)
	go func() { executionDone <- watchdog.Run(owned) }()
	select {
	case <-watchdog.Ready():
	case e := <-executionDone:
		return e
	case <-ctx.Done():
		cancel()
		return <-executionDone
	}
	go func() {
		controlDone <- s.Run(owned, func(status Status) {
			if status.RemoteState == "DRAINING" || status.RemoteState == "REVOKED" {
				cancel()
			}
			if report != nil {
				report(status)
			}
		})
	}()
	var controlErr, executionErr error
	select {
	case controlErr = <-controlDone:
		cancel()
		executionErr = <-executionDone
	case executionErr = <-executionDone:
		cancel()
		controlErr = <-controlDone
	}
	if executionErr != nil {
		return executionErr
	}
	return controlErr
}
