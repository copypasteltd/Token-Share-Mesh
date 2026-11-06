package cipherstream

import (
	"encoding/binary"
	"p11.local/compute/gateway/core"
	"p11.local/compute/shared/canonicaljson"
	"strconv"
	"unicode/utf8"
)

const (
	HeadersRecord byte = 1
	DataRecord    byte = 2
	UsageRecord   byte = 3
	ErrorRecord   byte = 4
	EndRecord     byte = 5
	CancelRecord  byte = 6
)

type PrivateHeaders struct {
	Profile          string `json:"profile_id"`
	Method           string `json:"method,omitempty"`
	Path             string `json:"path,omitempty"`
	Status           int    `json:"status,omitempty"`
	ContentType      string `json:"content_type,omitempty"`
	AnthropicVersion string `json:"anthropic_version,omitempty"`
	Operation        string `json:"operation,omitempty"`
}
type PrivateUsage struct {
	Profile      string `json:"profile_id"`
	InputTokens  string `json:"input_tokens"`
	OutputTokens string `json:"output_tokens"`
	Source       string `json:"source"`
	Evidence     string `json:"evidence_id,omitempty"`
}
type PrivateError struct {
	Profile string `json:"profile_id"`
	Code    string `json:"code"`
	Detail  string `json:"detail"`
}
type PrivateCancel struct {
	Profile string `json:"profile_id"`
	Reason  string `json:"reason"`
}
type End struct {
	Profile     string `json:"profile_id"`
	Outcome     string `json:"outcome"`
	Records     string `json:"records_before_end"`
	DataRecords string `json:"data_records_before_end"`
	DataBytes   string `json:"data_bytes_before_end"`
	DataHash    string `json:"data_sha256"`
}

// Only protocol-visible allowed metadata is expressible. Local API tokens,
// platform credentials, arbitrary headers, URLs and file paths have no field.
func (h PrivateHeaders) validate(c Context) error {
	if h.Profile != "P11_PRIVATE_HEADERS_1" {
		return ErrRecord
	}
	if c.Purpose == "CONTROL" {
		if h.Operation != "CANCEL" || h.Method != "" || h.Path != "" || h.Status != 0 || h.ContentType != "" || h.AnthropicVersion != "" {
			return ErrRecord
		}
		return nil
	}
	if h.Operation != "" {
		return ErrRecord
	}
	switch h.ContentType {
	case "application/json", "text/event-stream":
	default:
		return ErrRecord
	}
	if c.Purpose == "INPUT" {
		path := ""
		switch c.APIProfile {
		case "P11_CHAT_TEXT_NATIVE_1":
			path = "/v1/chat/completions"
		case "P11_RESPONSES_TEXT_NATIVE_1":
			path = "/v1/responses"
		case "P11_MESSAGES_TEXT_NATIVE_1":
			path = "/v1/messages"
		}
		if path == "" || h.Path != path || h.Method != "POST" || h.Status != 0 || h.ContentType != "application/json" {
			return ErrRecord
		}
	} else if h.Status < 200 || h.Status > 599 || h.Method != "" || h.Path != "" {
		return ErrRecord
	}
	if c.APIProfile == "P11_MESSAGES_TEXT_NATIVE_1" && c.Purpose == "INPUT" {
		if h.AnthropicVersion != core.AnthropicVersion {
			return ErrRecord
		}
	} else if h.AnthropicVersion != "" {
		return ErrRecord
	}
	return nil
}
func (u PrivateUsage) validate() error {
	if u.Profile != "P11_PRIVATE_USAGE_1" {
		return ErrRecord
	}
	if _, e := Counter(u.InputTokens, true, 63); e != nil {
		return ErrRecord
	}
	if _, e := Counter(u.OutputTokens, true, 63); e != nil {
		return ErrRecord
	}
	// Transport authentication does not establish independent metering.
	if u.Source != "LOCAL_UNVERIFIED" || u.Evidence != "" {
		return ErrRecord
	}
	return nil
}

type sequenceState struct{ headers, usage, failed, cancel, ended bool }

func (s *sequenceState) accept(c Context, typ byte, payload []byte, final bool) (*End, error) {
	if s.ended || len(payload) > MaximumPayloadBytes || final != (typ == EndRecord) {
		return nil, ErrRecord
	}
	if !s.headers && typ != HeadersRecord {
		return nil, ErrRecord
	}
	switch typ {
	case HeadersRecord:
		var h PrivateHeaders
		if s.headers || canonicaljson.DecodeExact(payload, &h, 4096) != nil || h.validate(c) != nil {
			return nil, ErrRecord
		}
		s.headers = true
	case DataRecord:
		if c.Purpose == "CONTROL" || s.usage || s.failed || len(payload) == 0 {
			return nil, ErrRecord
		}
	case UsageRecord:
		var u PrivateUsage
		if c.Purpose != "OUTPUT" || s.usage || s.failed || canonicaljson.DecodeExact(payload, &u, 4096) != nil || u.validate() != nil {
			return nil, ErrRecord
		}
		s.usage = true
	case ErrorRecord:
		var e PrivateError
		if c.Purpose == "CONTROL" || s.failed || canonicaljson.DecodeExact(payload, &e, 8192) != nil || e.Profile != "P11_PRIVATE_ERROR_1" || len(e.Code) < 1 || len(e.Code) > 64 || len(e.Detail) > 4096 || !utf8.ValidString(e.Detail) {
			return nil, ErrRecord
		}
		s.failed = true
	case CancelRecord:
		var v PrivateCancel
		if c.Purpose != "CONTROL" || s.cancel || canonicaljson.DecodeExact(payload, &v, 4096) != nil || v.Profile != "P11_PRIVATE_CANCEL_1" {
			return nil, ErrRecord
		}
		switch v.Reason {
		case "BUYER_CANCELED", "SELLER_STOPPED", "PERMISSION_REVOKED":
		default:
			return nil, ErrRecord
		}
		s.cancel = true
	case EndRecord:
		var e End
		if canonicaljson.DecodeExact(payload, &e, 4096) != nil || e.Profile != "P11_PRIVATE_END_1" || !ValidHash(e.DataHash) {
			return nil, ErrRecord
		}
		switch e.Outcome {
		case "SUCCESS":
			if s.failed {
				return nil, ErrRecord
			}
		case "FAILED", "CANCELED":
		default:
			return nil, ErrRecord
		}
		if c.Purpose == "CONTROL" && (!s.cancel || e.Outcome != "CANCELED") {
			return nil, ErrRecord
		}
		for _, v := range []string{e.Records, e.DataRecords, e.DataBytes} {
			if _, err := Counter(v, true, 64); err != nil {
				return nil, ErrRecord
			}
		}
		s.ended = true
		return &e, nil
	default:
		return nil, ErrRecord
	}
	return nil, nil
}
func recordAAD(headerHash [32]byte, body uint32, sequence uint64) []byte {
	out := append([]byte(domain+"RECORD_AAD\x00"), headerHash[:]...)
	out = binary.BigEndian.AppendUint32(out, body)
	return binary.BigEndian.AppendUint64(out, sequence)
}
func innerRecord(typ byte, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	out[0] = typ
	binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[8:], payload)
	return out
}
func verifyEnd(e End, records, dataRecords uint64, dataBytes int64, dataHash string) error {
	if e.Records != strconv.FormatUint(records, 10) || e.DataRecords != strconv.FormatUint(dataRecords, 10) || e.DataBytes != strconv.FormatInt(dataBytes, 10) || e.DataHash != dataHash {
		return ErrRecord
	}
	return nil
}
