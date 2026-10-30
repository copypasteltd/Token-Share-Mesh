package devicecontrol

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"p11.local/compute/shared/ids"
	"strings"
)

type EnrollmentClient struct {
	base *url.URL
	http *http.Client
}

func NewEnrollmentClient(base string) (*EnrollmentClient, error) {
	u, h, err := nativeTransport(base)
	if err != nil {
		return nil, err
	}
	return &EnrollmentClient{base: u, http: h}, nil
}

func PairingID(token string) (string, error) {
	p := strings.Split(token, ".")
	if len(p) != 3 || p[0] != "p11p" || !ids.Valid(p[1]) {
		return "", errors.New("one native pairing credential required")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(p[2])
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != p[2] {
		return "", errors.New("invalid pairing credential")
	}
	return p[1], nil
}

// Send performs one attempt. The caller durably freezes body/key/credential
// before calling and explicitly recovers an unknown result with those bytes.
func (c *EnrollmentClient) Send(ctx context.Context, body []byte, token, key string) (Reply, error) {
	if _, err := PairingID(token); err != nil {
		return Reply{}, err
	}
	if len(body) == 0 || len(body) > MaximumBody || len(key) < 8 || len(key) > 128 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return Reply{}, errors.New("bounded original enrollment required")
	}
	b := Binding{Method: "POST", URI: "/v1/p11/device-enrollments", IdempotencyKey: key, ContentType: "application/json"}
	return sendNative(ctx, c.http, c.base, b, body, token)
}
func (c *EnrollmentClient) Close() { c.http.CloseIdleConnections() }
