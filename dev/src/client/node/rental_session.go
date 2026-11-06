package node

import (
	"context"
	"sync"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

// ProviderRental joins original encrypted local approval, the shared physical
// gate, independent grant resolution and signed continuous control. OpenNative
// explicitly attaches the rental-only native adapter and owned watchdog;
// successful startup alone never approves hardware or platform readiness.
type ProviderRental struct {
	mu       sync.RWMutex
	node     *Store
	journal  *RentalJournal
	ledger   *execution.Store
	prepared rentalaccess.Prepared
	hold     execution.Hold
	control  *RentalControl
	closed   bool
}

func (s *Store) PrepareRentalSession(ctx context.Context, command string, p rentalaccess.Prepared) (*ProviderRental, string, error) {
	j, err := s.OpenRentalJournal()
	if err != nil {
		return nil, "", err
	}
	ledger, err := s.OpenExecution(ctx)
	if err != nil {
		return nil, "", err
	}
	if err = j.RecordPreparation(p, time.Now()); err != nil {
		return nil, "", err
	}
	h, disposition, err := ledger.ReceiveRentalPreparation(ctx, command, p)
	if err != nil {
		return nil, "", err
	}
	return &ProviderRental{node: s, journal: j, ledger: ledger, prepared: p, hold: h}, disposition, nil
}

func (r *ProviderRental) Hold() execution.Hold {
	if r == nil {
		return execution.Hold{}
	}
	return r.hold
}

func (r *ProviderRental) current(ctx context.Context) (rentalaccess.Expected, error) {
	if r == nil || !r.journal.matchesPreparation(r.prepared) {
		return rentalaccess.Expected{}, ErrRentalJournal
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return rentalaccess.Expected{}, ErrRentalJournal
	}
	status, err := r.ledger.RentalCurrent(ctx, r.hold)
	if err != nil || status.Phase != "ACTIVE" || status.ManifestHash != r.prepared.Digest() || status.ApprovalHash != r.prepared.ApprovalDigest() || !r.prepared.BindsGrant(status.Expected) {
		return rentalaccess.Expected{}, ErrRentalJournal
	}
	return status.Expected, nil
}

// AttachControl resolves the digest from the actual original platform record;
// the caller cannot supply it by hashing its own Bearer. The complete local
// preparation must match. No ALLOW exists until RefreshControl also commits
// the signed response to the original shared resource gate.
func (r *ProviderRental) AttachControl(ctx context.Context, verifier *rentalaccess.Verifier, credential string, approved rentalaccess.Expected) error {
	if r == nil {
		return ErrRentalJournal
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.control != nil || !r.journal.matchesPreparation(r.prepared) {
		return ErrRentalJournal
	}
	c, err := r.journal.ResolveControl(ctx, verifier, credential, approved, r.prepared.Approval().Connection)
	if err != nil {
		return err
	}
	if !r.prepared.BindsGrant(c.OriginalExpected()) {
		c.Close()
		return ErrRentalJournal
	}
	command := "rbind_" + hostDigest([]byte(c.OriginalExpected().Grant+"\n"+c.OriginalExpected().TokenDigest))
	if err = r.ledger.BindRentalGrant(ctx, command, r.hold, r.prepared, c.grant, c.OriginalExpected()); err != nil {
		c.Close()
		return err
	}
	r.control = c
	return nil
}

func (r *ProviderRental) controlSnapshot() (*RentalControl, error) {
	if r == nil {
		return nil, ErrRentalJournal
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed || r.control == nil {
		return nil, ErrRentalJournal
	}
	return r.control, nil
}

func (r *ProviderRental) RefreshControl(ctx context.Context) error {
	c, err := r.controlSnapshot()
	if err != nil {
		return err
	}
	err = c.Refresh(ctx, r.current)
	if err != nil {
		if c.window.Snapshot().Stopped {
			r.ledger.StopRental(ctx, r.hold)
		}
		return err
	}
	if err = r.ledger.ApplyRentalControl(ctx, r.hold, c.window.LastControl()); err != nil {
		c.Close()
		r.ledger.StopRental(ctx, r.hold)
		return err
	}
	return nil
}

func (r *ProviderRental) CheckScope(ctx context.Context, scope string) error {
	c, err := r.controlSnapshot()
	if err != nil {
		return err
	}
	if err = c.CheckScope(ctx, scope, r.current); err != nil {
		return err
	}
	return r.ledger.CheckRental(ctx, r.hold, scope)
}

func (r *ProviderRental) BeginInvocation(ctx context.Context, id, inputCiphertextDigest, recipeDigest string) (*execution.RentalSubmission, execution.RentalInvocation, string, error) {
	if err := r.CheckScope(ctx, "INVOKE"); err != nil {
		return nil, execution.RentalInvocation{}, "", err
	}
	return r.ledger.BeginRentalInvocation(ctx, r.hold, id, inputCiphertextDigest, recipeDigest)
}

// Stop records withdrawal and retains the actual physical owner. It does not
// claim that an engine stopped, clear outputs or release budget/physical locks.
// Real original-key query/cancel/cleanup adapters must supply those facts later.
func (r *ProviderRental) Stop(ctx context.Context) error {
	if r == nil {
		return ErrRentalJournal
	}
	r.mu.Lock()
	r.closed = true
	c := r.control
	r.mu.Unlock()
	var controlErr error
	if c != nil {
		controlErr = c.Close()
	}
	if err := r.ledger.StopRental(ctx, r.hold); err != nil {
		return err
	}
	return controlErr
}
