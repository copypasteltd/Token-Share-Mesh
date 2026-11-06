package invocation

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/settlement"
	"p11.local/compute/internal/usage"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

type Completion struct {
	claims *Claims
	db     *persistence.DB
	meters *usage.Verifier
}

func NewCompletion(claims *Claims, meters *usage.Verifier) (*Completion, error) {
	if claims == nil || claims.db == nil {
		return nil, fault.New(fault.Invalid, "Original execution verifier required")
	}
	return &Completion{claims: claims, db: claims.db, meters: meters}, nil
}
func (s *Completion) UsesClaims(c *Claims) bool      { return s != nil && s.claims == c }
func (s *Completion) UsesDB(db *persistence.DB) bool { return s != nil && s.db == db }

type ResultView struct {
	ID               string    `json:"delivery_id"`
	Attempt          string    `json:"attempt_id"`
	Revision         int64     `json:"revision"`
	ManifestHash     string    `json:"manifest_hash"`
	OutputHash       string    `json:"output_digest"`
	OutputBytes      int64     `json:"output_bytes"`
	State            string    `json:"state"`
	DecisionDeadline time.Time `json:"decision_deadline"`
	Usage            string    `json:"usage_state"`
}
type JobView struct {
	ID       string             `json:"job_id"`
	Order    string             `json:"order_id"`
	Seller   string             `json:"seller_id"`
	State    string             `json:"state"`
	Version  int64              `json:"version"`
	Attempt  string             `json:"current_attempt_id,omitempty"`
	Accepted string             `json:"accepted_delivery_id,omitempty"`
	Deadline time.Time          `json:"deadline_at"`
	Results  []ResultView       `json:"results"`
	Billing  settlement.Outcome `json:"billing"`
}
type DecisionInput struct {
	ExpectedVersion    int64  `json:"expected_job_version"`
	Delivery           string `json:"delivery_id"`
	Revision           int64  `json:"revision"`
	ManifestHash       string `json:"manifest_hash"`
	ReviewedOutputHash string `json:"reviewed_output_digest"`
	Decision           string `json:"decision"`
}
type CancelInput struct {
	ExpectedVersion int64  `json:"expected_job_version"`
	Reason          string `json:"reason_code"`
}
type CompletionAck struct {
	Job      string             `json:"job_id"`
	Delivery string             `json:"delivery_id,omitempty"`
	ID       string             `json:"id"`
	Version  int64              `json:"version"`
	State    string             `json:"state"`
	Billing  settlement.Outcome `json:"billing"`
}

func decisionValid(in DecisionInput) bool {
	return in.ExpectedVersion > 0 && in.ExpectedVersion < 2147483647 && ids.Valid(in.Delivery) && in.Revision == 1 && executionlease.Hash(in.ManifestHash) && executionlease.Hash(in.ReviewedOutputHash) && (in.Decision == "ACCEPT" || in.Decision == "REJECT")
}
func (s *Completion) ready() error {
	if s == nil || s.db == nil || s.claims == nil {
		return fault.New(fault.Unavailable, "Original completion service unavailable")
	}
	return nil
}

func (s *Completion) participantTx(ctx context.Context, tx pgx.Tx, p identity.Principal, job, scope string, buyerOnly bool) (string, error) {
	if p.RequireUser(scope) != nil {
		return "", fault.New(fault.Forbidden, "Current participant session required")
	}
	if e := identity.RequireCurrentTx(ctx, tx, p); e != nil {
		return "", e
	}
	var order, buyer, seller string
	e := tx.QueryRow(ctx, `SELECT o.id,o.buyer_id,o.seller_id FROM p11.jobs j JOIN p11.orders o ON o.id=j.order_id WHERE j.id=$1 AND (o.buyer_id=$2 OR o.seller_id=$2)`, job, p.UserID).Scan(&order, &buyer, &seller)
	if e == pgx.ErrNoRows {
		return "", fault.New(fault.NotFound, "Original participant job unavailable")
	}
	if e != nil {
		return "", e
	}
	role := "BUYER"
	if p.UserID != buyer {
		if buyerOnly {
			return "", fault.New(fault.Forbidden, "Original buyer required")
		}
		role = "SELLER"
	}
	if e = identity.RequireRoleTx(ctx, tx, p, scope, role, true); e != nil {
		return "", e
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM p11.orders WHERE id=$1 FOR UPDATE`, order).Scan(&id); e != nil {
		return "", e
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM p11.jobs WHERE id=$1 FOR UPDATE`, job).Scan(&id); e != nil {
		return "", e
	}
	return order, nil
}

func (s *Completion) nativeCommand(ctx context.Context, p identity.Principal, op, attempt, lease, fence, key string, in any, fn func(pgx.Tx, reportAuthority) (outbox.StoredResult, error)) (ReportResult, error) {
	if e := s.ready(); e != nil {
		return ReportResult{}, e
	}
	if !ids.Valid(attempt) || !ids.Valid(lease) {
		return ReportResult{}, fault.New(fault.Invalid, "Original result owner required")
	}
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > 16384 {
		return ReportResult{}, fault.New(fault.Invalid, "Bounded result statement required")
	}
	var a reportAuthority
	r, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: op, Resource: p.DeviceID + ":" + attempt, Key: key, Request: raw}, func(tx pgx.Tx) error {
		var e error
		a, e = s.claims.reportAuthorityTx(ctx, tx, p, attempt, lease, fence)
		return e
	}, func(tx pgx.Tx) (outbox.StoredResult, error) { return fn(tx, a) })
	return ReportResult{Body: r.Body, Replayed: r.Replayed}, e
}

func originalManifest(ctx context.Context, tx pgx.Tx, a reportAuthority) (executionlease.Manifest, string, error) {
	var raw []byte
	var buyer string
	var m executionlease.Manifest
	e := tx.QueryRow(ctx, `SELECT l.execution_manifest,o.buyer_id FROM p11.execution_leases l JOIN p11.jobs j ON j.id=l.job_id JOIN p11.orders o ON o.id=j.order_id WHERE l.id=$1`, a.lease.ID).Scan(&raw, &buyer)
	if e != nil {
		return m, "", e
	}
	if strictjson.Decode(raw, &m, 16384) != nil || m.Digest() != a.lease.ManifestHash || m.Mode != "PLAINTEXT" || m.Privacy != "STANDARD" {
		return m, "", fault.New(fault.Conflict, "Original supported result manifest required")
	}
	return m, buyer, nil
}

// Register records bounded metadata, not asset upload or buyer acceptance.
// Late/stale facts remain historical; no resource, lease or hold is released.
func (s *Completion) Register(ctx context.Context, p identity.Principal, attempt string, in resultwire.Delivery, key string) (ReportResult, error) {
	if in.Validate() != nil || in.Scope.Attempt != attempt {
		return ReportResult{}, fault.New(fault.Invalid, "Complete signed original delivery required")
	}
	return s.nativeCommand(ctx, p, "registerAttemptDelivery", attempt, in.Scope.Lease, in.Scope.Fence, key, in, func(tx pgx.Tx, a reportAuthority) (outbox.StoredResult, error) {
		if in.Scope != resultwire.Original(a.lease) || in.Verify(a.public) != nil {
			return outbox.StoredResult{}, fault.New(fault.Forbidden, "Original device delivery signature rejected")
		}
		m, _, e := originalManifest(ctx, tx, a)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		completed, _ := time.Parse(time.RFC3339Nano, in.Completed)
		hard, _ := time.Parse(time.RFC3339Nano, m.HardDeadline)
		if completed.Before(a.issued) || completed.After(a.now.Add(30*time.Second)) || completed.After(hard) || in.InputTokens > m.InputTokenLimit || in.OutputTokens > int64(m.Spec.MaxOutputTokens) {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Delivery outside original execution bounds")
		}
		var old string
		var previous []byte
		e = tx.QueryRow(ctx, `SELECT payload_digest,safe_response FROM p11.native_result_deliveries WHERE attempt_id=$1`, attempt).Scan(&old, &previous)
		if e == nil {
			if old != resultwire.Digest(in) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original attempt already has another complete result")
			}
			var view ResultView
			if strictjson.Decode(previous, &view, 4096) != nil {
				return outbox.StoredResult{}, fault.New(fault.Unavailable, "Original result recovery unavailable")
			}
			return outbox.StoredResult{Reference: view.ID, Body: previous, HTTPStatus: 200, Replayed: true}, nil
		}
		if e != pgx.ErrNoRows {
			return outbox.StoredResult{}, e
		}
		// Result custody outlives execution cleanup and physical resource reuse.
		// Only this job's current attempt/fence decides result currency. The next
		// physical owner is neither read as a result owner nor changed here.
		var currentJob bool
		var originalDeadline time.Time
		if e = tx.QueryRow(ctx, `SELECT current_attempt_id=$2 AND fencing_token=$3,deadline FROM p11.jobs WHERE id=$1`, in.Scope.Job, attempt, in.Scope.Fence).Scan(&currentJob, &originalDeadline); e != nil {
			return outbox.StoredResult{}, e
		}
		decisionDeadline := completed.Add(120 * time.Second)
		if originalDeadline.Before(decisionDeadline) {
			decisionDeadline = originalDeadline
		}
		state := "STALE"
		if currentJob && a.now.Before(decisionDeadline) && (a.jobState == "RUNNING" || a.jobState == "UNVERIFIED") {
			state = "WAITING_BUYER"
		}
		view := ResultView{ID: ids.New("delivery"), Attempt: attempt, Revision: 1, ManifestHash: resultwire.Digest(in), OutputHash: in.OutputHash, OutputBytes: in.OutputBytes, State: state, DecisionDeadline: decisionDeadline, Usage: "UNVERIFIED"}
		payload, _ := json.Marshal(in)
		safe, _ := json.Marshal(view)
		_, e = tx.Exec(ctx, `INSERT INTO p11.deliveries(id,job_id,attempt_id,fencing_token,revision,manifest_hash,asset_ids,state) VALUES($1,$2,$3,$4,1,$5,'{}',$6)`, view.ID, in.Scope.Job, attempt, in.Scope.Fence, view.ManifestHash, state)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO p11.native_result_deliveries(delivery_id,job_id,attempt_id,lease_id,payload_raw,payload_digest,output_digest,output_bytes,decision_deadline,safe_response) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, view.ID, in.Scope.Job, attempt, in.Scope.Lease, payload, view.ManifestHash, in.OutputHash, in.OutputBytes, view.DecisionDeadline, safe)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if state == "WAITING_BUYER" {
			if _, e = tx.Exec(ctx, `UPDATE p11.jobs SET state='WAITING_BUYER',version=version+1 WHERE id=$1`, in.Scope.Job); e != nil {
				return outbox.StoredResult{}, e
			}
			if _, e = tx.Exec(ctx, `UPDATE p11.attempts SET state='DELIVERED',version=version+1 WHERE id=$1 AND state IN('ISSUED','ACKED','RUNNING') AND cleanup_state='PENDING'`, attempt); e != nil {
				return outbox.StoredResult{}, e
			}
		}
		if e = completionEvent(ctx, tx, in.Scope.Job, "OriginalResultRegistered", map[string]any{"delivery_id": view.ID, "state": state}); e != nil {
			return outbox.StoredResult{}, e
		}
		return outbox.StoredResult{Reference: view.ID, Body: safe, HTTPStatus: 200}, nil
	})
}

// RecordUsage accepts only independently signed and currently approved original
// quantities. It may finish the accounting for an already accepted result.
func (s *Completion) RecordUsage(ctx context.Context, p identity.Principal, attempt string, in resultwire.Statement, key string) (ReportResult, error) {
	if in.Scope.Attempt != attempt {
		return ReportResult{}, fault.New(fault.Invalid, "Original usage attempt required")
	}
	if e := in.Validate(); e != nil {
		return ReportResult{}, fault.New(fault.Invalid, "Bounded independent usage statement required")
	}
	return s.nativeCommand(ctx, p, "recordAttemptUsage", attempt, in.Scope.Lease, in.Scope.Fence, key, in, func(tx pgx.Tx, a reportAuthority) (outbox.StoredResult, error) {
		if in.Scope != resultwire.Original(a.lease) {
			return outbox.StoredResult{}, fault.New(fault.Forbidden, "Original usage scope differs")
		}
		m, buyer, e := originalManifest(ctx, tx, a)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT payload_raw FROM p11.native_result_deliveries WHERE attempt_id=$1 AND payload_digest=$2`, attempt, in.DeliveryHash).Scan(&raw); e == pgx.ErrNoRows {
			return outbox.StoredResult{}, fault.New(fault.NotFound, "Original complete result unavailable")
		}
		if e != nil {
			return outbox.StoredResult{}, e
		}
		var d resultwire.Delivery
		if strictjson.Decode(raw, &d, 16384) != nil || d.Verify(a.public) != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original result archive rejected")
		}
		var id, digest string
		e = tx.QueryRow(ctx, `SELECT usage_id,statement_digest FROM p11.native_usage_verifications WHERE attempt_id=$1`, attempt).Scan(&id, &digest)
		if e == nil {
			if digest != resultwire.Digest(in) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original verified usage cannot be replaced")
			}
		} else if e == pgx.ErrNoRows {
			verified, ve := s.meters.VerifyTx(ctx, tx, in, d, m, buyer)
			if ve != nil {
				return outbox.StoredResult{}, ve
			}
			id = verified.ID
		} else {
			return outbox.StoredResult{}, e
		}
		bill, e := settlement.AcceptanceTx(ctx, tx, m.Job)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		ack := CompletionAck{Job: m.Job, ID: id, Version: 1, State: "VERIFIED", Billing: bill}
		safe, _ := json.Marshal(ack)
		if e = completionEvent(ctx, tx, m.Job, "OriginalUsageVerified", map[string]any{"usage_id": id, "billing_state": bill.State}); e != nil {
			return outbox.StoredResult{}, e
		}
		return outbox.StoredResult{Reference: id, Body: safe, HTTPStatus: 200}, nil
	})
}

func completionEvent(ctx context.Context, tx pgx.Tx, job, kind string, fields map[string]any) error {
	fields["job_id"] = job
	safe, _ := json.Marshal(fields)
	_, e := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) SELECT $1,'ORIGINAL_COMPLETION',id,version,$3,'3.0',$4,'PENDING' FROM p11.jobs WHERE id=$2`, ids.New("evt"), job, kind, safe)
	return e
}

func (s *Completion) Decide(ctx context.Context, p identity.Principal, job string, in DecisionInput, key string) (ReportResult, error) {
	if e := s.ready(); e != nil {
		return ReportResult{}, e
	}
	if !ids.Valid(job) || !decisionValid(in) {
		return ReportResult{}, fault.New(fault.Invalid, "Original delivery version and local reviewed digest required")
	}
	raw, _ := json.Marshal(in)
	r, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "recordBuyerReceipt", Resource: job, Key: key, Request: raw}, func(tx pgx.Tx) error { _, e := s.participantTx(ctx, tx, p, job, "job:decide", true); return e }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		var state, attempt string
		var version int64
		var current bool
		e := tx.QueryRow(ctx, `SELECT j.state,j.version,j.current_attempt_id,
 (d.id=$2 AND d.attempt_id=j.current_attempt_id AND d.revision=$3 AND d.manifest_hash=$4 AND d.state='WAITING_BUYER' AND n.output_digest=$5 AND n.decision_deadline>clock_timestamp())
 FROM p11.jobs j JOIN p11.deliveries d ON d.job_id=j.id AND d.id=$2 JOIN p11.native_result_deliveries n ON n.delivery_id=d.id WHERE j.id=$1`, job, in.Delivery, in.Revision, in.ManifestHash, in.ReviewedOutputHash).Scan(&state, &version, &attempt, &current)
		if e == pgx.ErrNoRows {
			return outbox.StoredResult{}, fault.New(fault.NotFound, "Original reviewable delivery unavailable")
		}
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if version != in.ExpectedVersion || state != "WAITING_BUYER" || !current {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original result changed, canceled, decided or timed out")
		}
		accept := ids.New("acceptance")
		digest := resultwire.Digest(struct {
			Buyer, Job string
			Input      DecisionInput
		}{p.UserID, job, in})
		_, e = tx.Exec(ctx, `INSERT INTO p11.acceptances(id,job_id,delivery_id,attempt_id,signer_id,evidence_hash,decision,native_profile,decision_version,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,'P11_BUYER_RESULT_DECISION_1',$8,$9)`, accept, job, in.Delivery, attempt, p.UserID, digest, in.Decision, version, outbox.Digest(raw))
		if e != nil {
			return outbox.StoredResult{}, e
		}
		next, deliveryState := "ACCEPTED", "VALID"
		var accepted *string
		if in.Decision == "ACCEPT" {
			accepted = &in.Delivery
		} else {
			next, deliveryState = "FAILED", "INVALID"
		}
		_, e = tx.Exec(ctx, `UPDATE p11.jobs SET state=$2,accepted_delivery_id=$3,version=version+1 WHERE id=$1`, job, next, accepted)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.deliveries SET state=$2 WHERE id=$1`, in.Delivery, deliveryState); e != nil {
			return outbox.StoredResult{}, e
		}
		bill, e := settlement.AcceptanceTx(ctx, tx, job)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		ack := CompletionAck{Job: job, Delivery: in.Delivery, ID: accept, Version: version + 1, State: next, Billing: bill}
		safe, _ := json.Marshal(ack)
		if e = completionEvent(ctx, tx, job, "OriginalBuyerResultDecided", map[string]any{"delivery_id": in.Delivery, "acceptance_id": accept, "decision": in.Decision, "billing_state": bill.State}); e != nil {
			return outbox.StoredResult{}, e
		}
		return outbox.StoredResult{Reference: accept, Body: safe, HTTPStatus: 200}, nil
	})
	return ReportResult{Body: r.Body, Replayed: r.Replayed}, e
}

// Cancel takes the same order/job lock as acceptance. It revokes execution and
// asks the exact original owner to stop; cleanup, result destruction and money
// are not inferred from that request, timeout or disconnected transport.
func (s *Completion) Cancel(ctx context.Context, p identity.Principal, job string, in CancelInput, key string) (ReportResult, error) {
	if e := s.ready(); e != nil {
		return ReportResult{}, e
	}
	if !ids.Valid(job) || in.ExpectedVersion < 1 || in.ExpectedVersion >= 2147483647 || (in.Reason != "BUYER_REQUEST" && in.Reason != "SERVICE_UNAVAILABLE") {
		return ReportResult{}, fault.New(fault.Invalid, "Bounded original cancellation required")
	}
	raw, _ := json.Marshal(in)
	r, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "cancelJob", Resource: job, Key: key, Request: raw}, func(tx pgx.Tx) error { _, e := s.participantTx(ctx, tx, p, job, "job:cancel", true); return e }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		var state string
		var version int64
		var attempt *string
		if e := tx.QueryRow(ctx, `SELECT state,version,current_attempt_id FROM p11.jobs WHERE id=$1`, job).Scan(&state, &version, &attempt); e != nil {
			return outbox.StoredResult{}, e
		}
		if version != in.ExpectedVersion || (state != "INPUT_PENDING" && state != "QUEUED" && state != "RUNNING" && state != "VALIDATING" && state != "WAITING_BUYER" && state != "UNVERIFIED") {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original job already changed or reached a terminal decision")
		}
		if attempt != nil {
			var resource string
			var generation int64
			if e := tx.QueryRow(ctx, `SELECT resource_id,resource_lock_generation FROM p11.attempts WHERE id=$1`, *attempt).Scan(&resource, &generation); e != nil {
				return outbox.StoredResult{}, e
			}
			var locked string
			if e := tx.QueryRow(ctx, `SELECT resource_id FROM p11.resource_locks WHERE resource_id=$1 FOR UPDATE`, resource).Scan(&locked); e != nil {
				return outbox.StoredResult{}, e
			}
			if _, e := tx.Exec(ctx, `UPDATE p11.execution_leases SET state='REVOKED' WHERE attempt_id=$1 AND state IN('ISSUED','ACKED')`, *attempt); e != nil {
				return outbox.StoredResult{}, e
			}
			if _, e := tx.Exec(ctx, `UPDATE p11.resource_locks SET phase='STOPPING',version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1 AND owner_attempt_id=$2 AND lock_generation=$3 AND phase='ACTIVE'`, resource, *attempt, generation); e != nil {
				return outbox.StoredResult{}, e
			}
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.jobs SET state='CANCELED',version=version+1 WHERE id=$1`, job); e != nil {
			return outbox.StoredResult{}, e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.deliveries SET state='STALE' WHERE job_id=$1 AND state IN('REGISTERED','VALIDATING','WAITING_BUYER')`, job); e != nil {
			return outbox.StoredResult{}, e
		}
		ack := CompletionAck{Job: job, ID: job, Version: version + 1, State: "CANCELED", Billing: settlement.Outcome{State: "HELD_PENDING_EVIDENCE"}}
		safe, _ := json.Marshal(ack)
		if e := completionEvent(ctx, tx, job, "OriginalJobCanceled", map[string]any{"reason_code": in.Reason}); e != nil {
			return outbox.StoredResult{}, e
		}
		return outbox.StoredResult{Reference: job, Body: safe, HTTPStatus: 200}, nil
	})
	return ReportResult{Body: r.Body, Replayed: r.Replayed}, e
}

func loadJobView(ctx context.Context, tx pgx.Tx, id string) (JobView, error) {
	v := JobView{Results: []ResultView{}, Billing: settlement.Outcome{State: "HELD_PENDING_EVIDENCE"}}
	e := tx.QueryRow(ctx, `SELECT id,order_id,seller_id,state,version,COALESCE(current_attempt_id::text,''),COALESCE(accepted_delivery_id::text,''),deadline FROM p11.jobs WHERE id=$1`, id).Scan(&v.ID, &v.Order, &v.Seller, &v.State, &v.Version, &v.Attempt, &v.Accepted, &v.Deadline)
	if e != nil {
		return v, e
	}
	rows, e := tx.Query(ctx, `SELECT d.id,d.attempt_id,d.revision,d.manifest_hash,n.output_digest,n.output_bytes,d.state,n.decision_deadline,CASE WHEN EXISTS(SELECT 1 FROM p11.native_usage_verifications u WHERE u.attempt_id=d.attempt_id) THEN 'VERIFIED' ELSE 'UNVERIFIED' END FROM p11.deliveries d JOIN p11.native_result_deliveries n ON n.delivery_id=d.id WHERE d.job_id=$1 ORDER BY d.created_at,d.id LIMIT 3`, id)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var d ResultView
		if e = rows.Scan(&d.ID, &d.Attempt, &d.Revision, &d.ManifestHash, &d.OutputHash, &d.OutputBytes, &d.State, &d.DecisionDeadline, &d.Usage); e != nil {
			rows.Close()
			return v, e
		}
		v.Results = append(v.Results, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT safe_outcome FROM p11.accepted_usage_settlements WHERE job_id=$1`, id).Scan(&raw)
	if e == nil && strictjson.Decode(raw, &v.Billing, 4096) != nil {
		return v, fault.New(fault.Unavailable, "Original billing archive rejected")
	}
	if e == pgx.ErrNoRows {
		e = nil
	}
	return v, e
}
func (s *Completion) Job(ctx context.Context, p identity.Principal, id string) (JobView, error) {
	var v JobView
	if e := s.ready(); e != nil {
		return v, e
	}
	if !ids.Valid(id) {
		return v, fault.New(fault.Invalid, "Original job reference required")
	}
	e := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if _, e := s.participantTx(ctx, tx, p, id, "job:read", false); e != nil {
			return e
		}
		var e error
		v, e = loadJobView(ctx, tx, id)
		return e
	})
	return v, e
}

type BillView struct {
	Order             string    `json:"order_id"`
	Version           int64     `json:"version"`
	Gross             money.Fen `json:"gross_fen"`
	Commission        money.Fen `json:"commission_fen"`
	ProvisionalSeller money.Fen `json:"provisional_seller_fen"`
	Reserved          money.Fen `json:"reserved_fen"`
	Unverified        money.Fen `json:"unverified_fen"`
	ChannelCost       string    `json:"channel_cost_state"`
	Payout            string    `json:"payout_state"`
	IsFinal           bool      `json:"is_final"`
	Watermark         time.Time `json:"as_of"`
}

func (s *Completion) Bill(ctx context.Context, p identity.Principal, id string) (BillView, error) {
	v := BillView{Order: id, ChannelCost: "UNKNOWN", Payout: "NOT_QUALIFIED"}
	if e := s.ready(); e != nil {
		return v, e
	}
	if !ids.Valid(id) {
		return v, fault.New(fault.Invalid, "Original order required")
	}
	e := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if p.RequireUser("bill:read") != nil {
			return fault.New(fault.Forbidden, "Current original participant required")
		}
		if e := identity.RequireCurrentTx(ctx, tx, p); e != nil {
			return e
		}
		var buyer, seller string
		e := tx.QueryRow(ctx, `SELECT buyer_id,seller_id,version,budget_reserved_fen,budget_unverified_fen,clock_timestamp() FROM p11.orders WHERE id=$1 AND (buyer_id=$2 OR seller_id=$2) FOR SHARE`, id, p.UserID).Scan(&buyer, &seller, &v.Version, &v.Reserved, &v.Unverified, &v.Watermark)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original bill unavailable")
		}
		if e != nil {
			return e
		}
		role := "BUYER"
		if buyer != p.UserID {
			role = "SELLER"
		}
		if e = identity.RequireRoleTx(ctx, tx, p, "bill:read", role, true); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT COALESCE(sum(gross_fen),0)::bigint,COALESCE(sum(commission_fen),0)::bigint,COALESCE(sum(provisional_seller_fen),0)::bigint FROM p11.accepted_usage_settlements WHERE order_id=$1`, id).Scan(&v.Gross, &v.Commission, &v.ProvisionalSeller)
	})
	return v, e
}
