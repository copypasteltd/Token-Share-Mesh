package rental

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

const TermsProfile = "P11_RENTAL_CONFIRMED_TERMS_1"
const ReservedTerm = 90 * time.Second
const PreparingTerm = 120 * time.Second
const BuyerReadyTerm = 120 * time.Second
const BuyerDisconnectGrace = 60 * time.Second
const MaximumSessionDurationMS = int64(365 * 24 * time.Hour / time.Millisecond)

var ErrSession = errors.New("current original rental session transition unavailable")

// Session is a transaction-domain value, not an HTTP write DTO. It confers no
// physical ticket. Repositories must lock order -> session -> resource, resolve
// authenticated actors and corroborated facts, recheck original qualification
// and funding, and atomically persist Session, owner, grants and safe outbox.
// Until that integration exists this value engine cannot open public rentals.
type Session struct {
	Contract        Contract    `json:"contract"`
	ID              string      `json:"session_id"`
	Generation      string      `json:"resource_lock_generation"`
	Version         int64       `json:"version"`
	State           string      `json:"state"`
	Cleanup         string      `json:"cleanup_state"`
	Terms           int64       `json:"terms_revision"`
	TermsHash       string      `json:"terms_hash"`
	TermsRef        string      `json:"terms_ref"`
	Budget          money.Fen   `json:"confirmed_budget_fen"`
	Created         time.Time   `json:"created_at"`
	ReservedUntil   time.Time   `json:"reserved_until"`
	PrepareUntil    time.Time   `json:"prepare_until"`
	BuyerReadyUntil time.Time   `json:"buyer_ready_until"`
	Started         time.Time   `json:"started_at"`
	HardDeadline    time.Time   `json:"hard_deadline_at"`
	Stopped         time.Time   `json:"stopped_at"`
	DeviceReady     *ReadyProof `json:"device_ready,omitempty"`
	BuyerReady      *ReadyProof `json:"buyer_ready,omitempty"`
	StopReason      string      `json:"stop_reason,omitempty"`
}
type Contract struct {
	Order              string        `json:"order_id"`
	ContractHash       string        `json:"contract_hash"`
	Buyer              string        `json:"buyer_id"`
	Seller             string        `json:"seller_id"`
	Device             string        `json:"device_id"`
	Resource           string        `json:"resource_id"`
	Environment        string        `json:"environment_revision"`
	EnvironmentHash    string        `json:"kernel_environment_digest"`
	Manifest           string        `json:"manifest_ref"`
	ManifestHash       string        `json:"manifest_hash"`
	Price              pricing.Price `json:"price"`
	Budget             money.Fen     `json:"confirmed_budget_fen"`
	MaximumDurationMS  int64         `json:"max_duration_ms"`
	OriginalDeadline   time.Time     `json:"original_deadline"`
	DisconnectPolicy   string        `json:"disconnect_policy"`
	PaymentEnvironment string        `json:"payment_environment"`
}

// ResourceSnapshot comes from the current row locked in the SAME transaction.
// It is not a device's HTTP assertion or evidence of actual stopped GPU work.
type ResourceSnapshot struct{ Resource, Seller, Device, Generation, Phase, SessionOwner, AttemptOwner string }

func (r ResourceSnapshot) available(c Contract) bool {
	return r.Resource == c.Resource && r.Seller == c.Seller && r.Device == c.Device && r.Phase == "FREE" && r.SessionOwner == "" && r.AttemptOwner == ""
}
func (r ResourceSnapshot) owned(s Session) bool {
	if r.Phase != "RESERVED" && r.Phase != "ACTIVE" && r.Phase != "STOPPING" && r.Phase != "CLEANING" && r.Phase != "QUARANTINED" {
		return false
	}
	return r.Resource == s.Contract.Resource && r.Seller == s.Contract.Seller && r.Device == s.Contract.Device && r.Generation == s.Generation && r.SessionOwner == s.ID && r.AttemptOwner == ""
}

// Funding is a resolved original-order authorization. Validate IDs/times here;
// the repository must independently verify payment signatures/environment,
// refund/revocation/qualification state and actual available capacity. A caller
// supplied Funding or a valid-looking reference does not authorize money.
type Funding struct {
	Order, Buyer, Seller, Payment, Evidence, Environment string
	Amount                                               money.Fen
	ValidUntil                                           time.Time
}

func (f Funding) covers(c Contract, amount money.Fen, at, until time.Time) bool {
	return f.Order == c.Order && f.Buyer == c.Buyer && f.Seller == c.Seller && ids.Valid(f.Payment) && ids.Valid(f.Evidence) && f.Environment == c.PaymentEnvironment && (f.Environment == "SANDBOX" || f.Environment == "PRODUCTION") && f.Amount >= amount && amount > 0 && int64(f.Amount) <= money.MaxFen && boundedTime(f.ValidUntil) && at.Before(f.ValidUntil) && !until.After(f.ValidUntil)
}

type ReadyProof struct {
	Environment string    `json:"environment_revision"`
	Generation  string    `json:"resource_lock_generation"`
	Connection  string    `json:"connection_evidence_ref"`
	Readiness   string    `json:"readiness_evidence_ref"`
	At          time.Time `json:"recorded_at"`
}

func (p ReadyProof) matches(s Session, now time.Time) bool {
	return p.Environment == s.Contract.Environment && p.Generation == s.Generation && ids.Valid(p.Connection) && ids.Valid(p.Readiness) && boundedTime(p.At) && !p.At.Before(s.Created) && !p.At.After(now)
}

func (c Contract) valid(now time.Time) bool {
	for _, id := range []string{c.Order, c.Buyer, c.Seller, c.Device, c.Resource, c.Environment, c.Manifest} {
		if !ids.Valid(id) {
			return false
		}
	}
	return c.Buyer != c.Seller && executionlease.Hash(c.ContractHash) && executionlease.Hash(c.EnvironmentHash) && executionlease.Hash(c.ManifestHash) && c.Price.Validate() == nil && c.Price.Unit == pricing.Hour && c.Budget > 0 && int64(c.Budget) <= money.MaxFen && c.MaximumDurationMS > 0 && c.MaximumDurationMS <= MaximumSessionDurationMS && boundedTime(now) && boundedTime(c.OriginalDeadline) && now.Before(c.OriginalDeadline) && (c.DisconnectPolicy == "STOP_AFTER_GRACE" || c.DisconnectPolicy == "CONTINUE_WITHIN_CONFIRMED_LIMIT") && (c.PaymentEnvironment == "SANDBOX" || c.PaymentEnvironment == "PRODUCTION")
}
func minTime(values ...time.Time) time.Time {
	v := values[0]
	for _, x := range values[1:] {
		if x.Before(v) {
			v = x
		}
	}
	return v.Round(0).UTC()
}
func clonePrice(p pricing.Price) pricing.Price {
	if p.HourRate != nil {
		r := *p.HourRate
		p.HourRate = &r
	}
	if p.InputRate != nil {
		r := *p.InputRate
		p.InputRate = &r
	}
	if p.OutputRate != nil {
		r := *p.OutputRate
		p.OutputRate = &r
	}
	if p.CommissionBPS != nil {
		n := *p.CommissionBPS
		p.CommissionBPS = &n
	}
	return p
}
func (s Session) clone() Session {
	s.Contract.Price = clonePrice(s.Contract.Price)
	if s.DeviceReady != nil {
		p := *s.DeviceReady
		s.DeviceReady = &p
	}
	if s.BuyerReady != nil {
		p := *s.BuyerReady
		s.BuyerReady = &p
	}
	return s
}
func (s Session) revision(expected int64) error {
	g, e := strconv.ParseInt(s.Generation, 10, 64)
	if e != nil || g < 1 || strconv.FormatInt(g, 10) != s.Generation || !ids.Valid(s.ID) || !boundedTime(s.Created) || s.Version < 1 || s.Version != expected || s.Version == math.MaxInt64 || s.Terms < 1 || !ids.Valid(s.TermsRef) || !executionlease.Hash(s.TermsHash) {
		return ErrSession
	}
	return nil
}

// Reserve increments the platform generation only after an explicit FREE row.
// An expired/unknown/quarantined owner cannot be stolen by a later reservation.
func Reserve(id, termsRef string, c Contract, r ResourceSnapshot, now time.Time) (Session, error) {
	if !ids.Valid(id) || !ids.Valid(termsRef) || !c.valid(now) || !r.available(c) {
		return Session{}, ErrSession
	}
	g, e := strconv.ParseInt(r.Generation, 10, 64)
	if e != nil || g < 0 || g == math.MaxInt64 || strconv.FormatInt(g, 10) != r.Generation {
		return Session{}, ErrSession
	}
	c.Price = clonePrice(c.Price)
	s := Session{Contract: c, ID: id, Generation: strconv.FormatInt(g+1, 10), Version: 1, State: "RESERVED", Cleanup: "NOT_STARTED", Terms: 1, TermsRef: termsRef, Budget: c.Budget, Created: now.Round(0).UTC(), ReservedUntil: minTime(now.Add(ReservedTerm), c.OriginalDeadline)}
	terms := struct {
		Profile    string    `json:"profile_id"`
		Session    string    `json:"session_id"`
		Reference  string    `json:"terms_ref"`
		Revision   int64     `json:"terms_revision"`
		Contract   Contract  `json:"contract"`
		Generation string    `json:"resource_lock_generation"`
		Created    time.Time `json:"created_at"`
	}{TermsProfile, id, termsRef, 1, c, s.Generation, s.Created}
	b, e := json.Marshal(terms)
	if e != nil {
		return Session{}, ErrSession
	}
	h := sha256.Sum256(b)
	s.TermsHash = hex.EncodeToString(h[:])
	return s, nil
}

func BeginPreparing(s Session, expected int64, r ResourceSnapshot, now time.Time) (Session, error) {
	if s.revision(expected) != nil || s.State != "RESERVED" || !r.owned(s) || r.Phase != "RESERVED" || !boundedTime(now) || now.Before(s.Created) || !now.Before(s.ReservedUntil) {
		return Session{}, ErrSession
	}
	s = s.clone()
	s.State = "PREPARING"
	s.Version++
	s.PrepareUntil = minTime(now.Add(PreparingTerm), s.Contract.OriginalDeadline)
	return s, nil
}
func RecordDeviceReady(s Session, expected int64, r ResourceSnapshot, p ReadyProof, now time.Time) (Session, error) {
	if s.revision(expected) != nil || s.State != "PREPARING" || !r.owned(s) || r.Phase != "RESERVED" || !boundedTime(now) || !now.Before(s.PrepareUntil) || !p.matches(s, now) {
		return Session{}, ErrSession
	}
	s = s.clone()
	p.At = p.At.Round(0).UTC()
	s.DeviceReady = &p
	s.State = "READY_WAIT_BUYER"
	s.Version++
	s.BuyerReadyUntil = minTime(now.Add(BuyerReadyTerm), s.Contract.OriginalDeadline)
	return s, nil
}

// ConfirmBuyer is the sole start transition. Persist the unique started_at,
// active physical owner, history and safe start event in one transaction.
// Explicit access issuance subsequently rechecks that original ACTIVE version
// and atomically stores the grant; readiness alone can never invoke a kernel.
// expected_version prevents concurrent ready requests starting separate clocks.
func ConfirmBuyer(s Session, expected int64, r ResourceSnapshot, p ReadyProof, confirmedBudget money.Fen, f Funding, now time.Time) (Session, error) {
	if s.revision(expected) != nil || s.State != "READY_WAIT_BUYER" || !r.owned(s) || r.Phase != "RESERVED" || !s.Started.IsZero() || s.DeviceReady == nil || !p.matches(s, now) || !boundedTime(now) || !now.Before(s.BuyerReadyUntil) || confirmedBudget != s.Budget {
		return Session{}, ErrSession
	}
	ms, e := s.Contract.Price.AffordableHourlyMS(s.Contract.MaximumDurationMS, s.Budget)
	if e != nil || ms < 1 {
		return Session{}, ErrSession
	}
	end := minTime(now.Add(time.Duration(ms)*time.Millisecond), s.Contract.OriginalDeadline, f.ValidUntil)
	if !end.After(now) || !f.covers(s.Contract, s.Budget, now, end) {
		return Session{}, ErrSession
	}
	s = s.clone()
	p.At = p.At.Round(0).UTC()
	s.BuyerReady = &p
	s.State = "ACTIVE"
	s.Version++
	s.Started = now.Round(0).UTC()
	s.HardDeadline = end
	return s, nil
}

// Stop freezes the metering upper bound and revokes work without waiting for
// banking/refund completion. The caller MUST revoke every grant atomically.
// It does not release the physical lock or assert work has actually stopped.
func Stop(s Session, expected int64, r ResourceSnapshot, reason string, now time.Time) (Session, error) {
	if s.revision(expected) != nil || !r.owned(s) || !ids.Valid(reason) || !boundedTime(now) {
		return Session{}, ErrSession
	}
	if s.State == "CLOSED" || s.State == "STOPPING" || s.State == "CLEANING" || s.State == "QUARANTINED" {
		return s.clone(), nil
	}
	if s.State != "RESERVED" && s.State != "PREPARING" && s.State != "READY_WAIT_BUYER" && s.State != "ACTIVE" && s.State != "FAULTED" {
		return Session{}, ErrSession
	}
	s = s.clone()
	s.State = "STOPPING"
	s.Cleanup = "PENDING"
	s.StopReason = reason
	s.Version++
	if s.Started.IsZero() {
		s.Stopped = now.Round(0).UTC()
	} else {
		if now.Before(s.Started) {
			s.State = "QUARANTINED"
			s.StopReason = "CLOCK_UNSAFE"
			s.Stopped = s.Started
		} else {
			s.Stopped = minTime(now, s.HardDeadline)
		}
	}
	return s, nil
}

type Renewal struct {
	ID, Quote, Payment, Order, Buyer, Seller, EnvironmentHash string
	Price                                                     pricing.Price
	NewBudget                                                 money.Fen
	NewDeadline, ConfirmedAt                                  time.Time
}
type RenewalTerms struct {
	Profile         string        `json:"profile_id"`
	Reference       string        `json:"terms_ref"`
	Session         string        `json:"session_id"`
	Order           string        `json:"order_id"`
	Buyer           string        `json:"buyer_id"`
	Seller          string        `json:"seller_id"`
	Quote           string        `json:"quote_id"`
	Payment         string        `json:"payment_id"`
	PreviousHash    string        `json:"previous_terms_hash"`
	Revision        int64         `json:"terms_revision"`
	Budget          money.Fen     `json:"confirmed_total_budget_fen"`
	AddedBudget     money.Fen     `json:"added_budget_fen"`
	NewDeadline     time.Time     `json:"new_hard_deadline"`
	ConfirmedAt     time.Time     `json:"confirmed_at"`
	EnvironmentHash string        `json:"kernel_environment_digest"`
	Price           pricing.Price `json:"price"`
}

// ApplyRenewal assumes the new quote/payment are locked and consumed once by
// the repository. It creates a new linked terms snapshot, never mutates the
// original order, and requires old grants to be revoked in the same commit.
func ApplyRenewal(s Session, expected int64, r ResourceSnapshot, in Renewal, f Funding, now time.Time) (Session, RenewalTerms, error) {
	if s.revision(expected) != nil || s.State != "ACTIVE" || !r.owned(s) || r.Phase != "ACTIVE" || s.Terms == math.MaxInt64 || !boundedTime(now) || !now.Before(s.HardDeadline) {
		return Session{}, RenewalTerms{}, ErrSession
	}
	for _, id := range []string{in.ID, in.Quote, in.Payment} {
		if !ids.Valid(id) {
			return Session{}, RenewalTerms{}, ErrSession
		}
	}
	if in.ID == s.TermsRef || in.Order != s.Contract.Order || in.Buyer != s.Contract.Buyer || in.Seller != s.Contract.Seller || in.Payment != f.Payment || in.EnvironmentHash != s.Contract.EnvironmentHash || in.Price.Validate() != nil || !boundedTime(in.NewDeadline) || !in.NewDeadline.After(s.HardDeadline) || in.NewDeadline.Sub(s.Started) > time.Duration(MaximumSessionDurationMS)*time.Millisecond || !boundedTime(in.ConfirmedAt) || in.ConfirmedAt.After(now) || in.NewBudget <= s.Budget || int64(in.NewBudget) > money.MaxFen {
		return Session{}, RenewalTerms{}, ErrSession
	}
	original, _ := json.Marshal(s.Contract.Price)
	next, _ := json.Marshal(in.Price)
	if !bytes.Equal(original, next) || !f.covers(s.Contract, in.NewBudget-s.Budget, now, in.NewDeadline) {
		return Session{}, RenewalTerms{}, ErrSession
	}
	// Ceiling the requested span for admission prevents granting fractional
	// milliseconds beyond the confirmed budget; final metering floors once.
	span := in.NewDeadline.Sub(s.Started)
	requested := int64(span / time.Millisecond)
	if span%time.Millisecond != 0 {
		requested++
	}
	affordable, e := s.Contract.Price.AffordableHourlyMS(requested, in.NewBudget)
	if e != nil || affordable < requested {
		return Session{}, RenewalTerms{}, ErrSession
	}
	t := RenewalTerms{TermsProfile, in.ID, s.ID, s.Contract.Order, s.Contract.Buyer, s.Contract.Seller, in.Quote, in.Payment, s.TermsHash, s.Terms + 1, in.NewBudget, in.NewBudget - s.Budget, in.NewDeadline.Round(0).UTC(), in.ConfirmedAt.Round(0).UTC(), s.Contract.EnvironmentHash, clonePrice(in.Price)}
	data, e := json.Marshal(t)
	if e != nil {
		return Session{}, RenewalTerms{}, ErrSession
	}
	h := sha256.Sum256(data)
	s = s.clone()
	s.Version++
	s.Terms++
	s.TermsRef = in.ID
	s.TermsHash = hex.EncodeToString(h[:])
	s.Budget = in.NewBudget
	s.HardDeadline = in.NewDeadline.Round(0).UTC()
	return s, t, nil
}

// Tick uses server-observed disconnect state, not a device-selected timestamp.
// Platform detection latency does not extend the frozen billable upper bound.
// Native workers must independently enforce the signed local control window.
func Tick(s Session, r ResourceSnapshot, now time.Time, buyerDisconnectedAt *time.Time) (Session, error) {
	if !boundedTime(now) || !r.owned(s) {
		return Session{}, ErrSession
	}
	var cutoff time.Time
	reason := ""
	switch s.State {
	case "RESERVED":
		cutoff = s.ReservedUntil
		reason = "RESERVATION_EXPIRED"
	case "PREPARING":
		cutoff = s.PrepareUntil
		reason = "PREPARATION_EXPIRED"
	case "READY_WAIT_BUYER":
		cutoff = s.BuyerReadyUntil
		reason = "BUYER_READY_EXPIRED"
	case "ACTIVE":
		if now.Before(s.Started) {
			return Stop(s, s.Version, r, "CLOCK_UNSAFE", now)
		}
		cutoff = s.HardDeadline
		reason = "CONFIRMED_TERM_ENDED"
		if buyerDisconnectedAt != nil && s.Contract.DisconnectPolicy == "STOP_AFTER_GRACE" {
			if !boundedTime(*buyerDisconnectedAt) || buyerDisconnectedAt.Before(s.Started) || buyerDisconnectedAt.After(now) {
				return Session{}, ErrSession
			}
			grace := buyerDisconnectedAt.Add(BuyerDisconnectGrace)
			if grace.Before(cutoff) {
				cutoff = grace
				reason = "BUYER_DISCONNECTED"
			}
		}
	default:
		return s.clone(), nil
	}
	if cutoff.IsZero() {
		return Session{}, ErrSession
	}
	if now.Before(cutoff) {
		return s.clone(), nil
	}
	return Stop(s, s.Version, r, reason, cutoff)
}

type CleanupProof struct {
	Session, Generation, ManifestHash                               string
	WorkStopped, TokensRevoked, WorkspaceCleared, ResidualsVerified bool
}

// CloseAfterCleanup only returns a state transition. Actual stopped-work and
// residue checks must be corroborated before constructing this proof; neither
// TTL, HTTP close, missing PID nor the owner's assertion supplies those facts.
func CloseAfterCleanup(s Session, expected int64, r ResourceSnapshot, p CleanupProof) (Session, error) {
	if s.revision(expected) != nil || !r.owned(s) || (s.State != "STOPPING" && s.State != "CLEANING" && s.State != "QUARANTINED") || (r.Phase != "STOPPING" && r.Phase != "CLEANING" && r.Phase != "QUARANTINED") || p.Session != s.ID || p.Generation != s.Generation || !executionlease.Hash(p.ManifestHash) {
		return Session{}, ErrSession
	}
	s = s.clone()
	s.Version++
	if !p.WorkStopped || !p.TokensRevoked || !p.WorkspaceCleared || !p.ResidualsVerified {
		s.State = "QUARANTINED"
		s.Cleanup = "FAILED"
		return s, nil
	}
	s.State = "CLOSED"
	s.Cleanup = "ACKED"
	return s, nil
}
