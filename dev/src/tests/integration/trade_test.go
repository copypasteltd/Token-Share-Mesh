package integration_test

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"sync"
	"testing"
	"time"
)

type offerFixture struct {
	seller                                          identity.Principal
	id, device, resource, capability, gate, release string
	evidence                                        []string
	terms                                           market.Terms
}

func buyer(t *testing.T) identity.Principal {
	p := user(t)
	p.Scopes = append(p.Scopes, "quote:create", "quote:read", "order:create", "order:read")
	return p
}

// These fixtures deliberately insert synthetic approvals into a disposable DB.
// They prove admission logic, not actual payment/SDK/device qualification.
func offer(t *testing.T, mode, region string) offerFixture {
	t.Helper()
	f := offerFixture{seller: buyer(t), id: ids.New("lst"), device: ids.New("dev"), resource: ids.New("res"), capability: ids.New("cap"), gate: ids.New("gate"), release: ids.New("rel")}
	exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'SELLER','self',$2)`, ids.New("grant"), f.seller.UserID)
	exec(t, `INSERT INTO p11.seller_profiles(id,state,legal_scope_ref,payee_binding_ref) VALUES($1,'APPROVED',$2,$3)`, f.seller.UserID, ids.New("legal"), ids.New("payee"))
	f.seller.Roles = append(f.seller.Roles, "SELLER")
	bps := 500
	f.terms = market.Terms{SKU: ids.New("sku"), SellerDisplayName: "星河同学", Policy: market.Policy{Mode: mode, Privacy: "STANDARD", Region: region, Protocol: "openai_nb1", Acceptance: "accept_nb1", Disclosure: "disclosure_nb1"}, DeadlineSec: 3600,
		Price:    pricing.Price{Version: pricing.Version, Currency: "CNY", Rounding: pricing.Rounding, CommissionBPS: &bps, ChannelCostOwner: "SELLER", FeePolicyVersion: "fee_nb1"},
		Bindings: []market.Binding{{DeviceID: f.device, CapabilityID: f.capability, EnvironmentRef: ids.New("env"), EnvironmentDigest: strings.Repeat("b", 64)}},
		Devices:  []market.DeviceDisplay{{DeviceID: f.device, ModelName: "RTX 4090 24GB"}}}
	platform, kind, engine := "WINDOWS", "GPU_DEVICE", "COMFYUI_NATIVE"
	kinds := []string{"LICENSE", "PAYMENT", "SDK", "PHYSICAL_TEST"}
	if mode == "GPU_RENTAL" {
		f.terms.Price.Unit = pricing.Hour
		f.terms.Price.HourRate = &pricing.Rate{NumeratorFen: 360, DenominatorUnits: 1}
		f.terms.Spec.DurationMS = 1800000
	} else {
		platform, kind, engine = "ANDROID", "ANDROID_DEVICE", "LMA_NATIVE"
		f.terms.SellerDisplayName = "阿澄"
		f.terms.Devices[0].ModelName = "摩托罗拉 X30"
		f.terms.Price.Unit = pricing.MillionTokens
		f.terms.Price.InputRate = &pricing.Rate{NumeratorFen: 80, DenominatorUnits: 1}
		f.terms.Price.OutputRate = &pricing.Rate{NumeratorFen: 160, DenominatorUnits: 1}
		f.terms.Spec = market.Spec{ModelID: "model_nb1", InputTokenLimit: 1000000, OutputTokenLimit: 250000}
		kinds = append(kinds, "USAGE")
		if mode == "E2E" {
			f.terms.Policy.Privacy = "PLATFORM_BLIND"
			kinds = append(kinds, "CRYPTO")
		}
	}
	exec(t, `INSERT INTO p11.devices(id,seller_id,platform,resource_kind,state,auth_key_digest,client_release,resource_policy,last_seen_at) VALUES($1,$2,$3,$4,'ONLINE',$5,'SYNTHETIC_TEST','{"accepting_new_jobs":true}',clock_timestamp())`, f.device, f.seller.UserID, platform, kind, outbox.Digest([]byte(f.device)))
	exec(t, `INSERT INTO p11.resources(id,device_id,seller_id,local_resource_key,kind) VALUES($1,$2,$3,'resource0',$4)`, f.resource, f.device, f.seller.UserID, kind)
	b := f.terms.Bindings[0]
	scope, err := json.Marshal(map[string]string{"seller_id": f.seller.UserID, "device_id": f.device, "capability_id": f.capability, "kernel_environment_ref": b.EnvironmentRef, "kernel_environment_digest": b.EnvironmentDigest, "mode": mode, "privacy": f.terms.Policy.Privacy, "region": region, "protocol_profile_id": f.terms.Policy.Protocol, "price_contract_version": pricing.Version, "sku_id": f.terms.SKU})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range kinds {
		id := ids.New("evd")
		f.evidence = append(f.evidence, id)
		exec(t, `INSERT INTO p11.evidence(id,evidence_type,seller_id,device_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,$2,$3,$4,'SYNTHETIC_NOT_PUBLIC',repeat('c',64),$5,'APPROVED',$3,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours')`, id, kind, f.seller.UserID, f.device, scope)
	}
	exec(t, `INSERT INTO p11.capabilities(id,device_id,seller_id,resource_id,engine_kind,engine_release,kernel_environment_ref,kernel_environment_digest,model_digest,recipe_digest,protocol_profile,mode,privacy,evidence_ids,state) VALUES($1,$2,$3,$4,$5,'SYNTHETIC_TEST',$6,$7,repeat('d',64),repeat('e',64),$8,$9,$10,$11,'QUALIFIED')`, f.capability, f.device, f.seller.UserID, f.resource, engine, b.EnvironmentRef, b.EnvironmentDigest, f.terms.Policy.Protocol, mode, f.terms.Policy.Privacy, f.evidence)
	exec(t, `INSERT INTO p11.feature_gates(id,scope_key,enabled,evidence_ids,approved_by) VALUES($1,$2,true,$3,$4)`, f.gate, f.gate, f.evidence, f.seller.UserID)
	exec(t, `INSERT INTO p11.releases(id,capability_id,gate_id,artifact_digest,scope_snapshot,evidence_ids,state,approved_by,expires_at) VALUES($1,$2,$3,repeat('f',64),$4,$5,'ACTIVE',$6,clock_timestamp()+interval '2 hours')`, f.release, f.capability, f.gate, scope, f.evidence, f.seller.UserID)
	exec(t, `INSERT INTO p11.listings(id,seller_id,state) VALUES($1,$2,'PUBLISHED')`, f.id, f.seller.UserID)
	f.version(t, 1)
	return f
}

func (f offerFixture) version(t *testing.T, revision int64) {
	t.Helper()
	raw, err := json.Marshal(f.terms)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, `INSERT INTO p11.listing_versions(listing_id,revision,seller_id,region,contract_template,price_version,valid_from,valid_until) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours')`, f.id, revision, f.seller.UserID, f.terms.Policy.Region, raw, pricing.Version)
	exec(t, `INSERT INTO p11.listing_devices(listing_id,revision,seller_id,device_id,capability_id) VALUES($1,$2,$3,$4,$5)`, f.id, revision, f.seller.UserID, f.device, f.capability)
}

func (f offerFixture) input() trade.QuoteInput {
	return trade.QuoteInput{ListingID: f.id, ListingRevision: 1, SKU: f.terms.SKU, Policy: f.terms.Policy, Spec: f.terms.Spec, BudgetFen: 500, Region: f.terms.Policy.Region, Quantity: 1}
}
func confirm(q trade.Quote) trade.OrderInput {
	return trade.OrderInput{QuoteID: q.ID, ContractHash: q.Contract.Hash, DisclosureVersion: q.Contract.Policy.Disclosure, DisclosureAcceptedAt: q.CreatedAt, CallerKey: ids.New("caller")}
}
func catalogItem(t *testing.T, f offerFixture) (market.Service, error) {
	t.Helper()
	var s market.Service
	err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		s, err = (market.Catalog{DB: db}).LoadTx(ctx, tx, f.id, 1)
		return err
	})
	return s, err
}

func TestMarketRequiresScopedApprovalsAndConcreteC2CModels(t *testing.T) {
	requireDB(t)
	region := ids.New("region")
	gpu, phone := offer(t, "GPU_RENTAL", region), offer(t, "E2E", region)
	catalog := market.Catalog{DB: db}
	page, err := catalog.List(ctx, market.Filter{Region: region}, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("first page", err, page)
	}
	next, err := catalog.List(ctx, market.Filter{Region: region}, *page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || page.Items[0].ListingID == next.Items[0].ListingID {
		t.Fatal("pagination", err)
	}
	_, err = catalog.List(ctx, market.Filter{Region: region, Mode: "E2E"}, *page.NextCursor, 1)
	assertCode(t, err, fault.Invalid)
	for _, f := range []offerFixture{gpu, phone} {
		s, err := catalogItem(t, f)
		if err != nil || s.SellerID != f.seller.UserID || s.SellerDisplayName != f.terms.SellerDisplayName || s.Devices[0].ModelName != f.terms.Devices[0].ModelName || s.Availability != "AVAILABLE" {
			t.Fatal(s, err)
		}
		raw, _ := json.Marshal(s)
		for _, secret := range []string{"auth_key_digest", "SYNTHETIC_NOT_PUBLIC", "payee_binding_ref", "external_subject_digest"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("private field exposed", secret)
			}
		}
	}
	exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, phone.gate)
	_, err = catalogItem(t, phone)
	assertCode(t, err, fault.NotFound)
	exec(t, `UPDATE p11.feature_gates SET enabled=true WHERE id=$1`, phone.gate)
	exec(t, `UPDATE p11.evidence SET scope_snapshot=scope_snapshot || '{"privacy":"STANDARD"}' WHERE id=$1`, phone.evidence[0])
	_, err = catalogItem(t, phone)
	assertCode(t, err, fault.NotFound)
	exec(t, `UPDATE p11.evidence SET scope_snapshot=scope_snapshot || '{"privacy":"PLATFORM_BLIND"}' WHERE id=$1`, phone.evidence[0])
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, phone.evidence[len(phone.evidence)-1])
	_, err = catalogItem(t, phone)
	assertCode(t, err, fault.NotFound)
	exec(t, `UPDATE p11.devices SET resource_policy='{}' WHERE id=$1`, gpu.device)
	s, err := catalogItem(t, gpu)
	if err != nil || s.Availability != "UNAVAILABLE" {
		t.Fatal("missing admission policy must fail closed", s, err)
	}
	exec(t, `UPDATE p11.devices SET resource_policy='{"accepting_new_jobs":true}' WHERE id=$1`, gpu.device)
	exec(t, `INSERT INTO p11.resource_locks(resource_id,phase) VALUES($1,'QUARANTINED')`, gpu.resource)
	s, err = catalogItem(t, gpu)
	if err != nil || s.Availability != "BUSY" {
		t.Fatal("quarantine must prevent purchase", s, err)
	}
	_, err = (trade.Service{DB: db}).CreateQuote(ctx, buyer(t), gpu.input(), "busy-resource-quote")
	assertCode(t, err, fault.Conflict)
}

func TestHourlyAndDualRateQuotesFreezeSellerBudgetAndPolicy(t *testing.T) {
	requireDB(t)
	svc, p := trade.Service{DB: db}, buyer(t)
	gpu, phone := offer(t, "GPU_RENTAL", ids.New("reg")), offer(t, "E2E", ids.New("reg"))
	for _, f := range []offerFixture{gpu, phone} {
		q, err := svc.CreateQuote(ctx, p, f.input(), "frozen-quote-001")
		if err != nil {
			t.Fatal(err)
		}
		want := int64(180)
		if f.terms.Policy.Mode == "E2E" {
			want = 120
		}
		hash, _ := q.Contract.Digest()
		if int64(q.Contract.SellerPriceFen) != want || q.Contract.SellerID != f.seller.UserID || q.Contract.Hash != hash || q.Contract.BudgetFen != 500 || q.Contract.BuyerFeeFen != 0 {
			t.Fatal("quote drift", q)
		}
		replay, err := svc.CreateQuote(ctx, p, f.input(), "frozen-quote-001")
		if err != nil || !replay.Replayed || replay.ID != q.ID {
			t.Fatal("quote replay", err)
		}
		bad := f.input()
		bad.BudgetFen = 1
		_, err = svc.CreateQuote(ctx, p, bad, "insufficient-budget")
		assertCode(t, err, fault.Invalid)
		_, err = svc.CreateQuote(ctx, p, bad, "frozen-quote-001")
		assertCode(t, err, fault.Conflict)
		bad = f.input()
		bad.Policy.Privacy = "ATTESTED_CONFIDENTIAL"
		_, err = svc.CreateQuote(ctx, p, bad, "no-privacy-downgrade")
		assertCode(t, err, fault.Conflict)
		bad = f.input()
		bad.Quantity = 2
		_, err = svc.CreateQuote(ctx, p, bad, "unsupported-quantity")
		assertCode(t, err, fault.Invalid)
		bad = f.input()
		bad.Spec.OutputTokenLimit++
		_, err = svc.CreateQuote(ctx, p, bad, "unsupported-extra-charge")
		assertCode(t, err, fault.Invalid)
		api := p
		api.Kind = identity.APIKey
		_, err = svc.CreateQuote(ctx, api, f.input(), "no-api-purchase")
		assertCode(t, err, fault.Forbidden)
		_, err = svc.CreateQuote(ctx, f.seller, f.input(), "no-self-purchase")
		assertCode(t, err, fault.Forbidden)
	}
	// Quote validity cannot outlive the earliest required evidence.
	exec(t, `UPDATE p11.evidence SET valid_until=clock_timestamp()+interval '30 seconds' WHERE id=$1`, phone.evidence[0])
	q, err := svc.CreateQuote(ctx, p, phone.input(), "short-evidence-validity")
	if err != nil || q.ExpiresAt.Sub(q.CreatedAt) > 31*time.Second {
		t.Fatal("quote outlives qualification", err)
	}
}

func TestOrderConfirmationConcurrentReplayAndOwnerIsolation(t *testing.T) {
	requireDB(t)
	f, p, svc := offer(t, "GPU_RENTAL", ids.New("reg")), buyer(t), trade.Service{DB: db}
	q, err := svc.CreateQuote(ctx, p, f.input(), "order-quote-001")
	if err != nil {
		t.Fatal(err)
	}
	input := confirm(q)
	bad := input
	bad.ContractHash = strings.Repeat("0", 64)
	_, err = svc.CreateOrder(ctx, p, bad, "bad-contract-confirmation")
	assertCode(t, err, fault.Conflict)
	bad = input
	bad.DisclosureAcceptedAt = q.CreatedAt.Add(-time.Second)
	_, err = svc.CreateOrder(ctx, p, bad, "bad-disclosure-time")
	assertCode(t, err, fault.Conflict)
	results := make(chan trade.Order, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { o, e := svc.CreateOrder(ctx, p, input, "one-order-intent"); results <- o; errs <- e })
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	fresh := 0
	for o := range results {
		if id == "" {
			id = o.ID
		}
		var frozen trade.Contract
		if json.Unmarshal(o.Contract, &frozen) != nil || id != o.ID || o.State != "WAITING_PAYMENT" || frozen.Hash != q.Contract.Hash {
			t.Fatal("duplicate or drifted order", o)
		}
		if !o.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal("fresh mutations", fresh)
	}
	var orders, events, consents int
	if err = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.orders WHERE quote_id=$1),(SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$2),(SELECT count(*) FROM p11.audit_events WHERE target_ref=$2 AND action='ORDER_CONTRACT_CONFIRMED')`, q.ID, id).Scan(&orders, &events, &consents); err != nil || orders != 1 || events != 1 || consents != 1 {
		t.Fatal("non-atomic order side effects", orders, events, consents, err)
	}
	for _, owner := range []identity.Principal{p, f.seller} {
		o, e := svc.GetOrder(ctx, owner, id)
		if e != nil || o.ID != id {
			t.Fatal("owner read", e)
		}
	}
	_, err = svc.GetOrder(ctx, buyer(t), id)
	assertCode(t, err, fault.NotFound)
	_, err = svc.GetQuote(ctx, buyer(t), q.ID)
	assertCode(t, err, fault.NotFound)
	consumed, err := svc.GetQuote(ctx, p, q.ID)
	if err != nil || consumed.State != "CONSUMED" {
		t.Fatal(consumed, err)
	}
	exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, p.UserID)
	_, err = svc.CreateOrder(ctx, p, input, "one-order-intent")
	assertCode(t, err, fault.Forbidden)
	_, err = svc.CreateQuote(ctx, p, f.input(), "order-quote-001")
	assertCode(t, err, fault.Forbidden)
	_, err = svc.GetOrder(ctx, p, id)
	assertCode(t, err, fault.Forbidden)
}

func TestQuoteSingleConsumptionAcrossDifferentOrderKeysAndRollback(t *testing.T) {
	requireDB(t)
	f, p, svc := offer(t, "E2E", ids.New("reg")), buyer(t), trade.Service{DB: db}
	q, err := svc.CreateQuote(ctx, p, f.input(), "unique-quote-001")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(q.Contract)
	// This deliberately bypasses the service. The DB must reject fee drift,
	// rolling back the quote-consumption trigger in the same statement.
	_, err = db.Pool.Exec(ctx, `INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,fee_bps,contract_snapshot,contract_hash,deadline) VALUES($1,$2,$3,$4,'WAITING_PAYMENT','E2E','PLATFORM_BLIND',500,100,$5,$6,clock_timestamp()+interval '1 hour')`, ids.New("ord"), q.ID, p.UserID, f.seller.UserID, raw, q.Contract.Hash)
	if err == nil {
		t.Fatal("DB accepted fee drift")
	}
	read, err := svc.GetQuote(ctx, p, q.ID)
	if err != nil || read.State != "VALID" {
		t.Fatal("failed order consumed quote", read, err)
	}
	input := confirm(q)
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for n := range 12 {
		wg.Go(func() { _, e := svc.CreateOrder(ctx, p, input, fmt.Sprintf("different-intent-%02d", n)); errs <- e })
	}
	wg.Wait()
	close(errs)
	ok, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if fault.Public(e).Code == fault.Conflict {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 11 {
		t.Fatal("quote reused", ok, conflicts)
	}
}

func TestNewListingRevisionPreservesHistoricalQuoteAndRejectsStalePurchase(t *testing.T) {
	requireDB(t)
	f, p, svc := offer(t, "GPU_RENTAL", ids.New("reg")), buyer(t), trade.Service{DB: db}
	q, err := svc.CreateQuote(ctx, p, f.input(), "old-revision-quote")
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE p11.listing_versions SET price_version='changed' WHERE listing_id=$1`, `DELETE FROM p11.listing_versions WHERE listing_id=$1`, `DELETE FROM p11.listing_devices WHERE listing_id=$1`} {
		if _, err = db.Pool.Exec(ctx, sql, f.id); err == nil {
			t.Fatal("frozen listing mutated")
		}
	}
	f.terms.Price.HourRate.NumeratorFen = 350
	f.version(t, 2)
	exec(t, `UPDATE p11.listings SET current_revision=2,version=version+1 WHERE id=$1`, f.id)
	_, err = svc.CreateOrder(ctx, p, confirm(q), "old-revision-order")
	assertCode(t, err, fault.NotFound)
	old, err := svc.GetQuote(ctx, p, q.ID)
	if err != nil || old.Contract.Price.HourRate.NumeratorFen != 360 || old.Contract.Hash != q.Contract.Hash {
		t.Fatal("old quote was rewritten", err)
	}
	in := f.input()
	in.ListingRevision = 2
	next, err := svc.CreateQuote(ctx, p, in, "new-revision-quote")
	if err != nil || next.Contract.SellerPriceFen != 175 || next.Contract.Price.HourRate.NumeratorFen != 350 {
		t.Fatal("nonintegral minute hourly price", next, err)
	}
	if _, err = next.Contract.Price.LegacyMinuteRate(); err == nil {
		t.Fatal("lossy minute conversion")
	}
}

func TestOrderRechecksDeviceEvidenceAndBuyerConsentBeforeMutation(t *testing.T) {
	requireDB(t)
	f, p, svc := offer(t, "E2E", ids.New("reg")), buyer(t), trade.Service{DB: db}
	q, err := svc.CreateQuote(ctx, p, f.input(), "recheck-quote")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE p11.devices SET resource_policy='{"accepting_new_jobs":false}' WHERE id=$1`, f.device)
	_, err = svc.CreateOrder(ctx, p, confirm(q), "stopped-new-orders")
	assertCode(t, err, fault.Conflict)
	exec(t, `UPDATE p11.devices SET resource_policy='{"accepting_new_jobs":true}' WHERE id=$1`, f.device)
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.evidence[0])
	_, err = svc.CreateOrder(ctx, p, confirm(q), "revoked-license-order")
	assertCode(t, err, fault.NotFound)
	exec(t, `UPDATE p11.evidence SET decision='APPROVED' WHERE id=$1`, f.evidence[0])
	input := confirm(q)
	input.ReplacesOrderID = ids.New("ord")
	_, err = svc.CreateOrder(ctx, p, input, "no-implicit-cross-seller-replacement")
	assertCode(t, err, fault.Invalid)
	input = confirm(q)
	api := p
	api.Kind = identity.APIKey
	_, err = svc.CreateOrder(ctx, api, input, "no-api-order")
	assertCode(t, err, fault.Forbidden)
	read, err := svc.GetQuote(ctx, p, q.ID)
	if err != nil || read.State != "VALID" {
		t.Fatal("rejected admission consumed quote", err)
	}
}

func TestHistoricalOrderProjectionRetainsLegacyPriceFields(t *testing.T) {
	requireDB(t)
	f, p, svc := offer(t, "GPU_RENTAL", ids.New("reg")), buyer(t), trade.Service{DB: db}
	legacy := map[string]any{"seller_id": f.seller.UserID, "listing_id": f.id, "listing_revision": 1, "policy": f.terms.Policy, "price": map[string]any{"currency": "CNY", "unit": "AVAILABLE_MINUTE", "unit_price_fen": 6, "commission_bps": 500, "channel_cost_owner": "SELLER", "fee_policy_version": "legacy_fee_1"}, "contract_hash": strings.Repeat("a", 64), "historical_extension": "retain_exact_field"}
	raw, _ := json.Marshal(legacy)
	qid, oid := ids.New("quo"), ids.New("ord")
	exec(t, `INSERT INTO p11.quotes(id,buyer_id,seller_id,listing_id,listing_revision,contract_snapshot,contract_hash,max_amount_fen,expires_at) VALUES($1,$2,$3,$4,1,$5,repeat('a',64),500,clock_timestamp()+interval '1 hour')`, qid, p.UserID, f.seller.UserID, f.id, raw)
	exec(t, `INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,fee_bps,contract_snapshot,contract_hash,deadline) VALUES($1,$2,$3,$4,'WAITING_PAYMENT','GPU_RENTAL','STANDARD',500,500,$5,repeat('a',64),clock_timestamp()+interval '1 hour')`, oid, qid, p.UserID, f.seller.UserID, raw)
	o, err := svc.GetOrder(ctx, p, oid)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(o.Contract, &got) != nil || got["historical_extension"] != "retain_exact_field" {
		t.Fatal("historical fields lost")
	}
	price := got["price"].(map[string]any)
	if price["unit"] != "AVAILABLE_MINUTE" || price["unit_price_fen"] != float64(6) || price["price_contract_version"] != nil {
		t.Fatal("historical price reinterpreted", price)
	}
	// Until a historical quote UI exists, refuse rather than returning a
	// new-sale struct with the old rate silently removed.
	_, err = svc.GetQuote(ctx, p, qid)
	assertCode(t, err, fault.Unavailable)
}
