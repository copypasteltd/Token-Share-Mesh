// Package rental implements hourly rental domain rules. Calculations do not
// authenticate device reports, debit money, settle a seller or free a GPU.
package rental

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

const MeterProfile = "P11_RENTAL_CONFIRMED_UNION_MS_CEIL_1"
const MaximumIntervals = 65536

var ErrMeter = errors.New("original confirmed rental metering context unavailable")

// ConfirmedInterval must be loaded from the platform's accepted evidence,
// independently of a device's CLAIMED/UNKNOWN interval. A nonempty reference
// cannot establish that trust; the transaction/repository must resolve it.
type ConfirmedInterval struct {
	ID           string    `json:"interval_id"`
	Session      string    `json:"session_id"`
	Generation   string    `json:"resource_lock_generation"`
	Sequence     string    `json:"sequence"`
	Begins       time.Time `json:"starts_at"`
	Ends         time.Time `json:"ends_at"`
	Confirmation string    `json:"confirmation_ref"`
	EvidenceHash string    `json:"evidence_hash"`
}

// ConfirmedFault is an appended corroborated correction, not an edited report.
// Its original evidence stays in the calculation receipt even when it overlaps
// another fault. The service must never fill unknown availability gaps.
type ConfirmedFault struct {
	ID           string    `json:"fault_id"`
	Session      string    `json:"session_id"`
	Generation   string    `json:"resource_lock_generation"`
	Begins       time.Time `json:"starts_at"`
	Ends         time.Time `json:"ends_at"`
	Confirmation string    `json:"confirmation_ref"`
	EvidenceHash string    `json:"evidence_hash"`
}

type MeterInput struct {
	Session, Generation                    string
	Started, UpperBound, HardDeadline, Now time.Time
	Price                                  pricing.Price
	Budget, Paid                           money.Fen
	TermsRefs                              []string
	Available                              []ConfirmedInterval
	Faults                                 []ConfirmedFault
}

type MeterResult struct {
	Profile                   string    `json:"profile_id"`
	Session                   string    `json:"session_id"`
	Generation                string    `json:"resource_lock_generation"`
	VerifiedMS                int64     `json:"verified_billable_ms"`
	DiscardedSubmillisecondNS int64     `json:"discarded_submillisecond_ns"`
	Charge                    money.Fen `json:"charge_fen"`
	Paid                      money.Fen `json:"paid_fen"`
	Refund                    money.Fen `json:"refund_fen"`
	Outstanding               money.Fen `json:"outstanding_fen"`
	ReceiptHash               string    `json:"calculation_evidence_hash"`
	IntervalRefs              []string  `json:"interval_refs"`
	FaultRefs                 []string  `json:"fault_refs"`
	TermsRefs                 []string  `json:"terms_refs"`
}

func canonicalPositive(s string) bool {
	n, e := strconv.ParseUint(s, 10, 64)
	return e == nil && n > 0 && strconv.FormatUint(n, 10) == s
}
func boundedTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 2000 && t.Year() <= 2100 }
func period(begin, end, lower, upper time.Time) bool {
	return boundedTime(begin) && boundedTime(end) && end.After(begin) && !begin.Before(lower) && !end.After(upper)
}

type span struct{ begin, end time.Time }

func merge(values []span) []span {
	sort.Slice(values, func(i, j int) bool {
		if values[i].begin.Equal(values[j].begin) {
			return values[i].end.Before(values[j].end)
		}
		return values[i].begin.Before(values[j].begin)
	})
	var out []span
	for _, s := range values {
		if len(out) == 0 || s.begin.After(out[len(out)-1].end) {
			out = append(out, s)
			continue
		}
		if s.end.After(out[len(out)-1].end) {
			out[len(out)-1].end = s.end
		}
	}
	return out
}
func subtract(available, faults []span) []span {
	var out []span
	j := 0
	for _, a := range available {
		start := a.begin
		for j < len(faults) && !faults[j].end.After(start) {
			j++
		}
		for k := j; k < len(faults) && faults[k].begin.Before(a.end); k++ {
			f := faults[k]
			if f.begin.After(start) {
				out = append(out, span{start, f.begin})
			}
			if f.end.After(start) {
				start = f.end
			}
			if !start.Before(a.end) {
				break
			}
		}
		if start.Before(a.end) {
			out = append(out, span{start, a.end})
		}
	}
	return out
}

// Calculate merges the complete accepted history, subtracts confirmed faults,
// floors nanoseconds to milliseconds ONCE and applies the exact NB hourly rate
// ONCE across same-price renewals. It never rounds each report or media result.
// Current policy forbids price/fee/environment changes within a session; callers
// have to resolve all APPLIED terms against the same immutable Price first.
func Calculate(in MeterInput) (MeterResult, error) {
	if !ids.Valid(in.Session) || !canonicalPositive(in.Generation) || in.Price.Validate() != nil || in.Price.Unit != pricing.Hour ||
		!boundedTime(in.Now) || !boundedTime(in.UpperBound) || !boundedTime(in.HardDeadline) || in.UpperBound.After(in.Now) || in.UpperBound.After(in.HardDeadline) ||
		len(in.Available) > MaximumIntervals || len(in.Faults) > MaximumIntervals || len(in.TermsRefs) < 1 || len(in.TermsRefs) > MaximumIntervals {
		return MeterResult{}, ErrMeter
	}
	for _, value := range []money.Fen{in.Budget, in.Paid} {
		if _, e := money.New(int64(value)); e != nil {
			return MeterResult{}, ErrMeter
		}
	}
	terms := slices.Clone(in.TermsRefs)
	sort.Strings(terms)
	for i, id := range terms {
		if !ids.Valid(id) || i > 0 && terms[i-1] == id {
			return MeterResult{}, ErrMeter
		}
	}
	if in.Started.IsZero() {
		if len(in.Available) > 0 || len(in.Faults) > 0 {
			return MeterResult{}, ErrMeter
		}
	} else if !boundedTime(in.Started) || in.Started.After(in.UpperBound) {
		return MeterResult{}, ErrMeter
	}
	available := slices.Clone(in.Available)
	faults := slices.Clone(in.Faults)
	// Stable sort receipts by canonical decimal sequence without float parsing.
	sort.Slice(available, func(i, j int) bool {
		a, _ := strconv.ParseUint(available[i].Sequence, 10, 64)
		b, _ := strconv.ParseUint(available[j].Sequence, 10, 64)
		return a < b
	})
	sort.Slice(faults, func(i, j int) bool { return faults[i].ID < faults[j].ID })
	var aSpans, fSpans []span
	seenID := map[string]ConfirmedInterval{}
	seenSeq := map[string]ConfirmedInterval{}
	cleanAvailable := make([]ConfirmedInterval, 0, len(available))
	intervalRefs := []string{}
	for _, a := range available {
		if !ids.Valid(a.ID) || a.Session != in.Session || a.Generation != in.Generation || !canonicalPositive(a.Sequence) || !ids.Valid(a.Confirmation) || !executionlease.Hash(a.EvidenceHash) || !period(a.Begins, a.Ends, in.Started, in.UpperBound) {
			return MeterResult{}, ErrMeter
		}
		a.Begins = a.Begins.Round(0).UTC()
		a.Ends = a.Ends.Round(0).UTC()
		if prior, ok := seenID[a.ID]; ok {
			if prior != a {
				return MeterResult{}, ErrMeter
			}
			continue
		}
		if _, ok := seenSeq[a.Sequence]; ok {
			return MeterResult{}, ErrMeter
		}
		seenID[a.ID] = a
		seenSeq[a.Sequence] = a
		// Accepted platform rows must be chronologically monotone and cannot
		// overlap. Union below also makes adjacent accepted spans one duration.
		if len(aSpans) > 0 && a.Begins.Before(aSpans[len(aSpans)-1].end) {
			return MeterResult{}, ErrMeter
		}
		aSpans = append(aSpans, span{a.Begins, a.Ends})
		cleanAvailable = append(cleanAvailable, a)
		intervalRefs = append(intervalRefs, a.ID)
	}
	seenFault := map[string]ConfirmedFault{}
	cleanFaults := make([]ConfirmedFault, 0, len(faults))
	faultRefs := []string{}
	for _, f := range faults {
		if !ids.Valid(f.ID) || f.Session != in.Session || f.Generation != in.Generation || !ids.Valid(f.Confirmation) || !executionlease.Hash(f.EvidenceHash) || !period(f.Begins, f.Ends, in.Started, in.UpperBound) {
			return MeterResult{}, ErrMeter
		}
		f.Begins = f.Begins.Round(0).UTC()
		f.Ends = f.Ends.Round(0).UTC()
		if prior, ok := seenFault[f.ID]; ok {
			if prior != f {
				return MeterResult{}, ErrMeter
			}
			continue
		}
		seenFault[f.ID] = f
		fSpans = append(fSpans, span{f.Begins, f.Ends})
		cleanFaults = append(cleanFaults, f)
		faultRefs = append(faultRefs, f.ID)
	}
	var ns int64
	for _, s := range subtract(merge(aSpans), merge(fSpans)) {
		ns += s.end.Sub(s.begin).Nanoseconds()
	}
	ms := ns / int64(time.Millisecond)
	charge, e := in.Price.AmountCapped(pricing.Usage{BillableMS: &ms}, in.Budget)
	if e != nil {
		return MeterResult{}, ErrMeter
	}
	refund, outstanding := money.Fen(0), money.Fen(0)
	if in.Paid > charge {
		refund = in.Paid - charge
	} else {
		outstanding = charge - in.Paid
	}
	result := MeterResult{Profile: MeterProfile, Session: in.Session, Generation: in.Generation, VerifiedMS: ms, DiscardedSubmillisecondNS: ns % int64(time.Millisecond), Charge: charge, Paid: in.Paid, Refund: refund, Outstanding: outstanding, IntervalRefs: intervalRefs, FaultRefs: faultRefs, TermsRefs: terms}
	// This is a deterministic receipt digest, not a new funding/availability
	// signature. Persist its full metadata and revision in an append-only record.
	receipt := struct {
		Profile    string              `json:"profile_id"`
		Session    string              `json:"session_id"`
		Generation string              `json:"resource_lock_generation"`
		Started    time.Time           `json:"started_at"`
		Upper      time.Time           `json:"upper_bound"`
		Hard       time.Time           `json:"hard_deadline"`
		Price      pricing.Price       `json:"price"`
		Budget     money.Fen           `json:"budget_fen"`
		Paid       money.Fen           `json:"paid_fen"`
		Terms      []string            `json:"terms_refs"`
		Available  []ConfirmedInterval `json:"confirmed_intervals"`
		Faults     []ConfirmedFault    `json:"confirmed_faults"`
		Result     MeterResult         `json:"result"`
	}{MeterProfile, in.Session, in.Generation, in.Started.Round(0).UTC(), in.UpperBound.Round(0).UTC(), in.HardDeadline.Round(0).UTC(), in.Price, in.Budget, in.Paid, terms, cleanAvailable, cleanFaults, result}
	raw, e := json.Marshal(receipt)
	if e != nil {
		return MeterResult{}, ErrMeter
	}
	h := sha256.Sum256(raw)
	result.ReceiptHash = hex.EncodeToString(h[:])
	return result, nil
}
