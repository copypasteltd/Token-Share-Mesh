package api_test

import (
	"bytes"
	"encoding/json"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/shared/ids"
	"testing"
)

func TestHTTPExecutionDeliveryNativeSignedReadsReturnManifestAndExactInput(t *testing.T) {
	h, f, issued, _ := claimHTTPFixture(t)
	c, e := devicecontrol.NewClient(h.server.URL, f.Device, f.DeviceKey)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	cmd, e := c.PrepareLeaseClaim(devicecontrol.LeaseClaim{Device: f.Device, Capability: f.Capability, ClaimKey: ids.New("claim"), Version: 1})
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.Send(ctx, cmd)
	if e != nil || r.Status != 200 {
		t.Fatal(r.Status, e)
	}
	manifestCmd, e := c.PrepareManifest(issued.LeaseID)
	if e != nil {
		t.Fatal(e)
	}
	inputCmd, e := c.PrepareInput(issued.LeaseID)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e = c.Send(ctx, manifestCmd)
		if e != nil || r.Status != 200 {
			t.Fatal(r.Status, e)
		}
		var m executionlease.Manifest
		if json.Unmarshal(r.Body, &m) != nil || m.Digest() != issued.Manifest.Digest() {
			t.Fatal("wrong manifest")
		}
		if bytes.Contains(r.Body, []byte("R018_SYNTHETIC_ONLY")) {
			t.Fatal("content in metadata")
		}
		r, e = c.Send(ctx, inputCmd)
		if e != nil || r.Status != 200 || !bytes.Equal(r.Body, []byte(claimHTTPBody)) {
			t.Fatal("exact raw input transport", r.Status, e)
		}
		clear(r.Body)
	}
	var intact bool
	if e = db.Pool.QueryRow(ctx, `SELECT l.state='ISSUED' AND a.state='ISSUED' AND b.state='RESERVED' AND r.phase='ACTIVE'
 FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id JOIN p11.budget_reservations b ON b.id=j.reservation_id JOIN p11.resource_locks r ON r.resource_id=a.resource_id WHERE l.id=$1`, issued.LeaseID).Scan(&intact); e != nil || !intact {
		t.Fatal("read is not execution ACK or release", e)
	}
}

func TestHTTPExecutionDeliveryRejectsUnclaimedSessionProofReplayAndPartialReads(t *testing.T) {
	h, f, issued, session := claimHTTPFixture(t)
	v := device.View{ID: f.Device}
	path := "/runner/leases/" + issued.LeaseID + "/input"
	hdr := proofHeaders(t, h, v, f.DeviceKey, "GET", path, nil, "", "")
	expectError(t, h.request(t, "GET", path, nil, hdr), 404, "NOT_FOUND")
	expectError(t, h.request(t, "GET", path, nil, bearer(session.AccessToken)), 403, "FORBIDDEN")
	c, e := devicecontrol.NewClient(h.server.URL, f.Device, f.DeviceKey)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	cmd, _ := c.PrepareLeaseClaim(devicecontrol.LeaseClaim{Device: f.Device, Capability: f.Capability, ClaimKey: ids.New("claim"), Version: 1})
	if r, e := c.Send(ctx, cmd); e != nil || r.Status != 200 {
		t.Fatal(r.Status, e)
	}
	hdr = proofHeaders(t, h, v, f.DeviceKey, "GET", path, nil, "", "")
	response := h.request(t, "GET", path, nil, hdr)
	raw := response.body
	defer clear(raw)
	if response.status != 200 || response.header.Get("Cache-Control") != "no-store" || !bytes.Equal(raw, []byte(claimHTTPBody)) {
		t.Fatal("unbounded/cached/rewritten input", e)
	}
	expectError(t, h.request(t, "GET", path, nil, hdr), 401, "UNAUTHENTICATED")
	for _, name := range []string{"Range", "If-Range", "If-None-Match"} {
		hdr = proofHeaders(t, h, v, f.DeviceKey, "GET", path, nil, "", "")
		hdr.Set(name, "bytes=0-10")
		expectError(t, h.request(t, "GET", path, nil, hdr), 400, "INVALID_REQUEST")
	}
}

func TestHTTPExecutionDeliveryUnconfiguredContentKeysFailClosed(t *testing.T) {
	h := deviceHarness(t)
	s, _ := h.login(t, "MINIAPP")
	grantDeviceSeller(t, s)
	v, key := httpEnrolled(t, h, bearer(s.AccessToken), nil)
	for _, suffix := range []string{"manifest", "input"} {
		path := "/runner/leases/" + ids.New("lease") + "/" + suffix
		hdr := proofHeaders(t, h, v, key, "GET", path, nil, "", "")
		expectError(t, h.request(t, "GET", path, nil, hdr), 503, "UNAVAILABLE")
	}
}
