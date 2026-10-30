package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type admitFunc func(context.Context, Credential, Intent) (Grant, error)

func (f admitFunc) Admit(ctx context.Context, c Credential, i Intent) (Grant, error) {
	return f(ctx, c, i)
}

type secretFunc func(context.Context, string, string) (string, error)

func (f secretFunc) Lookup(ctx context.Context, s, r string) (string, error) { return f(ctx, s, r) }

func testBinding(p Profile, url string) Binding {
	return Binding{
		RouteID: "route_1", ContractHash: strings.Repeat("a", 64), SellerID: "seller_1", ListingRevision: "rev_1",
		Mode: "STANDARD", ProfileRevision: "1", PublicModel: "mesh-local-text", BackendModel: "engine-local-text",
		Profile: p, Endpoint: url + p.path(), SecretRef: "secret_1", AllowLoopbackHTTP: true,
	}
}
func testGrant(b Binding) Grant {
	return Grant{InvocationID: "inv_1", SellerID: b.SellerID, ContractHash: b.ContractHash, RouteID: b.RouteID,
		AuthorityKind: "JOB_LEASE", ResourceID: "gpu_1", Fence: 1, ResourceGeneration: 1,
		Deadline: time.Now().Add(time.Minute), Revoked: make(chan struct{})}
}
func fixture(t *testing.T, p Profile, h http.HandlerFunc, change func(*Config)) *Core {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	b := testBinding(p, up.URL)
	config := Config{Domain: PlatformPlain, Binding: b,
		Authorizer: admitFunc(func(_ context.Context, cred Credential, i Intent) (Grant, error) {
			if cred.Value != "buyer-secret" || i.Binding != b {
				return Grant{}, ErrUnauthorized
			}
			return testGrant(b), nil
		}), Secrets: secretFunc(func(_ context.Context, s, r string) (string, error) {
			if s != b.SellerID || r != b.SecretRef {
				return "", ErrForbidden
			}
			return "upstream-secret", nil
		})}
	if change != nil {
		change(&config)
	}
	c, e := New(config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.Close)
	return c
}
func requestBody(p Profile, stream bool) string {
	switch p {
	case ChatText:
		return fmt.Sprintf(`{"model":"mesh-local-text","messages":[{"role":"user","content":"你好"}],"max_completion_tokens":128,"stream":%t}`, stream)
	case ResponsesText:
		return fmt.Sprintf(`{"model":"mesh-local-text","input":"你好","store":false,"max_output_tokens":128,"stream":%t}`, stream)
	case MessagesText:
		return fmt.Sprintf(`{"model":"mesh-local-text","system":"private instruction","messages":[{"role":"user","content":[{"type":"text","text":"你好"}]}],"max_tokens":128,"stream":%t}`, stream)
	}
	return ""
}
func nativeRequest(c *Core, body string) *http.Request {
	r := httptest.NewRequest("POST", c.binding.Profile.path(), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if c.binding.Profile == MessagesText {
		r.Header.Set("x-api-key", "buyer-secret")
		r.Header.Set("anthropic-version", AnthropicVersion)
	} else {
		r.Header.Set("Authorization", "Bearer buyer-secret")
	}
	return r
}

type capture struct {
	header    http.Header
	body      bytes.Buffer
	status    int
	deadline  time.Time
	writes    int
	failWrite int
}

type slowCapture struct{ *capture }

func (w *slowCapture) Write(p []byte) (int, error) {
	if w.writes == 1 {
		if remaining := time.Until(w.deadline); remaining > 0 {
			timer := time.NewTimer(remaining)
			defer timer.Stop()
			<-timer.C
		}
		w.writes++
		return 0, context.DeadlineExceeded
	}
	return w.capture.Write(p)
}

func newCapture() *capture             { return &capture{header: make(http.Header)} }
func (w *capture) Header() http.Header { return w.header }
func (w *capture) WriteHeader(n int)   { w.status = n }
func (w *capture) Write(p []byte) (int, error) {
	w.writes++
	if w.failWrite > 0 && w.writes >= w.failWrite {
		return 0, io.ErrClosedPipe
	}
	return w.body.Write(p)
}
func (w *capture) Flush()                             {}
func (w *capture) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }
func (w *capture) SetReadDeadline(time.Time) error    { return nil }
func (w *capture) FlushError() error                  { return nil }

func jsonResponse(p Profile) string {
	switch p {
	case ChatText:
		return `{"id":"chat_1","object":"chat.completion","created":1,"model":"engine-local-text","choices":[{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
	case ResponsesText:
		return `{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"engine-local-text","store":false,"output":[{"id":"item_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"你好","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`
	case MessagesText:
		return `{"id":"msg_1","type":"message","role":"assistant","model":"engine-local-text","content":[{"type":"text","text":"你好"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":2}}`
	}
	return ""
}
func chatEvents(includeUsage bool) string {
	prefix := `{"id":"chat_1","object":"chat.completion.chunk","created":1,"model":"engine-local-text",`
	s := "data: " + prefix + `"choices":[{"index":0,"delta":{"role":"assistant","content":"你"},"finish_reason":null}]}` + "\r\n\r\n"
	s += "data: " + prefix + `"choices":[{"index":0,"delta":{"content":"好"},"finish_reason":"stop"}]}` + "\n\n"
	if includeUsage {
		s += "data: " + prefix + `"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n\n"
	}
	return s + "data: [DONE]\n\n"
}
func messageEvents() string {
	return `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"engine-local-text","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}

event: ping
data: {"type":"ping"}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你好"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`
}
func responsesEvents() string {
	var out strings.Builder
	response := child([]byte(jsonResponse(ResponsesText)))
	start := child(encode(response))
	start["status"] = encode("in_progress")
	start["output"] = encode([]any{})
	start["usage"] = []byte("null")
	item := child(childArray(response["output"])[0])
	part := child(childArray(item["content"])[0])
	startItem := child(encode(item))
	startItem["status"] = encode("in_progress")
	startItem["content"] = encode([]any{})
	startPart := child(encode(part))
	startPart["text"] = encode("")
	events := []map[string]any{
		{"type": "response.created", "response": start},
		{"type": "response.in_progress", "response": start},
		{"type": "response.output_item.added", "output_index": 0, "item": startItem},
		{"type": "response.content_part.added", "output_index": 0, "item_id": "item_1", "content_index": 0, "part": startPart},
		{"type": "response.output_text.delta", "output_index": 0, "item_id": "item_1", "content_index": 0, "delta": "你好"},
		{"type": "response.output_text.done", "output_index": 0, "item_id": "item_1", "content_index": 0, "text": "你好"},
		{"type": "response.content_part.done", "output_index": 0, "item_id": "item_1", "content_index": 0, "part": part},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": response},
	}
	for i, e := range events {
		e["sequence_number"] = i
		fmt.Fprintf(&out, "event: %s\ndata: %s\n\n", e["type"], encode(e))
	}
	return out.String()
}
func childArray(v json.RawMessage) []json.RawMessage { a, _ := array(v); return a }
func emit(w http.ResponseWriter, p Profile, stream string) {
	w.Header().Set("Content-Type", "application/json")
	if stream != "" {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	} else {
		_, _ = io.WriteString(w, jsonResponse(p))
	}
}

func TestRelayNativeJSONThreeDialectsAndFixedSeller(t *testing.T) {
	for _, p := range []Profile{ChatText, ResponsesText, MessagesText} {
		t.Run(string(p), func(t *testing.T) {
			var calls atomic.Int32
			c := fixture(t, p, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), `"model":"engine-local-text"`) || r.URL.Path != p.path() {
					t.Error("wrong bound target")
				}
				emit(w, p, "")
			}, nil)
			w := newCapture()
			out := c.Forward(w, nativeRequest(c, requestBody(p, false)))
			if w.status != 200 || !out.SubmissionAttempted || !out.Delivered || !out.TerminalValidated || out.Terminal != "COMPLETED" || out.Usage == nil || *out.Usage != (Usage{3, 2}) || calls.Load() != 1 {
				t.Fatalf("invalid forwarding facts: %+v status=%d", out, w.status)
			}
			if strings.Contains(w.body.String(), "engine-local-text") || !strings.Contains(w.body.String(), "mesh-local-text") {
				t.Fatal("model alias escaped route")
			}
		})
	}
}

func TestRelayRejectsRequestAmbiguityBeforeAdmission(t *testing.T) {
	var calls, admissions atomic.Int32
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }, func(c *Config) {
		c.Authorizer = admitFunc(func(context.Context, Credential, Intent) (Grant, error) {
			admissions.Add(1)
			return testGrant(c.Binding), nil
		})
	})
	base := requestBody(ChatText, false)
	cases := []string{
		strings.Replace(base, `"model":`, `"model":"mesh-local-text","model":`, 1),
		strings.Replace(base, `"role":"user"`, `"role":"user","role":"assistant"`, 1),
		strings.Replace(base, `"model"`, `"Model"`, 1), base + `{}`,
		strings.Replace(base, "你好", `\ud800`, 1), strings.Replace(base, "你好", string([]byte{0xff}), 1),
		strings.Replace(base, `"max_completion_tokens":128`, `"max_completion_tokens":null`, 1),
		strings.Replace(base, `128`, `1e2`, 1), strings.Replace(base, `128`, `32769`, 1),
		strings.Replace(base, `"model":"mesh-local-text"`, `"model":"different"`, 1),
		strings.Replace(base, `"role":"user"`, `"role":"tool"`, 1),
		strings.Replace(base, `"stream":false`, `"stream":false,"tools":[]`, 1),
		strings.Replace(base, `"stream":false`, `"stream":false,"upstream_url":"http://evil.invalid"`, 1),
		strings.Replace(base, `"stream":false`, `"stream":false,"store":true`, 1),
		strings.Replace(base, `"stream":false`, `"stream":false,"stream_options":{"include_usage":true}`, 1),
		strings.Replace(base, `"max_completion_tokens":128`, `"max_completion_tokens":128,"max_tokens":128`, 1),
	}
	for i, b := range cases {
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, b))
		if w.status != 400 || o.SubmissionAttempted {
			t.Fatalf("case %d accepted", i)
		}
	}
	if calls.Load() != 0 || admissions.Load() != 0 {
		t.Fatal("invalid request reached admission or engine")
	}
	valid := strings.Replace(base, "你好", `\ud83d\ude80`, 1)
	if _, e := validateRequest([]byte(valid), c.binding, c.limits); e != nil {
		t.Fatal("valid surrogate pair rejected")
	}
}

func TestRelayProfilesRejectToolsStateBetaAndMixedCredentials(t *testing.T) {
	for _, p := range []Profile{ChatText, ResponsesText, MessagesText} {
		var calls atomic.Int32
		c := fixture(t, p, func(http.ResponseWriter, *http.Request) { calls.Add(1) }, nil)
		for _, field := range []string{`"tools":[]`, `"thinking":{"type":"enabled"}`, `"previous_response_id":"resp_0"`, `"metadata":{"secret":"x"}`} {
			b := requestBody(p, false)
			b = b[:len(b)-1] + "," + field + "}"
			w := newCapture()
			c.Forward(w, nativeRequest(c, b))
			if w.status != 400 {
				t.Fatal("unsupported field accepted", p, field)
			}
		}
		for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Set("anthropic-beta", "any") }, func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer buyer-secret")
			r.Header.Set("x-api-key", "buyer-secret")
		}, func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }} {
			r := nativeRequest(c, requestBody(p, false))
			change(r)
			w := newCapture()
			c.Forward(w, r)
			if w.status < 400 {
				t.Fatal("ambiguous header accepted")
			}
		}
		if calls.Load() != 0 {
			t.Fatal("invalid profile contacted upstream")
		}
	}
	for _, b := range []string{strings.Replace(requestBody(ResponsesText, false), `"store":false,`, "", 1), strings.Replace(requestBody(ResponsesText, false), `"store":false`, `"store":true`, 1)} {
		if _, e := validateRequest([]byte(b), testBinding(ResponsesText, "http://127.0.0.1"), DefaultLimits()); e == nil {
			t.Fatal("stateful default accepted")
		}
	}
}

func TestRelayStripsBuyerSecretsProxyAndBackendHeaders(t *testing.T) {
	var called atomic.Int32
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("wrong upstream identity")
		}
		for _, k := range []string{"Cookie", "X-Api-Key", "X-Forwarded-For", "X-Upstream-Url", "Idempotency-Key", "Connection"} {
			if r.Header.Get(k) != "" {
				t.Error("buyer header escaped", k)
			}
		}
		w.Header().Set("Set-Cookie", "backend-secret")
		w.Header().Set("X-Debug", "prompt-secret")
		w.Header().Set("Location", "http://private.invalid")
		emit(w, ChatText, "")
	}, nil)
	r := nativeRequest(c, requestBody(ChatText, false))
	for k, v := range map[string]string{"Cookie": "buyer-cookie", "X-Forwarded-For": "127.0.0.2", "X-Upstream-Url": "http://evil.invalid", "Idempotency-Key": "stable-key"} {
		r.Header.Set(k, v)
	}
	w := newCapture()
	o := c.Forward(w, r)
	if !o.Delivered || called.Load() != 1 {
		t.Fatal(o)
	}
	for _, k := range []string{"Set-Cookie", "X-Debug", "Location"} {
		if w.Header().Get(k) != "" {
			t.Fatal("sensitive response header exposed")
		}
	}
}

func TestRelayAdmissionDenialAndGrantMismatchNeverSubmit(t *testing.T) {
	for _, kind := range []string{"auth", "forbidden", "unavailable", "seller", "route", "contract", "resource", "fence", "expired", "nil-revoke", "revoked", "kind"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			c := fixture(t, ChatText, func(http.ResponseWriter, *http.Request) { calls.Add(1) }, func(c *Config) {
				b := c.Binding
				c.Authorizer = admitFunc(func(context.Context, Credential, Intent) (Grant, error) {
					g := testGrant(b)
					switch kind {
					case "auth":
						return Grant{}, ErrUnauthorized
					case "forbidden":
						return Grant{}, ErrForbidden
					case "unavailable":
						return Grant{}, errors.New("secret vendor diagnostic")
					case "seller":
						g.SellerID = "other"
					case "route":
						g.RouteID = "other_route"
					case "contract":
						g.ContractHash = strings.Repeat("b", 64)
					case "resource":
						g.ResourceID = ""
					case "fence":
						g.Fence = 0
					case "expired":
						g.Deadline = time.Now().Add(-time.Second)
					case "nil-revoke":
						g.Revoked = nil
					case "revoked":
						ch := make(chan struct{})
						close(ch)
						g.Revoked = ch
					case "kind":
						g.AuthorityKind = "PURCHASE"
					}
					return g, nil
				})
			})
			w := newCapture()
			o := c.Forward(w, nativeRequest(c, requestBody(ChatText, false)))
			if w.status < 400 || o.SubmissionAttempted || calls.Load() != 0 || strings.Contains(w.body.String(), "vendor diagnostic") {
				t.Fatal("denied authority escaped", kind, o)
			}
		})
	}
}

func TestRelayFrozenBindingAndPrivacyDomains(t *testing.T) {
	base := Config{Domain: PlatformPlain, Binding: testBinding(ChatText, "http://127.0.0.1:1"), Authorizer: admitFunc(func(context.Context, Credential, Intent) (Grant, error) { return Grant{}, ErrNotReady }), Secrets: secretFunc(func(context.Context, string, string) (string, error) { return "x", nil })}
	for _, change := range []func(*Config){func(c *Config) { c.Binding.Mode = "E2E_BASIC" }, func(c *Config) { c.Binding.Mode = "E2E_ENHANCED" }, func(c *Config) { c.Domain = "CIPHER_RELAY" }, func(c *Config) { c.Binding.Endpoint = "http://example.com/v1/chat/completions" }, func(c *Config) { c.Binding.Endpoint = "http://127.0.0.1:1/v1/chat/completions?target=x" }, func(c *Config) { c.Binding.Endpoint = "http://user:pass@127.0.0.1:1/v1/chat/completions" }, func(c *Config) { c.Binding.AllowLoopbackHTTP = false }, func(c *Config) { c.Binding.ProfileRevision = "2" }, func(c *Config) { c.Authorizer = nil }} {
		c := base
		change(&c)
		if got, e := New(c); e == nil {
			got.Close()
			t.Fatal("unsafe config accepted")
		}
	}
	c := base
	core, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	defer core.Close()
	c.Binding.SellerID = "changed"
	if core.binding.SellerID != "seller_1" {
		t.Fatal("registry not frozen")
	}
	c = base
	c.Domain = SellerDecrypted
	c.Binding.Mode = "E2E_BASIC"
	core, e = New(c)
	if e != nil {
		t.Fatal(e)
	}
	core.Close()
}

func TestRelayRedirectAndErrorNeverRetryOrLeak(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	for _, status := range []int{307, 401, 429, 500} {
		var calls atomic.Int32
		c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", target.URL)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "prompt-secret upstream-secret private-host")
		}, nil)
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, requestBody(ChatText, false)))
		if calls.Load() != 1 || o.Delivered || o.TerminalValidated || w.status < 400 || strings.Contains(w.body.String(), "prompt-secret") {
			t.Fatal("unsafe error relay", status, o)
		}
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestRelayUnknownSubmissionIsAttemptedOnce(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			_ = conn.Close()
		}
	}, nil)
	r := nativeRequest(c, requestBody(ChatText, false))
	r.Header.Set("Idempotency-Key", "original-key")
	w := newCapture()
	o := c.Forward(w, r)
	if !o.SubmissionAttempted || o.TerminalValidated || o.Delivered || calls.Load() != 1 || o.Code != "upstream_interrupted" {
		t.Fatal("unknown submit retried or called complete", o)
	}
}

func TestRelayNativeStreamsAcrossByteBoundaries(t *testing.T) {
	for _, p := range []Profile{ChatText, ResponsesText, MessagesText} {
		t.Run(string(p), func(t *testing.T) {
			s := chatEvents(false)
			if p == ResponsesText {
				s = responsesEvents()
			}
			if p == MessagesText {
				s = messageEvents()
			}
			c := fixture(t, p, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, b := range []byte(s) {
					_, e := w.Write([]byte{b})
					if e != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}, nil)
			w := newCapture()
			o := c.Forward(w, nativeRequest(c, requestBody(p, true)))
			if w.status != 200 || !o.Delivered || !o.TerminalValidated || o.Terminal != "COMPLETED" {
				t.Fatalf("native stream invalid: %+v body=%s", o, w.body.String())
			}
			if p != ChatText && strings.Contains(w.body.String(), "[DONE]") {
				t.Fatal("chat terminal inserted into another dialect")
			}
			if strings.Contains(w.body.String(), "engine-local-text") {
				t.Fatal("model not bound")
			}
		})
	}
}

func TestRelayChatRequiresFinishAndRequestedUsageBeforeDone(t *testing.T) {
	for _, s := range []string{"data: [DONE]\n\n", strings.Replace(chatEvents(false), `"finish_reason":"stop"`, `"finish_reason":null`, 1), strings.TrimSuffix(chatEvents(false), "data: [DONE]\n\n"), chatEvents(false)} {
		c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { emit(w, ChatText, s) }, nil)
		b := requestBody(ChatText, true)
		b = b[:len(b)-1] + `,"stream_options":{"include_usage":true}}`
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, b))
		if o.TerminalValidated || o.Delivered || strings.Contains(w.body.String(), "data: [DONE]") {
			t.Fatal("premature chat completion", o)
		}
	}
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { emit(w, ChatText, chatEvents(true)) }, nil)
	b := requestBody(ChatText, true)
	b = b[:len(b)-1] + `,"stream_options":{"include_usage":true}}`
	w := newCapture()
	o := c.Forward(w, nativeRequest(c, b))
	if !o.Delivered || o.Usage == nil || *o.Usage != (Usage{3, 2}) {
		t.Fatal(o)
	}
}

func TestRelayMessagesRejectsUnclosedBlocksThinkingAndErrors(t *testing.T) {
	base := messageEvents()
	cases := []string{
		strings.Replace(base, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n", "", 1),
		strings.Replace(base, `"type":"text_delta"`, `"type":"thinking_delta"`, 1),
		strings.Replace(base, `"index":0`, `"index":1`, 1),
		strings.Replace(base, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"prompt-secret\"}}\n\n", 1),
		strings.TrimSuffix(base, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"),
	}
	for i, s := range cases {
		c := fixture(t, MessagesText, func(w http.ResponseWriter, r *http.Request) { emit(w, MessagesText, s) }, nil)
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, requestBody(MessagesText, true)))
		if o.Delivered || o.TerminalValidated || strings.Contains(w.body.String(), "prompt-secret") || strings.Contains(w.body.String(), "event: message_stop") {
			t.Fatal("invalid messages completed", i, o)
		}
	}
}

func TestRelayResponsesMaintainsItemsSequenceAndTextIntegrity(t *testing.T) {
	base := responsesEvents()
	cases := []string{
		strings.Replace(base, `"sequence_number":4`, `"sequence_number":3`, 1),
		strings.Replace(base, `"item_id":"item_1"`, `"item_id":"item_wrong"`, 1),
		strings.Replace(base, `"text":"你好"`, `"text":"mismatch"`, 1),
		strings.Replace(base, "event: response.output_text.delta", "event: response.function_call_arguments.delta", 1),
		strings.Replace(base, "event: response.completed", "event: response.incomplete", 1),
		strings.Replace(base, "event: response.content_part.done", "event: unknown", 1),
	}
	for i, s := range cases {
		c := fixture(t, ResponsesText, func(w http.ResponseWriter, r *http.Request) { emit(w, ResponsesText, s) }, nil)
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, requestBody(ResponsesText, true)))
		if o.Delivered || o.TerminalValidated || strings.Contains(w.body.String(), "event: response.completed") {
			t.Fatal("invalid responses completed", i, o)
		}
	}
}

func TestRelayUsageAbsentInvalidAndIncompleteRemainHonest(t *testing.T) {
	for _, p := range []Profile{ChatText, ResponsesText, MessagesText} {
		o := child([]byte(jsonResponse(p)))
		delete(o, "usage")
		body, kind, u, e := responseJSON(encode(o), testBinding(p, "http://127.0.0.1"), 512<<10)
		if e != nil || len(body) == 0 || kind != "COMPLETED" || u != nil {
			t.Fatal("missing usage fabricated", p, e)
		}
		o = child([]byte(jsonResponse(p)))
		if p == MessagesText {
			o["usage"] = encode(map[string]any{"input_tokens": -1, "output_tokens": 2})
		} else {
			u := child(o["usage"])
			u["total_tokens"] = encode(999)
			o["usage"] = encode(u)
		}
		if _, _, _, e = responseJSON(encode(o), testBinding(p, "http://127.0.0.1"), 512<<10); e == nil {
			t.Fatal("bad usage accepted")
		}
	}
	b := strings.Replace(jsonResponse(ChatText), `"finish_reason":"stop"`, `"finish_reason":"length"`, 1)
	_, kind, _, e := responseJSON([]byte(b), testBinding(ChatText, "http://127.0.0.1"), 512<<10)
	if e != nil || kind != "INCOMPLETE" {
		t.Fatal("truncation converted to completion")
	}
	if _, e := protocolUsage([]byte(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":4}}`), ChatText); e == nil {
		t.Fatal("overlapping usage escaped validation")
	}
}

func TestRelayBoundsRequestEventResponseAndMalformedSSE(t *testing.T) {
	var calls atomic.Int32
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }, nil)
	w := newCapture()
	c.Forward(w, nativeRequest(c, strings.Repeat("x", 256<<10+1)))
	if w.status != 413 || calls.Load() != 0 {
		t.Fatal("request size not bounded")
	}
	for _, s := range []string{"data: " + strings.Repeat("x", 64<<10) + "\n\n", "data: {}\n", "data: {\"a\":1,\"a\":2}\n\n", "event: a\nevent: b\ndata: {}\n\n", "data: " + string([]byte{0xff}) + "\n\n"} {
		c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { emit(w, ChatText, s) }, nil)
		w := newCapture()
		o := c.Forward(w, nativeRequest(c, requestBody(ChatText, true)))
		if o.Delivered || o.TerminalValidated {
			t.Fatal("invalid SSE accepted")
		}
	}
	c = fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat("x", 512<<10+1))
	}, nil)
	w = newCapture()
	o := c.Forward(w, nativeRequest(c, requestBody(ChatText, false)))
	if o.Delivered || w.status != 502 {
		t.Fatal("response size not bounded")
	}
}

func TestRelaySSECommentsMultilineCRAndEOFFraming(t *testing.T) {
	for _, separator := range []string{"\n", "\r\n", "\r"} {
		s := ": comment\n\nevent: ping\ndata: {\ndata: \"type\":\"ping\"}\n\n"
		s = strings.ReplaceAll(s, "\n", separator)
		reader := newEventReader(strings.NewReader(s), 1024)
		name, data, e := reader.next()
		if e != nil || name != "ping" || string(data) != "{\n\"type\":\"ping\"}" {
			t.Fatal("SSE line framing failed", separator, e)
		}
		if _, _, e = reader.next(); !errors.Is(e, io.EOF) {
			t.Fatal("EOF not represented")
		}
	}
	reader := newEventReader(strings.NewReader("data: [DONE]"), 1024)
	if _, _, e := reader.next(); !errors.Is(e, io.EOF) {
		t.Fatal("unterminated event dispatched")
	}
}

func TestRelayCancellationRevocationAndHardDeadlineReachUpstream(t *testing.T) {
	for _, kind := range []string{"caller", "revoke", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			revoked := make(chan struct{})
			c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
				close(stopped)
			}, func(c *Config) {
				b := c.Binding
				c.Authorizer = admitFunc(func(context.Context, Credential, Intent) (Grant, error) {
					g := testGrant(b)
					g.Revoked = revoked
					if kind == "deadline" {
						g.Deadline = time.Now().Add(100 * time.Millisecond)
					}
					return g, nil
				})
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := nativeRequest(c, requestBody(ChatText, true)).WithContext(ctx)
			done := make(chan Outcome, 1)
			go func() { done <- c.Forward(newCapture(), r) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("upstream not reached")
			}
			if kind == "caller" {
				cancel()
			}
			if kind == "revoke" {
				close(revoked)
			}
			select {
			case o := <-done:
				if !o.SubmissionAttempted || o.Delivered || o.TerminalValidated {
					t.Fatal("cancel claimed success", o)
				}
			case <-time.After(time.Second):
				t.Fatal("cancel unbounded")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("transport did not close upstream")
			}
		})
	}
}

func TestRelayKeepaliveCannotRenewIdleOrAbsoluteDeadline(t *testing.T) {
	for _, validPing := range []bool{false, true} {
		c := fixture(t, MessagesText, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if validPing {
						_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
					} else {
						_, _ = io.WriteString(w, ": keepalive\n\n")
					}
					w.(http.Flusher).Flush()
				}
			}
		}, func(c *Config) {
			c.Limits = DefaultLimits()
			c.Limits.Total = 150 * time.Millisecond
			c.Limits.FirstByte = 50 * time.Millisecond
			c.Limits.Idle = 60 * time.Millisecond
			c.Limits.Write = 40 * time.Millisecond
		})
		start := time.Now()
		o := c.Forward(newCapture(), nativeRequest(c, requestBody(MessagesText, true)))
		if time.Since(start) > time.Second || o.Delivered || o.TerminalValidated {
			t.Fatal("keepalive renewed execution", o)
		}
	}
}

func TestRelayConcurrencyAndDurableAdmissionPortOwnReplay(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-release:
			emit(w, ChatText, "")
		case <-r.Context().Done():
		}
	}, func(c *Config) { c.Limits = DefaultLimits(); c.Limits.Concurrent = 1 })
	done := make(chan Outcome, 1)
	go func() { done <- c.Forward(newCapture(), nativeRequest(c, requestBody(ChatText, false))) }()
	<-started
	w := newCapture()
	o := c.Forward(w, nativeRequest(c, requestBody(ChatText, false)))
	if w.status != 429 || o.SubmissionAttempted {
		t.Fatal("concurrency not bounded")
	}
	close(release)
	<-done
	// A repeated SDK call is a fresh HTTP call. The production durable gate,
	// rather than a prompt hash/cache in this library, owns original-key replay.
	var claimed atomic.Bool
	b := c.binding
	c.authorizer = admitFunc(func(_ context.Context, _ Credential, i Intent) (Grant, error) {
		if i.IdempotencyKey != "stable-key" || claimed.Swap(true) {
			return Grant{}, ErrNotReady
		}
		return testGrant(b), nil
	})
	for i := 0; i < 2; i++ {
		r := nativeRequest(c, requestBody(ChatText, false))
		r.Header.Set("Idempotency-Key", "stable-key")
		out := c.Forward(newCapture(), r)
		if (i == 0) != out.SubmissionAttempted {
			t.Fatal("original gate bypassed", i, out)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate reached upstream")
	}
}

func TestRelayDownstreamFailureStopsReadingAndUsesWriteDeadline(t *testing.T) {
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { emit(w, ChatText, chatEvents(false)) }, nil)
	w := newCapture()
	w.failWrite = 2
	o := c.Forward(w, nativeRequest(c, requestBody(ChatText, true)))
	if o.Delivered || o.TerminalValidated || o.Code != "downstream_interrupted" || w.writes != 2 || w.deadline.IsZero() || strings.Contains(w.body.String(), "[DONE]") {
		t.Fatal("write failure ignored", o)
	}
	c.limits.Write = 30 * time.Millisecond
	slow := &slowCapture{newCapture()}
	start := time.Now()
	o = c.Forward(slow, nativeRequest(c, requestBody(ChatText, true)))
	if o.Delivered || o.Code != "downstream_interrupted" || time.Since(start) > time.Second || strings.Contains(slow.body.String(), "[DONE]") {
		t.Fatal("slow write ignored deadline", o)
	}
}

func TestRelayActualHTTPIngressAndOwnedServerClosure(t *testing.T) {
	c := fixture(t, ChatText, func(w http.ResponseWriter, r *http.Request) { emit(w, ChatText, chatEvents(true)) }, nil)
	finished := make(chan Outcome, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { finished <- c.Forward(w, r) }))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	b := requestBody(ChatText, true)
	b = b[:len(b)-1] + `,"stream_options":{"include_usage":true}}`
	r, _ := http.NewRequest("POST", server.URL+c.binding.Profile.path(), strings.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer buyer-secret")
	resp, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if e != nil || resp.StatusCode != 200 || !strings.HasSuffix(string(body), "data: [DONE]\n\n") {
		t.Fatal("actual HTTP flow incomplete", e)
	}
	select {
	case o := <-finished:
		if !o.Delivered || o.Usage == nil {
			t.Fatal(o)
		}
	case <-time.After(time.Second):
		t.Fatal("ingress did not close")
	}
	// Native inbound body timeout, not just a pre-buffered request fixture.
	c.limits.Total = 150 * time.Millisecond
	conn, e := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	start := time.Now()
	_, e = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nAuthorization: Bearer buyer-secret\r\nContent-Length: 1000\r\nConnection: close\r\n\r\n{", c.binding.Profile.path(), server.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	_, _ = io.Copy(io.Discard, conn)
	select {
	case o := <-finished:
		if o.SubmissionAttempted || o.Delivered || time.Since(start) > time.Second {
			t.Fatal("slow inbound request escaped deadline", o)
		}
	case <-time.After(time.Second):
		t.Fatal("inbound body unbounded")
	}
}
