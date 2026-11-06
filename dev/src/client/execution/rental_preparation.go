package execution

import (
	"context"
	"database/sql"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

// WithRentalPreparation serializes a metadata-only environment reference
// against the ORIGINAL live preparation owner. It grants no INVOKE/ready,
// starts no process and releases no resource. The callback may not reenter
// this store, start an engine or perform content/network/payment operations.
// A reopened owner has no in-memory preparation clock and is rejected.
func (s *Store) WithRentalPreparation(ctx context.Context, h Hold, p rentalaccess.Prepared, action func() error) (time.Time, error) {
	if s == nil || ctx == nil || action == nil || h.mode != "RENTAL" {
		return time.Time{}, ErrFence
	}
	var deadline time.Time
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe || !p.ValidAt(stamp.Wall) {
			return ErrRecovery
		}
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		g, live := s.rentalGuards[h.owner]
		if !live || g.allowed || o.phase != "PREPARING" || r.phase != "PREPARING" || r.stopped || o.grant != "" || o.digest != "" || o.context != "" || o.hard != "0" || o.manifestHash != p.Digest() || o.approvalHash != p.ApprovalDigest() || o.physical != p.Approval().PhysicalHash || !stamp.Wall.Before(g.clock.expires) || stamp.Elapsed >= g.clock.until || !time.Now().Before(g.clock.deadline) {
			return ErrRecovery
		}
		deadline = g.clock.deadline
		if ctx.Err() != nil {
			return ErrStorage
		}
		return action()
	})
	if err == nil && (ctx.Err() != nil || !time.Now().Before(deadline)) {
		return time.Time{}, ErrRecovery
	}
	return deadline, err
}

// CheckRentalExpected binds the *current* original grant, not just a scope
// currently available under some replacement grant. It creates no work ticket
// and must be followed by the original durable invocation admission.
func (s *Store) CheckRentalExpected(ctx context.Context, h Hold, expected rentalaccess.Expected, scope string) (time.Time, error) {
	if s == nil || ctx == nil || h.mode != "RENTAL" {
		return time.Time{}, ErrFence
	}
	var deadline time.Time
	e := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		g, live := s.rentalGuards[h.owner]
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if s.clockUnsafe || !live || !g.allowed || o.phase != "ACTIVE" || r.phase != "EXECUTING" || r.stopped || g.expected != expected || o.grant != expected.Grant || o.digest != expected.TokenDigest || o.manifestHash != expected.ManifestHash || o.physical != g.physical || !g.grant.Matches(expected, stamp.Wall) || !g.grant.Allows(scope, stamp.Wall) || !stamp.Wall.Before(g.clock.expires) || stamp.Elapsed >= g.clock.until || !time.Now().Before(g.clock.deadline) {
			return ErrRecovery
		}
		deadline = g.clock.deadline
		return nil
	})
	if e != nil || ctx.Err() != nil || !time.Now().Before(deadline) {
		return time.Time{}, ErrRecovery
	}
	return deadline, nil
}
