package pricing_test

import (
	"testing"

	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/money"
)

func TestHourlyAffordableDurationDoesNotGrantBeyondConfirmedBudget(t *testing.T) {
	p := sample(pricing.Hour)
	p.HourRate = &pricing.Rate{NumeratorFen: 350, DenominatorUnits: 1}
	for _, c := range []struct {
		requested int64
		budget    money.Fen
		want      int64
	}{{3600000, 350, 3600000}, {3600000, 1, 10285}, {3600000, 0, 0}, {5000, 1, 5000}, {0, 1, 0}} {
		got, e := p.AffordableHourlyMS(c.requested, c.budget)
		if e != nil || got != c.want {
			t.Fatal(c, got, e)
		}
		if amount, e := p.Amount(pricing.Usage{BillableMS: count(got)}); e != nil || amount > c.budget {
			t.Fatal("affordable duration exceeds consent", amount, e)
		}
	}
	if n, e := p.Amount(pricing.Usage{BillableMS: count(10286)}); e != nil || n != 2 {
		t.Fatal("fractional hourly threshold was rounded to a minute", n, e)
	}
	p.HourRate = &pricing.Rate{NumeratorFen: money.MaxFen, DenominatorUnits: money.MaxFen}
	if n, e := p.AffordableHourlyMS(3600000, 1); e != nil || n != 3600000 {
		t.Fatal("large rational multiplication overflowed", n, e)
	}
	p.HourRate = &pricing.Rate{NumeratorFen: 0, DenominatorUnits: 1}
	if n, e := p.AffordableHourlyMS(3600000, 0); e != nil || n != 3600000 {
		t.Fatal("explicit free rate", n, e)
	}
	p.HourRate = nil
	if _, e := p.AffordableHourlyMS(3600000, 0); e == nil {
		t.Fatal("missing price treated as free")
	}
}

func TestCumulativeCapPrecedesMoneyNarrowingAndStillValidatesUsage(t *testing.T) {
	p := sample(pricing.Hour)
	p.HourRate = &pricing.Rate{NumeratorFen: money.MaxFen, DenominatorUnits: 1}
	u := pricing.Usage{BillableMS: count(86400000)}
	if _, e := p.Amount(u); e == nil {
		t.Fatal("uncapped out-of-range amount should retain its original error")
	}
	if got, e := p.AmountCapped(u, 11); e != nil || got != 11 {
		t.Fatal("valid budget was clamped after integer overflow", got, e)
	}
	if got, e := p.AmountCapped(u, 0); e != nil || got != 0 {
		t.Fatal("explicit zero cap", got, e)
	}
	u.InputTokens = count(1)
	if _, e := p.AmountCapped(u, 0); e == nil {
		t.Fatal("zero cap bypassed usage validation")
	}
	if _, e := p.AmountCapped(pricing.Usage{BillableMS: count(1)}, -1); e == nil {
		t.Fatal("negative cap")
	}
}
