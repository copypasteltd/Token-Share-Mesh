package node

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"strconv"
)

const HostProfile = "P11_NATIVE_EXECUTION_HOST_1"
const MaximumHostCommands = 4096
const MaximumHostRules = 64

var ErrHost = errors.New("execution host state unavailable; retain original commands and owners")
var ErrHostPolicy = errors.New("execution is outside independent local order and kernel approval")
var ErrHostPending = errors.New("original execution command requires recovery; no replacement claim")

// HostRule is independently approved order/kernel metadata. Spec output tokens
// are the original contract's maximum; streaming is an exact allowed choice.
// Only the output limit may be specialized down after checking this rule.
// A downloaded manifest, response, URL or caller prompt cannot install a rule.
type HostRule struct {
	Approval       devicecontrol.Approval `json:"original_approval"`
	PhysicalDigest string                 `json:"physical_resource_digest"`
}
type HostConfig struct {
	Profile   string     `json:"profile_id"`
	IssuerPin string     `json:"issuer_ed25519_public_key"`
	Rules     []HostRule `json:"approved_orders"`
}

func ruleKey(a devicecontrol.Approval) string {
	return a.Order + "\n" + a.Route + "\n" + string(a.Spec.Protocol) + "\n" + a.Spec.Model + "\n" + strconv.FormatBool(a.Spec.Stream)
}
func (c HostConfig) validate(device, seller string) error {
	pin, e := base64.RawURLEncoding.Strict().DecodeString(c.IssuerPin)
	if c.Profile != HostProfile || e != nil || len(pin) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(pin) != c.IssuerPin || len(c.Rules) < 1 || len(c.Rules) > MaximumHostRules || len(canonicalHostJSON(c)) > hostConfigLimit-28 {
		return ErrHostPolicy
	}
	seen, physical := map[string]bool{}, map[string]string{}
	for _, r := range c.Rules {
		a := r.Approval
		for _, id := range []string{a.Order, a.Route, a.Seller, a.Device, a.Resource, a.Capability, a.EnvironmentRef, a.EngineKind, a.EngineRelease} {
			if !ids.Valid(id) || len(id) > 128 {
				return ErrHostPolicy
			}
		}
		for _, hash := range []string{a.ContractHash, a.EnvironmentDigest, a.ModelDigest, r.PhysicalDigest} {
			if !executionlease.Hash(hash) {
				return ErrHostPolicy
			}
		}
		if a.RecipeDigest != "" && !executionlease.Hash(a.RecipeDigest) {
			return ErrHostPolicy
		}
		if a.Device != device || a.Seller != seller || a.Mode != "PLAINTEXT" || a.Privacy != "STANDARD" || !text(a.Spec.Model, 128) || a.Spec.MaxOutputTokens < 1 || a.Spec.MaxOutputTokens > 1048576 || a.InputTokenLimit < 1 || a.InputTokenLimit > 1048576 {
			return ErrHostPolicy
		}
		switch a.Spec.Protocol {
		case relay.ChatText, relay.ResponsesText, relay.MessagesText:
		default:
			return ErrHostPolicy
		}
		key := ruleKey(a)
		if seen[key] {
			return ErrHostPolicy
		}
		seen[key] = true
		if old := physical[a.Resource]; old != "" && old != r.PhysicalDigest {
			return ErrHostPolicy
		}
		physical[a.Resource] = r.PhysicalDigest
		for resource, digest := range physical {
			if resource != a.Resource && digest == r.PhysicalDigest {
				return ErrHostPolicy
			}
		}
	}
	return nil
}
func (c HostConfig) pin() ed25519.PublicKey {
	b, _ := base64.RawURLEncoding.DecodeString(c.IssuerPin)
	return ed25519.PublicKey(b)
}
func (c HostConfig) approve(raw devicecontrol.Reply) (devicecontrol.Approval, error) {
	var m executionlease.Manifest
	if raw.Status != 200 || strictjson.Decode(raw.Body, &m, 16384) != nil {
		return devicecontrol.Approval{}, ErrHostPolicy
	}
	for _, r := range c.Rules {
		a := r.Approval
		if a.Order == m.Order && a.Route == m.Route && a.Spec.Protocol == m.Spec.Protocol && a.Spec.Model == m.Spec.Model && a.Spec.Stream == m.Spec.Stream && m.Spec.MaxOutputTokens > 0 && m.Spec.MaxOutputTokens <= a.Spec.MaxOutputTokens {
			// This is bounded specialization of an independent maximum, not
			// approval of other facts copied from an incoming manifest.
			a.Spec.MaxOutputTokens = m.Spec.MaxOutputTokens
			return a, nil
		}
	}
	return devicecontrol.Approval{}, ErrHostPolicy
}
func cloneHostConfig(c HostConfig) HostConfig {
	c.Rules = append([]HostRule(nil), c.Rules...)
	return c
}
func canonicalHostJSON(v any) []byte { b, _ := json.Marshal(v); return b }
