package rental_test

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"p11.local/compute/internal/rental"
)

func sessionContract() (rental.Contract, time.Time) {
	in := meterFixture()
	now := in.Started
	return rental.Contract{Order: "order_original", ContractHash: strings.Repeat("a", 64), Buyer: "buyer_original", Seller: "seller_original", Device: "device_original", Resource: "resource_original", Environment: "environment_original", EnvironmentHash: strings.Repeat("b", 64), Manifest: "manifest_original", ManifestHash: strings.Repeat("c", 64), Price: in.Price, Budget: 350, MaximumDurationMS: 3600000, OriginalDeadline: now.Add(4 * time.Hour), DisconnectPolicy: "STOP_AFTER_GRACE", PaymentEnvironment: "SANDBOX"}, now
}
func free(c rental.Contract) rental.ResourceSnapshot {
	return rental.ResourceSnapshot{Resource: c.Resource, Seller: c.Seller, Device: c.Device, Generation: "11", Phase: "FREE"}
}
func owner(s rental.Session, phase string) rental.ResourceSnapshot {
	return rental.ResourceSnapshot{Resource: s.Contract.Resource, Seller: s.Contract.Seller, Device: s.Contract.Device, Generation: s.Generation, Phase: phase, SessionOwner: s.ID}
}
func proof(s rental.Session, at time.Time) rental.ReadyProof {
	return rental.ReadyProof{Environment: s.Contract.Environment, Generation: s.Generation, Connection: "evidence_connection", Readiness: "evidence_readiness", At: at}
}
func funds(c rental.Contract) rental.Funding {
	return rental.Funding{Order: c.Order, Buyer: c.Buyer, Seller: c.Seller, Payment: "payment_original", Evidence: "evidence_funding", Environment: "SANDBOX", Amount: 350, ValidUntil: c.OriginalDeadline}
}

func activeSession(t *testing.T) rental.Session {
	t.Helper()
	c, now := sessionContract()
	s, e := rental.Reserve("session_original", "terms_original", c, free(c), now)
	if e != nil {
		t.Fatal(e)
	}
	s, e = rental.BeginPreparing(s, s.Version, owner(s, "RESERVED"), now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	s, e = rental.RecordDeviceReady(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(2*time.Second)), now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	s, e = rental.ConfirmBuyer(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(3*time.Second)), s.Budget, funds(c), now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestRentalReservationRequiresFreeOriginalDeviceAndNewGeneration(t *testing.T) {
	c, now := sessionContract()
	r := free(c)
	s, e := rental.Reserve("session_original", "terms_original", c, r, now)
	if e != nil || s.Generation != "12" || s.State != "RESERVED" || !s.Started.IsZero() || !s.ReservedUntil.Equal(now.Add(90*time.Second)) {
		t.Fatal("reservation shape", s, e)
	}
	*c.Price.CommissionBPS = 9000
	c.Price.HourRate.NumeratorFen = 999
	if *s.Contract.Price.CommissionBPS != 500 || s.Contract.Price.HourRate.NumeratorFen != 350 {
		t.Fatal("caller changed frozen price")
	}
	for _, phase := range []string{"RESERVED", "ACTIVE", "STOPPING", "CLEANING", "QUARANTINED"} {
		r.Phase = phase
		if _, e = rental.Reserve("session_new", "terms_new", s.Contract, r, now); e == nil {
			t.Fatal("TTL/busy owner was stolen", phase)
		}
	}
	r = free(s.Contract)
	r.Generation = strconv.FormatInt(math.MaxInt64, 10)
	if _, e = rental.Reserve("session_new", "terms_new", s.Contract, r, now); e == nil {
		t.Fatal("platform generation overflow")
	}
	r = free(s.Contract)
	r.Device = "device_other"
	if _, e = rental.Reserve("session_new", "terms_new", s.Contract, r, now); e == nil {
		t.Fatal("silent device fallback")
	}
}

func TestRentalReadyGatesSingleStartAndPreparationIsFree(t *testing.T) {
	c, now := sessionContract()
	s, _ := rental.Reserve("session_original", "terms_original", c, free(c), now)
	if _, e := rental.ConfirmBuyer(s, s.Version, owner(s, "RESERVED"), proof(s, now), s.Budget, funds(c), now); e == nil {
		t.Fatal("started before preparation/device-ready")
	}
	if _, e := rental.BeginPreparing(s, s.Version, owner(s, "RESERVED"), s.ReservedUntil); e == nil {
		t.Fatal("expired reservation")
	}
	s, e := rental.BeginPreparing(s, s.Version, owner(s, "RESERVED"), now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if !s.Started.IsZero() || !s.PrepareUntil.Equal(now.Add(121*time.Second)) {
		t.Fatal("preparation billed or kept old TTL")
	}
	s, e = rental.RecordDeviceReady(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(2*time.Second)), now.Add(2*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*rental.Funding){func(f *rental.Funding) { f.Order = "order_other" }, func(f *rental.Funding) { f.Seller = "seller_other" }, func(f *rental.Funding) { f.Amount = 349 }, func(f *rental.Funding) { f.Environment = "PRODUCTION" }, func(f *rental.Funding) { f.Evidence = "" }} {
		f := funds(c)
		change(&f)
		if _, e = rental.ConfirmBuyer(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(3*time.Second)), s.Budget, f, now.Add(3*time.Second)); e == nil {
			t.Fatal("unresolved/foreign funds started session")
		}
	}
	if _, e = rental.ConfirmBuyer(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(3*time.Second)), 349, funds(c), now.Add(3*time.Second)); e == nil {
		t.Fatal("budget consent changed")
	}
	active, e := rental.ConfirmBuyer(s, s.Version, owner(s, "RESERVED"), proof(s, now.Add(3*time.Second)), 350, funds(c), now.Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if !active.Started.Equal(now.Add(3*time.Second)) || !active.HardDeadline.Equal(active.Started.Add(time.Hour)) {
		t.Fatal("wrong active clock")
	}
	if _, e = rental.ConfirmBuyer(active, s.Version, owner(active, "ACTIVE"), proof(active, now.Add(4*time.Second)), 350, funds(c), now.Add(4*time.Second)); e == nil {
		t.Fatal("concurrent/duplicate ready restarted clock")
	}
	if s.Started.IsZero() == false || s.Version != 3 {
		t.Fatal("value transition mutated original")
	}
}

func TestRentalRenewalAppendsSamePriceTermsAndCannotReviveOrChangeSeller(t *testing.T) {
	s := activeSession(t)
	at := s.Started.Add(time.Minute)
	in := rental.Renewal{ID: "terms_renewal", Quote: "quote_renewal", Payment: "payment_renewal", Order: s.Contract.Order, Buyer: s.Contract.Buyer, Seller: s.Contract.Seller, EnvironmentHash: s.Contract.EnvironmentHash, Price: s.Contract.Price, NewBudget: 700, NewDeadline: s.HardDeadline.Add(time.Hour), ConfirmedAt: at}
	f := funds(s.Contract)
	f.Payment = in.Payment
	renewed, terms, e := rental.ApplyRenewal(s, s.Version, owner(s, "ACTIVE"), in, f, at)
	if e != nil || renewed.Terms != 2 || renewed.Version != s.Version+1 || terms.PreviousHash != s.TermsHash || terms.AddedBudget != 350 || renewed.Contract.Budget != 350 || renewed.Budget != 700 || renewed.TermsHash == s.TermsHash || !renewed.Started.Equal(s.Started) {
		t.Fatal("renewal overwrote original or failed to append terms", renewed, terms, e)
	}
	changes := map[string]func(*rental.Renewal){"seller": func(v *rental.Renewal) { v.Seller = "seller_other" }, "device-environment": func(v *rental.Renewal) { v.EnvironmentHash = strings.Repeat("f", 64) }, "insufficient-added-budget": func(v *rental.Renewal) { v.NewBudget = 699 }, "old-deadline": func(v *rental.Renewal) { v.NewDeadline = s.HardDeadline }, "future-consent": func(v *rental.Renewal) { v.ConfirmedAt = at.Add(time.Second) }, "new-price": func(v *rental.Renewal) { p := *v.Price.HourRate; p.NumeratorFen = 360; v.Price.HourRate = &p }}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := in
			change(&changed)
			if _, _, e := rental.ApplyRenewal(s, s.Version, owner(s, "ACTIVE"), changed, f, at); e == nil {
				t.Fatal("renewal changed immutable/budget terms")
			}
		})
	}
	if _, _, e = rental.ApplyRenewal(s, s.Version, owner(s, "ACTIVE"), in, f, s.HardDeadline); e == nil {
		t.Fatal("late paid renewal revived expired work")
	}
	stopped, e := rental.Stop(s, s.Version, owner(s, "ACTIVE"), "BUYER_STOP", at)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = rental.ApplyRenewal(stopped, stopped.Version, owner(stopped, "STOPPING"), in, f, at); e == nil {
		t.Fatal("renewal revived stopped work")
	}
}

func TestRentalDeadlineDisconnectAndCleanupKeepUnknownOwner(t *testing.T) {
	s := activeSession(t)
	disconnected := s.Started.Add(10 * time.Second)
	stopped, e := rental.Tick(s, owner(s, "ACTIVE"), s.Started.Add(100*time.Second), &disconnected)
	if e != nil || stopped.State != "STOPPING" || !stopped.Stopped.Equal(s.Started.Add(70*time.Second)) || stopped.Cleanup != "PENDING" {
		t.Fatal("detection delay increased billable upper bound", stopped, e)
	}
	p := rental.CleanupProof{Session: s.ID, Generation: s.Generation, ManifestHash: strings.Repeat("d", 64), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}
	quarantined, e := rental.CloseAfterCleanup(stopped, stopped.Version, owner(stopped, "STOPPING"), p)
	if e != nil || quarantined.State != "QUARANTINED" || quarantined.Cleanup != "FAILED" {
		t.Fatal("unverified residue freed original owner", quarantined, e)
	}
	p.ResidualsVerified = true
	closed, e := rental.CloseAfterCleanup(quarantined, quarantined.Version, owner(quarantined, "QUARANTINED"), p)
	if e != nil || closed.State != "CLOSED" || closed.Cleanup != "ACKED" || !closed.Stopped.Equal(stopped.Stopped) {
		t.Fatal("original cleanup recovery", closed, e)
	}
	p.Generation = "13"
	if _, e = rental.CloseAfterCleanup(quarantined, quarantined.Version, owner(quarantined, "QUARANTINED"), p); e == nil {
		t.Fatal("next owner cleanup applied")
	}
	expired, e := rental.Tick(s, owner(s, "ACTIVE"), s.HardDeadline.Add(time.Minute), nil)
	if e != nil || !expired.Stopped.Equal(s.HardDeadline) || expired.State != "STOPPING" {
		t.Fatal("hard deadline did not freeze time")
	}
	rollback, e := rental.Tick(s, owner(s, "ACTIVE"), s.Started.Add(-time.Second), nil)
	if e != nil || rollback.State != "QUARANTINED" || !rollback.Stopped.Equal(s.Started) {
		t.Fatal("clock rollback resumed billing")
	}
}
