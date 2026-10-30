package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

var db *persistence.DB
var ctx context.Context

func TestMain(m *testing.M) {
	dsn := os.Getenv("P11_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("P11_REQUIRE_INTEGRATION") == "1" {
			fmt.Fprintln(os.Stderr, "native database URL required")
			os.Exit(1)
		}
		os.Exit(m.Run())
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 220*time.Second)
	var err error
	db, err = persistence.Open(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "native database connection failed")
		os.Exit(1)
	}
	if err = db.Migrate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		db.Close()
		os.Exit(1)
	}
	code := m.Run()
	db.Close()
	cancel()
	os.Exit(code)
}
func requireDB(t *testing.T) {
	t.Helper()
	if db == nil {
		t.Skip("native PostgreSQL required; use dev/tools/原生数据库验证.py")
	}
}
func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func user(t *testing.T) identity.Principal {
	t.Helper()
	id := ids.New("usr")
	exec(t, `INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, id, outbox.Digest([]byte(id)))
	exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)`, ids.New("grant"), id)
	return identity.Principal{UserID: id, Kind: identity.UserSession, Scopes: []string{"key:manage", "identity:read"}, Roles: []string{"BUYER"}, Version: 1}
}
func assertCode(t *testing.T, err error, code fault.Code) {
	t.Helper()
	if err == nil || fault.Public(err).Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestMigrationsExecuteAllDomainDDLAndRejectDrift(t *testing.T) {
	requireDB(t)
	// Two migrators must wait behind a real held advisory lock longer than the
	// normal business lock timeout, then serialize without changing DDL bytes.
	holder, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(7410112026)`); err != nil {
		t.Fatal(err)
	}
	ready, results := make(chan struct{}, 2), make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { ready <- struct{}{}; results <- db.Migrate(ctx) }()
	}
	<-ready
	<-ready
	if _, err = holder.Exec(ctx, `SELECT pg_sleep(3.5)`); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = <-results; err != nil {
			t.Fatal("bounded migration admission:", err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("repeat migration:", err)
	}
	var tables int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='p11' AND table_type='BASE TABLE'`).Scan(&tables); err != nil || tables != 64 {
		t.Fatalf("domain tables %d %v", tables, err)
	}
	var migration string
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM p11_meta.schema_migrations WHERE version=1`).Scan(&migration); err != nil {
		t.Fatal(err)
	}
	err = db.MigrateFS(ctx, fstest.MapFS{"migrations/" + migration: &fstest.MapFile{Data: []byte("SELECT 1;")}})
	if err == nil || !strings.Contains(err.Error(), "checksum drift") {
		t.Fatal("modified migration was not rejected", err)
	}
}
func TestKeysSecretOnceScopesOwnershipAndRevocation(t *testing.T) {
	requireDB(t)
	p := user(t)
	other := user(t)
	s := identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "pepper_v1", Peppers: map[string][]byte{"pepper_v1": []byte(strings.Repeat("p", 32))}, MaxTTL: 24 * time.Hour}
	input := identity.KeyInput{Label: "SDK market key", Scopes: []string{"MARKET_READ"}, ExpiresAt: time.Now().Add(time.Hour)}
	first, err := s.Create(ctx, p, input, "create-key-001")
	if err != nil {
		t.Fatal(err)
	}
	if first.SecretOnce == "" || first.Replayed {
		t.Fatal("first secret missing")
	}
	replay, err := s.Create(ctx, p, input, "create-key-001")
	if err != nil || !replay.Replayed || replay.SecretOnce != "" || replay.Key.ID != first.Key.ID {
		t.Fatal("secret replay or duplicate key", err)
	}
	changed := input
	changed.Label = "different"
	_, err = s.Create(ctx, p, changed, "create-key-001")
	assertCode(t, err, fault.Conflict)
	credential, err := s.Authenticate(ctx, first.SecretOnce)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Kind != identity.APIKey || credential.UserID != p.UserID {
		t.Fatal("wrong API principal")
	}
	assertCode(t, credential.RequireUser("key:manage"), fault.Forbidden)
	_, err = s.Authenticate(ctx, first.SecretOnce+"x")
	assertCode(t, err, fault.Unauthenticated)
	if err = s.Revoke(ctx, other, first.Key.ID, "revoke-other-key"); err == nil {
		t.Fatal("cross-owner revoke accepted")
	} else {
		assertCode(t, err, fault.NotFound)
	}
	page, err := s.List(ctx, p, "", 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("owner listing failed", err)
	}
	data, _ := json.Marshal(page)
	if strings.Contains(string(data), first.SecretOnce) {
		t.Fatal("secret exposed by list")
	}
	var stored string
	if err = db.Pool.QueryRow(ctx, `SELECT key_digest FROM p11.api_keys WHERE id=$1`, first.Key.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, first.SecretOnce) || stored == first.Key.Fingerprint {
		t.Fatal("key stored in plaintext or unpeppered")
	}
	if err = s.Revoke(ctx, p, first.Key.ID, "revoke-own-key"); err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(ctx, p, first.Key.ID, "revoke-own-key"); err != nil {
		t.Fatal("revoke replay", err)
	}
	_, err = s.Authenticate(ctx, first.SecretOnce)
	assertCode(t, err, fault.Unauthenticated)
	page, err = s.List(ctx, other, "", 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("keys leaked to other user", err)
	}
	input.Scopes = []string{"key:manage"}
	_, err = s.Create(ctx, p, input, "bad-scope-001")
	assertCode(t, err, fault.Invalid)
	input.Scopes = []string{"ROUTE_INVOKE"}
	_, err = s.Create(ctx, p, input, "missing-route-001")
	assertCode(t, err, fault.Invalid)
}
func TestConcurrentIdempotencyCommitsOneBusinessMutation(t *testing.T) {
	requireDB(t)
	p := user(t)
	var applied atomic.Int64
	command := outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "nativeTest", Resource: p.UserID, Key: "concurrent-one-key", Request: []byte(`{"same":true}`)}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			_, err := (outbox.Idempotency{DB: db}).Do(ctx, command, func(tx pgx.Tx) (outbox.StoredResult, error) {
				applied.Add(1)
				ref := ids.New("aud")
				_, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,'IDEMPOTENCY_TEST',$4,$3,'{}')`, ref, p.UserID, ids.New("req"), p.UserID)
				return outbox.StoredResult{Reference: ref, Body: json.RawMessage(`{"ok":true}`), HTTPStatus: 200}, err
			})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if applied.Load() != 1 {
		t.Fatalf("callback executed %d times", applied.Load())
	}
	exec(t, `UPDATE p11.users SET version=version+1 WHERE id=$1`, p.UserID)
	_, err := (outbox.Idempotency{DB: db}).Do(ctx, command, func(pgx.Tx) (outbox.StoredResult, error) {
		t.Fatal("stale user replay entered callback")
		return outbox.StoredResult{}, nil
	})
	assertCode(t, err, fault.Unauthenticated)
}
func TestInboxReceiptRollsBackWithSideEffectAndDeduplicates(t *testing.T) {
	requireDB(t)
	p := user(t)
	event := ids.New("evt")
	ref := ids.New("aud")
	callback := func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,'CONSUMER_TEST',$4,$3,'{}')`, ref, p.UserID, ids.New("req"), p.UserID)
		return err
	}
	_, err := outbox.ConsumeOnce(ctx, db, "consumer_test", event, []byte(`{"safe":true}`), func(tx pgx.Tx) error {
		if err := callback(tx); err != nil {
			return err
		}
		return errors.New("simulated crash before commit")
	})
	if err == nil {
		t.Fatal("simulated failure ignored")
	}
	duplicate, err := outbox.ConsumeOnce(ctx, db, "consumer_test", event, []byte(`{"safe":true}`), callback)
	if err != nil || duplicate {
		t.Fatal("receipt survived rolled-back mutation", err)
	}
	for range 20 {
		duplicate, err = outbox.ConsumeOnce(ctx, db, "consumer_test", event, []byte(`{"safe":true}`), callback)
		if err != nil || !duplicate {
			t.Fatal("consumer duplicate re-applied", err)
		}
	}
	_, err = outbox.ConsumeOnce(ctx, db, "consumer_test", event, []byte(`{"changed":true}`), callback)
	assertCode(t, err, fault.Conflict)
}
func TestDurableJobsFenceStaleClaimsAndBoundRetries(t *testing.T) {
	requireDB(t)
	q := outbox.Queue{DB: db, MaxTries: 2}
	target := ids.New("pay")
	var id string
	err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		id, err = q.Enqueue(ctx, tx, "QUERY_PAYMENT", target, "query-"+target, time.Now().Add(-time.Second))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := q.Claim(ctx, 1, time.Minute)
	if err != nil || len(jobs) != 1 || jobs[0].ID != id {
		t.Fatal("claim failed", err, jobs)
	}
	old := jobs[0]
	exec(t, `UPDATE p11.durable_jobs SET claimed_until=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	jobs, err = q.Claim(ctx, 1, time.Minute)
	if err != nil || len(jobs) != 1 || jobs[0].Generation <= old.Generation {
		t.Fatal("claim generation did not advance", err)
	}
	assertCode(t, q.Complete(ctx, old), fault.Conflict)
	assertCode(t, q.Fail(ctx, jobs[0], "ARBITRARY_UPPERCASE_CONTENT"), fault.Invalid)
	if err = q.Fail(ctx, jobs[0], "QUERY_UNAVAILABLE"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.Pool.QueryRow(ctx, `SELECT state FROM p11.durable_jobs WHERE id=$1`, id).Scan(&state); err != nil || state != "DEAD" {
		t.Fatal("unbounded retries", state, err)
	}
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, err := q.Enqueue(ctx, tx, "CREATE_PAYMENT", target, "no-financial-retry", time.Now())
		return err
	})
	assertCode(t, err, fault.Invalid)
}
