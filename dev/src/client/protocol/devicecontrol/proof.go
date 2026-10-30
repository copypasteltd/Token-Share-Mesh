// Package devicecontrol defines the provider control identity protocol.
// These keys sign bounded control requests, never decrypt customer content.
package devicecontrol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"p11.local/compute/shared/ids"
	"strconv"
	"strings"
	"time"
)

const KeyProfile = "ED25519_DEVICE_1"
const ProofProfile = "P11_DEVICE_ED25519_1"
const MaximumBody = 65536
const ClockSkew = 60 * time.Second

type Proof struct {
	DeviceID, Timestamp, Nonce string
	Signature                  []byte
}
type Binding struct {
	Method, Authority, URI, IdempotencyKey, IfMatch, ContentType string
}

func PublicKey(encoded string) (ed25519.PublicKey, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(b) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(b) != encoded {
		return nil, errors.New("one canonical Ed25519 public key required")
	}
	return ed25519.PublicKey(b), nil
}
func Parse(token string, now time.Time) (Proof, error) {
	var p Proof
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != "p11d" || !ids.Valid(parts[1]) {
		return p, errors.New("invalid device proof")
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || seconds < 1 || strconv.FormatInt(seconds, 10) != parts[2] || seconds < now.Unix()-60 || seconds > now.Unix()+60 {
		return p, errors.New("expired device proof")
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || len(nonce) != 16 || base64.RawURLEncoding.EncodeToString(nonce) != parts[3] {
		return p, errors.New("invalid device nonce")
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(signature) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(signature) != parts[4] {
		return p, errors.New("invalid device signature")
	}
	return Proof{parts[1], parts[2], parts[3], signature}, nil
}

// Message signs exact URI/body bytes. Newline and control-character injection,
// URL authority confusion and noncanonical paths fail before signing/verifying.
func Message(p Proof, binding Binding, body []byte) ([]byte, error) {
	method, authority, requestURI := binding.Method, binding.Authority, binding.URI
	if !ids.Valid(p.DeviceID) || len(body) > MaximumBody || (method != "GET" && method != "POST" && method != "PATCH" && method != "DELETE") {
		return nil, errors.New("invalid signed control request")
	}
	for _, s := range []string{authority, requestURI, p.Timestamp, p.Nonce} {
		if s == "" || len(s) > 2048 {
			return nil, errors.New("invalid control signature field")
		}
		for _, c := range s {
			if c < 33 || c > 126 {
				return nil, errors.New("ASCII signature field required")
			}
		}
	}
	for _, s := range []string{binding.IdempotencyKey, binding.IfMatch, binding.ContentType} {
		if len(s) > 256 {
			return nil, errors.New("signed control header too long")
		}
		for _, c := range s {
			if c < 32 || c > 126 {
				return nil, errors.New("invalid signed control header")
			}
		}
	}
	u, err := url.Parse("https://" + authority)
	if err != nil || u.Host != authority || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid control authority")
	}
	uri, err := url.ParseRequestURI(requestURI)
	if err != nil || !strings.HasPrefix(requestURI, "/v1/p11/") || uri.IsAbs() || uri.Host != "" || uri.RawPath != "" || uri.Fragment != "" || strings.Contains(requestURI, "#") || strings.Contains(uri.Path, "//") {
		return nil, errors.New("invalid signed URI")
	}
	digest := sha256.Sum256(body)
	return []byte(strings.Join([]string{ProofProfile, p.DeviceID, p.Timestamp, p.Nonce, method, authority, requestURI, binding.IdempotencyKey, binding.IfMatch, binding.ContentType, hex.EncodeToString(digest[:]), ""}, "\n")), nil
}

func Sign(key ed25519.PrivateKey, deviceID string, binding Binding, body []byte, now time.Time) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("valid device private key required")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	p := Proof{DeviceID: deviceID, Timestamp: strconv.FormatInt(now.Unix(), 10), Nonce: base64.RawURLEncoding.EncodeToString(nonce[:])}
	message, err := Message(p, binding, body)
	if err != nil {
		return "", err
	}
	return "p11d." + deviceID + "." + p.Timestamp + "." + p.Nonce + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, message)), nil
}
