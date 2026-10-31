package devicecontrol

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"p11.local/compute/client/protocol/executionlease"
)

// PlaintextBinding is a transient local custody check made only after the
// authenticated-origin delivery and signed manifest have been accepted. Its
// hash is not the asset ciphertext digest, an end-to-end signature or authority
// to execute. Never persist it or use it to correlate customer content.
type PlaintextBinding struct {
	verified executionlease.Verified
	manifest string
	digest   [sha256.Size]byte
	bytes    int
}

// Format prevents ordinary diagnostic formatting from exposing the fingerprint.
func (PlaintextBinding) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[local input binding]")
}

// BindPlaintext must receive the original authenticated input response, never
// bytes from an untrusted task companion. Validation and hashing use one bounded
// owned copy, cleared before return. The platform asset digest stays unchanged.
func (a AcceptedDelivery) BindPlaintext(body []byte, now time.Time) (PlaintextBinding, error) {
	copy, err := a.AcceptInput(Reply{Status: 200, Body: body}, now)
	if err != nil {
		return PlaintextBinding{}, err
	}
	defer clear(copy)
	return PlaintextBinding{a.verified, a.manifest.Digest(), sha256.Sum256(copy), len(copy)}, nil
}

type plaintextKey struct{}

// WithPlaintextBinding forwards the accepted input's local custody check through
// the supervisor's separately owned call context. It adds no wire/SQL field.
func WithPlaintextBinding(ctx context.Context, b PlaintextBinding) context.Context {
	return context.WithValue(ctx, plaintextKey{}, b)
}

// CheckPlaintextBinding ties exact bytes to the original verified lease and
// frozen manifest. Zero, expired, another request, changed metadata or a changed
// body all fail closed before the native process receives any content.
func CheckPlaintextBinding(ctx context.Context, request string, m executionlease.Manifest, body []byte) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	b, ok := ctx.Value(plaintextKey{}).(PlaintextBinding)
	return ok && b.verified.ValidAt(time.Now()) && b.verified.Details().ID == request &&
		b.manifest == m.Digest() && b.bytes == len(body) && b.digest == sha256.Sum256(body)
}
