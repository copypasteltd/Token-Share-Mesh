package endpoint

// SOURCE_ONLY. The test-only X25519 fixture supplies synthetic public metadata;
// it is not a replacement cipher implementation or proof of native interop.
import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryJournal struct {
	raw  []byte
	fail bool
}

func (m *memoryJournal) Read() ([]byte, error) {
	if m.raw == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(m.raw), nil
}
func (m *memoryJournal) Commit(raw []byte) error {
	if m.fail {
		return ErrState
	}
	m.raw = bytes.Clone(raw)
	return nil
}
func testVault(t *testing.T) (*Vault, *memoryJournal) {
	t.Helper()
	j := &memoryJournal{}
	v, e := OpenOwned(j, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	return v, j
}
func syntheticLocal(t *testing.T, role string, index byte, now time.Time) localKey {
	t.Helper()
	k := localKey{Status: "ACTIVE", IdentitySeed: bytes.Repeat([]byte{index}, 32), ContentSecret: bytes.Repeat([]byte{index + 30}, 32)}
	i := ed25519.NewKeyFromSeed(k.IdentitySeed)
	defer clear(i)
	c, e := ecdh.X25519().NewPrivateKey(k.ContentSecret)
	if e != nil {
		t.Fatal(e)
	}
	suffix := strconv.Itoa(int(index))
	k.Bundle = Bundle{Profile: BundleProfile, Owner: "owner_" + suffix, Endpoint: "endpoint_" + suffix, Role: role, Version: "1",
		Identity:  key("sign_"+suffix, "Ed25519", i.Public().(ed25519.PublicKey)),
		Content:   key("recv_"+suffix, "X25519_SEALED_BOX", c.PublicKey().Bytes()),
		NotBefore: now.Add(-time.Minute).UTC().Format(time.RFC3339Nano), Expires: now.Add(time.Hour).UTC().Format(time.RFC3339Nano), SignatureProfile: SignatureProfile}
	if role == "BUYER" {
		k.ReceiptSeed = bytes.Repeat([]byte{index + 60}, 32)
		r := ed25519.NewKeyFromSeed(k.ReceiptSeed)
		rp := key("receipt_"+suffix, "Ed25519", r.Public().(ed25519.PublicKey))
		clear(r)
		k.Bundle.Receipt = &rp
	}
	resignSynthetic(t, &k)
	return k
}
func resignSynthetic(t *testing.T, k *localKey) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(k.IdentitySeed)
	defer clear(priv)
	msg, e := k.Bundle.message()
	if e != nil {
		t.Fatal(e)
	}
	k.Bundle.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, msg))
	if k.Bundle.Validate() != nil {
		t.Fatal("synthetic public bundle invalid")
	}
}
func installSynthetic(t *testing.T, v *Vault, k localKey) {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state.Local = append(v.state.Local, k)
	if v.saveLocked() != nil {
		t.Fatal("synthetic journal not persisted")
	}
}
func TestEndpointVaultRoundTripBindsOriginalMasterAndInstallation(t *testing.T) {
	now := time.Now().UTC()
	v, j := testVault(t)
	k := syntheticLocal(t, "BUYER", 2, now)
	installSynthetic(t, v, k)
	if bytes.Contains(j.raw, k.IdentitySeed) || bytes.Contains(j.raw, []byte(base64.StdEncoding.EncodeToString(k.ContentSecret))) {
		t.Fatal("private material escaped encrypted journal")
	}
	v.Close()
	for _, wrong := range []struct {
		key     []byte
		binding string
	}{
		{bytes.Repeat([]byte{18}, 32), strings.Repeat("a", 64)},
		{bytes.Repeat([]byte{19}, 32), strings.Repeat("b", 64)},
	} {
		if _, e := OpenOwned(j, wrong.key, wrong.binding, false); !errors.Is(e, ErrState) {
			t.Fatal("foreign original identity accepted", e)
		}
	}
	reopened, e := OpenOwned(j, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	pub, e := reopened.Public()
	if e != nil || len(pub) != 1 || pub[0].Bundle.Hash() != k.Bundle.Hash() {
		t.Fatal("original public identity not recovered", e)
	}
	pub[0].Bundle.Receipt.ID = "mutated_receipt"
	again, _ := reopened.Public()
	if again[0].Bundle.Receipt.ID == "mutated_receipt" {
		t.Fatal("caller changed stored public bundle")
	}
}
func TestEndpointVaultMissingOrCorruptStateCannotRecreateKeys(t *testing.T) {
	v, j := testVault(t)
	v.Close()
	j.raw[len(j.raw)-1] ^= 1
	if _, e := OpenOwned(j, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), false); !errors.Is(e, ErrState) {
		t.Fatal("corrupt journal accepted", e)
	}
	if _, e := OpenOwned(j, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), true); !errors.Is(e, ErrState) {
		t.Fatal("existing journal overwritten", e)
	}
	if _, e := OpenOwned(&memoryJournal{}, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), false); !errors.Is(e, ErrState) {
		t.Fatal("missing original journal regenerated", e)
	}
}
func TestEndpointInitialTrustNeedsIndependentFingerprint(t *testing.T) {
	now := time.Now().UTC()
	v, _ := testVault(t)
	peer := syntheticLocal(t, "SELLER", 3, now)
	proof := Verification{"OUT_OF_BAND", peer.Bundle.Identity.Fingerprint, "verified_reference"}
	for _, p := range []Verification{{"PLATFORM_PAGE", proof.Fingerprint, proof.Reference}, {"OUT_OF_BAND", strings.Repeat("b", 64), proof.Reference}, {"OUT_OF_BAND", proof.Fingerprint, "https://untrusted.invalid"}} {
		if v.PinInitial(peer.Bundle, p, now) == nil {
			t.Fatal("unverified first identity pinned")
		}
	}
	if e := v.PinInitial(peer.Bundle, proof, now); e != nil {
		t.Fatal(e)
	}
	peer.Bundle.Owner = "other_owner"
	if v.PinInitial(peer.Bundle, proof, now) == nil {
		t.Fatal("self signature did not bind owner")
	}
}
func TestEndpointKeyUseSeparatesIdentityContentAndBuyerReceipt(t *testing.T) {
	now := time.Now().UTC()
	v, _ := testVault(t)
	k := syntheticLocal(t, "BUYER", 4, now)
	installSynthetic(t, v, k)
	for _, purpose := range []string{"SIGN_STREAM", "OPEN_CONTENT", "SIGN_RECEIPT"} {
		scope := Scope{Owner: k.Bundle.Owner, Endpoint: k.Bundle.Endpoint, Role: "BUYER", BundleHash: k.Bundle.Hash(), Purpose: purpose}
		var captured *Material
		e := v.Use(context.Background(), scope, now, func(_ context.Context, m *Material) error {
			captured = m
			if purpose == "OPEN_CONTENT" {
				if len(m.Signing) != 0 || !bytes.Equal(m.ContentSecret, k.ContentSecret) {
					t.Error("key purpose mixed")
				}
			} else {
				fp := k.Bundle.Identity.Fingerprint
				if purpose == "SIGN_RECEIPT" {
					fp = k.Bundle.Receipt.Fingerprint
				}
				if len(m.ContentSecret) != 0 || Hash(m.Signing.Public().(ed25519.PublicKey)) != fp {
					t.Error("signing key purpose mixed")
				}
			}
			return nil
		})
		if e != nil || len(captured.Signing)+len(captured.ContentSecret) != 0 {
			t.Fatal("temporary key use not cleared", e)
		}
	}
}
func TestEndpointCloseCancelsAndJoinsOwnedKeyUse(t *testing.T) {
	now := time.Now().UTC()
	v, _ := testVault(t)
	k := syntheticLocal(t, "BUYER", 5, now)
	installSynthetic(t, v, k)
	started, canceled, resume, returned, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(unblock)
	go func() {
		defer close(returned)
		_ = v.Use(context.Background(), Scope{Owner: k.Bundle.Owner, Endpoint: k.Bundle.Endpoint, Role: "BUYER", BundleHash: k.Bundle.Hash(), Purpose: "SIGN_STREAM"}, now, func(ctx context.Context, _ *Material) error {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-resume
			return ctx.Err()
		})
	}()
	<-started
	go func() { v.Close(); close(closed) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("owned key use was not canceled")
	}
	select {
	case <-closed:
		t.Fatal("OS master could close before original key use joined")
	default:
	}
	unblock()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("key closure did not join")
	}
	<-returned
}
func TestEndpointPeerRevocationCancelsAlreadyOwnedStream(t *testing.T) {
	now := time.Now().UTC()
	v, _ := testVault(t)
	local, peer := syntheticLocal(t, "BUYER", 6, now), syntheticLocal(t, "SELLER", 7, now)
	installSynthetic(t, v, local)
	if e := v.PinInitial(peer.Bundle, Verification{"OUT_OF_BAND", peer.Bundle.Identity.Fingerprint, "verified_peer"}, now); e != nil {
		t.Fatal(e)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	scope := Scope{Owner: local.Bundle.Owner, Endpoint: local.Bundle.Endpoint, Role: "BUYER", BundleHash: local.Bundle.Hash(), Purpose: "SIGN_STREAM", PeerBundleHash: peer.Bundle.Hash()}
	go func() {
		done <- v.Use(context.Background(), scope, now, func(ctx context.Context, _ *Material) error { close(started); <-ctx.Done(); return ctx.Err() })
	}()
	<-started
	if e := v.RevokePeer(peer.Bundle.Hash()); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked peer retained active key use")
	}
	if v.Use(context.Background(), scope, now, func(context.Context, *Material) error { t.Error("revoked peer reopened"); return nil }) == nil {
		t.Fatal("revoked peer reopened")
	}
}
func TestEndpointRotationRequiresOldSignatureAndPreservesOriginalHistory(t *testing.T) {
	now := time.Now().UTC()
	v, _ := testVault(t)
	old, next := syntheticLocal(t, "SELLER", 8, now), syntheticLocal(t, "SELLER", 9, now)
	next.Bundle.Owner = old.Bundle.Owner
	next.Bundle.Endpoint = old.Bundle.Endpoint
	next.Bundle.Version = "2"
	resignSynthetic(t, &next)
	if v.PinInitial(old.Bundle, Verification{"OUT_OF_BAND", old.Bundle.Identity.Fingerprint, "verified_rotation"}, now) != nil {
		t.Fatal("initial pin failed")
	}
	transition := Transition{Profile: "P11_ENDPOINT_ROTATION_1", Previous: old.Bundle.Hash(), Next: next.Bundle, Recorded: now.Format(time.RFC3339Nano)}
	msg, _ := transition.message()
	priv := ed25519.NewKeyFromSeed(old.IdentitySeed)
	transition.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, msg))
	clear(priv)
	bad := transition
	bad.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	if v.RotatePeer(bad, now) == nil {
		t.Fatal("rotation without old identity accepted")
	}
	if e := v.RotatePeer(transition, now); e != nil {
		t.Fatal(e)
	}
	if v.RotatePeer(bad, now) == nil {
		t.Fatal("idempotent rotation bypassed signature")
	}
	scope := PeerScope{Owner: old.Bundle.Owner, Endpoint: old.Bundle.Endpoint, Role: "SELLER", BundleHash: old.Bundle.Hash()}
	if _, e := v.Peer(scope, now); e == nil {
		t.Fatal("new task used retired key")
	}
	scope.AllowRetired = true
	if _, e := v.Peer(scope, now); e != nil {
		t.Fatal("original contract lost its retained key", e)
	}
}
func TestEndpointCommitFailureClosesAllFutureKeyUses(t *testing.T) {
	now := time.Now().UTC()
	v, j := testVault(t)
	peer := syntheticLocal(t, "SELLER", 10, now)
	j.fail = true
	if v.PinInitial(peer.Bundle, Verification{"OUT_OF_BAND", peer.Bundle.Identity.Fingerprint, "verified_failure"}, now) == nil {
		t.Fatal("failed commit accepted")
	}
	if _, e := v.Public(); !errors.Is(e, ErrClosed) {
		t.Fatal("journal failure did not close key access", e)
	}
}
