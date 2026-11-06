package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"p11.local/compute/internal/persistence"
)

func TestRentalConfigurationIsOptionalButNeverPartiallyTrusted(t *testing.T) {
	t.Setenv("P11_RENTAL_SIGNER_FILE", "")
	t.Setenv("P11_RENTAL_VERIFIERS_FILE", "")
	if s, err := configuredRentals(nil); err != nil || s != nil {
		t.Fatal("absent rental config did not disable service", err)
	}
	t.Setenv("P11_RENTAL_SIGNER_FILE", filepath.Join(t.TempDir(), "missing.json"))
	if s, err := configuredRentals(&persistence.DB{}); err == nil {
		if s != nil {
			s.Close()
		}
		t.Fatal("partial configuration accepted")
	}
}

func TestRentalConfigurationRejectsIssuerPinMismatchAndAmbiguity(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{83}, 32))
	defer clear(key)
	dir := t.TempDir()
	signerPath, pinsPath := filepath.Join(dir, "signer.json"), filepath.Join(dir, "pins.json")
	t.Setenv("P11_RENTAL_SIGNER_FILE", signerPath)
	t.Setenv("P11_RENTAL_VERIFIERS_FILE", pinsPath)
	t.Setenv("P11_PAYMENT_ENVIRONMENT", "SANDBOX")
	write := func(path string, v any) {
		t.Helper()
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(signerPath, map[string]string{"profile_id": "P11_RENTAL_SIGNER_1", "issuer_id": "issuer_test", "key_id": "key_test", "private_key": base64.RawURLEncoding.EncodeToString(key)})
	write(pinsPath, map[string]any{"profile_id": "P11_RENTAL_VERIFIERS_1", "issuer_id": "issuer_test", "keys": map[string]string{"key_test": base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}})
	s, e := configuredRentals(&persistence.DB{})
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	write(pinsPath, map[string]any{"profile_id": "P11_RENTAL_VERIFIERS_1", "issuer_id": "issuer_other", "keys": map[string]string{"key_test": base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}})
	if s, e = configuredRentals(&persistence.DB{}); e == nil {
		s.Close()
		t.Fatal("different issuer trusted")
	}
	if e = os.WriteFile(pinsPath, []byte(`{"profile_id":"P11_RENTAL_VERIFIERS_1","profile_id":"P11_RENTAL_VERIFIERS_1","issuer_id":"issuer_test","keys":{}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if s, e = configuredRentals(&persistence.DB{}); e == nil {
		s.Close()
		t.Fatal("duplicate verifier field accepted")
	}
	if e = os.WriteFile(pinsPath, bytes.Repeat([]byte{' '}, 8193), 0600); e != nil {
		t.Fatal(e)
	}
	if s, e = configuredRentals(&persistence.DB{}); e == nil {
		s.Close()
		t.Fatal("oversized verifier configuration accepted")
	}
}
