package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/strictjson"
)

// Private input/fingerprint configuration is opt-in and separate from the
// verifier-only claim file. No signing key, inference listener or defaults.
func configuredDelivery(db *persistence.DB, claims *invocation.Claims) (*invocation.Delivery, func(), error) {
	noop := func() {}
	path := os.Getenv("P11_INPUT_DELIVERY_KEYS_FILE")
	if path == "" {
		return nil, noop, nil
	}
	if claims == nil {
		return nil, noop, fmt.Errorf("configured claims required for input delivery")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, noop, fmt.Errorf("input delivery configuration unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 16385))
	defer clear(raw)
	if e != nil || len(raw) > 16384 {
		return nil, noop, fmt.Errorf("bounded input delivery configuration required")
	}
	var c struct {
		Profile            string            `json:"profile_id"`
		FingerprintVersion string            `json:"fingerprint_version"`
		FingerprintKeys    map[string]string `json:"fingerprint_keys"`
		InputKeys          map[string]string `json:"input_keys"`
	}
	if strictjson.Decode(raw, &c, 16384) != nil || c.Profile != "P11_INPUT_DELIVERY_KEYS_1" {
		return nil, noop, fmt.Errorf("explicit input delivery profile required")
	}
	decode := func(src map[string]string) (map[string][]byte, error) {
		dst := map[string][]byte{}
		for id, value := range src {
			k, e := base64.RawURLEncoding.DecodeString(value)
			if e != nil || len(k) != 32 || base64.RawURLEncoding.EncodeToString(k) != value {
				clear(k)
				for _, v := range dst {
					clear(v)
				}
				return nil, fmt.Errorf("canonical 32-byte purpose keys required")
			}
			dst[id] = k
		}
		return dst, nil
	}
	fp, e := decode(c.FingerprintKeys)
	if e != nil {
		return nil, noop, e
	}
	defer func() {
		for _, v := range fp {
			clear(v)
		}
	}()
	input, e := decode(c.InputKeys)
	if e != nil {
		return nil, noop, e
	}
	defer func() {
		for _, v := range input {
			clear(v)
		}
	}()
	inv, e := invocation.New(invocation.Config{DB: db, PaymentEnvironment: os.Getenv("P11_PAYMENT_ENVIRONMENT"), FingerprintVersion: c.FingerprintVersion, FingerprintKeys: fp})
	if e != nil {
		return nil, noop, e
	}
	d, e := invocation.NewDelivery(invocation.DeliveryConfig{Claims: claims, Invocations: inv, InputKeys: input})
	if e != nil {
		inv.Close()
		return nil, noop, e
	}
	return d, func() { d.Close(); inv.Close() }, nil
}
