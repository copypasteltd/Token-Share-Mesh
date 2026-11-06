package endpointregistry

import (
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"testing"
	"time"
)

func TestDirectorySellerRegistrationRequiresExactSignedDeviceKind(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "seller_public", "device_public", "SELLER", 1, now, now.Add(time.Hour))
	p := identity.Principal{Kind: identity.Device, UserID: f.Bundle.Owner, DeviceID: f.Bundle.Endpoint}
	if validateBundle(p, f.Bundle) != nil {
		t.Fatal("exact provider proof shape rejected")
	}
	p.DeviceID = "device_other"
	if fault.Public(validateBundle(p, f.Bundle)).Code != fault.Forbidden {
		t.Fatal("cross-device public endpoint accepted")
	}
	p.Kind = identity.UserSession
	p.SessionID = "session_public"
	if fault.Public(validateBundle(p, f.Bundle)).Code != fault.Forbidden {
		t.Fatal("browser fabricated provider public identity")
	}
}
func TestDirectoryBuyerRegistrationRejectsAPIKeyAndMissingSession(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "buyer_public", "endpoint_public", "BUYER", 1, now, now.Add(time.Hour))
	for _, p := range []identity.Principal{{Kind: identity.APIKey, UserID: f.Bundle.Owner}, {Kind: identity.UserSession, UserID: f.Bundle.Owner}} {
		if validateBundle(p, f.Bundle) == nil {
			t.Fatal("unpaired/non-session buyer public key registered")
		}
	}
}
func TestDirectoryProviderCannotReuseItsControlAuthenticationPublicKey(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "seller_public", "device_public", "SELLER", 1, now, now.Add(time.Hour))
	for _, fp := range []string{f.Bundle.Identity.Fingerprint, f.Bundle.Content.Fingerprint} {
		p := identity.Principal{Kind: identity.Device, UserID: f.Bundle.Owner, DeviceID: f.Bundle.Endpoint, DeviceFingerprint: fp}
		if validateBundle(p, f.Bundle) == nil {
			t.Fatal("control authentication public purpose reused for content endpoint")
		}
	}
}
func TestDirectoryOrderDeviceMustBeBothListedAndKernelBound(t *testing.T) {
	c := trade.Contract{DeviceIDs: []string{"device_original"}, Bindings: []market.Binding{{DeviceID: "device_other"}}}
	if contractDevice(c, "device_original") || contractDevice(c, "device_other") {
		t.Fatal("one-sided order device projection accepted")
	}
	c.Bindings[0].DeviceID = "device_original"
	if !contractDevice(c, "device_original") {
		t.Fatal("original device rejected")
	}
}
