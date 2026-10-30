package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/internal/persistence"
)

func TestLeaseVerifierFileIsExplicitBoundedAndPublicOnly(t *testing.T) {
	t.Setenv("P11_LEASE_VERIFIERS_FILE", "")
	if s, e := configuredClaims(nil); e != nil || s != nil {
		t.Fatal("unset must disable", e)
	}
	path := filepath.Join(t.TempDir(), "verifiers.json")
	t.Setenv("P11_LEASE_VERIFIERS_FILE", path)
	t.Setenv("P11_PAYMENT_ENVIRONMENT", "SANDBOX")
	key := ed25519.NewKeyFromSeed(make([]byte, 32)).Public().(ed25519.PublicKey)
	good := `{"profile_id":"P11_LEASE_VERIFIERS_1","keys":{"issuer_original":"` + base64.RawURLEncoding.EncodeToString(key) + `"}}`
	if e := os.WriteFile(path, []byte(good), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := configuredClaims(&persistence.DB{})
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	for _, bad := range []string{`{}`, `{"profile_id":"P11_LEASE_VERIFIERS_1","keys":{}}`, strings.Replace(good, `"keys":`, `"private_key":"not_permitted","keys":`, 1), strings.Repeat(" ", 8193), strings.Replace(good, `"issuer_original":`, `"issuer_original":"AA","issuer_original":`, 1)} {
		if e = os.WriteFile(path, []byte(bad), 0600); e != nil {
			t.Fatal(e)
		}
		if s, e = configuredClaims(&persistence.DB{}); e == nil || s != nil {
			t.Fatal("malformed configuration enabled claims")
		}
	}
	if !json.Valid([]byte(good)) {
		t.Fatal("fixture")
	}
	if e = os.WriteFile(path, []byte(good), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("P11_PAYMENT_ENVIRONMENT", "")
	if _, e = configuredClaims(&persistence.DB{}); e == nil {
		t.Fatal("missing environment")
	}
}
