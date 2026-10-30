package executionlease

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sample() (Lease, Plan, ed25519.PrivateKey, time.Time) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	l := Lease{ID: "lease_test", Job: "job_test", Attempt: "attempt_test", Order: "order_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "18446744073709551615", Capability: "cap_test", Manifest: "manifest_test", ManifestHash: strings.Repeat("a", 64), Environment: strings.Repeat("b", 64), Revision: 1, Issued: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano), Fence: "9007199254740993", Expires: now.Add(45 * time.Second).Format(time.RFC3339Nano), ContractHash: strings.Repeat("c", 64), InputAssets: []string{"asset_one", "asset_two"}, Envelope: "envelope_test", Profile: Profile}
	p := Plan{Job: l.Job, Attempt: l.Attempt, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Capability: l.Capability, Manifest: l.Manifest, ManifestHash: l.ManifestHash, Environment: l.Environment, Fence: l.Fence, ContractHash: l.ContractHash, HardDeadline: now.Add(time.Minute)}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{29}, 32)) // synthetic, never a runtime trust anchor
	return l, p, key, now
}
func signed(t *testing.T, l Lease, key ed25519.PrivateKey) []byte {
	t.Helper()
	m, e := Message(l)
	if e != nil {
		t.Fatal(e)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, m))
	b, _ := json.Marshal(l)
	return b
}
func TestExecutionLeaseIndependentSignatureVector(t *testing.T) {
	l, p, k, now := sample()
	// Independent literal vector: changing the production field order/domain
	// cannot change the expected bytes along with the implementation.
	want := "P11_EXECUTION_LEASE_ED25519_1\nlease_test\njob_test\nattempt_test\norder_test\nseller_test\ndevice_test\nresource_test\n18446744073709551615\ncap_test\nmanifest_test\n" + strings.Repeat("a", 64) + "\n" + strings.Repeat("b", 64) + "\n1\n2026-10-30T00:00:00Z\n2026-10-30T00:00:00Z\n2026-10-30T00:01:00Z\n9007199254740993\n2026-10-30T00:00:45Z\n" + strings.Repeat("c", 64) + "\n2\nasset_one\nasset_two\nenvelope_test\n"
	got, e := Message(l)
	if e != nil || string(got) != want {
		t.Fatal("independent lease vector differs", e)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(k, []byte(want)))
	raw, _ := json.Marshal(l)
	v, e := Verify(raw, k.Public().(ed25519.PublicKey), p, now)
	if e != nil || !v.ValidAt(now) {
		t.Fatal("independent signature rejected", e)
	}
	copy := v.Details()
	copy.InputAssets[0] = "asset_mutation"
	if v.Details().InputAssets[0] != "asset_one" {
		t.Fatal("verified data is externally mutable")
	}
}
func TestExecutionLeaseRejectsAmbiguityTamperAndWrongPlan(t *testing.T) {
	l, p, k, now := sample()
	raw := signed(t, l, k)
	pub := k.Public().(ed25519.PublicKey)
	for name, change := range map[string]func(*Plan){
		"seller": func(p *Plan) { p.Seller = "seller_other" }, "device": func(p *Plan) { p.Device = "device_other" }, "resource": func(p *Plan) { p.Resource = "resource_other" }, "generation": func(p *Plan) { p.Generation = "1" }, "fence": func(p *Plan) { p.Fence = "1" }, "job": func(p *Plan) { p.Job = "job_other" }, "attempt": func(p *Plan) { p.Attempt = "attempt_other" }, "order": func(p *Plan) { p.Order = "order_other" }, "capability": func(p *Plan) { p.Capability = "cap_other" }, "manifest": func(p *Plan) { p.Manifest = "manifest_other" }, "manifest_hash": func(p *Plan) { p.ManifestHash = strings.Repeat("d", 64) }, "environment": func(p *Plan) { p.Environment = strings.Repeat("d", 64) }, "contract": func(p *Plan) { p.ContractHash = strings.Repeat("d", 64) }, "deadline": func(p *Plan) { p.HardDeadline = p.HardDeadline.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			before := p
			change(&before)
			if _, e := Verify(raw, pub, before, now); e == nil {
				t.Fatal("wrong approved plan accepted")
			}
		})
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"resource_test"`), []byte(`"resource_other"`), 1),
		bytes.Replace(raw, []byte(`"lease_id":`), []byte(`"Lease_ID":`), 1),
		append([]byte(`{"prompt":"SYNTHETIC_CONTENT_MUST_NEVER_ENTER_INBOX",`), raw[1:]...),
		append([]byte(`{"lease_id":"lease_other",`), raw[1:]...),
		bytes.Replace(raw, []byte(`["asset_one","asset_two"]`), []byte(`null`), 1),
		bytes.Replace(raw, []byte(`18446744073709551615`), []byte(`18446744073709551616`), 1),
		append(bytes.Clone(raw), []byte(`{}`)...), bytes.Repeat([]byte{'x'}, MaximumBody+1),
	} {
		if _, e := Verify(bad, pub, p, now); e == nil {
			t.Fatal("ambiguous or tampered lease accepted")
		}
	}
	for _, change := range []func(*Lease){func(l *Lease) { l.Generation = "01" }, func(l *Lease) { l.Fence = "0" }, func(l *Lease) { l.Profile = "other_profile" }, func(l *Lease) { l.InputAssets = []string{"asset_one", "asset_one"} }, func(l *Lease) { l.Issued = "2026-10-30T00:00:00+00:00" }, func(l *Lease) { l.Expires = now.Add(46 * time.Second).Format(time.RFC3339Nano) }, func(l *Lease) { l.Revision = 2147483648 }, func(l *Lease) { l.Envelope = "file:///secret" }} {
		before := l
		change(&before)
		if _, e := Message(before); e == nil {
			t.Fatal("invalid lease entered signature domain")
		}
	}
}
func TestExecutionLeaseFreshnessAndZeroTrustFailClosed(t *testing.T) {
	l, p, k, now := sample()
	raw := signed(t, l, k)
	pub := k.Public().(ed25519.PublicKey)
	for _, at := range []time.Time{now.Add(-time.Nanosecond), now.Add(45 * time.Second), now.Add(time.Hour)} {
		if _, e := Verify(raw, pub, p, at); e == nil {
			t.Fatal("future/expired lease accepted")
		}
	}
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{30}, 32)).Public().(ed25519.PublicKey)
	for _, key := range []ed25519.PublicKey{nil, wrong} {
		if _, e := Verify(raw, key, p, now); e == nil {
			t.Fatal("missing/wrong trust anchor accepted")
		}
	}
	var zero Verified
	if zero.ValidAt(now) || len(zero.Metadata()) != 0 {
		t.Fatal("zero verified value grants authority")
	}
}
