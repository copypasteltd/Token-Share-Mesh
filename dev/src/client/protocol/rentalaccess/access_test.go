package rentalaccess_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

type accessFixture struct {
	now        time.Time
	claims     rentalaccess.Claims
	expected   rentalaccess.Expected
	key        ed25519.PrivateKey
	signer     *rentalaccess.Signer
	verifier   *rentalaccess.Verifier
	metadata   rentalaccess.Metadata
	credential string
	verified   rentalaccess.Verified
}

func fixture(t *testing.T) accessFixture {
	t.Helper()
	// RFC 8032 test-vector key, synthetic and never a deployment credential.
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	key := ed25519.NewKeyFromSeed(seed)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	hash := strings.Repeat("a", 64)
	c := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: "issuer_platform", Subject: "usr_buyer", Audience: "endpoint_phone", ID: "grant_original", Issued: now.Unix(), NotBefore: now.Unix(), Expires: now.Add(5 * time.Minute).Unix(), Order: "order_original", Session: "session_original", Seller: "seller_original", Device: "device_original", Resource: "resource_original", Generation: "12", Terms: "3", TermsHash: hash, Environment: "environment_original", EnvironmentHash: hash, Manifest: "manifest_original", ManifestHash: hash, Scopes: []string{"INVOKE", "CANCEL_SELF", "EXPORT_SELF"}, HardDeadline: now.Add(10 * time.Minute).Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
	e := rentalaccess.Expected{Issuer: c.Issuer, Subject: c.Subject, Audience: c.Audience, Grant: c.ID, Order: c.Order, Session: c.Session, Seller: c.Seller, Device: c.Device, Resource: c.Resource, Generation: c.Generation, Terms: c.Terms, TermsHash: c.TermsHash, Environment: c.Environment, EnvironmentHash: c.EnvironmentHash, Manifest: c.Manifest, ManifestHash: c.ManifestHash, HardDeadline: c.HardDeadline}
	s, err := rentalaccess.NewSigner(c.Issuer, "issuer_key_original", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	v, err := rentalaccess.NewVerifier(map[string]ed25519.PublicKey{"issuer_key_original": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	i, err := s.Issue(c, now)
	if err != nil {
		t.Fatal(err)
	}
	metadata := i.Metadata()
	e.TokenDigest = metadata.Digest
	token, err := i.TakeCredential()
	if err != nil {
		t.Fatal(err)
	}
	verified, err := v.Verify(token, e, now)
	if err != nil {
		t.Fatal(err)
	}
	return accessFixture{now, c, e, key, s, v, metadata, token, verified}
}

func signWire(t *testing.T, key ed25519.PrivateKey, header, payload []byte) string {
	t.Helper()
	message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	return message + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(message)))
}
func standardHeader() []byte {
	return []byte(`{"alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original"}`)
}

// Test-only synthetic registry pin for wires the fixture itself constructs.
// Production Expected.TokenDigest MUST come from independent current storage,
// never from hashing the incoming caller's token as this adversarial setup does.
func syntheticRecordForTestWire(f accessFixture, token string) rentalaccess.Expected {
	e := f.expected
	h := sha256.Sum256([]byte(token))
	e.TokenDigest = hex.EncodeToString(h[:])
	return e
}
func claimBytes(t *testing.T, c rentalaccess.Claims) []byte {
	t.Helper()
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestRentalJWSBindsIndependentContextAndExpiry(t *testing.T) {
	f := fixture(t)
	if !f.verified.Allows("INVOKE", f.now) || f.verified.Allows("QUEUE_SELF", f.now) || f.verified.Allows("SHELL", f.now) || f.verified.Allows("INVOKE", f.now.Add(5*time.Minute)) {
		t.Fatal("scope/expiry escaped grant")
	}
	changes := map[string]func(*rentalaccess.Expected){
		"current-token-digest": func(e *rentalaccess.Expected) { e.TokenDigest = strings.Repeat("b", 64) },
		"missing-token-record": func(e *rentalaccess.Expected) { e.TokenDigest = "" },
		"issuer":               func(e *rentalaccess.Expected) { e.Issuer = "issuer_other" }, "subject": func(e *rentalaccess.Expected) { e.Subject = "usr_other" }, "audience": func(e *rentalaccess.Expected) { e.Audience = "endpoint_other" }, "revoked-grant": func(e *rentalaccess.Expected) { e.Grant = "grant_new" },
		"order": func(e *rentalaccess.Expected) { e.Order = "order_other" }, "session": func(e *rentalaccess.Expected) { e.Session = "session_other" }, "seller": func(e *rentalaccess.Expected) { e.Seller = "seller_other" }, "device": func(e *rentalaccess.Expected) { e.Device = "device_other" }, "resource": func(e *rentalaccess.Expected) { e.Resource = "resource_other" },
		"generation": func(e *rentalaccess.Expected) { e.Generation = "13" }, "renewed-terms": func(e *rentalaccess.Expected) { e.Terms = "4" }, "terms-hash": func(e *rentalaccess.Expected) { e.TermsHash = strings.Repeat("b", 64) }, "environment": func(e *rentalaccess.Expected) { e.Environment = "environment_other" }, "environment-hash": func(e *rentalaccess.Expected) { e.EnvironmentHash = strings.Repeat("b", 64) }, "manifest": func(e *rentalaccess.Expected) { e.Manifest = "manifest_other" }, "manifest-hash": func(e *rentalaccess.Expected) { e.ManifestHash = strings.Repeat("b", 64) }, "hard-deadline": func(e *rentalaccess.Expected) { e.HardDeadline = f.now.Add(11 * time.Minute).Format(time.RFC3339Nano) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			e := f.expected
			change(&e)
			if _, err := f.verifier.Verify(f.credential, e, f.now); err == nil {
				t.Fatal("accepted another current session context")
			}
		})
	}
	for _, at := range []time.Time{f.now.Add(-time.Nanosecond), f.now.Add(5 * time.Minute), f.now.Add(time.Hour), {}} {
		if _, e := f.verifier.Verify(f.credential, f.expected, at); e == nil {
			t.Fatal("accepted outside grant time")
		}
	}
	// A separate RFC-compatible JWS encoder, different property order/spacing.
	var fields map[string]any
	if json.Unmarshal(claimBytes(t, f.claims), &fields) != nil {
		t.Fatal("fixture")
	}
	b, e := json.MarshalIndent(fields, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	reordered := signWire(t, f.key, standardHeader(), b)
	if _, e = f.verifier.Verify(reordered, f.expected, f.now); e == nil {
		t.Fatal("a different credential reused an original grant record")
	}
	if _, e = f.verifier.Verify(reordered, syntheticRecordForTestWire(f, reordered), f.now); e != nil {
		t.Fatal("valid reordered JWS rejected", e)
	}
	copy := f.verified.Details()
	copy.Scopes[0] = "SHELL"
	if !f.verified.Allows("INVOKE", f.now) {
		t.Fatal("claims alias escaped")
	}
}

func TestRentalJWSRejectsAuthenticatedAmbiguityAndAlgorithmConfusion(t *testing.T) {
	f := fixture(t)
	payload := claimBytes(t, f.claims)
	badHeaders := [][]byte{
		[]byte(`{"alg":"none","typ":"p11-rental-access+jwt","kid":"issuer_key_original"}`),
		[]byte(`{"alg":"HS256","typ":"p11-rental-access+jwt","kid":"issuer_key_original"}`),
		[]byte(`{"alg":"EdDSA","typ":"JWT","kid":"issuer_key_original"}`),
		[]byte(`{"alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_other"}`),
		[]byte(`{"alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original","jku":"https://untrusted.invalid/key"}`),
		[]byte(`{"alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original","x5u":"https://untrusted.invalid/cert"}`),
		[]byte(`{"alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original","crit":[]}`),
		[]byte(`{"alg":"EdDSA","alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original"}`),
		[]byte(`{"Alg":"EdDSA","typ":"p11-rental-access+jwt","kid":"issuer_key_original"}`),
		[]byte(`{"alg":"EdDSA","typ":null,"kid":"issuer_key_original"}`),
	}
	for n, h := range badHeaders {
		t.Run(fmt.Sprint("header-", n), func(t *testing.T) {
			token := signWire(t, f.key, h, payload)
			if _, e := f.verifier.Verify(token, syntheticRecordForTestWire(f, token), f.now); e == nil {
				t.Fatal("accepted authenticated invalid header")
			}
		})
	}
	badPayloads := [][]byte{
		bytes.Replace(payload, []byte(`"iat":1793923200`), []byte(`"iat":1793923200.0`), 1),
		bytes.Replace(payload, []byte(`"nbf":1793923200`), []byte(`"nbf":1.7939232e9`), 1),
		bytes.Replace(payload, []byte(`"aud":"endpoint_phone"`), []byte(`"aud":["endpoint_phone"]`), 1),
		bytes.Replace(payload, []byte(`"iat":1793923200`), []byte(`"iat":"1793923200"`), 1),
		bytes.Replace(payload, []byte(`"terms_revision":"3"`), []byte(`"terms_revision":"03"`), 1),
		bytes.Replace(payload, []byte(`"resource_lock_generation":"12"`), []byte(`"resource_lock_generation":"0"`), 1),
		bytes.Replace(payload, []byte(`"scope":["INVOKE","CANCEL_SELF","EXPORT_SELF"]`), []byte(`"scope":["INVOKE","INVOKE"]`), 1),
		bytes.Replace(payload, []byte(`"scope":["INVOKE","CANCEL_SELF","EXPORT_SELF"]`), []byte(`"scope":["SHELL"]`), 1),
		bytes.Replace(payload, []byte(`"privacy":"STANDARD"`), []byte(`"privacy":"ATTESTED_CONFIDENTIAL"`), 1),
		bytes.Replace(payload, []byte(`"mode":"GPU_RENTAL"`), []byte(`"mode":"PLAINTEXT"`), 1),
		append(append([]byte{}, payload[:len(payload)-1]...), []byte(`,"jti":"grant_original"}`)...),
		append(append([]byte{}, payload[:len(payload)-1]...), []byte(`,"unknown":true}`)...),
		bytes.Replace(payload, []byte(`"terms_hash":"`+strings.Repeat("a", 64)+`"`), []byte(`"terms_hash":null`), 1),
	}
	for n, b := range badPayloads {
		t.Run(fmt.Sprint("payload-", n), func(t *testing.T) {
			if bytes.Equal(payload, b) {
				t.Fatal("adversarial fixture did not change")
			}
			token := signWire(t, f.key, standardHeader(), b)
			if _, e := f.verifier.Verify(token, syntheticRecordForTestWire(f, token), f.now); e == nil {
				t.Fatal("accepted authenticated invalid claims")
			}
		})
	}
	parts := strings.Split(f.credential, ".")
	for _, token := range []string{f.credential + ".", strings.Join(parts[:2], "."), parts[0] + "=." + parts[1] + "." + parts[2], parts[0] + "." + parts[1] + "." + parts[2] + "=", strings.Repeat("a", 4097)} {
		if _, e := f.verifier.Verify(token, syntheticRecordForTestWire(f, token), f.now); e == nil {
			t.Fatal("accepted noncanonical token")
		}
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32))
	wrongKey := signWire(t, other, standardHeader(), payload)
	if _, e := f.verifier.Verify(wrongKey, syntheticRecordForTestWire(f, wrongKey), f.now); e == nil {
		t.Fatal("accepted other signing key")
	}
}

func TestRentalCredentialOnceAndProvisionedKeyOwnership(t *testing.T) {
	f := fixture(t)
	i, e := f.signer.Issue(f.claims, f.now)
	if e != nil {
		t.Fatal(e)
	}
	m := i.Metadata()
	encoded, e := json.Marshal(i)
	if e != nil {
		t.Fatal(e)
	}
	token, e := i.TakeCredential()
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{string(encoded), fmt.Sprint(i), fmt.Sprintf("%#v", i), fmt.Sprintf("%+v", i)} {
		if strings.Contains(text, token) || strings.Contains(text, strings.Split(token, ".")[2]) {
			t.Fatal("credential escaped metadata/formatting")
		}
	}
	h := sha256.Sum256([]byte(token))
	if m.Digest != hex.EncodeToString(h[:]) {
		t.Fatal("wrong digest")
	}
	if _, e = i.TakeCredential(); e == nil {
		t.Fatal("credential returned twice")
	}
	m.Claims.Scopes[0] = "SHELL"
	if i.Metadata().Claims.Scopes[0] != "INVOKE" {
		t.Fatal("metadata alias")
	}
	i.Close()
	key := append(ed25519.PrivateKey(nil), f.key...)
	s, e := rentalaccess.NewSigner(f.claims.Issuer, "issuer_key_original", key)
	if e != nil {
		t.Fatal(e)
	}
	clear(key)
	issued, e := s.Issue(f.claims, f.now)
	if e != nil {
		t.Fatal("signer borrowed caller key")
	}
	issued.Close()
	s.Close()
	if _, e = s.Issue(f.claims, f.now); e == nil {
		t.Fatal("closed signer issued")
	}
	bad := append(ed25519.PrivateKey(nil), f.key...)
	bad[63] ^= 1
	if _, e = rentalaccess.NewSigner(f.claims.Issuer, "issuer_key_original", bad); e == nil {
		t.Fatal("inconsistent private key")
	}
	pin := append(ed25519.PublicKey(nil), f.key.Public().(ed25519.PublicKey)...)
	v, e := rentalaccess.NewVerifier(map[string]ed25519.PublicKey{"issuer_key_original": pin})
	if e != nil {
		t.Fatal(e)
	}
	clear(pin)
	if _, e = v.Verify(f.credential, f.expected, f.now); e != nil {
		t.Fatal("verifier borrowed caller key")
	}
	v.Close()
	if _, e = v.Verify(f.credential, f.expected, f.now); e == nil {
		t.Fatal("closed verifier authorized")
	}
}
