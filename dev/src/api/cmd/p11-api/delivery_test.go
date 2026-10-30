package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestInputDeliveryFileRequiresSeparateExplicitPurposeKeysAndClaims(t *testing.T) {
	t.Setenv("P11_INPUT_DELIVERY_KEYS_FILE", "")
	if d, close, e := configuredDelivery(nil, nil); e != nil || d != nil {
		t.Fatal(e)
	} else {
		close()
	}
	path := filepath.Join(t.TempDir(), "delivery.json")
	t.Setenv("P11_INPUT_DELIVERY_KEYS_FILE", path)
	t.Setenv("P11_PAYMENT_ENVIRONMENT", "SANDBOX")
	db := &persistence.DB{}
	pub := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{11}, 32)).Public().(ed25519.PublicKey)
	claims, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{"issuer_test": pub})
	if e != nil {
		t.Fatal(e)
	}
	defer claims.Close()
	fp := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{12}, 32))
	input := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{13}, 32))
	good := `{"profile_id":"P11_INPUT_DELIVERY_KEYS_1","fingerprint_version":"fp_original","fingerprint_keys":{"fp_original":"` + fp + `"},"input_keys":{"input_original":"` + input + `"}}`
	write := func(s string) {
		t.Helper()
		if e := os.WriteFile(path, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(good)
	if _, _, e := configuredDelivery(db, nil); e == nil {
		t.Fatal("claims omitted")
	}
	if d, close, e := configuredDelivery(db, claims); e != nil || d == nil {
		t.Fatal(e)
	} else {
		close()
	}
	for _, bad := range []string{`{}`, strings.Repeat(" ", 16385), strings.Replace(good, input, fp, 1), strings.Replace(good, `"input_keys":`, `"signing_private_key":"forbidden","input_keys":`, 1), strings.Replace(good, `"fp_original":"`, `"fp_original":"AA","fp_original":"`, 1), strings.Replace(good, input, input+"=", 1)} {
		write(bad)
		if d, close, e := configuredDelivery(db, claims); e == nil || d != nil {
			close()
			t.Fatal("invalid private configuration enabled input")
		}
	}
	write(good)
	t.Setenv("P11_PAYMENT_ENVIRONMENT", "")
	if d, close, e := configuredDelivery(db, claims); e == nil || d != nil {
		close()
		t.Fatal("implicit payment environment")
	}
}
