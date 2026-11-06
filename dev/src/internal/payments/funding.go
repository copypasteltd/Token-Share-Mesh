package payments

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/fundguard"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// admitOriginalFundingTx runs only under the original order/payment locks.
// Verification is a separate committed financial fact. Business refusal here
// never rewrites SETTLED, fabricates a refund or creates another charge.
func (s *Machine) admitOriginalFundingTx(ctx context.Context, tx pgx.Tx, o machineOrder, r machineRecord) error {
	var allocated string
	e := tx.QueryRow(ctx, `SELECT payment_id FROM p11.order_fundings WHERE order_id=$1`, o.ID).Scan(&allocated)
	if e == nil {
		if allocated != r.Intent.Scope.Payment {
			return ErrConflict
		}
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	if r.Snapshot.State != "SETTLED" || r.Intent.Scope.Purpose != "ORDER" || r.Intent.Scope.PurposeRef != o.ID {
		return ErrConflict
	}
	registration, e := s.registration(r)
	if e != nil {
		return e
	}
	if e = fundguard.PartiesTx(ctx, tx, o.Buyer, o.Seller, o.Now); e != nil {
		return e
	}
	if e = admitMachineSaleTx(ctx, tx, o, registration); e != nil {
		return e
	}
	var reserved, consumed, unknown int64
	if e = tx.QueryRow(ctx, `SELECT budget_reserved_fen,budget_consumed_fen,budget_unverified_fen FROM p11.orders WHERE id=$1`, o.ID).Scan(&reserved, &consumed, &unknown); e != nil {
		return e
	}
	if reserved != 0 || consumed != 0 || unknown != 0 {
		return ErrConflict
	}
	var receipt string
	var expiry *time.Time
	e = tx.QueryRow(ctx, `SELECT c.last_receipt_id,p.funds_expires_at FROM p11.payment_machine_contexts c
 JOIN p11.payments p ON p.id=c.payment_id JOIN p11.payment_machine_receipts x ON x.id=c.last_receipt_id
 WHERE c.payment_id=$1 AND x.payment_id=p.id AND x.scope_hash=c.scope_hash AND x.kind='VERIFIED_PAID'
 AND x.channel_reference=p.channel_reference`, r.Intent.Scope.Payment).Scan(&receipt, &expiry)
	if e == pgx.ErrNoRows {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	until := o.Deadline
	if r.Binding.ValidUntil.Before(until) {
		until = r.Binding.ValidUntil
	}
	if expiry != nil && expiry.Before(until) {
		until = *expiry
	}
	if !o.Now.Before(until) {
		return ErrConflict
	}
	b, p := r.Binding, r.Policy
	approvals := fundguard.Approval{Scope: machineAuthorityScope(registration), Evidence: []fundguard.Evidence{
		{ID: b.AuthorizationRef, Digest: b.AuthorizationHash, Kind: "MACHINE_PAY_AUTHORITY_1"},
		{ID: b.QualificationRef, Digest: b.QualificationHash, Kind: "MACHINE_PAY_PERSONAL_SELLER_1"},
		{ID: p.RefundEvidenceRef, Digest: p.RefundEvidenceHash, Kind: "MACHINE_PAY_REFUND_ROUTE_1"},
		{ID: p.CommissionEvidenceRef, Digest: p.CommissionEvidenceHash, Kind: "MACHINE_PAY_COMMISSION_ROUTE_1"},
	}}
	raw, _ := json.Marshal(approvals)
	grant, event := ids.New("ofg"), ids.New("evt")
	_, e = tx.Exec(ctx, `INSERT INTO p11.order_fundings(id,order_id,payment_id,buyer_id,seller_id,contract_hash,scope_hash,binding_hash,
 profile,environment,amount_fen,receipt_id,approval_raw,approval_digest,valid_until,admitted_at,event_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, grant, o.ID, r.Intent.Scope.Payment, o.Buyer, o.Seller, o.Hash, r.Intent.Scope.Digest(), b.Digest(), fundguard.Profile, b.Environment, o.Maximum, receipt, raw, outbox.Digest(raw), until, o.Now, event)
	if e != nil {
		return e
	}
	changed, e := tx.Exec(ctx, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1 AND state='WAITING_PAYMENT'`, o.ID)
	if e != nil {
		return e
	}
	if changed.RowsAffected() != 1 {
		return errors.New("original funding transition lost its locked order")
	}
	safe, _ := json.Marshal(map[string]string{"order_id": o.ID, "payment_id": r.Intent.Scope.Payment, "funding_ref": grant, "receipt_ref": receipt})
	_, e = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
 VALUES($1,'ORDER',$2,$3,'OriginalOrderFundsAdmitted','3.0',$4,'PENDING')`, event, o.ID, o.Version+1, safe)
	return e
}

func fundingHold(e error) bool {
	if errors.Is(e, ErrBinding) || errors.Is(e, ErrIntent) || errors.Is(e, ErrConflict) || errors.Is(e, ErrUnsupported) {
		return true
	}
	var safe *fault.Error
	return errors.As(e, &safe) && (safe.Code == fault.Conflict || safe.Code == fault.NotFound || safe.Code == fault.Forbidden || safe.Code == fault.Invalid || safe.Code == fault.Unavailable)
}

// RecoverFundingOne consumes only metadata admission jobs for this host's
// original capabilities. It performs no external I/O and starts no GPU work.
// Allocation, READY, outbox and job completion commit atomically; unknown
// commits are recovered with the same payment/job, never repeated payment.
func (s *Machine) RecoverFundingOne(ctx context.Context) (bool, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return false, e
	}
	defer end()
	capabilities := make([]string, 0, len(s.registrations))
	for id := range s.registrations {
		capabilities = append(capabilities, id)
	}
	var payment string
	e = s.db.Pool.QueryRow(ctx, `SELECT j.target_ref FROM p11.durable_jobs j
 JOIN p11.payment_machine_contexts c ON c.payment_id=j.target_ref
 WHERE j.kind='ADMIT_MACHINE_FUNDS' AND c.capability_ref=ANY($1::text[])
 AND ((j.state='PENDING' AND j.run_at<=clock_timestamp()) OR (j.state='CLAIMED' AND j.claimed_until<=clock_timestamp()))
 ORDER BY j.run_at,j.id LIMIT 1`, capabilities).Scan(&payment)
	if e == pgx.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		var id, state string
		var until *time.Time
		var runAt time.Time
		e = tx.QueryRow(ctx, `SELECT id,state,claimed_until,run_at FROM p11.durable_jobs
 WHERE dedupe_key=$1 AND target_ref=$2 AND kind='ADMIT_MACHINE_FUNDS' FOR UPDATE`, "machine-funds:"+payment, payment).Scan(&id, &state, &until, &runAt)
		if e != nil {
			return e
		}
		if state == "DONE" || state == "DEAD" || state == "PENDING" && runAt.After(o.Now) || state == "CLAIMED" && (until == nil || until.After(o.Now)) {
			return nil
		}
		e = s.admitOriginalFundingTx(ctx, tx, o, r)
		next, code := "DONE", ""
		if e != nil {
			if !fundingHold(e) {
				return e
			}
			next, code = "PENDING", "ORIGINAL_FUNDS_NOT_ADMITTED"
			// A legacy READY/FULFILLING order without its original allocation cannot
			// be repaired by manufacturing funds after execution has started.
			if !o.Now.Before(o.Deadline) || o.State != "WAITING_PAYMENT" || !o.Now.Before(r.Binding.ValidUntil) {
				next, code = "DEAD", "ORIGINAL_FUNDS_REQUIRE_REVIEW"
			}
		}
		_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state=$2,claim_generation=claim_generation+1,tries=tries+1,
 claimed_until=NULL,last_safe_error=NULLIF($3,''),run_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, id, next, code)
		return e
	})
	return true, e
}
