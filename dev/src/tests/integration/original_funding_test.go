package integration_test

// CODE_FIRST / SOURCE_ONLY. Definitions exercise native SQL and the registered
// synthetic RSA2 transport; no real qualification, money or device is proved.
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/fundguard"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/payments"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

func (f *machinePaymentFixture) paidOriginal(t *testing.T) payments.Payment {
	t.Helper()
	p := f.pending(t)
	if _, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "original_funding_private_session"), ids.New("idem")); e != nil {
		t.Fatal(e)
	}
	if worked, e := f.machine.RecoverOne(ctx); e != nil || !worked {
		t.Fatal("original signed payment", worked, e)
	}
	paid, e := f.machine.Get(ctx, f.buyer, p.ID)
	if e != nil || paid.State != "SETTLED" {
		t.Fatal("original paid fact missing", e)
	}
	return paid
}

func currentOriginalFunds(order, payment, environment string) (g fundguard.Grant, e error) {
	e = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		g, err = fundguard.CurrentTx(ctx, tx, order, payment, environment)
		return err
	})
	return
}

func assertFundingFact(t *testing.T, f *machinePaymentFixture, payment, orderState string, count int) {
	t.Helper()
	var state, paid string
	var n int
	e := db.Pool.QueryRow(ctx, `SELECT o.state,p.state,(SELECT count(*) FROM p11.order_fundings WHERE order_id=o.id)
 FROM p11.orders o JOIN p11.payments p ON p.order_id=o.id WHERE o.id=$1 AND p.id=$2`, f.order.ID, payment).Scan(&state, &paid, &n)
	if e != nil || state != orderState || paid != "SETTLED" || n != count {
		t.Fatal("financial fact and original allocation diverged", state, paid, n, e)
	}
}

func TestOriginalFundingIsSeparateFromVerifiedPaymentAndCommitsReadyWithJob(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	assertFundingFact(t, f, p.ID, "WAITING_PAYMENT", 0)
	if _, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX"); e == nil {
		t.Fatal("paid flag alone authorized work")
	}
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1`, f.order.ID); e == nil {
		t.Fatal("machine order bypassed unique allocation")
	}
	if worked, e := f.machine.RecoverFundingOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	assertFundingFact(t, f, p.ID, "READY", 1)
	g, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	if e != nil || g.Buyer != f.buyer.UserID || g.Seller != f.binding.Seller || g.Amount != 500 || g.Receipt == "" {
		t.Fatal("original current allocation", e)
	}
	var events, done int
	e = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='OriginalOrderFundsAdmitted'),
 (SELECT count(*) FROM p11.durable_jobs WHERE kind='ADMIT_MACHINE_FUNDS' AND target_ref=$2 AND state='DONE')`, f.order.ID, p.ID).Scan(&events, &done)
	if e != nil || events != 1 || done != 1 || f.calls.Load() != 2 {
		t.Fatal("ready/allocation/event/job not atomic or metadata used channel", events, done, e)
	}
}

func TestOriginalFundingRepeatedMetadataRecoveryCannotReallocateOrRecharge(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	before, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	if e != nil {
		t.Fatal(e)
	}
	// Simulated lost acknowledgement only requeues the SAME original job.
	exec(t, `UPDATE p11.durable_jobs SET state='PENDING',run_at=clock_timestamp(),claimed_until=NULL WHERE kind='ADMIT_MACHINE_FUNDS' AND target_ref=$1`, p.ID)
	if worked, e := f.machine.RecoverFundingOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	after, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	if e != nil || after.ID != before.ID || f.calls.Load() != 2 {
		t.Fatal("original recovery repeated financial or allocation operation", e)
	}
	var n int
	if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='OriginalOrderFundsAdmitted'`, f.order.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func TestOriginalFundingRevokedCurrentQualificationRetainsPaidObligation(t *testing.T) {
	cases := []struct {
		name   string
		revoke func(*testing.T, *machinePaymentFixture)
	}{
		{"refund_approval", func(t *testing.T, f *machinePaymentFixture) {
			exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.policy.RefundEvidenceRef)
		}},
		{"sale_gate", func(t *testing.T, f *machinePaymentFixture) {
			exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE scope_key=$1`, "machine-pay:"+f.binding.Digest())
		}},
		{"buyer_role", func(t *testing.T, f *machinePaymentFixture) {
			exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, f.buyer.UserID)
		}},
		{"seller_profile", func(t *testing.T, f *machinePaymentFixture) {
			exec(t, `UPDATE p11.seller_profiles SET state='SUSPENDED' WHERE id=$1`, f.binding.Seller)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := machineFixture(t)
			p := f.paidOriginal(t)
			c.revoke(t, f)
			if worked, e := f.machine.RecoverFundingOne(ctx); e != nil || !worked {
				t.Fatal("metadata hold", worked, e)
			}
			assertFundingFact(t, f, p.ID, "WAITING_PAYMENT", 0)
			var job, code string
			if e := db.Pool.QueryRow(ctx, `SELECT state,last_safe_error FROM p11.durable_jobs WHERE kind='ADMIT_MACHINE_FUNDS' AND target_ref=$1`, p.ID).Scan(&job, &code); e != nil || job != "PENDING" || code != "ORIGINAL_FUNDS_NOT_ADMITTED" || f.calls.Load() != 2 {
				t.Fatal(job, code, e)
			}
		})
	}
}

func TestOriginalFundingRefundUncertaintyBlocksAdmissionWithoutRewritingPaid(t *testing.T) {
	for _, state := range []string{"REQUESTED", "UNKNOWN", "SUCCEEDED"} {
		t.Run(state, func(t *testing.T) {
			f := machineFixture(t)
			p := f.paidOriginal(t)
			exec(t, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state) VALUES($1,$2,$3,$4,1,$5)`, ids.New("ref"), p.ID, f.order.ID, ids.New("rkey"), state)
			if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
				t.Fatal(e)
			}
			assertFundingFact(t, f, p.ID, "WAITING_PAYMENT", 0)
		})
	}
}

func TestOriginalFundingClosedOrderNeverRevivesFromLatePaidFact(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	exec(t, `UPDATE p11.orders SET state='CLOSED',version=version+1 WHERE id=$1`, f.order.ID)
	if worked, e := f.machine.RecoverFundingOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	assertFundingFact(t, f, p.ID, "CLOSED", 0)
	var state string
	if e := db.Pool.QueryRow(ctx, `SELECT state FROM p11.durable_jobs WHERE kind='ADMIT_MACHINE_FUNDS' AND target_ref=$1`, p.ID).Scan(&state); e != nil || state != "DEAD" {
		t.Fatal("closed paid obligation must require review", state, e)
	}
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.orders SET state='READY' WHERE id=$1`, f.order.ID); e == nil {
		t.Fatal("closed original machine order revived")
	}
}

func TestOriginalFundingGrantCannotCrossOriginalOrderPaymentOrEnvironment(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	for _, in := range [][3]string{{f.order.ID, ids.New("pay"), "SANDBOX"}, {ids.New("ord"), p.ID, "SANDBOX"}, {f.order.ID, p.ID, "PRODUCTION"}} {
		_, e := currentOriginalFunds(in[0], in[1], in[2])
		assertCode(t, e, fault.Conflict)
	}
	g, e := currentOriginalFunds(f.order.ID, "", "SANDBOX")
	if e != nil || g.Payment != p.ID || g.Order != f.order.ID {
		t.Fatal("unique original allocation lookup", e)
	}
}

func TestOriginalFundingGrantAndRefundHistoryCannotBeDeletedOrRetargeted(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	for _, query := range []string{`DELETE FROM p11.order_fundings WHERE payment_id=$1`, `UPDATE p11.order_fundings SET valid_until=valid_until+interval '1 minute' WHERE payment_id=$1`} {
		if _, e := db.Pool.Exec(ctx, query, p.ID); e == nil {
			t.Fatal("original allocation history mutated")
		}
	}
	ref := ids.New("ref")
	exec(t, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state) VALUES($1,$2,$3,$4,1,'SUCCEEDED')`, ref, p.ID, f.order.ID, ids.New("rkey"))
	for _, query := range []string{`DELETE FROM p11.refunds WHERE id=$1`, `UPDATE p11.refunds SET amount_fen=2 WHERE id=$1`, `UPDATE p11.refunds SET state='FAILED' WHERE id=$1`} {
		if _, e := db.Pool.Exec(ctx, query, ref); e == nil {
			t.Fatal("original refund obligation erased or retargeted")
		}
	}
	_, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	assertCode(t, e, fault.Conflict)
	assertFundingFact(t, f, p.ID, "READY", 1)
}

func TestOriginalFundingCurrentApprovalsRecheckedWithoutDestroyingGrant(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.policy.CommissionEvidenceRef)
	_, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	assertCode(t, e, fault.Conflict)
	assertFundingFact(t, f, p.ID, "READY", 1)
	if f.calls.Load() != 2 {
		t.Fatal("current eligibility triggered another payment")
	}
}

func TestOriginalFundingSyntheticSettledFlagsNeverSatisfyStrictConsumer(t *testing.T) {
	f := machineFixture(t)
	pay := ids.New("pay")
	exec(t, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,
 channel_reference,product_profile_revision,channel_binding_snapshot,amount_fen,state)
 VALUES($1,$2,'ORDER',$2,'ALIPAY','SANDBOX',$3,$3,'SYNTHETIC_TEST','{"fixture":"NOT_CHANNEL_EVIDENCE"}',500,'SETTLED')`, pay, f.order.ID, ids.New("synthetic"))
	exec(t, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1`, f.order.ID)
	_, e := currentOriginalFunds(f.order.ID, pay, "SANDBOX")
	assertCode(t, e, fault.Conflict)
}

func fundingRoute(t *testing.T, f *machinePaymentFixture, p payments.Payment) routecontract.Contract {
	t.Helper()
	var frozen trade.Contract
	if e := json.Unmarshal(f.order.Contract, &frozen); e != nil {
		t.Fatal(e)
	}
	f.buyer.Scopes = append(f.buyer.Scopes, "route:create", "route:read")
	in := routecontract.Write{OrderID: f.order.ID, SellerID: f.order.SellerID, ListingID: frozen.ListingID, Revision: frozen.ListingRevision, Policy: frozen.Policy, PerCallFen: 100, TotalFen: 500, MaxCalls: 5, ExpiresAt: time.Now().Add(5 * time.Minute), PaymentID: p.ID}
	c, e := routeService().Create(ctx, f.buyer, in, ids.New("route"))
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestOriginalFundingRouteAndNewKeyFollowCurrentGrantRevocation(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	c := fundingRoute(t, f, p)
	s := identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "fund_test", Peppers: map[string][]byte{"fund_test": make([]byte, 32)}, MaxTTL: time.Hour}
	input := identity.KeyInput{Label: "Original funding bound key", Scopes: []string{"ROUTE_INVOKE"}, RouteIDs: []string{c.ID}, ExpiresAt: time.Now().Add(time.Minute)}
	created, e := s.Create(ctx, f.buyer, input, ids.New("key"))
	if e != nil {
		t.Fatal(e)
	}
	actor, e := s.Authenticate(ctx, created.SecretOnce)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = resolveRoute(actor, c.PublicModel, relay.ChatText); e != nil {
		t.Fatal("current original route", e)
	}
	exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE scope_key=$1`, "machine-pay:"+f.binding.Digest())
	if _, e = s.Create(ctx, f.buyer, input, ids.New("key")); e == nil {
		t.Fatal("new credential ignored original approval revocation")
	}
	if _, e = resolveRoute(actor, c.PublicModel, relay.ChatText); e == nil {
		t.Fatal("existing scoped key bypassed current funding guard")
	}
	assertFundingFact(t, f, p.ID, "READY", 1)
}

func TestOriginalFundingValidityClippedToCurrentApprovalExpiry(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	until := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	exec(t, `UPDATE p11.evidence SET valid_until=$2 WHERE id=$1`, f.policy.RefundEvidenceRef, until)
	g, e := currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	if e != nil || !g.ValidUntil.Equal(until) {
		t.Fatal("original grant outlived current independent approval", g.ValidUntil, e)
	}
}

// Bounded native SQL source helper: observe the actual backend lock wait rather
// than using a guessed sleep to order a financial race. Never called here.
func waitFundingRowBlocked(watch context.Context, pid uint32) (bool, error) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		e := db.Pool.QueryRow(watch, `SELECT COALESCE((SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=$1),false)`, pid).Scan(&waiting)
		if e != nil {
			return false, e
		}
		if waiting {
			return true, nil
		}
		select {
		case <-watch.Done():
			return false, watch.Err()
		case <-tick.C:
		}
	}
}

func TestOriginalFundingReaderUsesSnapshotAfterOriginalRefundWriterCommits(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	race, end := context.WithTimeout(context.Background(), 6*time.Second)
	defer end()
	writer, e := db.Pool.BeginTx(race, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Rollback(ctx)
	if _, e = writer.Exec(race, `SELECT id FROM p11.orders WHERE id=$1 FOR UPDATE`, f.order.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = writer.Exec(race, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state) VALUES($1,$2,$3,$4,1,'UNKNOWN')`, ids.New("ref"), p.ID, f.order.ID, ids.New("rkey")); e != nil {
		t.Fatal(e)
	}
	reader, e := db.Pool.BeginTx(race, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Rollback(ctx)
	var pid uint32
	if e = reader.QueryRow(race, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	finished := make(chan error, 1)
	go func() { _, err := fundguard.CurrentTx(race, reader, f.order.ID, p.ID, "SANDBOX"); finished <- err }()
	waiting, e := waitFundingRowBlocked(race, pid)
	if e != nil || !waiting {
		_ = writer.Rollback(ctx)
		end()
		<-finished
		t.Fatal("reader did not wait behind original parent", e)
	}
	if e = writer.Commit(race); e != nil {
		end()
		<-finished
		t.Fatal(e)
	}
	e = <-finished
	assertCode(t, e, fault.Conflict)
	assertFundingFact(t, f, p.ID, "READY", 1)
}

func TestOriginalFundingRefundTriggerCannotInsertAcrossActiveParentRead(t *testing.T) {
	f := machineFixture(t)
	p := f.paidOriginal(t)
	if _, e := f.machine.RecoverFundingOne(ctx); e != nil {
		t.Fatal(e)
	}
	race, end := context.WithTimeout(context.Background(), 6*time.Second)
	defer end()
	reader, e := db.Pool.BeginTx(race, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Rollback(ctx)
	if _, e = fundguard.CurrentTx(race, reader, f.order.ID, p.ID, "SANDBOX"); e != nil {
		t.Fatal(e)
	}
	writer, e := db.Pool.BeginTx(race, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Rollback(ctx)
	var pid uint32
	if e = writer.QueryRow(race, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := writer.Exec(race, `INSERT INTO p11.refunds(id,payment_id,order_id,original_request_key,amount_fen,state) VALUES($1,$2,$3,$4,1,'REQUESTED')`, ids.New("ref"), p.ID, f.order.ID, ids.New("rkey"))
		finished <- err
	}()
	waiting, e := waitFundingRowBlocked(race, pid)
	if e != nil || !waiting {
		_ = reader.Rollback(ctx)
		end()
		<-finished
		t.Fatal("direct refund insert bypassed original parent serialization", e)
	}
	if e = reader.Commit(race); e != nil {
		end()
		<-finished
		t.Fatal(e)
	}
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	if e = writer.Commit(race); e != nil {
		t.Fatal(e)
	}
	_, e = currentOriginalFunds(f.order.ID, p.ID, "SANDBOX")
	assertCode(t, e, fault.Conflict)
}

func TestOriginalFundingWorkerCannotClaimDifferentCapabilityOrPendingPayment(t *testing.T) {
	first := machineFixture(t)
	p := first.pending(t)
	if worked, e := first.machine.RecoverFundingOne(ctx); e != nil || worked {
		t.Fatal("pending payment became funding job", worked, e)
	}
	if _, e := first.machine.SubmitProof(ctx, first.buyer, p.ID, first.proof(t, "first_funding_scope"), ids.New("idem")); e != nil {
		t.Fatal(e)
	}
	if _, e := first.machine.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	second := machineFixture(t)
	if worked, e := second.machine.RecoverFundingOne(ctx); e != nil || worked || second.calls.Load() != 0 {
		t.Fatal("host claimed another original payment capability", worked, e)
	}
	assertFundingFact(t, first, p.ID, "WAITING_PAYMENT", 0)
}
