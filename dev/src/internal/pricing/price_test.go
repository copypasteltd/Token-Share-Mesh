package pricing_test

import (
	"encoding/json"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/money"
	"strings"
	"testing"
)

func count(n int64) *int64 { return &n }
func sample(unit string) pricing.Price {
	bps := 500
	p := pricing.Price{Version: pricing.Version, Currency: "CNY", Unit: unit, Rounding: pricing.Rounding, CommissionBPS: &bps, ChannelCostOwner: "SELLER", FeePolicyVersion: "fee_v1"}
	if unit == pricing.Hour {
		p.HourRate = &pricing.Rate{NumeratorFen: 360, DenominatorUnits: 1}
	} else {
		p.InputRate = &pricing.Rate{NumeratorFen: 80, DenominatorUnits: 1}
		p.OutputRate = &pricing.Rate{NumeratorFen: 160, DenominatorUnits: 1}
	}
	return p
}

func TestHourlyAndTokenContractsUseExactAggregateRounding(t *testing.T) {
	h := sample(pricing.Hour)
	for _, c := range []struct {
		ms   int64
		want money.Fen
	}{{1800000, 180}, {600000, 60}, {10001, 2}, {0, 0}} {
		got, err := h.Amount(pricing.Usage{BillableMS: count(c.ms)})
		if err != nil || got != c.want {
			t.Fatalf("hour: %d %v want %d", got, err, c.want)
		}
	}
	p := sample(pricing.MillionTokens)
	for _, c := range []struct {
		in, out int64
		want    money.Fen
	}{{1000000, 250000, 120}, {1000, 500, 1}, {0, 0, 0}} {
		got, err := p.Amount(pricing.Usage{InputTokens: count(c.in), OutputTokens: count(c.out)})
		if err != nil || got != c.want {
			t.Fatalf("token: %d %v want %d", got, err, c.want)
		}
	}
	p.InputRate = &pricing.Rate{NumeratorFen: 1, DenominatorUnits: 3}
	p.OutputRate = &pricing.Rate{NumeratorFen: 2, DenominatorUnits: 3}
	got, err := p.Amount(pricing.Usage{InputTokens: count(1), OutputTokens: count(1)})
	if err != nil || got != 1 {
		t.Fatal("input/output rounded separately", got, err)
	}
}

func TestUnknownUsageInvalidRatesAndOverflowAreRejected(t *testing.T) {
	for _, u := range []pricing.Usage{{}, {InputTokens: count(0)}, {InputTokens: count(-1), OutputTokens: count(0)}, {InputTokens: count(1), OutputTokens: count(1), BillableMS: count(1)}} {
		if _, err := sample(pricing.MillionTokens).Amount(u); err == nil {
			t.Fatal("invalid/unknown usage accepted")
		}
	}
	p := sample(pricing.Hour)
	p.HourRate = &pricing.Rate{NumeratorFen: money.MaxFen, DenominatorUnits: 1}
	if _, err := p.Amount(pricing.Usage{BillableMS: count(money.MaxFen)}); err == nil {
		t.Fatal("overflow accepted")
	}
	p.HourRate.DenominatorUnits = 0
	if p.Validate() == nil {
		t.Fatal("zero denominator accepted")
	}
	p = sample(pricing.MillionTokens)
	p.OutputRate = nil
	if p.Validate() == nil {
		t.Fatal("missing output price accepted")
	}
	p = sample(pricing.Hour)
	p.CommissionBPS = nil
	if p.Validate() == nil {
		t.Fatal("missing fee policy treated as free")
	}
	p = sample("TOKEN_1K")
	if p.Validate() == nil {
		t.Fatal("legacy unit accepted for new sale")
	}
}

func TestFeeRefundTargetsAndLegacyConversion(t *testing.T) {
	p := sample(pricing.Hour)
	for _, c := range []struct{ gross, fee, net money.Fen }{{180, 9, 171}, {60, 3, 57}, {120, 6, 114}, {1, 0, 1}} {
		got, err := p.SplitRetained(c.gross)
		if err != nil || got.CommissionFen != c.fee || got.SellerNetFen != c.net {
			t.Fatal(got, err)
		}
	}
	got, err := p.LegacyMinuteRate()
	if err != nil || got != 6 {
		t.Fatal(got, err)
	}
	p.HourRate.NumeratorFen = 350
	if _, err := p.LegacyMinuteRate(); err == nil {
		t.Fatal("lossy legacy conversion accepted")
	}
	p.HourRate.NumeratorFen = 720
	p.HourRate.DenominatorUnits = 2
	if got, err = p.LegacyMinuteRate(); err != nil || got != 6 {
		t.Fatal("rational equivalent rejected", got, err)
	}
}

func TestPriceDecoderRejectsUnknownComponentsAndTrailingData(t *testing.T) {
	raw, _ := json.Marshal(sample(pricing.MillionTokens))
	if _, err := pricing.Decode(raw); err != nil {
		t.Fatal(err)
	}
	bad := append(append([]byte{}, raw...), []byte(" {}")...)
	if _, err := pricing.Decode(bad); err == nil {
		t.Fatal("trailing price accepted")
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["cache_rate"] = 1
	bad, _ = json.Marshal(m)
	if _, err := pricing.Decode(bad); err == nil {
		t.Fatal("unsupported cache pricing silently dropped")
	}
	for _, replacement := range []string{`"numerator_fen":80,"numerator_fen":0`, `"ignored":80`, `"numerator_fen":null`} {
		bad = []byte(strings.Replace(string(raw), `"numerator_fen":80`, replacement, 1))
		if _, err := pricing.Decode(bad); err == nil {
			t.Fatal("ambiguous or missing rate silently accepted")
		}
	}
}
