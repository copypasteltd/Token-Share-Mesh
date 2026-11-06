package devicecontrol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointdirectory"
	"testing"
	"time"
)

func TestEndpointDirectoryPreparedCommandKeepsOriginalBytesAndScope(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "seller_public", "device_public", "SELLER", 1, now, now.Add(time.Hour))
	raw, _ := canonicaljson.Encode(endpointdirectory.Registration{Bundle: f.Bundle}, 16384)
	c, e := NewClient("https://directory.example.invalid", "device_public", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32)))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	prepared, e := c.PrepareEndpointDirectory(EndpointRegister, f.Bundle.Content.ID, "seller_public", raw, "original_directory_key")
	if e != nil {
		t.Fatal(e)
	}
	saved := bytes.Clone(raw)
	clear(raw)
	if !bytes.Equal(prepared.body, saved) || prepared.binding.URI != "/v1/p11/endpoint-bundles" || prepared.binding.IdempotencyKey != "original_directory_key" {
		t.Fatal("prepared public command regenerated")
	}
	if _, e = c.PrepareEndpointDirectory(EndpointRegister, f.Bundle.Content.ID, "seller_other", saved, "other_directory_key"); e == nil {
		t.Fatal("foreign seller scope accepted")
	}
}
func TestEndpointDirectoryReplyRejectsTrustUpgradeAndUnknownSuccess(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "seller_public", "device_public", "SELLER", 1, now, now.Add(time.Hour))
	v := endpointdirectory.Record{Profile: endpointdirectory.Profile, Recipient: f.Bundle.Content.ID, Bundle: f.Bundle, BundleHash: f.Bundle.Hash(), State: "ACTIVE", IndependentTrustRequired: true}
	raw, _ := json.Marshal(v)
	if _, e := EndpointDirectoryReply(Reply{Status: 201, Body: raw}); e != nil {
		t.Fatal(e)
	}
	v.IndependentTrustRequired = false
	raw, _ = json.Marshal(v)
	if _, e := EndpointDirectoryReply(Reply{Status: 201, Body: raw}); e == nil {
		t.Fatal("directory trust upgrade accepted")
	}
	if _, e := EndpointDirectoryReply(Reply{Status: 202, Body: raw}); e == nil {
		t.Fatal("async unknown treated as committed")
	}
}
