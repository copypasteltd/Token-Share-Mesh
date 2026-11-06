package rental

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/persistence"
)

func syntheticIssued(t *testing.T) (*rentalaccess.Issued, string) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{82}, 32)) // Test-only deterministic key.
	defer clear(key)
	s, err := rentalaccess.NewSigner("issuer_test", "key_test", key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	c := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: "issuer_test", Subject: "buyer_test", Audience: "device_test", ID: "grant_test", Issued: now.Unix(), NotBefore: now.Unix(), Expires: now.Add(time.Minute).Unix(), Order: "order_test", Session: "session_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "4", Terms: "1", TermsHash: strings.Repeat("a", 64), Environment: "environment_test", EnvironmentHash: strings.Repeat("b", 64), Manifest: "manifest_test", ManifestHash: strings.Repeat("c", 64), Scopes: []string{"INVOKE"}, HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
	i, err := s.Issue(c, now)
	if err != nil {
		t.Fatal(err)
	}
	return i, c.ID
}

func TestRentalAuthorityRequiresIndependentConfiguredPins(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{81}, 32))
	defer clear(key)
	c := Config{DB: &persistence.DB{}, PaymentEnvironment: "SANDBOX", Issuer: "issuer_test", KeyID: "key_test", SigningKey: key, IssuerPins: map[string]ed25519.PublicKey{"key_test": key.Public().(ed25519.PublicKey)}}
	s, err := NewService(c)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if s.configured() == nil {
		t.Fatal("closed rental authority reopened")
	}
	for _, mutate := range []func(*Config){
		func(v *Config) { v.DB = nil }, func(v *Config) { v.PaymentEnvironment = "" },
		func(v *Config) { v.IssuerPins = nil }, func(v *Config) { v.SigningKey = key[:32] },
		func(v *Config) { v.IssuerPins = map[string]ed25519.PublicKey{"key_test": bytes.Repeat([]byte{1}, 32)} },
	} {
		bad := c
		mutate(&bad)
		if v, e := NewService(bad); e == nil {
			v.Close()
			t.Fatal("incomplete/unpinned rental configuration accepted")
		}
	}
}

func TestRentalAccessResponseNeverCachesOrFormatsBearer(t *testing.T) {
	i, id := syntheticIssued(t)
	r := &AccessResult{metadata: AccessMetadata{Grant: id, Session: "session_test", SessionVersion: 5, Scopes: []string{"INVOKE"}, CredentialState: "NOT_REPLAYABLE"}, issued: i}
	defer r.Close()
	raw, err := json.Marshal(r)
	if err != nil || bytes.Contains(raw, []byte("credential_once")) || !bytes.Contains(raw, []byte("NOT_REPLAYABLE")) {
		t.Fatal("ordinary serialization leaked first response")
	}
	if !strings.Contains(r.String(), "REDACTED") || !strings.Contains(r.GoString(), "REDACTED") {
		t.Fatal("formatted response exposed credential")
	}
	var first, second bytes.Buffer
	if err = r.WriteJSONOnce(&first); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Credential string `json:"credential_once"`
		State      string `json:"credential_state"`
	}
	if json.Unmarshal(first.Bytes(), &body) != nil || body.Credential == "" || body.State != "ISSUED_ONCE" {
		t.Fatal("committed first credential missing")
	}
	if err = r.WriteJSONOnce(&second); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(second.Bytes(), []byte(body.Credential)) || bytes.Contains(second.Bytes(), []byte("credential_once")) {
		t.Fatal("second response replayed credential")
	}
	m := r.Metadata()
	m.Scopes[0] = "EXPORT_SELF"
	if r.Metadata().Scopes[0] != "INVOKE" {
		t.Fatal("metadata scopes alias the stored response")
	}
}

type brokenRentalWriter struct{}

func (brokenRentalWriter) Write(p []byte) (int, error) {
	return len(p) / 2, errors.New("synthetic disconnected socket")
}

func TestRentalAccessLostWriteCannotReplayBearer(t *testing.T) {
	i, id := syntheticIssued(t)
	r := &AccessResult{metadata: AccessMetadata{Grant: id, Session: "session_test", SessionVersion: 5, CredentialState: "NOT_REPLAYABLE"}, issued: i}
	defer r.Close()
	if r.WriteJSONOnce(brokenRentalWriter{}) == nil {
		t.Fatal("failed first socket write reported success")
	}
	var next bytes.Buffer
	if err := r.WriteJSONOnce(&next); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(next.Bytes(), []byte("credential_once")) {
		t.Fatal("lost response was reissued")
	}
}

func TestRentalUnconfirmedViewOmitsClockAndAccounting(t *testing.T) {
	v := viewOf(Session{ID: "session_test", Version: 1, State: "RESERVED", Cleanup: "NOT_STARTED", Contract: Contract{Order: "order_test", Seller: "seller_test", Device: "device_test", Environment: "environment_test"}})
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hard_deadline_at", "started_at", "verified_available_ms", "charge_fen"} {
		if bytes.Contains(raw, []byte(field)) {
			t.Fatal("unconfirmed state invented a clock/statement", field)
		}
	}
}
