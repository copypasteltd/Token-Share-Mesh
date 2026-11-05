package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
)

func TestContinuationSignerIsExplicitBoundedAndPinned(t *testing.T) {
	t.Setenv("P11_LEASE_CONTINUATION_SIGNER_FILE", "")
	if s, err := configuredContinuations(nil); err != nil || s != nil {
		t.Fatal("unset signer must leave verifier-only mode", err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{61}, 32)) // synthetic fixture, never a runtime key
	claims, err := invocation.NewClaims(&persistence.DB{}, "SANDBOX", map[string]ed25519.PublicKey{"issuer_original": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	defer claims.Close()
	path := filepath.Join(t.TempDir(), "continuation.json")
	t.Setenv("P11_LEASE_CONTINUATION_SIGNER_FILE", path)
	good := `{"profile_id":"P11_LEASE_CONTINUATION_SIGNER_1","key_id":"issuer_original","private_key":"` + base64.RawURLEncoding.EncodeToString(key) + `"}`
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := configuredContinuations(nil); err == nil || s != nil {
		t.Fatal("missing independent verifier accepted")
	}
	s, err := configuredContinuations(claims)
	if err != nil || s == nil || !s.UsesClaims(claims) {
		t.Fatal("explicit pinned signer rejected", err)
	}
	s.Close()
	wrongKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{62}, 32))
	for _, bad := range []string{
		`{}`, strings.Repeat(" ", 4097),
		strings.Replace(good, `"issuer_original"`, `"issuer_other"`, 1),
		strings.Replace(good, base64.RawURLEncoding.EncodeToString(key), base64.RawURLEncoding.EncodeToString(wrongKey), 1),
		strings.Replace(good, `"private_key":`, `"content_key":"forbidden","private_key":`, 1),
		strings.Replace(good, `"key_id":`, `"key_id":"issuer_other","key_id":`, 1),
	} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if s, err := configuredContinuations(claims); err == nil || s != nil {
			if s != nil {
				s.Close()
			}
			t.Fatal("invalid signer enabled continuation")
		}
	}
}
