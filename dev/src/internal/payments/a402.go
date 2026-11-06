package payments

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Challenge struct {
	payment, prepay, amount string
	expires                 time.Time
}

func (c Challenge) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original payment challenge redacted]")
}
func (c Challenge) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (c Challenge) ValidAt(now time.Time) bool {
	return merchantNumber.MatchString(c.prepay) && c.payment != "" && now.Before(c.expires)
}

// NewChallenge is metadata-only. Its caller must first authenticate the buyer,
// read this exact original intent from durable storage and commit precreation.
// An UNKNOWN state cannot be converted to a fresh challenge or a new charge.
func NewChallenge(s Snapshot, i Intent, b Binding, now time.Time) (Challenge, error) {
	if !i.ValidAt(b, now) || s.Scope != i.Scope || s.Amount != i.Amount || s.State != "PENDING" || !merchantNumber.MatchString(s.PrepayReference) || s.ChannelReference != "" {
		return Challenge{}, ErrUnknown
	}
	yuan, e := Yuan(i.Amount)
	if e != nil {
		return Challenge{}, e
	}
	return Challenge{s.Scope.Payment, s.PrepayReference, yuan, i.PayBefore}, nil
}

// WriteChallenge emits the documented amtPaymentLink *prepay reference*. It is
// not trade_no, a native miniapp pay parameter, proof of funds or execution.
// The enclosing real server owns write deadlines, scope checks and request life.
func (c Challenge) WriteChallenge(w http.ResponseWriter, now time.Time) error {
	if w == nil || !c.ValidAt(now) {
		return ErrUnknown
	}
	raw, e := json.Marshal(map[string]any{"error": map[string]any{"type": "payment_required", "code": "payment_required", "message": "Payment required for the original approved service", "param": nil}, "payment_id": c.payment, "amount": c.amount, "currency": "CNY", "pay_before": c.expires.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return ErrUnknown
	}
	defer clear(raw)
	w.Header().Set("amtPaymentLink", c.prepay)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusPaymentRequired)
	n, e := w.Write(raw)
	if e != nil || n != len(raw) {
		return ErrUnknown
	}
	return nil
}
func ProofFromRequest(r *http.Request) (*Proof, error) {
	if r == nil {
		return nil, ErrProof
	}
	var encoded string
	count := 0
	for name, values := range r.Header {
		if strings.EqualFold(name, "Payment-Proof") {
			for _, v := range values {
				count++
				encoded = v
			}
		}
	}
	if count != 1 {
		return nil, ErrProof
	}
	return ParseProof(encoded)
}
