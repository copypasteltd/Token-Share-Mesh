package integration_test

import (
	"io/fs"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRouteLegacyUpgradeQuarantinesWithoutInventingConsent(t *testing.T) {
	requireDB(t)
	local, old, source := budgetUpgradeDB(t)
	pre := fstest.MapFS{}
	paths, e := fs.Glob(source, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range paths {
		if strings.TrimPrefix(name, "migrations/")[:3] >= "007" {
			continue
		}
		body, e := fs.ReadFile(source, name)
		if e != nil {
			t.Fatal(e)
		}
		pre[name] = &fstest.MapFile{Data: body}
	}
	if len(pre) != len(old.(fstest.MapFS))+1 {
		t.Fatal("exact pre-007 set")
	}
	if e = local.MigrateFS(ctx, pre); e != nil {
		t.Fatal(e)
	}
	f := testsupport.SeedOffer(t, ctx, local, "PLAINTEXT", ids.New("region"))
	p := identity.Principal{UserID: ids.New("usr"), Kind: identity.UserSession, Version: 1, Scopes: []string{"quote:create", "order:create", "route:read"}}
	run := func(sql string, args ...any) {
		t.Helper()
		if _, e := local.Pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	run(`INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, p.UserID, outbox.Digest([]byte(p.UserID)))
	run(`INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)`, ids.New("grant"), p.UserID)
	q, e := (trade.Service{DB: local}).CreateQuote(ctx, p, f.Input(), "legacy-route-quote")
	if e != nil {
		t.Fatal(e)
	}
	o, e := (trade.Service{DB: local}).CreateOrder(ctx, p, confirm(q), "legacy-route-order")
	if e != nil {
		t.Fatal(e)
	}
	id := ids.New("route")
	run(`INSERT INTO p11.route_contracts(id,order_id,buyer_id,seller_id,contract_hash,protocol_profile,state,expires_at) VALUES($1,$2,$3,$4,$5,'openai_nb1','ACTIVE',clock_timestamp()+interval '1 hour')`, id, o.ID, p.UserID, f.Seller.UserID, q.Contract.Hash)
	if e = local.VerifyMigrations(ctx); e == nil {
		t.Fatal("missing 007 accepted by startup verification")
	}
	if e = local.MigrateFS(ctx, source); e != nil {
		t.Fatal(e)
	}
	var state, profile string
	var version int64
	var absent bool
	if e = local.Pool.QueryRow(ctx, `SELECT state,binding_profile,version,definition IS NULL AND order_snapshot IS NULL AND payment_id IS NULL AND created_at IS NULL FROM p11.route_contracts WHERE id=$1`, id).Scan(&state, &profile, &version, &absent); e != nil || state != "SUSPENDED" || profile != "LEGACY_UNVERIFIED" || version != 2 || !absent {
		t.Fatal(state, profile, version, absent, e)
	}
	if e = local.VerifyMigrations(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = (routecontract.Service{DB: local, PaymentEnvironment: "SANDBOX"}).Get(ctx, p, id)
	assertCode(t, e, fault.NotFound)
	if e = local.MigrateFS(ctx, source); e != nil {
		t.Fatal("repeat upgrade", e)
	}
}

func TestReadOnlyStartupMigrationVerificationRejectsMetadataDrift(t *testing.T) {
	requireDB(t)
	local, _, source := budgetUpgradeDB(t)
	if e := local.MigrateFS(ctx, source); e != nil {
		t.Fatal(e)
	}
	if e := local.VerifyMigrations(ctx); e != nil {
		t.Fatal(e)
	}
	var hash, name string
	if e := local.Pool.QueryRow(ctx, `SELECT sha256,name FROM p11_meta.schema_migrations WHERE version=7`).Scan(&hash, &name); e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{
		`UPDATE p11_meta.schema_migrations SET sha256=repeat('0',64) WHERE version=7`,
		`UPDATE p11_meta.schema_migrations SET name='007_wrong.sql' WHERE version=7`,
	} {
		if _, e := local.Pool.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
		if e := local.VerifyMigrations(ctx); e == nil {
			t.Fatal("startup trusted changed metadata")
		}
		if _, e := local.Pool.Exec(ctx, `UPDATE p11_meta.schema_migrations SET sha256=$1,name=$2 WHERE version=7`, hash, name); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := local.Pool.Exec(ctx, `INSERT INTO p11_meta.schema_migrations(version,name,sha256) VALUES(10,'010_unknown.sql',repeat('a',64))`); e != nil {
		t.Fatal(e)
	}
	if e := local.VerifyMigrations(ctx); e == nil {
		t.Fatal("future migration accepted")
	}
}
