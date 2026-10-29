package api_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/ids"
)

var db *persistence.DB
var ctx context.Context
var fixtureKeys struct {
	once                  sync.Once
	application, platform *rsa.PrivateKey
	err                   error
}

func TestMain(m *testing.M) {
	dsn := os.Getenv("P11_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("P11_REQUIRE_INTEGRATION") == "1" {
			fmt.Fprintln(os.Stderr, "native database URL required")
			os.Exit(1)
		}
		os.Exit(m.Run())
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), 110*time.Second)
	var err error
	db, err = persistence.Open(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "native database connection failed")
		cancel()
		os.Exit(1)
	}
	if err = db.Migrate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "native migration failed")
		db.Close()
		cancel()
		os.Exit(1)
	}
	code := m.Run()
	db.Close()
	cancel()
	os.Exit(code)
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type harness struct {
	server *httptest.Server
	client *http.Client
	origin string
}

func newHarness(t *testing.T, limit, authLimit int) *harness {
	t.Helper()
	if db == nil {
		t.Skip("native PostgreSQL required; use dev/tools/原生数据库验证.py")
	}
	fixtureKeys.once.Do(func() {
		fixtureKeys.application, fixtureKeys.err = rsa.GenerateKey(rand.Reader, 2048)
		if fixtureKeys.err == nil {
			fixtureKeys.platform, fixtureKeys.err = rsa.GenerateKey(rand.Reader, 2048)
		}
	})
	if fixtureKeys.err != nil {
		t.Fatal(fixtureKeys.err)
	}
	provider, err := identity.NewAlipay(identity.AlipayConfig{AppID: "2026103000000001", SubjectField: "OPEN_ID",
		WebRedirect: "http://127.0.0.1:5173/auth/return", PrivateKey: fixtureKeys.application, PublicKey: &fixtureKeys.platform.PublicKey,
		HTTPClient: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://openapi.alipay.com/gateway.do" || r.Method != "POST" {
				return nil, fmt.Errorf("unexpected authorization target")
			}
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			names := []string{}
			for name, values := range r.PostForm {
				if len(values) != 1 {
					return nil, fmt.Errorf("ambiguous request")
				}
				if name != "sign" && values[0] != "" {
					names = append(names, name)
				}
			}
			sort.Strings(names)
			parts := []string{}
			for _, name := range names {
				parts = append(parts, name+"="+r.PostForm.Get(name))
			}
			digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
			signature, err := base64.StdEncoding.DecodeString(r.PostForm.Get("sign"))
			if err != nil || rsa.VerifyPKCS1v15(&fixtureKeys.application.PublicKey, crypto.SHA256, digest[:], signature) != nil {
				return nil, fmt.Errorf("unsigned application request")
			}
			// Independently signed synthetic provider response. No external OAuth call.
			raw := []byte(fmt.Sprintf("{ \"open_id\": %q, \"access_token\": \"SYNTHETIC_CHANNEL_TOKEN\", \"expires_in\": \"3600\" }", r.PostForm.Get("code")))
			digest = sha256.Sum256(raw)
			signature, err = rsa.SignPKCS1v15(rand.Reader, fixtureKeys.platform, crypto.SHA256, digest[:])
			if err != nil {
				return nil, err
			}
			envelope := fmt.Sprintf("{\"alipay_system_oauth_token_response\":%s,\"sign\":%q}", raw, base64.StdEncoding.EncodeToString(signature))
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(envelope))}, nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{server: httptest.NewUnstartedServer(nil), origin: "http://127.0.0.1:5173"}
	config := controlhttp.Config{AllowedHosts: []string{h.server.Listener.Addr().String()}, BrowserOrigin: h.origin, InsecureLoopback: true,
		CursorKey: []byte(strings.Repeat("c", 32)), RequestsPerMinute: limit, AuthPerMinute: authLimit,
		Sessions: identity.Sessions{DB: db, Provider: provider, PepperVersion: "http_test_v1", Peppers: map[string][]byte{"http_test_v1": []byte(strings.Repeat("s", 32))}, IdentityPepper: []byte(strings.Repeat("i", 32)), TTL: time.Hour},
		Keys:     identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "http_test_v1", Peppers: map[string][]byte{"http_test_v1": []byte(strings.Repeat("k", 32))}, MaxTTL: 24 * time.Hour},
		Market:   market.Catalog{DB: db}, Trade: trade.Service{DB: db}}
	handler, err := controlhttp.New(config)
	if err != nil {
		h.server.Close()
		t.Fatal(err)
	}
	h.server.Config = handler.NativeHTTPServer("")
	h.server.Start()
	h.client = h.server.Client()
	h.client.Timeout = 5 * time.Second
	t.Cleanup(h.server.Close)
	return h
}

type response struct {
	status  int
	header  http.Header
	body    []byte
	cookies []*http.Cookie
}

func (h *harness) request(t *testing.T, method, path string, body any, headers http.Header, cookies ...*http.Cookie) response {
	t.Helper()
	var raw []byte
	switch v := body.(type) {
	case nil:
	case string:
		raw = []byte(v)
	default:
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, h.server.URL+"/v1/p11"+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 131073))
	if err != nil || len(data) > 131072 {
		t.Fatal("unbounded or unreadable control response")
	}
	return response{resp.StatusCode, resp.Header, data, resp.Cookies()}
}
func decode[T any](t *testing.T, r response, status int) T {
	t.Helper()
	if r.status != status {
		t.Fatalf("HTTP %d want %d: %s", r.status, status, r.body)
	}
	var out T
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func expectError(t *testing.T, r response, status int, code string) {
	t.Helper()
	var out struct {
		Error struct {
			Code, Message, RequestID string
			Retryable                bool
			Details                  map[string]any
		} `json:"error"`
	}
	// Check required snake_case envelope separately; don't accept a false empty ID.
	fields := decode[map[string]json.RawMessage](t, r, status)
	if err := json.Unmarshal(fields["error"], &out.Error); err != nil {
		t.Fatal(err)
	}
	var safe map[string]any
	if err := json.Unmarshal(fields["error"], &safe); err != nil {
		t.Fatal(err)
	}
	requestID, _ := safe["request_id"].(string)
	if safe["code"] != code || !ids.Valid(requestID) || requestID != r.header.Get("X-Request-ID") || safe["details"] == nil {
		t.Fatalf("unsafe error envelope: %s", r.body)
	}
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing bounded response protections")
	}
}
func bearer(token string) http.Header {
	return http.Header{"Authorization": []string{"Bearer " + token}}
}
func (h *harness) login(t *testing.T, kind string) (identity.SessionView, *http.Cookie) {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	headers := http.Header{}
	if kind == "WEB" {
		headers.Set("Origin", h.origin)
	}
	challengeResponse := h.request(t, "POST", "/auth/challenges", identity.ChallengeInput{ClientType: kind, CodeChallenge: base64.RawURLEncoding.EncodeToString(hash[:])}, headers)
	challenge := decode[identity.Challenge](t, challengeResponse, 201)
	if challenge.BrowserSecret != "" || strings.Contains(string(challengeResponse.body), "browser_secret") {
		t.Fatal("browser binding secret exposed")
	}
	in := identity.SessionInput{ChallengeID: challenge.ID, Verifier: verifier, AuthCode: ids.New("SYNTHETIC_AUTH")}
	var loginCookie *http.Cookie
	if kind == "WEB" {
		in.WebState = challenge.ID
		if !strings.HasPrefix(challenge.AuthorizationURL, "https://openauth.alipay.com/") {
			t.Fatal("unbound web authorization target")
		}
		for _, c := range challengeResponse.cookies {
			if c.Name == "p11_login_dev" {
				loginCookie = c
			}
		}
		if loginCookie == nil || !loginCookie.HttpOnly || loginCookie.SameSite != http.SameSiteStrictMode {
			t.Fatal("missing browser challenge binding")
		}
	}
	cookies := []*http.Cookie{}
	if loginCookie != nil {
		cookies = append(cookies, loginCookie)
	}
	r := h.request(t, "POST", "/auth/sessions", in, headers, cookies...)
	view := decode[identity.SessionView](t, r, 201)
	if view.Profile.IdentityState != "UNVERIFIED" || len(view.Profile.Roles) != 1 || view.Profile.Roles[0] != "BUYER" {
		t.Fatal("authorization grants unsupported identity")
	}
	if strings.Contains(string(r.body), "SYNTHETIC_CHANNEL_TOKEN") {
		t.Fatal("vendor token leaked")
	}
	if kind == "MINIAPP" {
		if view.AccessToken == "" || view.CSRF != "" || len(r.cookies) != 0 {
			t.Fatal("invalid miniapp credential transport")
		}
		return view, nil
	}
	for _, c := range r.cookies {
		if c.Name == "p11_session_dev" && c.Value != "" {
			if !c.HttpOnly || c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || view.AccessToken != "" || len(view.CSRF) != 64 {
				t.Fatal("invalid loopback browser credential transport")
			}
			return view, c
		}
	}
	t.Fatal("missing web session cookie")
	return view, nil
}
func TestHTTPMiniappLoginQuoteOrderAndTenantIsolation(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, _ := h.login(t, "MINIAPP")
	headers := bearer(session.AccessToken)
	me := decode[identity.Profile](t, h.request(t, "GET", "/me", nil, headers), 200)
	if me.UserID != session.Profile.UserID {
		t.Fatal("wrong authenticated account")
	}
	current := decode[identity.SessionView](t, h.request(t, "GET", "/auth/session", nil, headers), 200)
	if current.AccessToken != "" {
		t.Fatal("credential reissued on read")
	}
	region := ids.New("region")
	offer := testsupport.SeedOffer(t, ctx, db, "GPU_RENTAL", region)
	page := decode[market.Page](t, h.request(t, "GET", "/services?region="+region, nil, headers), 200)
	if len(page.Items) != 1 || !strings.Contains(string(mustJSON(t, page)), "RTX 4090 24GB") {
		t.Fatal("qualified C2C market unavailable")
	}
	headers.Set("Idempotency-Key", "http-quote-create")
	quoted := decode[trade.Quote](t, h.request(t, "POST", "/quotes", offer.Input(), headers), 201)
	if quoted.Contract.SellerID != offer.Seller.UserID || quoted.Contract.Price.Unit != pricing.Hour {
		t.Fatal("seller or unit not frozen")
	}
	input := trade.OrderInput{QuoteID: quoted.ID, ContractHash: quoted.Contract.Hash, DisclosureVersion: quoted.Contract.Policy.Disclosure, DisclosureAcceptedAt: quoted.CreatedAt, CallerKey: ids.New("caller")}
	headers.Set("Idempotency-Key", "http-order-create")
	order := decode[trade.Order](t, h.request(t, "POST", "/orders", input, headers), 201)
	decode[trade.Quote](t, h.request(t, "GET", "/quotes/"+quoted.ID, nil, headers), 200)
	decode[trade.Order](t, h.request(t, "GET", "/orders/"+order.ID, nil, headers), 200)
	replayResponse := h.request(t, "POST", "/orders", input, headers)
	replay := decode[trade.Order](t, replayResponse, 201)
	if order.ID != replay.ID || replayResponse.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("HTTP order duplicated")
	}
	other, _ := h.login(t, "MINIAPP")
	expectError(t, h.request(t, "GET", "/orders/"+order.ID, nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
	expectError(t, h.request(t, "GET", "/quotes/"+quoted.ID, nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
	keyInput := identity.KeyInput{Label: "HTTP scoped key", Scopes: []string{"MARKET_READ", "ORDER_READ"}, ExpiresAt: time.Now().Add(time.Hour)}
	headers.Set("Idempotency-Key", "http-scoped-key")
	key := decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", keyInput, headers), 201)
	apiHeaders := bearer(key.SecretOnce)
	apiHeaders.Set("Idempotency-Key", "http-api-purchase")
	expectError(t, h.request(t, "POST", "/orders", input, apiHeaders), 403, "FORBIDDEN")
	expectError(t, h.request(t, "GET", "/auth/session", nil, apiHeaders), 403, "FORBIDDEN")
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHTTPBrowserCookiesCSRFAndSessionRevocation(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, cookie := h.login(t, "WEB")
	current := decode[identity.SessionView](t, h.request(t, "GET", "/auth/session", nil, nil, cookie), 200)
	if current.CSRF != session.CSRF || current.AccessToken != "" {
		t.Fatal("web session disclosure changed")
	}
	input := identity.KeyInput{Label: "Browser key", Scopes: []string{"MARKET_READ"}, ExpiresAt: time.Now().Add(time.Hour)}
	headers := http.Header{"Idempotency-Key": []string{"browser-key-create"}, "Origin": []string{h.origin}}
	expectError(t, h.request(t, "POST", "/api-keys", input, headers, cookie), 403, "FORBIDDEN")
	headers.Set("X-CSRF-Token", session.CSRF)
	headers.Set("Origin", "https://untrusted.invalid")
	expectError(t, h.request(t, "POST", "/api-keys", input, headers, cookie), 403, "FORBIDDEN")
	headers.Set("Origin", h.origin)
	decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", input, headers, cookie), 201)
	expectError(t, h.request(t, "GET", "/me", nil, bearer(cookie.Value)), 401, "UNAUTHENTICATED")
	expectError(t, h.request(t, "GET", "/me", nil, bearer(cookie.Value), cookie), 401, "UNAUTHENTICATED")
	mini, _ := h.login(t, "MINIAPP")
	wrong := bearer(mini.AccessToken)
	wrong.Set("Origin", h.origin)
	expectError(t, h.request(t, "GET", "/me", nil, wrong), 401, "UNAUTHENTICATED")
	expectError(t, h.request(t, "GET", "/me", nil, nil, &http.Cookie{Name: "p11_session_dev", Value: mini.AccessToken}), 401, "UNAUTHENTICATED")
	ack := decode[identity.SessionAck](t, h.request(t, "DELETE", "/auth/session", nil, headers, cookie), 200)
	if ack.ID != session.ID || ack.Status != "REVOKED" {
		t.Fatal("wrong session revoked")
	}
	expectError(t, h.request(t, "GET", "/me", nil, nil, cookie), 401, "UNAUTHENTICATED")
}

func TestHTTPRejectsAmbiguousOversizeAndUnsupportedRequests(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, _ := h.login(t, "MINIAPP")
	headers := bearer(session.AccessToken)
	for _, raw := range []string{`{"Label":"case alias","scopes":["MARKET_READ"]}`, `{"label":"a","label":"b"}`, `{"user_id":"attacker"}`, "null", "[]"} {
		expectError(t, h.request(t, "POST", "/api-keys", raw, headers), 400, "INVALID_REQUEST")
	}
	expectError(t, h.request(t, "POST", "/api-keys", strings.Repeat(" ", 65537), headers), 413, "PAYLOAD_TOO_LARGE")
	compressed := headers.Clone()
	compressed.Set("Content-Encoding", "gzip")
	expectError(t, h.request(t, "POST", "/api-keys", "{}", compressed), 415, "UNSUPPORTED_MEDIA_TYPE")
	for _, path := range []string{"/services?limit=1&limit=2", "/services?limit=0", "/services?limit=51", "/services?unknown=yes", "/me?user_id=attacker"} {
		expectError(t, h.request(t, "GET", path, nil, headers), 400, "INVALID_REQUEST")
	}
	expectError(t, h.request(t, "GET", "/me", "{}", headers), 400, "INVALID_REQUEST")
	expectError(t, h.request(t, "POST", "/me", "{}", headers), 405, "METHOD_NOT_ALLOWED")
	expectError(t, h.request(t, "GET", "/payment/fake-success", nil, headers), 404, "NOT_FOUND")
	duplicate := headers.Clone()
	duplicate.Add("Authorization", "Bearer hidden")
	expectError(t, h.request(t, "GET", "/me", nil, duplicate), 401, "UNAUTHENTICATED")
	// Real HTTP Host override must be rejected before authentication.
	req, err := http.NewRequest("GET", h.server.URL+"/v1/p11/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "attacker.invalid"
	req.Header = headers.Clone()
	r, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	expectError(t, response{r.StatusCode, r.Header, raw, nil}, 403, "FORBIDDEN")
}

func TestHTTPKeySecretOnceSignedPaginationAndRateLimits(t *testing.T) {
	h := newHarness(t, 1000, 100)
	session, _ := h.login(t, "MINIAPP")
	headers := bearer(session.AccessToken)
	input := identity.KeyInput{Label: "One time key", Scopes: []string{"MARKET_READ"}, ExpiresAt: time.Now().Add(time.Hour)}
	headers.Set("Idempotency-Key", "http-once-create")
	first := decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", input, headers), 201)
	replay := h.request(t, "POST", "/api-keys", input, headers)
	expectError(t, replay, 409, "SECRET_ALREADY_ISSUED")
	if strings.Contains(string(replay.body), first.SecretOnce) || !strings.Contains(string(replay.body), first.Key.ID) {
		t.Fatal("secret replay or missing recovery reference")
	}
	headers.Set("Idempotency-Key", "http-next-create")
	decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", input, headers), 201)
	pageResponse := h.request(t, "GET", "/api-keys?limit=1", nil, headers)
	page := decode[identity.KeyPage](t, pageResponse, 200)
	if page.NextCursor == nil || len(page.Items) != 1 || strings.Contains(string(pageResponse.body), first.SecretOnce) {
		t.Fatal("invalid safe key page")
	}
	decode[identity.KeyPage](t, h.request(t, "GET", "/api-keys?limit=1&cursor="+*page.NextCursor, nil, headers), 200)
	expectError(t, h.request(t, "GET", "/api-keys?cursor=x"+*page.NextCursor, nil, headers), 400, "INVALID_REQUEST")
	other, _ := h.login(t, "MINIAPP")
	expectError(t, h.request(t, "GET", "/api-keys?cursor="+*page.NextCursor, nil, bearer(other.AccessToken)), 400, "INVALID_REQUEST")
	headers.Set("Idempotency-Key", "http-key-revoke")
	decode[identity.SessionAck](t, h.request(t, "DELETE", "/api-keys/"+first.Key.ID, nil, headers), 200)
	expectError(t, h.request(t, "GET", "/services", nil, bearer(first.SecretOnce)), 401, "UNAUTHENTICATED")
	preflight := http.Header{"Origin": []string{h.origin}, "Access-Control-Request-Method": []string{"POST"}, "Access-Control-Request-Headers": []string{"content-type,x-csrf-token,idempotency-key"}}
	if r := h.request(t, "OPTIONS", "/api-keys", nil, preflight); r.status != 204 || r.header.Get("Access-Control-Allow-Origin") != h.origin {
		t.Fatal("allowed preflight rejected")
	}
	preflight.Set("Access-Control-Request-Headers", "x-user-id")
	expectError(t, h.request(t, "OPTIONS", "/api-keys", nil, preflight), 403, "FORBIDDEN")
	limited := newHarness(t, 100, 2)
	proof := sha256.Sum256([]byte(strings.Repeat("q", 43)))
	challenge := identity.ChallengeInput{ClientType: "MINIAPP", CodeChallenge: base64.RawURLEncoding.EncodeToString(proof[:])}
	for i := 0; i < 2; i++ {
		decode[identity.Challenge](t, limited.request(t, "POST", "/auth/challenges", challenge, nil), 201)
	}
	denied := limited.request(t, "POST", "/auth/challenges", challenge, nil)
	expectError(t, denied, 429, "RATE_LIMITED")
	if denied.header.Get("Retry-After") != "60" {
		t.Fatal("unbounded retry guidance")
	}
}
