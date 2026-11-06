// Package rentaltest supplies synthetic identities and metadata for source
// tests ONLY. Production code must never import it. No fixture qualifies an
// engine, GPU, readiness, payment, cleanup, E2E confidentiality or billing.
package rentaltest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/shared/contractspec"
)

const Body = `{"model":"public_model","messages":[{"role":"user","content":"private rental fixture"}],"max_tokens":32,"stream":true}`

type Fixture struct {
	Now        time.Time
	Prepared   rentalaccess.Prepared
	Policy     rentalaccess.TextPolicy
	Signer     *rentalaccess.Signer
	Verifier   *rentalaccess.Verifier
	Metadata   rentalaccess.Metadata
	Expected   rentalaccess.Expected
	Credential string
	Grant      rentalaccess.Verified
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func Text(t *testing.T, protocol relay.Profile) Fixture { return fixture(t, protocol, false) }
func Media(t *testing.T) Fixture                        { return fixture(t, relay.ChatText, true) }

// MediaProfile freezes a synthetic media dialect without qualifying any SDK.
func MediaProfile(t *testing.T, profile string) Fixture {
	return fixture(t, relay.Profile(profile), true)
}
func fixture(t *testing.T, protocol relay.Profile, media bool) Fixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	m := rentalaccess.Manifest{Profile: rentalaccess.ManifestProfile, Reference: "manifest_rental", Session: "session_rental", Order: "order_rental", Buyer: "buyer_test", Seller: "seller_test", Device: "device_test", Resource: "resource_rental", Generation: "1", Capability: "cap_native_text", Environment: "environment_rental", EnvironmentHash: strings.Repeat("c", 64), ContractHash: strings.Repeat("d", 64), Mode: "GPU_RENTAL", Privacy: "STANDARD", Engine: "LLAMA_CPP_NATIVE", Release: "llama.cpp-v0.8.0-8642541b", ModelHash: strings.Repeat("a", 64), RecipeHash: strings.Repeat("b", 64), Protocol: string(protocol), PublicSpec: contractspec.Selection{ModelID: "model_rental", DurationMS: 120000, InputTokenLimit: 1024, OutputTokenLimit: 32}}
	if media {
		m.Engine = "COMFYUI"
		m.Release = "comfy-private-2"
		m.PublicSpec = contractspec.Selection{WorkflowID: "workflow_media", WorkflowRevision: 1, DurationMS: 120000}
	}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	p, e := rentalaccess.VerifyPreparation(raw, rentalaccess.LocalApproval{Profile: rentalaccess.ApprovalProfile, Manifest: m, ManifestHash: digest(raw), PhysicalHash: strings.Repeat("e", 64), Connection: "connection_test", Issuer: "issuer_test", PrepareUntil: now.Add(time.Minute)}, now)
	if e != nil {
		t.Fatal(e)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{96}, ed25519.SeedSize))
	defer clear(key)
	s, e := rentalaccess.NewSigner("issuer_test", "key_test", key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	v, e := rentalaccess.NewVerifier(map[string]ed25519.PublicKey{"key_test": key.Public().(ed25519.PublicKey)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	c := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: "issuer_test", Subject: m.Buyer, Audience: m.Device, ID: "grant_rental", Issued: now.Unix(), NotBefore: now.Unix(), Expires: now.Add(2 * time.Minute).Unix(), Order: m.Order, Session: m.Session, Seller: m.Seller, Device: m.Device, Resource: m.Resource, Generation: m.Generation, Terms: "1", TermsHash: strings.Repeat("f", 64), Environment: m.Environment, EnvironmentHash: m.EnvironmentHash, Manifest: m.Reference, ManifestHash: p.Digest(), Scopes: []string{"INVOKE", "QUEUE_SELF", "CANCEL_SELF", "EXPORT_SELF"}, HardDeadline: now.Add(2 * time.Minute).Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
	issued, e := s.Issue(c, now)
	if e != nil {
		t.Fatal(e)
	}
	defer issued.Close()
	meta := issued.Metadata()
	token, e := issued.TakeCredential()
	if e != nil {
		t.Fatal(e)
	}
	expected := rentalaccess.Expected{TokenDigest: meta.Digest, Issuer: c.Issuer, Subject: c.Subject, Audience: c.Audience, Grant: c.ID, Order: c.Order, Session: c.Session, Seller: c.Seller, Device: c.Device, Resource: c.Resource, Generation: c.Generation, Terms: c.Terms, TermsHash: c.TermsHash, Environment: c.Environment, EnvironmentHash: c.EnvironmentHash, Manifest: c.Manifest, ManifestHash: c.ManifestHash, HardDeadline: c.HardDeadline}
	g, e := v.Verify(token, expected, now)
	if e != nil {
		t.Fatal(e)
	}
	return Fixture{Now: now, Prepared: p, Policy: rentalaccess.TextPolicy{Profile: rentalaccess.TextPolicyProfile, ModelRef: m.PublicSpec.ModelID, PublicModel: "public_model", Protocol: protocol, InputTokenLimit: 1024, OutputTokenLimit: 32}, Signer: s, Verifier: v, Metadata: meta, Expected: expected, Credential: token, Grant: g}
}

func (f Fixture) Input(t *testing.T, id, local string) *rentalaccess.TextInput {
	t.Helper()
	i, e := rentalaccess.SealTextInput(f.Prepared, f.Verifier, f.Credential, f.Expected, id, local, f.Policy, []byte(Body), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(i.Close)
	return i
}

// Start uses the actual native metadata schema/gate. Merely defining this test
// helper does not execute SQLite or enter the post-code validation stage.
func (f Fixture) Start(t *testing.T, allow bool) (*execution.Store, execution.Hold) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rental.db")
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	s, e := execution.Open(ctx, path, execution.Binding{Installation: strings.Repeat("1", 64), LedgerID: strings.Repeat("2", 64), Device: f.Expected.Device, Seller: f.Expected.Seller}, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if e = s.RegisterResource(ctx, f.Expected.Resource, f.Prepared.Approval().PhysicalHash); e != nil {
		t.Fatal(e)
	}
	h, d, e := s.ReceiveRentalPreparation(ctx, "prepare_rental", f.Prepared)
	if e != nil || d != "PREPARATION_RESERVED" {
		t.Fatal(d, e)
	}
	if e = s.BindRentalGrant(ctx, "bind_rental", h, f.Prepared, f.Grant, f.Expected); e != nil {
		t.Fatal(e)
	}
	if allow {
		reply, e := f.Signer.SignControl(f.Metadata, 1, "ALLOW", time.Now().UTC(), time.Now().UTC().Add(30*time.Second))
		if e != nil {
			t.Fatal(e)
		}
		wire, e := json.Marshal(reply)
		if e != nil {
			t.Fatal(e)
		}
		proof, e := f.Verifier.VerifyControl(wire, f.Grant, f.Expected, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if e = s.ApplyRentalControl(ctx, h, proof); e != nil {
			t.Fatal(e)
		}
	}
	return s, h
}
