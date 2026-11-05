package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/strictjson"
)

// Optional and independent of content delivery secrets. An absent file leaves
// verifier-only progress enabled; no signer/keypair is generated implicitly.
func configuredContinuations(claims *invocation.Claims) (*invocation.Continuations, error) {
	path := os.Getenv("P11_LEASE_CONTINUATION_SIGNER_FILE")
	if path == "" {
		return nil, nil
	}
	if claims == nil {
		return nil, fmt.Errorf("continuation requires explicit original lease verifiers")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("continuation signing configuration unavailable")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("regular continuation signing configuration required")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	defer clear(raw)
	if err != nil || len(raw) > 4096 {
		return nil, fmt.Errorf("bounded continuation signing configuration required")
	}
	var c struct {
		Profile    string `json:"profile_id"`
		KeyID      string `json:"key_id"`
		PrivateKey string `json:"private_key"`
	}
	if strictjson.Decode(raw, &c, 4096) != nil || c.Profile != "P11_LEASE_CONTINUATION_SIGNER_1" {
		return nil, fmt.Errorf("explicit continuation signing profile required")
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(c.PrivateKey)
	defer clear(key)
	if err != nil || len(key) != ed25519.PrivateKeySize || base64.RawURLEncoding.EncodeToString(key) != c.PrivateKey {
		return nil, fmt.Errorf("canonical Ed25519 continuation signing key required")
	}
	return invocation.NewContinuations(claims, c.KeyID, ed25519.PrivateKey(key))
}
