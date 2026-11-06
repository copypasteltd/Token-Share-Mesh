package payments

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"time"
)

func loadRefundApplicationTx(ctx context.Context, tx pgx.Tx, id string) (machineOrder, machineRecord, refundApplicationRecord, error) {
	var payment string
	e := tx.QueryRow(ctx, `SELECT payment_id FROM p11.refund_applications WHERE id=$1`, id).Scan(&payment)
	if e == pgx.ErrNoRows {
		e = fault.New(fault.NotFound, "Original refund application not found")
	}
	if e != nil {
		return machineOrder{}, machineRecord{}, refundApplicationRecord{}, e
	}
	o, paid, e := loadMachineTx(ctx, tx, payment)
	if e != nil {
		return o, paid, refundApplicationRecord{}, e
	}
	a := refundApplicationRecord{}
	var raw, refs []byte
	var hash string
	e = tx.QueryRow(ctx, `SELECT id,order_id,payment_id,buyer_id,seller_id,contract_hash,request_raw,request_digest,requested_fen,reason_code,evidence_refs,review_state,review_version,coalesce(review_reason_code,''),coalesce(decision_evidence_id,''),coalesce(decision_digest,''),created_at,updated_at
FROM p11.refund_applications WHERE id=$1 FOR UPDATE`, id).Scan(&a.ID, &a.Order, &a.Payment, &a.Buyer, &a.Seller, &a.ContractHash, &raw, &hash, &a.Requested, &a.Reason, &refs, &a.ReviewState, &a.ReviewVersion, &a.ReviewReason, &a.DecisionRef, &a.DecisionHash, &a.Created, &a.Updated)
	if e != nil {
		return o, paid, a, e
	}
	var input RefundApplicationInput
	if strictjson.Decode(raw, &input, 16384) != nil || strictjson.Decode(refs, &a.Evidence, 8192) != nil || hash != outbox.Digest(raw) || a.Order != o.ID || a.Buyer != o.Buyer || a.Seller != o.Seller || a.ContractHash != o.Hash || a.Payment != paid.Intent.Scope.Payment || a.Requested != input.Requested || a.Reason != input.Reason {
		return o, paid, a, ErrConflict
	}
	normalized, e := normalizeRefundApplication(input)
	if e != nil || len(normalized.Evidence) != len(a.Evidence) {
		return o, paid, a, ErrConflict
	}
	for i, ref := range normalized.Evidence {
		if a.Evidence[i] != ref {
			return o, paid, a, ErrConflict
		}
	}
	a.RequestHash = hash
	a.State = "REQUESTED"
	a.Version = a.ReviewVersion
	if a.DecisionRef != "" {
		_, r, e := loadRefundTx(ctx, tx, id)
		if e != nil {
			return o, paid, a, e
		}
		if r.Intent.Scope.DecisionRef != a.DecisionRef || r.Intent.Scope.DecisionHash != a.DecisionHash || r.Intent.Amount != a.Requested {
			return o, paid, a, ErrConflict
		}
		a.State = r.Snapshot.State
		a.ReviewRequired = r.public().ReviewRequired
		// A single monotonic public version spans review and financial changes.
		a.Version = a.ReviewVersion + r.Version - 1
		if a.State == "SUCCEEDED" {
			a.Confirmed = a.Requested
		}
		if r.Updated.After(a.Updated) {
			a.Updated = r.Updated
		}
	}
	a.Created = a.Created.UTC().Truncate(time.Microsecond)
	a.Updated = a.Updated.UTC().Truncate(time.Microsecond)
	return o, paid, a, nil
}
func refundApplicationEventTx(ctx context.Context, tx pgx.Tx, id string, version int64, kind, actor string) error {
	event := ids.New("evt")
	safe, _ := json.Marshal(map[string]string{"refund_id": id, "actor_id": actor})
	_, e := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'REFUND_APPLICATION',$2,$3,$4,'3.0',$5,'PENDING')`, event, id, version, kind, safe)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,$3,$4,$5,$6)`, ids.New("audit"), actor, kind, id, ids.New("req"), safe)
	return e
}
