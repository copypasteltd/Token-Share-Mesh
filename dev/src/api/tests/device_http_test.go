package api_test

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"time"
)

func deviceHarness(t *testing.T) *harness {
	return newHarnessConfigured(t, 1000, 1000, func(c *controlhttp.Config) {
		c.Devices = device.Service{DB: db, PepperVersion: "pair_http_v1", Peppers: map[string][]byte{"pair_http_v1": []byte(strings.Repeat("p", 32))}}
	}, nil)
}
func grantDeviceSeller(t *testing.T, s identity.SessionView) {
	t.Helper()
	testsupport.SeedOfferForSeller(t, ctx, db, "GPU_RENTAL", ids.New("reg"), identity.Principal{UserID: s.Profile.UserID, Kind: identity.UserSession, Version: s.Profile.Version})
}
func httpEnrollment(t *testing.T, id string) (device.Enrollment, ed25519.PrivateKey) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return device.Enrollment{PairingID: id, PublicKey: base64.RawURLEncoding.EncodeToString(pub), KeyProfile: devicecontrol.KeyProfile, OSBuild: "Windows SYNTHETIC_TEST", ClientRelease: "SYNTHETIC_TEST", Fingerprint: strings.Repeat("c", 64), Kind: "GPU_DEVICE", Model: "RTX 4090 24GB"}, key
}
func httpEnrolled(t *testing.T, h *harness, headers http.Header, cookie *http.Cookie) (device.View, ed25519.PrivateKey) {
	t.Helper()
	headers = headers.Clone()
	headers.Set("Idempotency-Key", ids.New("pair_intent"))
	var cookies []*http.Cookie
	if cookie != nil {
		cookies = append(cookies, cookie)
	}
	p := decode[device.Pairing](t, h.request(t, "POST", "/devices/pairing-intents", device.PairInput{Platform: "WINDOWS", Label: "我的闲置 RTX 4090", Confirmed: true}, headers, cookies...), 201)
	in, key := httpEnrollment(t, p.ID)
	eh := bearer(p.Token)
	eh.Set("Idempotency-Key", ids.New("enrollment_intent"))
	v := decode[device.View](t, h.request(t, "POST", "/device-enrollments", in, eh), 201)
	return v, key
}
func proofHeaders(t *testing.T, h *harness, v device.View, key ed25519.PrivateKey, method, path string, body any, intent, etag string) http.Header {
	t.Helper()
	var raw []byte
	if body != nil {
		switch b := body.(type) {
		case string:
			raw = []byte(b)
		default:
			var e error
			raw, e = json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	u, _ := url.Parse(h.server.URL)
	b := devicecontrol.Binding{Method: method, Authority: u.Host, URI: "/v1/p11" + path, IdempotencyKey: intent, IfMatch: etag}
	if body != nil {
		b.ContentType = "application/json"
	}
	token, e := devicecontrol.Sign(key, v.ID, b, raw, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r := bearer(token)
	if intent != "" {
		r.Set("Idempotency-Key", intent)
	}
	if etag != "" {
		r.Set("If-Match", etag)
	}
	if b.ContentType != "" {
		r.Set("Content-Type", b.ContentType)
	}
	return r
}

func TestHTTPDevicePairingCSRFEnrollmentAndNativeControl(t *testing.T) {
	h := deviceHarness(t)
	s, cookie := h.login(t, "WEB")
	grantDeviceSeller(t, s)
	headers := http.Header{"Origin": {h.origin}, "Idempotency-Key": {"browser_pairing_intent"}}
	in := device.PairInput{Platform: "WINDOWS", Label: "我的闲置 RTX 4090", Confirmed: true}
	expectError(t, h.request(t, "POST", "/devices/pairing-intents", in, headers, cookie), 403, "FORBIDDEN")
	headers.Set("X-CSRF-Token", s.CSRF)
	p := decode[device.Pairing](t, h.request(t, "POST", "/devices/pairing-intents", in, headers, cookie), 201)
	replay := h.request(t, "POST", "/devices/pairing-intents", in, headers, cookie)
	again := decode[device.Pairing](t, replay, 201)
	if p.Token == "" || again.Token != "" || again.ID != p.ID || replay.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("pairing secret redisclosed")
	}
	en, key := httpEnrollment(t, p.ID)
	eh := bearer(p.Token)
	eh.Set("Idempotency-Key", "original_http_enrollment")
	expectError(t, h.request(t, "POST", "/device-enrollments", en, eh, cookie), 401, "UNAUTHENTICATED")
	eh.Set("Origin", h.origin)
	expectError(t, h.request(t, "POST", "/device-enrollments", en, eh), 401, "UNAUTHENTICATED")
	eh.Del("Origin")
	r := h.request(t, "POST", "/device-enrollments", en, eh)
	v := decode[device.View](t, r, 201)
	if v.State != "PAIRED" || v.Model == nil || *v.Model != "RTX 4090 24GB" || r.header.Get("ETag") != `"1"` {
		t.Fatal("unsafe device registration", v)
	}
	r = h.request(t, "POST", "/device-enrollments", en, eh)
	againV := decode[device.View](t, r, 201)
	if againV.ID != v.ID || r.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("enrollment recovery duplicated device")
	}
	c, e := devicecontrol.NewClient(h.server.URL, v.ID, key)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	prepared, e := c.Prepare("GET", "/v1/p11/devices/"+v.ID, nil, "", "")
	if e != nil {
		t.Fatal(e)
	}
	reply, e := c.Send(ctx, prepared)
	if e != nil || reply.Status != 200 {
		t.Fatal("native signed read", e, reply.Status)
	}
	hb := device.Heartbeat{Sequence: "1", Version: 1, Recorded: time.Now().UTC(), State: "IDLE"}
	body, _ := json.Marshal(hb)
	prepared, e = c.Prepare("POST", "/v1/p11/devices/"+v.ID+"/heartbeats", body, "original_http_heartbeat", "")
	if e != nil {
		t.Fatal(e)
	}
	for i := range 2 {
		reply, e = c.Send(ctx, prepared)
		if e != nil || reply.Status != 200 || reply.Replayed != (i == 1) {
			t.Fatal("native heartbeat recovery", e, reply)
		}
	}
	expectError(t, h.request(t, "POST", "/devices/"+v.ID+"/heartbeats", hb, headers, cookie), 403, "FORBIDDEN")
}
func TestHTTPDeviceProofTamperingRevocationAndTenantPaging(t *testing.T) {
	h := deviceHarness(t)
	s, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, s)
	owner := bearer(s.AccessToken)
	v, key := httpEnrolled(t, h, owner, nil)
	other, _ := httpEnrolled(t, h, owner, nil)
	path := "/devices/" + v.ID
	proof := proofHeaders(t, h, v, key, "GET", path, nil, "", "")
	decode[device.View](t, h.request(t, "GET", path, nil, proof), 200)
	expectError(t, h.request(t, "GET", path, nil, proof), 401, "UNAUTHENTICATED")
	fresh := proofHeaders(t, h, v, key, "GET", path, nil, "", "")
	// Origin-form URI is mandatory even when an absolute-form proxy request
	// names the allowed Host. Reject it without consuming the original nonce.
	u, _ := url.Parse(h.server.URL)
	conn, e := net.DialTimeout("tcp", u.Host, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, e = fmt.Fprintf(conn, "GET %s/v1/p11%s HTTP/1.1\r\nHost: %s\r\nAuthorization: %s\r\nConnection: close\r\n\r\n", h.server.URL, path, u.Host, fresh.Get("Authorization"))
	if e != nil {
		t.Fatal(e)
	}
	wire, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(io.LimitReader(wire.Body, 65537))
	wire.Body.Close()
	if e != nil || wire.StatusCode != 400 || len(raw) > 65536 {
		t.Fatal("absolute-form request was not rejected", e, wire.StatusCode)
	}
	expectError(t, h.request(t, "GET", path+"?x=1", nil, fresh), 401, "UNAUTHENTICATED")
	decode[device.View](t, h.request(t, "GET", path, nil, fresh), 200)
	expectError(t, h.request(t, "GET", "/devices/"+other.ID, nil, proofHeaders(t, h, v, key, "GET", "/devices/"+other.ID, nil, "", "")), 404, "NOT_FOUND")
	page := decode[device.Page](t, h.request(t, "GET", "/seller/devices?limit=1", nil, owner), 200)
	if page.NextCursor == nil {
		t.Fatal("missing signed device cursor")
	}
	decode[device.Page](t, h.request(t, "GET", "/seller/devices?limit=1&cursor="+*page.NextCursor, nil, owner), 200)
	second, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, second)
	expectError(t, h.request(t, "GET", path, nil, bearer(second.AccessToken)), 404, "NOT_FOUND")
	expectError(t, h.request(t, "GET", "/seller/devices?limit=1&cursor="+*page.NextCursor, nil, bearer(second.AccessToken)), 400, "INVALID_REQUEST")
	owner.Set("Idempotency-Key", "http_owner_drains")
	drained := decode[device.View](t, h.request(t, "POST", path+"/drain", device.Reason{Code: "owner_stops", Version: 1}, owner), 200)
	if drained.State != "DRAINING" || drained.Version != 2 {
		t.Fatal(drained)
	}
	owner.Set("Idempotency-Key", "http_owner_revokes")
	decode[device.View](t, h.request(t, "POST", path+"/revoke", device.Reason{Code: "owner_revokes", Version: 2}, owner), 200)
	expectError(t, h.request(t, "GET", path, nil, proofHeaders(t, h, v, key, "GET", path, nil, "", "")), 401, "UNAUTHENTICATED")
}
func TestHTTPDevicePolicyETagAndStrictInputBoundaries(t *testing.T) {
	h := deviceHarness(t)
	s, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, s)
	owner := bearer(s.AccessToken)
	v, key := httpEnrolled(t, h, owner, nil)
	path := "/devices/" + v.ID + "/resource-policy"
	policy := `{"expected_version":1,"windows":[{"starts_at":"` + time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano) + `","ends_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano) + `"}],"sharing_enabled":true,"active_slots":1,"minimum_free_disk_bytes":0,"stop_policy":"DRAIN"}`
	owner.Set("Idempotency-Key", "http_resource_policy")
	expectError(t, h.request(t, "PATCH", path, policy, owner), 400, "INVALID_REQUEST")
	owner.Set("If-Match", `W/"1"`)
	expectError(t, h.request(t, "PATCH", path, policy, owner), 400, "INVALID_REQUEST")
	owner.Set("If-Match", `"1"`)
	a := decode[device.Ack](t, h.request(t, "PATCH", path, policy, owner), 200)
	if a.Status != "PROBING" || a.Version != 2 {
		t.Fatal("policy granted execution", a)
	}
	decode[device.Ack](t, h.request(t, "PATCH", path, policy, owner), 200)
	owner.Set("Idempotency-Key", "new_stale_resource_policy")
	expectError(t, h.request(t, "PATCH", path, policy, owner), 409, "CONFLICT")
	hb := device.Heartbeat{Sequence: "1", Version: 2, Recorded: time.Now().UTC(), State: "IDLE"}
	hbPath := "/devices/" + v.ID + "/heartbeats"
	signed := proofHeaders(t, h, v, key, "POST", hbPath, hb, "bound_heartbeat_key", "")
	changed := signed.Clone()
	changed.Set("Idempotency-Key", "tampered_heartbeat_key")
	expectError(t, h.request(t, "POST", hbPath, hb, changed), 401, "UNAUTHENTICATED")
	changed = signed.Clone()
	changed.Add("Content-Type", "application/json")
	expectError(t, h.request(t, "POST", hbPath, hb, changed), 400, "INVALID_REQUEST")
	changed = signed.Clone()
	changed.Set("Origin", h.origin)
	expectError(t, h.request(t, "POST", hbPath, hb, changed), 401, "UNAUTHENTICATED")
	decode[device.Ack](t, h.request(t, "POST", hbPath, hb, signed), 200)
	duplicate := `{"sequence":"2","sequence":"3","device_version":2,"recorded_at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","resource_state":"IDLE"}`
	expectError(t, h.request(t, "POST", hbPath, duplicate, proofHeaders(t, h, v, key, "POST", hbPath, duplicate, "duplicate_json_heartbeat", "")), 400, "INVALID_REQUEST")
	oversize := strings.Repeat(" ", 65537)
	expectError(t, h.request(t, "POST", hbPath, oversize, signed), 400, "INVALID_REQUEST")
}
