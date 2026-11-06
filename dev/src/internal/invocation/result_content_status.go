package invocation

import (
	"context"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

func (s *ResultContent) UploadStatus(ctx context.Context, p identity.Principal, attempt, delivery string) (resultwire.ContentAck, error) {
	var out resultwire.ContentAck
	if !ids.Valid(attempt) || !ids.Valid(delivery) {
		return out, fault.New(fault.Invalid, "Original upload identity required")
	}
	e := s.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var lease, fence string
		e := tx.QueryRow(ctx, `SELECT n.lease_id,a.fencing_token::text FROM p11.native_result_deliveries n JOIN p11.attempts a ON a.id=n.attempt_id WHERE n.delivery_id=$1 AND n.attempt_id=$2 AND a.device_id=$3 AND a.seller_id=$4`, delivery, attempt, p.DeviceID, p.UserID).Scan(&lease, &fence)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original upload unavailable")
		}
		if e != nil {
			return e
		}
		a, e := s.completion.claims.reportAuthorityTx(ctx, tx, p, attempt, lease, fence)
		if e != nil {
			return e
		}
		c, e := s.contentTx(ctx, tx, delivery, false)
		if e != nil {
			return e
		}
		if c.head.Declaration.Scope != resultwire.Original(a.lease) {
			return fault.New(fault.Forbidden, "Original upload scope differs")
		}
		out = resultwire.ContentAck{Delivery: delivery, Head: c.digest, State: c.state}
		return nil
	})
	return out, e
}
