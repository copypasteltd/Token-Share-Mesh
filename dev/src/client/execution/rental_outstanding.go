package execution

import (
	"context"
	"database/sql"
)

// RentalOutstanding counts only this ORIGINAL current owner. It is metadata,
// never evidence that an unobserved GPU, process, key or filesystem is stopped.
// Original adapters combine it with actual stop/close/join before projecting
// a session cleanup proof; callers cannot borrow a later owner's zero count.
type RentalOutstanding struct{ Invocations, Workspaces int }

func (s *Store) RentalOutstanding(ctx context.Context, h Hold) (RentalOutstanding, error) {
	var out RentalOutstanding
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM rental_invocations WHERE session_id=? AND phase<>'CLEANED'`, h.owner).Scan(&out.Invocations) != nil {
			return ErrStorage
		}
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM rental_media_workspaces WHERE session_id=? AND phase<>'CLEANED'`, h.owner).Scan(&out.Workspaces) != nil {
			return ErrStorage
		}
		return nil
	})
	return out, err
}
