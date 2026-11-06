package payments

// SOURCE_ONLY: domain and signed synthetic transport definitions, not evidence
// of actual cancellation policy, channel qualification or database locking.
import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"p11.local/compute/shared/money"
)

func dispatchedRefund(t *testing.T, r *refundFixture) RefundSnapshot {
	t.Helper()
	s, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), time.Time{}, r.payment.now)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestOriginalRefundIntentRejectsUnpaidAndUnapprovedReservation(t *testing.T) {
	r := newRefundFixture(t, nil)
	for _, variant := range []string{"unpaid", "other_payment", "unapproved_amount", "unknown_reservation", "paid_evidence", "created_before_paid", "unbounded_submit"} {
		t.Run(variant, func(t *testing.T) {
			paid := paidSnapshot(r.payment)
			paid.Amount = r.intent.Collected
			reserved := Refund{ID: r.intent.Scope.Refund, Payment: paid.Scope.Payment, Order: paid.Scope.Order, OriginalRequestKey: r.intent.Scope.OriginalRequestKey, Amount: r.intent.Amount, State: "REQUESTED"}
			authority := r.intent.Authority
			created, before := r.intent.CreatedAt, r.intent.SubmitBefore
			switch variant {
			case "unpaid":
				paid.State = "PENDING"
			case "other_payment":
				reserved.Payment = "another_payment"
			case "unapproved_amount":
				authority.Maximum = r.intent.Amount - 1
			case "unknown_reservation":
				reserved.State = "UNKNOWN"
			case "paid_evidence":
				paid.EvidenceHash = ""
			case "created_before_paid":
				created = created.Add(-time.Microsecond)
			case "unbounded_submit":
				before = r.route.ValidUntil.Add(time.Second)
			}
			if _, e := NewRefundIntent(paid, reserved, authority, r.route, r.payment.binding, created, before); e == nil {
				t.Fatal("unpaid/foreign/unapproved original refund frozen")
			}
		})
	}
}

func TestOriginalRefundIntentMutationRejectsBeforeAnyChannelIO(t *testing.T) {
	r := newRefundFixture(t, nil)
	for _, variant := range []string{"buyer", "seller", "order", "binding", "route", "decision", "decision_hash", "amount", "trade", "expiry"} {
		t.Run(variant, func(t *testing.T) {
			i := r.intent
			switch variant {
			case "buyer":
				i.Scope.Original.Buyer = ""
			case "seller":
				i.Scope.Original.Seller = "other_seller"
			case "order":
				i.Scope.Original.Order = "another_order"
			case "binding":
				i.Scope.Original.BindingHash = strings.Repeat("0", 64)
			case "route":
				i.Scope.RouteHash = strings.Repeat("0", 64)
			case "decision":
				i.Scope.DecisionRef = "other_policy"
			case "decision_hash":
				i.Scope.DecisionHash = strings.Repeat("0", 64)
			case "amount":
				i.Amount = i.Collected + 1
			case "trade":
				i.OriginalTrade = "other_trade"
			case "expiry":
				i.ResolveUntil = i.ResolveUntil.Add(time.Second)
			}
			o, e := r.channel.Submit(context.Background(), i)
			defer o.Close()
			if e == nil {
				t.Fatal("modified frozen original intent admitted")
			}
		})
	}
	if r.calls.Load() != 0 {
		t.Fatal("invalid intent reached financial channel")
	}
}

func TestOriginalRefundSubmitPreparationCommitsUnknownOnce(t *testing.T) {
	r := newRefundFixture(t, nil)
	first := dispatchedRefund(t, r)
	if first.State != "UNKNOWN" || first.Generation != 1 || !first.DispatchedAt.Equal(r.payment.now) || !first.NextQueryAt.Equal(r.payment.now.Add(10*time.Second)) {
		t.Fatal("pre-IO state did not reserve unknown original dispatch")
	}
	again, e := PrepareRefundSubmit(r.intent, first, time.Time{}, r.payment.now.Add(20*time.Second))
	if e == nil || again != first {
		t.Fatal("recovered UNKNOWN became a new refund submit")
	}
	if r.calls.Load() != 0 {
		t.Fatal("domain dispatch preparation invoked money transfer")
	}
}

func TestOriginalRefundDifferentPartialsShareThreeSecondSubmitBoundary(t *testing.T) {
	r := newRefundFixture(t, nil)
	last := r.payment.now
	if _, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), last, last.Add(3*time.Second-time.Microsecond)); e == nil {
		t.Fatal("same trade's partial refunds submitted too close together")
	}
	if _, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), last, last.Add(3*time.Second)); e != nil {
		t.Fatal("exact original-payment serialization boundary rejected")
	}
	if _, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), last.Add(time.Second), last); e == nil {
		t.Fatal("clock reversal bypassed payment dispatch watermark")
	}
}

func TestOriginalRefundQueriesAreSeparateTenSecondGenerations(t *testing.T) {
	r := newRefundFixture(t, nil)
	s := dispatchedRefund(t, r)
	if _, e := PrepareRefundQuery(r.intent, s, r.payment.now.Add(10*time.Second-time.Microsecond)); e == nil {
		t.Fatal("query admitted before documented earliest result boundary")
	}
	next, e := PrepareRefundQuery(r.intent, s, r.payment.now.Add(10*time.Second))
	if e != nil || next.Generation != 2 || !next.NextQueryAt.Equal(r.payment.now.Add(20*time.Second)) || next.DispatchedAt != s.DispatchedAt {
		t.Fatal("query replaced original dispatch or failed generation advance")
	}
	if _, e := PrepareRefundQuery(r.intent, next, r.payment.now.Add(10*time.Second)); e == nil {
		t.Fatal("same query generation replay admitted immediately")
	}
	if r.calls.Load() != 0 {
		t.Fatal("query scheduling performed external IO")
	}
}

func TestOriginalRefundPlannerRejectsMissingDispatchTamperAndOverflow(t *testing.T) {
	r := newRefundFixture(t, nil)
	if _, e := PrepareRefundQuery(r.intent, InitialRefundSnapshot(r.intent), r.payment.now.Add(time.Minute)); e == nil {
		t.Fatal("never-dispatched refund was queried")
	}
	for _, variant := range []string{"scope", "amount", "intent", "generation", "next_query", "dispatch_before_creation", "manufactured_final"} {
		t.Run(variant, func(t *testing.T) {
			s := dispatchedRefund(t, r)
			switch variant {
			case "scope":
				s.Scope.Refund = "other_refund"
			case "amount":
				s.Amount++
			case "intent":
				s.IntentHash = strings.Repeat("0", 64)
			case "generation":
				s.Generation = 9223372036854775807
			case "next_query":
				s.NextQueryAt = s.DispatchedAt
			case "dispatch_before_creation":
				s.DispatchedAt = r.intent.CreatedAt.Add(-time.Second)
			case "manufactured_final":
				s.State = "SUCCEEDED"
				s.NextQueryAt = time.Time{}
			}
			if _, e := PrepareRefundQuery(r.intent, s, r.payment.now.Add(time.Minute)); e == nil {
				t.Fatal("damaged original durable state queued financial work")
			}
		})
	}
}

func TestOriginalRefundPlannerExpiresWithoutReleasingOriginalCapacity(t *testing.T) {
	r := newRefundFixture(t, nil)
	s := dispatchedRefund(t, r)
	if _, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), time.Time{}, r.intent.SubmitBefore); e == nil {
		t.Fatal("expired submit deadline admitted")
	}
	old := s
	next, e := PrepareRefundQuery(r.intent, s, r.intent.ResolveUntil)
	if e == nil || next != old || next.State != "UNKNOWN" {
		t.Fatal("financial horizon expired into a fake failure or free balance")
	}
}

func TestOriginalRefundUnknownKeepsCumulativeBudgetUntilAuthoritativeResolution(t *testing.T) {
	r := newRefundFixture(t, nil)
	paid := paidSnapshot(r.payment)
	paid.Amount = r.intent.Collected
	held := Refund{ID: r.intent.Scope.Refund, Payment: paid.Scope.Payment, Order: paid.Scope.Order, OriginalRequestKey: r.intent.Scope.OriginalRequestKey, Amount: money.Fen(4500), State: "UNKNOWN"}
	if _, _, e := ReserveRefund(paid, []Refund{held}, r.intent.Authority, "refund_other", "RF_other", 501); e == nil {
		t.Fatal("unknown original refund reservation overspent collected payment")
	}
	original, replay, e := ReserveRefund(paid, []Refund{held}, r.intent.Authority, held.ID, held.OriginalRequestKey, held.Amount)
	if e != nil || !replay || original != held {
		t.Fatal("same original refund replay allocated replacement capacity")
	}
}

func TestOriginalRefundMergeIsMonotonicAndNeverChangesOriginalPaidState(t *testing.T) {
	r := newRefundFixture(t, nil)
	paid := paidSnapshot(r.payment)
	oldPaid := paid
	s := dispatchedRefund(t, r)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	final, e := MergeRefund(r.intent, s, o)
	if e != nil || final.State != "SUCCEEDED" || !final.NextQueryAt.IsZero() {
		t.Fatal("signed original success did not become a financial fact")
	}
	late := r.channel.unknown(r.intent, "TRANSPORT_UNKNOWN")
	defer late.Close()
	after, e := MergeRefund(r.intent, final, late)
	if e != nil || after != final || paid != oldPaid {
		t.Fatal("late uncertainty downgraded original financial result or rewrote paid history")
	}
}

func TestOriginalRefundMergeRejectsForeignIntentAndUncommittedDispatch(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	if _, e := MergeRefund(r.intent, InitialRefundSnapshot(r.intent), o); e == nil {
		t.Fatal("financial response accepted without committed original dispatch state")
	}
	foreign := dispatchedRefund(t, r)
	foreign.Scope.Original.Buyer = "different_buyer"
	if _, e := MergeRefund(r.intent, foreign, o); e == nil {
		t.Fatal("another buyer's snapshot accepted original refund")
	}
	changed := r.intent
	changed.Authority.Reference = "changed_policy"
	if _, e := MergeRefund(changed, dispatchedRefund(t, r), o); e == nil {
		t.Fatal("refund fact crossed immutable policy intent")
	}
}

func TestOriginalRefundLateSignedFactDoesNotReopenExecutionOrQueryWindow(t *testing.T) {
	r := newRefundFixture(t, nil)
	s := dispatchedRefund(t, r)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	r.payment.now = r.intent.ResolveUntil.Add(time.Second)
	next, e := MergeRefund(r.intent, s, o)
	if e != nil || next.State != "SUCCEEDED" {
		t.Fatal("late recording erased a previously verified original financial fact")
	}
	if _, e := PrepareRefundQuery(r.intent, next, r.payment.now); e == nil {
		t.Fatal("financial fact reopened expired channel work")
	}
}

func TestOriginalRefundRequestJSONCannotManufactureVerifiedObservation(t *testing.T) {
	var o RefundObservation
	if json.Unmarshal([]byte(`{"kind":"REFUND_CONFIRMED","method":"alipay.trade.fastpay.refund.query","amount":500,"evidence_hash":"`+strings.Repeat("f", 64)+`"}`), &o) != nil {
		t.Fatal("synthetic observation JSON")
	}
	if o.IsSucceeded() {
		t.Fatal("public JSON manufactured verified refund authority")
	}
}
