// Package cipherstream implements the separately versioned BINARY_STREAM_1
// object format. Cryptographic completeness never grants execution, acceptance,
// trusted usage, payment or replacement-seller authority.
package cipherstream

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/ids"
)

const Profile = "BINARY_STREAM_1"
const SignatureProfile = "ED25519_JCS_1"
const MaximumHeaderBytes = 16384
const MaximumPayloadBytes = 65528
const MaximumRecordBody = 65561
const MinimumRecordBody = 33
const MaximumObjectBytes int64 = 66 << 20
const domain = "P11/E2E/BINARY_STREAM_1/"

var ErrEnvelope = errors.New("original cipher envelope rejected")
var ErrRecord = errors.New("cipher stream incomplete or invalid")
var ErrLimit = errors.New("cipher stream budget exceeded")
var ErrClosed = errors.New("cipher stream closed")

// All cross-system counters are decimal strings. The two resource-generation
// fields are deliberately absent: an INPUT is prepared before they are known.
type Context struct {
	Profile                string `json:"profile_id"`
	Suite                  string `json:"suite_id"`
	Canonicalization       string `json:"canonicalization_id"`
	Contract               string `json:"contract_hash"`
	Order                  string `json:"order_id"`
	Job                    string `json:"job_id"`
	Attempt                string `json:"attempt_id"`
	Fence                  string `json:"fencing_token"`
	Seller                 string `json:"seller_id"`
	Buyer                  string `json:"buyer_id"`
	SellerEndpoint         string `json:"seller_endpoint_id"`
	BuyerEndpoint          string `json:"buyer_endpoint_id"`
	BuyerResultKey         string `json:"buyer_result_key_id"`
	BuyerResultFingerprint string `json:"buyer_result_key_fingerprint"`
	Sender                 string `json:"sender_endpoint_id"`
	SenderKey              string `json:"sender_signing_key_id"`
	Receiver               string `json:"receiver_endpoint_id"`
	ReceiverKey            string `json:"receiver_encryption_key_id"`
	ReceiverFingerprint    string `json:"receiver_key_fingerprint"`
	Environment            string `json:"kernel_environment_digest"`
	LocalPolicy            string `json:"local_policy_id"`
	Mode                   string `json:"service_mode"`
	Privacy                string `json:"privacy_profile"`
	APIProfile             string `json:"api_profile_id"`
	Direction              string `json:"direction"`
	Purpose                string `json:"purpose"`
	Object                 string `json:"object_id"`
	Stream                 string `json:"stream_id"`
	Epoch                  string `json:"epoch"`
	Deadline               string `json:"deadline"`
	HardDeadline           string `json:"original_hard_deadline"`
	Delivery               string `json:"delivery_id,omitempty"`
	DeliveryRevision       string `json:"delivery_revision,omitempty"`
	StreamHeader           string `json:"stream_header"`
}

func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func ValidHash(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == v
}
func Counter(v string, zero bool, bits int) (uint64, error) {
	n, e := strconv.ParseUint(v, 10, bits)
	if e != nil || strconv.FormatUint(n, 10) != v || !zero && n == 0 {
		return 0, ErrEnvelope
	}
	return n, nil
}
func CanonicalTime(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil || t.Location() != time.UTC || t.UTC().Format(time.RFC3339Nano) != v {
		return time.Time{}, ErrEnvelope
	}
	return t, nil
}
func decode64(v string, n int) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(v)
	if e != nil || len(b) != n || base64.RawURLEncoding.EncodeToString(b) != v {
		clear(b)
		return nil, ErrEnvelope
	}
	return b, nil
}
func (c Context) Validate(now time.Time) error {
	if c.Profile != Profile || c.Suite != sodium.Suite || c.Canonicalization != canonicaljson.Profile ||
		!ValidHash(c.Contract) || !ValidHash(c.Environment) || !ValidHash(c.ReceiverFingerprint) || !ValidHash(c.BuyerResultFingerprint) ||
		c.Mode != "E2E" || c.Privacy != "PLATFORM_BLIND" {
		return ErrEnvelope
	}
	for _, v := range []string{c.Order, c.Job, c.Attempt, c.Seller, c.Buyer, c.SellerEndpoint, c.BuyerEndpoint, c.BuyerResultKey, c.Sender, c.SenderKey, c.Receiver, c.ReceiverKey, c.LocalPolicy, c.Object, c.Stream} {
		if !ids.Valid(v) {
			return ErrEnvelope
		}
	}
	if c.Seller == c.Buyer || c.SellerEndpoint == c.BuyerEndpoint {
		return ErrEnvelope
	}
	if _, e := Counter(c.Fence, false, 63); e != nil {
		return ErrEnvelope
	}
	if _, e := Counter(c.Epoch, false, 64); e != nil {
		return ErrEnvelope
	}
	if _, e := decode64(c.StreamHeader, sodium.HeaderBytes); e != nil {
		return ErrEnvelope
	}
	until, e := CanonicalTime(c.Deadline)
	hard, he := CanonicalTime(c.HardDeadline)
	if e != nil || he != nil || !now.Before(until) {
		return ErrEnvelope
	}
	if c.Purpose == "OUTPUT" {
		if until.After(hard.Add(30 * time.Minute)) {
			return ErrEnvelope
		}
	} else if until.After(hard) {
		return ErrEnvelope
	}
	switch c.APIProfile {
	case "P11_CHAT_TEXT_NATIVE_1", "P11_RESPONSES_TEXT_NATIVE_1", "P11_MESSAGES_TEXT_NATIVE_1":
	default:
		return ErrEnvelope
	}
	switch c.Direction {
	case "BUYER_TO_SELLER":
		if c.Sender != c.BuyerEndpoint || c.Receiver != c.SellerEndpoint || c.Purpose != "INPUT" && c.Purpose != "CONTROL" {
			return ErrEnvelope
		}
	case "SELLER_TO_BUYER":
		if c.Sender != c.SellerEndpoint || c.Receiver != c.BuyerEndpoint || c.ReceiverKey != c.BuyerResultKey || c.ReceiverFingerprint != c.BuyerResultFingerprint || c.Purpose != "OUTPUT" && c.Purpose != "CONTROL" {
			return ErrEnvelope
		}
	default:
		return ErrEnvelope
	}
	if c.Purpose == "OUTPUT" {
		if !ids.Valid(c.Delivery) {
			return ErrEnvelope
		}
		if _, e := Counter(c.DeliveryRevision, false, 63); e != nil {
			return ErrEnvelope
		}
	} else if c.Delivery != "" || c.DeliveryRevision != "" {
		return ErrEnvelope
	}
	return nil
}

type Header struct {
	Context          Context `json:"context"`
	Sealed           string  `json:"sealed_key"`
	SealedHash       string  `json:"sealed_key_sha256"`
	SignatureProfile string  `json:"signature_profile_id"`
	Signature        string  `json:"signature"`
}
type unsignedHeader struct {
	Context          Context `json:"context"`
	Sealed           string  `json:"sealed_key"`
	SealedHash       string  `json:"sealed_key_sha256"`
	SignatureProfile string  `json:"signature_profile_id"`
}

func (h Header) unsigned() unsignedHeader {
	return unsignedHeader{h.Context, h.Sealed, h.SealedHash, h.SignatureProfile}
}

type envelopePayload struct {
	Profile      string `json:"profile_id"`
	Key          string `json:"stream_key"`
	ContextHash  string `json:"context_hash"`
	StreamHeader string `json:"stream_header"`
}
type Limits struct {
	DataBytes, WireBytes int64
	Records              uint64
}

func DefaultLimits(purpose string) Limits {
	n := int64(64 << 20)
	if purpose == "INPUT" {
		n = 2 << 20
	}
	if purpose == "CONTROL" {
		n = 0
	}
	return Limits{DataBytes: n, WireBytes: MaximumObjectBytes, Records: 131072}
}
func (l Limits) validate(purpose string) error {
	if purpose != "INPUT" && purpose != "OUTPUT" && purpose != "CONTROL" {
		return ErrLimit
	}
	max := DefaultLimits(purpose)
	if l.DataBytes < 0 || l.DataBytes > max.DataBytes || l.WireBytes < 64 || l.WireBytes > MaximumObjectBytes || l.Records < 2 || l.Records > max.Records {
		return ErrLimit
	}
	return nil
}

// Validate permits callers to bound input allocation before native cipher
// construction. It does not validate an original business authorization.
func (l Limits) Validate(purpose string) error { return l.validate(purpose) }

type Expected struct {
	Context         Context
	SenderPublic    ed25519.PublicKey
	RecipientPublic []byte
	Limits          Limits
}

func (e Expected) verify(h Header, now time.Time) error {
	c := h.Context
	expected := e.Context
	if expected.StreamHeader != "" || len(e.SenderPublic) != ed25519.PublicKeySize || len(e.RecipientPublic) != sodium.KeyBytes ||
		c.Validate(now) != nil || e.Limits.validate(c.Purpose) != nil || Hash(e.RecipientPublic) != c.ReceiverFingerprint {
		return ErrEnvelope
	}
	c.StreamHeader = ""
	if c != expected {
		return ErrEnvelope
	}
	return nil
}
func signedBytes(suffix string, value any, limit int) ([]byte, error) {
	raw, e := canonicaljson.Encode(value, limit)
	if e != nil {
		return nil, ErrEnvelope
	}
	return append([]byte(domain+suffix+"\x00"), raw...), nil
}
func contextHash(c Context) string {
	raw, e := canonicaljson.Encode(c, MaximumHeaderBytes)
	if e != nil {
		return ""
	}
	return canonicaljson.Digest(domain+"CONTEXT\x00", raw)
}
func ParseHeader(raw []byte, expected Expected, now time.Time) (Header, error) {
	var h Header
	if canonicaljson.DecodeExact(raw, &h, MaximumHeaderBytes) != nil || expected.verify(h, now) != nil || h.SignatureProfile != SignatureProfile || !ValidHash(h.SealedHash) {
		return h, ErrEnvelope
	}
	sealed, e := base64.RawURLEncoding.Strict().DecodeString(h.Sealed)
	if e != nil || len(sealed) <= sodium.SealBytes || len(sealed) > 4096 || base64.RawURLEncoding.EncodeToString(sealed) != h.Sealed || Hash(sealed) != h.SealedHash {
		return h, ErrEnvelope
	}
	signature, e := decode64(h.Signature, ed25519.SignatureSize)
	if e != nil {
		return h, ErrEnvelope
	}
	msg, e := signedBytes("HEADER_SIGNATURE", h.unsigned(), MaximumHeaderBytes)
	if e != nil || !ed25519.Verify(expected.SenderPublic, msg, signature) {
		return h, ErrEnvelope
	}
	return h, nil
}
