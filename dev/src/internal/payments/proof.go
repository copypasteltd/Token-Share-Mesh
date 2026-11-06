package payments

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"p11.local/compute/shared/strictjson"
)

type proofPayload struct {
	Protocol struct {
		PaymentProof string `json:"payment_proof"`
		TradeNo      string `json:"trade_no"`
	} `json:"protocol"`
	Method *struct {
		ClientSession string `json:"client_session,omitempty"`
	} `json:"method,omitempty"`
}
type proofState struct {
	mu     sync.Mutex
	raw    []byte
	closed bool
}
type Proof struct{ state *proofState }

func (p Proof) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[payment proof redacted]") }
func (p Proof) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (p *Proof) Close() {
	if p == nil || p.state == nil {
		return
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	p.state.closed = true
	clear(p.state.raw)
	p.state.raw = nil
}

func validProofPayload(v proofPayload) bool {
	if !paymentProof.MatchString(v.Protocol.PaymentProof) || !transactionNumber.MatchString(v.Protocol.TradeNo) {
		return false
	}
	if v.Method != nil {
		if len(v.Method.ClientSession) > 1024 || strings.ContainsAny(v.Method.ClientSession, "\x00\r\n") {
			return false
		}
		for _, c := range v.Method.ClientSession {
			if c < 33 || c > 126 {
				return false
			}
		}
	}
	return true
}

// ParseProof is strict Base64 JSON, not decryption or payment verification. The
// original client_session is preserved when supplied and never invented.
func ParseProof(header string) (*Proof, error) {
	if len(header) < 1 || len(header) > MaximumProofHeader || strings.ContainsAny(header, " \t\r\n\x00") {
		return nil, ErrProof
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(header)
	if e != nil {
		clear(raw)
		return nil, ErrProof
	}
	defer clear(raw)
	return decodeProof(raw)
}
func decodeProof(raw []byte) (*Proof, error) {
	var v proofPayload
	var fields map[string]json.RawMessage
	if len(raw) > 4096 || strictjson.Decode(raw, &fields, 4096) != nil || bytes.Equal(fields["method"], []byte("null")) || strictjson.Decode(raw, &v, 4096) != nil || !validProofPayload(v) {
		return nil, ErrProof
	}
	normalized, e := json.Marshal(v)
	if e != nil || len(normalized) > 4096 {
		clear(normalized)
		return nil, ErrProof
	}
	return &Proof{state: &proofState{raw: normalized}}, nil
}
func (p *Proof) payload() (proofPayload, error) {
	if p == nil || p.state == nil {
		return proofPayload{}, ErrProof
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	var v proofPayload
	if p.state.closed || strictjson.Decode(p.state.raw, &v, 4096) != nil || !validProofPayload(v) {
		return proofPayload{}, ErrProof
	}
	return v, nil
}
func (p *Proof) copyRaw() ([]byte, error) {
	if p == nil || p.state == nil {
		return nil, ErrProof
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	if p.state.closed {
		return nil, ErrProof
	}
	return append([]byte{}, p.state.raw...), nil
}
