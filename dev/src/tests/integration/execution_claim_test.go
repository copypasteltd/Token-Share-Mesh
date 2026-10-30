package integration_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

type claimFixture struct {
	f        routeFixture
	p        identity.Principal
	buyerKey identity.Principal
	in       devicecontrol.LeaseClaim
	issued   invocation.Issuance
	claims   *invocation.Claims
	issuer   *invocation.Execution
	request  invocation.Request
	config   invocation.ExecutionConfig
}

func newClaimFixture(t *testing.T) claimFixture {
	t.Helper()
	f, p, r, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	readyExecution(t, s, p, r, body)
	issued := issueExecution(t, s, p, r)
	claims, err := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{c.SigningKeyID: c.SigningPrivateKey.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(claims.Close)
	in := devicecontrol.LeaseClaim{Device: f.offer.Device, Capability: f.offer.Capability, ClaimKey: ids.New("claim"), Version: 1}
	b, _ := json.Marshal(in)
	binding := devicecontrol.Binding{Method: "POST", Authority: "127.0.0.1:8787", URI: "/v1/p11/runner/leases/claim", ContentType: "application/json", IdempotencyKey: in.ClaimKey}
	proof, err := devicecontrol.Sign(f.offer.DeviceKey, in.Device, binding, b, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dp, err := (device.Service{DB: db}).Authenticate(ctx, proof, binding, b)
	if err != nil {
		t.Fatal(err)
	}
	return claimFixture{f, dp, p, in, issued, claims, s, r, c}
}

func (f claimFixture) claim() (invocation.ClaimResult, error) {
	return f.claims.Claim(ctx, f.p, f.in, f.in.ClaimKey)
}

func assertClaimHold(t *testing.T, f claimFixture, claims, events int) {
	t.Helper()
	var c, e int
	var original bool
	err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.execution_leases WHERE id=$1 AND claim_profile=$2),
(SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='ExecutionLeaseClaimed'),
(l.state='ISSUED' AND a.state='ISSUED' AND lk.phase='ACTIVE' AND lk.owner_attempt_id=a.id AND b.state='RESERVED'
 AND j.current_attempt_id=a.id AND lk.lock_generation=a.resource_lock_generation
 AND EXISTS(SELECT 1 FROM p11.durable_jobs d WHERE d.kind='DELIVER_EXECUTION_LEASE' AND d.target_ref=l.id AND d.state='PENDING'))
FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id JOIN p11.resource_locks lk ON lk.resource_id=a.resource_id
JOIN p11.budget_reservations b ON b.id=j.reservation_id WHERE l.id=$1`, f.issued.LeaseID, invocation.ClaimProfile).Scan(&c, &e, &original)
	if err != nil || c != claims || e != events || !original {
		t.Fatal("claim is not ACK, usage, task completion or release", c, e, original, err)
	}
}

func TestExecutionClaimSignedDeviceGetsExactOriginalLeaseWithoutDecryptor(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	f.issuer.Close() // claim has public verification keys only, not the closed issuer/content keys.
	v, err := f.claim()
	if err != nil || v.Replayed || v.Lease.ID != f.issued.LeaseID || v.Lease.Signature != f.issued.Lease.Signature || v.Lease.Expires != f.issued.Lease.Expires {
		t.Fatal(v, err)
	}
	got, _ := json.Marshal(v.Lease)
	want, _ := json.Marshal(f.issued.Lease)
	if !bytes.Equal(got, want) {
		t.Fatal("claim changed original signed payload")
	}
	if bytes.Contains(got, []byte(syntheticInput)) {
		t.Fatal("claim disclosed prompt")
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimEightConcurrentRecoveriesPersistOneBindingAndEvent(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	var wg sync.WaitGroup
	results := make(chan invocation.ClaimResult, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := f.claim(); results <- v; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	fresh := 0
	for v := range results {
		if !v.Replayed {
			fresh++
		}
		if v.Lease.Signature != f.issued.Lease.Signature {
			t.Fatal("resigned")
		}
	}
	if fresh != 1 {
		t.Fatal(fresh)
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimOriginalKeyCannotSelectAnotherBodyOrClaim(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	if _, e := f.claim(); e != nil {
		t.Fatal(e)
	}
	changed := f.in
	changed.Capability = ids.New("cap")
	if _, e := f.claims.Claim(ctx, f.p, changed, changed.ClaimKey); fault.Public(e).Code != fault.Conflict {
		t.Fatal("changed original body", e)
	}
	changed = f.in
	changed.ClaimKey = ids.New("different")
	if _, e := f.claims.Claim(ctx, f.p, changed, changed.ClaimKey); fault.Public(e).Code != fault.Conflict {
		t.Fatal("different claim got same lease", e)
	}
	if _, e := f.claims.Claim(ctx, f.p, f.in, ids.New("header")); fault.Public(e).Code != fault.Invalid {
		t.Fatal("header mismatch", e)
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimRejectsSessionOtherDeviceAndMissingExecuteScope(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	for _, p := range []identity.Principal{f.f.offer.Seller, f.buyerKey, {UserID: f.p.UserID, Kind: identity.Device, DeviceID: ids.New("other"), DeviceFingerprint: f.p.DeviceFingerprint, Version: 1, Scopes: []string{"device:execute"}}} {
		if _, e := f.claims.Claim(ctx, p, f.in, f.in.ClaimKey); e == nil {
			t.Fatal("wrong actor accepted")
		}
	}
	p := f.p
	p.Scopes = []string{"device:read"}
	if _, e := f.claims.Claim(ctx, p, f.in, f.in.ClaimKey); fault.Public(e).Code != fault.Forbidden {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 0, 0)
}

func TestExecutionClaimCurrentAuthorityAlsoAppliesToReplay(t *testing.T) {
	requireDB(t)
	cases := []struct {
		name, sql string
		arg       func(claimFixture) string
	}{
		{"device-revoke", `UPDATE p11.devices SET state='REVOKED',version=version+1 WHERE id=$1`, func(f claimFixture) string { return f.in.Device }},
		{"device-version", `UPDATE p11.devices SET version=version+1 WHERE id=$1`, func(f claimFixture) string { return f.in.Device }},
		{"seller-role", `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='SELLER'`, func(f claimFixture) string { return f.p.UserID }},
		{"buyer-key", `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, func(f claimFixture) string { return f.buyerKey.KeyID }},
		{"buyer-role", `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, func(f claimFixture) string { return f.buyerKey.UserID }},
		{"funds-shortened", `UPDATE p11.payments SET funds_expires_at=clock_timestamp()+interval '1 second' WHERE order_id=$1`, func(f claimFixture) string { return f.f.order.ID }},
		{"funding", `UPDATE p11.payments SET state='UNKNOWN' WHERE order_id=$1`, func(f claimFixture) string { return f.f.order.ID }},
		{"qualification", `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, func(f claimFixture) string { return f.f.offer.Gate }},
		{"input", `UPDATE p11.assets SET state='QUARANTINED',version=version+1 WHERE job_id=$1`, func(f claimFixture) string { return f.issued.Preparation.JobID }},
		{"lease", `UPDATE p11.execution_leases SET state='REVOKED' WHERE id=$1`, func(f claimFixture) string { return f.issued.LeaseID }},
	}
	for _, c := range cases {
		for _, replay := range []bool{false, true} {
			t.Run(c.name+map[bool]string{false: "/first", true: "/replay"}[replay], func(t *testing.T) {
				f := newClaimFixture(t)
				if replay {
					if _, e := f.claim(); e != nil {
						t.Fatal(e)
					}
				}
				exec(t, c.sql, c.arg(f))
				if v, e := f.claim(); e == nil || v.Lease.ID != "" {
					t.Fatal("current authority not enforced", v, e)
				}
				var count int
				if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='ExecutionLeaseClaimed'`, f.issued.LeaseID).Scan(&count); e != nil || count != map[bool]int{true: 1, false: 0}[replay] {
					t.Fatal(count, e)
				}
			})
		}
	}
}

func TestExecutionClaimResourceAndBudgetCannotBeBorrowed(t *testing.T) {
	requireDB(t)
	for _, mutation := range []string{
		`UPDATE p11.resource_locks SET phase='QUARANTINED' WHERE resource_id=$1`,
		`UPDATE p11.resource_locks SET lock_generation=lock_generation+1 WHERE resource_id=$1`,
	} {
		f := newClaimFixture(t)
		exec(t, mutation, f.f.offer.Resource)
		if _, e := f.claim(); e == nil {
			t.Fatal("changed original resource accepted")
		}
	}
	// The ordinary budget API cannot zero-release without a trusted qualification;
	// claim never fabricates one and always leaves the full reserved hold intact.
	f := newClaimFixture(t)
	if _, e := f.claim(); e != nil {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimVerifierCopiesRetainsHistoryAndCloses(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	pub := append(ed25519.PublicKey(nil), f.config.SigningPrivateKey.Public().(ed25519.PublicKey)...)
	s, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{f.config.SigningKeyID: pub})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	clear(pub)
	if _, e = s.Claim(ctx, f.p, f.in, f.in.ClaimKey); e != nil {
		t.Fatal("caller mutated verifier copy", e)
	}
	s.Close()
	if _, e = s.Claim(ctx, f.p, f.in, f.in.ClaimKey); fault.Public(e).Code != fault.Unavailable {
		t.Fatal("closed verifier", e)
	}
	missing, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{"another_issuer": f.config.SigningPrivateKey.Public().(ed25519.PublicKey)})
	if e != nil {
		t.Fatal(e)
	}
	defer missing.Close()
	if _, e = missing.Claim(ctx, f.p, f.in, f.in.ClaimKey); fault.Public(e).Code != fault.Unavailable {
		t.Fatal("missing original pin", e)
	}
	for _, keys := range []map[string]ed25519.PublicKey{nil, {"bad": {1}}} {
		if _, e = invocation.NewClaims(db, "SANDBOX", keys); e == nil {
			t.Fatal("bad keys accepted")
		}
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimNoCurrentLeaseNeverCreatesAttemptOrFreesResource(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	in := f.in
	in.Capability = ids.New("missing")
	if _, e := f.claims.Claim(ctx, f.p, in, in.ClaimKey); fault.Public(e).Code != fault.NotFound {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 0, 0)
}

func TestExecutionClaimSQLRequiresSafeEventAndRetainsOriginalCommand(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	body, _ := json.Marshal(f.in)
	_, err := db.Pool.Exec(ctx, `UPDATE p11.execution_leases SET claim_profile=$2,claim_device_id=$3,claim_key_digest=$4,claim_request_digest=$5,claim_device_version=1,claimed_at=clock_timestamp() WHERE id=$1`, f.issued.LeaseID, invocation.ClaimProfile, f.in.Device, outbox.Digest([]byte(f.in.ClaimKey)), outbox.Digest(body))
	if err == nil {
		t.Fatal("claim committed without safe event")
	}
	assertClaimHold(t, f, 0, 0)
	if _, err = f.claim(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE p11.execution_leases SET claim_key_digest=repeat('a',64) WHERE id=$1`, `UPDATE p11.execution_leases SET claim_device_version=2 WHERE id=$1`, `UPDATE p11.execution_leases SET claimed_at=claimed_at+interval '1 second' WHERE id=$1`, `DELETE FROM p11.execution_leases WHERE id=$1`} {
		if _, e := db.Pool.Exec(ctx, q, f.issued.LeaseID); e == nil {
			t.Fatal("original claim rewritten", q)
		}
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionClaimExpiryDoesNotReturnOldLeaseOrReleaseHold(t *testing.T) {
	requireDB(t)
	f, p, r, body := executionFixture(t, relay.ChatText)
	exec(t, `UPDATE p11.api_keys SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, p.KeyID)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	readyExecution(t, s, p, r, body)
	issued := issueExecution(t, s, p, r)
	claims, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{c.SigningKeyID: c.SigningPrivateKey.Public().(ed25519.PublicKey)})
	if e != nil {
		t.Fatal(e)
	}
	defer claims.Close()
	dp := identity.Principal{UserID: f.offer.Seller.UserID, Kind: identity.Device, DeviceID: f.offer.Device, DeviceFingerprint: outbox.Digest(f.offer.DeviceKey.Public().(ed25519.PublicKey)), Version: 1, Scopes: []string{"device:execute"}}
	in := devicecontrol.LeaseClaim{Device: f.offer.Device, Capability: f.offer.Capability, ClaimKey: ids.New("claim"), Version: 1}
	cf := claimFixture{f: f, p: dp, in: in, issued: issued, claims: claims}
	if _, e = cf.claim(); e != nil {
		t.Fatal(e)
	}
	exp, _ := time.Parse(time.RFC3339Nano, issued.Lease.Expires)
	time.Sleep(time.Until(exp) + 10*time.Millisecond)
	if v, e := cf.claim(); e == nil || v.Lease.ID != "" {
		t.Fatal("expired original delivered", v, e)
	}
	assertClaimHold(t, cf, 1, 1)
}

func TestExecutionClaimReturnedBytesStillNeedIndependentPinAndPlan(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	v, e := f.claim()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v.Lease)
	b, e := devicecontrol.UnverifiedClaim(devicecontrol.Reply{Status: 200, Body: raw})
	if e != nil {
		t.Fatal(e)
	}
	pub := f.config.SigningPrivateKey.Public().(ed25519.PublicKey)
	plan := f.issued.Manifest.ApprovedPlan() // independently authenticated fixture SQL/order, not copied from the HTTP response.
	if _, e = executionlease.Verify(b, pub, plan, time.Now()); e != nil {
		t.Fatal(e)
	}
	plan.Order = ids.New("wrong_order")
	if _, e = executionlease.Verify(b, pub, plan, time.Now()); e == nil {
		t.Fatal("unapproved plan accepted")
	}
	if _, e = devicecontrol.UnverifiedClaim(devicecontrol.Reply{Status: 503, Body: raw}); e == nil {
		t.Fatal("error treated as lease")
	}
}

func TestExecutionClaimWaitsForOriginalOrderBeforeDeviceOrSellerLocks(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	local, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, e := db.Pool.Begin(local)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.WithoutCancel(local))
	var holder int
	if e = tx.QueryRow(local, `SELECT pg_backend_pid() FROM p11.orders WHERE id=$1 FOR UPDATE`, f.f.order.ID).Scan(&holder); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, err := f.claims.Claim(local, f.p, f.in, f.in.ClaimKey); done <- err }()
	blocked := false
	for local.Err() == nil {
		if e = db.Pool.QueryRow(local, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)) AND a.query LIKE 'SELECT id FROM p11.orders WHERE id=%')`, holder).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("claim did not reach original order lock")
	}
	// A writer can take seller/device while the claim waits for its order. This
	// exercises real PostgreSQL locks, including the queued-writer inversion.
	var id string
	if e = tx.QueryRow(local, `SELECT id FROM p11.seller_profiles WHERE id=$1 FOR UPDATE`, f.p.UserID).Scan(&id); e != nil {
		t.Fatal("claim held seller before order", e)
	}
	if e = tx.QueryRow(local, `SELECT id FROM p11.devices WHERE id=$1 FOR UPDATE`, f.in.Device).Scan(&id); e != nil {
		t.Fatal("claim held device before order", e)
	}
	if e = tx.Commit(local); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 1, 1)

	// The same device/command also serializes BEFORE candidate lookup. A
	// concurrent committed version change must be observed after that wait.
	g := newClaimFixture(t)
	second, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	guard, e := db.Pool.Begin(second)
	if e != nil {
		t.Fatal(e)
	}
	defer guard.Rollback(context.WithoutCancel(second))
	command := "P11_CLAIM_COMMAND_1:" + g.p.DeviceID + ":" + outbox.Digest([]byte(g.in.ClaimKey))
	if _, e = guard.Exec(second, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, command); e != nil {
		t.Fatal(e)
	}
	if e = guard.QueryRow(second, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
		t.Fatal(e)
	}
	waiting := make(chan error, 1)
	go func() { _, err := g.claims.Claim(second, g.p, g.in, g.in.ClaimKey); waiting <- err }()
	blocked = false
	for second.Err() == nil {
		if e = db.Pool.QueryRow(second, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`, holder).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("original command was not serialized")
	}
	if _, e = guard.Exec(second, `UPDATE p11.devices SET version=version+1 WHERE id=$1`, g.in.Device); e != nil {
		t.Fatal(e)
	}
	if e = guard.Commit(second); e != nil {
		t.Fatal(e)
	}
	if e = <-waiting; fault.Public(e).Code != fault.Conflict {
		t.Fatal("claim ignored concurrent committed version", e)
	}
	assertClaimHold(t, g, 0, 0)
}

func TestExecutionClaimUsesOwnReservationAfterNewCallCapacityIsExhausted(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	for i := 0; i < 4; i++ {
		r, body := executionBody(relay.ChatText, ids.New("call"))
		readyExecution(t, f.issuer, f.buyerKey, r, body)
	}
	if _, e := f.claim(); e != nil {
		t.Fatal("original full reservation mistaken for new-call capacity", e)
	}
	assertClaimHold(t, f, 1, 1)
}
