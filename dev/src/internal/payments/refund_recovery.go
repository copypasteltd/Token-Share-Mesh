package payments

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/ids"
)

const maximumRefundClaims = 128

type refundTicket struct {
	Record             refundRecord
	Registration       RefundRegistration
	Request            string
	JobGeneration      int64
	SnapshotGeneration uint64
	Submit, Run        bool
}

func refundReviewTx(ctx context.Context, tx pgx.Tx, r refundRecord, code string) error {
	_, e := tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DEAD',claimed_until=NULL,last_safe_error=$2 WHERE id=$1`, r.Job, code)
	if e != nil {
		return e
	}
	if e = writeRefundStateTx(ctx, tx, r, r.Snapshot, ""); e != nil {
		return e
	}
	_, e = refundEventTx(ctx, tx, r.Intent.Scope.Refund, r.Version+1, "OriginalRefundReviewRequired", "")
	return e
}
func refundBusinessDenial(e error) bool {
	return errors.Is(e, ErrBinding) || errors.Is(e, ErrConflict) || errors.Is(e, ErrUnsupported) || errors.Is(e, ErrProof)
}
func (s *RefundService) claim(ctx context.Context, id string) (refundTicket, error) {
	ticket := refundTicket{}
	e := s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, r, e := loadRefundTx(ctx, tx, id)
		if e != nil {
			return e
		}
		var state, kind, target, dedupe string
		var generation int64
		var tries int
		var until *time.Time
		var runAt time.Time
		e = tx.QueryRow(ctx, `SELECT state,kind,target_ref,dedupe_key,claim_generation,tries,claimed_until,run_at FROM p11.durable_jobs WHERE id=$1 FOR UPDATE`, r.Job).Scan(&state, &kind, &target, &dedupe, &generation, &tries, &until, &runAt)
		if e != nil {
			return e
		}
		if kind != "RESOLVE_MACHINE_REFUND" || target != id || dedupe != "machine-refund:"+id || generation < 0 || tries < 0 {
			return ErrConflict
		}
		if state == "DONE" || state == "DEAD" || state == "PENDING" && runAt.After(o.Now) || state == "CLAIMED" && (until == nil || until.After(o.Now)) {
			return nil
		}
		if r.Snapshot.State == "SUCCEEDED" {
			_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DONE',claimed_until=NULL,last_safe_error=NULL WHERE id=$1`, r.Job)
			return e
		}
		if !o.Now.Before(r.Intent.ResolveUntil) || generation >= 9223372036854775807 || tries >= maximumRefundClaims {
			return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_REVIEW_REQUIRED")
		}
		registration, original, e := s.registration(r)
		if e != nil {
			return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_REGISTRATION_UNAVAILABLE")
		}
		submit := r.Snapshot.State == "REQUESTED"
		if e = requireRefundRouteTx(ctx, tx, o, original, registration, submit); e != nil {
			if refundBusinessDenial(e) {
				return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_AUTHORITY_CLOSED")
			}
			return e
		}
		var prepared RefundSnapshot
		if submit {
			if !o.Now.Before(r.Intent.SubmitBefore) {
				return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_SUBMIT_EXPIRED")
			}
			if e = requireFrozenRefundDecisionTx(ctx, tx, o, r); e != nil {
				if refundBusinessDenial(e) {
					return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_DECISION_CLOSED")
				}
				return e
			}
			rows, e := originalRefundsTx(ctx, tx, r.Payment.Snapshot)
			if e != nil {
				if refundBusinessDenial(e) {
					return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_CAPACITY_UNRESOLVED")
				}
				return e
			}
			held, e := HeldRefunds(r.Payment.Snapshot, rows)
			if e != nil {
				return e
			}
			required := r.Intent.Authority.Maximum
			if held > required {
				required = held
			}
			if e = refundCapacityTx(ctx, tx, o, r.Payment, required); e != nil {
				if refundBusinessDenial(e) {
					return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_WORK_OR_BUDGET_UNRESOLVED")
				}
				return e
			}
			frozenPaid := r.Payment
			frozenPaid.Snapshot.EvidenceHash = r.Intent.PaidEvidenceHash
			_, receipt, e := s.reverifyOriginalPaidTx(ctx, tx, frozenPaid, original, r.PaidReceipt)
			if e != nil {
				if refundBusinessDenial(e) {
					return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_PAID_ARCHIVE_UNVERIFIED")
				}
				return e
			}
			if receipt != r.PaidReceipt {
				return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_PAID_RECEIPT_CHANGED")
			}
			var last *time.Time
			if e = tx.QueryRow(ctx, `SELECT max(dispatched_at) FROM p11.refund_machine_contexts WHERE payment_id=$1`, r.Payment.Intent.Scope.Payment).Scan(&last); e != nil {
				return e
			}
			lastAt := time.Time{}
			if last != nil {
				lastAt = last.UTC().Round(0)
			}
			if !lastAt.IsZero() && o.Now.Before(lastAt.Add(MinimumRefundSubmitInterval)) {
				_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='PENDING',run_at=$2,claimed_until=NULL WHERE id=$1`, r.Job, lastAt.Add(MinimumRefundSubmitInterval))
				return e
			}
			prepared, e = PrepareRefundSubmit(r.Intent, r.Snapshot, lastAt, o.Now)
			if e != nil {
				return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_SUBMIT_PREPARATION_CLOSED")
			}
		} else {
			if o.Now.Before(r.Snapshot.NextQueryAt) {
				_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='PENDING',run_at=$2,claimed_until=NULL WHERE id=$1`, r.Job, r.Snapshot.NextQueryAt)
				return e
			}
			prepared, e = PrepareRefundQuery(r.Intent, r.Snapshot, o.Now)
			if e != nil {
				return refundReviewTx(ctx, tx, r, "REFUND_ORIGINAL_QUERY_PREPARATION_CLOSED")
			}
		}
		request := r.QueryRequest
		if submit {
			request = r.SubmitRequest
		}
		var operation string
		var valid bool
		e = tx.QueryRow(ctx, `SELECT operation,(target_ref=$2 AND provider=$3 AND environment=$4 AND original_request_key=$5 AND request_digest=$6)
FROM p11.channel_requests WHERE id=$1 FOR UPDATE`, request, id, r.Payment.Binding.Provider, r.Payment.Binding.Environment, r.Intent.Scope.OriginalRequestKey, r.Intent.Digest()).Scan(&operation, &valid)
		if e != nil {
			return e
		}
		if !valid || submit && operation != "REFUND" || !submit && operation != "QUERY" {
			return ErrConflict
		}
		if e = writeRefundStateTx(ctx, tx, r, prepared, ""); e != nil {
			return e
		}
		if _, e = refundEventTx(ctx, tx, id, r.Version+1, "OriginalRefundDispatchPrepared", ""); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.channel_requests SET state='SENT',next_query_at=$2,version=version+1 WHERE id=$1`, request, prepared.NextQueryAt); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='CLAIMED',claim_generation=claim_generation+1,tries=tries+1,claimed_until=$2,last_safe_error=NULL WHERE id=$1`, r.Job, o.Now.Add(20*time.Second))
		if e != nil {
			return e
		}
		r.Snapshot = prepared
		ticket = refundTicket{Record: r, Registration: registration, Request: request, JobGeneration: generation + 1, SnapshotGeneration: prepared.Generation, Submit: submit, Run: true}
		return nil
	})
	return ticket, e
}

// RecoverOne only selects installed original routes. It commits the claim and
// UNKNOWN/query generation BEFORE exactly one RPC; crash recovery queries the
// same original refund, never resubmits an UNKNOWN/PROCESSING financial action.
func (s *RefundService) RecoverOne(ctx context.Context) (bool, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return false, e
	}
	defer end()
	routes := make([]string, 0, len(s.registrations))
	for ref := range s.registrations {
		routes = append(routes, ref)
	}
	var id string
	e = s.machine.db.Pool.QueryRow(ctx, `SELECT c.refund_id FROM p11.refund_machine_contexts c JOIN p11.durable_jobs j ON j.id=c.durable_job_id
WHERE c.route_ref=ANY($1::text[]) AND j.kind='RESOLVE_MACHINE_REFUND'
AND ((j.state='PENDING' AND j.run_at<=clock_timestamp()) OR (j.state='CLAIMED' AND j.claimed_until<=clock_timestamp()))
ORDER BY j.run_at,j.id LIMIT 1`, routes).Scan(&id)
	if e == pgx.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	ticket, e := s.claim(ctx, id)
	if e != nil {
		return true, e
	}
	if !ticket.Run {
		return true, nil
	}
	var observation RefundObservation
	if ticket.Submit {
		observation, e = ticket.Registration.Channel.Submit(ctx, ticket.Record.Intent)
	} else {
		observation, e = ticket.Registration.Channel.Query(ctx, ticket.Record.Intent)
	}
	defer observation.Close()
	// Closing/cancelling the caller cannot throw away an already verified late
	// financial response. This owned operation is joined only after bounded save.
	persistCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	if saveErr := s.record(persistCtx, ticket, observation); saveErr != nil {
		return true, saveErr
	}
	return true, e
}

func (s *RefundService) record(ctx context.Context, t refundTicket, observation RefundObservation) error {
	var encrypted []byte
	var e error
	if observation.kind != "UNRESOLVED" {
		encrypted, e = s.machine.receipts.SealRefundReceipt(observation)
		if e != nil {
			return e
		}
		defer clear(encrypted)
	}
	return s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, r, e := loadRefundTx(ctx, tx, t.Record.Intent.Scope.Refund)
		if e != nil {
			return e
		}
		if r.Intent.Digest() != t.Record.Intent.Digest() || observation.scope != r.Intent.Scope || observation.intentHash != r.Intent.Digest() {
			return ErrConflict
		}
		var generation int64
		var state string
		var tries int
		e = tx.QueryRow(ctx, `SELECT claim_generation,state,tries FROM p11.durable_jobs WHERE id=$1 FOR UPDATE`, r.Job).Scan(&generation, &state, &tries)
		if e != nil {
			return e
		}
		current := state == "CLAIMED" && generation == t.JobGeneration && r.Snapshot.Generation == t.SnapshotGeneration
		receipt := ""
		if len(encrypted) > 0 {
			receipt = "rfc_" + outbox.Digest([]byte(r.Intent.Scope.Refund+"\x00"+observation.kind+"\x00"+observation.evidenceHash+"\x00"+observation.observed.UTC().Format(time.RFC3339Nano)))
			event := ids.New("evt")
			tag, e := tx.Exec(ctx, `INSERT INTO p11.refund_machine_receipts(id,refund_id,scope_hash,intent_digest,kind,method,evidence_hash,amount_fen,cumulative_fen,observed_at,encrypted_raw,encrypted_digest,event_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(id) DO NOTHING`, receipt, r.Intent.Scope.Refund, r.Intent.Scope.Digest(), r.Intent.Digest(), observation.kind, observation.method, observation.evidenceHash, observation.amount, observation.cumulative, observation.observed, encrypted, outbox.Digest(encrypted), event)
			if e != nil {
				return e
			}
			if tag.RowsAffected() == 1 {
				safe, _ := json.Marshal(map[string]string{"refund_id": r.Intent.Scope.Refund, "receipt_ref": receipt, "kind": observation.kind})
				_, e = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'REFUND_RECEIPT',$2,1,'OriginalRefundReceiptArchived','3.0',$3,'PENDING')`, event, receipt, safe)
				if e != nil {
					return e
				}
			} else {
				var same bool
				e = tx.QueryRow(ctx, `SELECT refund_id=$2 AND scope_hash=$3 AND intent_digest=$4 AND kind=$5 AND method=$6 AND evidence_hash=$7 AND amount_fen=$8 AND cumulative_fen=$9 AND observed_at=$10 FROM p11.refund_machine_receipts WHERE id=$1`,
					receipt, r.Intent.Scope.Refund, r.Intent.Scope.Digest(), r.Intent.Digest(), observation.kind, observation.method, observation.evidenceHash, observation.amount, observation.cumulative, observation.observed).Scan(&same)
				if e != nil {
					return e
				}
				if !same {
					return ErrConflict
				}
			}
		}
		// A stale worker can append a true original financial fact, never acknowledge
		// a later claim or overwrite its pending state/channel query metadata.
		if !current && !observation.IsSucceeded() {
			return nil
		}
		if r.Snapshot.State == "SUCCEEDED" {
			if current {
				_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DONE',claimed_until=NULL,last_safe_error=NULL WHERE id=$1 AND claim_generation=$2`, r.Job, t.JobGeneration)
			}
			return e
		}
		next, e := MergeRefund(r.Intent, r.Snapshot, observation)
		if e != nil {
			return e
		}
		if e = writeRefundStateTx(ctx, tx, r, next, receipt); e != nil {
			return e
		}
		kind := "OriginalRefundUnknown"
		if next.State == "PROCESSING" {
			kind = "OriginalRefundProcessing"
		}
		if next.State == "SUCCEEDED" {
			kind = "OriginalRefundSucceeded"
		}
		if _, e = refundEventTx(ctx, tx, r.Intent.Scope.Refund, r.Version+1, kind, receipt); e != nil {
			return e
		}
		if !current {
			return nil
		}
		requestState := "SUCCEEDED"
		if observation.kind == "UNRESOLVED" {
			requestState = "UNKNOWN"
		}
		safe, _ := json.Marshal(map[string]string{"profile": RefundProfile, "kind": observation.kind, "code": observation.safeCode, "receipt_ref": receipt})
		if _, e = tx.Exec(ctx, `UPDATE p11.channel_requests SET state=$2,safe_result=$3,next_query_at=$4,version=version+1 WHERE id=$1`, t.Request, requestState, safe, nullableRefundTime(next.NextQueryAt)); e != nil {
			return e
		}
		jobState, code := "PENDING", "REFUND_NOT_CONFIRMED"
		if next.State == "SUCCEEDED" {
			jobState = "DONE"
			code = ""
		} else if tries >= maximumRefundClaims {
			jobState = "DEAD"
			code = "REFUND_ORIGINAL_REVIEW_REQUIRED"
		}
		_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state=$3,run_at=COALESCE($4,run_at),claimed_until=NULL,last_safe_error=NULLIF($5,'') WHERE id=$1 AND claim_generation=$2 AND state='CLAIMED'`, r.Job, t.JobGeneration, jobState, nullableRefundTime(next.NextQueryAt), code)
		return e
	})
}
