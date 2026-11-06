// Package sdkhttp exposes a bounded non-streaming native text data plane.
// It uses existing funded C2C contracts, never a seller pool or payment proxy.
package sdkhttp

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

const RequestBytes = 256 << 10
const TextBytes = 1 << 20
const ResponseBytes = 8 << 20
const MaximumWait = 120 * time.Second

type Authenticator interface {
	Authenticate(context.Context, string) (identity.Principal, error)
}
type Calls interface {
	Begin(context.Context, identity.Principal, relay.Profile, string, []byte) (invocation.Preparation, error)
	State(context.Context, identity.Principal, string) (invocation.SDKState, error)
	Chunk(context.Context, identity.Principal, string, string, string, int) (resultwire.ContentChunk, error)
	Recheck(context.Context, identity.Principal, string, string, string) error
}
type Config struct {
	AllowedHosts []string
	Keys         Authenticator
	Calls        Calls
	Wait         time.Duration
}
type bucket struct {
	since time.Time
	count int
}
type Server struct {
	c       Config
	hosts   map[string]bool
	slots   chan struct{}
	mu      sync.Mutex
	buckets map[string]bucket
	life    context.Context
	cancel  context.CancelFunc
	closed  bool
	workers sync.WaitGroup
	done    chan struct{}
}

func New(c Config) (*Server, error) {
	if c.Keys == nil || c.Calls == nil || len(c.AllowedHosts) < 1 || len(c.AllowedHosts) > 8 || c.Wait < 0 || c.Wait > MaximumWait {
		return nil, fault.New(fault.Invalid, "Explicit bounded SDK configuration required")
	}
	if c.Wait == 0 {
		c.Wait = MaximumWait
	}
	s := &Server{c: c, hosts: map[string]bool{}, slots: make(chan struct{}, 4), buckets: map[string]bucket{}}
	for _, h := range c.AllowedHosts {
		u, err := url.Parse("http://" + h)
		if err != nil || u.Host != h || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || h == "" || strings.Contains(h, "*") {
			return nil, fault.New(fault.Invalid, "Exact SDK host allowlist required")
		}
		s.hosts[h] = true
	}
	s.life, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})
	return s, nil
}

// Close prevents new admissions, cancels only this host's active readers and
// waits for their actual return before the owner may erase issuer/input keys.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.cancel()
		go func() { s.workers.Wait(); close(s.done) }()
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func profile(path string) relay.Profile {
	switch path {
	case "/v1/chat/completions":
		return relay.ChatText
	case "/v1/responses":
		return relay.ResponsesText
	case "/v1/messages":
		return relay.MessagesText
	}
	return ""
}

// Handles does no path normalization or redirects. Malformed SDK paths stay
// with this handler and cannot fall through to a different authenticated plane.
func Handles(path string) bool {
	return strings.HasPrefix(path, "/v1/chat") || strings.HasPrefix(path, "/v1/responses") || strings.HasPrefix(path, "/v1/messages")
}

func (s *Server) take(key string, limit int) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[key]
	if !ok && len(s.buckets) >= 4096 {
		for k, v := range s.buckets {
			if now.Sub(v.since) >= time.Minute {
				delete(s.buckets, k)
			}
		}
		if len(s.buckets) >= 4096 {
			return false
		}
	}
	if !ok || now.Sub(b.since) >= time.Minute {
		b = bucket{since: now}
	}
	if b.count >= limit {
		return false
	}
	b.count++
	s.buckets[key] = b
	return true
}

func one(h http.Header, name string) (string, bool) {
	v := h.Values(name)
	return h.Get(name), len(v) == 1 && v[0] != "" && strings.TrimSpace(v[0]) == v[0]
}

func credentials(h http.Header, p relay.Profile) (string, error) {
	// Mixed credentials, browser sessions and payment capabilities never become
	// inference identity. SDK metadata headers may be present but are not forwarded.
	if len(h.Values("Cookie")) != 0 || len(h.Values("Payment-Proof")) != 0 || len(h.Values("Anthropic-Beta")) != 0 {
		return "", fault.New(fault.Invalid, "One native SDK credential without payment, beta or session headers required")
	}
	a, hasA := one(h, "Authorization")
	x, hasX := one(h, "X-Api-Key")
	if len(h.Values("Authorization")) > 1 || len(h.Values("X-Api-Key")) > 1 ||
		len(h.Values("Authorization")) == 1 && !hasA || len(h.Values("X-Api-Key")) == 1 && !hasX || hasA == hasX {
		return "", fault.New(fault.Unauthenticated, "Exactly one SDK credential required")
	}
	if p == relay.MessagesText {
		v, ok := one(h, "Anthropic-Version")
		if !ok || v != relay.AnthropicVersion {
			return "", fault.New(fault.Invalid, "Explicit supported Anthropic version required")
		}
	} else if len(h.Values("Anthropic-Version")) != 0 || len(h.Values("X-Api-Key")) != 0 {
		return "", fault.New(fault.Invalid, "OpenAI bearer credential required")
	}
	if hasA {
		if !strings.HasPrefix(a, "Bearer ") || strings.Count(a, " ") != 1 {
			return "", fault.New(fault.Unauthenticated, "SDK bearer credential required")
		}
		x = strings.TrimPrefix(a, "Bearer ")
	}
	if len(x) < 16 || len(x) > 256 || !strings.HasPrefix(x, "p11k.") || strings.ContainsAny(x, " \t\r\n,") {
		return "", fault.New(fault.Unauthenticated, "SDK credential required")
	}
	return x, nil
}

func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, b := range []byte(key) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

// nativeError uses each SDK's error envelope, never arbitrary internal error
// messages, received bodies, original stop text or a key in a URL/log.
func nativeError(w http.ResponseWriter, p relay.Profile, requestID string, status int, code string) {
	message := "Native request unavailable; keep the original Idempotency-Key and body for recovery."
	typ := "api_error"
	switch status {
	case 400, 405, 413:
		typ = "invalid_request_error"
	case 401:
		typ = "authentication_error"
	case 403:
		typ = "permission_error"
	case 404:
		typ = "not_found_error"
	case 429:
		typ = "rate_limit_error"
	}
	var envelope any = map[string]any{"error": map[string]any{"message": message, "type": typ, "param": nil, "code": code}}
	if p == relay.MessagesText {
		envelope = map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}, "request_id": requestID}
	}
	if status == 429 {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope)
}

func faultStatus(err error) (int, string) {
	code := fault.Public(err).Code
	switch code {
	case fault.Invalid:
		return 400, string(code)
	case fault.Unauthenticated:
		return 401, string(code)
	case fault.Forbidden:
		return 403, string(code)
	case fault.NotFound:
		return 404, string(code)
	case fault.Conflict:
		return 409, string(code)
	}
	return 503, string(code)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()
	}
	defer func() {
		for _, name := range []string{"Authorization", "X-Api-Key", "Payment-Proof", "Cookie"} {
			r.Header.Del(name)
		}
	}()
	requestID := ids.New("req")
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Request-Id", requestID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	p := profile(r.URL.Path)
	fail := func(status int, code string) { nativeError(w, p, requestID, status, code) }
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		fail(503, "SDK_CLOSING")
		return
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	if !s.hosts[r.Host] || len(r.Header.Values("Origin")) != 0 || len(r.Header.Values("Referer")) != 0 {
		fail(403, "SDK_HOST_OR_BROWSER_ORIGIN")
		return
	}
	if len(r.RequestURI) > 512 || r.URL.IsAbs() || r.URL.Host != "" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" || p == "" {
		fail(404, "SDK_NATIVE_PATH")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(405, "SDK_NATIVE_METHOD")
		return
	}
	ct, ok := one(r.Header, "Content-Type")
	media, params, err := mime.ParseMediaType(ct)
	if !ok || err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") || len(r.Header.Values("Content-Encoding")) != 0 {
		fail(400, "SDK_NATIVE_JSON")
		return
	}
	key, ok := one(r.Header, "Idempotency-Key")
	if !ok || !validKey(key) {
		fail(400, "ORIGINAL_IDEMPOTENCY_KEY_REQUIRED")
		return
	}
	secret, err := credentials(r.Header, p)
	if err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	r.Header.Del("Authorization")
	r.Header.Del("X-Api-Key")
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(peer) == nil {
		fail(400, "SDK_PEER_REQUIRED")
		return
	}
	if !s.take("ip:"+peer, 120) {
		fail(429, "SDK_ADMISSION_LIMIT")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(429, "SDK_BUSY")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.c.Wait)
	defer cancel()
	stop := context.AfterFunc(s.life, cancel)
	defer stop()
	// The shared control server keeps its 20s write bound. Only this explicit
	// handler gets a finite longer data-plane bound; no global timeout is widened.
	controller := http.NewResponseController(w)
	if err = controller.SetWriteDeadline(time.Now().Add(s.c.Wait + 5*time.Second)); err != nil && err != http.ErrNotSupported {
		fail(503, "SDK_WRITE_BOUND")
		return
	}
	principal, err := s.c.Keys.Authenticate(ctx, secret)
	secret = ""
	if err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	if principal.Kind != identity.APIKey || !ids.Valid(principal.KeyID) || principal.Require("route:invoke", "") != nil {
		fail(403, "SDK_KEY_SCOPE")
		return
	}
	if !s.take("key:"+principal.KeyID, 60) {
		fail(429, "SDK_KEY_LIMIT")
		return
	}
	if r.Body == nil {
		fail(400, "SDK_NATIVE_BODY")
		return
	}
	if r.ContentLength > RequestBytes {
		fail(413, "SDK_BODY_LIMIT")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, RequestBytes+1))
	defer clear(body)
	if err != nil || len(body) == 0 {
		fail(400, "SDK_NATIVE_BODY")
		return
	}
	if len(body) > RequestBytes {
		fail(413, "SDK_BODY_LIMIT")
		return
	}
	_, meta, err := relay.Inspect(p, body)
	if err != nil || meta.Stream {
		fail(400, "SDK_NATIVE_NONSTREAM_SUBSET")
		return
	}
	prepared, err := s.c.Calls.Begin(ctx, principal, p, key, body)
	clear(body)
	if err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	w.Header().Set("X-P11-Job-ID", prepared.JobID)
	if prepared.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	state, err := s.wait(ctx, principal, prepared.JobID)
	if err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	if state.Spec.Protocol != p || state.Spec.Stream {
		fail(503, "SDK_ORIGINAL_PROFILE")
		return
	}
	encoded, err := s.render(ctx, principal, state)
	if err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	defer clear(encoded)
	if err = s.c.Calls.Recheck(ctx, principal, state.Job, state.Head.Delivery, state.Head.Digest()); err != nil {
		status, code := faultStatus(err)
		fail(status, code)
		return
	}
	if ctx.Err() != nil {
		fail(503, "ORIGINAL_RESULT_PENDING")
		return
	}
	w.Header().Set("X-P11-Result-Decision", "REQUIRED")
	w.WriteHeader(http.StatusOK)
	// A partial network write is not success evidence or an instruction to
	// release a resource. Recovery uses the same request, not a new purchase.
	_, _ = w.Write(encoded)
}

func (s *Server) wait(ctx context.Context, p identity.Principal, job string) (invocation.SDKState, error) {
	timer := time.NewTicker(500 * time.Millisecond)
	defer timer.Stop()
	for {
		v, err := s.c.Calls.State(ctx, p, job)
		if err != nil {
			return invocation.SDKState{}, err
		}
		if v.Job != job || v.Order == "" || v.Route == "" || v.Seller == "" {
			return invocation.SDKState{}, fault.New(fault.Unavailable, "Original SDK state unavailable")
		}
		if v.Head != nil {
			return v, nil
		}
		switch v.State {
		case "INPUT_PENDING", "QUEUED", "RUNNING", "VALIDATING", "WAITING_BUYER", "ACCEPTED", "UNVERIFIED":
		default:
			return invocation.SDKState{}, fault.New(fault.Conflict, "Original SDK task is terminal without readable content")
		}
		select {
		case <-ctx.Done():
			return invocation.SDKState{}, fault.New(fault.Unavailable, "Original result pending; recover the same request")
		case <-timer.C:
		}
	}
}
