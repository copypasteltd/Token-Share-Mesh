package invocation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// Failed cleanup remains immutable. A separate signed successful recovery can
// clear only its original quarantined occupant; it cannot restore its lease,
// re-execute, accept delivery, release a budget or grant billing authority.
func (s *Claims) recordCleanupRecovery(ctx context.Context, p identity.Principal, attempt string, in devicecontrol.CleanupWrite, key string) (ReportResult, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return ReportResult{}, err
	}
	var owner reportAuthority
	r, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "recordAttemptCleanup", Resource: p.DeviceID + ":" + attempt, Key: key, Request: body}, func(tx pgx.Tx) error {
		var err error
		owner, err = s.reportAuthorityTx(ctx, tx, p, attempt, in.Lease, in.Fence)
		if err != nil {
			return err
		}
		message, err := devicecontrol.CleanupMessage(p.DeviceID, attempt, owner.lease.Generation, in)
		sig, _ := base64.RawURLEncoding.Strict().DecodeString(in.Signature)
		recorded, _ := time.Parse(time.RFC3339Nano, in.Recorded)
		if err != nil || !ed25519.Verify(owner.public, message, sig) || recorded.Before(owner.issued) || recorded.After(owner.now.Add(devicecontrol.ClockSkew)) {
			return fault.New(fault.Forbidden, "Original cleanup recovery signature or time mismatch")
		}
		var original time.Time
		err = tx.QueryRow(ctx, `SELECT recorded_at FROM p11.native_execution_reports WHERE id=$1 AND lease_id=$2 AND attempt_id=$3 AND job_id=$4 AND device_id=$5 AND seller_id=$6 AND resource_id=$7 AND fencing_token=$8 AND resource_lock_generation=$9 AND kind='CLEANUP' AND sequence=0 AND safe_request->>'state'='FAILED' AND safe_response->>'status'='QUARANTINED'`, in.RecoveryOf, in.Lease, attempt, owner.lease.Job, p.DeviceID, p.UserID, owner.lease.Resource, in.Fence, owner.lease.Generation).Scan(&original)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Conflict, "Original failed cleanup receipt required")
		}
		if err != nil {
			return err
		}
		if recorded.Before(original) {
			return fault.New(fault.Forbidden, "Cleanup recovery cannot predate its original failure")
		}
		return nil
	}, func(tx pgx.Tx) (outbox.StoredResult, error) {
		var id, digest, request string
		var previous []byte
		err := tx.QueryRow(ctx, `SELECT id,key_digest,request_digest,safe_response FROM p11.native_cleanup_recoveries WHERE original_report_id=$1`, in.RecoveryOf).Scan(&id, &digest, &request, &previous)
		if err == nil {
			if digest != outbox.Digest([]byte(key)) || request != outbox.Digest(body) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original recovery key and signed facts cannot change")
			}
			return outbox.StoredResult{Reference: id, Body: previous, HTTPStatus: 200, Replayed: true}, nil
		}
		if err != pgx.ErrNoRows {
			return outbox.StoredResult{}, err
		}
		if !owner.owns() || owner.lockPhase != "QUARANTINED" || owner.attemptState != "QUARANTINED" || owner.leaseState != "REVOKED" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Only the original failed-cleanup occupant can recover")
		}
		ack := devicecontrol.Ack{ID: ids.New("recovery"), Status: "CLEANED", Version: 1, Recorded: owner.now.UTC()}
		response, err := json.Marshal(ack)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO p11.native_cleanup_recoveries(id,original_report_id,lease_id,attempt_id,job_id,device_id,seller_id,resource_id,fencing_token,resource_lock_generation,key_digest,request_digest,cleanup_manifest_hash,safe_request,safe_response,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, ack.ID, in.RecoveryOf, in.Lease, attempt, owner.lease.Job, p.DeviceID, p.UserID, owner.lease.Resource, in.Fence, owner.lease.Generation, outbox.Digest([]byte(key)), outbox.Digest(body), in.ManifestHash, body, response, owner.now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		updated, err := tx.Exec(ctx, `UPDATE p11.attempts SET state='CLEANED',cleanup_state='ACKED',version=version+1 WHERE id=$1 AND state='QUARANTINED' AND cleanup_state='FAILED'`, attempt)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if updated.RowsAffected() != 1 {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original failed cleanup state changed")
		}
		// Keep the lease REVOKED permanently. Recovery supplies no future time.
		updated, err = tx.Exec(ctx, `UPDATE p11.resource_locks SET phase='FREE',owner_attempt_id=NULL,expires_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1 AND phase='QUARANTINED' AND owner_attempt_id=$2 AND owner_session_id IS NULL AND lock_generation=$3`, owner.lease.Resource, attempt, owner.lease.Generation)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if updated.RowsAffected() != 1 {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original quarantine cannot clear a different owner")
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.transfer_grants SET revoked_at=clock_timestamp() WHERE subject_id=$1 AND revoked_at IS NULL AND asset_id IN(SELECT id FROM p11.assets WHERE job_id=$2)`, p.UserID, owner.lease.Job); err != nil {
			return outbox.StoredResult{}, err
		}
		safe, _ := json.Marshal(map[string]string{"recovery_id": ack.ID, "original_report_id": in.RecoveryOf, "lease_id": in.Lease, "attempt_id": attempt, "job_id": owner.lease.Job, "device_id": p.DeviceID, "resource_id": owner.lease.Resource, "fencing_token": in.Fence, "resource_lock_generation": owner.lease.Generation})
		_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'EXECUTION_CLEANUP_RECOVERY',$2,1,'ExecutionCleanupRecovered','3.0',$3,'PENDING')`, ids.New("evt"), ack.ID, safe)
		return outbox.StoredResult{Reference: ack.ID, Body: response, HTTPStatus: 200}, err
	})
	return ReportResult{Body: r.Body, Replayed: r.Replayed}, err
}
