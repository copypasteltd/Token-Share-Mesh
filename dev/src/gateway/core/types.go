// Package core implements bounded, native text forwarding inside an already
// authorized execution domain. It is not a payment, lease or decryption service.
package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Profile string

const (
	ChatText         Profile = "P11_CHAT_TEXT_NATIVE_1"
	ResponsesText    Profile = "P11_RESPONSES_TEXT_NATIVE_1"
	MessagesText     Profile = "P11_MESSAGES_TEXT_NATIVE_1"
	AnthropicVersion         = "2026-10-21"
	PlatformPlain            = "PLATFORM_PLAIN"
	SellerDecrypted          = "SELLER_DECRYPTED"
)

func (p Profile) path() string {
	switch p {
	case ChatText:
		return "/v1/chat/completions"
	case ResponsesText:
		return "/v1/responses"
	case MessagesText:
		return "/v1/messages"
	}
	return ""
}

// Binding is supplied by a trusted, preconfirmed route resolver, never by model
// JSON. A core owns one immutable seller/profile/endpoint; there is no pool.
type Binding struct {
	RouteID, ContractHash, SellerID, ListingRevision string
	Mode, ProfileRevision, PublicModel, BackendModel string
	Profile                                          Profile
	Endpoint, SecretRef                              string
	AllowLoopbackHTTP                                bool
}

// Intent contains no prompt, output, credential or content key. Its digest is
// local to the permitted plaintext domain, not metadata for a cipher relay.
type Intent struct {
	Binding                       Binding
	RequestDigest, IdempotencyKey string
	MaxOutputTokens               int
	Stream                        bool
}

type Credential struct{ Kind, Value string }

// Grant must come from durable, current route/funding/owner admission. The
// production implementation must claim the original invocation only once and
// honor cancellation; this package supplies no permissive/default authorizer.
// Revoked closes on withdrawal. Deadline is an execution hard stop, not a timer
// that a ping, usage event or retry can extend. No grant is minted from HTTP.
type Grant struct {
	InvocationID, SellerID, ContractHash string
	RouteID                              string
	AuthorityKind, ResourceID            string
	Fence, ResourceGeneration            uint64
	Deadline                             time.Time
	Revoked                              <-chan struct{}
}

type Authorizer interface {
	Admit(context.Context, Credential, Intent) (Grant, error)
}

// SecretSource is restricted to the binding's seller and opaque reference. It
// must honor context. Buyer credentials are never passed to this port.
type SecretSource interface {
	Lookup(context.Context, string, string) (string, error)
}

var (
	ErrUnauthorized = errors.New("relay credential rejected")
	ErrForbidden    = errors.New("relay authority rejected")
	ErrNotReady     = errors.New("relay admission unavailable")
	ErrConfig       = errors.New("invalid bound relay configuration")
	errProtocol     = errors.New("backend protocol rejected")
)

type Limits struct {
	RequestBytes, ResponseBytes, EventBytes, MaxOutputTokens int
	Concurrent                                               int
	Total, FirstByte, Idle, Write                            time.Duration
}

func DefaultLimits() Limits {
	return Limits{256 << 10, 512 << 10, 64 << 10, 32768, 4,
		2 * time.Minute, 15 * time.Second, 20 * time.Second, 5 * time.Second}
}

// Usage is an untrusted protocol candidate. It grants no billable evidence,
// acceptance, settlement or resource release. Missing usage stays nil.
type Usage struct{ InputTokens, OutputTokens uint64 }

type Outcome struct {
	InvocationID                                                   string
	SubmissionAttempted, HeadersSent, TerminalValidated, Delivered bool
	Terminal, Code                                                 string
	Usage                                                          *Usage
}

type Config struct {
	Domain     string
	Binding    Binding
	Authorizer Authorizer
	Secrets    SecretSource
	Limits     Limits
}

type Core struct {
	domain     string
	binding    Binding
	endpoint   url.URL
	authorizer Authorizer
	secrets    SecretSource
	limits     Limits
	transport  *http.Transport
	slots      chan struct{}
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var modelIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./:@-]{0,199}$`)
var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func New(c Config) (*Core, error) {
	b, l := c.Binding, c.Limits
	if l == (Limits{}) {
		l = DefaultLimits()
	}
	if c.Authorizer == nil || c.Secrets == nil || b.Profile.path() == "" ||
		!identifier.MatchString(b.RouteID) || !identifier.MatchString(b.SellerID) ||
		!identifier.MatchString(b.ListingRevision) || !digest.MatchString(b.ContractHash) ||
		b.ProfileRevision != "1" || !identifier.MatchString(b.SecretRef) ||
		!identifier.MatchString(b.PublicModel) || !modelIdentifier.MatchString(b.BackendModel) ||
		!((c.Domain == PlatformPlain && b.Mode == "STANDARD") ||
			(c.Domain == SellerDecrypted && b.Mode == "E2E_BASIC")) {
		return nil, ErrConfig
	}
	if l.RequestBytes < 256 || l.RequestBytes > 256<<10 || l.ResponseBytes < 256 || l.ResponseBytes > 512<<10 ||
		l.EventBytes < 128 || l.EventBytes > 64<<10 || l.MaxOutputTokens < 1 || l.MaxOutputTokens > 32768 ||
		l.Concurrent < 1 || l.Concurrent > 32 || l.Total < 20*time.Millisecond || l.Total > 10*time.Minute ||
		l.FirstByte < 10*time.Millisecond || l.FirstByte > l.Total || l.Idle < 10*time.Millisecond || l.Idle > l.Total ||
		l.Write < 10*time.Millisecond || l.Write > l.Total {
		return nil, ErrConfig
	}
	u, err := url.Parse(b.Endpoint)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || u.Path != b.Profile.path() {
		return nil, ErrConfig
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && b.AllowLoopbackHTTP && ip != nil && ip.IsLoopback()) {
		return nil, ErrConfig
	}
	// Own transport: no environment proxy, cookies, redirect client, compression,
	// injected RoundTripper, account rotation or retry middleware. The POST body
	// is intentionally not replayable and no idempotency header reaches upstream.
	t := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: l.FirstByte, KeepAlive: 30 * time.Second}).DialContext,
		DisableCompression: true, MaxResponseHeaderBytes: 32 << 10, ResponseHeaderTimeout: l.FirstByte,
		TLSHandshakeTimeout: l.FirstByte, ExpectContinueTimeout: time.Second,
		MaxConnsPerHost: l.Concurrent, MaxIdleConnsPerHost: l.Concurrent, IdleConnTimeout: 30 * time.Second,
	}
	return &Core{c.Domain, b, *u, c.Authorizer, c.Secrets, l, t, make(chan struct{}, l.Concurrent)}, nil
}

// Close releases this core's idle connections. Active calls remain bounded by
// their caller/grant context; an HTTP disconnect does not prove GPU stopped.
func (c *Core) Close() { c.transport.CloseIdleConnections() }

func grantValid(g Grant, b Binding) bool {
	return identifier.MatchString(g.InvocationID) && g.SellerID == b.SellerID && g.RouteID == b.RouteID &&
		g.ContractHash == b.ContractHash && identifier.MatchString(g.ResourceID) &&
		(g.AuthorityKind == "JOB_LEASE" || g.AuthorityKind == "RENTAL_SESSION") &&
		g.Fence > 0 && g.ResourceGeneration > 0 && g.Revoked != nil && time.Now().Before(g.Deadline)
}

func secretValid(s string) bool {
	if len(s) < 1 || len(s) > 4096 {
		return false
	}
	for _, ch := range s {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return !strings.ContainsAny(s, "\r\n")
}
