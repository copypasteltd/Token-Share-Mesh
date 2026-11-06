package rental_test

import (
	"strings"
	"testing"
	"time"

	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/rental"
	"p11.local/compute/shared/money"
)

func meterFixture() rental.MeterInput {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	bps := 500
	return rental.MeterInput{Session: "session_original", Generation: "12", Started: now, UpperBound: now.Add(time.Hour), HardDeadline: now.Add(2 * time.Hour), Now: now.Add(time.Hour), Budget: 500, Paid: 500, TermsRefs: []string{"terms_original"}, Price: pricing.Price{Version: pricing.Version, Currency: "CNY", Unit: pricing.Hour, Rounding: pricing.Rounding, HourRate: &pricing.Rate{NumeratorFen: 350, DenominatorUnits: 1}, CommissionBPS: &bps, ChannelCostOwner: "SELLER", FeePolicyVersion: "fee_original"}}
}
func available(in rental.MeterInput, id, seq string, begin, end time.Duration) rental.ConfirmedInterval {
	return rental.ConfirmedInterval{ID: id, Session: in.Session, Generation: in.Generation, Sequence: seq, Begins: in.Started.Add(begin), Ends: in.Started.Add(end), Confirmation: "confirmation_original", EvidenceHash: strings.Repeat("a", 64)}
}
func fault(in rental.MeterInput, id string, begin, end time.Duration) rental.ConfirmedFault {
	return rental.ConfirmedFault{ID: id, Session: in.Session, Generation: in.Generation, Begins: in.Started.Add(begin), Ends: in.Started.Add(end), Confirmation: "confirmation_fault", EvidenceHash: strings.Repeat("b", 64)}
}

func TestRentalExactHourlyCumulativeRoundingAndConfirmedRefund(t *testing.T) {
	in := meterFixture()
	a := available(in, "interval_first", "1", 0, 30*time.Minute)
	b := available(in, "interval_second", "2", 30*time.Minute, time.Hour)
	in.Available = []rental.ConfirmedInterval{b, a, a}
	in.TermsRefs = []string{"terms_renewal", "terms_original"}
	got, e := rental.Calculate(in)
	if e != nil || got.VerifiedMS != 3600000 || got.Charge != 350 || got.Refund != 150 || got.Outstanding != 0 || len(got.IntervalRefs) != 2 {
		t.Fatal("350/hour was rounded per minute/report or duplicated", got, e)
	}
	in.Available = []rental.ConfirmedInterval{a, b}
	reordered, e := rental.Calculate(in)
	if e != nil || reordered.ReceiptHash != got.ReceiptHash {
		t.Fatal("same confirmed history has unstable receipt", e)
	}
	in.Price.HourRate = &pricing.Rate{NumeratorFen: 1800, DenominatorUnits: 1}
	in.Budget = 300
	in.Paid = 300
	in.Available = []rental.ConfirmedInterval{available(in, "interval_fixture", "1", 0, 550000*time.Millisecond)}
	got, e = rental.Calculate(in)
	if e != nil || got.Charge != 275 || got.Refund != 25 {
		t.Fatal("technical minute fixture was not preserved exactly", got, e)
	}
	if _, e = meterFixture().Price.LegacyMinuteRate(); e == nil {
		t.Fatal("350/hour silently used integer minute schema")
	}
}

func TestRentalFaultUnionUnknownGapsAndSingleSubmillisecondFloor(t *testing.T) {
	in := meterFixture()
	in.Available = []rental.ConfirmedInterval{available(in, "interval_first", "1", 0, 10*time.Second), available(in, "interval_second", "2", 20*time.Second, 30*time.Second)}
	in.Faults = []rental.ConfirmedFault{fault(in, "fault_first", 2*time.Second, 7*time.Second), fault(in, "fault_second", 5*time.Second, 9*time.Second), fault(in, "fault_gap", 12*time.Second, 18*time.Second)}
	got, e := rental.Calculate(in)
	if e != nil || got.VerifiedMS != 13000 || got.Charge != 2 || got.Refund != 498 {
		t.Fatal("fault overlaps or unknown gap were billed", got, e)
	}
	in.Available = []rental.ConfirmedInterval{available(in, "interval_first", "1", 0, 600*time.Microsecond), available(in, "interval_second", "2", time.Millisecond, 1600*time.Microsecond)}
	in.Faults = nil
	got, e = rental.Calculate(in)
	if e != nil || got.VerifiedMS != 1 || got.DiscardedSubmillisecondNS != 200000 || got.Charge != 1 {
		t.Fatal("submillisecond floor/fee occurred per heartbeat", got, e)
	}
	in.Started = time.Time{}
	in.Available = nil
	got, e = rental.Calculate(in)
	if e != nil || got.Charge != 0 || got.VerifiedMS != 0 || got.Refund != in.Paid {
		t.Fatal("preparation was charged", got, e)
	}
}

func TestRentalCappedBigIntegerAmountAndUnpaidBalance(t *testing.T) {
	in := meterFixture()
	in.Budget = 11
	in.Paid = 7
	in.Price.HourRate = &pricing.Rate{NumeratorFen: money.MaxFen, DenominatorUnits: 1}
	in.UpperBound = in.Started.Add(24 * time.Hour)
	in.Now = in.UpperBound
	in.HardDeadline = in.UpperBound
	in.Available = []rental.ConfirmedInterval{available(in, "interval_original", "1", 0, 24*time.Hour)}
	got, e := rental.Calculate(in)
	if e != nil || got.Charge != 11 || got.Outstanding != 4 || got.Refund != 0 {
		t.Fatal("budget clamped after overflow or invented paid funds", got, e)
	}
	in.Budget = 0
	got, e = rental.Calculate(in)
	if e != nil || got.Charge != 0 || got.Refund != 7 {
		t.Fatal("zero confirmed bound", got, e)
	}
}

func TestRentalMeterRejectsForeignFutureConflictingOrUnconfirmedContext(t *testing.T) {
	changes := map[string]func(*rental.MeterInput){
		"other-session":  func(in *rental.MeterInput) { in.Available[0].Session = "session_other" },
		"old-generation": func(in *rental.MeterInput) { in.Available[0].Generation = "11" },
		"future":         func(in *rental.MeterInput) { in.Available[0].Ends = in.Now.Add(time.Nanosecond) },
		"before-start":   func(in *rental.MeterInput) { in.Available[0].Begins = in.Started.Add(-time.Nanosecond) },
		"unconfirmed":    func(in *rental.MeterInput) { in.Available[0].Confirmation = "" },
		"missing-hash":   func(in *rental.MeterInput) { in.Available[0].EvidenceHash = "" },
		"sequence-alias": func(in *rental.MeterInput) { in.Available[0].Sequence = "01" },
		"same-sequence-new-row": func(in *rental.MeterInput) {
			a := in.Available[0]
			a.ID = "interval_replacement"
			in.Available = append(in.Available, a)
		},
		"same-row-new-sequence": func(in *rental.MeterInput) {
			a := in.Available[0]
			a.Sequence = "2"
			in.Available = append(in.Available, a)
		},
		"different-sequence-overlap": func(in *rental.MeterInput) {
			in.Available = append(in.Available, available(*in, "interval_other", "2", time.Second, 12*time.Second))
		},
		"foreign-fault": func(in *rental.MeterInput) {
			in.Faults = []rental.ConfirmedFault{fault(*in, "fault_other", time.Second, 2*time.Second)}
			in.Faults[0].Generation = "13"
		},
		"duplicate-terms": func(in *rental.MeterInput) { in.TermsRefs = append(in.TermsRefs, in.TermsRefs[0]) },
		"negative-budget": func(in *rental.MeterInput) { in.Budget = -1 },
		"token-billing":   func(in *rental.MeterInput) { in.Price.Unit = pricing.MillionTokens },
		"future-upper":    func(in *rental.MeterInput) { in.UpperBound = in.Now.Add(time.Second) },
		"past-hard":       func(in *rental.MeterInput) { in.HardDeadline = in.UpperBound.Add(-time.Second) },
		"unknown-start":   func(in *rental.MeterInput) { in.Started = time.Time{} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			in := meterFixture()
			in.Available = []rental.ConfirmedInterval{available(in, "interval_original", "1", 0, 10*time.Second)}
			change(&in)
			if _, e := rental.Calculate(in); e == nil {
				t.Fatal("unqualified interval entered meter")
			}
		})
	}
}
