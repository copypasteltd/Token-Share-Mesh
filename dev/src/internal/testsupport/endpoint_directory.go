package testsupport

// Synthetic public-metadata fixtures. Random 32-byte content public material
// is NOT an X25519 possession, native AEAD, device or execution qualification.
import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/ids"
	"strconv"
	"testing"
	"time"
)

type PublicEndpointFixture struct {
	Bundle   endpointwire.Bundle
	Identity ed25519.PrivateKey
}

func FixturePublicKey(prefix, algorithm string, raw []byte) endpointwire.PublicKey {
	return endpointwire.PublicKey{ID: ids.New(prefix), Algorithm: algorithm, Public: base64.RawURLEncoding.EncodeToString(raw), Fingerprint: endpointwire.Hash(raw)}
}
func SignPublicEndpoint(t testing.TB, f *PublicEndpointFixture) {
	t.Helper()
	msg, e := f.Bundle.SigningMessage()
	if e != nil {
		t.Fatal(e)
	}
	f.Bundle.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.Identity, msg))
}
func PublicEndpoint(t testing.TB, owner, endpoint, role string, version int, now, expires time.Time) PublicEndpointFixture {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	content := make([]byte, 32)
	if _, e = rand.Read(content); e != nil {
		t.Fatal(e)
	}
	f := PublicEndpointFixture{Identity: priv, Bundle: endpointwire.Bundle{Profile: endpointwire.BundleProfile, Owner: owner, Endpoint: endpoint, Role: role, Version: strconv.Itoa(version), Identity: FixturePublicKey("identity", "Ed25519", pub), Content: FixturePublicKey("recipient", "X25519_SEALED_BOX", content), NotBefore: now.UTC().Format(time.RFC3339Nano), Expires: expires.UTC().Format(time.RFC3339Nano), SignatureProfile: endpointwire.SignatureProfile}}
	if role == "BUYER" {
		p, _, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		k := FixturePublicKey("receipt", "Ed25519", p)
		f.Bundle.Receipt = &k
	}
	SignPublicEndpoint(t, &f)
	return f
}
func SignPublicRotation(t testing.TB, old PublicEndpointFixture, next endpointwire.Bundle, now time.Time) endpointwire.Transition {
	t.Helper()
	r := endpointwire.Transition{Profile: "P11_ENDPOINT_ROTATION_1", Previous: old.Bundle.Hash(), Next: next, Recorded: now.UTC().Format(time.RFC3339Nano)}
	msg, e := r.SigningMessage()
	if e != nil {
		t.Fatal(e)
	}
	r.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(old.Identity, msg))
	return r
}
func RotatePublicEndpoint(t testing.TB, old PublicEndpointFixture, now, expires time.Time) (PublicEndpointFixture, endpointwire.Transition) {
	t.Helper()
	n, e := strconv.Atoi(old.Bundle.Version)
	if e != nil {
		t.Fatal(e)
	}
	next := PublicEndpoint(t, old.Bundle.Owner, old.Bundle.Endpoint, old.Bundle.Role, n+1, now, expires)
	return next, SignPublicRotation(t, old, next.Bundle, now)
}
