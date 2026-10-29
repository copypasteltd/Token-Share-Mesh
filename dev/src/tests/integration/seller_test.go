package integration_test

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/seller"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"reflect"
	"sync"
	"testing"
	"time"
)

func sellerService() seller.Service {
	return seller.Service{DB: db, Fees: &seller.FeePolicy{BPS: 500, Version: "fee_nb1", ChannelCostOwner: "SELLER"}}
}
func sellerActor(f offerFixture) identity.Principal {
	p := f.seller
	p.Scopes = append(p.Scopes, "seller:read", "seller:manage", "listing:write", "seller:apply", "identity:consent")
	return p
}
func definition(f offerFixture) seller.Definition {
	return seller.Definition{Title: "个人闲置算力", SKU: f.terms.SKU, Devices: []string{f.device}, Policy: f.terms.Policy, Spec: f.terms.Spec, Price: f.terms.Price,
		Windows: []seller.Window{{StartsAt: time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond), EndsAt: time.Now().UTC().Add(90 * time.Minute).Truncate(time.Millisecond)}}, Capacity: 1, Evidence: f.evidence}
}
func createOwnListing(t *testing.T, f offerFixture) seller.Listing {
	t.Helper()
	l, err := sellerService().CreateListing(ctx, sellerActor(f), definition(f), "listing-create-"+ids.New("intent"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSellerApplicationAndConsentAreIdempotentUntrustedSubmissions(t *testing.T) {
	requireDB(t)
	p := user(t)
	p.Scopes = append(p.Scopes, "seller:apply", "seller:read", "seller:manage", "identity:consent")
	in := seller.Application{Identity: ids.New("claim"), Rights: []string{ids.New("claim")}, Payee: ids.New("claim"), Regions: []string{"CN_TEST"}, Agreement: "seller_terms_v1", AcceptedAt: time.Now().UTC()}
	svc := sellerService()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := svc.Apply(ctx, p, in, "same-application-intent")
			if err == nil && (v.State != "SUBMITTED" || len(v.Regions) != 0 || v.PayeeState != "UNVERIFIED") {
				err = fmt.Errorf("submitted claim acquired authority")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var apps, roles, events int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.seller_applications WHERE seller_id=$1", p.UserID).Scan(&apps); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.role_grants WHERE user_id=$1 AND role_code='SELLER'", p.UserID).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='SellerApplicationSubmitted'", p.UserID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if apps != 1 || roles != 0 || events != 1 {
		t.Fatal("application did not commit exactly one unapproved intent", apps, roles, events)
	}
	in.Regions = []string{"other"}
	_, err := svc.Apply(ctx, p, in, "same-application-intent")
	assertCode(t, err, fault.Conflict)
	if _, err = db.Pool.Exec(ctx, "UPDATE p11.seller_applications SET submitted_claims='{}' WHERE seller_id=$1", p.UserID); err == nil {
		t.Fatal("application evidence was mutable")
	}
	accepted := true
	c := seller.Consent{Document: "privacy_v1", Purpose: "PRIVACY", Scope: ids.New("scope"), Accepted: &accepted, ConfirmedAt: time.Now().UTC()}
	first, err := svc.RecordConsent(ctx, p, c, "explicit-consent-intent")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.RecordConsent(ctx, p, c, "explicit-consent-intent")
	if err != nil || again != first {
		t.Fatal("consent replay drift", err)
	}
	accepted = false
	withdraw, err := svc.RecordConsent(ctx, p, c, "withdraw-consent-intent")
	if err != nil || withdraw.ID == first.ID || withdraw.Status != "WITHDRAW" {
		t.Fatal("withdraw did not append", err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE p11.consents SET decision='WITHDRAW' WHERE id=$1", first.ID); err == nil {
		t.Fatal("original consent was mutable")
	}
	c.Accepted = nil
	_, err = svc.RecordConsent(ctx, p, c, "no-implicit-false-consent")
	assertCode(t, err, fault.Invalid)
	api := p
	api.Kind = identity.APIKey
	_, err = svc.Apply(ctx, api, in, "api-cannot-apply")
	assertCode(t, err, fault.Forbidden)
}

func TestListingSubmissionNeverAutoPublishesAndRechecksEvidence(t *testing.T) {
	requireDB(t)
	f := offer(t, "E2E", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	l := createOwnListing(t, f)
	if l.State != "DRAFT" || l.Version != 1 || l.Definition.Price.Unit != pricing.MillionTokens {
		t.Fatal(l)
	}
	reason := seller.Reason{Code: "owner_confirmed", ExpectedVersion: l.Version}
	exec(t, "UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1", f.evidence[0])
	_, err := svc.ChangeListingState(ctx, p, l.ID, reason, "submit-revoked-evidence", true)
	assertCode(t, err, fault.NotFound)
	after, err := svc.GetListing(ctx, p, l.ID)
	if err != nil || after.Version != 1 || after.State != "DRAFT" {
		t.Fatal("failed admission mutated listing", err)
	}
	exec(t, "UPDATE p11.evidence SET decision='APPROVED' WHERE id=$1", f.evidence[0])
	submitted, err := svc.ChangeListingState(ctx, p, l.ID, reason, "submit-current-evidence", true)
	if err != nil || submitted.State != "SUBMITTED" || submitted.Version != 2 {
		t.Fatal(submitted, err)
	}
	page, err := (market.Catalog{DB: db}).List(ctx, market.Filter{Region: f.terms.Policy.Region}, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ListingID == l.ID {
			t.Fatal("submission granted public sale")
		}
	}
	submitted2, err := svc.ChangeListingState(ctx, p, l.ID, reason, "submit-current-evidence", true)
	if err != nil || !submitted2.Replayed || submitted2.Version != 2 {
		t.Fatal("submit replay", err)
	}
	var content string
	if err = db.Pool.QueryRow(ctx, "SELECT safe_facts->>'reason_code' FROM p11.audit_events WHERE action='ListingSubmitted' AND target_ref=$1", l.ID).Scan(&content); err != nil || content != "owner_confirmed" {
		t.Fatal("reason lost", err)
	}
}

func TestListingFutureRevisionsPreserveSoldContractsAndRejectStaleETags(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	l := createOwnListing(t, f)
	// Explicit synthetic publication in this disposable test DB; no runtime API
	// silently approves or publishes a seller's submission.
	exec(t, "UPDATE p11.listings SET state='PUBLISHED' WHERE id=$1", l.ID)
	buyer := buyer(t)
	tradeSvc := trade.Service{DB: db}
	input := f.input()
	input.ListingID = l.ID
	q, err := tradeSvc.CreateQuote(ctx, buyer, input, "quote-before-listing-edit")
	if err != nil {
		t.Fatal(err)
	}
	order, err := tradeSvc.CreateOrder(ctx, buyer, confirm(q), "order-before-listing-edit")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := tradeSvc.GetOrder(ctx, buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := string(persisted.Contract)
	price := l.Definition.Price
	price.HourRate = &pricing.Rate{NumeratorFen: 350, DenominatorUnits: 1}
	updated, err := svc.UpdateListing(ctx, p, l.ID, 1, seller.Patch{Price: &price}, "future-price-edit")
	if err != nil || updated.Revision != 2 || updated.Version != 2 || updated.State != "DRAFT" {
		t.Fatal(updated, err)
	}
	again, err := tradeSvc.GetOrder(ctx, buyer, order.ID)
	if err != nil || string(again.Contract) != old {
		t.Fatal("sold contract was rewritten", err)
	}
	_, err = svc.UpdateListing(ctx, p, l.ID, 1, seller.Patch{Price: &price}, "other-stale-price-edit")
	assertCode(t, err, fault.Conflict)
	replay, err := svc.UpdateListing(ctx, p, l.ID, 1, seller.Patch{Price: &price}, "future-price-edit")
	if err != nil || !replay.Replayed || replay.Revision != 2 {
		t.Fatal("future revision replay", err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE p11.listing_definitions SET definition='{}' WHERE listing_id=$1", l.ID); err == nil {
		t.Fatal("listing definition mutable")
	}
	if _, err = db.Pool.Exec(ctx, "DELETE FROM p11.listing_versions WHERE listing_id=$1 AND revision=1", l.ID); err == nil {
		t.Fatal("old listing version deleted")
	}
	var versions int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.listing_versions WHERE listing_id=$1", l.ID).Scan(&versions); err != nil || versions != 2 {
		t.Fatal("version cardinality", versions, err)
	}
}

func TestConcurrentListingEditsCommitOneRevisionAndRevocationBlocksReplay(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	l := createOwnListing(t, f)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			title := fmt.Sprintf("未来挂牌%d", i)
			_, err := svc.UpdateListing(ctx, p, l.ID, 1, seller.Patch{Title: &title}, fmt.Sprintf("distinct-edit-intent-%d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			assertCode(t, err, fault.Conflict)
		}
	}
	if success != 1 {
		t.Fatal("CAS failed", success)
	}
	title := "第三修订"
	next, err := svc.UpdateListing(ctx, p, l.ID, 2, seller.Patch{Title: &title}, "revocable-listing-edit")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='SELLER'", p.UserID)
	_, err = svc.UpdateListing(ctx, p, l.ID, 2, seller.Patch{Title: &title}, "revocable-listing-edit")
	assertCode(t, err, fault.Forbidden)
	if next.Revision != 3 {
		t.Fatal(next)
	}
}

func TestListingOwnershipFeeRegionAndEnvironmentCannotBeClientForged(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	other := offer(t, "GPU_RENTAL", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	in := definition(f)
	in.Devices = []string{other.device}
	_, err := svc.CreateListing(ctx, p, in, "foreign-device-listing")
	assertCode(t, err, fault.NotFound)
	in = definition(f)
	in.Evidence = other.evidence
	_, err = svc.CreateListing(ctx, p, in, "foreign-evidence-listing")
	assertCode(t, err, fault.NotFound)
	in = definition(f)
	zero := 0
	in.Price.CommissionBPS = &zero
	_, err = svc.CreateListing(ctx, p, in, "forged-platform-fee")
	assertCode(t, err, fault.Conflict)
	in = definition(f)
	in.Policy.Region = "not_approved"
	_, err = svc.CreateListing(ctx, p, in, "unapproved-region-listing")
	assertCode(t, err, fault.Forbidden)
	in = definition(f)
	closed := svc
	closed.Fees = nil
	_, err = closed.CreateListing(ctx, p, in, "missing-platform-fee")
	assertCode(t, err, fault.Unavailable)
	exec(t, "UPDATE p11.devices SET model_name='Android设备' WHERE id=$1", f.device)
	_, err = svc.CreateListing(ctx, p, in, "generic-model-rejected")
	assertCode(t, err, fault.Invalid)
	exec(t, "UPDATE p11.devices SET model_name='RTX 4090 24GB' WHERE id=$1", f.device)
	l := createOwnListing(t, f)
	_, err = svc.GetListing(ctx, sellerActor(other), l.ID)
	assertCode(t, err, fault.NotFound)
	exec(t, "UPDATE p11.capabilities SET kernel_environment_digest=repeat('e',64) WHERE id=$1", f.capability)
	_, err = svc.ChangeListingState(ctx, p, l.ID, seller.Reason{Code: "submit_current", ExpectedVersion: 1}, "changed-environment-submit", true)
	assertCode(t, err, fault.Conflict)
}

func TestListingWindowsRejectGapSalesAndBoundRemainingExecution(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	in := definition(f)
	in.Spec.DurationMS = 1000
	now := time.Now().UTC()
	in.Windows = []seller.Window{{StartsAt: now.Add(-time.Hour), EndsAt: now.Add(30 * time.Second)}, {StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)}}
	l, err := svc.CreateListing(ctx, p, in, "availability-window-listing")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "UPDATE p11.listings SET state='PUBLISHED' WHERE id=$1", l.ID)
	var current market.Service
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		current, err = (market.Catalog{DB: db}).LoadTx(ctx, tx, l.ID, 1)
		return err
	})
	if err != nil || current.Availability != "AVAILABLE" || current.DeadlineSec > 30 || current.ValidUntil.After(now.Add(31*time.Second)) {
		t.Fatal("active window did not bound execution", current, err)
	}
	// New revision puts now in the gap; broad first/last validity must not sell it.
	in.Windows = []seller.Window{{StartsAt: now.Add(-time.Hour), EndsAt: now.Add(10 * time.Second)}, {StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)}}
	in.Spec.DurationMS = 15000
	// Each declared window is long enough, but too little time remains now.
	next, err := svc.UpdateListing(ctx, p, l.ID, 1, seller.Patch{Windows: &in.Windows, Spec: &in.Spec}, "too-little-current-window")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "UPDATE p11.listings SET state='PUBLISHED' WHERE id=$1", l.ID)
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		current, err = (market.Catalog{DB: db}).LoadTx(ctx, tx, l.ID, next.Revision)
		return err
	})
	if err != nil || current.Availability != "UNAVAILABLE" {
		t.Fatal("insufficient remaining window was sold", err)
	}
	in.Spec.DurationMS = 1
	instant := time.Now().UTC()
	in.Windows = []seller.Window{{StartsAt: instant.Add(-time.Hour), EndsAt: instant.Add(1500 * time.Millisecond)}, {StartsAt: instant.Add(time.Hour), EndsAt: instant.Add(2 * time.Hour)}}
	gap, err := svc.UpdateListing(ctx, p, l.ID, 2, seller.Patch{Windows: &in.Windows, Spec: &in.Spec}, "actual-gap-window")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "UPDATE p11.listings SET state='PUBLISHED' WHERE id=$1", l.ID)
	time.Sleep(1600 * time.Millisecond)
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		current, err = (market.Catalog{DB: db}).LoadTx(ctx, tx, l.ID, gap.Revision)
		return err
	})
	if err != nil || current.Availability != "UNAVAILABLE" {
		t.Fatal("gap between windows was sold", err)
	}

}

func TestSellerExitStopsNewAdmissionWithoutErasingOrdersOrResourceFences(t *testing.T) {
	requireDB(t)
	f := offer(t, "GPU_RENTAL", ids.New("reg"))
	p := sellerActor(f)
	svc := sellerService()
	l := createOwnListing(t, f)
	b := buyer(t)
	q, err := (trade.Service{DB: db}).CreateQuote(ctx, b, f.input(), "quote-before-seller-exit")
	if err != nil {
		t.Fatal(err)
	}
	o, err := (trade.Service{DB: db}).CreateOrder(ctx, b, confirm(q), "order-before-seller-exit")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "INSERT INTO p11.resource_locks(resource_id,phase,lock_generation) VALUES($1,'QUARANTINED',77)", f.resource)
	profile, err := svc.Profile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	// SELLER is independent of BUYER: revoking buying permission must not trap
	// the owner into continuing to offer idle hardware.
	exec(t, "UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'", p.UserID)
	ack, replayed, err := svc.Exit(ctx, p, seller.Reason{Code: "owner_stops_sharing", ExpectedVersion: profile.Version}, "seller-exit-intent")
	if err != nil || ack.Status != "EXITING" || replayed {
		t.Fatal(ack, err)
	}
	_, replayed, err = svc.Exit(ctx, p, seller.Reason{Code: "owner_stops_sharing", ExpectedVersion: profile.Version}, "seller-exit-intent")
	if err != nil || !replayed {
		t.Fatal("exit replay", err)
	}
	_, err = svc.CreateListing(ctx, p, definition(f), "no-new-listing-after-exit")
	assertCode(t, err, fault.Forbidden)
	_, err = (trade.Service{DB: db}).CreateQuote(ctx, b, f.input(), "no-new-purchase-after-exit")
	assertCode(t, err, fault.NotFound)
	historical, err := (trade.Service{DB: db}).GetOrder(ctx, b, o.ID)
	var before, after any
	json.Unmarshal(o.Contract, &before)
	json.Unmarshal(historical.Contract, &after)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("history erased", err)
	}
	var generation int64
	var phase string
	var accepting bool
	if err = db.Pool.QueryRow(ctx, "SELECT lock_generation,phase FROM p11.resource_locks WHERE resource_id=$1", f.resource).Scan(&generation, &phase); err != nil || generation != 77 || phase != "QUARANTINED" {
		t.Fatal("resource fence changed", err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT (resource_policy->>'accepting_new_jobs')::boolean FROM p11.devices WHERE id=$1", f.device).Scan(&accepting); err != nil || accepting {
		t.Fatal("new device admission remained open", err)
	}
	own, err := svc.GetListing(ctx, p, l.ID)
	if err != nil || own.State != "DRAFT" {
		t.Fatal("owner lost historical read", err)
	}
	raw, _ := json.Marshal(ack)
	if string(raw) == "" {
		t.Fatal("missing safe receipt")
	}
}
