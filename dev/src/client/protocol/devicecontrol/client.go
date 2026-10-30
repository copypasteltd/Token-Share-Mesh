package devicecontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"p11.local/compute/shared/ids"
	"strings"
	"sync"
	"time"
)

// Client is a native control transport, not a provider daemon or a key store.
// The caller supplies an identity loaded from its native credential store.
type Client struct {
	base *url.URL
	http *http.Client
	key  ed25519.PrivateKey
	id   string
	mu   sync.RWMutex
}
type Prepared struct {
	binding Binding
	body    []byte
	owner   *Client
}
type Reply struct {
	Status   int
	Body     []byte
	ETag     string
	Replayed bool
}
type TransportError struct{ Unknown bool }

func (e *TransportError) Error() string {
	if e.Unknown {
		return "control outcome unknown; recover the original prepared command"
	}
	return "control response unavailable"
}

func NewClient(base, deviceID string, key ed25519.PrivateKey) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !ids.Valid(deviceID) || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("explicit control base and identity required")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil, errors.New("HTTPS or explicit native loopback required")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16384, MaxIdleConnsPerHost: 2, IdleConnTimeout: 20 * time.Second}
	return &Client{base: u, id: deviceID, key: append(ed25519.PrivateKey{}, key...), http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Prepare freezes exact bytes and command headers. There is no automatic retry,
// new idempotency key, redirect, cookie jar or content-generating side effect.
func (c *Client) Prepare(method, uri string, body []byte, key, etag string) (Prepared, error) {
	b := Binding{Method: method, Authority: c.base.Host, URI: uri, IdempotencyKey: key, IfMatch: etag}
	if method == "GET" || method == "DELETE" {
		if len(body) != 0 {
			return Prepared{}, errors.New("body not permitted")
		}
	} else {
		if !json.Valid(body) || len(key) < 8 || len(key) > 128 {
			return Prepared{}, errors.New("explicit JSON command and original idempotency key required")
		}
		b.ContentType = "application/json"
	}
	p := Proof{DeviceID: c.id, Timestamp: "1", Nonce: "AAAAAAAAAAAAAAAAAAAAAA"}
	if _, err := Message(p, b, body); err != nil {
		return Prepared{}, err
	}
	return Prepared{binding: b, body: bytes.Clone(body), owner: c}, nil
}
func (c *Client) Send(ctx context.Context, p Prepared) (Reply, error) {
	var reply Reply
	if p.owner != c {
		return reply, errors.New("prepared command belongs to another control identity")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.key) != ed25519.PrivateKeySize {
		return reply, errors.New("control client closed")
	}
	proof, err := Sign(c.key, c.id, p.binding, p.body, time.Now())
	if err != nil {
		return reply, err
	}
	r, err := http.NewRequestWithContext(ctx, p.binding.Method, c.base.Scheme+"://"+c.base.Host+p.binding.URI, bytes.NewReader(p.body))
	if err != nil {
		return reply, errors.New("invalid prepared request")
	}
	r.Header.Set("Authorization", "Bearer "+proof)
	for name, value := range map[string]string{"Idempotency-Key": p.binding.IdempotencyKey, "If-Match": p.binding.IfMatch, "Content-Type": p.binding.ContentType} {
		if value != "" {
			r.Header.Set(name, value)
		}
	}
	unknown := p.binding.Method != "GET"
	response, err := c.http.Do(r)
	if err != nil {
		return reply, &TransportError{Unknown: unknown}
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return reply, &TransportError{Unknown: unknown}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if err != nil || len(raw) > 1048576 {
		return reply, &TransportError{Unknown: unknown}
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") || !json.Valid(raw) {
		return reply, &TransportError{Unknown: unknown}
	}
	return Reply{Status: response.StatusCode, Body: raw, ETag: response.Header.Get("ETag"), Replayed: response.Header.Get("Idempotency-Replayed") == "true"}, nil
}

// Close clears this library's key copy and idle sockets. It cannot erase copies
// owned by callers or defend against privileged memory inspection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.key)
	c.key = nil
	c.http.CloseIdleConnections()
}
