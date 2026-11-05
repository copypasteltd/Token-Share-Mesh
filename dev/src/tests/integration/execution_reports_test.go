package integration_test

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/ids"
)

func TestSignedProgressContinuationRetainsOriginalAdmissionAndReplay(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	if _, err := f.claim(); err != nil {
		t.Fatal(err)
	}
	s, err := invocation.NewContinuations(f.claims, f.config.SigningKeyID, f.config.SigningPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l := *f.issued.Lease
	in := devicecontrol.ProgressWrite{Lease: l.ID, Fence: l.Fence, Sequence: "1", Phase: "ACKED"}
	initial, err := s.RecordProgress(ctx, f.p, l.Attempt, in, ids.New("progress"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := devicecontrol.ProgressReceipt(devicecontrol.Reply{Status: 200, Body: initial.Body})
	if err != nil || a.Action != "NO_CHANGE" || a.Lease != nil {
		t.Fatal("initial report unexpectedly extended a full lease", err)
	}
	expires, _ := time.Parse(time.RFC3339Nano, l.Expires)
	hard, _ := time.Parse(time.RFC3339Nano, l.HardDeadline)
	if !hard.After(expires) {
		t.Fatal("fixture requires room before the original hard deadline")
	}
	// Real signed time is deliberately allowed to pass. Do not rewrite an
	// immutable lease or weaken production clocks to make this integration fast.
	if delay := time.Until(expires.Add(-19 * time.Second)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	in.Sequence, in.Phase = "2", "RUNNING"
	key := ids.New("progress")
	first, err := s.RecordProgress(ctx, f.p, l.Attempt, in, key)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := devicecontrol.ProgressReceipt(devicecontrol.Reply{Status: 200, Body: first.Body})
	if err != nil || receipt.Action != "CONTINUE" || receipt.Lease == nil {
		t.Fatal("live original continuation missing", err)
	}
	next := *receipt.Lease
	if next.Revision != 2 || !executionlease.SameOwner(l, next) {
		t.Fatal("continuation replaced original ownership")
	}
	plan := executionlease.Plan{Job: l.Job, Attempt: l.Attempt, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Capability: l.Capability, Manifest: l.Manifest, ManifestHash: l.ManifestHash, Environment: l.Environment, Fence: l.Fence, ContractHash: l.ContractHash, HardDeadline: hard}
	wire, _ := json.Marshal(next)
	if _, err := executionlease.Verify(wire, f.config.SigningPrivateKey.Public().(ed25519.PublicKey), plan, time.Now()); err != nil {
		t.Fatal("continuation failed independent signature verification", err)
	}
	// Database history is bound to the exact response and original budget.
	var atomic bool
	err = db.Pool.QueryRow(ctx, `SELECT c.previous_payload=$2::jsonb AND c.signed_payload=$3::jsonb
AND l.lease_revision=2 AND lk.expires_at=l.expires_at AND lk.owner_attempt_id=a.id AND lk.lock_generation=a.resource_lock_generation
AND b.state='RESERVED' AND j.current_attempt_id=a.id
FROM p11.native_lease_continuations c JOIN p11.execution_leases l ON l.id=c.lease_id
JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id
JOIN p11.resource_locks lk ON lk.resource_id=a.resource_id JOIN p11.budget_reservations b ON b.id=j.reservation_id
WHERE c.lease_id=$1`, l.ID, mustLeaseJSON(t, l), wire).Scan(&atomic)
	if err != nil || !atomic {
		t.Fatal("continuation was not atomic with original ownership and budget", err)
	}
	replayed, err := s.RecordProgress(ctx, f.p, l.Attempt, in, key)
	if err != nil || !replayed.Replayed {
		t.Fatal("lost reply did not recover original receipt", err)
	}
	again, err := devicecontrol.ProgressReceipt(devicecontrol.Reply{Status: 200, Body: replayed.Body})
	if err != nil || again.Lease == nil {
		t.Fatal(err)
	}
	before, _ := executionlease.SignedIdentity(next)
	after, _ := executionlease.SignedIdentity(*again.Lease)
	if before != after {
		t.Fatal("replay issued a different continuation")
	}
	if _, err = s.RecordProgress(ctx, f.p, l.Attempt, in, ids.New("replacement")); err == nil {
		t.Fatal("sequence accepted a replacement original key")
	}
	exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, f.buyerKey.KeyID)
	in.Sequence = "3"
	stopped, err := s.RecordProgress(ctx, f.p, l.Attempt, in, ids.New("stop"))
	if err != nil {
		t.Fatal(err)
	}
	stop, err := devicecontrol.ProgressReceipt(devicecontrol.Reply{Status: 200, Body: stopped.Body})
	if err != nil || stop.Action != "STOP" || stop.Lease != nil {
		t.Fatal("revoked original buyer key renewed", err)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.native_lease_continuations WHERE lease_id=$1`, l.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("replay or stop added continuation", count, err)
	}
}

func mustLeaseJSON(t *testing.T, l executionlease.Lease) []byte {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFailedCleanupRecoveryRetainsFailureAndRevokedLease(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	if _, err := f.claim(); err != nil {
		t.Fatal(err)
	}
	l := *f.issued.Lease
	failed, err := devicecontrol.SignCleanup(f.f.offer.DeviceKey, l, devicecontrol.CleanupWrite{Lease: l.ID, Fence: l.Fence, ManifestHash: outbox.Digest([]byte("synthetic failed cleanup facts")), State: "FAILED", Recorded: time.Now().UTC().Format(time.RFC3339Nano), Profile: devicecontrol.CleanupProfile})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.claims.RecordCleanup(ctx, f.p, l.Attempt, failed, ids.New("failed_cleanup"))
	if err != nil {
		t.Fatal(err)
	}
	ack, err := devicecontrol.CleanupAck(devicecontrol.Reply{Status: 200, Body: result.Body})
	if err != nil || ack.Status != "QUARANTINED" {
		t.Fatal("failure did not retain original quarantine", err)
	}
	// Revoking buyer invocation must not prevent the seller from stopping and
	// cleaning this original occupant. It grants no replacement execution.
	exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, f.buyerKey.KeyID)
	recovery, err := devicecontrol.SignCleanup(f.f.offer.DeviceKey, l, devicecontrol.CleanupWrite{Lease: l.ID, Fence: l.Fence, ManifestHash: outbox.Digest([]byte("synthetic all-three cleanup facts")), State: "ACKED", WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, Recorded: time.Now().UTC().Format(time.RFC3339Nano), Profile: devicecontrol.CleanupRecoveryProfile, RecoveryOf: ack.ID})
	if err != nil {
		t.Fatal(err)
	}
	key := ids.New("recovery")
	got, err := f.claims.RecordCleanup(ctx, f.p, l.Attempt, recovery, key)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := devicecontrol.CleanupAck(devicecontrol.Reply{Status: 200, Body: got.Body})
	if err != nil || clean.Status != "CLEANED" || clean.ID == ack.ID {
		t.Fatal("separate recovery receipt missing", err)
	}
	var preserved bool
	err = db.Pool.QueryRow(ctx, `SELECT r.safe_request->>'state'='FAILED' AND r.safe_response->>'status'='QUARANTINED'
AND c.original_report_id=r.id AND c.safe_request->>'recovery_of'=r.id::text
AND l.state='REVOKED' AND a.state='CLEANED' AND a.cleanup_state='ACKED'
AND lk.phase='FREE' AND lk.owner_attempt_id IS NULL AND lk.lock_generation=a.resource_lock_generation
AND j.state='UNVERIFIED' AND b.state='RESERVED'
FROM p11.native_cleanup_recoveries c JOIN p11.native_execution_reports r ON r.id=c.original_report_id
JOIN p11.execution_leases l ON l.id=c.lease_id JOIN p11.attempts a ON a.id=l.attempt_id
JOIN p11.resource_locks lk ON lk.resource_id=a.resource_id JOIN p11.jobs j ON j.id=a.job_id
JOIN p11.budget_reservations b ON b.id=j.reservation_id WHERE c.id=$1`, clean.ID).Scan(&preserved)
	if err != nil || !preserved {
		t.Fatal("recovery rewrote failure, restored lease or released original budget", err)
	}
	replay, err := f.claims.RecordCleanup(ctx, f.p, l.Attempt, recovery, key)
	if err != nil || !replay.Replayed {
		t.Fatal("recovery original reply not replayed", err)
	}
	if _, err = f.claims.RecordCleanup(ctx, f.p, l.Attempt, recovery, ids.New("different_recovery")); err == nil {
		t.Fatal("original recovery accepted another key")
	}
	changed := recovery
	changed.RecoveryOf = ids.New("different_failure")
	changed, err = devicecontrol.SignCleanup(f.f.offer.DeviceKey, l, changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.claims.RecordCleanup(ctx, f.p, l.Attempt, changed, ids.New("wrong_owner")); err == nil {
		t.Fatal("recovery referenced a different original failure")
	}
}
