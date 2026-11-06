package payments

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

type refundRecord struct {
	Payment                                    machineRecord
	Intent                                     RefundIntent
	Route                                      RefundRoute
	RouteRef                                   string
	DecisionRaw                                []byte
	PaidReceipt                                string
	SubmitRequest, QueryRequest, Job, JobState string
	Snapshot                                   RefundSnapshot
	Version                                    int64
	Updated                                    time.Time
}

func (r refundRecord) public() OriginalRefundView {
	return OriginalRefundView{ID: r.Intent.Scope.Refund, Order: r.Intent.Scope.Original.Order, Payment: r.Intent.Scope.Original.Payment, Version: r.Version, State: r.Snapshot.State, Amount: r.Intent.Amount, Updated: r.Updated, ReviewRequired: r.JobState == "DEAD" && r.Snapshot.State != "SUCCEEDED"}
}
func loadRefundTx(ctx context.Context, tx pgx.Tx, id string) (machineOrder, refundRecord, error) {
	var payment string
	e := tx.QueryRow(ctx, `SELECT payment_id FROM p11.refund_machine_contexts WHERE refund_id=$1`, id).Scan(&payment)
	if e == pgx.ErrNoRows {
		return machineOrder{}, refundRecord{}, fault.New(fault.NotFound, "Original refund not found")
	}
	if e != nil {
		return machineOrder{}, refundRecord{}, e
	}
	o, paid, e := loadMachineTx(ctx, tx, payment)
	if e != nil {
		return o, refundRecord{}, e
	}
	r := refundRecord{Payment: paid}
	var intent, route, scope []byte
	var intentHash, routeHash, scopeHash, decisionHash, decisionRef string
	var dispatched, next, observed *time.Time
	var generation int64
	e = tx.QueryRow(ctx, `SELECT c.intent_raw,c.intent_digest,c.route_raw,c.route_digest,c.scope_raw,c.scope_hash,c.route_ref,
c.decision_raw,c.decision_digest,c.decision_evidence_id,c.paid_receipt_id,c.submit_request_id,c.query_request_id,c.durable_job_id,
f.amount_fen,f.state,f.version,c.dispatched_at,c.next_query_at,c.query_generation,COALESCE(c.last_evidence_hash,''),c.observed_at,c.updated_at,j.state
FROM p11.refund_machine_contexts c JOIN p11.refunds f ON f.id=c.refund_id JOIN p11.durable_jobs j ON j.id=c.durable_job_id
WHERE c.refund_id=$1 FOR UPDATE OF c,f`, id).Scan(&intent, &intentHash, &route, &routeHash, &scope, &scopeHash, &r.RouteRef,
		&r.DecisionRaw, &decisionHash, &decisionRef, &r.PaidReceipt, &r.SubmitRequest, &r.QueryRequest, &r.Job,
		&r.Snapshot.Amount, &r.Snapshot.State, &r.Version, &dispatched, &next, &generation, &r.Snapshot.EvidenceHash, &observed, &r.Updated, &r.JobState)
	if e != nil {
		return o, r, e
	}
	if outbox.Digest(intent) != intentHash || outbox.Digest(route) != routeHash || outbox.Digest(scope) != scopeHash || outbox.Digest(r.DecisionRaw) != decisionHash ||
		strictjson.Decode(intent, &r.Intent, 8192) != nil || strictjson.Decode(route, &r.Route, 8192) != nil || r.Intent.Digest() != intentHash || r.Route.Digest() != routeHash ||
		r.Intent.Scope.Digest() != scopeHash || !r.Intent.bound(r.Route, paid.Binding) || r.Intent.Scope.Original != paid.Intent.Scope || r.Intent.Scope.Refund != id ||
		r.Intent.Scope.DecisionRef != decisionRef || r.Intent.Collected != paid.Intent.Amount || r.Intent.OriginalTrade != paid.Snapshot.ChannelReference ||
		paid.Snapshot.State != "SETTLED" || generation < 0 {
		return o, r, ErrConflict
	}
	r.Snapshot.Scope = r.Intent.Scope
	r.Snapshot.IntentHash = intentHash
	r.Snapshot.Generation = uint64(generation)
	if dispatched != nil {
		r.Snapshot.DispatchedAt = dispatched.UTC().Round(0)
	}
	if next != nil {
		r.Snapshot.NextQueryAt = next.UTC().Round(0)
	}
	if observed != nil {
		r.Snapshot.ObservedAt = observed.UTC().Round(0)
	}
	if !r.Snapshot.bound(r.Intent) {
		return o, r, ErrConflict
	}
	return o, r, nil
}

func originalRefundsTx(ctx context.Context, tx pgx.Tx, paid Snapshot) ([]Refund, error) {
	rows, e := tx.Query(ctx, `SELECT id,payment_id,order_id,original_request_key,amount_fen,state FROM p11.refunds WHERE payment_id=$1 ORDER BY id LIMIT 4097 FOR UPDATE`, paid.Scope.Payment)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []Refund
	for rows.Next() {
		var r Refund
		if e = rows.Scan(&r.ID, &r.Payment, &r.Order, &r.OriginalRequestKey, &r.Amount, &r.State); e != nil {
			return nil, e
		}
		// This service has no signed definitive-failure profile. Historical FAILED
		// rows therefore require review, never inference of reusable capacity.
		if r.State == "FAILED" {
			return nil, ErrConflict
		}
		result = append(result, r)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if _, e = HeldRefunds(paid, result); e != nil {
		return nil, e
	}
	return result, nil
}
func (s *RefundService) reverifyOriginalPaidTx(ctx context.Context, tx pgx.Tx, paid machineRecord, original Registration, originalReceipt string) (Snapshot, string, error) {
	var id, hash string
	var encoded []byte
	var observed time.Time
	e := tx.QueryRow(ctx, `SELECT r.id,r.encrypted_raw,r.encrypted_digest,r.observed_at
FROM p11.payment_machine_receipts r JOIN p11.payment_machine_contexts c ON c.payment_id=r.payment_id
WHERE r.payment_id=$1 AND r.kind='VERIFIED_PAID' AND r.scope_hash=$2 AND r.evidence_hash=$3 AND r.channel_reference=$4
AND (($5='' AND r.id=c.last_receipt_id) OR r.id=$5) FOR SHARE OF r`,
		paid.Intent.Scope.Payment, paid.Intent.Scope.Digest(), paid.Snapshot.EvidenceHash, paid.Snapshot.ChannelReference, originalReceipt).Scan(&id, &encoded, &hash, &observed)
	if e == pgx.ErrNoRows {
		return Snapshot{}, "", ErrProof
	}
	if e != nil {
		return Snapshot{}, "", e
	}
	defer clear(encoded)
	if outbox.Digest(encoded) != hash {
		return Snapshot{}, "", ErrProof
	}
	raw, e := s.machine.receipts.OpenReceipt(paid.Intent.Scope, encoded)
	if e != nil {
		return Snapshot{}, "", e
	}
	defer clear(raw)
	snapshot := paid.Snapshot
	snapshot.ObservedAt = observed.UTC().Round(0)
	verified, e := original.Channel.ReverifyPaidReceipt(ctx, paid.Intent, snapshot, raw)
	defer verified.Close()
	if e != nil || !verified.IsSettled() {
		return Snapshot{}, "", ErrProof
	}
	return snapshot, id, nil
}

func insertRefundTx(ctx context.Context, tx pgx.Tx, o machineOrder, paid machineRecord, registration RefundRegistration, i RefundIntent, decision []byte, receipt string) (OriginalRefundView, error) {
	intent, _ := json.Marshal(i)
	route, _ := json.Marshal(registration.Channel.route)
	scope, _ := json.Marshal(i.Scope)
	submit, query, job := ids.New("ch"), ids.New("ch"), ids.New("bg")
	_, e := tx.Exec(ctx, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state,created_at) VALUES($1,$2,$3,$4,$5,'REQUESTED',$6)`,
		i.Scope.Refund, paid.Intent.Scope.Payment, o.ID, i.Scope.OriginalRequestKey, i.Amount, o.Now)
	if e != nil {
		return OriginalRefundView{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.channel_requests(id,operation,target_ref,provider,environment,original_request_key,request_digest,state)
VALUES($1,'REFUND',$2,$3,$4,$5,$6,'INTENT'),($7,'QUERY',$2,$3,$4,$5,$6,'INTENT')`, submit, i.Scope.Refund, paid.Binding.Provider, paid.Binding.Environment, i.Scope.OriginalRequestKey, i.Digest(), query)
	if e != nil {
		return OriginalRefundView{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.durable_jobs(id,kind,dedupe_key,target_ref,state,run_at) VALUES($1,'RESOLVE_MACHINE_REFUND',$2,$3,'PENDING',$4)`, job, "machine-refund:"+i.Scope.Refund, i.Scope.Refund, o.Now)
	if e != nil {
		return OriginalRefundView{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.refund_machine_contexts(refund_id,payment_id,order_id,route_ref,scope_raw,scope_hash,intent_raw,intent_digest,route_raw,route_digest,
decision_evidence_id,decision_raw,decision_digest,paid_receipt_id,submit_request_id,query_request_id,durable_job_id,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, i.Scope.Refund, paid.Intent.Scope.Payment, o.ID, registration.Ref, scope, i.Scope.Digest(), intent, i.Digest(), route, registration.Channel.route.Digest(), i.Scope.DecisionRef, decision, outbox.Digest(decision), receipt, submit, query, job, o.Now)
	if e != nil {
		return OriginalRefundView{}, e
	}
	if _, e = refundEventTx(ctx, tx, i.Scope.Refund, 1, "OriginalRefundReserved", ""); e != nil {
		return OriginalRefundView{}, e
	}
	return OriginalRefundView{ID: i.Scope.Refund, Order: o.ID, Payment: paid.Intent.Scope.Payment, Version: 1, State: "REQUESTED", Amount: i.Amount, Updated: o.Now}, nil
}
func refundEventTx(ctx context.Context, tx pgx.Tx, id string, version int64, kind, receipt string) (string, error) {
	event := ids.New("evt")
	safe, _ := json.Marshal(map[string]string{"refund_id": id, "receipt_ref": receipt})
	_, e := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'REFUND',$2,$3,$4,'3.0',$5,'PENDING')`, event, id, version, kind, safe)
	return event, e
}
func nullableRefundTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Truncate(time.Microsecond)
}
func writeRefundStateTx(ctx context.Context, tx pgx.Tx, r refundRecord, next RefundSnapshot, receipt string) error {
	_, e := tx.Exec(ctx, `UPDATE p11.refund_machine_contexts SET dispatched_at=$2,next_query_at=$3,query_generation=$4,
last_receipt_id=CASE WHEN $5='' THEN last_receipt_id ELSE $5 END,last_evidence_hash=NULLIF($6,''),observed_at=$7,updated_at=clock_timestamp() WHERE refund_id=$1`,
		r.Intent.Scope.Refund, nullableRefundTime(next.DispatchedAt), nullableRefundTime(next.NextQueryAt), int64(next.Generation), receipt, next.EvidenceHash, nullableRefundTime(next.ObservedAt))
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE p11.refunds SET state=$2,version=version+1 WHERE id=$1`, r.Intent.Scope.Refund, next.State)
	return e
}
