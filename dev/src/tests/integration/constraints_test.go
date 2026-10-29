package integration_test

import (
	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/ids"
	"testing"
)

func order(t *testing.T) string {
	t.Helper()
	buyer := user(t)
	seller := user(t)
	exec(t, `INSERT INTO p11.seller_profiles(id,state) VALUES($1,'ACTIVE')`, seller.UserID)
	listing, quote, order := ids.New("lst"), ids.New("quo"), ids.New("ord")
	exec(t, `INSERT INTO p11.listings(id,seller_id,state) VALUES($1,$2,'ACTIVE')`, listing, seller.UserID)
	exec(t, `INSERT INTO p11.listing_versions(listing_id,revision,seller_id,region,contract_template,price_version,valid_from,valid_until) VALUES($1,1,$2,'TEST','{}','v1',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours')`, listing, seller.UserID)
	exec(t, `INSERT INTO p11.quotes(id,buyer_id,seller_id,listing_id,listing_revision,contract_snapshot,contract_hash,max_amount_fen,expires_at) VALUES($1,$2,$3,$4,1,'{}',repeat('a',64),100,clock_timestamp()+interval '1 hour')`, quote, buyer.UserID, seller.UserID, listing)
	exec(t, `INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,fee_bps,contract_snapshot,contract_hash,deadline) VALUES($1,$2,$3,$4,'CREATED','PLAINTEXT','STANDARD',100,500,'{}',repeat('a',64),clock_timestamp()+interval '1 hour')`, order, quote, buyer.UserID, seller.UserID)
	return order
}

func TestJournalCommitBalancesAndPreventsLaterMutation(t *testing.T) {
	requireDB(t)
	orderID := order(t)
	left, right := ids.New("acc"), ids.New("acc")
	exec(t, `INSERT INTO p11.ledger_accounts(id,account_code,classification) VALUES($1,$2,'ASSET'),($3,$4,'LIABILITY')`, left, left, right, right)
	create := func(journal string, amount int64, withEntries bool) error {
		return db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO p11.journal_transactions(id,order_id,source_type,source_id) VALUES($1,$2,'NATIVE_TEST',$3)`, journal, orderID, ids.New("src"))
			if err != nil || !withEntries {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO p11.journal_entries(id,journal_id,account_id,amount_fen,line_number) VALUES($1,$2,$3,100,1),($4,$2,$5,$6,2)`, ids.New("line"), journal, left, ids.New("line"), right, amount)
			return err
		})
	}
	if err := create(ids.New("jrnl"), -99, true); err == nil {
		t.Fatal("unbalanced journal committed")
	}
	if err := create(ids.New("jrnl"), 0, false); err == nil {
		t.Fatal("empty journal committed")
	}
	valid := ids.New("jrnl")
	if err := create(valid, -100, true); err != nil {
		t.Fatal("balanced journal rejected", err)
	}
	for _, sql := range []string{`UPDATE p11.journal_entries SET amount_fen=1 WHERE journal_id=$1`, `DELETE FROM p11.journal_entries WHERE journal_id=$1`, `DELETE FROM p11.journal_transactions WHERE id=$1`, `UPDATE p11.journal_transactions SET source_type='CHANGED' WHERE id=$1`} {
		if _, err := db.Pool.Exec(ctx, sql, valid); err == nil {
			t.Fatal("immutable journal mutated", sql)
		}
	}
	err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO p11.journal_entries(id,journal_id,account_id,amount_fen,line_number) VALUES($1,$2,$3,1,3),($4,$2,$5,-1,4)`, ids.New("line"), valid, left, ids.New("line"), right)
		return err
	})
	if err == nil {
		t.Fatal("entries appended in a later transaction")
	}
	// The application role has no DDL/TRUNCATE privileges, so it cannot disable
	// append-only triggers or bypass them with TRUNCATE.
	exec(t, `CREATE ROLE p11_finance_test NOLOGIN; GRANT USAGE ON SCHEMA p11 TO p11_finance_test; GRANT SELECT,INSERT ON p11.journal_transactions,p11.journal_entries,p11.ledger_accounts TO p11_finance_test`)
	for _, sql := range []string{`TRUNCATE p11.journal_entries`, `ALTER TABLE p11.journal_entries DISABLE TRIGGER ALL`} {
		err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL ROLE p11_finance_test`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if err == nil {
			t.Fatal("application role bypassed immutable journal", sql)
		}
	}
}

func TestCompositeForeignKeyRejectsWrongSellerResource(t *testing.T) {
	requireDB(t)
	seller := user(t)
	other := user(t)
	exec(t, `INSERT INTO p11.seller_profiles(id,state) VALUES($1,'ACTIVE'),($2,'ACTIVE')`, seller.UserID, other.UserID)
	device := ids.New("dev")
	exec(t, `INSERT INTO p11.devices(id,seller_id,platform,resource_kind,state,auth_key_digest,client_release,resource_policy) VALUES($1,$2,'WINDOWS','GPU_DEVICE','OFFLINE',repeat('b',64),'native-test','{}')`, device, seller.UserID)
	_, err := db.Pool.Exec(ctx, `INSERT INTO p11.resources(id,device_id,seller_id,local_resource_key,kind) VALUES($1,$2,$3,'gpu0','GPU_DEVICE')`, ids.New("res"), device, other.UserID)
	if err == nil {
		t.Fatal("resource crossed seller ownership")
	}
}

func TestConsumedQuoteAndOrderTermsCannotBeChanged(t *testing.T) {
	requireDB(t)
	id := order(t)
	if _, err := db.Pool.Exec(ctx, `UPDATE p11.orders SET seller_id=$2 WHERE id=$1`, id, user(t).UserID); err == nil {
		t.Fatal("order seller silently changed")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE p11.quotes SET consumed_by=NULL,consumption_kind=NULL WHERE consumed_by=$1`, id); err == nil {
		t.Fatal("quote consumption was reset")
	}
}
