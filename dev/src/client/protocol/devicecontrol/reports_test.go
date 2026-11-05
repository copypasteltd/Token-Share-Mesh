package devicecontrol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"p11.local/compute/client/protocol/executionlease"
)

func TestProgressReceiptCannotGrantExecutionWithoutIndependentVerification(t *testing.T) {
	ack := `{"id":"report_original","status":"RECORDED","version":1,"recorded_at":"2026-11-06T00:00:00Z"}`
	good := `{"ack":` + ack + `,"action":"NO_CHANGE"}`
	if got, err := ProgressReceipt(Reply{Status: 200, Body: []byte(good)}); err != nil || got.Lease != nil || got.Action != "NO_CHANGE" {
		t.Fatal("bounded verifier-only receipt rejected", err)
	}
	for _, bad := range []string{
		`{"ack":` + ack + `,"action":"CONTINUE"}`,
		`{"ack":` + ack + `,"action":"CONTINUE","lease":null}`,
		`{"ack":` + ack + `,"action":"NO_CHANGE","lease":null}`,
		`{"ack":` + ack + `,"action":"STOP","lease":{}}`,
		strings.Replace(good, `"NO_CHANGE"`, `"RESTART"`, 1),
		strings.Replace(good, `"action":`, `"action":"STOP","action":`, 1),
		strings.Replace(good, `"action":`, `"plaintext":"must_not_cross","action":`, 1),
	} {
		if _, err := ProgressReceipt(Reply{Status: 200, Body: []byte(bad)}); err == nil {
			t.Fatal("invalid continuation shape accepted")
		}
	}
	if _, err := ProgressReceipt(Reply{Status: 202, Body: []byte(good)}); err == nil {
		t.Fatal("unconfirmed receipt accepted")
	}
	var shape map[string]any
	if err := json.Unmarshal([]byte(good), &shape); err != nil {
		t.Fatal(err)
	}
	shape["padding"] = strings.Repeat("x", 32768)
	oversized, _ := json.Marshal(shape)
	if _, err := ProgressReceipt(Reply{Status: 200, Body: oversized}); err == nil {
		t.Fatal("oversized reply accepted")
	}
}

func TestCleanupRecoveryHasSeparateDomainAndOriginalFailureBinding(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{63}, 32)) // synthetic only
	l := executionlease.Lease{ID: "lease_original", Device: "device_original", Attempt: "attempt_original", Generation: "9", Fence: "11"}
	in := CleanupWrite{Lease: l.ID, Fence: l.Fence, ManifestHash: strings.Repeat("a", 64), State: "ACKED", WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, Recorded: "2026-11-06T00:00:00Z", Profile: CleanupRecoveryProfile, RecoveryOf: "report_failed_original"}
	signed, err := SignCleanup(key, l, in)
	if err != nil || signed.Validate() != nil {
		t.Fatal("valid recovery could not be signed", err)
	}
	message, err := CleanupMessage(l.Device, l.Attempt, l.Generation, signed)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"lease_id":"lease_original","fencing_token":"11","cleanup_manifest_hash":"` + strings.Repeat("a", 64) + `","state":"ACKED","work_stopped":true,"tokens_revoked":true,"workspace_cleared":true,"recorded_at":"2026-11-06T00:00:00Z","signature_profile_id":"P11_DEVICE_CLEANUP_RECOVERY_1","signature":"","recovery_of":"report_failed_original"}`
	if string(message) != "P11_DEVICE_CLEANUP_RECOVERY_1\ndevice_original\nattempt_original\n9\nreport_failed_original\n"+want {
		t.Fatal("independent recovery wire vector changed")
	}
	sig, _ := base64.RawURLEncoding.Strict().DecodeString(signed.Signature)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, sig) {
		t.Fatal("recovery signature invalid")
	}
	changed := signed
	changed.RecoveryOf = "report_other_failure"
	altered, err := CleanupMessage(l.Device, l.Attempt, l.Generation, changed)
	if err != nil || ed25519.Verify(key.Public().(ed25519.PublicKey), altered, sig) {
		t.Fatal("signature accepted a different original failure", err)
	}
	for _, mutate := range []func(*CleanupWrite){
		func(v *CleanupWrite) { v.Profile = CleanupProfile }, func(v *CleanupWrite) { v.RecoveryOf = "" },
		func(v *CleanupWrite) { v.State = "FAILED" }, func(v *CleanupWrite) { v.WorkStopped = false },
		func(v *CleanupWrite) { v.TokensRevoked = false }, func(v *CleanupWrite) { v.WorkspaceCleared = false },
	} {
		bad := signed
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("incomplete or cross-profile cleanup recovery accepted")
		}
	}
	legacy := signed
	legacy.Profile = CleanupProfile
	legacy.RecoveryOf = ""
	legacyMessage, err := CleanupMessage(l.Device, l.Attempt, l.Generation, legacy)
	if err != nil || bytes.Contains(legacyMessage, []byte("recovery_of")) || !bytes.HasPrefix(legacyMessage, []byte("P11_DEVICE_CLEANUP_1\n")) {
		t.Fatal("legacy cleanup encoding changed", err)
	}
}
