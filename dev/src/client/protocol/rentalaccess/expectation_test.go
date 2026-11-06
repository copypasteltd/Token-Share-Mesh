package rentalaccess_test

import (
	"strings"
	"testing"

	"p11.local/compute/client/protocol/rentalaccess"
)

func TestRentalExpectationStoredDigestCannotRebindApprovedTuple(t *testing.T) {
	f := fixture(t)
	g := rentalaccess.GrantExpectation{Profile: rentalaccess.ExpectationProfile, Metadata: f.metadata, SessionVersion: 7, Connection: "connection_original"}
	approved := f.expected
	approved.TokenDigest = ""
	bound, err := g.Bind(approved, f.now)
	if err != nil || bound.TokenDigest != f.metadata.Digest {
		t.Fatal("original independent digest not bound", err)
	}
	for _, field := range []string{"buyer", "device", "generation", "environment", "manifest", "grant", "digest"} {
		wrong := approved
		switch field {
		case "buyer":
			wrong.Subject = "buyer_other"
		case "device":
			wrong.Device = "device_other"
		case "generation":
			wrong.Generation = "13"
		case "environment":
			wrong.Environment = "environment_other"
		case "manifest":
			wrong.ManifestHash = strings.Repeat("b", 64)
		case "grant":
			wrong.Grant = "grant_other"
		case "digest":
			wrong.TokenDigest = strings.Repeat("b", 64)
		}
		if _, err := g.Bind(wrong, f.now); err == nil {
			t.Fatal("stored digest rebound local approval", field)
		}
	}
	bad := g
	bad.Metadata.Profile = "LEGACY_UNVERIFIED"
	if _, err := bad.Bind(approved, f.now); err == nil {
		t.Fatal("legacy metadata promoted to authority")
	}
}
