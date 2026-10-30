package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
	"time"
)

func delivery(t *testing.T, f claimFixture, inv *invocation.Service, keys map[string][]byte) *invocation.Delivery {
	t.Helper()
	d, e := invocation.NewDelivery(invocation.DeliveryConfig{Claims: f.claims, Invocations: inv, InputKeys: keys})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(d.Close)
	return d
}
func originalDelivery(t *testing.T, f claimFixture) *invocation.Delivery {
	return delivery(t, f, f.config.Invocations, f.config.InputKeys)
}
func approvedOriginal(t *testing.T, f claimFixture) devicecontrol.Approval {
	t.Helper()
	var c trade.Contract
	if e := json.Unmarshal(f.f.order.Contract, &c); e != nil {
		t.Fatal(e)
	}
	a := devicecontrol.Approval{Order: f.f.order.ID, Route: f.buyerKey.RouteIDs[0], Mode: c.Policy.Mode, Privacy: c.Policy.Privacy, Seller: f.p.UserID, Device: f.p.DeviceID, Resource: f.f.offer.Resource, Capability: f.in.Capability, ContractHash: c.Hash,
		Spec: executionlease.Spec{Protocol: f.request.Protocol, Model: f.request.Model, MaxOutputTokens: f.request.MaxOutputTokens, Stream: f.request.Stream}, InputTokenLimit: c.Spec.InputTokenLimit}
	// Independently approved fixture catalog/order facts, never the response.
	if e := db.Pool.QueryRow(ctx, `SELECT kernel_environment_ref,kernel_environment_digest,engine_kind,engine_release,model_digest,recipe_digest FROM p11.capabilities WHERE id=$1`, f.in.Capability).Scan(&a.EnvironmentRef, &a.EnvironmentDigest, &a.EngineKind, &a.EngineRelease, &a.ModelDigest, &a.RecipeDigest); e != nil {
		t.Fatal(e)
	}
	return a
}

func TestExecutionDeliveryThreeProfilesReturnExactOriginalBytesWithoutIssuer(t *testing.T) {
	requireDB(t)
	for _, profile := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		t.Run(string(profile), func(t *testing.T) {
			f := newClaimFixtureProtocol(t, profile)
			d := originalDelivery(t, f)
			if _, e := f.claim(); e != nil {
				t.Fatal(e)
			}
			f.issuer.Close()
			for i := 0; i < 2; i++ {
				m, e := f.claims.Manifest(ctx, f.p, f.issued.LeaseID)
				if e != nil || m.Digest() != f.issued.Manifest.Digest() {
					t.Fatal("original manifest", e)
				}
				got, e := d.Input(ctx, f.p, f.issued.LeaseID)
				if e != nil {
					t.Fatal(e)
				}
				_, want := executionBody(profile, f.request.IdempotencyKey)
				if !bytes.Equal(got, want) {
					t.Fatal("original bytes changed")
				}
				clear(got)
			}
			assertClaimHold(t, f, 1, 1)
		})
	}
}

func TestExecutionDeliveryRequiresExistingOriginalClaimAndDevice(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	d := originalDelivery(t, f)
	for _, p := range []identity.Principal{f.p, f.buyerKey, f.f.offer.Seller} {
		if m, e := f.claims.Manifest(ctx, p, f.issued.LeaseID); e == nil || m.ID != "" {
			t.Fatal("unclaimed or non-device manifest")
		}
		if b, e := d.Input(ctx, p, f.issued.LeaseID); e == nil || len(b) != 0 {
			t.Fatal("unclaimed or non-device input")
		}
	}
	if _, e := f.claim(); e != nil {
		t.Fatal(e)
	}
	wrong := f.p
	wrong.DeviceID = ids.New("other")
	for _, p := range []identity.Principal{wrong, {UserID: f.p.UserID, Kind: identity.Device, DeviceID: f.p.DeviceID, Version: 1, Scopes: []string{"device:read"}}} {
		if _, e := d.Input(ctx, p, f.issued.LeaseID); e == nil {
			t.Fatal("wrong device/scope")
		}
	}
	if _, e := d.Input(ctx, f.p, ids.New("lease")); fault.Public(e).Code != fault.NotFound {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionDeliveryEveryReadRechecksCurrentOriginalAuthority(t *testing.T) {
	requireDB(t)
	for _, kind := range []string{"version", "key", "funds", "qualification", "input", "resource", "generation", "lease"} {
		t.Run(kind, func(t *testing.T) {
			f := newClaimFixture(t)
			d := originalDelivery(t, f)
			if _, e := f.claim(); e != nil {
				t.Fatal(e)
			}
			if b, e := d.Input(ctx, f.p, f.issued.LeaseID); e != nil {
				t.Fatal(e)
			} else {
				clear(b)
			}
			switch kind {
			case "version":
				exec(t, `UPDATE p11.devices SET version=version+1 WHERE id=$1`, f.in.Device)
			case "key":
				exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, f.buyerKey.KeyID)
			case "funds":
				exec(t, `UPDATE p11.payments SET funds_expires_at=clock_timestamp()+interval '1 second' WHERE order_id=$1`, f.f.order.ID)
			case "qualification":
				exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, f.f.offer.Gate)
			case "input":
				exec(t, `UPDATE p11.assets SET state='QUARANTINED',version=version+1 WHERE job_id=$1`, f.issued.Preparation.JobID)
			case "resource":
				exec(t, `UPDATE p11.resource_locks SET phase='QUARANTINED' WHERE resource_id=$1`, f.f.offer.Resource)
			case "generation":
				exec(t, `UPDATE p11.resource_locks SET lock_generation=lock_generation+1 WHERE resource_id=$1`, f.f.offer.Resource)
			case "lease":
				exec(t, `UPDATE p11.execution_leases SET state='REVOKED' WHERE id=$1`, f.issued.LeaseID)
			}
			if m, e := f.claims.Manifest(ctx, f.p, f.issued.LeaseID); e == nil || m.ID != "" {
				t.Fatal("stale manifest disclosed")
			}
			if b, e := d.Input(ctx, f.p, f.issued.LeaseID); e == nil || len(b) != 0 {
				t.Fatal("stale input disclosed")
			}
		})
	}
}

func TestExecutionDeliveryMissingWrongOrClosedPurposeKeysFailWithoutBytes(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	if _, e := f.claim(); e != nil {
		t.Fatal(e)
	}
	for _, keys := range []map[string][]byte{{"other_version": bytes.Repeat([]byte{41}, 32)}, {f.config.InputVersion: bytes.Repeat([]byte{42}, 32)}} {
		d := delivery(t, f, f.config.Invocations, keys)
		if b, e := d.Input(ctx, f.p, f.issued.LeaseID); e == nil || len(b) != 0 {
			t.Fatal("missing/wrong input key")
		}
	}
	wrongFP := invocationKeys(t, "fp_one", map[string][]byte{"fp_one": bytes.Repeat([]byte{43}, 32)})
	d := delivery(t, f, wrongFP, f.config.InputKeys)
	if b, e := d.Input(ctx, f.p, f.issued.LeaseID); e == nil || len(b) != 0 {
		t.Fatal("original HMAC mismatch returned body")
	}
	d = originalDelivery(t, f)
	d.Close()
	if b, e := d.Input(ctx, f.p, f.issued.LeaseID); fault.Public(e).Code != fault.Unavailable || len(b) != 0 {
		t.Fatal("closed input keys", e)
	}
	d = originalDelivery(t, f)
	f.config.Invocations.Close()
	if b, e := d.Input(ctx, f.p, f.issued.LeaseID); fault.Public(e).Code != fault.Unavailable || len(b) != 0 {
		t.Fatal("closed fingerprint keys", e)
	}
	// Metadata service remains verifier-only and independent of both purpose keys.
	if _, e := f.claims.Manifest(ctx, f.p, f.issued.LeaseID); e != nil {
		t.Fatal(e)
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionDeliveryOwnsKeyCopiesAndRejectsCrossDomainConfiguration(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	keys := map[string][]byte{}
	for id, k := range f.config.InputKeys {
		keys[id] = bytes.Clone(k)
	}
	d := delivery(t, f, f.config.Invocations, keys)
	for _, k := range keys {
		clear(k)
	}
	if _, e := f.claim(); e != nil {
		t.Fatal(e)
	}
	if b, e := d.Input(ctx, f.p, f.issued.LeaseID); e != nil {
		t.Fatal(e)
	} else {
		clear(b)
	}
	for _, cfg := range []invocation.DeliveryConfig{{}, {Claims: f.claims, Invocations: f.config.Invocations}, {Claims: f.claims, Invocations: f.config.Invocations, InputKeys: map[string][]byte{"bad": {1}}}, {Claims: f.claims, Invocations: f.config.Invocations, InputKeys: map[string][]byte{"same": []byte(strings.Repeat("f", 32))}}} {
		if s, e := invocation.NewDelivery(cfg); e == nil || s != nil {
			t.Fatal("bad purpose configuration")
		}
	}
}

func TestExecutionDeliveryIndependentApprovalPinsAndNativeInputAcceptance(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	d := originalDelivery(t, f)
	approved := approvedOriginal(t, f)
	c, e := f.claim()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c.Lease)
	m, e := f.claims.Manifest(ctx, f.p, f.issued.LeaseID)
	if e != nil {
		t.Fatal(e)
	}
	meta, _ := json.Marshal(m)
	claim, manifest := devicecontrol.Reply{Status: 200, Body: raw}, devicecontrol.Reply{Status: 200, Body: meta}
	pin := f.config.SigningPrivateKey.Public().(ed25519.PublicKey)
	accepted, e := devicecontrol.AcceptDelivery(claim, manifest, pin, approved, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	body, e := d.Input(ctx, f.p, f.issued.LeaseID)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(body)
	got, e := accepted.AcceptInput(devicecontrol.Reply{Status: 200, Body: body}, time.Now())
	if e != nil || !bytes.Equal(got, body) {
		t.Fatal(e)
	}
	clear(got)
	copy := accepted.Manifest()
	copy.Spec.Model = "altered"
	if accepted.Manifest().Spec.Model != approved.Spec.Model {
		t.Fatal("mutable accepted manifest")
	}
	for _, bad := range []devicecontrol.Approval{{}, func() devicecontrol.Approval { a := approved; a.Privacy = "PLATFORM_BLIND"; return a }(), func() devicecontrol.Approval { a := approved; a.Route = ids.New("other"); return a }(), func() devicecontrol.Approval { a := approved; a.Device = ids.New("other"); return a }(), func() devicecontrol.Approval { a := approved; a.EngineRelease = "unapproved"; return a }(), func() devicecontrol.Approval { a := approved; a.Spec.MaxOutputTokens++; return a }()} {
		if _, e := devicecontrol.AcceptDelivery(claim, manifest, pin, bad, time.Now()); e == nil {
			t.Fatal("approval copied or omitted")
		}
	}
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32)).Public().(ed25519.PublicKey)
	if _, e := devicecontrol.AcceptDelivery(claim, manifest, wrong, approved, time.Now()); e == nil {
		t.Fatal("wrong independent pin")
	}
	m.InputBytes++
	bad, _ := json.Marshal(m)
	if _, e := devicecontrol.AcceptDelivery(claim, devicecontrol.Reply{Status: 200, Body: bad}, pin, approved, time.Now()); e == nil {
		t.Fatal("tampered signed manifest hash")
	}
	assertClaimHold(t, f, 1, 1)
}

func TestExecutionDeliveryNativeInputRejectsExpiryAmbiguousProtocolAndOversize(t *testing.T) {
	requireDB(t)
	f := newClaimFixture(t)
	approved := approvedOriginal(t, f)
	c, e := f.claim()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c.Lease)
	meta, _ := json.Marshal(f.issued.Manifest)
	a, e := devicecontrol.AcceptDelivery(devicecontrol.Reply{Status: 200, Body: raw}, devicecontrol.Reply{Status: 200, Body: meta}, f.config.SigningPrivateKey.Public().(ed25519.PublicKey), approved, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	_, body := executionBody(f.request.Protocol, f.request.IdempotencyKey)
	for _, r := range []devicecontrol.Reply{{Status: 200, Body: []byte(`{"model":"model_nb1","model":"bad"}`)}, {Status: 200, Body: bytes.Repeat([]byte{' '}, 262145)}, {Status: 503, Body: body}, {Status: 200, Body: bytes.Replace(body, []byte("\"model_nb1\""), []byte("\"model_bad\""), 1)}} {
		if b, e := a.AcceptInput(r, time.Now()); e == nil || len(b) != 0 {
			t.Fatal("invalid content accepted")
		}
	}
	if b, e := a.AcceptInput(devicecontrol.Reply{Status: 200, Body: body}, time.Now().Add(time.Minute)); e == nil || len(b) != 0 {
		t.Fatal("expired lease accepted")
	}
	var zero devicecontrol.AcceptedDelivery
	if _, e := zero.AcceptInput(devicecontrol.Reply{Status: 200, Body: body}, time.Now()); e == nil {
		t.Fatal("zero receiver accepted input")
	}
}
