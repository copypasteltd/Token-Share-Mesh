// Package controlhttp adapts authenticated control resources. It does not
// implement OpenAI/Anthropic inference endpoints or payment success callbacks.
package controlhttp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/seller"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"strconv"
	"strings"
	"time"
)

const prefix = "/v1/p11"

type Config struct {
	AllowedHosts                     []string
	BrowserOrigin                    string
	InsecureLoopback                 bool
	CursorKey                        []byte
	RequestsPerMinute, AuthPerMinute int
	Sessions                         identity.Sessions
	Keys                             identity.KeyService
	Market                           market.Catalog
	Trade                            trade.Service
	Seller                           seller.Service
}
type Server struct {
	c                          Config
	hosts                      map[string]bool
	sessionCookie, loginCookie string
	limiter                    limiter
	slots                      chan struct{}
	routes                     []registeredRoute
}
type actor struct {
	p       identity.Principal
	session *identity.SessionCredential
}

func New(c Config) (*Server, error) {
	u, err := url.Parse(c.BrowserOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && !(c.InsecureLoopback && u.Scheme == "http" && loopback(u.Hostname()))) ||
		len(c.CursorKey) < 32 || len(c.AllowedHosts) < 1 || len(c.AllowedHosts) > 8 ||
		c.Sessions.DB == nil || c.Keys.DB == nil || c.Market.DB == nil || c.Trade.DB == nil {
		return nil, fault.New(fault.Unavailable, "Control HTTP configuration required")
	}
	s := &Server{c: c, hosts: map[string]bool{}, sessionCookie: "__Host-p11_session", loginCookie: "__Host-p11_login",
		limiter: limiter{entries: map[string]bucket{}, capacity: 4096}, slots: make(chan struct{}, 32)}
	for _, host := range c.AllowedHosts {
		parsed, err := url.Parse("http://" + host)
		if err != nil || parsed.Host != host || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
			host == "" || strings.Contains(host, "*") || c.InsecureLoopback && !loopback(parsed.Hostname()) {
			return nil, fault.New(fault.Unavailable, "Explicit HTTP host allowlist required")
		}
		s.hosts[host] = true
	}
	if c.InsecureLoopback {
		s.sessionCookie = "p11_session_dev"
		s.loginCookie = "p11_login_dev"
	}
	if s.c.Seller.DB == nil {
		s.c.Seller.DB = c.Trade.DB
	}
	if c.RequestsPerMinute <= 0 {
		s.c.RequestsPerMinute = 120
	}
	if c.AuthPerMinute <= 0 {
		s.c.AuthPerMinute = 12
	}
	if !s.loadRoutes() {
		return nil, fault.New(fault.Unavailable, "Invalid HTTP route registration")
	}
	return s, nil
}
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func (s *Server) NativeHTTPServer(addr string) *http.Server {
	return &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	requestID := ids.New("req")
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	fail := func(status int, code, message string) { writeError(w, requestID, status, code, message, nil) }
	if !s.hosts[r.Host] {
		fail(403, "FORBIDDEN", "Request host not allowed")
		return
	}
	if len(r.URL.RequestURI()) > 2048 || r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") {
		fail(400, "INVALID_REQUEST", "Invalid control request path")
		return
	}
	origin := r.Header.Values("Origin")
	if len(origin) > 1 || len(origin) == 1 && origin[0] != s.c.BrowserOrigin {
		fail(403, "FORBIDDEN", "Request origin not allowed")
		return
	}
	if len(origin) == 1 {
		w.Header().Set("Access-Control-Allow-Origin", s.c.BrowserOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Idempotency-Replayed, Retry-After, ETag")
		w.Header().Set("Vary", "Origin")
	}
	route, method, anonymous := s.route(r)
	if route == nil {
		fail(404, "NOT_FOUND", "Control resource not found")
		return
	}
	if r.Method == http.MethodOptions {
		if len(origin) != 1 {
			fail(403, "FORBIDDEN", "Preflight origin required")
			return
		}
		requested := r.Header.Get("Access-Control-Request-Method")
		if !strings.Contains(","+method+",", ","+requested+",") {
			fail(405, "METHOD_NOT_ALLOWED", "Method not supported")
			return
		}
		for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
			switch strings.ToLower(strings.TrimSpace(header)) {
			case "", "content-type", "authorization", "x-csrf-token", "idempotency-key", "if-match":
			default:
				fail(403, "FORBIDDEN", "Request header not allowed")
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", method)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, Idempotency-Key, If-Match")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !strings.Contains(","+method+",", ","+r.Method+",") {
		w.Header().Set("Allow", method)
		fail(405, "METHOD_NOT_ALLOWED", "Method not supported")
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodDelete) && (r.ContentLength != 0 || len(r.TransferEncoding) != 0) {
		fail(400, "INVALID_REQUEST", "This control operation has no request body")
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(ip) == nil {
		ip = "unknown"
	}
	if !s.limiter.take("ip:"+ip, s.c.RequestsPerMinute, time.Now()) ||
		anonymous && !s.limiter.take("login:"+ip, s.c.AuthPerMinute, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, requestID, 429, "RATE_LIMITED", "Request limit reached", map[string]any{"retry_after_sec": 60})
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		fail(429, "RATE_LIMITED", "Control capacity temporarily full")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	var a actor
	if !anonymous {
		a, err = s.authenticate(r)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		if !s.limiter.take("user:"+a.p.UserID, s.c.RequestsPerMinute, time.Now()) {
			w.Header().Set("Retry-After", "60")
			fail(429, "RATE_LIMITED", "Request limit reached")
			return
		}
		if a.session != nil && a.session.ClientType == "WEB" && r.Method != http.MethodGet {
			values := r.Header.Values("X-CSRF-Token")
			if len(origin) != 1 || len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(a.session.CSRF)) != 1 {
				fail(403, "FORBIDDEN", "Session CSRF confirmation required")
				return
			}
		}
	}
	route(w, r, a)
}

type handler func(http.ResponseWriter, *http.Request, actor)

func oneCookie(r *http.Request, name string) (string, error) {
	value := ""
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == name {
			value = c.Value
			count++
		}
	}
	if count > 1 {
		return "", fault.New(fault.Unauthenticated, "Ambiguous credential")
	}
	return value, nil
}
func (s *Server) authenticate(r *http.Request) (actor, error) {
	var a actor
	cookie, err := oneCookie(r, s.sessionCookie)
	if err != nil {
		return a, err
	}
	headers := r.Header.Values("Authorization")
	if len(headers) > 1 || cookie != "" && len(headers) > 0 {
		return a, fault.New(fault.Unauthenticated, "Ambiguous credential")
	}
	// Reject an opposite-mode session cookie rather than silently selecting one.
	opposite := "p11_session_dev"
	if s.c.InsecureLoopback {
		opposite = "__Host-p11_session"
	}
	if value, _ := oneCookie(r, opposite); value != "" {
		return a, fault.New(fault.Unauthenticated, "Credential transport mismatch")
	}
	token := cookie
	bearer := len(headers) == 1
	if bearer {
		parts := strings.Split(headers[0], " ")
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			return a, fault.New(fault.Unauthenticated, "Authentication required")
		}
		token = parts[1]
	}
	if strings.HasPrefix(token, "p11k.") && bearer {
		a.p, err = s.c.Keys.Authenticate(r.Context(), token)
		return a, err
	}
	credential, err := s.c.Sessions.Authenticate(r.Context(), token)
	if err != nil {
		return a, err
	}
	if credential.ClientType == "WEB" && bearer || credential.ClientType == "MINIAPP" && (!bearer || r.Header.Get("Origin") != "") {
		return a, fault.New(fault.Unauthenticated, "Credential transport mismatch")
	}
	a.p, a.session = credential.Principal, &credential
	return a, nil
}
func (s *Server) body(w http.ResponseWriter, r *http.Request, out any) bool {
	contentType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || parameters["charset"] != "" && !strings.EqualFold(parameters["charset"], "utf-8") ||
		r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		writeError(w, w.Header().Get("X-Request-ID"), 415, "UNSUPPORTED_MEDIA_TYPE", "UTF-8 JSON required", nil)
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		writeError(w, w.Header().Get("X-Request-ID"), 413, "PAYLOAD_TOO_LARGE", "Control request body too large or unreadable", nil)
		return false
	}
	if len(raw) == 0 || strictjson.Decode(raw, out, 65536) != nil {
		writeError(w, w.Header().Get("X-Request-ID"), 400, "INVALID_REQUEST", "Invalid or unsupported control JSON", nil)
		return false
	}
	return true
}
func query(r *http.Request, names ...string) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fault.New(fault.Invalid, "Invalid control query")
	}
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for name, values := range q {
		if !allowed[name] || len(values) != 1 || values[0] == "" {
			return nil, fault.New(fault.Invalid, "Unsupported or ambiguous query field")
		}
	}
	return q, nil
}
func (s *Server) noQuery(w http.ResponseWriter, r *http.Request) bool {
	_, err := query(r)
	if err != nil {
		s.appError(w, r, err)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeError(w, w.Header().Get("X-Request-ID"), 503, "UNAVAILABLE", "Response temporarily unavailable", nil)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
func writeError(w http.ResponseWriter, requestID string, status int, code, message string, details map[string]any, retryable ...bool) {
	if details == nil {
		details = map[string]any{}
	}
	mayRetry := len(retryable) == 1 && retryable[0]
	body := map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": mayRetry, "request_id": requestID, "details": details}}
	raw, _ := json.Marshal(body)
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *Server) appError(w http.ResponseWriter, r *http.Request, err error) {
	safe := fault.Public(err)
	status := 503
	switch safe.Code {
	case fault.Invalid:
		status = 400
	case fault.Unauthenticated:
		status = 401
	case fault.Forbidden:
		status = 403
	case fault.NotFound:
		status = 404
	case fault.Conflict:
		status = 409
	}
	writeError(w, w.Header().Get("X-Request-ID"), status, string(safe.Code), safe.Message, nil, safe.Retryable)
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if value == "" {
		maxAge = -1
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !s.c.InsecureLoopback,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}
func (s *Server) challenge(w http.ResponseWriter, r *http.Request, _ actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in identity.ChallengeInput
	if !s.body(w, r, &in) {
		return
	}
	if in.ClientType == "WEB" && r.Header.Get("Origin") != s.c.BrowserOrigin || in.ClientType == "MINIAPP" && r.Header.Get("Origin") != "" {
		writeError(w, w.Header().Get("X-Request-ID"), 403, "FORBIDDEN", "Authorization client origin mismatch", nil)
		return
	}
	c, err := s.c.Sessions.CreateChallenge(r.Context(), in)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if c.ClientType == "WEB" {
		s.setCookie(w, s.loginCookie, c.ID+"."+c.BrowserSecret, c.ExpiresAt)
	}
	writeJSON(w, 201, c)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request, _ actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in identity.SessionInput
	if !s.body(w, r, &in) {
		return
	}
	binding, err := oneCookie(r, s.loginCookie)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	browserSecret := ""
	if binding != "" {
		parts := strings.Split(binding, ".")
		if len(parts) != 2 || parts[0] != in.ChallengeID || r.Header.Get("Origin") != s.c.BrowserOrigin {
			writeError(w, w.Header().Get("X-Request-ID"), 401, "UNAUTHENTICATED", "Authorization browser binding failed", nil)
			return
		}
		browserSecret = parts[1]
	} else if r.Header.Get("Origin") != "" {
		writeError(w, w.Header().Get("X-Request-ID"), 401, "UNAUTHENTICATED", "Authorization browser binding required", nil)
		return
	}
	issued, err := s.c.Sessions.Create(r.Context(), in, browserSecret)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if issued.View.ClientType == "WEB" {
		s.setCookie(w, s.sessionCookie, issued.Secret, issued.View.ExpiresAt)
		s.setCookie(w, s.loginCookie, "", time.Time{})
	}
	writeJSON(w, 201, issued.View)
}
func (s *Server) currentSession(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	if a.session == nil {
		s.appError(w, r, fault.New(fault.Forbidden, "Account session required"))
		return
	}
	if r.Method == http.MethodDelete {
		ack, err := s.c.Sessions.Revoke(r.Context(), a.p)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		if a.session.ClientType == "WEB" {
			s.setCookie(w, s.sessionCookie, "", time.Time{})
		}
		writeJSON(w, 200, ack)
		return
	}
	profile, err := s.c.Sessions.Profile(r.Context(), a.p)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, identity.SessionView{ID: a.p.SessionID, ClientType: a.session.ClientType, ExpiresAt: a.session.ExpiresAt,
		Profile: profile, CSRF: a.session.CSRF})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	profile, err := s.c.Sessions.Profile(r.Context(), a.p)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, profile)
}
func (s *Server) cursor(raw, scope string, encode bool) (string, error) {
	sign := func(value string) []byte {
		m := hmac.New(sha256.New, s.c.CursorKey)
		m.Write([]byte("P11_HTTP_CURSOR_1\x00" + scope + "\x00" + value))
		return m.Sum(nil)
	}
	if raw == "" {
		return "", nil
	}
	if encode {
		payload := base64.RawURLEncoding.EncodeToString([]byte(raw))
		result := payload + "." + base64.RawURLEncoding.EncodeToString(sign(payload))
		if len(result) > 512 {
			return "", fault.New(fault.Unavailable, "Cursor exceeds supported bounds")
		}
		return result, nil
	}
	if len(raw) > 512 {
		return "", fault.New(fault.Invalid, "Invalid page cursor")
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return "", fault.New(fault.Invalid, "Invalid page cursor")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, sign(parts[0])) {
		return "", fault.New(fault.Invalid, "Invalid page cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) > 384 {
		return "", fault.New(fault.Invalid, "Invalid page cursor")
	}
	return string(payload), nil
}
func pageLimit(q url.Values, max int) (int, error) {
	if q.Get("limit") == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(q.Get("limit"))
	if err != nil || n < 1 || n > max {
		return 0, fault.New(fault.Invalid, "Invalid page limit")
	}
	return n, nil
}
func (s *Server) keys(w http.ResponseWriter, r *http.Request, a actor) {
	if r.Method == http.MethodPost {
		if !s.noQuery(w, r) {
			return
		}
		var in identity.KeyInput
		if !s.body(w, r, &in) {
			return
		}
		key, ok := s.idempotency(w, r)
		if !ok {
			return
		}
		result, err := s.c.Keys.Create(r.Context(), a.p, in, key)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		if result.Replayed {
			writeError(w, w.Header().Get("X-Request-ID"), 409, "SECRET_ALREADY_ISSUED", "Secret was already issued; query or revoke the original key", map[string]any{"resource_id": result.Key.ID})
			return
		}
		writeJSON(w, 201, result)
		return
	}
	q, err := query(r, "cursor", "limit")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	limit, err := pageLimit(q, 100)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	scope := "keys:" + a.p.UserID
	cursor, err := s.cursor(q.Get("cursor"), scope, false)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	page, err := s.c.Keys.List(r.Context(), a.p, cursor, limit)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if page.NextCursor != nil {
		value, err := s.cursor(*page.NextCursor, scope, true)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		page.NextCursor = &value
	}
	writeJSON(w, 200, page)
}
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix+"/api-keys/")
	if err := s.c.Keys.Revoke(r.Context(), a.p, id, key); err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, identity.SessionAck{ID: id, Version: 1, Status: "REVOKED", RecordedAt: time.Now().UTC()})
}
func (s *Server) services(w http.ResponseWriter, r *http.Request, a actor) {
	if err := a.p.Require("market:read", ""); err != nil {
		s.appError(w, r, err)
		return
	}
	q, err := query(r, "cursor", "limit", "mode", "region")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	limit, err := pageLimit(q, 50)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	scope := "market:" + a.p.UserID + ":" + q.Get("mode") + ":" + q.Get("region")
	cursor, err := s.cursor(q.Get("cursor"), scope, false)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	page, err := s.c.Market.List(r.Context(), market.Filter{Mode: q.Get("mode"), Region: q.Get("region")}, cursor, limit)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if page.NextCursor != nil {
		value, err := s.cursor(*page.NextCursor, scope, true)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		page.NextCursor = &value
	}
	writeJSON(w, 200, page)
}
func (s *Server) idempotency(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || len(values[0]) < 8 || len(values[0]) > 128 {
		writeError(w, w.Header().Get("X-Request-ID"), 400, "INVALID_REQUEST", "One stable Idempotency-Key required", nil)
		return "", false
	}
	return values[0], true
}
func (s *Server) quote(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in trade.QuoteInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	value, err := s.c.Trade.CreateQuote(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if value.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 201, value)
}
func (s *Server) getQuote(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	value, err := s.c.Trade.GetQuote(r.Context(), a.p, strings.TrimPrefix(r.URL.Path, prefix+"/quotes/"))
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, value)
}
func (s *Server) order(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in trade.OrderInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	value, err := s.c.Trade.CreateOrder(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if value.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 201, value)
}
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	value, err := s.c.Trade.GetOrder(r.Context(), a.p, strings.TrimPrefix(r.URL.Path, prefix+"/orders/"))
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, value)
}
