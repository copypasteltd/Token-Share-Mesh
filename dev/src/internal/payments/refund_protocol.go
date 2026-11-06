package payments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

const RefundProfile = "P11_ALIPAY_ORIGINAL_REFUND_1"
const MinimumRefundSubmitInterval = 3 * time.Second
const MinimumRefundQueryInterval = 10 * time.Second

// RefundRoute is an independently approved bridge from this exact original
// payment binding to the two documented generic trade refund methods. Its
// existence in configuration is not proof of current database qualification.
// No Machine Pay capability, fee arrangement or seller authority is inferred.
type RefundRoute struct {
	Profile             string    `json:"profile"`
	Revision            string    `json:"revision"`
	OriginalBindingHash string    `json:"original_binding_hash"`
	EvidenceRef         string    `json:"evidence_ref"`
	EvidenceHash        string    `json:"evidence_hash"`
	ValidFrom           time.Time `json:"valid_from"`
	ValidUntil          time.Time `json:"valid_until"`
	ResolveUntil        time.Time `json:"resolve_until"`
}

func (r RefundRoute) ValidAt(b Binding, now time.Time, submit bool) bool {
	if r.Profile != RefundProfile || !reference.MatchString(r.Revision) || r.OriginalBindingHash != b.Digest() ||
		!ids.Valid(r.EvidenceRef) || !hashPattern.MatchString(r.EvidenceHash) || !b.ResolvableAt(now) ||
		r.ValidFrom.Before(b.ValidFrom) || !r.ValidUntil.After(r.ValidFrom) || r.ResolveUntil.Before(r.ValidUntil) ||
		r.ResolveUntil.After(b.ResolveUntil) || now.Before(r.ValidFrom) || !now.Before(r.ResolveUntil) {
		return false
	}
	return !submit || now.Before(r.ValidUntil)
}
func (r RefundRoute) Digest() string {
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (r RefundRoute) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[independently registered original refund route]")
}

type RefundScope struct {
	Original           Scope  `json:"original"`
	Refund             string `json:"refund_id"`
	OriginalRequestKey string `json:"original_request_key"`
	RouteHash          string `json:"route_hash"`
	DecisionRef        string `json:"decision_ref"`
	DecisionHash       string `json:"decision_hash"`
}

func (s RefundScope) Valid() bool {
	return s.Original.Valid() && ids.Valid(s.Refund) && merchantNumber.MatchString(s.OriginalRequestKey) &&
		hashPattern.MatchString(s.RouteHash) && reference.MatchString(s.DecisionRef) && hashPattern.MatchString(s.DecisionHash)
}
func (s RefundScope) Digest() string {
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RefundIntent freezes the ORIGINAL reserved refund after policy approval and
// cumulative reservation. It contains no arbitrary destination, reason text,
// model content, credentials, fee recipient or royalty instructions.
type RefundIntent struct {
	Profile          string          `json:"profile"`
	Scope            RefundScope     `json:"scope"`
	Amount           money.Fen       `json:"amount_fen"`
	Collected        money.Fen       `json:"collected_fen"`
	OriginalTrade    string          `json:"original_trade_no"`
	PaidEvidenceHash string          `json:"paid_evidence_hash"`
	Authority        RefundAuthority `json:"authority"`
	CreatedAt        time.Time       `json:"created_at"`
	SubmitBefore     time.Time       `json:"submit_before"`
	ResolveUntil     time.Time       `json:"resolve_until"`
}

// The caller must first derive policy authority independently, verify the
// ORIGINAL signed paid receipt, reserve via ReserveRefund under original
// order->payment->refund locks, then commit this immutable intent before IO.
// This constructor does not execute or persist that required transaction.
func NewRefundIntent(paid Snapshot, reserved Refund, authority RefundAuthority, route RefundRoute, b Binding, created, submitBefore time.Time) (RefundIntent, error) {
	i := RefundIntent{Profile: RefundProfile, Scope: RefundScope{Original: paid.Scope, Refund: reserved.ID, OriginalRequestKey: reserved.OriginalRequestKey,
		RouteHash: route.Digest(), DecisionRef: authority.Reference, DecisionHash: authority.EvidenceHash}, Amount: reserved.Amount, Collected: paid.Amount,
		OriginalTrade: paid.ChannelReference, PaidEvidenceHash: paid.EvidenceHash, Authority: authority,
		CreatedAt: created.UTC().Round(0), SubmitBefore: submitBefore.UTC().Round(0), ResolveUntil: route.ResolveUntil.UTC().Round(0)}
	if paid.State != "SETTLED" || paid.ObservedAt.IsZero() || created.Before(paid.ObservedAt) || reserved.State != "REQUESTED" ||
		reserved.Payment != paid.Scope.Payment || reserved.Order != paid.Scope.Order || !i.ValidAt(route, b, created, true) {
		return RefundIntent{}, ErrConflict
	}
	return i, nil
}
func (i RefundIntent) bound(route RefundRoute, b Binding) bool {
	return i.Profile == RefundProfile && i.Scope.Valid() && i.Scope.Original.BindingHash == b.Digest() && i.Scope.Original.Seller == b.Seller &&
		i.Scope.RouteHash == route.Digest() && i.Scope.DecisionRef == i.Authority.Reference && i.Scope.DecisionHash == i.Authority.EvidenceHash &&
		i.Authority.Payment == i.Scope.Original.Payment && i.Authority.Order == i.Scope.Original.Order &&
		i.Amount >= 1 && i.Amount <= i.Authority.Maximum && i.Authority.Maximum <= i.Collected && i.Collected >= 1 && i.Collected <= 5000 &&
		transactionNumber.MatchString(i.OriginalTrade) && hashPattern.MatchString(i.PaidEvidenceHash) &&
		!i.CreatedAt.IsZero() && i.SubmitBefore.After(i.CreatedAt) && !i.SubmitBefore.After(route.ValidUntil) &&
		i.ResolveUntil.Equal(route.ResolveUntil) && !i.ResolveUntil.Before(i.SubmitBefore) && route.ValidAt(b, i.CreatedAt, true)
}
func (i RefundIntent) ValidAt(route RefundRoute, b Binding, now time.Time, submit bool) bool {
	return i.bound(route, b) && !now.Before(i.CreatedAt) && now.Before(i.ResolveUntil) && route.ValidAt(b, now, submit) && (!submit || now.Before(i.SubmitBefore))
}
func (i RefundIntent) Digest() string {
	raw, _ := json.Marshal(i)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (i RefundIntent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[immutable original refund intent]")
}

// RefundObservation cannot be constructed by request JSON or another package.
// Only the native verifier can supply a query-confirmed own-refund fact.
type RefundObservation struct {
	scope                                RefundScope
	intentHash                           string
	amount                               money.Fen
	kind, method, evidenceHash, safeCode string
	cumulative                           money.Fen
	observed                             time.Time
	receipt                              *receiptState
}

func (o RefundObservation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[verified original refund observation]")
}
func (o RefundObservation) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (o RefundObservation) SafeCode() string             { return o.safeCode }
func (o RefundObservation) IsSucceeded() bool {
	return o.scope.Valid() && hashPattern.MatchString(o.intentHash) && o.amount > 0 &&
		o.kind == "REFUND_CONFIRMED" && o.method == refundQueryMethod && hashPattern.MatchString(o.evidenceHash) && !o.observed.IsZero()
}
func (o RefundObservation) Close() {
	if o.receipt != nil {
		o.receipt.mu.Lock()
		defer o.receipt.mu.Unlock()
		o.receipt.closed = true
		clear(o.receipt.raw)
		o.receipt.raw = nil
	}
}

type RefundSnapshot struct {
	Scope        RefundScope `json:"scope"`
	IntentHash   string      `json:"intent_hash"`
	Amount       money.Fen   `json:"amount_fen"`
	State        string      `json:"state"`
	EvidenceHash string      `json:"evidence_hash,omitempty"`
	ObservedAt   time.Time   `json:"observed_at,omitempty"`
	DispatchedAt time.Time   `json:"dispatched_at,omitempty"`
	NextQueryAt  time.Time   `json:"next_query_at,omitempty"`
	Generation   uint64      `json:"generation"`
}

func InitialRefundSnapshot(i RefundIntent) RefundSnapshot {
	return RefundSnapshot{Scope: i.Scope, IntentHash: i.Digest(), Amount: i.Amount, State: "REQUESTED"}
}
func (s RefundSnapshot) bound(i RefundIntent) bool {
	if !s.Scope.Valid() || s.Scope != i.Scope || s.IntentHash != i.Digest() || s.Amount != i.Amount || s.Generation > 9223372036854775807 {
		return false
	}
	if s.State == "REQUESTED" {
		return s.Generation == 0 && s.DispatchedAt.IsZero() && s.NextQueryAt.IsZero() && s.EvidenceHash == "" && s.ObservedAt.IsZero()
	}
	if s.Generation < 1 || s.DispatchedAt.IsZero() || s.DispatchedAt.Before(i.CreatedAt) || !s.DispatchedAt.Before(i.SubmitBefore) ||
		!s.ObservedAt.IsZero() && s.ObservedAt.Before(i.CreatedAt) {
		return false
	}
	switch s.State {
	case "SUCCEEDED":
		return hashPattern.MatchString(s.EvidenceHash) && !s.ObservedAt.IsZero() && s.NextQueryAt.IsZero()
	case "PROCESSING":
		if !hashPattern.MatchString(s.EvidenceHash) || s.ObservedAt.IsZero() {
			return false
		}
	case "UNKNOWN":
		if s.EvidenceHash != "" && (!hashPattern.MatchString(s.EvidenceHash) || s.ObservedAt.IsZero()) {
			return false
		}
	default:
		return false
	}
	return !s.NextQueryAt.Before(s.DispatchedAt.Add(MinimumRefundQueryInterval))
}

// PrepareRefundSubmit must be committed under the original parent/payment lock
// BEFORE invoking the financial channel. Restarted UNKNOWN/PROCESSING never
// emits another submit. A future same-number explicit retry needs independent
// policy, not inference from a timeout or a query missing refund_status.
func PrepareRefundSubmit(i RefundIntent, s RefundSnapshot, lastPaymentDispatch, now time.Time) (RefundSnapshot, error) {
	if !s.bound(i) || s.State != "REQUESTED" || !s.DispatchedAt.IsZero() || s.Generation >= 9223372036854775807 || now.Before(i.CreatedAt) || !now.Before(i.SubmitBefore) ||
		!lastPaymentDispatch.IsZero() && now.Before(lastPaymentDispatch.Add(MinimumRefundSubmitInterval)) {
		return s, ErrConflict
	}
	s.State = "UNKNOWN"
	s.DispatchedAt = now.UTC().Round(0)
	s.NextQueryAt = s.DispatchedAt.Add(MinimumRefundQueryInterval)
	s.Generation++
	return s, nil
}

// Query preparation is a separate bounded durable action, never a refund retry.
// Persist the new generation and next-query time before IO; a stale worker
// may archive a true late success but must not acknowledge another generation.
func PrepareRefundQuery(i RefundIntent, s RefundSnapshot, now time.Time) (RefundSnapshot, error) {
	if !s.bound(i) || s.State == "SUCCEEDED" || s.DispatchedAt.IsZero() || s.NextQueryAt.Before(s.DispatchedAt.Add(MinimumRefundQueryInterval)) ||
		now.Before(s.NextQueryAt) || !now.Before(i.ResolveUntil) || s.Generation >= 9223372036854775807 {
		return s, ErrConflict
	}
	s.NextQueryAt = now.UTC().Round(0).Add(MinimumRefundQueryInterval)
	s.Generation++
	return s, nil
}
func MergeRefund(i RefundIntent, old RefundSnapshot, o RefundObservation) (RefundSnapshot, error) {
	if !old.bound(i) || old.State == "REQUESTED" || o.scope != i.Scope || o.intentHash != i.Digest() || o.amount != i.Amount || o.observed.IsZero() || o.observed.Before(i.CreatedAt) {
		return old, ErrConflict
	}
	if old.State == "SUCCEEDED" {
		return old, nil
	}
	switch o.kind {
	case "REFUND_CONFIRMED":
		if !o.IsSucceeded() {
			return old, ErrConflict
		}
		old.State = "SUCCEEDED"
		old.EvidenceHash = o.evidenceHash
		old.NextQueryAt = time.Time{}
	case "REFUND_SUBMITTED", "REFUND_QUERY_UNRESOLVED":
		if !hashPattern.MatchString(o.evidenceHash) || o.receipt == nil {
			return old, ErrConflict
		}
		old.State = "PROCESSING"
		old.EvidenceHash = o.evidenceHash
	case "UNRESOLVED":
		old.State = "UNKNOWN"
	default:
		return old, ErrConflict
	}
	if o.observed.After(old.ObservedAt) {
		old.ObservedAt = o.observed
	}
	if old.State != "SUCCEEDED" {
		next := o.observed.Add(MinimumRefundQueryInterval)
		if next.After(old.NextQueryAt) {
			old.NextQueryAt = next
		}
	}
	return old, nil
}
