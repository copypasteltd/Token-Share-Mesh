// Package money performs bounded integer/rational billing. Rounding happens
// once on cumulative usage; individual stream chunks must not be rounded.
package money

import (
	"errors"
	"math/big"
)

const MaxFen int64 = 9007199254740991

var ErrRange = errors.New("amount or rate outside supported range")

type Fen int64

func New(value int64) (Fen, error) {
	if value < 0 || value > MaxFen {
		return 0, ErrRange
	}
	return Fen(value), nil
}
func Add(a, b Fen) (Fen, error) {
	if a < 0 || b < 0 || int64(a) > MaxFen || int64(b) > MaxFen || int64(a) > MaxFen-int64(b) {
		return 0, ErrRange
	}
	return a + b, nil
}
func Sub(a, b Fen) (Fen, error) {
	if a < 0 || b < 0 || int64(a) > MaxFen || int64(b) > MaxFen || b > a {
		return 0, ErrRange
	}
	return a - b, nil
}

// CumulativeCeil returns ceil(units*numerator/denominator), without overflowing
// intermediate multiplication. The contract's numerator is fen per unit.
func CumulativeCeil(units, numerator, denominator int64) (Fen, error) {
	if units < 0 || numerator < 0 || denominator <= 0 {
		return 0, ErrRange
	}
	n := new(big.Int).Mul(big.NewInt(units), big.NewInt(numerator))
	n.Add(n, big.NewInt(denominator-1)).Quo(n, big.NewInt(denominator))
	if !n.IsInt64() {
		return 0, ErrRange
	}
	return New(n.Int64())
}

// Incremental computes the newly billable difference from two cumulative
// readings. Replays with the same cumulative reading produce zero.
func Incremental(previous, current, numerator, denominator int64) (Fen, error) {
	if current < previous {
		return 0, ErrRange
	}
	a, err := CumulativeCeil(previous, numerator, denominator)
	if err != nil {
		return 0, err
	}
	b, err := CumulativeCeil(current, numerator, denominator)
	if err != nil {
		return 0, err
	}
	return Sub(b, a)
}
