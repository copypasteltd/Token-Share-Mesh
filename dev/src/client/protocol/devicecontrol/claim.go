package devicecontrol

import (
	"encoding/json"
	"errors"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

// LeaseClaim mirrors the authoritative control contract. ClaimKey is the
// original command ID, also sent as Idempotency-Key. Persist it BEFORE sending.
// A claim is not an execution ACK or permission to create another command.
type LeaseClaim struct {
	Device     string `json:"device_id"`
	Capability string `json:"capability_id"`
	ClaimKey   string `json:"claim_key"`
	Version    int64  `json:"expected_device_version"`
}

func (in LeaseClaim) Validate(key string) error {
	if !ids.Valid(in.Device) || !ids.Valid(in.Capability) || !ids.Valid(in.ClaimKey) || in.ClaimKey != key || len(key) < 8 || in.Version < 1 || in.Version >= 2147483647 {
		return errors.New("original claim ID, device, capability and version required")
	}
	return nil
}

// PrepareLeaseClaim freezes a signed native request. Send may be retried only
// with this prepared command (fresh proof nonce, original key and exact body).
// The library never polls, changes the key, renews or submits to an engine.
func (c *Client) PrepareLeaseClaim(in LeaseClaim) (Prepared, error) {
	if in.Device != c.id || in.Validate(in.ClaimKey) != nil {
		return Prepared{}, errors.New("claim belongs to another device or command")
	}
	body, _ := json.Marshal(in)
	return c.Prepare("POST", "/v1/p11/runner/leases/claim", body, in.ClaimKey, "")
}

// UnverifiedClaim checks only bounded wire shape. Its bytes MUST subsequently
// pass executionlease.Verify using independent pins AND an approved plan. A
// valid HTTP proof/response does not approve a kernel or populate that plan.
func UnverifiedClaim(r Reply) ([]byte, error) {
	var l executionlease.Lease
	if r.Status != 200 || strictjson.Decode(r.Body, &l, executionlease.MaximumBody) != nil {
		return nil, errors.New("bounded claim response required")
	}
	if _, err := executionlease.Message(l); err != nil {
		return nil, errors.New("invalid claim lease shape")
	}
	return append([]byte(nil), r.Body...), nil
}
