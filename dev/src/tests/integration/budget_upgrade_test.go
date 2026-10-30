package integration_test

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io/fs"
	"net/url"
	"os"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Each child DB belongs to this disposable native test cluster and is dropped
// after closing its own connections. Never use an external development DB here.
func budgetUpgradeDB(t *testing.T) (*persistence.DB, fs.FS, fs.FS) {
	t.Helper()
	name := strings.ToLower(ids.New("budget_upgrade"))
	if _, e := db.Pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	connection, e := url.Parse(os.Getenv("P11_TEST_DATABASE_URL"))
	if e != nil || connection.Scheme != "postgresql" && connection.Scheme != "postgres" {
		t.Fatal("native test DB configuration")
	}
	// pgx Config.ConnString preserves its original parsed text; changing the
	// Database field and reusing that string would reopen the parent database.
	connection.Path = "/" + name
	local, e := persistence.Open(ctx, connection.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		local.Close()
		if _, e := db.Pool.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
			t.Error("owned upgrade database cleanup", e)
		}
	})
	source := os.DirFS(filepath.Join("..", "..", "internal", "persistence"))
	paths, e := fs.Glob(source, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	old := fstest.MapFS{}
	for _, name := range paths {
		if strings.HasPrefix(filepath.Base(name), "006_") {
			continue
		}
		body, e := fs.ReadFile(source, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if len(old) != 5 {
		t.Fatal("exact pre-006 migration set required")
	}
	if e = local.MigrateFS(ctx, old); e != nil {
		t.Fatal(e)
	}
	return local, old, source
}

func TestBudgetLegacyMigrationRetainsUnknownCapacityAndRejectsOverbooking(t *testing.T) {
	requireDB(t)
	for _, over := range []bool{false, true} {
		name := "conservative_upgrade"
		if over {
			name = "inconsistent_upgrade_aborts"
		}
		t.Run(name, func(t *testing.T) {
			local, _, source := budgetUpgradeDB(t)
			f := testsupport.SeedOffer(t, ctx, local, "PLAINTEXT", ids.New("region"))
			p := identity.Principal{UserID: ids.New("usr"), Kind: identity.UserSession, Scopes: []string{"quote:create", "order:create"}, Version: 1}
			run := func(sql string, args ...any) {
				t.Helper()
				if _, e := local.Pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			run(`INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, p.UserID, outbox.Digest([]byte(p.UserID)))
			run(`INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)`, ids.New("grant"), p.UserID)
			q, e := (trade.Service{DB: local}).CreateQuote(ctx, p, f.Input(), ids.New("quote"))
			if e != nil {
				t.Fatal(e)
			}
			order := ids.New("ord")
			raw, _ := json.Marshal(q.Contract)
			run(`INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,fee_bps,contract_snapshot,contract_hash,deadline)
VALUES($1,$2,$3,$4,'READY','PLAINTEXT','STANDARD',500,500,$5,$6,clock_timestamp()+interval '1 hour')`, order, q.ID, p.UserID, f.Seller.UserID, raw, q.Contract.Hash)
			purpose := ids.New("purpose")
			amount := 100
			if over {
				amount = 200
			}
			for i, state := range []string{"RESERVED", "CONSUMED", "RELEASED"} {
				id := ids.New("old")
				if i == 0 {
					id = purpose
				}
				run(`INSERT INTO p11.budget_reservations(id,order_id,purpose_ref,amount_fen,state) VALUES($1,$2,$1,$3,$4)`, id, order, amount, state)
			}
			e = local.MigrateFS(ctx, source)
			if over {
				if e == nil {
					t.Fatal("inconsistent legacy history silently clamped")
				}
				var columns, version int
				if e = local.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='p11' AND table_name='orders' AND column_name='budget_reserved_fen'`).Scan(&columns); e != nil {
					t.Fatal(e)
				}
				if e = local.Pool.QueryRow(ctx, `SELECT count(*) FROM p11_meta.schema_migrations WHERE version=6`).Scan(&version); e != nil {
					t.Fatal(e)
				}
				if columns != 0 || version != 0 {
					t.Fatal("failed migration partially applied", columns, version)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			scope := budget.Scope{OrderID: order, BuyerID: p.UserID, SellerID: f.Seller.UserID, ContractHash: q.Contract.Hash}
			e = local.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
				c, e := budget.SnapshotTx(ctx, tx, scope)
				if e == nil && (c.Unverified != 300 || c.Reserved != 0 || c.Consumed != 0 || c.Remaining != 200) {
					t.Fatal("legacy state inferred actual usage or release", c)
				}
				return e
			})
			if e != nil {
				t.Fatal(e)
			}
			e = local.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
				_, e := budget.ReserveTx(ctx, tx, scope, budget.Request{PurposeRef: purpose, PurposeKind: "JOB", MaximumFen: 100})
				return e
			})
			assertCode(t, e, fault.Unavailable)
		})
	}
}
