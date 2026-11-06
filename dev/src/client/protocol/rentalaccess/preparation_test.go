package rentalaccess_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/contractspec"
)

func preparationFixture(t *testing.T, now time.Time) ([]byte, rentalaccess.LocalApproval) {
	t.Helper()
	m := rentalaccess.Manifest{Profile: rentalaccess.ManifestProfile, Reference: "manifest_test", Session: "session_test", Order: "order_test", Buyer: "buyer_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "4", Capability: "capability_test", Environment: "environment_test", EnvironmentHash: strings.Repeat("b", 64), ContractHash: strings.Repeat("d", 64), Mode: "GPU_RENTAL", Privacy: "STANDARD", Engine: "LLAMA_CPP_NATIVE", Release: "llama.cpp-b11960", ModelHash: strings.Repeat("e", 64), RecipeHash: strings.Repeat("f", 64), Protocol: "openai_nb1", PublicSpec: contractspec.Selection{ModelID: "model_test", DurationMS: 60000, InputTokenLimit: 2048, OutputTokenLimit: 256}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	a := rentalaccess.LocalApproval{Profile: rentalaccess.ApprovalProfile, Manifest: m, ManifestHash: hex.EncodeToString(sum[:]), PhysicalHash: strings.Repeat("a", 64), Connection: "connection_test", Issuer: "issuer_test", PrepareUntil: now.Add(time.Minute).UTC()}
	return raw, a
}

func TestRentalPreparationUsesExactOriginalRawAndIndependentApproval(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw, approval := preparationFixture(t, now)
	p, err := rentalaccess.VerifyPreparation(raw, approval, now)
	if err != nil || p.Digest() != approval.ManifestHash {
		t.Fatal("valid original preparation rejected", err)
	}
	for _, field := range []string{"buyer", "device", "physical", "manifest", "environment", "mode", "engine"} {
		bad := approval
		switch field {
		case "buyer":
			bad.Manifest.Buyer = "buyer_other"
		case "device":
			bad.Manifest.Device = "device_other"
		case "physical":
			bad.PhysicalHash = ""
		case "manifest":
			bad.ManifestHash = strings.Repeat("1", 64)
		case "environment":
			bad.Manifest.EnvironmentHash = strings.Repeat("2", 64)
		case "mode":
			bad.Manifest.Mode = "PLAINTEXT"
		case "engine":
			bad.Manifest.Engine = "SHELL"
		}
		if _, err := rentalaccess.VerifyPreparation(raw, bad, now); err == nil {
			t.Fatal("remote metadata redefined local approval", field)
		}
	}
	spaced := append([]byte(" "), raw...)
	if _, err := rentalaccess.VerifyPreparation(spaced, approval, now); err == nil {
		t.Fatal("semantic JSON substituted for original hashed bytes")
	}
	unknown := append([]byte(nil), raw[:len(raw)-1]...)
	unknown = append(unknown, []byte(`,"prompt":"customer content"}`)...)
	if _, err := rentalaccess.VerifyPreparation(unknown, approval, now); err == nil {
		t.Fatal("content/unknown field accepted in preparation")
	}
	copyRaw := p.Raw()
	copyRaw[0] = '!'
	if p.Raw()[0] != '{' {
		t.Fatal("caller mutated prepared original raw bytes")
	}
}

func TestRentalPreparationRecoveryMetadataNeverRestoresTTL(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	raw, approval := preparationFixture(t, now)
	late := now.Add(2 * time.Minute)
	if _, err := rentalaccess.VerifyPreparation(raw, approval, late); err == nil {
		t.Fatal("expired preparation became a new reservation")
	}
	p, err := rentalaccess.DecodePreparationRecord(raw, approval)
	if err != nil || p.ValidAt(late) {
		t.Fatal("metadata restore created live authority", err)
	}
	approval.PrepareUntil = now.Add(121 * time.Second)
	if _, err := rentalaccess.VerifyPreparation(raw, approval, now); err == nil {
		t.Fatal("unbounded preparation TTL accepted")
	}
	far, err := rentalaccess.DecodePreparationRecord(raw, approval)
	if err != nil || far.ValidAt(now) {
		t.Fatal("metadata decoder bypassed the bounded preparation window", err)
	}
}
