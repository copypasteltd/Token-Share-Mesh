// Package pricing owns the NB_PRICE_1 contract. All arithmetic is exact and
// cumulative; display units never change raw token or millisecond metering.
package pricing

import (
	"encoding/json"
	"math/big"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const Version = "NB_PRICE_1"
const Rounding = "ORDER_CEIL_FEN_1"
const Hour = "AVAILABLE_HOUR"
const MillionTokens = "TOKEN_1M"

type Rate struct {
	NumeratorFen     int64 `json:"numerator_fen"`
	DenominatorUnits int64 `json:"denominator_units"`
}

// Explicit zero is a valid free rate; an omitted numerator is not consent to
// free service. Pointer fields in the wire decoder preserve that distinction.
func (r *Rate) UnmarshalJSON(raw []byte) error {
	var fields struct {
		Numerator   *int64 `json:"numerator_fen"`
		Denominator *int64 `json:"denominator_units"`
	}
	var keys map[string]json.RawMessage
	if strictjson.Decode(raw, &fields, 1024) != nil || json.Unmarshal(raw, &keys) != nil || fields.Numerator == nil || fields.Denominator == nil || keys["numerator_fen"] == nil || keys["denominator_units"] == nil {
		return fault.New(fault.Invalid, "Explicit rational rate required")
	}
	*r = Rate{NumeratorFen: *fields.Numerator, DenominatorUnits: *fields.Denominator}
	return r.Validate()
}

type Price struct {
	Version          string `json:"price_contract_version"`
	Currency         string `json:"currency"`
	Unit             string `json:"unit"`
	Rounding         string `json:"rounding_profile"`
	HourRate         *Rate  `json:"hour_rate,omitempty"`
	InputRate        *Rate  `json:"input_rate,omitempty"`
	OutputRate       *Rate  `json:"output_rate,omitempty"`
	CommissionBPS    *int   `json:"commission_bps"`
	ChannelCostOwner string `json:"channel_cost_owner"`
	FeePolicyVersion string `json:"fee_policy_version"`
}

func (r Rate) Validate() error {
	if r.NumeratorFen < 0 || r.NumeratorFen > money.MaxFen || r.DenominatorUnits < 1 || r.DenominatorUnits > money.MaxFen {
		return fault.New(fault.Invalid, "Invalid rational price")
	}
	return nil
}

func (p Price) Validate() error {
	if p.Version != Version || p.Currency != "CNY" || p.Rounding != Rounding || p.CommissionBPS == nil || *p.CommissionBPS < 0 || *p.CommissionBPS > 10000 || !ids.Valid(p.FeePolicyVersion) || (p.ChannelCostOwner != "SELLER" && p.ChannelCostOwner != "PLATFORM") {
		return fault.New(fault.Invalid, "Missing or unsupported price/fee policy")
	}
	switch p.Unit {
	case Hour:
		if p.HourRate == nil || p.InputRate != nil || p.OutputRate != nil {
			return fault.New(fault.Invalid, "Hourly price requires exactly one hourly rate")
		}
		return p.HourRate.Validate()
	case MillionTokens:
		if p.InputRate == nil || p.OutputRate == nil || p.HourRate != nil {
			return fault.New(fault.Invalid, "LLM price requires separate input and output rates")
		}
		if err := p.InputRate.Validate(); err != nil {
			return err
		}
		return p.OutputRate.Validate()
	default:
		return fault.New(fault.Invalid, "Unsupported new-sale price unit")
	}
}

// Decode rejects unknown fields and trailing values, rather than silently
// accepting a cache/reasoning price this implementation cannot faithfully bill.
func Decode(data []byte) (Price, error) {
	var p Price
	if err := strictjson.Decode(data, &p, 8192); err != nil {
		return p, fault.New(fault.Invalid, "Invalid price contract")
	}
	return p, p.Validate()
}

// Usage has pointer counts because unavailable usage is different from zero.
// Token totals must already be verified, disjoint input/output totals. Cached
// input and reasoning output included by a backend must not be added again.
type Usage struct {
	BillableMS   *int64
	InputTokens  *int64
	OutputTokens *int64
}

func charge(quantity int64, rate Rate, basis int64) *big.Rat {
	n := new(big.Int).Mul(big.NewInt(quantity), big.NewInt(rate.NumeratorFen))
	d := new(big.Int).Mul(big.NewInt(rate.DenominatorUnits), big.NewInt(basis))
	return new(big.Rat).SetFrac(n, d)
}

func ceil(value *big.Rat) (money.Fen, error) {
	n := new(big.Int).Set(value.Num())
	d := value.Denom()
	n.Add(n, new(big.Int).Sub(d, big.NewInt(1))).Quo(n, d)
	if !n.IsInt64() {
		return 0, money.ErrRange
	}
	return money.New(n.Int64())
}

func validCount(n *int64) bool { return n != nil && *n >= 0 && *n <= money.MaxFen }

func (p Price) exactAmount(u Usage) (*big.Rat, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.Unit == Hour {
		if !validCount(u.BillableMS) || u.InputTokens != nil || u.OutputTokens != nil {
			return nil, fault.New(fault.Invalid, "Verified millisecond usage required")
		}
		return charge(*u.BillableMS, *p.HourRate, 3600000), nil
	}
	if !validCount(u.InputTokens) || !validCount(u.OutputTokens) || u.BillableMS != nil {
		return nil, fault.New(fault.Invalid, "Verified input and output usage required")
	}
	total := charge(*u.InputTokens, *p.InputRate, 1000000)
	total.Add(total, charge(*u.OutputTokens, *p.OutputRate, 1000000))
	return total, nil
}

func (p Price) Amount(u Usage) (money.Fen, error) {
	value, err := p.exactAmount(u)
	if err != nil {
		return 0, err
	}
	return ceil(value)
}

// AmountCapped clamps the exact cumulative amount BEFORE narrowing to money.
// Even when the mathematical uncapped amount exceeds the monetary range, a
// valid confirmed budget remains an exact bound. This grants no right to spend
// that budget and must only consume independently verified disjoint usage.
func (p Price) AmountCapped(u Usage, cap money.Fen) (money.Fen, error) {
	if _, err := money.New(int64(cap)); err != nil {
		return 0, err
	}
	value, err := p.exactAmount(u)
	if err != nil {
		return 0, err
	}
	if value.Cmp(new(big.Rat).SetInt64(int64(cap))) >= 0 {
		return cap, nil
	}
	return ceil(value)
}

// AffordableHourlyMS bounds an explicitly requested duration by the confirmed
// budget using the exact rational hourly rate. A zero rate is explicit, never
// inferred from a missing field. This is a conservative admission cap; accepted
// availability, faults and final fees still require their own complete history.
func (p Price) AffordableHourlyMS(requested int64, budget money.Fen) (int64, error) {
	if p.Validate() != nil || p.Unit != Hour || requested < 0 || requested > money.MaxFen {
		return 0, fault.New(fault.Invalid, "Explicit hourly duration and rate required")
	}
	if _, err := money.New(int64(budget)); err != nil {
		return 0, err
	}
	if p.HourRate.NumeratorFen == 0 {
		return requested, nil
	}
	n := new(big.Int).Mul(big.NewInt(int64(budget)), big.NewInt(p.HourRate.DenominatorUnits))
	n.Mul(n, big.NewInt(3600000))
	n.Quo(n, big.NewInt(p.HourRate.NumeratorFen))
	if n.Cmp(big.NewInt(requested)) >= 0 {
		return requested, nil
	}
	return n.Int64(), nil
}

type Split struct {
	GrossFen      money.Fen `json:"gross_fen"`
	CommissionFen money.Fen `json:"commission_fen"`
	SellerNetFen  money.Fen `json:"seller_net_fen"`
}

// SplitRetained computes the target fee after refunds; journal writers append
// the difference from the preceding target, never charge a second buyer fee.
// Separately contracted channel/tax costs are not invented by this calculator.
func (p Price) SplitRetained(gross money.Fen) (Split, error) {
	if err := p.Validate(); err != nil {
		return Split{}, err
	}
	if _, err := money.New(int64(gross)); err != nil {
		return Split{}, err
	}
	n := new(big.Int).Mul(big.NewInt(int64(gross)), big.NewInt(int64(*p.CommissionBPS)))
	n.Quo(n, big.NewInt(10000))
	fee := money.Fen(n.Int64())
	return Split{GrossFen: gross, CommissionFen: fee, SellerNetFen: gross - fee}, nil
}

// LegacyMinuteRate is deliberately exact. 350 fen/hour is not representable
// by the legacy integer-fen/minute column and must not silently become 360.
func (p Price) LegacyMinuteRate() (money.Fen, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if p.Unit != Hour {
		return 0, fault.New(fault.Invalid, "Hourly price required")
	}
	n := big.NewInt(p.HourRate.NumeratorFen)
	d := new(big.Int).Mul(big.NewInt(p.HourRate.DenominatorUnits), big.NewInt(60))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, d, r)
	if r.Sign() != 0 || !q.IsInt64() {
		return 0, fault.New(fault.Invalid, "Hourly price cannot use legacy minute storage")
	}
	return money.New(q.Int64())
}
