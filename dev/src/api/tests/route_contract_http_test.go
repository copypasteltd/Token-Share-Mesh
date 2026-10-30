package api_test

import (
	"encoding/json"
	"net/http"
	controlhttp "p11.local/compute/api/internal/http"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"time"
)

func routeHarness(t *testing.T) *harness {
	return newHarnessConfigured(t, 1000, 100, func(c *controlhttp.Config) { c.Routes = routecontract.Service{DB: db, PaymentEnvironment: "SANDBOX"} }, nil)
}

func httpRouteOrder(t *testing.T, h *harness, s identity.SessionView, cookie *http.Cookie) routecontract.Write {
	t.Helper()
	f := testsupport.SeedOfferForProtocol(t, ctx, db, "PLAINTEXT", ids.New("region"), string(relay.ChatText))
	return httpRouteOrderOffer(t, h, s, cookie, f)
}
func httpRouteOrderOffer(t *testing.T, h *harness, s identity.SessionView, cookie *http.Cookie, f testsupport.Offer) routecontract.Write {
	t.Helper()
	headers := bearer(s.AccessToken)
	if cookie != nil {
		headers = http.Header{"Origin": []string{h.origin}, "X-CSRF-Token": []string{s.CSRF}}
	}
	headers.Set("Idempotency-Key", ids.New("quote"))
	var cookies []*http.Cookie
	if cookie != nil {
		cookies = append(cookies, cookie)
	}
	q := decode[trade.Quote](t, h.request(t, "POST", "/quotes", f.Input(), headers, cookies...), 201)
	headers.Set("Idempotency-Key", ids.New("order"))
	o := decode[trade.Order](t, h.request(t, "POST", "/orders", trade.OrderInput{QuoteID: q.ID, ContractHash: q.Contract.Hash, DisclosureVersion: q.Contract.Policy.Disclosure, DisclosureAcceptedAt: q.CreatedAt, CallerKey: ids.New("caller")}, headers, cookies...), 201)
	pay := ids.New("pay")
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.orders SET state='READY',version=version+1 WHERE id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Pool.Exec(ctx, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,channel_reference,product_profile_revision,channel_binding_snapshot,amount_fen,state) VALUES($1,$2,'ORDER',$2,'ALIPAY','SANDBOX',$3,$3,'SYNTHETIC_TEST','{"fixture":"NOT_CHANNEL_EVIDENCE"}',500,'SETTLED')`, pay, o.ID, ids.New("synthetic")); e != nil {
		t.Fatal(e)
	}
	return routecontract.Write{OrderID: o.ID, SellerID: f.Seller.UserID, ListingID: f.ID, Revision: 1, Policy: f.Terms.Policy, PerCallFen: 100, TotalFen: 500, MaxCalls: 5, ExpiresAt: time.Now().Add(10 * time.Minute), PaymentID: pay}
}

func TestHTTPRouteContractsUseOriginalOrderScopedKeyAndSafeReplay(t *testing.T) {
	h := routeHarness(t)
	s, _ := h.login(t, "MINIAPP")
	in := httpRouteOrder(t, h, s, nil)
	headers := bearer(s.AccessToken)
	headers.Set("Idempotency-Key", "http-route-original")
	r := h.request(t, "POST", "/route-contracts", in, headers)
	c := decode[routecontract.Contract](t, r, 201)
	if c.State != "ACTIVE" || c.Definition.OrderID != in.OrderID || c.Definition.SellerID != in.SellerID {
		t.Fatal(c)
	}
	r = h.request(t, "POST", "/route-contracts", in, headers)
	if decode[routecontract.Contract](t, r, 201).ID != c.ID || r.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("route replay changed")
	}
	decode[routecontract.Contract](t, h.request(t, "GET", "/route-contracts/"+c.ID, nil, headers), 200)
	decode[routecontract.Page](t, h.request(t, "GET", "/route-contracts?limit=1", nil, headers), 200)
	key := identity.KeyInput{Label: "Single confirmed route", Scopes: []string{"ROUTE_INVOKE"}, RouteIDs: []string{c.ID}, ExpiresAt: time.Now().Add(time.Minute)}
	headers.Set("Idempotency-Key", "http-route-scoped-key")
	k := decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", key, headers), 201)
	api := bearer(k.SecretOnce)
	api.Set("Idempotency-Key", "sdk-cannot-create-route")
	expectError(t, h.request(t, "POST", "/route-contracts", in, api), 403, "FORBIDDEN")
	expectError(t, h.request(t, "GET", "/route-contracts", nil, api), 403, "FORBIDDEN")
	other, _ := h.login(t, "MINIAPP")
	expectError(t, h.request(t, "GET", "/route-contracts/"+c.ID, nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.orders SET state='CLOSED' WHERE id=$1`, in.OrderID); e != nil {
		t.Fatal(e)
	}
	headers.Set("Idempotency-Key", "http-route-original")
	r = h.request(t, "POST", "/route-contracts", in, headers)
	if decode[routecontract.Contract](t, r, 201).State != "SUSPENDED" {
		t.Fatal("stale ACTIVE replay")
	}
	for _, secret := range []string{"channel_reference", "NOT_CHANNEL_EVIDENCE", "secret_ref"} {
		if strings.Contains(string(r.body), secret) {
			t.Fatal("private field", secret)
		}
	}
}

func TestHTTPRouteContractsRejectAmbiguousInputCursorAndMissingCSRF(t *testing.T) {
	h := routeHarness(t)
	s, cookie := h.login(t, "WEB")
	in := httpRouteOrder(t, h, s, cookie)
	headers := http.Header{"Origin": []string{h.origin}, "Idempotency-Key": []string{"csrf-route-original"}}
	expectError(t, h.request(t, "POST", "/route-contracts", in, headers, cookie), 403, "FORBIDDEN")
	headers.Set("X-CSRF-Token", s.CSRF)
	raw := map[string]any{}
	b, _ := json.Marshal(in)
	if e := json.Unmarshal(b, &raw); e != nil {
		t.Fatal(e)
	}
	raw["endpoint"] = "http://127.0.0.1:1234/v1/chat/completions"
	expectError(t, h.request(t, "POST", "/route-contracts", raw, headers, cookie), 400, "INVALID_REQUEST")
	decode[routecontract.Contract](t, h.request(t, "POST", "/route-contracts", in, headers, cookie), 201)
	expectError(t, h.request(t, "GET", "/route-contracts?cursor=unsigned_value", nil, headers, cookie), 400, "INVALID_REQUEST")
	expectError(t, h.request(t, "GET", "/route-contracts?limit=101", nil, headers, cookie), 400, "INVALID_REQUEST")
	expectError(t, h.request(t, "GET", "/route-contracts?limit=1&limit=2", nil, headers, cookie), 400, "INVALID_REQUEST")
	expectError(t, h.request(t, "GET", "/route-contracts", nil, nil), 401, "UNAUTHENTICATED")
}
