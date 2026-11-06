package main

import (
	"fmt"
	"io"
	"os"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/usage"
	"p11.local/compute/shared/strictjson"
)

// Completion has no engine or payment signing key. Meter authority remains
// opt-in; absence keeps acceptance separate from unverified billable usage.
func configuredCompletion(claims *invocation.Claims) (*invocation.Completion, error) {
	path := os.Getenv("P11_INDEPENDENT_METERS_FILE")
	if claims == nil {
		if path != "" {
			return nil, fmt.Errorf("original execution verifier required for independent meters")
		}
		return nil, nil
	}
	var meters *usage.Verifier
	if path != "" {
		f, e := os.Open(path)
		if e != nil {
			return nil, fmt.Errorf("independent meter configuration unavailable")
		}
		defer f.Close()
		raw, e := io.ReadAll(io.LimitReader(f, 16385))
		defer clear(raw)
		if e != nil || len(raw) > 16384 {
			return nil, fmt.Errorf("bounded independent meter configuration required")
		}
		var c struct {
			Profile string      `json:"profile_id"`
			Pins    []usage.Pin `json:"pins"`
		}
		if strictjson.Decode(raw, &c, 16384) != nil || c.Profile != "P11_INDEPENDENT_METER_PINS_1" {
			return nil, fmt.Errorf("explicit independent meter profile required")
		}
		meters, e = usage.New(c.Pins)
		if e != nil {
			return nil, e
		}
	}
	return invocation.NewCompletion(claims, meters)
}
