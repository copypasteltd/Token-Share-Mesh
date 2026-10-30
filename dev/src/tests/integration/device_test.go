package integration_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"sync"
	"testing"
	"time"
)

func devices() device.Service {
	return device.Service{DB: db, PepperVersion: "pair_test_v1", Peppers: map[string][]byte{"pair_test_v1": []byte(strings.Repeat("p", 32))}}
}
func deviceOwner(t *testing.T) identity.Principal {
	t.Helper()
	p := sellerActor(offer(t, "GPU_RENTAL", ids.New("reg")))
	p.Scopes = append(p.Scopes, "device:pair", "device:read", "device:manage")
	return p
}
func pair(t *testing.T, p identity.Principal, platform string) device.Pairing {
	t.Helper()
	v, e := devices().CreatePairing(ctx, p, device.PairInput{Platform: platform, Label: "我自愿共享的闲置设备", Confirmed: true}, ids.New("pair_intent"))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func enrollment(t *testing.T, pairID string, android bool) (device.Enrollment, ed25519.PrivateKey) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	kind, model := "GPU_DEVICE", "RTX 4090 24GB"
	if android {
		kind, model = "ANDROID_DEVICE", "摩托罗拉 X30"
	}
	return device.Enrollment{PairingID: pairID, PublicKey: base64.RawURLEncoding.EncodeToString(pub), KeyProfile: devicecontrol.KeyProfile, OSBuild: "SYNTHETIC_TEST", ClientRelease: "SYNTHETIC_TEST", Fingerprint: strings.Repeat("f", 64), Kind: kind, Model: model}, key
}
func enrolled(t *testing.T, p identity.Principal, platform string) (device.View, ed25519.PrivateKey) {
	t.Helper()
	v := pair(t, p, platform)
	in, key := enrollment(t, v.ID, platform == "ANDROID")
	d, e := devices().Enroll(ctx, v.Token, in, ids.New("enroll_intent"))
	if e != nil {
		t.Fatal(e)
	}
	return d, key
}
func signedDevice(t *testing.T, v device.View, key ed25519.PrivateKey) (identity.Principal, devicecontrol.Binding, []byte) {
	t.Helper()
	body := []byte(`{"sequence":"1"}`)
	b := devicecontrol.Binding{Method: "POST", Authority: "127.0.0.1:8787", URI: "/v1/p11/devices/" + v.ID + "/heartbeats", IdempotencyKey: "original_heartbeat", ContentType: "application/json"}
	token, e := devicecontrol.Sign(key, v.ID, b, body, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	p, e := devices().Authenticate(ctx, token, b, body)
	if e != nil {
		t.Fatal(e)
	}
	return p, b, body
}
func policy(version int64) device.Policy {
	enabled := true
	disk := int64(1024)
	return device.Policy{Version: version, Enabled: &enabled, FreeDisk: &disk, Slots: 1, Windows: []device.Window{{From: time.Now().UTC().Add(-time.Minute), Until: time.Now().UTC().Add(time.Hour)}}, Stop: "DRAIN"}
}

func TestPairingSecretOnceAndConcurrentEnrollmentOriginalRecovery(t *testing.T) {
	requireDB(t)
	p := deviceOwner(t)
	svc := devices()
	input := device.PairInput{Platform: "WINDOWS", Label: "我的 RTX 4090", Confirmed: true}
	var wg sync.WaitGroup
	values := make(chan device.Pairing, 16)
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := svc.CreatePairing(ctx, p, input, "one_pairing_intent")
			values <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(values)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first device.Pairing
	secrets := 0
	for v := range values {
		if v.Token != "" {
			first = v
			secrets++
		} else if !v.Replayed {
			t.Fatal("replayed pairing missing marker")
		}
	}
	if secrets != 1 {
		t.Fatal("one-time secret redisclosed", secrets)
	}
	in, _ := enrollment(t, first.ID, false)
	results := make(chan device.View, 16)
	errs = make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := svc.Enroll(ctx, first.Token, in, "original_enrollment")
			results <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var id string
	fresh := 0
	for v := range results {
		if id != "" && id != v.ID {
			t.Fatal("duplicate registration")
		}
		id = v.ID
		if !v.Replayed {
			fresh++
		}
		if v.State != "PAIRED" || v.LastSeen != nil || v.ActiveSlots != 0 || len(v.Capabilities) != 0 || strings.Contains(string(v.Policy), `"sharing_enabled": true`) {
			t.Fatal("registration granted execution", v)
		}
	}
	if fresh != 1 {
		t.Fatal("wrong first-commit count", fresh)
	}
	var rows int
	var safe string
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.devices WHERE seller_id=$1 AND identity_profile='ED25519_DEVICE_1'`, p.UserID).Scan(&rows); e != nil || rows != 1 {
		t.Fatal(e, rows)
	}
	if e := db.Pool.QueryRow(ctx, `SELECT string_agg(row_to_json(i)::text,'') FROM p11.idempotency_records i WHERE principal_id=$1`, p.UserID).Scan(&safe); e != nil || strings.Contains(safe, first.Token) {
		t.Fatal("pairing credential persisted", e)
	}
	_, e := svc.Enroll(ctx, first.Token, in, "changed_enrollment_key")
	assertCode(t, e, fault.Unauthenticated)
	in.Model = "RTX 3080"
	_, e = svc.Enroll(ctx, first.Token, in, "original_enrollment")
	assertCode(t, e, fault.Unauthenticated)
	second := pair(t, p, "WINDOWS")
	in.PairingID = second.ID
	_, e = svc.Enroll(ctx, second.Token, in, "duplicate_identity_key")
	assertCode(t, e, fault.Conflict)
	if _, e = db.Pool.Exec(ctx, `UPDATE p11.devices SET device_public_key=repeat('A',43) WHERE id=$1`, id); e == nil {
		t.Fatal("device identity rotated in place")
	}
}
func TestPairingLimitsExpiryFailedAttemptsAndCurrentSellerAuthority(t *testing.T) {
	requireDB(t)
	p := deviceOwner(t)
	svc := devices()
	first := pair(t, p, "WINDOWS")
	expired := pair(t, p, "LINUX")
	pair(t, p, "ANDROID")
	_, e := svc.CreatePairing(ctx, p, device.PairInput{Platform: "WINDOWS", Label: "fourth", Confirmed: true}, "fourth_open_pairing")
	assertCode(t, e, fault.Conflict)
	in, _ := enrollment(t, first.ID, false)
	wrong := strings.TrimSuffix(first.Token, strings.Split(first.Token, ".")[2]) + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for range 5 {
		_, e = svc.Enroll(ctx, wrong, in, "failed_pairing_intent")
		assertCode(t, e, fault.Unauthenticated)
	}
	var failed int
	if e = db.Pool.QueryRow(ctx, `SELECT failed_attempts FROM p11.pairing_intents WHERE id=$1`, first.ID).Scan(&failed); e != nil || failed != 5 {
		t.Fatal("failed attempts rolled back", failed, e)
	}
	_, e = svc.Enroll(ctx, first.Token, in, "correct_after_lockout")
	assertCode(t, e, fault.Unauthenticated)
	exec(t, `UPDATE p11.pairing_intents SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.ID)
	in, _ = enrollment(t, expired.ID, false)
	_, e = svc.Enroll(ctx, expired.Token, in, "expired_pairing_intent")
	assertCode(t, e, fault.Unauthenticated)
	current := pair(t, p, "WINDOWS")
	in, _ = enrollment(t, current.ID, false)
	in.Model = "Android 设备"
	_, e = svc.Enroll(ctx, current.Token, in, "generic_model_rejected")
	assertCode(t, e, fault.Invalid)
	in.Model = "RTX 3080"
	exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='SELLER'`, p.UserID)
	_, e = svc.Enroll(ctx, current.Token, in, "revoked_pairing_owner")
	assertCode(t, e, fault.Unauthenticated)
	_, e = svc.CreatePairing(ctx, p, device.PairInput{Platform: "WINDOWS", Label: "owner revoked", Confirmed: true}, "revoked_owner_cannot_pair")
	assertCode(t, e, fault.Forbidden)
	buyerOnly := buyer(t)
	buyerOnly.Scopes = append(buyerOnly.Scopes, "device:pair")
	_, e = svc.CreatePairing(ctx, buyerOnly, device.PairInput{Platform: "WINDOWS", Label: "unapproved", Confirmed: true}, "buyer_cannot_pair")
	assertCode(t, e, fault.Forbidden)
}
func TestDeviceProofCurrentIdentityDurableNonceAndTamperRejection(t *testing.T) {
	requireDB(t)
	owner := deviceOwner(t)
	v, key := enrolled(t, owner, "WINDOWS")
	_, b, body := signedDevice(t, v, key)
	token, _ := devicecontrol.Sign(key, v.ID, b, body, time.Now())
	for _, field := range []string{"body", "host", "path", "query", "key", "etag", "contenttype"} {
		bad := b
		raw := body
		switch field {
		case "body":
			raw = []byte(`{"sequence":"2"}`)
		case "host":
			bad.Authority = "localhost:8787"
		case "path":
			bad.URI = "/v1/p11/devices/dev_other/heartbeats"
		case "query":
			bad.URI += "?x=1"
		case "key":
			bad.IdempotencyKey = "new_intent"
		case "etag":
			bad.IfMatch = `"1"`
		case "contenttype":
			bad.ContentType = "text/plain"
		}
		_, e := devices().Authenticate(ctx, token, bad, raw)
		assertCode(t, e, fault.Unauthenticated)
	}
	p, e := devices().Authenticate(ctx, token, b, body)
	if e != nil || p.Kind != identity.Device || p.DeviceID != v.ID {
		t.Fatal("original signature failed", e)
	}
	_, e = devices().Authenticate(ctx, token, b, body)
	assertCode(t, e, fault.Unauthenticated) // Fresh service object; nonce is not process memory.
	expired, _ := devicecontrol.Sign(key, v.ID, b, body, time.Now().Add(-61*time.Second))
	_, e = devices().Authenticate(ctx, expired, b, body)
	assertCode(t, e, fault.Unauthenticated)
	other, otherKey := enrolled(t, owner, "LINUX")
	forged, _ := devicecontrol.Sign(otherKey, v.ID, b, body, time.Now())
	_, e = devices().Authenticate(ctx, forged, b, body)
	assertCode(t, e, fault.Unauthenticated)
	_, e = devices().Get(ctx, p, other.ID)
	assertCode(t, e, fault.NotFound)
	exec(t, `UPDATE p11.users SET state='SUSPENDED',version=version+1 WHERE id=$1`, owner.UserID)
	fresh, _ := devicecontrol.Sign(key, v.ID, b, body, time.Now())
	_, e = devices().Authenticate(ctx, fresh, b, body)
	assertCode(t, e, fault.Unauthenticated)
}
func TestDeviceHeartbeatMonotonicConcurrencyAndNoAutomaticQualification(t *testing.T) {
	requireDB(t)
	owner := deviceOwner(t)
	v, key := enrolled(t, owner, "ANDROID")
	p, _, _ := signedDevice(t, v, key)
	in := device.Heartbeat{Sequence: "0", Version: 1, Recorded: time.Now().UTC(), State: "IDLE"}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	acks := make(chan device.Ack, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := devices().Heartbeat(ctx, p, v.ID, in, "same_heartbeat_intent")
			acks <- a
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	close(acks)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first time.Time
	fresh := 0
	for a := range acks {
		if first.IsZero() {
			first = a.Recorded
		}
		if !a.Recorded.Equal(first) {
			t.Fatal("replay changed server time")
		}
		if !a.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal("duplicate liveness mutation", fresh)
	}
	_, e := devices().Heartbeat(ctx, p, v.ID, in, "new_key_same_sequence")
	assertCode(t, e, fault.Conflict)
	in.Sequence = "18446744073709551615"
	a, e := devices().Heartbeat(ctx, p, v.ID, in, "full_uint64_sequence")
	if e != nil || a.Version != 1 {
		t.Fatal("full uint64 failed", e)
	}
	in.Sequence = "18446744073709551616"
	_, e = devices().Heartbeat(ctx, p, v.ID, in, "overflow_sequence")
	assertCode(t, e, fault.Invalid)
	in.Sequence = "01"
	_, e = devices().Heartbeat(ctx, p, v.ID, in, "leading_zero_sequence")
	assertCode(t, e, fault.Invalid)
	in.Sequence = "1"
	in.Version = 2
	_, e = devices().Heartbeat(ctx, p, v.ID, in, "stale_device_policy")
	assertCode(t, e, fault.Conflict)
	in.Version = 1
	in.Recorded = time.Now().Add(6 * time.Minute)
	_, e = devices().Heartbeat(ctx, p, v.ID, in, "future_device_clock")
	assertCode(t, e, fault.Invalid)
	in.Recorded = time.Now()
	in.Attempt = ids.New("foreign_attempt")
	in.Fence = "1"
	_, e = devices().Heartbeat(ctx, p, v.ID, in, "invented_active_attempt")
	assertCode(t, e, fault.Conflict)
	_, e = devices().Heartbeat(ctx, owner, v.ID, in, "session_cannot_heartbeat")
	assertCode(t, e, fault.Forbidden)
	after, e := devices().Get(ctx, owner, v.ID)
	if e != nil || after.State != "PAIRED" || len(after.Capabilities) != 0 || after.LastSeen == nil || after.HeartbeatSequence == nil || *after.HeartbeatSequence != "18446744073709551615" || after.ReportedState != "IDLE" {
		t.Fatal("heartbeat qualified device", after, e)
	}
	if _, e = db.Pool.Exec(ctx, `UPDATE p11.devices SET heartbeat_sequence=1 WHERE id=$1`, v.ID); e == nil {
		t.Fatal("counter rollback permitted")
	}
}
func TestDeviceResourcePolicyVersionsSchedulesAndPhoneConditions(t *testing.T) {
	requireDB(t)
	owner := deviceOwner(t)
	v, _ := enrolled(t, owner, "WINDOWS")
	in := policy(1)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := devices().UpdatePolicy(ctx, owner, v.ID, 1, in, fmt.Sprintf("policy_competing_%d", i))
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	wins := 0
	for e := range errs {
		if e == nil {
			wins++
		} else {
			assertCode(t, e, fault.Conflict)
		}
	}
	if wins != 1 {
		t.Fatal("multiple policy revisions won", wins)
	}
	after, e := devices().Get(ctx, owner, v.ID)
	if e != nil || after.Version != 2 || after.State != "PROBING" || strings.Contains(string(after.Policy), `"accepting_new_jobs": true`) {
		t.Fatal("policy edit granted qualification", e, after)
	}
	other := deviceOwner(t)
	_, e = devices().Get(ctx, other, v.ID)
	assertCode(t, e, fault.NotFound)
	_, e = devices().UpdatePolicy(ctx, other, v.ID, 2, policy(2), "foreign_device_policy")
	assertCode(t, e, fault.NotFound)
	bad := policy(2)
	bad.Windows = append(bad.Windows, bad.Windows[0])
	_, e = devices().UpdatePolicy(ctx, owner, v.ID, 2, bad, "overlapping_device_windows")
	assertCode(t, e, fault.Invalid)
	bad = policy(2)
	bad.Slots = 2
	_, e = devices().UpdatePolicy(ctx, owner, v.ID, 2, bad, "multiple_gpu_slots")
	assertCode(t, e, fault.Invalid)
	phone, _ := enrolled(t, owner, "ANDROID")
	phonePolicy := policy(1)
	_, e = devices().UpdatePolicy(ctx, owner, phone.ID, 1, phonePolicy, "missing_phone_conditions")
	assertCode(t, e, fault.Invalid)
	battery, temp, charge := 30, 45.0, true
	phonePolicy.Battery = &battery
	phonePolicy.Temperature = &temp
	phonePolicy.Charging = &charge
	a, e := devices().UpdatePolicy(ctx, owner, phone.ID, 1, phonePolicy, "explicit_phone_conditions")
	if e != nil || a.Status != "PROBING" {
		t.Fatal("phone policy", e)
	}
	page, e := devices().List(ctx, owner, "", 1)
	if e != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("device pagination", e)
	}
}
func TestDeviceDrainRevokePreserveFencesAndInvalidateCredential(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	owner := sellerActor(f)
	owner.Scopes = append(owner.Scopes, "device:manage", "device:read", "device:pair")
	// Explicit synthetic in-flight ownership; no payment or GPU execution is implied.
	bp := buyer(t)
	q, e := (trade.Service{DB: db}).CreateQuote(ctx, bp, f.input(), "before_device_drain_quote")
	if e != nil {
		t.Fatal(e)
	}
	o, e := (trade.Service{DB: db}).CreateOrder(ctx, bp, confirm(q), "before_device_drain_order")
	if e != nil {
		t.Fatal(e)
	}
	job, attempt := ids.New("job"), ids.New("attempt")
	exec(t, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,fencing_token,deadline) VALUES($1,$2,$3,'SYNTHETIC_TEST',repeat('d',64),'RUNNING',1,clock_timestamp()+interval '1 hour')`, job, o.ID, owner.UserID)
	exec(t, `INSERT INTO p11.attempts(id,job_id,order_id,seller_id,device_id,resource_id,capability_id,fencing_token,resource_lock_generation,state) VALUES($1,$2,$3,$4,$5,$6,$7,1,77,'QUARANTINED')`, attempt, job, o.ID, owner.UserID, f.device, f.resource, f.capability)
	exec(t, `UPDATE p11.jobs SET current_attempt_id=$2 WHERE id=$1`, job, attempt)
	exec(t, `INSERT INTO p11.resource_locks(resource_id,phase,lock_generation,owner_attempt_id) VALUES($1,'QUARANTINED',77,$2)`, f.resource, attempt)
	exec(t, `UPDATE p11.devices SET state='QUARANTINED' WHERE id=$1`, f.device)
	v, e := devices().ChangeState(ctx, owner, f.device, device.Reason{Code: "owner_stops", Version: 1}, "drain_quarantined_device", false)
	if e != nil || v.State != "QUARANTINED" {
		t.Fatal("drain removed quarantine", e, v)
	}
	_, e = devices().UpdatePolicy(ctx, owner, f.device, 2, policy(2), "cannot_clear_quarantine")
	assertCode(t, e, fault.Conflict)
	v, e = devices().ChangeState(ctx, owner, f.device, device.Reason{Code: "owner_revokes", Version: 2}, "revoke_quarantined_device", true)
	if e != nil || v.State != "REVOKED" {
		t.Fatal(e, v)
	}
	var generation int64
	var phase, lockOwner string
	if e = db.Pool.QueryRow(ctx, `SELECT phase,lock_generation,owner_attempt_id FROM p11.resource_locks WHERE resource_id=$1`, f.resource).Scan(&phase, &generation, &lockOwner); e != nil || phase != "QUARANTINED" || generation != 77 || lockOwner != attempt {
		t.Fatal("unsafe lock release", e, phase, generation)
	}
	if _, e = db.Pool.Exec(ctx, `UPDATE p11.devices SET state='ONLINE' WHERE id=$1`, f.device); e == nil {
		t.Fatal("revoked identity resurrected")
	}
	fresh, key := enrolled(t, owner, "LINUX")
	p, b, body := signedDevice(t, fresh, key)
	in := device.Heartbeat{Sequence: "1", Version: 1, Recorded: time.Now(), State: "IDLE"}
	if _, e = devices().Heartbeat(ctx, p, fresh.ID, in, "before_revoke_heartbeat"); e != nil {
		t.Fatal(e)
	}
	if _, e = devices().ChangeState(ctx, owner, fresh.ID, device.Reason{Code: "owner_revokes", Version: 1}, "revoke_current_identity", true); e != nil {
		t.Fatal(e)
	}
	token, _ := devicecontrol.Sign(key, fresh.ID, b, body, time.Now())
	_, e = devices().Authenticate(ctx, token, b, body)
	assertCode(t, e, fault.Unauthenticated)
	_, e = devices().Heartbeat(ctx, p, fresh.ID, in, "before_revoke_heartbeat")
	assertCode(t, e, fault.Unauthenticated)
}
func TestDeviceReportedHealthStopsOfferAdmissionWithoutGrantingQualification(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	for _, state := range []string{"HOT", "LOW_BATTERY", "LOW_DISK", "BUSY", "DRAINING", "IDLE"} {
		exec(t, `UPDATE p11.devices SET reported_resource_state=$2,last_seen_at=clock_timestamp() WHERE id=$1`, f.device, state)
		v, e := catalogItem(t, f)
		if e != nil {
			t.Fatal(e)
		}
		if (v.Availability == "AVAILABLE") != (state == "IDLE") {
			t.Fatal("unsafe health admission", state, v.Availability)
		}
	}
	exec(t, `UPDATE p11.devices SET last_seen_at=clock_timestamp()-interval '46 seconds' WHERE id=$1`, f.device)
	v, e := catalogItem(t, f)
	if e != nil || v.Availability == "AVAILABLE" {
		t.Fatal("expired heartbeat sold", e)
	}
	raw, _ := json.Marshal(v)
	if len(raw) == 0 {
		t.Fatal("missing safe market view")
	}
}
