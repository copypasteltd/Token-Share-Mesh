package devicecontrol

import (
	"crypto/ed25519"
	"errors"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"time"
)

// Approval is independently provisioned original-order consent and locally
// approved kernel state. NEVER fill it by copying an incoming manifest/lease.
// This is not a persistent KernelRegistry, installer or production pin store.
type Approval struct {
	Order, Route, Seller, Device, Resource, Capability, ContractHash                        string
	Mode, Privacy                                                                           string
	EnvironmentRef, EnvironmentDigest, EngineKind, EngineRelease, ModelDigest, RecipeDigest string
	Spec                                                                                    executionlease.Spec
	InputTokenLimit                                                                         int64
}
type AcceptedDelivery struct {
	verified executionlease.Verified
	manifest executionlease.Manifest
}

func (c *Client) PrepareManifest(id string) (Prepared, error) {
	return c.prepareDelivery(id, "manifest")
}
func (c *Client) PrepareInput(id string) (Prepared, error) { return c.prepareDelivery(id, "input") }
func (c *Client) prepareDelivery(id, suffix string) (Prepared, error) {
	if !ids.Valid(id) {
		return Prepared{}, errors.New("valid original lease reference required")
	}
	return c.Prepare("GET", "/v1/p11/runner/leases/"+id+"/"+suffix, nil, "", "")
}

// AcceptDelivery is used only with responses from the configured authenticated
// native origin. The independent issuer pin authenticates the frozen manifest
// through its signed hash. Exact local approval is still required. It does not
// authenticate plaintext bytes against a signed plaintext digest (none exists).
func AcceptDelivery(claim, manifest Reply, pin ed25519.PublicKey, approved Approval, now time.Time) (AcceptedDelivery, error) {
	var zero AcceptedDelivery
	var m executionlease.Manifest
	raw, e := UnverifiedClaim(claim)
	if e != nil || manifest.Status != 200 || strictjson.Decode(manifest.Body, &m, 16384) != nil {
		return zero, errors.New("bounded original lease and manifest required")
	}
	if m.Profile != executionlease.ManifestProfile || m.Mode != "PLAINTEXT" || m.Privacy != "STANDARD" || m.Mode != approved.Mode || m.Privacy != approved.Privacy || m.InputBytes < 1 || m.InputBytes > 256<<10 || !executionlease.Hash(m.InputDigest) || !ids.Valid(m.InputAsset) || !ids.Valid(m.EnvironmentRef) || m.InputTokenLimit < 1 ||
		m.Order != approved.Order || m.Route != approved.Route || m.Seller != approved.Seller || m.Device != approved.Device || m.Resource != approved.Resource || m.Capability != approved.Capability || m.ContractHash != approved.ContractHash ||
		m.EnvironmentRef != approved.EnvironmentRef || m.EnvironmentDigest != approved.EnvironmentDigest || m.EngineKind != approved.EngineKind || m.EngineRelease != approved.EngineRelease || m.ModelDigest != approved.ModelDigest || m.RecipeDigest != approved.RecipeDigest || m.Spec != approved.Spec || m.InputTokenLimit != approved.InputTokenLimit {
		return zero, errors.New("manifest differs from independent original-order and kernel approval")
	}
	v, e := executionlease.Verify(raw, pin, m.ApprovedPlan(), now)
	if e != nil {
		return zero, e
	}
	l := v.Details()
	if l.Revision != 1 || l.Envelope != "" || len(l.InputAssets) != 1 || l.InputAssets[0] != m.InputAsset {
		return zero, errors.New("original native text input binding required")
	}
	return AcceptedDelivery{v, m}, nil
}
func (a AcceptedDelivery) Verified() executionlease.Verified { return a.verified }
func (a AcceptedDelivery) Manifest() executionlease.Manifest { return a.manifest }

// AcceptInput bounds and validates the exact plaintext response. Authenticity
// of this hop relies on the configured HTTP origin / TLS and server-side AEAD
// plus HMAC checks. CiphertextDigest is NOT a plaintext digest. No E2E claim.
// No content is persisted, logged or submitted here; the caller clears buffers.
func (a AcceptedDelivery) AcceptInput(r Reply, now time.Time) ([]byte, error) {
	if !a.verified.ValidAt(now) || r.Status != 200 || int64(len(r.Body)) != a.manifest.InputBytes || len(r.Body) > 256<<10 {
		return nil, errors.New("original bounded input response required before expiry")
	}
	m, e := relay.Preflight(a.manifest.Spec.Protocol, a.manifest.Spec.Model, r.Body)
	if e != nil || m.MaxOutputTokens != a.manifest.Spec.MaxOutputTokens || m.Stream != a.manifest.Spec.Stream {
		return nil, errors.New("input differs from approved protocol constraints")
	}
	return append([]byte(nil), r.Body...), nil
}
