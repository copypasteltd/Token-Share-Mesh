package api_test

import (
	"fmt"
	"net/http"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/seller"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/ids"
	"testing"
	"time"
)

func TestHTTPApplicantCanReadPendingProfileWithoutObtainingSellerAuthority(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, _ := h.login(t, "MINIAPP")
	headers := bearer(session.AccessToken)
	headers.Set("Idempotency-Key", "applicant-original-intent")
	in := seller.Application{Identity: ids.New("claim"), Rights: []string{ids.New("claim")}, Payee: ids.New("claim"), Regions: []string{"CN_TEST"}, Agreement: "seller_terms_v1", AcceptedAt: time.Now().UTC()}
	submitted := decode[seller.Profile](t, h.request(t, "POST", "/seller/applications", in, headers), 201)
	if submitted.State != "SUBMITTED" || len(submitted.Regions) != 0 || submitted.PayeeState != "UNVERIFIED" {
		t.Fatal("claim acquired approval")
	}
	replay := h.request(t, "POST", "/seller/applications", in, headers)
	decode[seller.Profile](t, replay, 201)
	if replay.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("application replay not identified")
	}
	profile := decode[seller.Profile](t, h.request(t, "GET", "/seller/profile", nil, bearer(session.AccessToken)), 200)
	if profile.ID != session.Profile.UserID {
		t.Fatal("wrong pending owner")
	}
	me := decode[identity.Profile](t, h.request(t, "GET", "/me", nil, bearer(session.AccessToken)), 200)
	if len(me.Roles) != 1 || me.Roles[0] != "BUYER" {
		t.Fatal("application granted SELLER")
	}
	headers.Set("Idempotency-Key", "applicant-cannot-list")
	expectError(t, h.request(t, "GET", "/seller/listings", nil, headers), 403, "FORBIDDEN")
	accepted := true
	c := seller.Consent{Document: "privacy_v1", Purpose: "PRIVACY", Scope: "privacy_self", Accepted: &accepted, ConfirmedAt: time.Now().UTC()}
	headers.Set("Idempotency-Key", "applicant-consent-intent")
	ack := decode[identity.SessionAck](t, h.request(t, "POST", "/me/consents", c, headers), 200)
	if ack.Status != "ACCEPT" {
		t.Fatal(ack)
	}
	headers.Set("Idempotency-Key", "applicant-exit-intent")
	exit := decode[identity.SessionAck](t, h.request(t, "POST", "/seller/exit", seller.Reason{Code: "withdraw_application", ExpectedVersion: profile.Version}, headers), 200)
	if exit.Status != "EXITING" {
		t.Fatal("application withdrawal claimed complete settlement")
	}
}
func TestHTTPSellerListingETagsTenantPagingAndBrowserCSRF(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, cookie := h.login(t, "WEB")
	f := testsupport.SeedOfferForSeller(t, ctx, db, "GPU_RENTAL", ids.New("reg"), identity.Principal{UserID: session.Profile.UserID, Kind: identity.UserSession, Version: session.Profile.Version})
	def := seller.Definition{Title: "我的 RTX 4090 闲置时段", SKU: f.Terms.SKU, Devices: []string{f.Device}, Policy: f.Terms.Policy, Spec: f.Terms.Spec, Price: f.Terms.Price,
		Windows: []seller.Window{{StartsAt: time.Now().UTC().Add(-time.Minute), EndsAt: time.Now().UTC().Add(time.Hour)}}, Capacity: 1, Evidence: f.Evidence}
	headers := http.Header{"Origin": []string{h.origin}, "Idempotency-Key": []string{"browser-listing-intent"}}
	expectError(t, h.request(t, "POST", "/seller/listings", def, headers, cookie), 403, "FORBIDDEN")
	headers.Set("X-CSRF-Token", session.CSRF)
	createdResponse := h.request(t, "POST", "/seller/listings", def, headers, cookie)
	created := decode[seller.Listing](t, createdResponse, 201)
	if createdResponse.header.Get("ETag") != "\"1\"" {
		t.Fatal("missing strong resource version")
	}
	path := "/seller/listings/" + created.ID
	title := "新的未来挂牌标题"
	patch := seller.Patch{Title: &title}
	headers.Set("Idempotency-Key", "browser-listing-patch")
	expectError(t, h.request(t, "PATCH", path, patch, headers, cookie), 400, "INVALID_REQUEST")
	headers.Set("If-Match", "W/\"1\"")
	expectError(t, h.request(t, "PATCH", path, patch, headers, cookie), 400, "INVALID_REQUEST")
	headers.Set("If-Match", "\"1\"")
	changedResponse := h.request(t, "PATCH", path, patch, headers, cookie)
	changed := decode[seller.Listing](t, changedResponse, 200)
	if changed.Revision != 2 || changedResponse.header.Get("ETag") != "\"2\"" {
		t.Fatal("new revision/version missing")
	}
	replay := h.request(t, "PATCH", path, patch, headers, cookie)
	decode[seller.Listing](t, replay, 200)
	if replay.header.Get("ETag") != "\"2\"" || replay.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("ETag replay changed")
	}
	headers.Set("Idempotency-Key", "stale-browser-listing-patch")
	expectError(t, h.request(t, "PATCH", path, patch, headers, cookie), 409, "CONFLICT")
	headers.Set("If-Match", "\"2\"")
	expectError(t, h.request(t, "PATCH", path, `{"title":null,"capacity":1}`, headers, cookie), 400, "INVALID_REQUEST")
	headers.Del("If-Match")
	headers.Set("Idempotency-Key", "browser-listing-submit")
	subResp := h.request(t, "POST", path+"/submit", seller.Reason{Code: "owner_confirmed", ExpectedVersion: 2}, headers, cookie)
	sub := decode[seller.Listing](t, subResp, 200)
	if sub.State != "SUBMITTED" || subResp.header.Get("ETag") != "\"3\"" {
		t.Fatal(sub)
	}
	headers.Set("Idempotency-Key", "browser-listing-pause")
	paused := decode[seller.Listing](t, h.request(t, "POST", path+"/pause", seller.Reason{Code: "owner_paused", ExpectedVersion: 3}, headers, cookie), 200)
	if paused.State != "PAUSED" {
		t.Fatal(paused)
	}
	headers.Set("Idempotency-Key", "browser-second-listing")
	decode[seller.Listing](t, h.request(t, "POST", "/seller/listings", def, headers, cookie), 201)
	page := decode[seller.ListingPage](t, h.request(t, "GET", "/seller/listings?limit=1", nil, headers, cookie), 200)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("owned pagination missing")
	}
	decode[seller.ListingPage](t, h.request(t, "GET", "/seller/listings?limit=1&cursor="+*page.NextCursor, nil, headers, cookie), 200)
	other, _ := h.login(t, "MINIAPP")
	testsupport.SeedOfferForSeller(t, ctx, db, "E2E", ids.New("reg"), identity.Principal{UserID: other.Profile.UserID, Kind: identity.UserSession, Version: other.Profile.Version})
	expectError(t, h.request(t, "GET", path, nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
	expectError(t, h.request(t, "GET", "/seller/listings?limit=1&cursor="+*page.NextCursor, nil, bearer(other.AccessToken)), 400, "INVALID_REQUEST")
	preflight := h.request(t, "OPTIONS", path, nil, http.Header{"Origin": []string{h.origin}, "Access-Control-Request-Method": []string{"PATCH"}, "Access-Control-Request-Headers": []string{"if-match, x-csrf-token, idempotency-key"}})
	if preflight.status != 204 {
		t.Fatalf("ETag preflight %d", preflight.status)
	}
	notRoute := h.request(t, "POST", fmt.Sprintf("%s/submit/extra", path), seller.Reason{Code: "bad_route", ExpectedVersion: 4}, headers, cookie)
	expectError(t, notRoute, 404, "NOT_FOUND")
}
