package integration_test

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"strings"
	"sync"
	"testing"
	"time"
)

type budgetFixture struct {
	scope budget.Scope
	offer offerFixture
}

// The quote is real application SQL. READY orders, qualifications and usage are
// explicit synthetic financial fixtures, not channel-confirmed payments or GPUs.
func budgetOrder(t *testing.T, mode, state string, expired bool) budgetFixture {
	t.Helper()
	f, p := offer(t, mode, ids.New("region")), buyer(t)
	q, e := (trade.Service{DB: db}).CreateQuote(ctx, p, f.input(), ids.New("quote"))
	if e != nil {
		t.Fatal(e)
	}
	order := ids.New("ord")
	deadline := time.Now().Add(time.Hour)
	if expired {
		deadline = time.Now().Add(-time.Minute)
	}
	exec(t, `INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,
fee_bps,contract_snapshot,contract_hash,deadline)
SELECT $1,id,buyer_id,seller_id,$2,$3,$4,max_amount_fen,500,contract_snapshot,contract_hash,$5
FROM p11.quotes WHERE id=$6`, order, state, mode, q.Contract.Policy.Privacy, deadline, q.ID)
	return budgetFixture{budget.Scope{OrderID: order, BuyerID: p.UserID, SellerID: f.seller.UserID, ContractHash: q.Contract.Hash}, f}
}

func (f budgetFixture) job(t *testing.T) string {
	t.Helper()
	id := ids.New("job")
	exec(t, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,fencing_token,deadline)
VALUES($1,$2,$3,$4,repeat('1',64),'QUEUED',1,clock_timestamp()+interval '1 hour')`, id, f.scope.OrderID, f.scope.SellerID, id)
	return id
}

func (f budgetFixture) usage(t *testing.T, purpose string, actual int64, status string) string {
	t.Helper()
	a, u, q := ids.New("att"), ids.New("usg"), ids.New("tqf")
	exec(t, `INSERT INTO p11.attempts(id,job_id,order_id,seller_id,device_id,resource_id,capability_id,fencing_token,state)
VALUES($1,$2,$3,$4,$5,$6,$7,1,'PREPARED')`, a, purpose, f.scope.OrderID, f.scope.SellerID, f.offer.device, f.offer.resource, f.offer.capability)
	exec(t, `UPDATE p11.jobs SET current_attempt_id=$1 WHERE id=$2`, a, purpose)
	exec(t, `INSERT INTO p11.usage_evidence(id,job_id,attempt_id,fencing_token,sequence,tokenizer_profile,model_digest,
input_tokens,output_tokens,status,evidence_hash) VALUES($1,$2,$3,1,1,'SYNTHETIC_TEST',repeat('2',64),1,1,$4,repeat('3',64))`, u, purpose, a, status)
	exec(t, `INSERT INTO p11.trade_qualifications(id,order_id,source_type,source_id,gross_fen,evidence_hash)
VALUES($1,$2,'VERIFIED_USAGE',$3,$4,repeat('4',64))`, q, f.scope.OrderID, u, actual)
	return q
}

func reserveBudget(s budget.Scope, purpose string, amount money.Fen) (out budget.Result, err error) {
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		out, e = budget.ReserveTx(ctx, tx, s, budget.Request{PurposeRef: purpose, PurposeKind: "JOB", MaximumFen: amount})
		return e
	})
	return
}
func completeBudget(s budget.Scope, purpose, qualification string) (out budget.Result, err error) {
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		out, e = budget.CompleteTx(ctx, tx, s, purpose, qualification)
		return e
	})
	return
}
func budgetCapacity(t *testing.T, s budget.Scope) budget.Capacity {
	t.Helper()
	var out budget.Capacity
	e := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		out, e = budget.SnapshotTx(ctx, tx, s)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if out.Maximum != out.Reserved+out.Consumed+out.Unverified+out.Remaining {
		t.Fatal("capacity conservation", out)
	}
	return out
}

func TestBudgetConcurrentReservationsCannotExceedOriginalOrder(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	jobs := make([]string, 20)
	for i := range jobs {
		jobs[i] = f.job(t)
	}
	results := make(chan error, len(jobs))
	for _, j := range jobs {
		go func() { _, e := reserveBudget(f.scope, j, 100); results <- e }()
	}
	accepted := 0
	for range jobs {
		if e := <-results; e == nil {
			accepted++
		} else if fault.Public(e).Code != fault.Conflict {
			t.Fatal(e)
		}
	}
	c := budgetCapacity(t, f.scope)
	if accepted != 5 || c.Reserved != 500 || c.Remaining != 0 {
		t.Fatal("concurrent capacity", accepted, c)
	}
}

func TestBudgetOriginalPurposeReplaysWithoutSecondReservation(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	type result struct {
		out budget.Result
		err error
	}
	results := make(chan result, 16)
	for range 16 {
		go func() { r, e := reserveBudget(f.scope, j, 100); results <- result{r, e} }()
	}
	created, original := 0, ""
	for range 16 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if original == "" {
			original = r.out.Reservation.ID
		}
		if r.out.Reservation.ID != original {
			t.Fatal("original reservation changed")
		}
		if r.out.New {
			created++
		}
	}
	if created != 1 || budgetCapacity(t, f.scope).Reserved != 100 {
		t.Fatal("duplicate reservation", created)
	}
	_, e := reserveBudget(f.scope, j, 101)
	assertCode(t, e, fault.Conflict)
}

func TestBudgetQualifiedActualConsumesAndZeroReleasesUnusedCeiling(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	a, b := f.job(t), f.job(t)
	if _, e := reserveBudget(f.scope, a, 150); e != nil {
		t.Fatal(e)
	}
	if _, e := reserveBudget(f.scope, b, 100); e != nil {
		t.Fatal(e)
	}
	q := f.usage(t, a, 80, "VERIFIED")
	r, e := completeBudget(f.scope, a, q)
	if e != nil || !r.New || r.Reservation.State != "CONSUMED" || *r.Reservation.ConsumedFen != 80 {
		t.Fatal(r, e)
	}
	z := f.usage(t, b, 0, "VERIFIED")
	r, e = completeBudget(f.scope, b, z)
	if e != nil || r.Reservation.State != "RELEASED" || *r.Reservation.ConsumedFen != 0 {
		t.Fatal(r, e)
	}
	c := budgetCapacity(t, f.scope)
	if c.Reserved != 0 || c.Consumed != 80 || c.Remaining != 420 {
		t.Fatal(c)
	}
	r, e = reserveBudget(f.scope, a, 150)
	if e != nil || r.New || r.Reservation.State != "CONSUMED" {
		t.Fatal("resolved purpose was reused", r, e)
	}
}

func TestBudgetUnknownAndUnverifiedUsageNeverReleaseHold(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	if _, e := reserveBudget(f.scope, j, 150); e != nil {
		t.Fatal(e)
	}
	_, e := completeBudget(f.scope, j, ids.New("missing"))
	assertCode(t, e, fault.Unavailable)
	q := f.usage(t, j, 0, "UNVERIFIED")
	if _, e = completeBudget(f.scope, j, q); e == nil {
		t.Fatal("unverified zero released capacity")
	}
	exec(t, `UPDATE p11.orders SET state='CLOSED' WHERE id=$1`, f.scope.OrderID)
	var got budget.Result
	e = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error { var e error; got, e = budget.LookupTx(ctx, tx, f.scope, j); return e })
	if e != nil || got.Reservation.State != "RESERVED" || got.Capacity.Reserved != 150 || got.Reservation.ConsumedFen != nil {
		t.Fatal(got, e)
	}
}

func TestBudgetScopeAndOriginalPurposeCannotCrossOrders(t *testing.T) {
	requireDB(t)
	f, other := budgetOrder(t, "PLAINTEXT", "READY", false), budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	for _, kind := range []string{"buyer", "seller", "contract", "purpose"} {
		t.Run(kind, func(t *testing.T) {
			s := f.scope
			switch kind {
			case "buyer":
				s.BuyerID = other.scope.BuyerID
			case "seller":
				s.SellerID = other.scope.SellerID
			case "contract":
				s.ContractHash = strings.Repeat("0", 64)
			case "purpose":
				s = other.scope
			}
			if _, e := reserveBudget(s, j, 100); e == nil {
				t.Fatal("cross-scope reserve accepted")
			}
		})
	}
	if budgetCapacity(t, f.scope).Reserved != 0 || budgetCapacity(t, other.scope).Reserved != 0 {
		t.Fatal("rejected request changed capacity")
	}
}

func TestBudgetDeadlineAndOrderStateStopNewCapacity(t *testing.T) {
	requireDB(t)
	for _, state := range []string{"WAITING_PAYMENT", "REVIEW", "CLOSED", "COMPLETED", "READY"} {
		t.Run(state, func(t *testing.T) {
			f := budgetOrder(t, "PLAINTEXT", state, state == "READY")
			_, e := reserveBudget(f.scope, f.job(t), 100)
			assertCode(t, e, fault.Conflict)
			if budgetCapacity(t, f.scope).Reserved != 0 {
				t.Fatal("ineligible order reserved")
			}
		})
	}
}

func TestBudgetRollbackIncludesCapacityAndCallerMutation(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	stop := errors.New("controlled rollback")
	e := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if _, e := budget.ReserveTx(ctx, tx, f.scope, budget.Request{PurposeRef: j, PurposeKind: "JOB", MaximumFen: 120}); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE p11.jobs SET state='RUNNING' WHERE id=$1`, j); e != nil {
			return e
		}
		return stop
	})
	if !errors.Is(e, stop) {
		t.Fatal(e)
	}
	var state string
	if e = db.Pool.QueryRow(ctx, `SELECT state FROM p11.jobs WHERE id=$1`, j).Scan(&state); e != nil || state != "QUEUED" {
		t.Fatal(state, e)
	}
	if budgetCapacity(t, f.scope).Reserved != 0 {
		t.Fatal("rollback retained hold")
	}
	r, e := reserveBudget(f.scope, j, 120)
	if e != nil || !r.New {
		t.Fatal("original retry after verified rollback", r, e)
	}
}

func TestBudgetConcurrentCompletionIsOneImmutableOutcome(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	if _, e := reserveBudget(f.scope, j, 150); e != nil {
		t.Fatal(e)
	}
	q := f.usage(t, j, 80, "VERIFIED")
	var wg sync.WaitGroup
	results := make(chan budget.Result, 12)
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() { r, e := completeBudget(f.scope, j, q); results <- r; errs <- e })
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	created := 0
	for r := range results {
		if r.New {
			created++
		}
	}
	if created != 1 || budgetCapacity(t, f.scope).Consumed != 80 {
		t.Fatal("double consumed", created)
	}
	_, e := completeBudget(f.scope, j, ids.New("other"))
	assertCode(t, e, fault.Conflict)
}

func TestBudgetSourceAmountAndPurposeMustMatchReservation(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	a, b := f.job(t), f.job(t)
	if _, e := reserveBudget(f.scope, a, 100); e != nil {
		t.Fatal(e)
	}
	q := f.usage(t, b, 80, "VERIFIED")
	if _, e := completeBudget(f.scope, a, q); e == nil {
		t.Fatal("unrelated source consumed")
	}
	q = f.usage(t, a, 101, "VERIFIED")
	_, e := completeBudget(f.scope, a, q)
	assertCode(t, e, fault.Conflict)
	if budgetCapacity(t, f.scope).Reserved != 100 {
		t.Fatal("failed finalization released hold")
	}
}

func TestBudgetSQLGuardsRejectCounterAndHistoryBypasses(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	r, e := reserveBudget(f.scope, j, 100)
	if e != nil {
		t.Fatal(e)
	}
	for _, query := range []string{
		`UPDATE p11.orders SET budget_reserved_fen=0 WHERE id=$1`,
		`UPDATE p11.budget_reservations SET amount_fen=1 WHERE order_id=$1`,
		`DELETE FROM p11.budget_reservations WHERE order_id=$1`,
		`UPDATE p11.budget_reservations SET state='RELEASED',consumed_fen=0,version=version+1 WHERE order_id=$1`,
	} {
		if _, e = db.Pool.Exec(ctx, query, f.scope.OrderID); e == nil {
			t.Fatal("budget bypass", query)
		}
	}
	q := f.usage(t, j, 50, "VERIFIED")
	if _, e = completeBudget(f.scope, j, q); e != nil {
		t.Fatal(e)
	}
	for _, query := range []string{`UPDATE p11.trade_qualifications SET gross_fen=0 WHERE id=$1`, `DELETE FROM p11.trade_qualifications WHERE id=$1`} {
		if _, e = db.Pool.Exec(ctx, query, q); e == nil {
			t.Fatal("qualification mutation")
		}
	}
	if _, e = db.Pool.Exec(ctx, `UPDATE p11.budget_reservations SET state='RESERVED',consumed_fen=NULL,qualification_id=NULL,resolved_at=NULL,version=version+1 WHERE id=$1`, r.Reservation.ID); e == nil {
		t.Fatal("resolved reservation reopened")
	}
	if budgetCapacity(t, f.scope).Consumed != 50 {
		t.Fatal("bypass changed accounting")
	}
}

func TestBudgetRepeatableReadCannotUseStaleCapacity(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	a, b := f.job(t), f.job(t)
	stale, e := db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		t.Fatal(e)
	}
	defer stale.Rollback(ctx)
	var n int64
	if e = stale.QueryRow(ctx, `SELECT budget_reserved_fen FROM p11.orders WHERE id=$1`, f.scope.OrderID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if _, e = reserveBudget(f.scope, a, 400); e != nil {
		t.Fatal(e)
	}
	_, e = budget.ReserveTx(ctx, stale, f.scope, budget.Request{PurposeRef: b, PurposeKind: "JOB", MaximumFen: 400})
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "40001" {
		t.Fatal("stale snapshot did not abort", e)
	}
	stale.Rollback(ctx)
	if budgetCapacity(t, f.scope).Reserved != 400 {
		t.Fatal("stale snapshot overspent")
	}
}

func TestBudgetInvalidAndZeroRequestsStayBounded(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	for _, n := range []money.Fen{-1, money.Fen(money.MaxFen + 1)} {
		_, e := reserveBudget(f.scope, j, n)
		assertCode(t, e, fault.Invalid)
	}
	r, e := reserveBudget(f.scope, j, 0)
	if e != nil || !r.New || r.Reservation.MaximumFen != 0 || r.Reservation.ConsumedFen != nil {
		t.Fatal(r, e)
	}
}

func TestBudgetDirectSQLConcurrencySharesParentCapacityGuard(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	jobs := make([]string, 12)
	for i := range jobs {
		jobs[i] = f.job(t)
	}
	results := make(chan error, len(jobs))
	for _, j := range jobs {
		go func() {
			_, e := db.Pool.Exec(ctx, `INSERT INTO p11.budget_reservations
(id,order_id,purpose_ref,amount_fen,state,accounting_profile,purpose_kind)
VALUES($1,$2,$3,100,'RESERVED','ORDER_CAPACITY_1','JOB')`, ids.New("bgt"), f.scope.OrderID, j)
			results <- e
		}()
	}
	n := 0
	for range jobs {
		if e := <-results; e == nil {
			n++
		} else {
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "P0001" || pg.Message != "order capacity exhausted" {
				t.Fatal("raw SQL failed for an unrelated reason", e)
			}
		}
	}
	if n != 5 || budgetCapacity(t, f.scope).Reserved != 500 {
		t.Fatal("raw SQL bypassed shared capacity", n)
	}
}

func TestBudgetFinalRentalMeterReleasesOnlyItsOriginalSession(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "GPU_RENTAL", "READY", false)
	session := ids.New("rnt")
	exec(t, `INSERT INTO p11.rental_sessions(id,order_id,seller_id,device_id,resource_id,resource_lock_generation,
state,cleanup_state,rate_fen_per_minute,budget_fen) VALUES($1,$2,$3,$4,$5,1,'ACTIVE','PENDING',6,500)`,
		session, f.scope.OrderID, f.scope.SellerID, f.offer.device, f.offer.resource)
	e := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, e := budget.ReserveTx(ctx, tx, f.scope, budget.Request{PurposeRef: session, PurposeKind: "RENTAL_SESSION", MaximumFen: 300})
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	m, q := ids.New("mtr"), ids.New("tqf")
	exec(t, `INSERT INTO p11.meter_statements(id,session_id,revision,confirmed_duration_ms,amount_fen,evidence_hash,is_final)
VALUES($1,$2,1,1000000,100,repeat('6',64),false)`, m, session)
	exec(t, `INSERT INTO p11.trade_qualifications(id,order_id,source_type,source_id,gross_fen,evidence_hash)
VALUES($1,$2,'FINAL_RENTAL_METER',$3,100,repeat('7',64))`, q, f.scope.OrderID, m)
	if _, e = completeBudget(f.scope, session, q); e == nil {
		t.Fatal("nonfinal meter released capacity")
	}
	if budgetCapacity(t, f.scope).Reserved != 300 {
		t.Fatal("nonfinal meter lost hold")
	}
	exec(t, `UPDATE p11.meter_statements SET is_final=true WHERE id=$1`, m)
	r, e := completeBudget(f.scope, session, q)
	if e != nil || r.Reservation.State != "CONSUMED" || r.Capacity.Consumed != 100 || r.Capacity.Remaining != 400 {
		t.Fatal(r, e)
	}
}

func TestBudgetAcceptanceRequiresAcceptedOriginalDelivery(t *testing.T) {
	requireDB(t)
	f := budgetOrder(t, "PLAINTEXT", "READY", false)
	j := f.job(t)
	if _, e := reserveBudget(f.scope, j, 100); e != nil {
		t.Fatal(e)
	}
	a, d, ac, q := ids.New("att"), ids.New("dlv"), ids.New("acc"), ids.New("tqf")
	exec(t, `INSERT INTO p11.attempts(id,job_id,order_id,seller_id,device_id,resource_id,capability_id,fencing_token,state)
VALUES($1,$2,$3,$4,$5,$6,$7,1,'PREPARED')`, a, j, f.scope.OrderID, f.scope.SellerID, f.offer.device, f.offer.resource, f.offer.capability)
	exec(t, `INSERT INTO p11.deliveries(id,job_id,attempt_id,fencing_token,revision,manifest_hash,asset_ids,state)
VALUES($1,$2,$3,1,1,repeat('8',64),'{}','VALID')`, d, j, a)
	exec(t, `INSERT INTO p11.acceptances(id,job_id,delivery_id,attempt_id,signer_id,evidence_hash,decision)
VALUES($1,$2,$3,$4,$5,repeat('9',64),'ACCEPT')`, ac, j, d, a, f.scope.BuyerID)
	exec(t, `INSERT INTO p11.trade_qualifications(id,order_id,source_type,source_id,gross_fen,evidence_hash)
VALUES($1,$2,'ACCEPTANCE',$3,50,repeat('a',64))`, q, f.scope.OrderID, ac)
	if _, e := completeBudget(f.scope, j, q); e == nil {
		t.Fatal("unaccepted original job consumed capacity")
	}
	exec(t, `UPDATE p11.jobs SET current_attempt_id=$1,accepted_delivery_id=$2,state='ACCEPTED' WHERE id=$3`, a, d, j)
	exec(t, `UPDATE p11.acceptances SET signer_id=$1 WHERE id=$2`, f.scope.SellerID, ac)
	if _, e := completeBudget(f.scope, j, q); e == nil {
		t.Fatal("seller-signed acceptance consumed buyer capacity")
	}
	if c := budgetCapacity(t, f.scope); c.Reserved != 100 || c.Consumed != 0 || c.Remaining != 400 {
		t.Fatal("wrong signer released or consumed capacity", c)
	}
	exec(t, `UPDATE p11.acceptances SET signer_id=$1 WHERE id=$2`, f.scope.BuyerID, ac)
	exec(t, `UPDATE p11.deliveries SET state='INVALID' WHERE id=$1`, d)
	if _, e := completeBudget(f.scope, j, q); e == nil {
		t.Fatal("invalid original delivery consumed capacity")
	}
	if c := budgetCapacity(t, f.scope); c.Reserved != 100 || c.Consumed != 0 || c.Remaining != 400 {
		t.Fatal("invalid delivery released or consumed capacity", c)
	}
	exec(t, `UPDATE p11.deliveries SET state='VALID' WHERE id=$1`, d)
	r, e := completeBudget(f.scope, j, q)
	if e != nil || r.Capacity.Consumed != 50 {
		t.Fatal(r, e)
	}
}
