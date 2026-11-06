// Package payments implements original C2C Machine Pay boundaries. Channel
// payment facts never constitute accepted delivery, GPU readiness or a wallet.
package payments

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"p11.local/compute/internal/fundguard"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

const Profile = fundguard.ChannelProfile
const MaximumProofHeader = 16384
const MaximumChannelResponse = 65536

var ErrBinding = errors.New("current original seller payment binding required")
var ErrIntent = errors.New("original bounded payment intent required")
var ErrProof = errors.New("original payment proof unavailable")
var ErrUnknown = errors.New("channel outcome unknown; resolve the original request")
var ErrConflict = errors.New("original payment facts conflict; retain obligations")
var ErrUnsupported = errors.New("channel capability not approved")
var reference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var merchantNumber = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
var transactionNumber = regexp.MustCompile(`^[0-9]{32}$`)
var paymentProof = regexp.MustCompile(`^[A-Za-z0-9]{64}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Binding is independently administered configuration, not a buyer request or
// proof field. Evidence references/hashes must come from an authorized registry.
// Constructing a value does not prove that Alipay approved a personal seller.
type Binding struct {
	Profile, Revision, Provider, Environment           string
	Seller, AppID, ServiceID, AuthoritySubject         string
	CredentialRef, AuthorizationRef, AuthorizationHash string
	QualificationRef, QualificationHash                string
	SigningKeyHash, AlipayKeyHash                      string
	ThirdParty                                         bool
	AppAuthTokenRef                                    string
	ValidFrom, ValidUntil, ResolveUntil                time.Time
}

func (b Binding) ValidAt(now time.Time) bool {
	if b.Profile != Profile || b.Provider != "ALIPAY" || (b.Environment != "SANDBOX" && b.Environment != "PRODUCTION") ||
		!reference.MatchString(b.Revision) || !ids.Valid(b.Seller) || !reference.MatchString(b.ServiceID) || len(b.ServiceID) > 32 ||
		!reference.MatchString(b.AuthoritySubject) || !reference.MatchString(b.CredentialRef) ||
		!reference.MatchString(b.AuthorizationRef) || !hashPattern.MatchString(b.AuthorizationHash) ||
		!reference.MatchString(b.QualificationRef) || !hashPattern.MatchString(b.QualificationHash) ||
		!hashPattern.MatchString(b.SigningKeyHash) || !hashPattern.MatchString(b.AlipayKeyHash) ||
		len(b.AppID) < 8 || len(b.AppID) > 32 || !b.ValidUntil.After(b.ValidFrom) || b.ResolveUntil.Before(b.ValidUntil) || b.ResolveUntil.After(b.ValidUntil.Add(30*24*time.Hour)) || now.Before(b.ValidFrom) || !now.Before(b.ValidUntil) {
		return false
	}
	for _, c := range b.AppID {
		if c < '0' || c > '9' {
			return false
		}
	}
	return b.ThirdParty && reference.MatchString(b.AppAuthTokenRef) || !b.ThirdParty && b.AppAuthTokenRef == ""
}

// Original financial resolution has its own approved horizon. It does not
// grant new sales or inference after ValidUntil and must not outlive authority.
func (b Binding) ResolvableAt(now time.Time) bool {
	return b.ValidAt(b.ValidFrom) && !now.Before(b.ValidFrom) && now.Before(b.ResolveUntil)
}
func (b Binding) Digest() string {
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (b Binding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original seller payment binding]")
}

type Scope struct {
	Payment, Order, Buyer, Seller, Purpose, PurposeRef string
	ContractHash, BindingHash, OutTradeNo              string
}

func (s Scope) Valid() bool {
	for _, id := range []string{s.Payment, s.Order, s.Buyer, s.Seller, s.PurposeRef} {
		if !ids.Valid(id) {
			return false
		}
	}
	return s.Buyer != s.Seller && (s.Purpose == "ORDER" && s.PurposeRef == s.Order || s.Purpose == "RENTAL_RENEWAL") &&
		hashPattern.MatchString(s.ContractHash) && hashPattern.MatchString(s.BindingHash) && merchantNumber.MatchString(s.OutTradeNo)
}
func (s Scope) Digest() string {
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Intent contains public contract metadata only. GoodsName must be taken from
// the approved service, never prompts, asset names or client-supplied secrets.
type Intent struct {
	Scope                                  Scope
	Amount                                 money.Fen
	GoodsName                              string
	CreatedAt, PayBefore, ContractDeadline time.Time
}

func (i Intent) ValidAt(b Binding, now time.Time) bool {
	return i.Scope.Valid() && b.ValidAt(now) && i.Scope.Seller == b.Seller && i.Scope.BindingHash == b.Digest() &&
		i.Amount >= 1 && i.Amount <= 5000 && utf8.ValidString(i.GoodsName) && utf8.RuneCountInString(i.GoodsName) >= 1 && utf8.RuneCountInString(i.GoodsName) <= 32 &&
		!strings.ContainsAny(i.GoodsName, "\x00\r\n") && !i.CreatedAt.IsZero() && !now.Before(i.CreatedAt) && i.ContractDeadline.After(now) && i.PayBefore.After(now) &&
		!i.PayBefore.After(i.ContractDeadline) && !i.PayBefore.After(b.ValidUntil) && !i.PayBefore.After(i.CreatedAt.Add(12*time.Hour))
}
func (i Intent) BoundTo(b Binding) bool { return i.ValidAt(b, i.CreatedAt) }
func (i Intent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original bounded payment intent]")
}

// NewOutTradeNo creates a full original 32-character number; it never truncates
// an order ID. It must be persisted with the original intent before any I/O.
func NewOutTradeNo() (string, error) {
	raw := make([]byte, 15)
	defer clear(raw)
	if _, e := rand.Read(raw); e != nil {
		return "", ErrIntent
	}
	return "MP" + hex.EncodeToString(raw), nil
}
func Yuan(f money.Fen) (string, error) {
	if f < 1 || f > 5000 {
		return "", ErrIntent
	}
	return fmt.Sprintf("%d.%02d", int64(f)/100, int64(f)%100), nil
}
func ParseYuan(raw string) (money.Fen, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || len(parts[0]) < 1 || len(parts[0]) > 2 || len(parts[1]) != 2 || len(parts[0]) > 1 && parts[0][0] == '0' {
		return 0, ErrIntent
	}
	var n int64
	for _, c := range parts[0] + parts[1] {
		if c < '0' || c > '9' {
			return 0, ErrIntent
		}
		n = n*10 + int64(c-'0')
	}
	if n < 1 || n > 5000 {
		return 0, ErrIntent
	}
	return money.Fen(n), nil
}

// Observation can only be constructed by this package's channel adapter. Raw
// request bodies, redirect parameters and JSON cannot create a SETTLED fact.
type Observation struct {
	scope                                       Scope
	amount                                      money.Fen
	kind, trade, prepay, evidenceHash, safeCode string
	observed                                    time.Time
	receipt                                     *receiptState
}

func (o Observation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[verified channel observation]")
}
func (o Observation) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (o Observation) SafeCode() string             { return o.safeCode }
func (o Observation) IsSettled() bool {
	return o.kind == "VERIFIED_PAID" && o.trade != "" && o.amount > 0 && hashPattern.MatchString(o.evidenceHash)
}

type Snapshot struct {
	Scope            Scope     `json:"scope"`
	Amount           money.Fen `json:"amount_fen"`
	State            string    `json:"state"`
	ChannelReference string    `json:"channel_reference,omitempty"`
	PrepayReference  string    `json:"prepay_reference,omitempty"`
	EvidenceHash     string    `json:"evidence_hash,omitempty"`
	ObservedAt       time.Time `json:"observed_at,omitempty"`
}

// Merge records late paid facts even after local expiry/closure; it never
// resurrects execution or extends a contract. Late funds require resolution.
func Merge(old Snapshot, o Observation) (Snapshot, error) {
	if !old.Scope.Valid() || old.Scope != o.scope || old.Amount != o.amount || o.observed.IsZero() {
		return old, ErrConflict
	}
	if old.State != "CREATED" && old.State != "PENDING" && old.State != "UNKNOWN" && old.State != "SETTLED" && old.State != "CLOSED" && old.State != "FAILED" {
		return old, ErrConflict
	}
	if o.kind == "VERIFIED_PAID" {
		if !o.IsSettled() || !transactionNumber.MatchString(o.trade) || old.ChannelReference != "" && old.ChannelReference != o.trade {
			return old, ErrConflict
		}
		old.State = "SETTLED"
		old.ChannelReference = o.trade
		old.EvidenceHash = o.evidenceHash
		old.ObservedAt = o.observed
		return old, nil
	}
	if old.State == "SETTLED" {
		return old, nil
	}
	switch o.kind {
	case "PRECREATED":
		if !merchantNumber.MatchString(o.prepay) || old.PrepayReference != "" && old.PrepayReference != o.prepay {
			return old, ErrConflict
		}
		if old.State == "CLOSED" || old.State == "FAILED" {
			return old, ErrConflict
		}
		old.State = "PENDING"
		old.PrepayReference = o.prepay
	case "UNRESOLVED":
		if old.State != "CLOSED" && old.State != "FAILED" {
			old.State = "UNKNOWN"
		}
	default:
		return old, ErrConflict
	}
	old.ObservedAt = o.observed
	return old, nil
}

type FundingCheck struct {
	Scope                                                        Scope
	Minimum                                                      money.Fen
	HardDeadline                                                 time.Time
	CurrentBuyer, CurrentSeller, CurrentDevice, OriginalContract bool
	RefundsResolved                                              bool
	RefundHeld                                                   money.Fen
}

func CanFund(v Snapshot, b Binding, c FundingCheck, now time.Time) bool {
	return v.Scope == c.Scope && v.Scope.Valid() && v.State == "SETTLED" && transactionNumber.MatchString(v.ChannelReference) && hashPattern.MatchString(v.EvidenceHash) &&
		b.ValidAt(now) && v.Scope.BindingHash == b.Digest() && v.Scope.Seller == b.Seller && c.Minimum > 0 && v.Amount >= c.Minimum && v.Amount <= 5000 &&
		c.RefundsResolved && c.RefundHeld == 0 && c.CurrentBuyer && c.CurrentSeller && c.CurrentDevice && c.OriginalContract && now.Before(c.HardDeadline)
}
