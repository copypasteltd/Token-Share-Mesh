package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/strictjson"
)

// Public verifier configuration is explicit, bounded and never generated from
// received lease JSON. Device pins and local kernel approval remain separate.
func configuredClaims(db *persistence.DB) (*invocation.Claims, error) {
	path := os.Getenv("P11_LEASE_VERIFIERS_FILE")
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("lease verifier configuration unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(raw) > 8192 {
		return nil, fmt.Errorf("bounded lease verifier configuration required")
	}
	var c struct {
		Profile string            `json:"profile_id"`
		Keys    map[string]string `json:"keys"`
	}
	if strictjson.Decode(raw, &c, 8192) != nil || c.Profile != "P11_LEASE_VERIFIERS_1" {
		return nil, fmt.Errorf("explicit lease verifier profile required")
	}
	keys := map[string]ed25519.PublicKey{}
	for id, value := range c.Keys {
		key, e := base64.RawURLEncoding.DecodeString(value)
		if e != nil || len(key) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(key) != value {
			return nil, fmt.Errorf("canonical Ed25519 verifier required")
		}
		keys[id] = key
	}
	return invocation.NewClaims(db, os.Getenv("P11_PAYMENT_ENVIRONMENT"), keys)
}
