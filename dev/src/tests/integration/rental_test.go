package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/rental"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// Every approval/channel fact here is synthetic data in the disposable native
// test database. These tests cannot qualify a seller, phone, GPU or payment.
type rentalFixture struct {
	s         *rental.Service
	f         testsupport.Offer
	p, device identity.Principal
	in        rental.CreateWrite
}

func newRentalFixture(t *testing.T, state, environment string) rentalFixture {
	t.Helper()
	requireDB(t)
	f := testsupport.SeedNativeRentalOffer(t, ctx, db, ids.New("region"), "openai_nb1")
	p := buyer(t)
	p.Scopes = append(p.Scopes, "rental:create", "rental:read", "rental:ready", "rental:stop", "rental:access")
	q, err := (trade.Service{DB: db}).CreateQuote(ctx, p, f.Input(), ids.New("quote"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := (trade.Service{DB: db}).CreateOrder(ctx, p, confirm(q), ids.New("order"))
	if err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1`, o.ID)
	exec(t, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,channel_reference,
product_profile_revision,channel_binding_snapshot,amount_fen,state)
VALUES($1,$2,'ORDER',$2,'ALIPAY',$3,$4,$4,'SYNTHETIC_NOT_CHANNEL_EVIDENCE','{"fixture":"NOT_CHANNEL_EVIDENCE"}',500,$5)`, ids.New("pay"), o.ID, environment, ids.New("synthetic"), state)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	s, err := rental.NewService(rental.Config{DB: db, PaymentEnvironment: "SANDBOX", Issuer: "rental_test_issuer", KeyID: "rental_test_key", SigningKey: key, IssuerPins: map[string]ed25519.PublicKey{"rental_test_key": pub}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	device := identity.Principal{UserID: f.Seller.UserID, Kind: identity.Device, DeviceID: f.Device, DeviceFingerprint: outbox.Digest(f.DeviceKey.Public().(ed25519.PublicKey)), Scopes: []string{"device:rental"}, Version: 1}
	return rentalFixture{s: s, f: f, p: p, device: device, in: rental.CreateWrite{Order: o.ID, OrderVersion: o.Version + 1, Device: f.Device, Environment: f.Terms.Bindings[0].EnvironmentRef, MaximumMS: q.Contract.Spec.DurationMS, Budget: q.Contract.BudgetFen, Disconnect: "STOP_AFTER_GRACE"}}
}

func (f rentalFixture) reserve(t *testing.T) rental.View {
	t.Helper()
	v, err := f.s.Create(ctx, f.p, f.in, ids.New("reserve"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f rentalFixture) proofs(t *testing.T, id, actor, kind string) (string, string) {
	t.Helper()
	reviewer := user(t)
	exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'OPERATOR','self',$2)`, ids.New("grant"), reviewer.UserID)
	var raw []byte
	var nonce, generation, manifest string
	if err := db.Pool.QueryRow(ctx, `SELECT s.authority_snapshot,c.connection_ref,s.resource_lock_generation::text,c.manifest_hash
FROM p11.rental_sessions s JOIN p11.rental_authority_contexts c ON c.session_id=s.id WHERE s.id=$1`, id).Scan(&raw, &nonce, &generation, &manifest); err != nil {
		t.Fatal(err)
	}
	var session rental.Session
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal(map[string]string{"profile_id": "P11_RENTAL_READINESS_EVIDENCE_1", "session_id": id, "order_id": f.in.Order, "buyer_id": f.p.UserID, "seller_id": f.f.Seller.UserID, "device_id": f.f.Device, "resource_id": f.f.Resource, "resource_lock_generation": generation, "environment_revision": f.in.Environment, "kernel_environment_digest": session.Contract.EnvironmentHash, "manifest_hash": manifest, "connection_ref": nonce, "actor_id": actor})
	connection, ready := ids.New("evidence"), ids.New("evidence")
	for _, p := range []struct{ id, kind string }{{connection, "RENTAL_CONNECTION"}, {ready, kind}} {
		exec(t, `INSERT INTO p11.evidence(id,evidence_type,seller_id,device_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until)
VALUES($1,$2,$3,$4,'SYNTHETIC_NOT_PHYSICAL_EVIDENCE',$5,$6,'APPROVED',$7,clock_timestamp()-interval '1 second',clock_timestamp()+interval '1 hour')`, p.id, p.kind, f.f.Seller.UserID, f.f.Device, outbox.Digest(append(append([]byte(nil), scope...), []byte(p.id)...)), scope, reviewer.UserID)
	}
	return connection, ready
}

func (f rentalFixture) activate(t *testing.T) rental.View {
	t.Helper()
	v := f.reserve(t)
	prep, err := f.s.ClaimPreparation(ctx, f.device, v.ID, rental.PreparationWrite{Expected: v.Version}, ids.New("prepare"))
	if err != nil {
		t.Fatal(err)
	}
	c, r := f.proofs(t, v.ID, f.f.Seller.UserID, "RENTAL_DEVICE_READY")
	ready, err := f.s.DeviceReady(ctx, f.device, v.ID, rental.DeviceReadyWrite{Expected: prep.Session.Version, Environment: f.in.Environment, Generation: prep.Generation, Connection: c, Readiness: r}, ids.New("deviceReady"))
	if err != nil {
		t.Fatal(err)
	}
	c, r = f.proofs(t, v.ID, f.p.UserID, "RENTAL_BUYER_READY")
	active, err := f.s.ConfirmReady(ctx, f.p, v.ID, rental.ReadyWrite{Expected: ready.Version, Environment: f.in.Environment, Connection: c, Readiness: r, Budget: f.in.Budget}, ids.New("buyerReady"))
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func TestRentalReservationPersistsHourlySnapshotOwnerBudgetAndSafeReplay(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	key := ids.New("reserve")
	v, err := f.s.Create(ctx, f.p, f.in, key)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != "RESERVED" || v.Started != nil || v.HardDeadline != nil || v.Charge != nil || v.VerifiedMS != nil {
		t.Fatal("reservation invented work or accounting")
	}
	replay, err := f.s.Create(ctx, f.p, f.in, key)
	if err != nil || !replay.Replayed || replay.ID != v.ID {
		t.Fatal("original reservation replay changed owner", err)
	}
	var owner, phase, generation string
	var minute *int64
	var price []byte
	var held int64
	if err = db.Pool.QueryRow(ctx, `SELECT l.owner_session_id,l.phase,l.lock_generation::text,s.rate_fen_per_minute,s.hourly_price_snapshot,b.amount_fen
FROM p11.rental_sessions s JOIN p11.resource_locks l ON l.resource_id=s.resource_id
JOIN p11.budget_reservations b ON b.purpose_ref=s.id AND b.order_id=s.order_id WHERE s.id=$1`, v.ID).Scan(&owner, &phase, &generation, &minute, &price, &held); err != nil {
		t.Fatal(err)
	}
	if owner != v.ID || phase != "RESERVED" || generation != "1" || minute != nil || held != int64(f.in.Budget) || !bytes.Contains(price, []byte("AVAILABLE_HOUR")) {
		t.Fatal("atomic original rental reservation differs")
	}
	if _, err = f.s.Create(ctx, f.p, f.in, ids.New("second")); err == nil {
		t.Fatal("second session allocated for same order")
	}
	foreign := buyer(t)
	foreign.Scopes = append(foreign.Scopes, "rental:read")
	if _, err = f.s.Get(ctx, foreign, v.ID); err == nil {
		t.Fatal("foreign buyer read session")
	}
}

func TestRentalReservationRejectsUnresolvedFundsAndForeignBindingWithoutLeaks(t *testing.T) {
	for _, tc := range []struct {
		name, state, environment string
		foreign                  bool
	}{{"unknown", "UNKNOWN", "SANDBOX", false}, {"unsettled", "AUTHORIZED", "SANDBOX", false}, {"otherEnvironment", "SETTLED", "PRODUCTION", false}, {"foreignDevice", "SETTLED", "SANDBOX", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRentalFixture(t, tc.state, tc.environment)
			if tc.foreign {
				f.in.Device = ids.New("foreign")
			}
			if _, err := f.s.Create(ctx, f.p, f.in, ids.New("reserve")); err == nil {
				t.Fatal("unresolved funding or foreign device admitted")
			}
			var count int
			var phase string
			if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.rental_sessions WHERE order_id=$1`, f.in.Order).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if err := db.Pool.QueryRow(ctx, `SELECT phase FROM p11.resource_locks WHERE resource_id=$1`, f.f.Resource).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			if count != 0 || phase != "FREE" {
				t.Fatal("rejected rental leaked reservation")
			}
		})
	}
}

func TestRentalReadinessNeedsIndependentEvidenceAndUniqueBuyerStart(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	v := f.reserve(t)
	key := ids.New("prepare")
	prep, err := f.s.ClaimPreparation(ctx, f.device, v.ID, rental.PreparationWrite{Expected: v.Version}, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(prep.ManifestBase64)
	if err != nil || outbox.Digest(raw) != prep.ManifestHash {
		t.Fatal("manifest raw bytes changed")
	}
	again, err := f.s.ClaimPreparation(ctx, f.device, v.ID, rental.PreparationWrite{Expected: v.Version}, key)
	if err != nil || !again.Replayed || !again.PrepareUntil.Equal(prep.PrepareUntil) {
		t.Fatal("replayed preparation extended TTL", err)
	}
	write := rental.DeviceReadyWrite{Expected: prep.Session.Version, Environment: f.in.Environment, Generation: prep.Generation, Connection: ids.New("evidence"), Readiness: ids.New("evidence")}
	if _, err = f.s.DeviceReady(ctx, f.device, v.ID, write, ids.New("ready")); err == nil {
		t.Fatal("unapproved device claim became ready")
	}
	write.Connection, write.Readiness = f.proofs(t, v.ID, f.f.Seller.UserID, "RENTAL_DEVICE_READY")
	ready, err := f.s.DeviceReady(ctx, f.device, v.ID, write, ids.New("ready"))
	if err != nil {
		t.Fatal(err)
	}
	c, r := f.proofs(t, v.ID, f.p.UserID, "RENTAL_BUYER_READY")
	buyerWrite := rental.ReadyWrite{Expected: ready.Version, Environment: f.in.Environment, Connection: c, Readiness: r, Budget: f.in.Budget - 1}
	if _, err = f.s.ConfirmReady(ctx, f.p, v.ID, buyerWrite, ids.New("start")); err == nil {
		t.Fatal("different buyer budget started clock")
	}
	buyerWrite.Budget = f.in.Budget
	startKey := ids.New("start")
	active, err := f.s.ConfirmReady(ctx, f.p, v.ID, buyerWrite, startKey)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != "ACTIVE" || active.Started == nil || active.HardDeadline == nil || !active.HardDeadline.After(*active.Started) {
		t.Fatal("double readiness did not start unique paid span")
	}
	if _, err = f.s.ConfirmReady(ctx, f.p, v.ID, buyerWrite, ids.New("start")); err == nil {
		t.Fatal("stale ready created a second start")
	}
	replay, err := f.s.ConfirmReady(ctx, f.p, v.ID, buyerWrite, startKey)
	if err != nil || !replay.Replayed || !replay.Started.Equal(*active.Started) {
		t.Fatal("ready replay changed original start", err)
	}
}

func TestRentalGrantReplayReplacementAndStopRetainPhysicalOwner(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in := rental.AccessWrite{Expected: active.Version, Scopes: []string{"INVOKE", "EXPORT_SELF"}}
	key := ids.New("access")
	first, err := f.s.CreateAccess(ctx, f.p, active.ID, in, key)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	safe, err := json.Marshal(first)
	if err != nil || bytes.Contains(safe, []byte("credential_once")) {
		t.Fatal("ordinary access serialization exposed credential")
	}
	var body bytes.Buffer
	if err = first.WriteJSONOnce(&body); err != nil {
		t.Fatal(err)
	}
	var credential struct {
		Token string `json:"credential_once"`
	}
	if json.Unmarshal(body.Bytes(), &credential) != nil || credential.Token == "" {
		t.Fatal("missing first credential")
	}
	var cached []byte
	if err = db.Pool.QueryRow(ctx, `SELECT safe_response FROM p11.idempotency_records WHERE operation_id='createRentalAccessGrant' AND resource_scope=$1 AND key_digest=$2`, active.ID, outbox.Digest([]byte(key))).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cached, []byte(credential.Token)) || bytes.Contains(cached, []byte("credential_once")) {
		t.Fatal("durable idempotency record stored bearer")
	}
	replay, err := f.s.CreateAccess(ctx, f.p, active.ID, in, key)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	var replayBody bytes.Buffer
	if !replay.Replayed || replay.WriteJSONOnce(&replayBody) != nil || bytes.Contains(replayBody.Bytes(), []byte("credential_once")) {
		t.Fatal("original key replayed bearer")
	}
	in.Expected = first.Metadata().SessionVersion
	replacement, err := f.s.CreateAccess(ctx, f.p, active.ID, in, ids.New("replace"))
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	var oldRevoked bool
	if err = db.Pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM p11.rental_access_grants WHERE id=$1`, first.Metadata().Grant).Scan(&oldRevoked); err != nil || !oldRevoked {
		t.Fatal("replacement retained old grant", err)
	}
	// ACTIVE -> ACTIVE is otherwise legal. No matching history/event must make
	// the deferred commit fail atomically rather than silently accepting a row.
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE p11.rental_sessions SET version=version+1,
authority_snapshot=jsonb_set(authority_snapshot,'{version}',to_jsonb(version+1)) WHERE id=$1`, active.ID)
		return e
	})
	if err == nil {
		t.Fatal("authority update committed without append-only event/history")
	}
	if _, err = db.Pool.Exec(ctx, `DELETE FROM p11.resource_locks WHERE resource_id=$1`, f.f.Resource); err == nil {
		t.Fatal("active physical owner was deleted")
	}
	// A buyer can request stop after qualification loss; stop revokes access,
	// freezes the upper bound and retains owner/full capacity for real cleanup.
	exec(t, `UPDATE p11.capabilities SET state='REVOKED',version=version+1 WHERE id=$1`, f.f.Capability)
	stopped, err := f.s.Stop(ctx, f.p, active.ID, rental.StopWrite{Expected: replacement.Metadata().SessionVersion, Reason: "BUYER_STOP"}, ids.New("stop"))
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != "STOPPING" || stopped.Cleanup != "PENDING" {
		t.Fatal("stop invented cleanup completion")
	}
	var owner, phase string
	var live int
	if err = db.Pool.QueryRow(ctx, `SELECT owner_session_id,phase FROM p11.resource_locks WHERE resource_id=$1`, f.f.Resource).Scan(&owner, &phase); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.rental_access_grants WHERE session_id=$1 AND revoked_at IS NULL`, active.ID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if owner != active.ID || phase != "STOPPING" || live != 0 {
		t.Fatal("stop released physical owner or retained access")
	}
	_, err = f.s.CreateAccess(ctx, f.p, active.ID, rental.AccessWrite{Expected: stopped.Version, Scopes: []string{"INVOKE"}}, ids.New("access"))
	assertCode(t, err, fault.Conflict)
	var stopRaw []byte
	if err = db.Pool.QueryRow(ctx, `SELECT authority_snapshot FROM p11.rental_sessions WHERE id=$1`, active.ID).Scan(&stopRaw); err != nil {
		t.Fatal(err)
	}
	var value rental.Session
	if json.Unmarshal(stopRaw, &value) != nil || value.Stopped.IsZero() || value.Stopped.After(time.Now().UTC()) {
		t.Fatal("stop did not freeze original metering upper bound")
	}
}
