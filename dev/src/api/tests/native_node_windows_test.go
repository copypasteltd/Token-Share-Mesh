package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/client/node"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/shared/ids"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPNativeProviderOriginalRecoveryAndRevocation(t *testing.T) {
	var droppedEnrollment, droppedHeartbeat atomic.Bool
	h := newHarnessConfigured(t, 1000, 1000, func(c *controlhttp.Config) {
		c.Devices = device.Service{DB: db, PepperVersion: "node_test_v1", Peppers: map[string][]byte{"node_test_v1": []byte(strings.Repeat("p", 32))}}
	}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			drop := r.Method == "POST" && r.URL.Path == "/v1/p11/device-enrollments" && droppedEnrollment.CompareAndSwap(false, true)
			drop = drop || r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/heartbeats") && droppedHeartbeat.CompareAndSwap(false, true)
			if !drop {
				next.ServeHTTP(w, r)
				return
			}
			recorder := httptest.NewRecorder()
			next.ServeHTTP(recorder, r)
			if recorder.Code >= 200 && recorder.Code < 300 {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					connection.Close()
				}
				return
			}
			for name, values := range recorder.Header() {
				w.Header()[name] = values
			}
			w.WriteHeader(recorder.Code)
			_, _ = w.Write(recorder.Body.Bytes())
		})
	})
	session, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, session)
	headers := bearer(session.AccessToken)
	headers.Set("Idempotency-Key", ids.New("node_pair"))
	p := decode[device.Pairing](t, h.request(t, "POST", "/devices/pairing-intents", device.PairInput{Platform: "WINDOWS", Label: "我自愿共享的 RTX 4090", Confirmed: true}, headers), 201)
	path := filepath.Join(t.TempDir(), "native-node")
	n, err := node.Create(ctx, path, node.Config{Base: h.server.URL, Model: "RTX 4090 24GB", Kind: "GPU_DEVICE", OSBuild: "SYNTHETIC_WINDOWS_TEST", Release: "node_http_test_1"})
	if err != nil {
		t.Fatal(err)
	}
	fp := n.Status().Fingerprint
	if err = n.Enroll(ctx, p.Token); err == nil {
		t.Fatal("lost committed enrollment not marked unknown")
	}
	n.Close()
	n, err = node.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = n.Recover(ctx); err != nil || n.Status().DeviceID == "" || n.Status().Fingerprint != fp {
		n.Close()
		t.Fatal("real API enrollment not recovered", err)
	}
	id := n.Status().DeviceID
	if err = n.Step(ctx); err == nil {
		n.Close()
		t.Fatal("lost committed heartbeat not marked unknown")
	}
	n.Close()
	n, err = node.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err = n.Recover(ctx); err != nil || n.Status().Pending != "" {
		t.Fatal("real API heartbeat not recovered", err)
	}
	var count int
	var sequence, state, reported string
	var accepting, sharing bool
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='DeviceEnrolled'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("original registration duplicated", err, count)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT heartbeat_sequence::text,state,reported_resource_state,(resource_policy->>'accepting_new_jobs')::boolean,(resource_policy->>'sharing_enabled')::boolean FROM p11.devices WHERE id=$1`, id).Scan(&sequence, &state, &reported, &accepting, &sharing); err != nil || sequence != "0" || state != "PAIRED" || reported != "UNKNOWN" || accepting || sharing {
		t.Fatal("heartbeat self-approved capacity or duplicated sequence", err)
	}
	if err = n.Step(ctx); err != nil || *n.Status().Sequence != "1" {
		t.Fatal("next original sequence failed", err)
	}
	headers.Set("Idempotency-Key", ids.New("node_revoke"))
	decode[device.View](t, h.request(t, "POST", "/devices/"+id+"/revoke", device.Reason{Version: n.Status().Version, Code: "OWNER_REQUEST"}, headers), 200)
	if err = n.Run(ctx, nil); !errors.Is(err, node.ErrAuthority) {
		t.Fatal("revoked native agent kept sending", err)
	}
	if n.Status().ExecutionEnabled {
		t.Fatal("control agent enabled an engine")
	}
	var transport *devicecontrol.TransportError
	if errors.As(err, &transport) {
		t.Fatal("authority rejection misclassified as retryable")
	}
}
