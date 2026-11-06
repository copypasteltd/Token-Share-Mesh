package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const refundMethod = "alipay.trade.refund"
const refundQueryMethod = "alipay.trade.fastpay.refund.query"

type AlipayRefundConfig struct {
	Channel AlipayConfig
	Route   RefundRoute
}

func (c AlipayRefundConfig) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original refund configuration redacted]")
}
func (c AlipayRefundConfig) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

type AlipayRefund struct {
	channel *Alipay
	route   RefundRoute
}

func (a *AlipayRefund) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original refund channel redacted]")
}
func (a *AlipayRefund) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// Independent native ownership: copied keys/token, bounded RPC/body, pinned
// original app/seller/environment, cancellation and actual close/join reuse the
// existing verified transport. No public Machine Pay methods are extended.
func NewAlipayRefund(c AlipayRefundConfig) (*AlipayRefund, error) {
	now := time.Now()
	if c.Channel.Now != nil {
		now = c.Channel.Now()
	}
	if !c.Route.ValidAt(c.Channel.Binding, now, false) {
		return nil, ErrBinding
	}
	channel, e := NewAlipay(c.Channel)
	if e != nil {
		return nil, e
	}
	channel.refundOnly = true
	return &AlipayRefund{channel: channel, route: c.Route}, nil
}
func (a *AlipayRefund) Close(ctx context.Context) error {
	if a == nil || a.channel == nil {
		return nil
	}
	return a.channel.Close(ctx)
}
func (a *AlipayRefund) Capabilities() []string {
	return []string{"REFUND_ORIGINAL", "QUERY_ORIGINAL_REFUND"}
}
func (a *AlipayRefund) unknown(i RefundIntent, code string) RefundObservation {
	return RefundObservation{scope: i.Scope, intentHash: i.Digest(), amount: i.Amount, kind: "UNRESOLVED", safeCode: code, observed: a.channel.now().UTC().Truncate(time.Microsecond)}
}
func (a *AlipayRefund) observation(i RefundIntent, r *verifiedRPCReply, method, kind string, cumulative money.Fen) RefundObservation {
	sum := sha256.Sum256(r.node)
	return RefundObservation{scope: i.Scope, intentHash: i.Digest(), amount: i.Amount, kind: kind, method: method,
		cumulative: cumulative, evidenceHash: hex.EncodeToString(sum[:]), observed: a.channel.now().UTC().Truncate(time.Microsecond), receipt: newReceiptState(r.envelope)}
}

// Generic trade price fields may omit trailing decimal zeros. Parse their
// exact decimal value without floating point/exponents/rounding; original
// Machine amounts still bound this separate profile to 1..5000 fen.
func parseRefundYuan(raw string, zero bool) (money.Fen, error) {
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 2 || len(parts[0]) < 1 || len(parts[0]) > 2 || len(parts[0]) > 1 && parts[0][0] == '0' {
		return 0, ErrIntent
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, ErrIntent
		}
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
	}
	var value int64
	for _, c := range parts[0] + fraction {
		if c < '0' || c > '9' {
			return 0, ErrIntent
		}
		value = value*10 + int64(c-'0')
	}
	if value > 5000 || !zero && value == 0 {
		return 0, ErrIntent
	}
	return money.Fen(value), nil
}

// Submit executes ONE previously committed original dispatch. Its caller owns
// cumulative locks/current refund-route qualification and per-payment >=3s
// submit serialization across processes. This primitive has no automatic retry.
// Even fund_change=Y is held PROCESSING until the exact out_request_no query
// confirms this partial refund; refund_fee is cumulative, not this amount.
func (a *AlipayRefund) Submit(ctx context.Context, i RefundIntent) (RefundObservation, error) {
	if a == nil || a.channel == nil {
		return RefundObservation{}, ErrBinding
	}
	if !i.ValidAt(a.route, a.channel.binding, a.channel.now(), true) {
		return a.unknown(i, "LOCAL_REFUND_ADMISSION_CLOSED"), ErrBinding
	}
	amount, _ := Yuan(i.Amount)
	request := struct {
		Trade  string `json:"trade_no"`
		Number string `json:"out_trade_no"`
		Refund string `json:"out_request_no"`
		Amount string `json:"refund_amount"`
	}{i.OriginalTrade, i.Scope.Original.OutTradeNo, i.Scope.OriginalRequestKey, amount}
	reply, code, e := a.channel.call(ctx, refundMethod, request)
	if e != nil {
		return a.unknown(i, code), e
	}
	defer reply.close()
	var fields map[string]json.RawMessage
	if strictjson.Decode(reply.node, &fields, MaximumChannelResponse) != nil {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	var trade, number, fee, change string
	if json.Unmarshal(fields["trade_no"], &trade) != nil || trade != i.OriginalTrade || json.Unmarshal(fields["out_trade_no"], &number) != nil || number != i.Scope.Original.OutTradeNo ||
		json.Unmarshal(fields["refund_fee"], &fee) != nil {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	cumulative, e := parseRefundYuan(fee, true)
	if e != nil || cumulative > i.Collected {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	if raw, ok := fields["fund_change"]; ok {
		if json.Unmarshal(raw, &change) != nil || change != "Y" && change != "N" {
			return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
		}
	}
	if raw, ok := fields["out_request_no"]; ok {
		var requestNo string
		if json.Unmarshal(raw, &requestNo) != nil || requestNo != i.Scope.OriginalRequestKey {
			return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
		}
	}
	if change == "Y" && cumulative < i.Amount {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	return a.observation(i, reply, refundMethod, "REFUND_SUBMITTED", cumulative), nil
}

// Query refers to the SAME trade/out_trade_no/out_request_no. Missing or
// negative business status remains unresolved and holds cumulative capacity;
// code=10000 alone never creates success, FAILED, a replacement or free money.
func (a *AlipayRefund) Query(ctx context.Context, i RefundIntent) (RefundObservation, error) {
	if a == nil || a.channel == nil {
		return RefundObservation{}, ErrBinding
	}
	if !i.ValidAt(a.route, a.channel.binding, a.channel.now(), false) {
		return a.unknown(i, "LOCAL_REFUND_RESOLUTION_CLOSED"), ErrBinding
	}
	request := struct {
		Trade  string `json:"trade_no"`
		Number string `json:"out_trade_no"`
		Refund string `json:"out_request_no"`
	}{i.OriginalTrade, i.Scope.Original.OutTradeNo, i.Scope.OriginalRequestKey}
	reply, code, e := a.channel.call(ctx, refundQueryMethod, request)
	if e != nil {
		return a.unknown(i, code), e
	}
	defer reply.close()
	var fields map[string]json.RawMessage
	if strictjson.Decode(reply.node, &fields, MaximumChannelResponse) != nil {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	var trade, number, refund, amount, total, status string
	// A success requires complete exact original attribution. Incomplete
	// code=10000 responses remain UNKNOWN; never fill fields from another order.
	if json.Unmarshal(fields["trade_no"], &trade) != nil || trade != i.OriginalTrade || json.Unmarshal(fields["out_trade_no"], &number) != nil || number != i.Scope.Original.OutTradeNo ||
		json.Unmarshal(fields["out_request_no"], &refund) != nil || refund != i.Scope.OriginalRequestKey || json.Unmarshal(fields["refund_amount"], &amount) != nil ||
		json.Unmarshal(fields["total_amount"], &total) != nil {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	fen, e := parseRefundYuan(amount, false)
	if e != nil || fen != i.Amount {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	collected, e := parseRefundYuan(total, false)
	if e != nil || collected != i.Collected {
		return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
	}
	if raw, ok := fields["refund_status"]; ok {
		if json.Unmarshal(raw, &status) != nil {
			return a.unknown(i, "SIGNED_REFUND_MISMATCH"), ErrUnknown
		}
	}
	kind := "REFUND_QUERY_UNRESOLVED"
	if status == "REFUND_SUCCESS" {
		kind = "REFUND_CONFIRMED"
	}
	return a.observation(i, reply, refundQueryMethod, kind, 0), nil
}
