package devicecontrol_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
)

func TestNativeLeaseClaimFreezesOriginalCommandAndUsesFreshProofs(t *testing.T) {
	private := key()
	nonces := map[string]bool{}
	count := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		proof, e := devicecontrol.Parse(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
		if e != nil {
			t.Error(e)
			w.WriteHeader(401)
			return
		}
		if nonces[proof.Nonce] {
			t.Error("reused proof")
		}
		nonces[proof.Nonce] = true
		binding := devicecontrol.Binding{Method: r.Method, Authority: r.Host, URI: r.RequestURI, ContentType: r.Header.Get("Content-Type"), IdempotencyKey: r.Header.Get("Idempotency-Key")}
		message, e := devicecontrol.Message(proof, binding, body)
		if e != nil || !ed25519.Verify(private.Public().(ed25519.PublicKey), message, proof.Signature) {
			t.Error("invalid bound proof", e)
		}
		var in devicecontrol.LeaseClaim
		if json.Unmarshal(body, &in) != nil || in.Device != "device_native" || in.Capability != "cap_native" || in.ClaimKey != "claim_original" || in.Validate(binding.IdempotencyKey) != nil {
			t.Error("changed command")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/p11/runner/leases/claim" || r.Header.Get("Cookie") != "" {
			t.Error("wrong transport")
		}
		count++
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
	in := devicecontrol.LeaseClaim{Device: "device_native", Capability: "cap_native", ClaimKey: "claim_original", Version: 1}
	p, e := c.PrepareLeaseClaim(in)
	if e != nil {
		t.Fatal(e)
	}
	in.Capability = "cap_mutated"
	for i := 0; i < 2; i++ {
		r, e := c.Send(context.Background(), p)
		if e != nil || r.Status != 503 {
			t.Fatal(r, e)
		}
		if _, e = devicecontrol.UnverifiedClaim(r); e == nil {
			t.Fatal("error response accepted")
		}
	}
	if count != 2 || len(nonces) != 2 {
		t.Fatal(count, len(nonces))
	}
	in.Device = "another_device"
	if _, e = c.PrepareLeaseClaim(in); e == nil {
		t.Fatal("other device")
	}
}
