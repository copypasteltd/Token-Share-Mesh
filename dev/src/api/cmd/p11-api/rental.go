package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/rental"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

func boundedRentalConfig(path string, maximum int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("rental authority configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(maximum) {
		return nil, fmt.Errorf("bounded regular rental authority configuration required")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maximum)+1))
	if err != nil || len(raw) > maximum {
		clear(raw)
		return nil, fmt.Errorf("bounded rental authority configuration required")
	}
	return raw, nil
}

// Missing both files disables the optional rental service. Partial or invalid
// configuration stops startup; it never generates a signer or trusts a public
// key copied from a submitted grant. Neither file contains content keys.
func configuredRentals(db *persistence.DB) (*rental.Service, error) {
	signerPath, pinsPath := os.Getenv("P11_RENTAL_SIGNER_FILE"), os.Getenv("P11_RENTAL_VERIFIERS_FILE")
	if signerPath == "" && pinsPath == "" {
		return nil, nil
	}
	if signerPath == "" || pinsPath == "" {
		return nil, fmt.Errorf("rental signer and independent verifier files are both required")
	}
	pinsRaw, err := boundedRentalConfig(pinsPath, 8192)
	if err != nil {
		return nil, err
	}
	defer clear(pinsRaw)
	var pins struct {
		Profile string            `json:"profile_id"`
		Issuer  string            `json:"issuer_id"`
		Keys    map[string]string `json:"keys"`
	}
	if strictjson.Decode(pinsRaw, &pins, 8192) != nil || pins.Profile != "P11_RENTAL_VERIFIERS_1" || !ids.Valid(pins.Issuer) || len(pins.Keys) < 1 || len(pins.Keys) > 16 {
		return nil, fmt.Errorf("explicit rental verifier profile required")
	}
	keys := map[string]ed25519.PublicKey{}
	for id, encoded := range pins.Keys {
		key, e := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if e != nil || !ids.Valid(id) || len(key) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(key) != encoded {
			return nil, fmt.Errorf("canonical rental Ed25519 verifier required")
		}
		keys[id] = ed25519.PublicKey(key)
	}
	raw, err := boundedRentalConfig(signerPath, 4096)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var signer struct {
		Profile string `json:"profile_id"`
		Issuer  string `json:"issuer_id"`
		KeyID   string `json:"key_id"`
		Key     string `json:"private_key"`
	}
	if strictjson.Decode(raw, &signer, 4096) != nil || signer.Profile != "P11_RENTAL_SIGNER_1" || signer.Issuer != pins.Issuer {
		return nil, fmt.Errorf("explicit matching rental signing profile required")
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(signer.Key)
	defer clear(key)
	if err != nil || len(key) != ed25519.PrivateKeySize || base64.RawURLEncoding.EncodeToString(key) != signer.Key {
		return nil, fmt.Errorf("canonical rental Ed25519 private key required")
	}
	return rental.NewService(rental.Config{DB: db, PaymentEnvironment: os.Getenv("P11_PAYMENT_ENVIRONMENT"), Issuer: signer.Issuer, KeyID: signer.KeyID, SigningKey: ed25519.PrivateKey(key), IssuerPins: keys})
}
