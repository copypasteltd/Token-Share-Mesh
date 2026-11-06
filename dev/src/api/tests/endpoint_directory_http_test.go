package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/endpointregistry"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"time"
)

func endpointDirectoryHarness(t *testing.T) *harness {
	return newHarnessConfigured(t, 2000, 2000, func(c *controlhttp.Config) {
		c.EndpointDirectory = &endpointregistry.Service{DB: db}
		c.Devices = device.Service{DB: db, PepperVersion: "directory_http_v1", Peppers: map[string][]byte{"directory_http_v1": []byte(strings.Repeat("p", 32))}}
	}, nil)
}
func endpointBrowserHeaders(h *harness, view identity.SessionView) http.Header {
	return http.Header{"Origin": []string{h.origin}, "X-CSRF-Token": []string{view.CSRF}, "Idempotency-Key": []string{ids.New("endpoint")}}
}
func TestHTTPEndpointDirectoryCurrentRevocationAndReferenceReplay(t *testing.T) {
	h := endpointDirectoryHarness(t)
	view, cookie := h.login(t, "WEB")
	headers := endpointBrowserHeaders(h, view)
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, view.Profile.UserID, ids.New("endpoint"), "BUYER", 1, now, now.Add(time.Hour))
	in := endpointdirectory.Registration{Bundle: f.Bundle}
	registered := decode[endpointdirectory.Record](t, h.request(t, "POST", "/endpoint-bundles", in, headers, cookie), 201)
	if registered.Validate() != nil || registered.State != "ACTIVE" || !registered.IndependentTrustRequired {
		t.Fatal("account binding claimed endpoint trust")
	}
	got := decode[endpointdirectory.Record](t, h.request(t, "GET", "/endpoint-bundles/"+registered.Recipient, nil, headers, cookie), 200)
	if got.BundleHash != registered.BundleHash {
		t.Fatal("original public metadata changed")
	}
	revokeHeaders := headers.Clone()
	revokeHeaders.Set("Idempotency-Key", ids.New("revoke"))
	revoked := decode[endpointdirectory.Record](t, h.request(t, "POST", "/endpoint-bundles/"+registered.Recipient+"/revoke", endpointdirectory.Revocation{BundleHash: registered.BundleHash}, revokeHeaders, cookie), 200)
	if revoked.State != "REVOKED" {
		t.Fatal("directory revocation not current")
	}
	replay := h.request(t, "POST", "/endpoint-bundles", in, headers, cookie)
	again := decode[endpointdirectory.Record](t, replay, 201)
	if again.State != "REVOKED" || !again.Replayed || replay.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("historical registration response resurrected endpoint")
	}
}
func TestHTTPEndpointDirectoryCSRFAndStrictPublicBody(t *testing.T) {
	h := endpointDirectoryHarness(t)
	view, cookie := h.login(t, "WEB")
	headers := endpointBrowserHeaders(h, view)
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, view.Profile.UserID, ids.New("endpoint"), "BUYER", 1, now, now.Add(time.Hour))
	in := endpointdirectory.Registration{Bundle: f.Bundle}
	missing := headers.Clone()
	missing.Del("X-CSRF-Token")
	if r := h.request(t, "POST", "/endpoint-bundles", in, missing, cookie); r.status != 403 {
		t.Fatal("public key write bypassed CSRF", r.status)
	}
	raw, _ := json.Marshal(in)
	bad := string(raw[:len(raw)-1]) + `,"private_key":"MUST_NOT_PERSIST"}`
	if r := h.request(t, "POST", "/endpoint-bundles", bad, headers, cookie); r.status != 400 || strings.Contains(string(r.body), "MUST_NOT_PERSIST") {
		t.Fatal("unexpected private field accepted or echoed", r.status)
	}
	if r := h.request(t, "GET", "/endpoint-bundles?limit=100", nil, headers, cookie); r.status != 400 {
		t.Fatal("directory reply page bound overridden", r.status)
	}
}
func TestHTTPEndpointDirectoryOwnerPaginationAndCursorCannotCrossAccounts(t *testing.T) {
	h := endpointDirectoryHarness(t)
	view, cookie := h.login(t, "WEB")
	headers := endpointBrowserHeaders(h, view)
	now := time.Now().UTC()
	first := ""
	for range endpointdirectory.PageSize + 1 {
		f := testsupport.PublicEndpoint(t, view.Profile.UserID, ids.New("endpoint"), "BUYER", 1, now, now.Add(time.Hour))
		headers.Set("Idempotency-Key", ids.New("register"))
		v := decode[endpointdirectory.Record](t, h.request(t, "POST", "/endpoint-bundles", endpointdirectory.Registration{Bundle: f.Bundle}, headers, cookie), 201)
		first = v.Recipient
	}
	page := decode[endpointdirectory.Page](t, h.request(t, "GET", "/endpoint-bundles", nil, headers, cookie), 200)
	if len(page.Items) != endpointdirectory.PageSize || page.Next == nil {
		t.Fatal("unbounded public directory page")
	}
	other, otherCookie := h.login(t, "WEB")
	otherHeaders := endpointBrowserHeaders(h, other)
	if r := h.request(t, "GET", "/endpoint-bundles/"+first, nil, otherHeaders, otherCookie); r.status != 404 {
		t.Fatal("foreign account public directory enumerated", r.status)
	}
	if r := h.request(t, "GET", "/endpoint-bundles?cursor="+url.QueryEscape(*page.Next), nil, otherHeaders, otherCookie); r.status != 400 {
		t.Fatal("foreign account cursor reused", r.status)
	}
}
func TestHTTPEndpointDirectoryProviderRequiresSignedExactDevice(t *testing.T) {
	h := endpointDirectoryHarness(t)
	view, cookie := h.login(t, "WEB")
	grantDeviceSeller(t, view)
	headers := endpointBrowserHeaders(h, view)
	d, key := httpEnrolled(t, h, headers, cookie)
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, view.Profile.UserID, d.ID, "SELLER", 1, now, now.Add(time.Hour))
	in := endpointdirectory.Registration{Bundle: f.Bundle}
	if r := h.request(t, "POST", "/endpoint-bundles", in, headers, cookie); r.status != 403 {
		t.Fatal("browser fabricated provider public key", r.status)
	}
	signed := proofHeaders(t, h, d, key, "POST", "/endpoint-bundles", in, ids.New("register"), "")
	v := decode[endpointdirectory.Record](t, h.request(t, "POST", "/endpoint-bundles", in, signed), 201)
	if v.Bundle.Endpoint != d.ID {
		t.Fatal("signed device binding changed")
	}
	if r := h.request(t, "POST", "/endpoint-bundles", in, signed); r.status != 401 {
		t.Fatal("device proof nonce replayed", r.status)
	}
}
func TestHTTPEndpointDirectoryAbsentCompositionFailsClosed(t *testing.T) {
	h := newHarness(t, 1000, 1000)
	view, cookie := h.login(t, "WEB")
	if r := h.request(t, "GET", "/endpoint-bundles", nil, endpointBrowserHeaders(h, view), cookie); r.status != 503 {
		t.Fatal("missing directory composition became ready", r.status)
	}
}
