package invocation

// SOURCE_ONLY configuration definitions. No database is opened by the fixture.
import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"p11.local/compute/internal/persistence"
)

func syntheticSDKConfig(t *testing.T) SDKConfig {
	t.Helper()
	db := &persistence.DB{}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{11}, 32))
	claims, err := NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{"issuer_test": private.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(claims.Close)
	inv, err := New(Config{DB: db, PaymentEnvironment: "SANDBOX", FingerprintVersion: "fp_test", FingerprintKeys: map[string][]byte{"fp_test": bytes.Repeat([]byte{12}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inv.Close)
	delivery, err := NewDelivery(DeliveryConfig{Claims: claims, Invocations: inv, InputKeys: map[string][]byte{"input_test": bytes.Repeat([]byte{13}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(delivery.Close)
	completion, err := NewCompletion(claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err := NewResultContent(completion, "content_test", map[string][]byte{"content_test": bytes.Repeat([]byte{14}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(content.Close)
	return SDKConfig{Delivery: delivery, Content: content, InputVersion: "input_test", SigningKeyID: "issuer_test", SigningPrivateKey: private}
}
func TestSDKRequiresIndependentlyPinnedSigner(t *testing.T) {
	c := syntheticSDKConfig(t)
	c.SigningKeyID = "issuer_other"
	if s, err := NewSDK(c); err == nil || s != nil {
		t.Fatal("unprovisioned signer enabled")
	}
	c.SigningKeyID = "issuer_test"
	c.SigningPrivateKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{15}, 32))
	if s, err := NewSDK(c); err == nil || s != nil {
		t.Fatal("conflicting signer enabled")
	}
}
func TestSDKRequiresSameOriginalCompletionAuthority(t *testing.T) {
	c := syntheticSDKConfig(t)
	other := syntheticSDKConfig(t)
	c.Content = other.Content
	if s, err := NewSDK(c); err == nil || s != nil {
		t.Fatal("unrelated completion enabled")
	}
}
func TestSDKRequiresFourSeparateKeyPurposes(t *testing.T) {
	for _, value := range []byte{11, 12, 13} {
		c := syntheticSDKConfig(t)
		clear(c.Content.keys["content_test"])
		c.Content.keys["content_test"] = bytes.Repeat([]byte{value}, 32)
		if s, err := NewSDK(c); err == nil || s != nil {
			t.Fatal("cross-purpose reuse enabled")
		}
	}
}
func TestSDKCloseDoesNotEraseSharedDeliveryOrContent(t *testing.T) {
	c := syntheticSDKConfig(t)
	s, err := NewSDK(c)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if len(c.Delivery.keys["input_test"]) != 32 || len(c.Content.keys["content_test"]) != 32 || len(s.execution.signer) != 0 || len(s.execution.keys) != 0 {
		t.Fatal("ownership/erasure boundary")
	}
}
