package sdkhttp

// SOURCE_ONLY. Synthetic signatures/adapters define transport failure cases;
// they are not original payments, real providers or public-SDK certification.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/resultwire"
)

const fakeSecret = "p11k.key_synthetic.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type syntheticKeys struct{ deny bool }

func (a syntheticKeys) Authenticate(_ context.Context, secret string) (identity.Principal, error) {
	if a.deny || secret != fakeSecret {
		return identity.Principal{}, fault.New(fault.Unauthenticated, "synthetic denied")
	}
	return identity.Principal{Kind: identity.APIKey, UserID: "buyer_original", KeyID: "key_synthetic", Version: 1, Scopes: []string{"route:invoke"}, RouteIDs: []string{"route_original"}}, nil
}

type syntheticCalls struct {
	state            invocation.SDKState
	text             []byte
	beginBody        []byte
	beginKey         string
	begins           atomic.Int32
	rechecks         atomic.Int32
	revoked, corrupt bool
	block            chan struct{}
	entered          chan struct{}
}

func (c *syntheticCalls) Begin(ctx context.Context, _ identity.Principal, _ relay.Profile, key string, b []byte) (invocation.Preparation, error) {
	c.begins.Add(1)
	if c.block != nil {
		if c.entered != nil {
			c.entered <- struct{}{}
		}
		select {
		case <-c.block:
		case <-ctx.Done():
			return invocation.Preparation{}, ctx.Err()
		}
	} else {
		c.beginBody = append([]byte(nil), b...)
		c.beginKey = key
	}
	return invocation.Preparation{JobID: "job_original", Replayed: true}, nil
}
func (c *syntheticCalls) State(context.Context, identity.Principal, string) (invocation.SDKState, error) {
	return c.state, nil
}
func (c *syntheticCalls) Chunk(_ context.Context, _ identity.Principal, _ string, delivery, head string, index int) (resultwire.ContentChunk, error) {
	start := index * resultwire.ChunkBytes
	end := start + resultwire.ChunkBytes
	if end > len(c.text) {
		end = len(c.text)
	}
	b := append([]byte(nil), c.text[start:end]...)
	out := resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: c.state.Head.Declaration.Scope, Delivery: delivery, Head: head, Index: index, Content: b, Digest: resultwire.BytesDigest(b)}
	if c.corrupt {
		out.Scope.Seller = "seller_other"
	}
	return out, nil
}
func (c *syntheticCalls) Recheck(context.Context, identity.Principal, string, string, string) error {
	c.rechecks.Add(1)
	if c.revoked {
		return fault.New(fault.Unauthenticated, "synthetic revoked")
	}
	return nil
}
func originalSynthetic(t *testing.T, p relay.Profile) *syntheticCalls {
	t.Helper()
	m := executionlease.Manifest{Profile: executionlease.ManifestProfile, ID: "manifest_original", Job: "job_original", Attempt: "attempt_original", Order: "order_original", Route: "route_original", Seller: "seller_original", Device: "device_original", Resource: "resource_original", Capability: "cap_original", Generation: "1", Fence: "1", ContractHash: strings.Repeat("a", 64), EnvironmentRef: "env_original", EnvironmentDigest: strings.Repeat("b", 64), EngineKind: "LLAMA_CPP_NATIVE", EngineRelease: "engine_original", ModelDigest: strings.Repeat("c", 64), RecipeDigest: strings.Repeat("d", 64), InputAsset: "asset_original", InputDigest: strings.Repeat("e", 64), InputBytes: 100, Mode: "PLAINTEXT", Privacy: "STANDARD", InputTokenLimit: 32, HardDeadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Spec: executionlease.Spec{Protocol: p, Model: "model_original", MaxOutputTokens: 16}}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{87}, 32))
	text := []byte("你好，synthetic original")
	completed := time.Now().Add(-time.Second).UTC()
	d, err := resultwire.SignDelivery(key, resultwire.Delivery{Profile: resultwire.DeliveryProfile, Scope: resultwire.Scope{Job: m.Job, Attempt: m.Attempt, Lease: "lease_original", Order: m.Order, Seller: m.Seller, Device: m.Device, Resource: m.Resource, Generation: "1", Fence: "1", ContractHash: m.ContractHash, ManifestHash: m.Digest(), Environment: m.EnvironmentDigest}, Result: "result_original", Revision: 1, OutputHash: resultwire.BytesDigest(text), OutputBytes: int64(len(text)), InputTokens: 8, OutputTokens: 4, Finish: "eog", Completed: completed.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	h := resultwire.ContentHead{Profile: resultwire.ContentProfile, Delivery: "delivery_original", Declaration: d, Public: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey)), Manifest: m, ChunkBytes: resultwire.ChunkBytes, Chunks: resultwire.ChunkCount(d.OutputBytes), Expires: completed.Add(30 * time.Minute).Format(time.RFC3339Nano), State: "READY", End: &resultwire.End{}}
	if h.Validate() != nil {
		t.Fatal("invalid synthetic content")
	}
	return &syntheticCalls{text: text, state: invocation.SDKState{Job: m.Job, Order: m.Order, Route: m.Route, Seller: m.Seller, State: "WAITING_BUYER", Spec: m.Spec, Head: &h}}
}
func testServer(t *testing.T, c *syntheticCalls) *Server {
	t.Helper()
	s, err := New(Config{AllowedHosts: []string{"127.0.0.1:8787"}, Keys: syntheticKeys{}, Calls: c, Wait: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}
func request(p relay.Profile) *http.Request {
	path, body := "/v1/chat/completions", `{"model":"model_original","messages":[{"role":"user","content":"synthetic"}],"max_tokens":16}`
	if p == relay.ResponsesText {
		path, body = "/v1/responses", `{"model":"model_original","input":"synthetic","store":false,"max_output_tokens":16}`
	}
	if p == relay.MessagesText {
		path = "/v1/messages"
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Host = "127.0.0.1:8787"
	r.RemoteAddr = "127.0.0.1:56000"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-call-key")
	if p == relay.MessagesText {
		r.Header.Set("X-Api-Key", fakeSecret)
		r.Header.Set("Anthropic-Version", relay.AnthropicVersion)
	} else {
		r.Header.Set("Authorization", "Bearer "+fakeSecret)
	}
	return r
}
func TestNativeSDKThreeProfilesRenderOriginalWithoutAccepting(t *testing.T) {
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		t.Run(string(p), func(t *testing.T) {
			c := originalSynthetic(t, p)
			s := testServer(t, c)
			r := request(p)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 200 || !strings.Contains(w.Body.String(), string(c.text)) || c.begins.Load() != 1 || c.rechecks.Load() != 1 || c.beginKey != "original-call-key" || w.Header().Get("X-P11-Result-Decision") != "REQUIRED" {
				t.Fatal(w.Code, w.Body.String())
			}
			if len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("X-Api-Key")) != 0 {
				t.Fatal("credential retained")
			}
			var parsed map[string]any
			if json.Unmarshal(w.Body.Bytes(), &parsed) != nil {
				t.Fatal("not native JSON")
			}
			if p == relay.ChatText && parsed["object"] != "chat.completion" || p == relay.ResponsesText && parsed["object"] != "response" || p == relay.MessagesText && parsed["type"] != "message" {
				t.Fatal(parsed)
			}
		})
	}
}
func TestNativeSDKRejectsMalformedHeadersBeforeAdmission(t *testing.T) {
	cases := map[string]func(*http.Request){
		"missing_key":    func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		"duplicate_key":  func(r *http.Request) { r.Header.Add("Idempotency-Key", "other-key") },
		"cookie":         func(r *http.Request) { r.Header.Set("Cookie", "session=synthetic") },
		"browser":        func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:8787") },
		"mixed_auth":     func(r *http.Request) { r.Header.Set("X-Api-Key", fakeSecret) },
		"blank_auth":     func(r *http.Request) { r.Header.Set("Authorization", " ") },
		"duplicate_auth": func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+fakeSecret) },
		"wrong_host":     func(r *http.Request) { r.Host = "other.invalid" },
		"query_token":    func(r *http.Request) { r.URL.RawQuery = "key=private" },
		"encoded_path":   func(r *http.Request) { r.URL.RawPath = "/v1/%63hat/completions" },
		"gzip":           func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
		"proof":          func(r *http.Request) { r.Header.Set("Payment-Proof", "private") },
		"beta":           func(r *http.Request) { r.Header.Set("Anthropic-Beta", "anything") },
		"method":         func(r *http.Request) { r.Method = "GET" },
		"peer":           func(r *http.Request) { r.RemoteAddr = "untrusted" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := originalSynthetic(t, relay.ChatText)
			s := testServer(t, c)
			r := request(relay.ChatText)
			mutate(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code < 400 || c.begins.Load() != 0 || strings.Contains(w.Body.String(), fakeSecret) {
				t.Fatal(w.Code, "admission or disclosure")
			}
		})
	}
}
func TestNativeSDKStreamToolsAndOversizeNeverReserve(t *testing.T) {
	for _, body := range []string{`{"model":"model_original","messages":[{"role":"user","content":"synthetic"}],"max_tokens":16,"stream":true}`, `{"model":"model_original","messages":[{"role":"user","content":"synthetic"}],"max_tokens":16,"tools":[]}`, strings.Repeat(" ", RequestBytes+1)} {
		c := originalSynthetic(t, relay.ChatText)
		s := testServer(t, c)
		r := request(relay.ChatText)
		r.Body = ioBody(body)
		r.ContentLength = int64(len(body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code < 400 || c.begins.Load() != 0 {
			t.Fatal(w.Code, "unsupported body admitted")
		}
	}
}

type stringBody struct{ *strings.Reader }

func (stringBody) Close() error  { return nil }
func ioBody(s string) stringBody { return stringBody{strings.NewReader(s)} }
func TestNativeSDKBadChunkOrRevocationCannotSendSuccessfulBody(t *testing.T) {
	for _, badChunk := range []bool{true, false} {
		c := originalSynthetic(t, relay.ChatText)
		c.corrupt = badChunk
		c.revoked = !badChunk
		s := testServer(t, c)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, request(relay.ChatText))
		if w.Code < 400 || strings.Contains(w.Body.String(), string(c.text)) {
			t.Fatal(w.Code, "unverified content escaped")
		}
	}
}
func TestNativeSDKAggregateMismatchCannotEmitSuccess(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	c.text[0] ^= 1
	s := testServer(t, c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 503 || strings.Contains(w.Body.String(), "chat.completion") {
		t.Fatal("aggregate mismatch accepted")
	}
}
func TestNativeSDKAnthropicVersionAndMixedBlankHeaderFailBeforeAdmission(t *testing.T) {
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Anthropic-Version") }, func(r *http.Request) { r.Header.Set("Anthropic-Version", "unknown") }, func(r *http.Request) { r.Header.Set("Authorization", "") }} {
		c := originalSynthetic(t, relay.MessagesText)
		s := testServer(t, c)
		r := request(relay.MessagesText)
		mutate(r)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code < 400 || c.begins.Load() != 0 {
			t.Fatal("ambiguous version/identity")
		}
	}
}
func TestNativeSDKCloseCancelsAndJoinsOwnedAdmission(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	c.block = make(chan struct{})
	c.entered = make(chan struct{}, 1)
	s := testServer(t, c)
	done := make(chan struct{})
	go func() { defer close(done); s.ServeHTTP(httptest.NewRecorder(), request(relay.ChatText)) }()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("admission not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s.Close(ctx) != nil {
		t.Fatal("close did not join")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker retained")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 503 || c.begins.Load() != 1 {
		t.Fatal("closed host admitted")
	}
}
func TestNativeSDKFourCallsBoundConcurrency(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	c.block = make(chan struct{})
	c.entered = make(chan struct{}, 4)
	s := testServer(t, c)
	for range 4 {
		go s.ServeHTTP(httptest.NewRecorder(), request(relay.ChatText))
	}
	for range 4 {
		select {
		case <-c.entered:
		case <-time.After(time.Second):
			t.Fatal("missing bounded caller")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 429 || c.begins.Load() != 4 {
		t.Fatal("unbounded calls")
	}
	if s.Close(context.Background()) != nil {
		t.Fatal("close failed")
	}
}
func TestNativeSDKPendingTimeoutDoesNotReissueOrReturnEmptySuccess(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	c.state.Head = nil
	c.state.State = "RUNNING"
	s, err := New(Config{AllowedHosts: []string{"127.0.0.1:8787"}, Keys: syntheticKeys{}, Calls: c, Wait: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 503 || c.begins.Load() != 1 || c.rechecks.Load() != 0 || w.Header().Get("X-P11-Job-ID") != "job_original" {
		t.Fatal("pending request lost/reissued", w.Code)
	}
}
func TestNativeSDKAuthenticationFailureHasNoAdmission(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	s, err := New(Config{AllowedHosts: []string{"127.0.0.1:8787"}, Keys: syntheticKeys{deny: true}, Calls: c})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 401 || c.begins.Load() != 0 {
		t.Fatal("failed key admitted")
	}
}
func TestNativeSDKResultLimitFailsBeforeAnyPublicContent(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	c.text = bytes.Repeat([]byte{'a'}, TextBytes+1)
	d := c.state.Head.Declaration
	d.OutputBytes = int64(len(c.text))
	d.OutputHash = resultwire.BytesDigest(c.text)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{87}, 32))
	var err error
	d, err = resultwire.SignDelivery(key, d)
	if err != nil {
		t.Fatal(err)
	}
	c.state.Head.Declaration = d
	c.state.Head.Chunks = resultwire.ChunkCount(d.OutputBytes)
	if c.state.Head.Validate() != nil {
		t.Fatal("invalid limit fixture")
	}
	s := testServer(t, c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request(relay.ChatText))
	if w.Code != 503 || strings.Contains(w.Body.String(), string(c.text)) {
		t.Fatal("oversized output escaped")
	}
}
func TestNativeSDKRateBucketsRemainBoundedWithoutSecretKeys(t *testing.T) {
	c := originalSynthetic(t, relay.ChatText)
	s := testServer(t, c)
	for i := 0; i < 4096; i++ {
		if !s.take("ip:synthetic_"+time.Unix(int64(i), 0).Format(time.RFC3339), 1) {
			t.Fatal("early capacity")
		}
	}
	if s.take("new", 1) || len(s.buckets) != 4096 {
		t.Fatal("unbounded peer keys")
	}
	for key := range s.buckets {
		if strings.Contains(key, fakeSecret) {
			t.Fatal("credential used as bucket key")
		}
	}
}
