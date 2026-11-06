package endpointwire_test

import (
	"bytes"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointwire"
	"testing"
	"time"
)

func TestPublicEndpointSignatureDomainAndExactCanonicalRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "BUYER", 1, now, now.Add(time.Hour))
	if f.Bundle.Validate() != nil {
		t.Fatal("original public signature rejected")
	}
	raw, e := f.Bundle.Bytes()
	if e != nil {
		t.Fatal(e)
	}
	var b endpointwire.Bundle
	if canonicaljson.DecodeExact(raw, &b, 8192) != nil || b.Hash() != f.Bundle.Hash() {
		t.Fatal("canonical public bundle changed")
	}
	msg, e := b.SigningMessage()
	if e != nil || !bytes.HasPrefix(msg, []byte("P11/E2E/ENDPOINT/BUNDLE_SIGNATURE\x00")) {
		t.Fatal("bundle signature domain changed")
	}
	if canonicaljson.DecodeExact(append([]byte(" "), raw...), &b, 8192) == nil {
		t.Fatal("noncanonical public metadata accepted as exact")
	}
}
func TestPublicEndpointTamperingIsRejectedWithoutNativeCrypto(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "SELLER", 1, now, now.Add(time.Hour))
	for name, edit := range map[string]func(*endpointwire.Bundle){"owner": func(b *endpointwire.Bundle) { b.Owner = "owner_other" }, "endpoint": func(b *endpointwire.Bundle) { b.Endpoint = "endpoint_other" }, "version": func(b *endpointwire.Bundle) { b.Version = "2" }, "purpose": func(b *endpointwire.Bundle) { b.Content.Algorithm = "Ed25519" }, "expiry": func(b *endpointwire.Bundle) { b.Expires = now.Add(2 * time.Hour).Format(time.RFC3339Nano) }} {
		t.Run(name, func(t *testing.T) {
			b := f.Bundle
			edit(&b)
			if b.Validate() == nil {
				t.Fatal("tampered public metadata verified")
			}
		})
	}
}
func TestPublicEndpointRolePurposeAndCanonicalCounterConstraints(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []string{"missing_receipt", "seller_receipt", "same_key", "bad_version", "noncanonical_time"} {
		t.Run(kind, func(t *testing.T) {
			f := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "BUYER", 1, now, now.Add(time.Hour))
			switch kind {
			case "missing_receipt":
				f.Bundle.Receipt = nil
			case "seller_receipt":
				f.Bundle.Role = "SELLER"
			case "same_key":
				f.Bundle.Content.ID = f.Bundle.Identity.ID
			case "bad_version":
				f.Bundle.Version = "01"
			case "noncanonical_time":
				f.Bundle.NotBefore = now.Format("2006-01-02T15:04:05.999999999-07:00")
			}
			testsupport.SignPublicEndpoint(t, &f)
			if f.Bundle.Validate() == nil {
				t.Fatal("signed invalid purpose metadata accepted")
			}
		})
	}
}
func TestPublicEndpointExactValidityBoundaries(t *testing.T) {
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "SELLER", 1, now, now.Add(time.Hour))
	if f.Bundle.Current(now.Add(-time.Nanosecond)) == nil || f.Bundle.Current(now) != nil || f.Bundle.Current(now.Add(time.Hour-time.Nanosecond)) != nil || f.Bundle.Current(now.Add(time.Hour)) == nil {
		t.Fatal("public endpoint validity widened")
	}
}
func TestPublicRotationRequiresOriginalIdentityAndMonotonicBinding(t *testing.T) {
	now := time.Now().UTC()
	old := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "SELLER", 1, now.Add(-time.Hour), now.Add(time.Hour))
	_, r := testsupport.RotatePublicEndpoint(t, old, now, now.Add(2*time.Hour))
	if r.Verify(old.Bundle, now) != nil {
		t.Fatal("original public transition rejected")
	}
	for _, kind := range []string{"owner", "endpoint", "role", "skip", "previous", "recorded_after_expiry"} {
		t.Run(kind, func(t *testing.T) {
			next := testsupport.PublicEndpoint(t, old.Bundle.Owner, old.Bundle.Endpoint, "SELLER", 2, now, now.Add(time.Hour))
			switch kind {
			case "owner":
				next.Bundle.Owner = "owner_other"
			case "endpoint":
				next.Bundle.Endpoint = "endpoint_other"
			case "role":
				next = testsupport.PublicEndpoint(t, old.Bundle.Owner, old.Bundle.Endpoint, "BUYER", 2, now, now.Add(time.Hour))
			case "skip":
				next.Bundle.Version = "3"
			}
			testsupport.SignPublicEndpoint(t, &next)
			bad := testsupport.SignPublicRotation(t, old, next.Bundle, now)
			if kind == "previous" {
				bad.Previous = endpointwire.Hash([]byte("unrelated"))
			}
			if kind == "recorded_after_expiry" {
				bad = testsupport.SignPublicRotation(t, old, next.Bundle, now.Add(2*time.Hour))
			}
			if bad.Verify(old.Bundle, now) == nil {
				t.Fatal("invalid signed rotation accepted")
			}
		})
	}
}
func TestPublicRotationRejectsReuseAcrossOldPurposes(t *testing.T) {
	now := time.Now().UTC()
	old := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "BUYER", 1, now.Add(-time.Hour), now.Add(time.Hour))
	for _, kind := range []string{"identity", "content", "receipt", "key_id_alias", "cross_purpose"} {
		t.Run(kind, func(t *testing.T) {
			next, _ := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
			switch kind {
			case "identity":
				next.Identity = old.Identity
				next.Bundle.Identity = old.Bundle.Identity
			case "content":
				next.Bundle.Content = old.Bundle.Content
			case "receipt":
				k := *old.Bundle.Receipt
				next.Bundle.Receipt = &k
			case "key_id_alias":
				next.Bundle.Content.ID = old.Bundle.Identity.ID
			case "cross_purpose":
				k := old.Bundle.Identity
				next.Bundle.Receipt = &k
			}
			testsupport.SignPublicEndpoint(t, &next)
			r := testsupport.SignPublicRotation(t, old, next.Bundle, now)
			if r.Verify(old.Bundle, now) == nil {
				t.Fatal("old public purpose reused by successor")
			}
		})
	}
}
