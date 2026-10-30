package devicecontrol_test

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"p11.local/compute/client/protocol/devicecontrol"
	"strings"
	"testing"
	"time"
)

func TestNativeDeliveryReadCommandsUseExactOriginAndFreshProof(t *testing.T) {
	private := key()
	seen := map[string]bool{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proof, e := devicecontrol.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
		if e != nil || seen[proof.Nonce] {
			t.Error("invalid/repeated proof")
			w.WriteHeader(401)
			return
		}
		seen[proof.Nonce] = true
		binding := devicecontrol.Binding{Method: r.Method, Authority: r.Host, URI: r.RequestURI}
		message, e := devicecontrol.Message(proof, binding, nil)
		if e != nil || !ed25519.Verify(private.Public().(ed25519.PublicKey), message, proof.Signature) || r.Method != "GET" || r.ContentLength != 0 || r.Header.Get("Cookie") != "" || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("Content-Type") != "" {
			t.Error("unbounded read or proof binding", e)
		}
		if r.URL.Path != "/v1/p11/runner/leases/lease_original/manifest" && r.URL.Path != "/v1/p11/runner/leases/lease_original/input" {
			t.Error("untrusted URI")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"code":"UNAVAILABLE"}`))
	}))
	defer s.Close()
	c, e := devicecontrol.NewClient(s.URL, "device_native", private)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	m, e := c.PrepareManifest("lease_original")
	if e != nil {
		t.Fatal(e)
	}
	input, e := c.PrepareInput("lease_original")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []devicecontrol.Prepared{m, m, input, input} {
		r, e := c.Send(context.Background(), p)
		if e != nil || r.Status != 503 {
			t.Fatal(r.Status, e)
		}
	}
	if len(seen) != 4 {
		t.Fatal("missing fresh proofs")
	}
	for _, id := range []string{"", "../other", "lease_a?download=1", "https://elsewhere/input"} {
		if _, e := c.PrepareInput(id); e == nil {
			t.Fatal("untrusted input location")
		}
	}
}
