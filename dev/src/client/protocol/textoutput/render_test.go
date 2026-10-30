package textoutput

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

type source struct {
	d      supervisor.TextDescriptor
	pieces [][]byte
	at     int
	v      supervisor.TextResult
	err    error
	closed bool
	stop   string
}

func newSource(p relay.Profile, stream bool) *source {
	return &source{d: supervisor.TextDescriptor{ResultID: "result_synthetic_only", CreatedUnix: 1, IncludeUsage: true, Manifest: executionlease.Manifest{InputTokenLimit: 16, Spec: executionlease.Spec{Protocol: p, Model: "engine_local", Stream: stream, MaxOutputTokens: 16}}}, pieces: [][]byte{{0xe4}, {0xbd, 0xa0, 0xe5}, {0xa5, 0xbd}, []byte("🚀")}, v: supervisor.TextResult{InputTokens: 3, OutputTokens: 2, Reason: "eog", StopIndex: supervisor.NoStopIndex}}
}
func (s *source) Descriptor() supervisor.TextDescriptor { return s.d }
func (s *source) Started(context.Context) (supervisor.TextStart, error) {
	return supervisor.TextStart{InputTokens: 3}, nil
}
func (s *source) Read(context.Context) ([]byte, error) {
	if s.at == len(s.pieces) {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	b := append([]byte(nil), s.pieces[s.at]...)
	s.at++
	return b, nil
}
func (s *source) Result(context.Context) (supervisor.TextResult, error) { return s.v, s.err }
func (s *source) StopSequence(context.Context, uint32) ([]byte, error)  { return []byte(s.stop), nil }
func (s *source) Close(context.Context) error                           { s.closed = true; return nil }

type buffer struct {
	bytes.Buffer
	failAt, writes int
}

func (b *buffer) Write(ctx context.Context, p []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.writes++
	if b.failAt > 0 && b.writes >= b.failAt {
		return io.ErrClosedPipe
	}
	_, e := b.Buffer.Write(p)
	return e
}

type authorize struct{ binding relay.Binding }

func (a authorize) Admit(_ context.Context, c relay.Credential, i relay.Intent) (relay.Grant, error) {
	if c.Value != "synthetic-buyer" || i.Binding != a.binding {
		return relay.Grant{}, relay.ErrUnauthorized
	}
	b := a.binding
	return relay.Grant{InvocationID: "synthetic_invocation", SellerID: b.SellerID, ContractHash: b.ContractHash, RouteID: b.RouteID, AuthorityKind: "JOB_LEASE", ResourceID: "synthetic_gpu", Fence: 1, ResourceGeneration: 1, Deadline: time.Now().Add(time.Minute), Revoked: make(chan struct{})}, nil
}

type secrets struct{}

func (secrets) Lookup(context.Context, string, string) (string, error) {
	return "synthetic-upstream", nil
}

func TestNativeTextOutputThreeProfilesRoundTripStrictRelay(t *testing.T) {
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", p, stream), func(t *testing.T) {
				for _, reason := range []string{"eog", "limit", "stop"} {
					t.Run(reason, func(t *testing.T) {
						up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							s := newSource(p, stream)
							s.v.Reason = reason
							if reason == "stop" {
								s.v.StopIndex = 0
								s.stop = "<END>"
							}
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
							} else {
								w.Header().Set("Content-Type", "application/json")
							}
							sink, e := NewHTTPSink(w, time.Second)
							if e != nil {
								t.Error(e)
								return
							}
							receipt, e := Render(r.Context(), sink, s, DefaultLimits())
							if e != nil || receipt.Terminal == "" || !s.closed {
								t.Errorf("renderer failed: %v", e)
							}
						}))
						defer up.Close()
						path := "/v1/chat/completions"
						body := fmt.Sprintf(`{"model":"public_local","messages":[{"role":"user","content":"synthetic input"}],"max_tokens":16,"stream":%t,"stream_options":{"include_usage":true}}`, stream)
						if !stream {
							body = strings.Replace(body, `,"stream_options":{"include_usage":true}`, "", 1)
						}
						if p == relay.ResponsesText {
							path = "/v1/responses"
							body = fmt.Sprintf(`{"model":"public_local","input":"synthetic input","max_output_tokens":16,"store":false,"stream":%t}`, stream)
						}
						if p == relay.MessagesText {
							path = "/v1/messages"
							body = fmt.Sprintf(`{"model":"public_local","messages":[{"role":"user","content":"synthetic input"}],"max_tokens":16,"stream":%t}`, stream)
						}
						b := relay.Binding{RouteID: "route_synthetic", ContractHash: strings.Repeat("a", 64), SellerID: "seller_synthetic", ListingRevision: "revision_synthetic", Mode: "STANDARD", ProfileRevision: "1", PublicModel: "public_local", BackendModel: "engine_local", Profile: p, Endpoint: up.URL + path, SecretRef: "secret_synthetic", AllowLoopbackHTTP: true}
						core, e := relay.New(relay.Config{Domain: relay.PlatformPlain, Binding: b, Authorizer: authorize{b}, Secrets: secrets{}})
						if e != nil {
							t.Fatal(e)
						}
						defer core.Close()
						req := httptest.NewRequest("POST", path, strings.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						if p == relay.MessagesText {
							req.Header.Set("x-api-key", "synthetic-buyer")
							req.Header.Set("anthropic-version", relay.AnthropicVersion)
						} else {
							req.Header.Set("Authorization", "Bearer synthetic-buyer")
						}
						ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							out := core.Forward(w, r)
							if out.Code != "" && out.Code != "OK" {
								t.Errorf("strict relay rejected source output: %+v", out)
							}
						}))
						defer ingress.Close()
						req.RequestURI = ""
						req.URL.Scheme = "http"
						req.URL.Host = strings.TrimPrefix(ingress.URL, "http://")
						client := &http.Client{Timeout: 5 * time.Second}
						resp, e := client.Do(req)
						if e != nil {
							t.Fatal(e)
						}
						defer resp.Body.Close()
						raw, e := io.ReadAll(resp.Body)
						if e != nil || resp.StatusCode != 200 || !bytes.Contains(raw, []byte("public_local")) {
							t.Fatalf("actual HTTP/strict relay output: status=%d err=%v body=%s", resp.StatusCode, e, raw)
						}
						if stream && p == relay.ChatText && !bytes.Contains(raw, []byte("[DONE]")) {
							t.Fatal("missing chat END")
						}
						if !stream && !bytes.Contains(raw, []byte("你好🚀")) {
							t.Fatal("split UTF-8 lost")
						}
					})
				}
			})
		}
	}
}

func TestNativeTextOutputFailureNeverWritesSuccessTail(t *testing.T) {
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		for _, failure := range []string{"native", "invalid_utf8", "truncated_utf8", "count_mismatch", "invalid_stop"} {
			t.Run(string(p)+"/"+failure, func(t *testing.T) {
				s := newSource(p, true)
				switch failure {
				case "native":
					s.err = io.ErrUnexpectedEOF
				case "invalid_utf8":
					s.pieces = [][]byte{{0xff}}
				case "truncated_utf8":
					s.pieces = [][]byte{{0xe4}}
				case "count_mismatch":
					s.v.InputTokens++
				case "invalid_stop":
					s.v.Reason = "stop"
					s.v.StopIndex = 0
				}
				b := &buffer{}
				receipt, e := Render(context.Background(), b, s, DefaultLimits())
				if e == nil || receipt.Terminal != "" || !s.closed {
					t.Fatal("failed output reported completed")
				}
				for _, end := range []string{"[DONE]", "message_stop", "response.completed"} {
					if strings.Contains(b.String(), end) {
						t.Fatal("false success tail", end)
					}
				}
			})
		}
	}
}

func TestNativeTextOutputResponsesTerminalOverflowHasNoPartialEnd(t *testing.T) {
	s := newSource(relay.ResponsesText, true)
	s.pieces = [][]byte{bytes.Repeat([]byte("x"), 3000)}
	limits := DefaultLimits()
	limits.EventBytes = 2048
	b := &buffer{}
	receipt, e := Render(context.Background(), b, s, limits)
	if e != ErrLimit || receipt.Terminal != "" || !strings.Contains(b.String(), "response.output_text.delta") {
		t.Fatal("boundary was not exercised", e)
	}
	if strings.Contains(b.String(), "response.output_text.done") || strings.Contains(b.String(), "response.completed") {
		t.Fatal("oversize final emitted partial terminal sequence")
	}
}

func TestNativeTextOutputSinkFailureAndCancelledContextCloseReader(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		s := newSource(relay.ChatText, true)
		b := &buffer{failAt: 2}
		if cancelled {
			cancel()
		}
		receipt, e := Render(ctx, b, s, DefaultLimits())
		cancel()
		if e == nil || receipt.Terminal != "" || !s.closed || strings.Contains(b.String(), "[DONE]") {
			t.Fatal("failed/cancelled sink manufactured completion")
		}
	}
}

func TestNativeTextOutputChatUsageOnlyWhenRequested(t *testing.T) {
	for _, include := range []bool{false, true} {
		s := newSource(relay.ChatText, true)
		s.d.IncludeUsage = include
		b := &buffer{}
		if _, e := Render(context.Background(), b, s, DefaultLimits()); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(b.String(), `"usage"`) != include || strings.Contains(b.String(), `"choices":[]`) != include {
			t.Fatal("include_usage contract changed")
		}
	}
}

func TestNativeTextOutputHTTPSinkRequiresDeadlineSupport(t *testing.T) {
	sink, e := NewHTTPSink(httptest.NewRecorder(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if e = sink.Write(context.Background(), []byte("private fixture")); e == nil {
		t.Fatal("unsupported writer silently accepted")
	}
	if _, e = NewHTTPSink(httptest.NewRecorder(), 0); e == nil {
		t.Fatal("unbounded write accepted")
	}
}
