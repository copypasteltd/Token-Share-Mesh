package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"p11.local/compute/gateway/platform/sdkhttp"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/strictjson"
)

// This separate opt-in file contains an issuer private key; the verifier-only
// file is never upgraded into signing authority. Independent provider pins
// and local order approval remain mandatory and are not copied from requests.
func configuredSDK(db *persistence.DB, d *invocation.Delivery, content *invocation.ResultContent, keys identity.KeyService, hosts []string) (*sdkhttp.Server, func(), error) {
	noop := func() {}
	path := os.Getenv("P11_NATIVE_TEXT_SDK_FILE")
	if path == "" {
		return nil, noop, nil
	}
	if d == nil || content == nil || db == nil || keys.DB != db {
		return nil, noop, fmt.Errorf("same-store input delivery, content and SDK keys required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, fmt.Errorf("SDK issuer configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, noop, fmt.Errorf("bounded regular SDK issuer configuration required")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	defer clear(raw)
	if err != nil || len(raw) > 4096 {
		return nil, noop, fmt.Errorf("bounded SDK issuer configuration required")
	}
	var c struct {
		Profile           string `json:"profile_id"`
		InputVersion      string `json:"input_version"`
		SigningKeyID      string `json:"signing_key_id"`
		SigningPrivateKey string `json:"signing_private_key"`
	}
	if strictjson.Decode(raw, &c, 4096) != nil || c.Profile != "P11_NATIVE_TEXT_SDK_1" {
		return nil, noop, fmt.Errorf("explicit SDK issuer profile required")
	}
	private, err := base64.RawURLEncoding.Strict().DecodeString(c.SigningPrivateKey)
	defer clear(private)
	if err != nil || len(private) != 64 || base64.RawURLEncoding.EncodeToString(private) != c.SigningPrivateKey {
		return nil, noop, fmt.Errorf("canonical Ed25519 SDK issuer required")
	}
	c.SigningPrivateKey = ""
	sdk, err := invocation.NewSDK(invocation.SDKConfig{Delivery: d, Content: content, InputVersion: c.InputVersion, SigningKeyID: c.SigningKeyID, SigningPrivateKey: private})
	if err != nil {
		return nil, noop, err
	}
	if !sdk.UsesDB(db) {
		sdk.Close()
		return nil, noop, fmt.Errorf("same original SDK store required")
	}
	handler, err := sdkhttp.New(sdkhttp.Config{AllowedHosts: hosts, Keys: keys, Calls: sdk})
	if err != nil {
		sdk.Close()
		return nil, noop, err
	}
	return handler, func() { _ = handler.Close(context.Background()); sdk.Close() }, nil
}
