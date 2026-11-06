// Package canonicaljson bounds and rejects ambiguous JSON before delegating to
// the pinned RFC8785 reference implementation. It never normalizes prompt text.
package canonicaljson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"p11.local/compute/shared/strictjson"
	"p11.local/compute/thirdparty/jsoncanonicalizer"
)

var ErrCanonical = errors.New("bounded canonical JSON required")

const Profile = "RFC8785_JCS_1"

func Encode(value any, limit int) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrCanonical
	}
	defer clear(raw)
	return Normalize(raw, limit)
}
func Normalize(raw []byte, limit int) ([]byte, error) {
	if limit < 1 || limit > 1<<20 || len(raw) > limit {
		return nil, ErrCanonical
	}
	var document any
	if strictjson.Decode(raw, &document, limit) != nil {
		return nil, ErrCanonical
	}
	// The upstream parser also rejects invalid Unicode escapes, including lone
	// UTF-16 surrogates which encoding/json would otherwise replace silently.
	out, err := jsoncanonicalizer.Transform(raw)
	if err != nil || len(out) > limit {
		clear(out)
		return nil, ErrCanonical
	}
	return out, nil
}
func DecodeExact(raw []byte, target any, limit int) error {
	if strictjson.Decode(raw, target, limit) != nil {
		return ErrCanonical
	}
	canonical, err := Normalize(raw, limit)
	defer clear(canonical)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ErrCanonical
	}
	return nil
}
func Digest(domain string, raw []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(domain))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
