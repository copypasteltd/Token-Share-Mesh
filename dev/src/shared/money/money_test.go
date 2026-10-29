package money_test

import (
	"math"
	"p11.local/compute/shared/money"
	"testing"
)

func TestCumulativeBillingDoesNotRoundEveryChunk(t *testing.T) {
	var total money.Fen
	for n := int64(1); n <= 1000; n++ {
		delta, err := money.Incremental(n-1, n, 1, 1000)
		if err != nil {
			t.Fatal(err)
		}
		total += delta
	}
	if total != 1 {
		t.Fatalf("1000 fractional chunks charged %d fen, want 1", total)
	}
	replay, err := money.Incremental(1000, 1000, 1, 1000)
	if err != nil || replay != 0 {
		t.Fatal("duplicate usage charged twice", replay, err)
	}
}
func TestAmountsRejectOverflowNegativeAndReversal(t *testing.T) {
	for _, v := range []int64{-1, money.MaxFen + 1, math.MaxInt64} {
		if _, err := money.New(v); err == nil {
			t.Fatalf("accepted invalid amount %d", v)
		}
	}
	if _, err := money.Add(money.Fen(money.MaxFen), 1); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := money.CumulativeCeil(math.MaxInt64, math.MaxInt64, 1); err == nil {
		t.Fatal("rational overflow accepted")
	}
	if _, err := money.Incremental(20, 19, 1, 1000); err == nil {
		t.Fatal("usage reversal accepted")
	}
	if _, err := money.CumulativeCeil(1, 1, 0); err == nil {
		t.Fatal("zero denominator accepted")
	}
}
