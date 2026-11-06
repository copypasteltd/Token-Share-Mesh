package payments

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

func reviewedRefundEvidenceTx(ctx context.Context, tx pgx.Tx, buyer, seller, id, digest, kind string, now time.Time, needed map[string]string) ([]byte, error) {
	scope, _ := json.Marshal(needed)
	var actual []byte
	e := tx.QueryRow(ctx, `SELECT e.scope_snapshot FROM p11.evidence e JOIN p11.users u ON u.id=e.reviewed_by
JOIN p11.role_grants g ON g.user_id=u.id AND g.role_code='PAYMENT_REVIEWER' AND g.scope_ref='self'
WHERE e.id=$1 AND e.digest=$2 AND e.evidence_type=$3 AND e.seller_id=$4 AND e.device_id IS NULL
AND e.decision='APPROVED' AND e.valid_from<=$5 AND e.valid_until>$5 AND e.scope_snapshot @> $6::jsonb
AND u.state='ACTIVE' AND u.id<>$4 AND u.id<>$7 AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at>$5)
ORDER BY g.id LIMIT 1 FOR SHARE OF e,u,g`, id, digest, kind, seller, now, scope, buyer).Scan(&actual)
	if e == pgx.ErrNoRows {
		return nil, ErrBinding
	}
	if e != nil {
		return nil, e
	}
	if len(actual) > 8192 {
		return nil, ErrBinding
	}
	return actual, nil
}
func refundRouteScope(original Registration, r RefundRegistration) map[string]string {
	scope := machineAuthorityScope(original)
	scope["refund_profile"] = RefundProfile
	scope["refund_route_revision"] = r.Channel.route.Revision
	scope["refund_route_hash"] = r.Channel.route.Digest()
	scope["refund_methods"] = refundMethod + "," + refundQueryMethod
	return scope
}
func requireRefundRouteTx(ctx context.Context, tx pgx.Tx, o machineOrder, original Registration, r RefundRegistration, submit bool) error {
	if !r.Channel.route.ValidAt(original.Channel.binding, o.Now, submit) || r.OriginalCapability != original.CapabilityRef {
		return ErrBinding
	}
	if e := requireMachineAuthorityTx(ctx, tx, original, o.Buyer, o.Now, false); e != nil {
		return e
	}
	_, e := reviewedRefundEvidenceTx(ctx, tx, o.Buyer, o.Seller, r.Channel.route.EvidenceRef, r.Channel.route.EvidenceHash, "MACHINE_PAY_REFUND_ROUTE_1", o.Now, refundRouteScope(original, r))
	return e
}

// This policy handles independently reviewed unused ORIGINAL consideration.
// Earned-service reversals/fees/commission need their own actual qualified
// financial policies, never a fabricated release of accepted usage.
func refundDecisionTx(ctx context.Context, tx pgx.Tx, o machineOrder, paid machineRecord, original Registration, r RefundRegistration, ref, digest string) (RefundAuthority, money.Fen, []byte, error) {
	needed := refundRouteScope(original, r)
	needed["refund_policy"] = "UNUSED_BALANCE_1"
	needed["payment_id"] = paid.Intent.Scope.Payment
	needed["order_id"] = o.ID
	needed["buyer_id"] = o.Buyer
	needed["contract_hash"] = o.Hash
	needed["paid_evidence_hash"] = paid.Snapshot.EvidenceHash
	needed["order_version"] = strconv.FormatInt(o.Version, 10)
	needed["payment_version"] = strconv.FormatInt(paid.Version, 10)
	raw, e := reviewedRefundEvidenceTx(ctx, tx, o.Buyer, o.Seller, ref, digest, "MACHINE_PAY_REFUND_DECISION_1", o.Now, needed)
	if e != nil {
		return RefundAuthority{}, 0, nil, e
	}
	var scope map[string]json.RawMessage
	if strictjson.Decode(raw, &scope, 8192) != nil {
		return RefundAuthority{}, 0, nil, ErrBinding
	}
	var maxText, amountText string
	if json.Unmarshal(scope["maximum_refundable_fen"], &maxText) != nil || json.Unmarshal(scope["refund_amount_fen"], &amountText) != nil {
		return RefundAuthority{}, 0, nil, ErrBinding
	}
	maximum, e := strconv.ParseInt(maxText, 10, 64)
	if e != nil || maximum < 1 || maximum > int64(paid.Snapshot.Amount) || strconv.FormatInt(maximum, 10) != maxText {
		return RefundAuthority{}, 0, nil, ErrBinding
	}
	amount, e := strconv.ParseInt(amountText, 10, 64)
	if e != nil || amount < 1 || amount > maximum || strconv.FormatInt(amount, 10) != amountText {
		return RefundAuthority{}, 0, nil, ErrBinding
	}
	needed["maximum_refundable_fen"] = maxText
	needed["refund_amount_fen"] = amountText
	frozen, _ := json.Marshal(needed)
	return RefundAuthority{Payment: paid.Intent.Scope.Payment, Order: o.ID, Reference: ref, EvidenceHash: digest, Maximum: money.Fen(maximum)}, money.Fen(amount), frozen, nil
}
func requireFrozenRefundDecisionTx(ctx context.Context, tx pgx.Tx, o machineOrder, r refundRecord) error {
	var scope map[string]string
	if strictjson.Decode(r.DecisionRaw, &scope, 8192) != nil || scope["refund_policy"] != "UNUSED_BALANCE_1" ||
		scope["payment_id"] != r.Intent.Scope.Original.Payment || scope["order_id"] != o.ID || scope["buyer_id"] != o.Buyer || scope["contract_hash"] != o.Hash ||
		scope["paid_evidence_hash"] != r.Intent.PaidEvidenceHash || scope["refund_route_hash"] != r.Intent.Scope.RouteHash ||
		scope["maximum_refundable_fen"] != strconv.FormatInt(int64(r.Intent.Authority.Maximum), 10) || scope["refund_amount_fen"] != strconv.FormatInt(int64(r.Intent.Amount), 10) {
		return ErrBinding
	}
	_, e := reviewedRefundEvidenceTx(ctx, tx, o.Buyer, o.Seller, r.Intent.Scope.DecisionRef, r.Intent.Scope.DecisionHash, "MACHINE_PAY_REFUND_DECISION_1", o.Now, scope)
	return e
}

// Parent and payment locks are already held. An unresolved amount, legacy
// reservation, live task, uncleaned attempt or rental owner cannot turn into
// refundable consideration. Actual final qualifications must match consumption.
func refundCapacityTx(ctx context.Context, tx pgx.Tx, o machineOrder, paid machineRecord, maximum money.Fen) error {
	if paid.Intent.Scope.Purpose != "ORDER" || paid.Snapshot.State != "SETTLED" || paid.Snapshot.Amount != o.Maximum ||
		o.Contract.BuyerFeeFen != 0 || o.Contract.Price.CommissionBPS == nil || *o.Contract.Price.CommissionBPS != 0 || maximum < 1 || maximum > paid.Snapshot.Amount {
		return ErrUnsupported
	}
	var reserved, consumed, unverified, accounted, qualified int64
	var busy bool
	e := tx.QueryRow(ctx, `SELECT o.budget_reserved_fen,o.budget_consumed_fen,o.budget_unverified_fen,
COALESCE((SELECT sum(b.consumed_fen::numeric) FROM p11.budget_reservations b WHERE b.order_id=o.id AND b.accounting_profile='ORDER_CAPACITY_1' AND b.state IN ('CONSUMED','RELEASED')),0)::bigint,
COALESCE((SELECT sum(q.gross_fen::numeric) FROM p11.trade_qualifications q WHERE q.order_id=o.id),0)::bigint,
EXISTS(SELECT 1 FROM p11.jobs j WHERE j.order_id=o.id AND j.state NOT IN ('ACCEPTED','FAILED','CANCELED')) OR
EXISTS(SELECT 1 FROM p11.attempts a JOIN p11.jobs j ON j.id=a.job_id WHERE j.order_id=o.id AND a.resource_lock_generation IS NOT NULL AND a.state<>'CLEANED') OR
EXISTS(SELECT 1 FROM p11.execution_leases l JOIN p11.jobs j ON j.id=l.job_id WHERE j.order_id=o.id AND l.state NOT IN ('CLOSED','REVOKED','EXPIRED')) OR
EXISTS(SELECT 1 FROM p11.rental_sessions s WHERE s.order_id=o.id AND s.state<>'CLOSED') OR
EXISTS(SELECT 1 FROM p11.resource_locks l LEFT JOIN p11.attempts a ON a.id=l.owner_attempt_id LEFT JOIN p11.jobs j ON j.id=a.job_id LEFT JOIN p11.rental_sessions s ON s.id=l.owner_session_id WHERE (j.order_id=o.id OR s.order_id=o.id) AND l.phase<>'FREE') OR
EXISTS(SELECT 1 FROM p11.budget_reservations b WHERE b.order_id=o.id AND (b.accounting_profile<>'ORDER_CAPACITY_1' OR b.state='RESERVED' OR b.resolved_at IS NULL OR b.qualification_id IS NULL))
FROM p11.orders o WHERE o.id=$1`, o.ID).Scan(&reserved, &consumed, &unverified, &accounted, &qualified, &busy)
	if e != nil {
		return e
	}
	if busy || reserved != 0 || unverified != 0 || consumed < 0 || consumed != accounted || consumed != qualified || consumed > int64(paid.Snapshot.Amount) || int64(maximum) > int64(paid.Snapshot.Amount)-consumed {
		return ErrConflict
	}
	return nil
}
