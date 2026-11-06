package paymenthost

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (h *Host) retireOne(ctx context.Context) error {
	var id string
	e := h.db.Pool.QueryRow(ctx, `SELECT x.id FROM p11.payment_machine_recoveries x
 JOIN p11.payment_machine_contexts c ON c.payment_id=x.payment_id
 JOIN p11.durable_jobs j ON j.id=x.durable_job_id
 WHERE c.capability_ref=ANY($1::text[]) AND x.state<>'RETIRED' AND x.expires_at<=clock_timestamp()
 AND NOT (j.state='CLAIMED' AND j.claimed_until>clock_timestamp())
 ORDER BY x.expires_at,x.id LIMIT 1`, h.capabilities).Scan(&id)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	return h.service.RetireExpiredProof(ctx, id)
}
