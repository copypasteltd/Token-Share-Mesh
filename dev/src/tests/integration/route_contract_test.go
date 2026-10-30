package integration_test

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"time"
)

type routeFixture struct {
	buyer identity.Principal
	offer testsupport.Offer
	order trade.Order
	in    routecontract.Write
}

func routeService() routecontract.Service {
	return routecontract.Service{DB: db, PaymentEnvironment: "SANDBOX"}
}

// READY and SETTLED are explicitly synthetic DB facts. No channel, phone or
// model is called. Actual quote, consent and original order use application code.
func routeOrder(t *testing.T, p identity.Principal, protocol relay.Profile) routeFixture {
	t.Helper()
	p.Scopes = append(p.Scopes, "route:create", "route:read")
	f := testsupport.SeedOfferForProtocol(t, ctx, db, "PLAINTEXT", ids.New("region"), string(protocol))
	return routeOrderOffer(t, p, f)
}

func routeOrderOffer(t *testing.T, p identity.Principal, f testsupport.Offer) routeFixture {
	t.Helper()
	p.Scopes = append(p.Scopes, "route:create", "route:read")
	s := trade.Service{DB: db}
	q, err := s.CreateQuote(ctx, p, f.Input(), ids.New("quote"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, p, confirm(q), ids.New("order"))
	if err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1`, o.ID)
	pay := ids.New("pay")
	exec(t, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,
channel_reference,product_profile_revision,channel_binding_snapshot,amount_fen,state)
VALUES($1,$2,'ORDER',$2,'ALIPAY','SANDBOX',$3,$3,'SYNTHETIC_TEST','{"fixture":"NOT_CHANNEL_EVIDENCE"}',500,'SETTLED')`, pay, o.ID, ids.New("synthetic"))
	return routeFixture{p, f, o, routecontract.Write{OrderID: o.ID, SellerID: f.Seller.UserID, ListingID: f.ID, Revision: 1, Policy: f.Terms.Policy, PerCallFen: 100, TotalFen: 500, MaxCalls: 5, ExpiresAt: time.Now().Add(10 * time.Minute), PaymentID: pay}}
}

func (f routeFixture) create(t *testing.T) routecontract.Contract {
	t.Helper()
	c, e := routeService().Create(ctx, f.buyer, f.in, ids.New("create"))
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func routeKey(t *testing.T, p identity.Principal, routes ...string) (identity.KeyService, identity.CreatedKey, identity.Principal) {
	t.Helper()
	s := identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "route_test", Peppers: map[string][]byte{"route_test": []byte(strings.Repeat("p", 32))}, MaxTTL: time.Hour}
	k, err := s.Create(ctx, p, identity.KeyInput{Label: "Bound standard SDK key", Scopes: []string{"ROUTE_INVOKE"}, RouteIDs: routes, ExpiresAt: time.Now().Add(time.Minute)}, ids.New("keycreate"))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(ctx, k.SecretOnce)
	if err != nil {
		t.Fatal(err)
	}
	return s, k, actor
}

func resolveRoute(p identity.Principal, model string, profile relay.Profile) (c routecontract.Contract, err error) {
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		c, e = routeService().ResolveTx(ctx, tx, p, model, profile)
		return e
	})
	return
}

func TestRouteContractFreezesOriginalOrderAndConcurrentReplay(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	type result struct {
		c routecontract.Contract
		e error
	}
	results := make(chan result, 12)
	for range 12 {
		go func() {
			c, e := routeService().Create(ctx, f.buyer, f.in, "route-concurrent-original")
			results <- result{c, e}
		}()
	}
	first, created := "", 0
	for range 12 {
		r := <-results
		if r.e != nil {
			t.Fatal(r.e)
		}
		if first == "" {
			first = r.c.ID
		}
		if r.c.ID != first || r.c.State != "ACTIVE" || r.c.PublicModel != f.offer.Terms.Spec.ModelID || r.c.RemainingFen != 500 || r.c.ProfileRevision != "1" {
			t.Fatal(r)
		}
		if !r.c.Replayed {
			created++
		}
	}
	var count int
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='RouteContractCreated'`, first).Scan(&count); e != nil || created != 1 || count != 1 {
		t.Fatal(created, count, e)
	}
	_, e := routeService().Create(ctx, f.buyer, f.in, "another-key-same-order")
	assertCode(t, e, fault.Conflict)
	bad := f.in
	bad.MaxCalls++
	_, e = routeService().Create(ctx, f.buyer, bad, "route-concurrent-original")
	assertCode(t, e, fault.Conflict)
}

func TestRouteContractRejectsCrossOwnerAndChangedConfirmation(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	other := buyer(t)
	other.Scopes = append(other.Scopes, "route:create", "route:read")
	_, e := routeService().Create(ctx, other, f.in, "foreign-order-route")
	assertCode(t, e, fault.NotFound)
	for _, change := range []func(*routecontract.Write){
		func(w *routecontract.Write) { w.SellerID = ids.New("seller") },
		func(w *routecontract.Write) { w.ListingID = ids.New("listing") },
		func(w *routecontract.Write) { w.Revision++ },
		func(w *routecontract.Write) { w.Policy.Protocol = string(relay.ResponsesText) },
		func(w *routecontract.Write) { w.Policy.Region = ids.New("region") },
		func(w *routecontract.Write) { w.TotalFen = 400 },
	} {
		bad := f.in
		change(&bad)
		_, e = routeService().Create(ctx, f.buyer, bad, ids.New("attempt"))
		assertCode(t, e, fault.Conflict)
	}
	api := f.buyer
	api.Kind = identity.APIKey
	_, e = routeService().Create(ctx, api, f.in, "no-sdk-auto-buy")
	assertCode(t, e, fault.Forbidden)
}

func TestRouteContractRequiresOriginalSettledEnvironmentAndNoRefund(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	for _, state := range []string{"CREATED", "PENDING", "AUTHORIZED", "UNKNOWN", "FAILED", "CLOSED"} {
		exec(t, `UPDATE p11.payments SET state=$1 WHERE id=$2`, state, f.in.PaymentID)
		_, e := routeService().Create(ctx, f.buyer, f.in, ids.New("route"))
		assertCode(t, e, fault.Conflict)
	}
	exec(t, `UPDATE p11.payments SET state='SETTLED' WHERE id=$1`, f.in.PaymentID)
	_, e := (routecontract.Service{DB: db, PaymentEnvironment: "PRODUCTION"}).Create(ctx, f.buyer, f.in, "no-environment-mixing")
	assertCode(t, e, fault.Conflict)
	_, e = (routecontract.Service{DB: db}).Create(ctx, f.buyer, f.in, "no-environment-default")
	assertCode(t, e, fault.Unavailable)
	for _, test := range []struct {
		amount  int64
		channel *string
		binding string
		until   *time.Time
	}{
		{499, ptrString(ids.New("synthetic_channel")), `{"fixture":"NOT_CHANNEL_EVIDENCE"}`, nil},
		{500, nil, `{"fixture":"NOT_CHANNEL_EVIDENCE"}`, nil},
		{500, ptrString(ids.New("synthetic_channel")), `{}`, nil},
		{500, ptrString(ids.New("synthetic_channel")), `{"fixture":"NOT_CHANNEL_EVIDENCE"}`, ptrTime(time.Now().Add(-time.Minute))},
	} {
		negative := routeOrder(t, buyer(t), relay.ChatText)
		exec(t, `UPDATE p11.payments SET state='CLOSED' WHERE id=$1`, negative.in.PaymentID)
		bad := negative.in
		bad.PaymentID = ids.New("pay")
		exec(t, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,
channel_reference,product_profile_revision,channel_binding_snapshot,amount_fen,state,funds_expires_at)
SELECT $1,order_id,purpose,purpose_ref,provider,environment,$2,$3,product_profile_revision,$4::jsonb,$5,'SETTLED',$6
FROM p11.payments WHERE id=$7`, bad.PaymentID, ids.New("synthetic"), test.channel, test.binding, test.amount, test.until, negative.in.PaymentID)
		_, e = routeService().Create(ctx, negative.buyer, bad, ids.New("route"))
		assertCode(t, e, fault.Conflict)
	}
	foreign := routeOrder(t, buyer(t), relay.ChatText)
	bad := f.in
	bad.PaymentID = foreign.in.PaymentID
	_, e = routeService().Create(ctx, f.buyer, bad, "foreign-payment-route")
	assertCode(t, e, fault.Conflict)
	exec(t, `INSERT INTO p11.refunds(id,order_id,payment_id,state,amount_fen,original_request_key) VALUES($1,$2,$3,'REQUESTED',1,$4)`, ids.New("refund"), f.order.ID, f.in.PaymentID, ids.New("synthetic"))
	_, e = routeService().Create(ctx, f.buyer, f.in, "refund-pending-route")
	assertCode(t, e, fault.Conflict)
}

func TestRouteContractBoundsDeadlineCapacityAndUnsupportedModes(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	for _, change := range []func(*routecontract.Write){
		func(w *routecontract.Write) { w.PerCallFen = 0 }, func(w *routecontract.Write) { w.PerCallFen = 501 },
		func(w *routecontract.Write) { w.MaxCalls = 0 }, func(w *routecontract.Write) { w.MaxCalls = 2147483648 },
		func(w *routecontract.Write) { w.Policy.Mode = "E2E"; w.Policy.Privacy = "PLATFORM_BLIND" },
		func(w *routecontract.Write) { w.Policy.Mode = "GPU_RENTAL" }, func(w *routecontract.Write) { w.Policy.Protocol = "openai_nb1" },
		func(w *routecontract.Write) { w.RecipientID = "recipient_not_implemented" },
	} {
		bad := f.in
		change(&bad)
		_, e := routeService().Create(ctx, f.buyer, bad, ids.New("reject"))
		assertCode(t, e, fault.Invalid)
	}
	for _, expiry := range []time.Time{time.Now().Add(-time.Minute), time.Now().Add(2 * time.Hour)} {
		bad := f.in
		bad.ExpiresAt = expiry
		_, e := routeService().Create(ctx, f.buyer, bad, ids.New("deadline"))
		assertCode(t, e, fault.Conflict)
	}
	exec(t, `UPDATE p11.orders SET state='WAITING_PAYMENT' WHERE id=$1`, f.order.ID)
	_, e := routeService().Create(ctx, f.buyer, f.in, "not-ready-original")
	assertCode(t, e, fault.Conflict)
	exec(t, `UPDATE p11.orders SET state='READY' WHERE id=$1`, f.order.ID)
	exec(t, `UPDATE p11.evidence SET valid_until=clock_timestamp()+interval '2 minutes' WHERE id=$1`, f.offer.Evidence[0])
	_, e = routeService().Create(ctx, f.buyer, f.in, "cannot-outlive-evidence")
	assertCode(t, e, fault.Conflict)
}

func TestRouteContractCurrentRevocationsAndReplayNeverRestoreActive(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c, e := routeService().Create(ctx, f.buyer, f.in, "current-route-original")
	if e != nil {
		t.Fatal(e)
	}
	// Old terms are append-only. A legitimate future version also cannot
	// rewrite, remap or cancel this already confirmed original-order route.
	if _, err := db.Pool.Exec(ctx, `UPDATE p11.listing_versions SET contract_template=jsonb_set(contract_template,'{public_spec,model_id}','"different_model"') WHERE listing_id=$1 AND revision=1`, f.offer.ID); err == nil {
		t.Fatal("rewrote immutable original listing version")
	}
	future := f.offer
	future.Terms.Spec.ModelID = "future_model"
	future.Version(t, ctx, db, 2)
	exec(t, `UPDATE p11.listings SET current_revision=2,version=version+1 WHERE id=$1`, f.offer.ID)
	changed, e := routeService().Get(ctx, f.buyer, c.ID)
	if e != nil || changed.State != "ACTIVE" || changed.PublicModel != c.PublicModel || changed.Definition.Revision != 1 {
		t.Fatal(changed, e)
	}
	exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, f.offer.Gate)
	r, e := routeService().Get(ctx, f.buyer, c.ID)
	if e != nil || r.State != "SUSPENDED" || r.RemainingFen != 0 {
		t.Fatal(r, e)
	}
	r, e = routeService().Create(ctx, f.buyer, f.in, "current-route-original")
	if e != nil || !r.Replayed || r.State != "SUSPENDED" {
		t.Fatal(r, e)
	}
	exec(t, `UPDATE p11.feature_gates SET enabled=true WHERE id=$1`, f.offer.Gate)
	exec(t, `UPDATE p11.payments SET state='UNKNOWN' WHERE id=$1`, f.in.PaymentID)
	r, e = routeService().Get(ctx, f.buyer, c.ID)
	if e != nil || r.State != "SUSPENDED" {
		t.Fatal(r, e)
	}
	exec(t, `UPDATE p11.payments SET state='SETTLED' WHERE id=$1`, f.in.PaymentID)
	exec(t, `UPDATE p11.orders SET state='CLOSED' WHERE id=$1`, f.order.ID)
	r, e = routeService().Get(ctx, f.buyer, c.ID)
	if e != nil || r.State != "SUSPENDED" {
		t.Fatal(r, e)
	}
	exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, f.buyer.UserID)
	_, e = routeService().Create(ctx, f.buyer, f.in, "current-route-original")
	assertCode(t, e, fault.Forbidden)
}

func TestRouteSDKUsesOnlyKeyBoundModelAndExactProtocol(t *testing.T) {
	requireDB(t)
	for _, profile := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		f := routeOrder(t, buyer(t), profile)
		c := f.create(t)
		_, _, p := routeKey(t, f.buyer, c.ID)
		r, e := resolveRoute(p, c.PublicModel, profile)
		if e != nil || r.ID != c.ID || r.Definition.SellerID != f.in.SellerID {
			t.Fatal(r, e)
		}
		_, e = resolveRoute(p, "another_model", profile)
		assertCode(t, e, fault.NotFound)
		wrong := relay.ChatText
		if profile == wrong {
			wrong = relay.MessagesText
		}
		_, e = resolveRoute(p, c.PublicModel, wrong)
		assertCode(t, e, fault.NotFound)
		_, e = resolveRoute(f.buyer, c.PublicModel, profile)
		assertCode(t, e, fault.Forbidden)
	}
}

func TestRouteSDKAmbiguityIncludesUnavailableSellerAndNeverFailsOver(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	a := f.create(t)
	g := routeOrder(t, f.buyer, relay.ChatText)
	b := g.create(t)
	_, _, p := routeKey(t, f.buyer, a.ID, b.ID)
	_, e := resolveRoute(p, a.PublicModel, relay.ChatText)
	assertCode(t, e, fault.Conflict)
	exec(t, `UPDATE p11.orders SET state='CLOSED' WHERE id=$1`, f.order.ID)
	_, e = resolveRoute(p, a.PublicModel, relay.ChatText)
	assertCode(t, e, fault.Conflict)
	_, _, p = routeKey(t, f.buyer, b.ID)
	r, e := resolveRoute(p, b.PublicModel, relay.ChatText)
	if e != nil || r.ID != b.ID {
		t.Fatal(r, e)
	}
}

func TestRouteSDKRechecksKeyRoleOrderAndSellerRevocation(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	s, k, p := routeKey(t, f.buyer, c.ID)
	exec(t, `UPDATE p11.devices SET resource_policy='{}' WHERE id=$1`, f.offer.Device)
	_, e := resolveRoute(p, c.PublicModel, relay.ChatText)
	assertCode(t, e, fault.Conflict)
	exec(t, `UPDATE p11.devices SET resource_policy='{"accepting_new_jobs":true}' WHERE id=$1`, f.offer.Device)
	if e = s.Revoke(ctx, f.buyer, k.Key.ID, "route-revoke-key"); e != nil {
		t.Fatal(e)
	}
	_, e = resolveRoute(p, c.PublicModel, relay.ChatText)
	assertCode(t, e, fault.Unauthenticated)
	_, _, p = routeKey(t, f.buyer, c.ID)
	p.RouteIDs = append(p.RouteIDs, ids.New("unbound"))
	_, e = resolveRoute(p, c.PublicModel, relay.ChatText)
	assertCode(t, e, fault.Unauthenticated)
}

func TestRouteSQLBindingsAreImmutableAndCannotCreateBareActiveRows(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	c := f.create(t)
	for _, sql := range []string{
		`DELETE FROM p11.route_contracts WHERE id=$1`,
		`UPDATE p11.route_contracts SET definition=jsonb_set(definition,'{max_call_count}','99'),version=version+1 WHERE id=$1`,
		`UPDATE p11.route_contracts SET expires_at=expires_at+interval '1 hour',version=version+1 WHERE id=$1`,
		`UPDATE p11.route_contracts SET state='ACTIVE',version=version+1 WHERE id=$1`,
	} {
		if _, e := db.Pool.Exec(ctx, sql, c.ID); e == nil {
			t.Fatal("mutable route", sql)
		}
	}
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.orders SET max_amount_fen=600 WHERE id=$1`, f.order.ID); e == nil {
		t.Fatal("rewrote order budget")
	}
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.payments SET environment='PRODUCTION' WHERE id=$1`, f.in.PaymentID); e == nil {
		t.Fatal("rewrote payment environment")
	}
	if _, e := db.Pool.Exec(ctx, `INSERT INTO p11.route_contracts(id,order_id,buyer_id,seller_id,contract_hash,protocol_profile,state,expires_at) VALUES($1,$2,$3,$4,$5,$6,'ACTIVE',clock_timestamp()+interval '1 minute')`, ids.New("bare"), f.order.ID, f.buyer.UserID, f.in.SellerID, c.ContractHash, f.in.Policy.Protocol); e == nil {
		t.Fatal("bare route accepted")
	}
	exec(t, `UPDATE p11.route_contracts SET state='REVOKED',version=version+1 WHERE id=$1`, c.ID)
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.route_contracts SET state='ACTIVE',version=version+1 WHERE id=$1`, c.ID); e == nil {
		t.Fatal("reactivated route")
	}
}

func TestRouteListIsOwnedBoundedAndSharesOriginalBudgetCapacity(t *testing.T) {
	requireDB(t)
	f := routeOrder(t, buyer(t), relay.ChatText)
	a := f.create(t)
	g := routeOrder(t, f.buyer, relay.ResponsesText)
	g.create(t)
	p, e := routeService().List(ctx, f.buyer, "", 1)
	if e != nil || len(p.Items) != 1 || p.NextCursor == nil {
		t.Fatal(p, e)
	}
	next, e := routeService().List(ctx, f.buyer, *p.NextCursor, 1)
	if e != nil || len(next.Items) != 1 || next.Items[0].ID == p.Items[0].ID {
		t.Fatal(next, e)
	}
	other := buyer(t)
	other.Scopes = append(other.Scopes, "route:read")
	_, e = routeService().Get(ctx, other, a.ID)
	assertCode(t, e, fault.NotFound)
	p, e = routeService().List(ctx, other, "", 20)
	if e != nil || len(p.Items) != 0 {
		t.Fatal(p, e)
	}
	scope := budget.Scope{OrderID: f.order.ID, BuyerID: f.buyer.UserID, SellerID: f.in.SellerID, ContractHash: a.ContractHash}
	j := ids.New("job")
	exec(t, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline) VALUES($1,$2,$3,$4,repeat('a',64),'QUEUED',clock_timestamp()+interval '1 hour')`, j, f.order.ID, f.in.SellerID, j)
	if _, e = reserveBudget(scope, j, 450); e != nil {
		t.Fatal(e)
	}
	r, e := routeService().Get(ctx, f.buyer, a.ID)
	if e != nil || r.State != "SUSPENDED" || r.RemainingFen != 0 || r.UsedCalls != 0 {
		t.Fatal(r, e)
	}
	raw, _ := json.Marshal(r)
	for _, secret := range []string{"channel_reference", "channel_binding_snapshot", "NOT_CHANNEL_EVIDENCE", "endpoint", "secret_ref", "SYNTHETIC_NOT_PUBLIC"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private payment/backend material exposed", secret)
		}
	}
}

func ptrString(v string) *string     { return &v }
func ptrTime(v time.Time) *time.Time { return &v }
