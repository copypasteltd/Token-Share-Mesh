package api_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/ids"
)

const claimHTTPBody = "{\n  \"model\":\"model_nb1\",\"messages\":[{\"role\":\"user\",\"content\":\"R018_SYNTHETIC_ONLY\"}],\"max_completion_tokens\":16,\"stream\":false\n}\n"

func claimHTTPFixture(t *testing.T) (*harness, testsupport.Offer, invocation.Issuance, identity.SessionView) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	claims, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{"issuer_http": pub})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(claims.Close)
	fp, content := make([]byte, 32), make([]byte, 32)
	if _, e = rand.Read(fp); e != nil {
		t.Fatal(e)
	}
	if _, e = rand.Read(content); e != nil {
		t.Fatal(e)
	}
	inv, e := invocation.New(invocation.Config{DB: db, PaymentEnvironment: "SANDBOX", FingerprintVersion: "fp_http", FingerprintKeys: map[string][]byte{"fp_http": fp}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(inv.Close)
	issuer, e := invocation.NewExecution(invocation.ExecutionConfig{Invocations: inv, InputVersion: "input_http", InputKeys: map[string][]byte{"input_http": content}, SigningKeyID: "issuer_http", SigningPrivateKey: key})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(issuer.Close)

	delivery, e := invocation.NewDelivery(invocation.DeliveryConfig{Claims: claims, Invocations: inv, InputKeys: map[string][]byte{"input_http": content}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(delivery.Close)
	var keys identity.KeyService
	h := newHarnessConfigured(t, 1000, 1000, func(c *controlhttp.Config) {
		c.Claims = claims
		c.Delivery = delivery
		c.Routes = routecontract.Service{DB: db, PaymentEnvironment: "SANDBOX"}
		keys = c.Keys
	}, nil)
	s, _ := h.login(t, "MINIAPP")
	f := testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(relay.ChatText))
	w := httpRouteOrderOffer(t, h, s, nil, f)
	headers := bearer(s.AccessToken)
	headers.Set("Idempotency-Key", ids.New("route"))
	route := decode[routecontract.Contract](t, h.request(t, "POST", "/route-contracts", w, headers), 201)
	headers.Set("Idempotency-Key", ids.New("sdk_key"))
	k := decode[identity.CreatedKey](t, h.request(t, "POST", "/api-keys", identity.KeyInput{Label: "Original claim fixture", Scopes: []string{"ROUTE_INVOKE"}, RouteIDs: []string{route.ID}, ExpiresAt: time.Now().Add(time.Minute)}, headers), 201)
	p, e := keys.Authenticate(ctx, k.SecretOnce)
	if e != nil {
		t.Fatal(e)
	}
	body := []byte(claimHTTPBody)
	meta, e := relay.Preflight(relay.ChatText, "model_nb1", body)
	if e != nil {
		t.Fatal(e)
	}
	r := invocation.Request{Model: "model_nb1", Protocol: relay.ChatText, RequestDigest: meta.Digest, IdempotencyKey: ids.New("call"), MaxOutputTokens: 16}
	if _, e = issuer.Ready(ctx, p, r, body); e != nil {
		t.Fatal(e)
	}
	issued, e := issuer.Issue(ctx, p, r)
	if e != nil || issued.Lease == nil {
		t.Fatal(issued, e)
	}
	return h, f, issued, s
}

func TestHTTPExecutionClaimNativeTransportReturnsOneOriginalSignedLease(t *testing.T) {
	h, f, issued, _ := claimHTTPFixture(t)
	c, e := devicecontrol.NewClient(h.server.URL, f.Device, f.DeviceKey)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in := devicecontrol.LeaseClaim{Device: f.Device, Capability: f.Capability, ClaimKey: ids.New("claim"), Version: 1}
	cmd, e := c.PrepareLeaseClaim(in)
	if e != nil {
		t.Fatal(e)
	}
	var previous []byte
	for i := 0; i < 2; i++ {
		r, e := c.Send(ctx, cmd)
		if e != nil || r.Status != 200 || r.Replayed != (i == 1) {
			t.Fatal(r, e)
		}
		raw, e := devicecontrol.UnverifiedClaim(r)
		if e != nil {
			t.Fatal(e)
		}
		var l executionlease.Lease
		if e = json.Unmarshal(raw, &l); e != nil || l.ID != issued.LeaseID || l.Signature != issued.Lease.Signature || l.Expires != issued.Lease.Expires {
			t.Fatal(l, e)
		}
		if i == 1 && !bytes.Equal(previous, raw) {
			t.Fatal("replay changed bytes")
		}
		previous = raw
		if bytes.Contains(raw, []byte("R018_SYNTHETIC_ONLY")) || bytes.Contains(raw, []byte("input_ciphertext")) {
			t.Fatal("content returned")
		}
	}
	var count int
	if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='ExecutionLeaseClaimed'`, issued.LeaseID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}

func TestHTTPExecutionClaimRejectsNonDeviceBadProofAndChangedOriginalBody(t *testing.T) {
	h, f, _, session := claimHTTPFixture(t)
	in := devicecontrol.LeaseClaim{Device: f.Device, Capability: f.Capability, ClaimKey: ids.New("claim"), Version: 1}
	path := "/runner/leases/claim"
	hdr := bearer(session.AccessToken)
	hdr.Set("Idempotency-Key", in.ClaimKey)
	expectError(t, h.request(t, "POST", path, in, hdr), 403, "FORBIDDEN")
	v := device.View{ID: f.Device}
	headers := proofHeaders(t, h, v, f.DeviceKey, "POST", path, in, in.ClaimKey, "")
	decode[executionlease.Lease](t, h.request(t, "POST", path, in, headers), 200)
	expectError(t, h.request(t, "POST", path, in, headers), 401, "UNAUTHENTICATED")
	in.Capability = ids.New("wrong")
	headers = proofHeaders(t, h, v, f.DeviceKey, "POST", path, in, in.ClaimKey, "")
	expectError(t, h.request(t, "POST", path, in, headers), 409, "CONFLICT")
}

func TestHTTPExecutionClaimUnconfiguredServiceFailsClosed(t *testing.T) {
	h := deviceHarness(t)
	s, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, s)
	v, key := httpEnrolled(t, h, bearer(s.AccessToken), nil)
	in := devicecontrol.LeaseClaim{Device: v.ID, Capability: ids.New("cap"), ClaimKey: ids.New("claim"), Version: v.Version}
	headers := proofHeaders(t, h, v, key, "POST", "/runner/leases/claim", in, in.ClaimKey, "")
	expectError(t, h.request(t, "POST", "/runner/leases/claim", in, headers), 503, "UNAVAILABLE")
}
