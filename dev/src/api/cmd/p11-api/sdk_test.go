package main

// SOURCE_ONLY, no listener or database is started by these file fixtures.
import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
)

func sdkHostFixture(t *testing.T) (*persistence.DB, *invocation.Delivery, *invocation.ResultContent, string) {
	t.Helper()
	db := &persistence.DB{}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{71}, 32))
	claims, err := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{"issuer_sdk": private.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(claims.Close)
	inv, err := invocation.New(invocation.Config{DB: db, PaymentEnvironment: "SANDBOX", FingerprintVersion: "fp_sdk", FingerprintKeys: map[string][]byte{"fp_sdk": bytes.Repeat([]byte{72}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inv.Close)
	d, err := invocation.NewDelivery(invocation.DeliveryConfig{Claims: claims, Invocations: inv, InputKeys: map[string][]byte{"input_sdk": bytes.Repeat([]byte{73}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	completion, err := invocation.NewCompletion(claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err := invocation.NewResultContent(completion, "content_sdk", map[string][]byte{"content_sdk": bytes.Repeat([]byte{74}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(content.Close)
	good := `{"profile_id":"P11_NATIVE_TEXT_SDK_1","input_version":"input_sdk","signing_key_id":"issuer_sdk","signing_private_key":"` + base64.RawURLEncoding.EncodeToString(private) + `"}`
	return db, d, content, good
}
func TestSDKHostIsExplicitlyOptIn(t *testing.T) {
	t.Setenv("P11_NATIVE_TEXT_SDK_FILE", "")
	s, close, err := configuredSDK(nil, nil, nil, identity.KeyService{}, nil)
	if err != nil || s != nil {
		t.Fatal(err)
	}
	close()
}
func TestSDKHostRequiresPinnedCompleteBoundedConfig(t *testing.T) {
	db, d, content, good := sdkHostFixture(t)
	path := filepath.Join(t.TempDir(), "sdk.json")
	t.Setenv("P11_NATIVE_TEXT_SDK_FILE", path)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(good)
	s, close, err := configuredSDK(db, d, content, identity.KeyService{DB: db}, []string{"127.0.0.1:8787"})
	if err != nil || s == nil {
		t.Fatal(err)
	}
	close()
	close()
	for _, bad := range []string{`{}`, strings.Repeat(" ", 4097), strings.Replace(good, `"issuer_sdk"`, `"issuer_other"`, 1), strings.Replace(good, `"input_sdk"`, `"input_other"`, 1), strings.Replace(good, `"input_version":`, `"input_version":"input_other","input_version":`, 1), strings.Replace(good, `"profile_id":`, `"unsafe_download":true,"profile_id":`, 1)} {
		write(bad)
		s, close, err = configuredSDK(db, d, content, identity.KeyService{DB: db}, []string{"127.0.0.1:8787"})
		if err == nil || s != nil {
			close()
			t.Fatal("bad file enabled SDK")
		}
	}
}
func TestSDKHostRejectsDifferentIdentityStore(t *testing.T) {
	db, d, content, good := sdkHostFixture(t)
	path := filepath.Join(t.TempDir(), "sdk.json")
	t.Setenv("P11_NATIVE_TEXT_SDK_FILE", path)
	if err := os.WriteFile(path, []byte(good), 0600); err != nil {
		t.Fatal(err)
	}
	s, close, err := configuredSDK(db, d, content, identity.KeyService{DB: &persistence.DB{}}, []string{"127.0.0.1:8787"})
	if err == nil || s != nil {
		close()
		t.Fatal("identity store substituted")
	}
}
