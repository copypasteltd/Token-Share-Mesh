package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"p11.local/compute/client/protocol/devicecontrol"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The portable tests inject only the master-key boundary. Files, AEAD, process
// locks and HTTP are real; separate Windows tests use actual current-user DPAPI.
func testLoader(context.Context, *Store, string, bool) ([]byte, error) {
	h := sha256.Sum256([]byte("SYNTHETIC_TEST_MASTER_NOT_A_RUNTIME_CREDENTIAL"))
	return bytes.Clone(h[:]), nil
}
func testConfig(base string) Config {
	return Config{Base: base, Model: "RTX 4090 24GB", Kind: "GPU_DEVICE", OSBuild: "SYNTHETIC_TEST", Release: "node_test_1"}
}
func testStore(t *testing.T, base string) *Store {
	t.Helper()
	s, e := createWith(context.Background(), filepath.Join(t.TempDir(), "node"), testConfig(base), testLoader)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func reopen(t *testing.T, s *Store) *Store {
	t.Helper()
	path := s.path
	s.Close()
	n, e := openWith(context.Background(), path, testLoader)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(n.Close)
	return n
}
func pairingToken() string {
	return "p11p.pair_synthetic." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
}
func view(s *Store) devicecontrol.Device {
	model := s.s.Config.Model
	return devicecontrol.Device{ID: "device_synthetic", SellerID: "seller_synthetic", Platform: s.s.Platform, Kind: s.s.Config.Kind, State: "PAIRED", Version: 1, Model: &model, IdentityProfile: devicecontrol.KeyProfile, KeyFingerprint: s.fingerprint(), ClientRelease: s.s.Config.Release, Capabilities: []string{}, ReportedState: "UNKNOWN", Policy: json.RawMessage(`{}`)}
}
func writeReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func drop(w http.ResponseWriter) {
	c, _, e := w.(http.Hijacker).Hijack()
	if e == nil {
		c.Close()
	}
}
func verify(t *testing.T, s *Store, r *http.Request, body []byte) string {
	t.Helper()
	proof, e := devicecontrol.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
	if e != nil {
		t.Error(e)
		return ""
	}
	b := devicecontrol.Binding{Method: r.Method, Authority: r.Host, URI: r.RequestURI, IdempotencyKey: r.Header.Get("Idempotency-Key"), IfMatch: r.Header.Get("If-Match"), ContentType: r.Header.Get("Content-Type")}
	message, e := devicecontrol.Message(proof, b, body)
	key := s.key()
	defer clear(key)
	if e != nil || !ed25519.Verify(key.Public().(ed25519.PublicKey), message, proof.Signature) {
		t.Error("invalid native request proof")
	}
	return proof.Nonce
}

func TestNodeEncryptedStateLockAndTamper(t *testing.T) {
	s := testStore(t, "http://127.0.0.1:1")
	fp := s.fingerprint()
	seed := bytes.Clone(s.s.Seed)
	if _, e := openWith(context.Background(), s.path, testLoader); !errors.Is(e, ErrLocked) {
		t.Fatal("second writer acquired the same state", e)
	}
	raw, e := os.ReadFile(filepath.Join(s.path, "state.enc"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, seed) || bytes.Contains(raw, []byte(s.s.Config.Model)) {
		t.Fatal("plaintext state escaped")
	}
	s = reopen(t, s)
	if s.fingerprint() != fp {
		t.Fatal("identity changed after reopen")
	}
	path := s.path
	s.Close()
	raw[len(raw)-1] ^= 1
	if e = os.WriteFile(filepath.Join(path, "state.enc"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = openWith(context.Background(), path, testLoader); !errors.Is(e, ErrState) {
		t.Fatal("tampered state accepted", e)
	}
	if _, e = Create(context.Background(), path, testConfig("http://127.0.0.1:1")); !errors.Is(e, ErrState) {
		t.Fatal("existing identity silently reinitialized", e)
	}
	if _, e = openWith(context.Background(), filepath.Join(path, "missing"), testLoader); !errors.Is(e, ErrState) {
		t.Fatal("missing state recreated")
	}
}

func TestNodeEnrollmentUnknownSurvivesRestart(t *testing.T) {
	var s *Store
	requests := 0
	var original []byte
	var originalKey string
	var registered devicecontrol.Device
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/p11/device-enrollments" || r.Header.Get("Authorization") != "Bearer "+pairingToken() {
			t.Error("wrong enrollment target/credential")
		}
		if requests == 1 {
			original = bytes.Clone(body)
			originalKey = r.Header.Get("Idempotency-Key")
			registered = view(s)
			drop(w)
			return
		}
		if !bytes.Equal(body, original) || originalKey != r.Header.Get("Idempotency-Key") {
			t.Error("enrollment changed during recovery")
		}
		writeReply(w, 201, registered)
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	fp := s.fingerprint()
	if e := s.Enroll(context.Background(), pairingToken()); !retryable(e) || requests != 1 {
		t.Fatal("unknown enrollment was retried or lost", e, requests)
	}
	if e := s.Refresh(context.Background()); !errors.Is(e, ErrPending) {
		t.Fatal("pending enrollment bypassed", e)
	}
	s = reopen(t, s)
	if e := s.Recover(context.Background()); e != nil || s.s.Pending != nil || s.s.DeviceID != registered.ID || s.fingerprint() != fp || requests != 2 {
		t.Fatal("original identity not recovered", e)
	}
	raw, _ := json.Marshal(s.s)
	status, _ := json.Marshal(s.Status())
	if bytes.Contains(raw, []byte(pairingToken())) || bytes.Contains(status, []byte(pairingToken())) || bytes.Contains(status, []byte("control_seed")) {
		t.Fatal("credential exposed after enrollment")
	}
}

func TestNodeHeartbeatUnknownSurvivesRestart(t *testing.T) {
	var s *Store
	var current devicecontrol.Device
	count := 0
	var firstBody []byte
	var key, nonce string
	accepted := map[string]devicecontrol.Ack{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := verify(t, s, r, body)
		if r.Method == "GET" {
			writeReply(w, 200, current)
			return
		}
		count++
		var h devicecontrol.Heartbeat
		_ = json.Unmarshal(body, &h)
		if h.State != "UNKNOWN" || h.Attempt != "" || h.Fence != "" {
			t.Error("unprobed agent claimed execution/capacity")
		}
		if count == 1 {
			firstBody = bytes.Clone(body)
			key = r.Header.Get("Idempotency-Key")
			nonce = n
			value := h.Sequence
			current.HeartbeatSequence = &value
			accepted[key] = devicecontrol.Ack{ID: current.ID, Version: 1, Status: "RECORDED", Recorded: time.Now().UTC()}
			drop(w)
			return
		}
		if count == 2 && (!bytes.Equal(body, firstBody) || r.Header.Get("Idempotency-Key") != key || n == nonce) {
			t.Error("pending heartbeat did not keep original bytes/key with fresh nonce")
		}
		ack, exists := accepted[r.Header.Get("Idempotency-Key")]
		if !exists {
			ack = devicecontrol.Ack{ID: current.ID, Version: 1, Status: "RECORDED", Recorded: time.Now().UTC()}
			accepted[r.Header.Get("Idempotency-Key")] = ack
			value := h.Sequence
			current.HeartbeatSequence = &value
		}
		writeReply(w, 200, ack)
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	current = view(s)
	if e := s.adopt(current); e != nil {
		t.Fatal(e)
	}
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	if e := s.Step(context.Background()); !retryable(e) || count != 1 || s.s.Pending == nil {
		t.Fatal("lost heartbeat not retained", e)
	}
	s = reopen(t, s)
	if e := s.Step(context.Background()); e != nil || len(accepted) != 1 || s.s.Pending != nil {
		t.Fatal("pending heartbeat not recovered", e)
	}
	if e := s.Step(context.Background()); e != nil || len(accepted) != 2 || *s.s.Sequence != "1" {
		t.Fatal("heartbeat high water reset", e)
	}
}

func TestNodeRejectedVersionRefreshAndCounterHighWater(t *testing.T) {
	var s *Store
	var current devicecontrol.Device
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		verify(t, s, r, body)
		if r.Method == "GET" {
			writeReply(w, 200, current)
			return
		}
		count++
		var h devicecontrol.Heartbeat
		_ = json.Unmarshal(body, &h)
		if count == 1 {
			current.Version = 2
			v := "7"
			current.HeartbeatSequence = &v
			writeReply(w, 409, map[string]any{"error": map[string]any{"code": "CONFLICT", "message": "refresh", "retryable": false, "request_id": "test", "details": nil}})
			return
		}
		if h.Sequence != "8" || h.Version != 2 {
			t.Error("rejected sequence reused or current policy not loaded")
		}
		writeReply(w, 200, devicecontrol.Ack{ID: current.ID, Version: 2, Status: "RECORDED", Recorded: time.Now().UTC()})
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	current = view(s)
	_ = s.adopt(current)
	_ = s.save()
	if e := s.Step(context.Background()); e != nil || s.s.Pending != nil || *s.s.Sequence != "7" || s.s.LastOutcome != "HEARTBEAT_REJECTED_REFRESHED" {
		t.Fatal("authoritative rejection not reconciled", e)
	}
	s = reopen(t, s)
	if e := s.Step(context.Background()); e != nil || count != 2 {
		t.Fatal("current version heartbeat failed", e)
	}
}

func TestNodeInvalidAcknowledgementAndAuthorityPreservePending(t *testing.T) {
	var s *Store
	var current devicecontrol.Device
	rejected := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		verify(t, s, r, body)
		if r.Method == "GET" {
			writeReply(w, 200, current)
			return
		}
		if rejected {
			writeReply(w, 401, map[string]any{"error": map[string]string{"code": "UNAUTHENTICATED"}})
			return
		}
		writeReply(w, 200, devicecontrol.Ack{ID: "another_device", Version: 1, Status: "RECORDED", Recorded: time.Now().UTC()})
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	current = view(s)
	_ = s.adopt(current)
	_ = s.save()
	if e := s.Step(context.Background()); !retryable(e) || s.s.Pending == nil {
		t.Fatal("invalid ack discarded pending command", e)
	}
	rejected = true
	before := calls
	if e := s.Run(context.Background(), nil); !errors.Is(e, ErrAuthority) || calls != before+1 || s.s.Pending == nil {
		t.Fatal("revoked authority retried or lost pending command", e)
	}
}

func TestNodePersistenceFailureAndExhaustionStayClosed(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeReply(w, 500, map[string]string{"error": "unused"})
	}))
	defer server.Close()
	s := testStore(t, server.URL)
	fp := s.fingerprint()
	path := s.path
	_ = s.root.Close()
	if e := s.Enroll(context.Background(), pairingToken()); !errors.Is(e, ErrState) || calls != 0 {
		t.Fatal("network ran before durable journal", e)
	}
	s.Close()
	s, e := openWith(context.Background(), path, testLoader)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.fingerprint() != fp || s.s.Pending != nil {
		t.Fatal("failed persistence regenerated or committed identity")
	}
	current := view(s)
	maximum := strconv.FormatUint(math.MaxUint64, 10)
	current.HeartbeatSequence = &maximum
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("exhausted counter emitted a write")
		}
		writeReply(w, 200, current)
	})
	_ = s.adopt(current)
	_ = s.save()
	projection := s.Status()
	*projection.Sequence = "0"
	if *s.s.Sequence != maximum {
		t.Fatal("status caller mutated durable high water")
	}
	if e = s.Step(context.Background()); !errors.Is(e, ErrCounter) || calls != 1 || s.Status().ExecutionEnabled {
		t.Fatal("counter wrapped or execution enabled", e)
	}
}

func TestNodeRunCancellationStopsOutboundControl(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	s := testStore(t, server.URL)
	_ = s.adopt(view(s))
	_ = s.save()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("outbound request did not start")
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop agent")
	}
}
