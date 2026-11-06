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

func (s *Machine) recordObservation(ctx context.Context, payment, request string, o Observation) error {
	var encrypted []byte
	var e error
	if o.kind != "UNRESOLVED" {
		encrypted, e = s.receipts.SealReceipt(o)
		if e != nil {
			return e
		}
		defer clear(encrypted)
	}
	return s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		next, e := Merge(r.Snapshot, o)
		if e != nil {
			return e
		}
		var provider, environment, target, operation string
		e = tx.QueryRow(ctx, `SELECT provider,environment,target_ref,operation FROM p11.channel_requests WHERE id=$1 FOR UPDATE`, request).Scan(&provider, &environment, &target, &operation)
		if e != nil {
			return e
		}
		if provider != r.Binding.Provider || environment != r.Binding.Environment || target != payment ||
			(operation != "PAY" && operation != "QUERY") || (operation == "PAY" && request != r.PrecreateRequest) {
			return ErrConflict
		}
		receipt := ""
		if len(encrypted) > 0 {
			receipt = "mpc_" + outbox.Digest([]byte(payment+"\x00"+o.kind+"\x00"+o.evidenceHash))
			event := ids.New("evt")
			tag, e := tx.Exec(ctx, `INSERT INTO p11.payment_machine_receipts(id,payment_id,scope_hash,kind,evidence_hash,channel_reference,prepay_reference,encrypted_raw,encrypted_digest,observed_at,event_id)
VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11) ON CONFLICT(id) DO NOTHING`, receipt, payment, r.Intent.Scope.Digest(), o.kind, o.evidenceHash, o.trade, o.prepay, encrypted, outbox.Digest(encrypted), o.observed, event)
			if e != nil {
				return e
			}
			if tag.RowsAffected() == 1 {
				facts, _ := json.Marshal(map[string]string{"payment_id": payment, "receipt_ref": receipt, "kind": o.kind})
				_, e = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'PAYMENT_RECEIPT',$2,1,'PaymentMachineReceiptArchived','3.0',$3,'PENDING')`, event, receipt, facts)
				if e != nil {
					return e
				}
			} else {
				var same bool
				e = tx.QueryRow(ctx, `SELECT payment_id=$2 AND scope_hash=$3 AND kind=$4 AND evidence_hash=$5
AND COALESCE(channel_reference,'')=$6 AND COALESCE(prepay_reference,'')=$7 FROM p11.payment_machine_receipts WHERE id=$1`, receipt, payment, r.Intent.Scope.Digest(), o.kind, o.evidenceHash, o.trade, o.prepay).Scan(&same)
				if e != nil {
					return e
				}
				if !same {
					return ErrConflict
				}
			}
			if next.State != "SETTLED" || o.IsSettled() {
				_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_contexts SET last_receipt_id=$2,last_evidence_hash=$3,updated_at=clock_timestamp() WHERE payment_id=$1`, payment, receipt, o.evidenceHash)
				if e != nil {
					return e
				}
			}
		}
		// A late UNKNOWN cannot overwrite any previously verified paid fact.
		if !(r.Snapshot.State == "SETTLED" && !o.IsSettled()) {
			_, e = tx.Exec(ctx, `UPDATE p11.payments SET state=$2,prepay_reference=NULLIF($3,''),channel_reference=NULLIF($4,''),version=version+1 WHERE id=$1`, payment, next.State, next.PrepayReference, next.ChannelReference)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_contexts SET updated_at=clock_timestamp() WHERE payment_id=$1`, payment)
			if e != nil {
				return e
			}
			kind := "PaymentMachineUnknown"
			if o.kind == "PRECREATED" {
				kind = "PaymentMachinePrecreated"
			}
			if o.IsSettled() {
				kind = "PaymentMachineVerified"
			}
			if _, e = machineEventTx(ctx, tx, payment, r.Version+1, kind, receipt); e != nil {
				return e
			}
		}
		state := "SUCCEEDED"
		if o.kind == "UNRESOLVED" {
			state = "UNKNOWN"
		}
		safe, _ := json.Marshal(map[string]string{"profile": Profile, "kind": o.kind, "code": o.safeCode, "receipt_ref": receipt})
		_, e = tx.Exec(ctx, `UPDATE p11.channel_requests SET state=CASE WHEN state='SUCCEEDED' THEN state ELSE $2 END,
safe_result=CASE WHEN state='SUCCEEDED' AND $2='UNKNOWN' THEN safe_result ELSE $3::jsonb END,version=version+1 WHERE id=$1`, request, state, safe)
		return e
	})
}

type recoveryTicket struct {
	ID, Job, ChannelRequest string
	Generation              int64
	Record                  machineRecord
	Registration            Registration
	Ciphertext              []byte
	Expires                 time.Time
	Run                     bool
}

func (s *Machine) claimRecovery(ctx context.Context, id string) (recoveryTicket, error) {
	t := recoveryTicket{ID: id}
	e := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var payment string
		e := tx.QueryRow(ctx, `SELECT payment_id FROM p11.payment_machine_recoveries WHERE id=$1`, id).Scan(&payment)
		if e != nil {
			return e
		}
		o, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		t.Record = r
		var state, jobState string
		var until *time.Time
		var runAt time.Time
		var tries int
		e = tx.QueryRow(ctx, `SELECT x.state,x.proof_ciphertext,x.expires_at,x.channel_request_id,x.durable_job_id,
j.state,j.claim_generation,j.claimed_until,j.run_at,j.tries
FROM p11.payment_machine_recoveries x JOIN p11.durable_jobs j ON j.id=x.durable_job_id
WHERE x.id=$1 AND j.kind='VERIFY_MACHINE_PAYMENT' AND j.target_ref=x.id FOR UPDATE OF x,j`, id).
			Scan(&state, &t.Ciphertext, &t.Expires, &t.ChannelRequest, &t.Job, &jobState, &t.Generation, &until, &runAt, &tries)
		if e != nil {
			return e
		}
		if state == "RETIRED" || jobState == "DONE" || jobState == "DEAD" || jobState == "PENDING" && runAt.After(o.Now) || jobState == "CLAIMED" && (until == nil || until.After(o.Now)) {
			return nil
		}
		if r.Snapshot.State == "SETTLED" {
			var sameProofVerified bool
			e = tx.QueryRow(ctx, `SELECT state='SUCCEEDED' AND COALESCE(safe_result->>'kind','')='VERIFIED_PAID' FROM p11.channel_requests WHERE id=$1`, t.ChannelRequest).Scan(&sameProofVerified)
			if e != nil {
				return e
			}
			proofState, code := "UNKNOWN", "PAID_FACT_ALREADY_RECORDED"
			if sameProofVerified {
				proofState, code = "VERIFIED", ""
			}
			_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_recoveries SET state=$2,version=version+1 WHERE id=$1`, id, proofState)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DONE',claimed_until=NULL,last_safe_error=NULLIF($2,'') WHERE id=$1`, t.Job, code)
			return e
		}
		t.Registration, e = s.registration(r)
		if e == nil {
			e = requireMachineAuthorityTx(ctx, tx, t.Registration, o.Buyer, o.Now, false)
		}
		if e != nil && !errors.Is(e, ErrBinding) {
			return e
		}
		if e != nil || tries >= 8 || !o.Now.Before(t.Expires) {
			// This job ends, not its financial obligation. Retain original ids.
			_, err := tx.Exec(ctx, `UPDATE p11.payment_machine_recoveries SET state='UNKNOWN',version=version+1 WHERE id=$1`, id)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DEAD',claimed_until=NULL,last_safe_error='ORIGINAL_PROOF_REQUIRES_REVIEW' WHERE id=$1`, t.Job)
			return err
		}
		t.Generation++
		_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='CLAIMED',claim_generation=$2,claimed_until=clock_timestamp()+interval '20 seconds',tries=tries+1 WHERE id=$1`, t.Job, t.Generation)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_recoveries SET state='VERIFYING',version=version+1 WHERE id=$1`, id)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.channel_requests SET state='SENT',version=version+1 WHERE id=$1 AND state<>'SUCCEEDED'`, t.ChannelRequest)
		t.Run = e == nil
		return e
	})
	if e != nil {
		clear(t.Ciphertext)
		return recoveryTicket{}, e
	}
	return t, nil
}

// RecoverOne processes ONLY durable same-proof verification. It never repeats
// precreate, creates refunds, changes seller, or treats a lease timeout as an
// actual query return. No worker is started implicitly by the constructor.
func (s *Machine) RecoverOne(ctx context.Context) (bool, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return false, e
	}
	defer end()
	capabilities := make([]string, 0, len(s.registrations))
	for capability := range s.registrations {
		capabilities = append(capabilities, capability)
	}
	var id string
	e = s.db.Pool.QueryRow(ctx, `SELECT x.id FROM p11.payment_machine_recoveries x JOIN p11.durable_jobs j ON j.id=x.durable_job_id
JOIN p11.payment_machine_contexts c ON c.payment_id=x.payment_id
WHERE c.capability_ref=ANY($1::text[]) AND j.kind='VERIFY_MACHINE_PAYMENT'
AND ((j.state='PENDING' AND j.run_at<=clock_timestamp()) OR (j.state='CLAIMED' AND j.claimed_until<=clock_timestamp()))
ORDER BY j.run_at,j.id LIMIT 1`, capabilities).Scan(&id)
	if e == pgx.ErrNoRows {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	t, e := s.claimRecovery(ctx, id)
	if e != nil {
		return true, e
	}
	defer clear(t.Ciphertext)
	if !t.Run {
		return true, nil
	}
	proof, e := s.vault.Open(t.Record.Intent.Scope, t.Ciphertext, time.Now().UTC())
	o := Observation{scope: t.Record.Intent.Scope, amount: t.Record.Intent.Amount, kind: "UNRESOLVED", safeCode: "PROOF_UNAVAILABLE", observed: time.Now().UTC().Truncate(time.Microsecond)}
	if e == nil {
		o, e = t.Registration.Channel.Verify(ctx, t.Record.Intent, proof)
		proof.Close()
	}
	defer o.Close()
	// Even a delayed valid paid response is archived under its original scope;
	// a stale worker cannot acknowledge a newer job generation.
	persistCtx, persistEnd := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	saveErr := s.recordObservation(persistCtx, t.Record.Intent.Scope.Payment, t.ChannelRequest, o)
	persistEnd()
	if saveErr != nil {
		return true, saveErr
	}
	paid := o.IsSettled()
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, _, err := loadMachineTx(ctx, tx, t.Record.Intent.Scope.Payment)
		if err != nil {
			return err
		}
		var owned bool
		err = tx.QueryRow(ctx, `SELECT state='CLAIMED' AND claim_generation=$2 AND claimed_until>clock_timestamp()
FROM p11.durable_jobs WHERE id=$1 FOR UPDATE`, t.Job, t.Generation).Scan(&owned)
		if err != nil {
			return err
		}
		if !owned {
			return ErrUnknown
		}
		state, jobState, code := "UNKNOWN", "PENDING", "CHANNEL_PENDING"
		if paid {
			state, jobState, code = "VERIFIED", "DONE", ""
		}
		_, err = tx.Exec(ctx, `UPDATE p11.payment_machine_recoveries SET state=$2,version=version+1 WHERE id=$1 AND state<>'RETIRED'`, t.ID, state)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state=$2,claimed_until=NULL,last_safe_error=NULLIF($3,''),
run_at=clock_timestamp()+make_interval(secs=>power(2,least(greatest(tries,1),8)-1)::double precision) WHERE id=$1`, t.Job, jobState, code)
		return err
	})
	return true, e
}

// RetireExpiredProof erases only expired credential ciphertext after retaining
// the original tombstone. It changes neither payment nor refund obligations.
func (s *Machine) RetireExpiredProof(ctx context.Context, id string) error {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return e
	}
	defer end()
	if !ids.Valid(id) {
		return ErrIntent
	}
	return s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var payment string
		if e := tx.QueryRow(ctx, `SELECT payment_id FROM p11.payment_machine_recoveries WHERE id=$1`, id).Scan(&payment); e != nil {
			return e
		}
		o, _, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		var expires time.Time
		var job string
		if e = tx.QueryRow(ctx, `SELECT expires_at,durable_job_id FROM p11.payment_machine_recoveries WHERE id=$1 FOR UPDATE`, id).Scan(&expires, &job); e != nil {
			return e
		}
		if o.Now.Before(expires) {
			return ErrConflict
		}
		var live bool
		if e = tx.QueryRow(ctx, `SELECT state='CLAIMED' AND claimed_until>clock_timestamp() FROM p11.durable_jobs WHERE id=$1 FOR UPDATE`, job).Scan(&live); e != nil {
			return e
		}
		if live {
			return ErrUnknown
		}
		_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_recoveries SET state='RETIRED',proof_ciphertext=NULL,version=version+1 WHERE id=$1`, id)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.durable_jobs SET state=CASE WHEN state='DONE' THEN state ELSE 'DEAD' END,claimed_until=NULL,last_safe_error=CASE WHEN state='DONE' THEN NULL ELSE 'ORIGINAL_PROOF_EXPIRED' END WHERE id=$1`, job)
		return e
	})
}
