package execution

import (
	"context"
	"database/sql"
)

// CheckMaintenance never acquires or reconstructs an owner. A recovered,
// stopping, local, JOB or RENTAL hold cannot authorize an environment switch.
func (s *Store) CheckMaintenance(ctx context.Context, h Hold) error {
	return s.WithMaintenance(ctx, h, func() error { return nil })
}

// WithMaintenance serializes a metadata-only operation against this original
// physical resource's gate. The callback must not call this store recursively,
// start engines, run qualification or transfer funds. The callback may commit
// a local environment selection, which remains an unqualified hint. If either
// commit is uncertain, preserve the maintenance owner; never report it FREE.
func (s *Store) WithMaintenance(ctx context.Context, h Hold, action func() error) error {
	if s == nil || ctx == nil || action == nil || h.mode != "MAINTENANCE" {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		r, err := readResource(ctx, tx, h.resource)
		if err != nil {
			return err
		}
		if !match(r, h) || r.phase != "PREPARING" || r.stopped {
			return ErrFence
		}
		if ctx.Err() != nil {
			return ErrStorage
		}
		return action()
	})
}
