package integration_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
)

func invocationService(t *testing.T) *invocation.Service {
	t.Helper()
	return invocationKeys(t, "fp_one", map[string][]byte{"fp_one": []byte(strings.Repeat("f", 32))})
}

func invocationKeys(t *testing.T, version string, keys map[string][]byte) *invocation.Service {
	t.Helper()
	s, err := invocation.New(invocation.Config{DB: db, PaymentEnvironment: "SANDBOX", FingerprintVersion: version, FingerprintKeys: keys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func invocationRequest(protocol relay.Profile, key string) invocation.Request {
	return invocation.Request{Model: "model_nb1", Protocol: protocol, RequestDigest: outbox.Digest([]byte("synthetic plaintext request; NOT CUSTOMER CONTENT")), IdempotencyKey: key, MaxOutputTokens: 16}
}

func invocationCounts(t *testing.T, f routeFixture, jobs, events int, reserved int64) {
	t.Helper()
	var n, ev int
	var amount int64
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.jobs WHERE order_id=$1 AND invocation_profile='ROUTE_PREPARE_1'`, f.order.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_type='ROUTE_INVOCATION' AND safe_payload->>'order_id'=$1`, f.order.ID).Scan(&ev); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT budget_reserved_fen FROM p11.orders WHERE id=$1`, f.order.ID).Scan(&amount); err != nil || n != jobs || ev != events || amount != reserved {
		t.Fatal("atomic original invocation counts", n, ev, amount, err)
	}
}

func TestInvocationPreparationAtomicAcrossThreeNativeProfiles(t *testing.T) {
	requireDB(t)
	for _, protocol := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		t.Run(string(protocol), func(t *testing.T) {
			f := routeOrder(t, buyer(t), protocol)
			c := f.create(t)
			_, _, p := routeKey(t, f.buyer, c.ID)
			r := invocationRequest(protocol, "native-original-call")
			v, err := invocationService(t).Prepare(ctx, p, r)
			if err != nil || v.State != "INPUT_PENDING" || v.Replayed || v.UsedCalls != 1 || v.ReservedFen != 100 || v.RemainingFen != 400 || v.BudgetState != "RESERVED" || !v.Deadline.Before(c.Definition.ExpiresAt) {
				t.Fatal(v, err)
			}
			invocationCounts(t, f, 1, 1, 100)
			var stored, spec string
			var attempts int
			if err = db.Pool.QueryRow(ctx, `SELECT request_digest,invocation_spec::text,(SELECT count(*) FROM p11.attempts WHERE job_id=j.id) FROM p11.jobs j WHERE id=$1`, v.JobID).Scan(&stored, &spec, &attempts); err != nil || stored == r.RequestDigest || strings.Contains(spec, r.RequestDigest) || attempts != 0 {
				t.Fatal("raw digest/content or execution authority persisted", stored, spec, attempts, err)
			}
			current, err := routeService().Get(ctx, f.buyer, c.ID)
			if err != nil || current.UsedCalls != 1 || current.RemainingFen != 400 || current.State != "ACTIVE" {
				t.Fatal(current, err)
			}
		})
	}
}

func TestInvocationConcurrentOriginalRetryCommitsOnePreparation(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s := invocationService(t)
	type outcome struct {
		value invocation.Preparation
		err   error
	}
	results := make(chan outcome, 12)
	for range 12 {
		go func() {
			v, err := s.Prepare(ctx, p, invocationRequest(relay.ChatText, "same-original-request"))
			results <- outcome{v, err}
		}()
	}
	id, fresh := "", 0
	for range 12 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id == "" {
			id = r.value.JobID
		}
		if r.value.JobID != id {
			t.Fatal("original request was replaced")
		}
		if !r.value.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal("new preparations", fresh)
	}
	invocationCounts(t, f, 1, 1, 100)
}

func TestInvocationConcurrentCallsCannotExceedCountOrRestoreCanceledSlot(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	f.in.MaxCalls = 2
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s := invocationService(t)
	results := make(chan error, 10)
	for range 10 {
		go func() {
			_, err := s.Prepare(ctx, p, invocationRequest(relay.ChatText, ids.New("original")))
			results <- err
		}()
	}
	success := 0
	for range 10 {
		err := <-results
		if err == nil {
			success++
		} else {
			assertCode(t, err, fault.Conflict)
		}
	}
	if success != 2 {
		t.Fatal(success)
	}
	invocationCounts(t, f, 2, 2, 200)
	exec(t, `UPDATE p11.jobs SET state='CANCELED',version=version+1 WHERE order_id=$1`, f.order.ID)
	_, err := s.Prepare(ctx, p, invocationRequest(relay.ChatText, "after-cancellation"))
	assertCode(t, err, fault.Conflict)
	current, err := routeService().Get(ctx, f.buyer, c.ID)
	if err != nil || current.UsedCalls != 2 || current.State != "SUSPENDED" {
		t.Fatal(current, err)
	}
	invocationCounts(t, f, 2, 2, 200)
}

func TestInvocationDistinctCallsShareOriginalOrderCapacity(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	f.in.PerCallFen, f.in.MaxCalls = 300, 10
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s := invocationService(t)
	results := make(chan error, 8)
	for range 8 {
		go func() {
			_, err := s.Prepare(ctx, p, invocationRequest(relay.ChatText, ids.New("original")))
			results <- err
		}()
	}
	success := 0
	for range 8 {
		err := <-results
		if err == nil {
			success++
		} else {
			assertCode(t, err, fault.Conflict)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	invocationCounts(t, f, 1, 1, 300)
}

func TestInvocationChangedInputOrBoundKeyCannotReplaceOriginal(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s, in := invocationService(t), invocationRequest(relay.ChatText, "fixed-original-input")
	if _, err := s.Prepare(ctx, p, in); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*invocation.Request){func(r *invocation.Request) { r.RequestDigest = strings.Repeat("a", 64) }, func(r *invocation.Request) { r.Stream = true }, func(r *invocation.Request) { r.MaxOutputTokens++ }} {
		changed := in
		mutate(&changed)
		_, err := s.Prepare(ctx, p, changed)
		assertCode(t, err, fault.Conflict)
	}
	_, _, other := routeKey(t, f.buyer, c.ID)
	_, err := s.Prepare(ctx, other, in)
	assertCode(t, err, fault.Conflict)
	invocationCounts(t, f, 1, 1, 100)
}

func TestInvocationOriginalRecoveryAfterRouteClosureRetainsHold(t *testing.T) {
	requireDB(t)
	for _, condition := range []string{"expired", "refund", "canceled", "device"} {
		t.Run(condition, func(t *testing.T) {
			f := routeOrder(t, buyer(t), relay.ChatText)
			c := f.create(t)
			_, _, p := routeKey(t, f.buyer, c.ID)
			s, in := invocationService(t), invocationRequest(relay.ChatText, "original-recovery-call")
			v, err := s.Prepare(ctx, p, in)
			if err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "expired":
				exec(t, `UPDATE p11.route_contracts SET state='EXPIRED',version=version+1 WHERE id=$1`, c.ID)
			case "refund":
				exec(t, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state) VALUES($1,$2,$3,$4,1,'UNKNOWN')`, ids.New("ref"), f.in.PaymentID, f.order.ID, ids.New("request"))
			case "canceled":
				exec(t, `UPDATE p11.jobs SET state='CANCELED',version=version+1 WHERE id=$1`, v.JobID)
				exec(t, `UPDATE p11.orders SET state='CLOSED',version=version+1 WHERE id=$1`, f.order.ID)
			case "device":
				exec(t, `UPDATE p11.devices SET state='REVOKED',version=version+1 WHERE id=$1`, f.offer.Device)
			}
			recovered, err := s.Prepare(ctx, p, in)
			if err != nil || !recovered.Replayed || recovered.JobID != v.JobID || recovered.BudgetState != "RESERVED" {
				t.Fatal(recovered, err)
			}
			in.IdempotencyKey = "another-new-request"
			_, err = s.Prepare(ctx, p, in)
			assertCode(t, err, fault.Conflict)
			invocationCounts(t, f, 1, 1, 100)
		})
	}
}

func TestInvocationCurrentIdentityBlocksNewAndOriginalReplay(t *testing.T) {
	requireDB(t)
	for _, condition := range []string{"key", "role", "user", "scope"} {
		t.Run(condition, func(t *testing.T) {
			f := routeOrder(t, buyer(t), relay.ChatText)
			c := f.create(t)
			_, key, p := routeKey(t, f.buyer, c.ID)
			s, in := invocationService(t), invocationRequest(relay.ChatText, "current-authority-call")
			if _, err := s.Prepare(ctx, p, in); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "key":
				exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, key.Key.ID)
			case "role":
				exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, p.UserID)
			case "user":
				exec(t, `UPDATE p11.users SET version=version+1 WHERE id=$1`, p.UserID)
			case "scope":
				exec(t, `UPDATE p11.api_keys SET route_ids='{}' WHERE id=$1`, key.Key.ID)
			}
			if _, err := s.Prepare(ctx, p, in); err == nil {
				t.Fatal("revoked authority replayed")
			}
			in.IdempotencyKey = "new-after-revocation"
			if _, err := s.Prepare(ctx, p, in); err == nil {
				t.Fatal("revoked authority prepared")
			}
			invocationCounts(t, f, 1, 1, 100)
		})
	}
}

func TestInvocationRejectsInvalidUnfundedOrUncoveredRequests(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s, in := invocationService(t), invocationRequest(relay.ChatText, "reject-invalid-call")
	for _, mutate := range []func(*invocation.Request){func(r *invocation.Request) { r.RequestDigest = "bad" }, func(r *invocation.Request) { r.IdempotencyKey = "" }, func(r *invocation.Request) { r.IdempotencyKey = "invalid\nkey" }, func(r *invocation.Request) { r.Protocol = "openai_nb1" }, func(r *invocation.Request) { r.MaxOutputTokens = 32769 }, func(r *invocation.Request) { r.MaxOutputTokens = 0 }} {
		bad := in
		mutate(&bad)
		_, err := s.Prepare(ctx, p, bad)
		assertCode(t, err, fault.Invalid)
	}
	if _, err := s.Prepare(ctx, f.buyer, in); err == nil {
		t.Fatal("session acted as scoped SDK key")
	}
	exec(t, `UPDATE p11.payments SET state='UNKNOWN',version=version+1 WHERE id=$1`, f.in.PaymentID)
	_, err := s.Prepare(ctx, p, in)
	assertCode(t, err, fault.Conflict)
	invocationCounts(t, f, 0, 0, 0)
	g := routeOrder(t, buyer(t), relay.ChatText)
	g.in.PerCallFen = 1
	other := g.create(t)
	_, _, actor := routeKey(t, g.buyer, other.ID)
	_, err = s.Prepare(ctx, actor, in)
	assertCode(t, err, fault.Conflict)
	invocationCounts(t, g, 0, 0, 0)
}

func TestInvocationFingerprintRotationAndMissingOldKeyFailClosed(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	in := invocationRequest(relay.ChatText, "rotate-original-call")
	old := invocationService(t)
	v, err := old.Prepare(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string][]byte{"fp_one": []byte(strings.Repeat("f", 32)), "fp_two": []byte(strings.Repeat("g", 32))}
	rotated := invocationKeys(t, "fp_two", keys)
	clear(keys["fp_one"])
	clear(keys["fp_two"])
	replay, err := rotated.Prepare(ctx, p, in)
	if err != nil || !replay.Replayed || replay.JobID != v.JobID {
		t.Fatal(replay, err)
	}
	missing := invocationKeys(t, "fp_two", map[string][]byte{"fp_two": []byte(strings.Repeat("g", 32))})
	_, err = missing.Prepare(ctx, p, in)
	assertCode(t, err, fault.Unavailable)
	rotated.Close()
	_, err = rotated.Prepare(ctx, p, in)
	assertCode(t, err, fault.Unavailable)
	invocationCounts(t, f, 1, 1, 100)
}

func TestInvocationRollbackAndMissingEventCannotCommitPartialPreparation(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	s := invocationService(t)
	marker := errors.New("fixture transaction rollback")
	err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if _, err := s.PrepareTx(ctx, tx, p, invocationRequest(relay.ChatText, "rollback-original-call")); err != nil {
			return err
		}
		return marker
	})
	if !errors.Is(err, marker) {
		t.Fatal(err)
	}
	invocationCounts(t, f, 0, 0, 0)
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		v, err := s.PrepareTx(ctx, tx, p, invocationRequest(relay.ChatText, "missing-event-call"))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM p11.outbox_events WHERE aggregate_type='ROUTE_INVOCATION' AND aggregate_id=$1`, v.JobID)
		return err
	})
	if err == nil {
		t.Fatal("partial job/budget without outbox committed")
	}
	invocationCounts(t, f, 0, 0, 0)
}

func TestInvocationSQLBindingsCannotMutateDeleteOrRetrofit(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	v, err := invocationService(t).Prepare(ctx, p, invocationRequest(relay.ChatText, "immutable-call"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE p11.jobs SET request_digest=repeat('a',64) WHERE id=$1`, `UPDATE p11.jobs SET caller_key='changed' WHERE id=$1`, `UPDATE p11.jobs SET deadline=deadline+interval '1 second' WHERE id=$1`, `UPDATE p11.jobs SET reservation_id='bgt_changed' WHERE id=$1`, `UPDATE p11.jobs SET invocation_profile='UNBOUND_JOB',route_contract_id=NULL,invoker_key_id=NULL,fingerprint_version=NULL,invocation_spec=NULL,reservation_id=NULL WHERE id=$1`, `DELETE FROM p11.jobs WHERE id=$1`} {
		if _, err = db.Pool.Exec(ctx, sql, v.JobID); err == nil {
			t.Fatal("SQL mutated original invocation", sql)
		}
	}
	invocationCounts(t, f, 1, 1, 100)
}

func TestInvocationDirectSQLCannotBypassCountOrAtomicBudget(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	f.in.MaxCalls = 1
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	v, err := invocationService(t).Prepare(ctx, p, invocationRequest(relay.ChatText, "one-slot-call"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline,invocation_profile,route_contract_id,invoker_key_id,fingerprint_version,invocation_spec,reservation_id)
SELECT $1,order_id,seller_id,$2,request_digest,'INPUT_PENDING',deadline,invocation_profile,route_contract_id,invoker_key_id,fingerprint_version,invocation_spec,$3 FROM p11.jobs WHERE id=$4`, ids.New("job"), ids.New("original"), ids.New("bgt"), v.JobID)
	if err == nil {
		t.Fatal("direct SQL bypassed max calls")
	}
	invocationCounts(t, f, 1, 1, 100)
	// A different route still has capacity. Missing atomic budget/event must
	// reject at COMMIT, not be confused with the count-limit rejection above.
	g := routeOrder(t, buyer(t), relay.ChatText)
	other := g.create(t)
	_, _, actor := routeKey(t, g.buyer, other.ID)
	first, err := invocationService(t).Prepare(ctx, actor, invocationRequest(relay.ChatText, "atomic-budget-call"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline,invocation_profile,route_contract_id,invoker_key_id,fingerprint_version,invocation_spec,reservation_id)
SELECT $1,order_id,seller_id,$2,request_digest,'INPUT_PENDING',deadline,invocation_profile,route_contract_id,invoker_key_id,fingerprint_version,invocation_spec,$3 FROM p11.jobs WHERE id=$4`, ids.New("job"), ids.New("original"), ids.New("bgt"), first.JobID)
	if err == nil {
		t.Fatal("direct SQL committed without original budget/event")
	}
	invocationCounts(t, g, 1, 1, 100)
}

func TestInvocationDeadlineUsesCurrentFundsEvidenceAndKey(t *testing.T) {
	requireDB(t)
	for _, shortest := range []string{"funds", "evidence", "key"} {
		t.Run(shortest, func(t *testing.T) {
			f := routeOrder(t, buyer(t), relay.ChatText)
			c := f.create(t)
			_, key, p := routeKey(t, f.buyer, c.ID)
			deadline := time.Now().UTC().Add(20 * time.Second).Truncate(time.Microsecond)
			funds, evidence, keyLimit := deadline.Add(10*time.Second), deadline.Add(10*time.Second), deadline.Add(10*time.Second)
			switch shortest {
			case "funds":
				funds = deadline
			case "evidence":
				evidence = deadline
			case "key":
				keyLimit = deadline
			}
			exec(t, `UPDATE p11.payments SET funds_expires_at=$1,version=version+1 WHERE id=$2`, funds, f.in.PaymentID)
			exec(t, `UPDATE p11.evidence SET valid_until=$1 WHERE id=$2`, evidence, f.offer.Evidence[0])
			exec(t, `UPDATE p11.api_keys SET expires_at=$1 WHERE id=$2`, keyLimit, key.Key.ID)
			v, err := invocationService(t).Prepare(ctx, p, invocationRequest(relay.ChatText, "bounded-deadline-call"))
			if err != nil || !v.Deadline.Equal(deadline) {
				t.Fatal(v, err)
			}
			invocationCounts(t, f, 1, 1, 100)
		})
	}
}

func TestInvocationUpgradePreservesUnboundHistoryWithoutConsent(t *testing.T) {
	requireDB(t)
	local, _, source := budgetUpgradeDB(t)
	pre := fstest.MapFS{}
	paths, err := fs.Glob(source, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range paths {
		if strings.TrimPrefix(name, "migrations/")[:3] >= "008" {
			continue
		}
		body, err := fs.ReadFile(source, name)
		if err != nil {
			t.Fatal(err)
		}
		pre[name] = &fstest.MapFile{Data: body}
	}
	if len(pre) != 7 {
		t.Fatal(len(pre))
	}
	if err = local.MigrateFS(ctx, pre); err != nil {
		t.Fatal(err)
	}
	f := testsupport.SeedOffer(t, ctx, local, "PLAINTEXT", ids.New("region"))
	p := identity.Principal{UserID: ids.New("usr"), Kind: identity.UserSession, Version: 1, Scopes: []string{"quote:create", "order:create"}}
	run := func(sql string, args ...any) {
		t.Helper()
		if _, err := local.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, p.UserID, outbox.Digest([]byte(p.UserID)))
	run(`INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)`, ids.New("grant"), p.UserID)
	q, err := (trade.Service{DB: local}).CreateQuote(ctx, p, f.Input(), "old-unbound-quote")
	if err != nil {
		t.Fatal(err)
	}
	o, err := (trade.Service{DB: local}).CreateOrder(ctx, p, confirm(q), "old-unbound-order")
	if err != nil {
		t.Fatal(err)
	}
	job := ids.New("job")
	digest := outbox.Digest([]byte("synthetic legacy metadata"))
	run(`INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline) VALUES($1,$2,$3,'original-legacy-key',$4,'CANCELED',clock_timestamp()+interval '1 hour')`, job, o.ID, f.Seller.UserID, digest)
	if err = local.MigrateFS(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err = local.VerifyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	var profile, state, stored string
	var absent bool
	if err = local.Pool.QueryRow(ctx, `SELECT invocation_profile,state,request_digest,route_contract_id IS NULL AND invoker_key_id IS NULL AND fingerprint_version IS NULL AND invocation_spec IS NULL AND reservation_id IS NULL FROM p11.jobs WHERE id=$1`, job).Scan(&profile, &state, &stored, &absent); err != nil || profile != "UNBOUND_JOB" || state != "CANCELED" || stored != digest || !absent {
		t.Fatal(profile, state, stored, absent, err)
	}
	var jobs, invocations, holds int
	if err = local.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE invocation_profile='ROUTE_PREPARE_1'),(SELECT count(*) FROM p11.budget_reservations) FROM p11.jobs`).Scan(&jobs, &invocations, &holds); err != nil || jobs != 1 || invocations != 0 || holds != 0 {
		t.Fatal(jobs, invocations, holds, err)
	}
	// Even a well-shaped attempted retrofit cannot turn retained history into
	// current buyer consent. Trigger runs before the foreign-key checks.
	spec, _ := json.Marshal(map[string]any{"protocol_profile_id": relay.ChatText, "public_model": "model_nb1", "max_output_tokens": 16, "stream": false})
	_, err = local.Pool.Exec(ctx, `UPDATE p11.jobs SET invocation_profile='ROUTE_PREPARE_1',route_contract_id=$1,invoker_key_id=$2,fingerprint_version='fp_one',invocation_spec=$3,reservation_id=$4 WHERE id=$5`, ids.New("route"), ids.New("key"), spec, ids.New("bgt"), job)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatal("historical job consent retrofitted", err)
	}
	if err = local.MigrateFS(ctx, source); err != nil {
		t.Fatal("repeat 008 upgrade", err)
	}
}
