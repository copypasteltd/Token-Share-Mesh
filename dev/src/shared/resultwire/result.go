// Package resultwire defines signed, content-free original result metadata.
// A device signature is an assertion of origin, never trusted metering.
package resultwire

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

const DeliveryProfile = "P11_NATIVE_RESULT_DELIVERY_1"
const UsageProfile = "P11_INDEPENDENT_TOKEN_USAGE_1"
const MaxTokens int64 = 2147483647

type Scope struct {
	Job          string `json:"job_id"`
	Attempt      string `json:"attempt_id"`
	Lease        string `json:"lease_id"`
	Order        string `json:"order_id"`
	Seller       string `json:"seller_id"`
	Device       string `json:"device_id"`
	Resource     string `json:"resource_id"`
	Generation   string `json:"resource_lock_generation"`
	Fence        string `json:"fencing_token"`
	ContractHash string `json:"contract_hash"`
	ManifestHash string `json:"execution_manifest_hash"`
	Environment  string `json:"kernel_environment_digest"`
}

func Original(l executionlease.Lease) Scope {
	return Scope{Job: l.Job, Attempt: l.Attempt, Lease: l.ID, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Fence: l.Fence, ContractHash: l.ContractHash, ManifestHash: l.ManifestHash, Environment: l.Environment}
}

func (s Scope) Validate() error {
	for _, id := range []string{s.Job, s.Attempt, s.Lease, s.Order, s.Seller, s.Device, s.Resource} {
		if !ids.Valid(id) {
			return errors.New("original result identifiers required")
		}
	}
	for _, n := range []string{s.Generation, s.Fence} {
		v, e := executionlease.Counter(n)
		if e != nil || v == 0 {
			return errors.New("original result counters required")
		}
	}
	for _, h := range []string{s.ContractHash, s.ManifestHash, s.Environment} {
		if !executionlease.Hash(h) {
			return errors.New("original result digests required")
		}
	}
	return nil
}

// Counts preserves raw backend quantities, including unavailable vs explicit
// zero. Cached input and reasoning output are subsets only in OPENAI_TOTALS_1.
type Counts struct {
	Input         *int64 `json:"input_tokens"`
	Output        *int64 `json:"output_tokens"`
	Cached        *int64 `json:"cache_read_tokens,omitempty"`
	CacheCreation *int64 `json:"cache_creation_tokens,omitempty"`
	Reasoning     *int64 `json:"reasoning_tokens,omitempty"`
}
type Candidate struct {
	Model          string `json:"model_digest"`
	Tokenizer      string `json:"tokenizer_digest"`
	Template       string `json:"template_digest"`
	Backend        string `json:"backend_id"`
	CounterProfile string `json:"counter_profile_id"`
	Raw            Counts `json:"raw_counts"`
}
type Totals struct {
	Input  int64 `json:"input_tokens"`
	Output int64 `json:"output_tokens"`
}

func (c Candidate) Normalize() (Totals, error) {
	var t Totals
	for _, h := range []string{c.Model, c.Tokenizer, c.Template} {
		if !executionlease.Hash(h) {
			return t, errors.New("fixed model/tokenizer/template digests required")
		}
	}
	if !ids.Valid(c.Backend) || c.Raw.Input == nil || c.Raw.Output == nil {
		return t, errors.New("known original token totals required")
	}
	for _, n := range []*int64{c.Raw.Input, c.Raw.Output, c.Raw.Cached, c.Raw.CacheCreation, c.Raw.Reasoning} {
		if n != nil && (*n < 0 || *n > MaxTokens) {
			return t, errors.New("bounded nonnegative token count required")
		}
	}
	t = Totals{*c.Raw.Input, *c.Raw.Output}
	value := func(n *int64) int64 {
		if n == nil {
			return 0
		}
		return *n
	}
	if value(c.Raw.Reasoning) > t.Output {
		return Totals{}, errors.New("reasoning must be an output subset")
	}
	switch c.CounterProfile {
	case "OPENAI_TOTALS_1":
		if value(c.Raw.Cached) > t.Input || value(c.Raw.CacheCreation) != 0 {
			return Totals{}, errors.New("unsupported or overlapping input accounting")
		}
	case "ANTHROPIC_DISJOINT_1":
		if c.Raw.Cached == nil || c.Raw.CacheCreation == nil {
			return Totals{}, errors.New("explicit disjoint cache accounting required")
		}
		t.Input += *c.Raw.Cached + *c.Raw.CacheCreation
		if t.Input > MaxTokens {
			return Totals{}, errors.New("aggregate input count exceeds limit")
		}
	default:
		return Totals{}, errors.New("unsupported token counting profile")
	}
	return t, nil
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func canonicalTime(raw string) bool {
	if len(raw) > 40 {
		return false
	}
	t, e := time.Parse(time.RFC3339Nano, raw)
	return e == nil && !t.IsZero() && raw == t.UTC().Format(time.RFC3339Nano)
}
func signature(raw string) ([]byte, error) {
	if len(raw) != 86 {
		return nil, errors.New("bounded canonical result signature required")
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	if e != nil || len(b) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(b) != raw {
		return nil, errors.New("canonical result signature required")
	}
	return b, nil
}

// Delivery covers the concatenated UTF-8 output bytes, independent of SSE
// chunk boundaries. It contains no prompt, output, path, URL or executable node.
type Delivery struct {
	Profile      string `json:"profile_id"`
	Scope        Scope  `json:"scope"`
	Result       string `json:"local_result_id"`
	Revision     int64  `json:"revision"`
	OutputHash   string `json:"output_digest"`
	OutputBytes  int64  `json:"output_bytes"`
	InputTokens  int64  `json:"reported_input_tokens"`
	OutputTokens int64  `json:"reported_output_tokens"`
	Finish       string `json:"finish_reason"`
	Completed    string `json:"completed_at"`
	Signature    string `json:"signature"`
}

func (d Delivery) Validate() error {
	if d.Profile != DeliveryProfile || d.Scope.Validate() != nil || !ids.Valid(d.Result) || d.Revision != 1 || !executionlease.Hash(d.OutputHash) || d.OutputBytes < 0 || d.OutputBytes > 32<<20 || d.InputTokens < 1 || d.InputTokens > MaxTokens || d.OutputTokens < 0 || d.OutputTokens > MaxTokens || (d.Finish != "eog" && d.Finish != "limit" && d.Finish != "stop") || !canonicalTime(d.Completed) {
		return errors.New("bounded complete original result metadata required")
	}
	_, e := signature(d.Signature)
	return e
}
func (d Delivery) Message() ([]byte, error) {
	d.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if e := d.Validate(); e != nil {
		return nil, e
	}
	d.Signature = ""
	b, e := json.Marshal(d)
	return append([]byte(DeliveryProfile+"\n"), b...), e
}
func SignDelivery(key ed25519.PrivateKey, d Delivery) (Delivery, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Delivery{}, errors.New("device signing key required")
	}
	b, e := d.Message()
	if e != nil {
		return Delivery{}, e
	}
	d.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, b))
	return d, nil
}
func (d Delivery) Verify(key ed25519.PublicKey) error {
	b, e := d.Message()
	sig, se := signature(d.Signature)
	if e != nil || se != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, b, sig) {
		return errors.New("original device result signature rejected")
	}
	return nil
}

// Statement must be produced by a separately approved meter. Its issuer is
// never inferred from the seller/device/buyer key, result or configuration URL.
type Statement struct {
	Profile      string    `json:"profile_id"`
	Scope        Scope     `json:"scope"`
	DeliveryHash string    `json:"delivery_digest"`
	KeyID        string    `json:"meter_key_id"`
	Evidence     string    `json:"authority_evidence_ref"`
	Candidate    Candidate `json:"candidate"`
	Recorded     string    `json:"recorded_at"`
	Signature    string    `json:"signature"`
}

func (s Statement) Validate() error {
	if len(s.Signature) != 86 {
		return errors.New("canonical independent meter signature required")
	}
	if _, e := s.Message(); e != nil {
		return e
	}
	_, e := signature(s.Signature)
	return e
}

func (s Statement) Message() ([]byte, error) {
	if s.Profile != UsageProfile || s.Scope.Validate() != nil || !executionlease.Hash(s.DeliveryHash) || !ids.Valid(s.KeyID) || !ids.Valid(s.Evidence) || !canonicalTime(s.Recorded) {
		return nil, errors.New("original independent usage statement required")
	}
	if _, e := s.Candidate.Normalize(); e != nil {
		return nil, e
	}
	s.Signature = ""
	b, e := json.Marshal(s)
	return append([]byte(UsageProfile+"\n"), b...), e
}
func SignStatement(key ed25519.PrivateKey, s Statement) (Statement, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Statement{}, errors.New("independent meter key required")
	}
	b, e := s.Message()
	if e != nil {
		return Statement{}, e
	}
	s.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, b))
	return s, nil
}
func (s Statement) Verify(key ed25519.PublicKey) error {
	b, e := s.Message()
	sig, se := signature(s.Signature)
	if e != nil || se != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, b, sig) {
		return errors.New("independent metering signature rejected")
	}
	return nil
}
