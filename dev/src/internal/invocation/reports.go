package invocation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

type ReportResult struct {
	Body     json.RawMessage
	Replayed bool
}
type reportAuthority struct {
	lease                                                                                               executionlease.Lease
	public                                                                                              ed25519.PublicKey
	now, issued, expires                                                                                time.Time
	jobState, attemptState, leaseState, lockPhase, owner, deviceState, sellerState, last, progressPhase string
	current                                                                                             bool
}

// Report authorization deliberately differs from fresh input admission: an
// expired/revoked buyer call must still be able to STOP and clean its ORIGINAL
// owner. No current funds, input decryption, replacement seller or renewal is
// inferred. Every replay still rechecks the current signing device and seller.
func (s *Claims) reportAuthorityTx(ctx context.Context, tx pgx.Tx, p identity.Principal, attempt, leaseID, fence string) (reportAuthority, error) {
	var v reportAuthority
	if p.Kind != identity.Device || !ids.Valid(p.DeviceID) || p.Require("device:execute", "") != nil {
		return v, fault.New(fault.Forbidden, "Current device signing identity required")
	}
	if e := identity.RequireRoleTx(ctx, tx, p, "device:execute", "SELLER", false); e != nil {
		return v, e
	}
	var order, job, resource string
	e := tx.QueryRow(ctx, `SELECT a.order_id,a.job_id,a.resource_id FROM p11.attempts a JOIN p11.execution_leases l ON l.attempt_id=a.id WHERE a.id=$1 AND a.seller_id=$2 AND a.device_id=$3 AND l.id=$4`, attempt, p.UserID, p.DeviceID, leaseID).Scan(&order, &job, &resource)
	if e == pgx.ErrNoRows {
		return v, fault.New(fault.NotFound, "Original report owner unavailable")
	}
	if e != nil {
		return v, e
	}
	// Same business lock order as original admission: order, job, physical gate,
	// then original attempt/lease. Never lock a guessed replacement occupant.
	for _, q := range []struct{ sql, id string }{{`SELECT id FROM p11.orders WHERE id=$1 FOR UPDATE`, order}, {`SELECT id FROM p11.jobs WHERE id=$1 FOR UPDATE`, job}, {`SELECT resource_id FROM p11.resource_locks WHERE resource_id=$1 FOR UPDATE`, resource}} {
		var locked string
		if e = tx.QueryRow(ctx, q.sql, q.id).Scan(&locked); e != nil {
			return v, e
		}
	}
	var payload, manifest []byte
	var signer, pub, fp, profile, capability, generation, actualFence, env, manifestID, manifestHash, contract, route, claimProfile, claimDevice, admission string
	var hard time.Time
	e = tx.QueryRow(ctx, `SELECT l.signed_payload,l.execution_manifest,l.issuer_key_id,l.admission_profile,l.claim_profile,COALESCE(l.claim_device_id::text,''),
a.capability_id,a.resource_lock_generation::text,a.fencing_token::text,c.kernel_environment_digest,l.execution_manifest_ref,l.execution_manifest_hash,o.contract_hash,j.route_contract_id,
l.hard_deadline,l.issued_at,l.expires_at,l.state,l.last_progress_sequence::text,a.state,j.state,rl.phase,COALESCE(rl.owner_attempt_id::text,''),d.state,sp.state,
d.device_public_key,d.auth_key_digest,d.identity_profile,clock_timestamp(),
(j.current_attempt_id=a.id AND j.fencing_token=a.fencing_token AND rl.lock_generation=a.resource_lock_generation AND rl.owner_session_id IS NULL),
COALESCE((SELECT safe_request->>'phase' FROM p11.native_execution_reports r WHERE r.lease_id=l.id AND r.kind='PROGRESS' ORDER BY r.sequence DESC LIMIT 1),'')
FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id
JOIN p11.orders o ON o.id=a.order_id JOIN p11.devices d ON d.id=a.device_id JOIN p11.seller_profiles sp ON sp.id=a.seller_id
JOIN p11.capabilities c ON c.id=a.capability_id JOIN p11.resource_locks rl ON rl.resource_id=a.resource_id
WHERE l.id=$1 AND a.id=$2 AND a.device_id=$3 AND a.seller_id=$4 FOR UPDATE OF l,a FOR SHARE OF d,c,sp`, leaseID, attempt, p.DeviceID, p.UserID).Scan(&payload, &manifest, &signer, &admission, &claimProfile, &claimDevice, &capability, &generation, &actualFence, &env, &manifestID, &manifestHash, &contract, &route,
		&hard, &v.issued, &v.expires, &v.leaseState, &v.last, &v.attemptState, &v.jobState, &v.lockPhase, &v.owner, &v.deviceState, &v.sellerState, &pub, &fp, &profile, &v.now, &v.current, &v.progressPhase)
	if e != nil {
		return v, e
	}
	key, ke := devicecontrol.PublicKey(pub)
	if ke != nil || profile != devicecontrol.KeyProfile || fp != outbox.Digest(key) || p.DeviceFingerprint != fp || v.deviceState == "REVOKED" || v.deviceState == "RETIRED" || v.deviceState == "QUARANTINED" || (v.sellerState != "APPROVED" && v.sellerState != "EXITING") {
		return v, fault.New(fault.Forbidden, "Original current device identity required")
	}
	var m Manifest
	if admission != AdmissionProfile || claimProfile != ClaimProfile || claimDevice != p.DeviceID || actualFence != fence || strictjson.Decode(manifest, &m, 16384) != nil || m.Digest() != manifestHash || m.ID != manifestID || m.Attempt != attempt || m.Job != job || m.Order != order || m.Route != route || m.Seller != p.UserID || m.Device != p.DeviceID || m.Resource != resource || m.Capability != capability || m.Generation != generation || m.Fence != actualFence || m.EnvironmentDigest != env || m.ContractHash != contract || m.HardDeadline != hard.UTC().Format(time.RFC3339Nano) {
		return v, fault.New(fault.Conflict, "Original report plan or dual fence mismatch")
	}
	plan := executionlease.Plan{Job: job, Attempt: attempt, Order: order, Seller: p.UserID, Device: p.DeviceID, Resource: resource, Generation: generation, Capability: capability, Manifest: manifestID, ManifestHash: manifestHash, Environment: env, Fence: fence, ContractHash: contract, HardDeadline: hard}
	s.mu.RLock()
	verified, ve := executionlease.Verify(payload, s.verifiers[signer], plan, v.issued)
	s.mu.RUnlock()
	if ve != nil {
		return v, fault.New(fault.Unavailable, "Original historical lease signature unavailable")
	}
	v.lease = verified.Details()
	v.public = key
	if v.lease.ID != leaseID || v.lease.Expires != v.expires.UTC().Format(time.RFC3339Nano) {
		return v, fault.New(fault.Conflict, "Original lease metadata mismatch")
	}
	return v, nil
}
func (v reportAuthority) owns() bool {
	return v.current && v.owner == v.lease.Attempt && (v.lockPhase == "ACTIVE" || v.lockPhase == "STOPPING" || v.lockPhase == "CLEANING" || v.lockPhase == "QUARANTINED")
}

func (s *Claims) report(ctx context.Context, p identity.Principal, attempt, lease, fence, kind, sequence, key string, body []byte, verify func(reportAuthority) error, apply func(pgx.Tx, reportAuthority, devicecontrol.Ack) (any, error)) (ReportResult, error) {
	if s == nil || s.db == nil {
		return ReportResult{}, fault.New(fault.Unavailable, "Execution report service unavailable")
	}
	if !ids.Valid(attempt) || !ids.Valid(lease) || !devicecontrol.ReportCounter(fence) {
		return ReportResult{}, fault.New(fault.Invalid, "Original report identifiers required")
	}
	var v reportAuthority
	op := "recordAttemptProgress"
	if kind == "CLEANUP" {
		op = "recordAttemptCleanup"
	}
	r, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: op, Resource: p.DeviceID + ":" + attempt, Key: key, Request: body}, func(tx pgx.Tx) error {
		var e error
		v, e = s.reportAuthorityTx(ctx, tx, p, attempt, lease, fence)
		if e != nil {
			return e
		}
		return verify(v)
	}, func(tx pgx.Tx) (outbox.StoredResult, error) {
		var oldKey, oldDigest, id string
		var previous []byte
		e := tx.QueryRow(ctx, `SELECT id,key_digest,request_digest,safe_response FROM p11.native_execution_reports WHERE lease_id=$1 AND kind=$2 AND sequence=$3`, lease, kind, sequence).Scan(&id, &oldKey, &oldDigest, &previous)
		if e == nil {
			if oldKey != outbox.Digest([]byte(key)) || oldDigest != outbox.Digest(body) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original report key or facts cannot be replaced")
			}
			return outbox.StoredResult{Reference: id, Body: previous, HTTPStatus: 200, Replayed: true}, nil
		}
		if e != pgx.ErrNoRows {
			return outbox.StoredResult{}, e
		}
		if !v.owns() {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original physical owner is no longer current")
		}
		ack := devicecontrol.Ack{ID: ids.New("report"), Version: 1, Recorded: v.now.UTC()}
		response, e := apply(tx, v, ack)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		b, e := json.Marshal(response)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO p11.native_execution_reports(id,lease_id,attempt_id,job_id,device_id,seller_id,resource_id,fencing_token,resource_lock_generation,kind,sequence,key_digest,request_digest,safe_request,safe_response,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, ack.ID, lease, attempt, v.lease.Job, p.DeviceID, p.UserID, v.lease.Resource, fence, v.lease.Generation, kind, sequence, outbox.Digest([]byte(key)), outbox.Digest(body), body, b, v.now)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		safe, _ := json.Marshal(map[string]string{"report_id": ack.ID, "lease_id": lease, "attempt_id": attempt, "job_id": v.lease.Job, "device_id": p.DeviceID, "resource_id": v.lease.Resource, "fencing_token": fence, "resource_lock_generation": v.lease.Generation, "kind": kind, "sequence": sequence})
		_, e = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'EXECUTION_REPORT',$2,1,'ExecutionReportRecorded','3.0',$3,'PENDING')`, ids.New("evt"), ack.ID, safe)
		return outbox.StoredResult{Reference: ack.ID, Body: b, HTTPStatus: 200}, e
	})
	return ReportResult{Body: r.Body, Replayed: r.Replayed}, e
}
func (s *Claims) RecordProgress(ctx context.Context, p identity.Principal, attempt string, in devicecontrol.ProgressWrite, key string) (ReportResult, error) {
	return s.recordProgress(ctx, p, attempt, in, key, nil)
}

type continueProgress func(context.Context, pgx.Tx, identity.Principal, reportAuthority, devicecontrol.Ack) (*executionlease.Lease, error)

func (s *Claims) recordProgress(ctx context.Context, p identity.Principal, attempt string, in devicecontrol.ProgressWrite, key string, continuation continueProgress) (ReportResult, error) {
	if s == nil || s.db == nil {
		return ReportResult{}, fault.New(fault.Unavailable, "Original report verifier unavailable")
	}
	if in.Validate() != nil {
		return ReportResult{}, fault.New(fault.Invalid, "Bounded progress facts required")
	}
	b, _ := json.Marshal(in)
	return s.report(ctx, p, attempt, in.Lease, in.Fence, "PROGRESS", in.Sequence, key, b, func(reportAuthority) error { return nil }, func(tx pgx.Tx, v reportAuthority, ack devicecontrol.Ack) (any, error) {
		last, e := strconv.ParseUint(v.last, 10, 63)
		if e != nil {
			return nil, e
		}
		seq, _ := strconv.ParseUint(in.Sequence, 10, 63)
		if seq <= last {
			return nil, fault.New(fault.Conflict, "Progress sequence cannot rewind")
		}
		rank := map[string]int{"": 0, "ACKED": 1, "RUNNING": 2, "UPLOADING": 3, "FAILED": 4, "CANCELED": 4}
		if rank[in.Phase] < rank[v.progressPhase] || rank[v.progressPhase] == 4 && in.Phase != v.progressPhase {
			return nil, fault.New(fault.Conflict, "Original progress phase cannot rewind")
		}
		stop := v.jobState != "RUNNING" || v.deviceState != "ONLINE" || v.sellerState != "APPROVED" || v.lockPhase != "ACTIVE" || v.leaseState != "ISSUED" && v.leaseState != "ACKED" || !v.now.Before(v.expires) || in.Phase == "FAILED" || in.Phase == "CANCELED"
		state := in.Phase
		var eligible bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.jobs j JOIN p11.orders o ON o.id=j.order_id
JOIN p11.users u ON u.id=o.buyer_id JOIN p11.api_keys k ON k.id=j.invoker_key_id AND k.user_id=u.id
JOIN p11.route_contracts r ON r.id=j.route_contract_id AND r.order_id=o.id
WHERE j.id=$1 AND u.state='ACTIVE' AND k.revoked_at IS NULL AND k.expires_at>clock_timestamp()
AND 'ROUTE_INVOKE'=ANY(k.scopes) AND r.id=ANY(k.route_ids) AND r.state='ACTIVE' AND r.expires_at>clock_timestamp()
AND EXISTS(SELECT 1 FROM p11.role_grants g WHERE g.user_id=u.id AND g.role_code='BUYER' AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())))`, v.lease.Job).Scan(&eligible); e != nil {
			return nil, e
		}
		stop = stop || !eligible
		if state == "UPLOADING" {
			state = "RUNNING"
		}
		if stop {
			state = v.attemptState
		}
		if (in.Phase == "FAILED" || in.Phase == "CANCELED") && (v.attemptState == "ISSUED" || v.attemptState == "ACKED" || v.attemptState == "RUNNING") {
			state = in.Phase
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.attempts SET state=$2,version=version+1 WHERE id=$1`, attempt, state); e != nil {
			return nil, e
		}
		leaseState := v.leaseState
		if !stop {
			leaseState = "ACKED"
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.execution_leases SET state=$2,last_progress_sequence=$3 WHERE id=$1`, in.Lease, leaseState, in.Sequence); e != nil {
			return nil, e
		}
		action := "NO_CHANGE"
		var next *executionlease.Lease
		if !stop && continuation != nil && (in.Phase == "ACKED" || in.Phase == "RUNNING") {
			next, e = continuation(ctx, tx, p, v, ack)
			if e != nil {
				return nil, e
			}
			if next != nil {
				action = "CONTINUE"
			}
		}
		if stop {
			action = "STOP"
			if _, e = tx.Exec(ctx, `UPDATE p11.resource_locks SET phase='STOPPING',version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1 AND phase='ACTIVE'`, v.lease.Resource); e != nil {
				return nil, e
			}
		}
		ack.Status = "RECORDED"
		return devicecontrol.ProgressAck{Ack: ack, Action: action, Lease: next}, nil
	})
}
func (s *Claims) RecordCleanup(ctx context.Context, p identity.Principal, attempt string, in devicecontrol.CleanupWrite, key string) (ReportResult, error) {
	if s == nil || s.db == nil {
		return ReportResult{}, fault.New(fault.Unavailable, "Original cleanup verifier unavailable")
	}
	if in.Validate() != nil {
		return ReportResult{}, fault.New(fault.Invalid, "Signed original cleanup facts required")
	}
	if in.RecoveryOf != "" {
		return s.recordCleanupRecovery(ctx, p, attempt, in, key)
	}
	b, _ := json.Marshal(in)
	return s.report(ctx, p, attempt, in.Lease, in.Fence, "CLEANUP", "0", key, b, func(v reportAuthority) error {
		m, e := devicecontrol.CleanupMessage(p.DeviceID, attempt, v.lease.Generation, in)
		sig, _ := base64.RawURLEncoding.Strict().DecodeString(in.Signature)
		t, _ := time.Parse(time.RFC3339Nano, in.Recorded)
		if e != nil || !ed25519.Verify(v.public, m, sig) || t.Before(v.issued) || t.After(v.now.Add(devicecontrol.ClockSkew)) {
			return fault.New(fault.Forbidden, "Original cleanup signature or time mismatch")
		}
		return nil
	}, func(tx pgx.Tx, v reportAuthority, ack devicecontrol.Ack) (any, error) {
		state, leaseState, phase, cleanup := "CLEANED", "CLOSED", "FREE", "ACKED"
		if in.State == "FAILED" {
			state, leaseState, phase, cleanup = "QUARANTINED", "REVOKED", "QUARANTINED", "FAILED"
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.attempts SET state=$2,cleanup_state=$3,version=version+1 WHERE id=$1`, attempt, state, cleanup); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.execution_leases SET state=$2 WHERE id=$1`, in.Lease, leaseState); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.jobs SET state=CASE WHEN state='RUNNING' THEN 'UNVERIFIED' ELSE state END,version=version+1 WHERE id=$1`, v.lease.Job); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.transfer_grants SET revoked_at=clock_timestamp() WHERE subject_id=$1 AND revoked_at IS NULL AND asset_id IN(SELECT id FROM p11.assets WHERE job_id=$2)`, p.UserID, v.lease.Job); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.resource_locks SET phase=$2,owner_attempt_id=CASE WHEN $2='FREE' THEN NULL ELSE owner_attempt_id END,expires_at=CASE WHEN $2='FREE' THEN NULL ELSE expires_at END,version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1 AND owner_attempt_id=$3 AND lock_generation=$4`, v.lease.Resource, phase, attempt, v.lease.Generation); e != nil {
			return nil, e
		}
		ack.Status = state
		return ack, nil
	})
}
