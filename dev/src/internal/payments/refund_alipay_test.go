package payments

// SOURCE_ONLY: synthetic signed envelopes and an in-memory RoundTripper. No
// Alipay, buyer/seller approval, funds, database, model or device is exercised.
import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type refundFixture struct {
	payment *payFixture
	route   RefundRoute
	intent  RefundIntent
	channel *AlipayRefund
	calls   atomic.Int32
}

func newRefundFixture(t *testing.T, handler func(*refundFixture, *http.Request) (*http.Response, error)) *refundFixture {
	t.Helper()
	f := fixture(t, nil)
	paid := paidSnapshot(f)
	paid.Amount = 5000
	authority := RefundAuthority{Payment: paid.Scope.Payment, Order: paid.Scope.Order, Reference: "cancel_policy_test", EvidenceHash: strings.Repeat("d", 64), Maximum: 5000}
	reserved, _, e := ReserveRefund(paid, nil, authority, "refund_test", "RF_original", 500)
	if e != nil {
		t.Fatal(e)
	}
	r := &refundFixture{payment: f, route: RefundRoute{Profile: RefundProfile, Revision: "refund_1", OriginalBindingHash: f.binding.Digest(),
		EvidenceRef: "refund_route_test", EvidenceHash: strings.Repeat("e", 64), ValidFrom: f.now.Add(-10 * time.Minute), ValidUntil: f.now.Add(time.Hour), ResolveUntil: f.binding.ResolveUntil}}
	r.intent, e = NewRefundIntent(paid, reserved, authority, r.route, f.binding, f.now, f.now.Add(30*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	r.channel, e = NewAlipayRefund(AlipayRefundConfig{Channel: AlipayConfig{Binding: f.binding, PrivateKey: f.application, AlipayPublicKey: &f.provider.PublicKey,
		Now: func() time.Time { return f.now }, Transport: tripFunc(func(request *http.Request) (*http.Response, error) {
			r.calls.Add(1)
			if handler != nil {
				return handler(r, request)
			}
			return r.reply(t, request)
		})}, Route: r.route})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		if r.channel.Close(ctx) != nil {
			t.Error("original refund channel still owned")
		}
	})
	return r
}
func (r *refundFixture) reply(t *testing.T, request *http.Request) (*http.Response, error) {
	t.Helper()
	if request.ParseForm() != nil {
		t.Fatal("synthetic refund form")
	}
	method := request.PostForm.Get("method")
	node := `{"code":"10000","trade_no":"` + r.intent.OriginalTrade + `","out_trade_no":"merchant_original","refund_fee":"5.00","fund_change":"Y"}`
	if method == refundQueryMethod {
		node = `{"code":"10000","trade_no":"` + r.intent.OriginalTrade + `","out_trade_no":"merchant_original","out_request_no":"RF_original","refund_amount":"5.00","total_amount":"50.00","refund_status":"REFUND_SUCCESS"}`
	}
	return r.payment.signed(t, method, []byte(node)), nil
}
func refundReply(t *testing.T, r *refundFixture, method string, fields map[string]any) *http.Response {
	t.Helper()
	raw, e := json.Marshal(fields)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	return r.payment.signed(t, method, raw)
}
func refundFields(r *refundFixture, query bool) map[string]any {
	fields := map[string]any{"code": "10000", "trade_no": r.intent.OriginalTrade, "out_trade_no": r.intent.Scope.Original.OutTradeNo}
	if query {
		fields["out_request_no"] = r.intent.Scope.OriginalRequestKey
		fields["refund_amount"] = "5.00"
		fields["total_amount"] = "50.00"
		fields["refund_status"] = "REFUND_SUCCESS"
	} else {
		fields["refund_fee"] = "5.00"
		fields["fund_change"] = "Y"
	}
	return fields
}

func TestOriginalRefundHasIndependentCapabilitiesWithoutExtendingMachinePay(t *testing.T) {
	r := newRefundFixture(t, nil)
	if strings.Join(r.payment.channel.Capabilities(), ",") != "PRECREATE,VERIFY_SAME_PROOF" || strings.Join(r.channel.Capabilities(), ",") != "REFUND_ORIGINAL,QUERY_ORIGINAL_REFUND" {
		t.Fatal("conditional bridge extended normal Machine Pay")
	}
	for _, pair := range []struct {
		channel *Alipay
		method  string
	}{{r.payment.channel, refundMethod}, {r.payment.channel, refundQueryMethod}, {r.channel.channel, precreateMethod}, {r.channel.channel, verifyMethod}} {
		reply, _, e := pair.channel.call(context.Background(), pair.method, struct{}{})
		if reply != nil {
			reply.close()
		}
		if e == nil {
			t.Fatal("cross-profile method admitted")
		}
	}
	if r.calls.Load() != 0 || r.payment.calls.Load() != 0 {
		t.Fatal("rejected method reached a transport")
	}
}

func TestOriginalRefundSubmitSignsOnlyOriginalIdentifiersAndAmount(t *testing.T) {
	r := newRefundFixture(t, func(r *refundFixture, request *http.Request) (*http.Response, error) {
		if request.ParseForm() != nil {
			t.Fatal("refund form")
		}
		form := request.PostForm
		signature, e := base64.StdEncoding.DecodeString(form.Get("sign"))
		if e != nil {
			t.Fatal(e)
		}
		defer clear(signature)
		digest := sha256.Sum256([]byte(canonical(form)))
		if rsa.VerifyPKCS1v15(&r.payment.application.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Fatal("original application did not sign exact form")
		}
		if request.URL.Host != "openapi-sandbox.dl.alipaydev.com" || form.Get("app_id") != r.payment.binding.AppID || form.Get("method") != refundMethod || form.Get("sign_type") != "RSA2" {
			t.Fatal("refund departed original app/environment")
		}
		var biz map[string]string
		if json.Unmarshal([]byte(form.Get("biz_content")), &biz) != nil || len(biz) != 4 || biz["trade_no"] != r.intent.OriginalTrade || biz["out_trade_no"] != r.intent.Scope.Original.OutTradeNo || biz["out_request_no"] != "RF_original" || biz["refund_amount"] != "5.00" {
			t.Fatal("original refund changed scope/amount or added destination/royalty")
		}
		return r.reply(t, request)
	})
	o, e := r.channel.Submit(context.Background(), r.intent)
	defer o.Close()
	if e != nil || o.IsSucceeded() || o.kind != "REFUND_SUBMITTED" || r.calls.Load() != 1 {
		t.Fatal("submit fabricated final financial result")
	}
}

func TestOriginalRefundCumulativeFeeDoesNotAttributePartialRefund(t *testing.T) {
	for _, variant := range []struct{ fee, change string }{{"12.50", "Y"}, {"0", "N"}, {"12.5", "N"}, {"5", ""}} {
		t.Run(variant.fee+variant.change, func(t *testing.T) {
			r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
				fields := refundFields(r, false)
				fields["refund_fee"] = variant.fee
				if variant.change == "" {
					delete(fields, "fund_change")
				} else {
					fields["fund_change"] = variant.change
				}
				return refundReply(t, r, refundMethod, fields), nil
			})
			o, e := r.channel.Submit(context.Background(), r.intent)
			defer o.Close()
			if e != nil || o.IsSucceeded() || o.kind != "REFUND_SUBMITTED" {
				t.Fatal("cumulative response treated as own partial success or invalid exact decimal")
			}
		})
	}
}

func TestOriginalRefundSubmitRejectsSignedCrossTradeAndImpossibleTotals(t *testing.T) {
	for _, variant := range []struct {
		field string
		value any
	}{{"trade_no", strings.Repeat("2", 32)}, {"out_trade_no", "other_order"}, {"out_request_no", "other_refund"}, {"refund_fee", "50.01"}, {"refund_fee", "4.99"}, {"refund_fee", "5e0"}, {"fund_change", "unexpected"}} {
		t.Run(variant.field+refundTestJSONName(variant.value), func(t *testing.T) {
			r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
				fields := refundFields(r, false)
				fields[variant.field] = variant.value
				return refundReply(t, r, refundMethod, fields), nil
			})
			o, e := r.channel.Submit(context.Background(), r.intent)
			defer o.Close()
			if e == nil || o.IsSucceeded() || o.kind != "UNRESOLVED" || r.calls.Load() != 1 {
				t.Fatal("signed foreign/impossible submit accepted")
			}
		})
	}
}

func TestOriginalRefundQueryRequiresExactOriginalAmountAndRequestNumber(t *testing.T) {
	for _, variant := range []struct {
		field string
		value any
	}{{"trade_no", strings.Repeat("2", 32)}, {"out_trade_no", "other_order"}, {"out_request_no", "other_refund"}, {"refund_amount", "12.50"}, {"total_amount", "49.99"}, {"refund_amount", 5}, {"total_amount", nil}} {
		t.Run(variant.field+refundTestJSONName(variant.value), func(t *testing.T) {
			r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
				fields := refundFields(r, true)
				if variant.value == nil {
					delete(fields, variant.field)
				} else {
					fields[variant.field] = variant.value
				}
				return refundReply(t, r, refundQueryMethod, fields), nil
			})
			o, e := r.channel.Query(context.Background(), r.intent)
			defer o.Close()
			if e == nil || o.IsSucceeded() || o.kind != "UNRESOLVED" {
				t.Fatal("signed query attributed another original refund or invented missing total")
			}
		})
	}
}

func TestOriginalRefundQueryCodeAloneCannotReleaseHeldCapacity(t *testing.T) {
	for _, status := range []string{"", "REFUND_PROCESSING", "REFUND_FAILED"} {
		t.Run(status, func(t *testing.T) {
			r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
				fields := refundFields(r, true)
				if status == "" {
					delete(fields, "refund_status")
				} else {
					fields["refund_status"] = status
				}
				return refundReply(t, r, refundQueryMethod, fields), nil
			})
			old, e := PrepareRefundSubmit(r.intent, InitialRefundSnapshot(r.intent), time.Time{}, r.payment.now)
			if e != nil {
				t.Fatal(e)
			}
			o, e := r.channel.Query(context.Background(), r.intent)
			defer o.Close()
			if e != nil {
				t.Fatal(e)
			}
			next, e := MergeRefund(r.intent, old, o)
			if e != nil || o.IsSucceeded() || next.State != "PROCESSING" {
				t.Fatal("code=10000/missing/negative status freed refund capacity")
			}
			paid := paidSnapshot(r.payment)
			paid.Amount = r.intent.Collected
			held, e := HeldRefunds(paid, []Refund{{ID: r.intent.Scope.Refund, Payment: paid.Scope.Payment, Order: paid.Scope.Order, OriginalRequestKey: r.intent.Scope.OriginalRequestKey, Amount: next.Amount, State: next.State}})
			if e != nil || held != r.intent.Amount {
				t.Fatal("unresolved channel status released original reservation")
			}
		})
	}
}

func TestOriginalRefundSignedExactQueryCreatesOnlyOriginalFinancialFact(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	defer o.Close()
	if e != nil || !o.IsSucceeded() || o.scope != r.intent.Scope || o.intentHash != r.intent.Digest() || r.calls.Load() != 1 {
		t.Fatal("exact signed original query failed attribution")
	}
	if r.payment.calls.Load() != 0 {
		t.Fatal("refund invoked another original payment")
	}
}

func TestOriginalRefundTransportFailureIsUnknownWithoutResubmit(t *testing.T) {
	r := newRefundFixture(t, func(*refundFixture, *http.Request) (*http.Response, error) {
		return nil, errors.New("private vendor error must not escape")
	})
	o, e := r.channel.Submit(context.Background(), r.intent)
	defer o.Close()
	if e == nil || o.IsSucceeded() || o.kind != "UNRESOLVED" || o.SafeCode() != "TRANSPORT_UNKNOWN" || strings.Contains(e.Error(), "private") || r.calls.Load() != 1 {
		t.Fatal("transport failure leaked/retried/settled")
	}
}

func TestOriginalRefundBusinessFailureAndDuplicateJSONStayUnknown(t *testing.T) {
	for _, variant := range []string{"business", "duplicate", "foreign_signature", "missing_attribution"} {
		t.Run(variant, func(t *testing.T) {
			r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
				node := `{"code":"10000","trade_no":"` + r.intent.OriginalTrade + `","out_trade_no":"merchant_original","out_request_no":"RF_original","refund_amount":"5.00","total_amount":"50.00","refund_status":"REFUND_SUCCESS"}`
				switch variant {
				case "business":
					node = `{"code":"40004","sub_msg":"private business detail"}`
				case "duplicate":
					node = strings.Replace(node, `"code":"10000"`, `"code":"10000","code":"10000"`, 1)
				case "missing_attribution":
					node = `{"code":"10000","refund_status":"REFUND_SUCCESS"}`
				}
				response := r.payment.signed(t, refundQueryMethod, []byte(node))
				if variant == "foreign_signature" {
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					var envelope map[string]json.RawMessage
					json.Unmarshal(body, &envelope)
					envelope["sign"] = json.RawMessage(`"` + base64.StdEncoding.EncodeToString(make([]byte, 256)) + `"`)
					changed, _ := json.Marshal(envelope)
					clear(body)
					response.Body = io.NopCloser(strings.NewReader(string(changed)))
				}
				return response, nil
			})
			o, e := r.channel.Query(context.Background(), r.intent)
			defer o.Close()
			if e == nil || o.IsSucceeded() || o.kind != "UNRESOLVED" || r.calls.Load() != 1 {
				t.Fatal("error/duplicate/unverified envelope created refund fact")
			}
		})
	}
}

func TestOriginalRefundResolutionOutlivesSalesButCannotSubmitAgain(t *testing.T) {
	r := newRefundFixture(t, nil)
	r.payment.now = r.payment.binding.ValidUntil.Add(time.Second)
	o, e := r.channel.Submit(context.Background(), r.intent)
	defer o.Close()
	if e == nil || r.calls.Load() != 0 {
		t.Fatal("expired submit admitted")
	}
	o, e = r.channel.Query(context.Background(), r.intent)
	defer o.Close()
	if e != nil || !o.IsSucceeded() || r.calls.Load() != 1 {
		t.Fatal("original financial resolution closed with new-sale window")
	}
	r.payment.now = r.intent.ResolveUntil
	o, e = r.channel.Query(context.Background(), r.intent)
	defer o.Close()
	if e == nil || r.calls.Load() != 1 {
		t.Fatal("expired financial horizon reached channel")
	}
}

func TestOriginalRefundInvalidRouteOrCredentialCannotCreateDefaultBridge(t *testing.T) {
	r := newRefundFixture(t, nil)
	for _, variant := range []string{"profile", "original_binding", "qualification", "horizon", "signing_key"} {
		t.Run(variant, func(t *testing.T) {
			c := AlipayRefundConfig{Channel: AlipayConfig{Binding: r.payment.binding, PrivateKey: r.payment.application, AlipayPublicKey: &r.payment.provider.PublicKey, Now: func() time.Time { return r.payment.now }}, Route: r.route}
			switch variant {
			case "profile":
				c.Route.Profile = Profile
			case "original_binding":
				c.Route.OriginalBindingHash = strings.Repeat("0", 64)
			case "qualification":
				c.Route.EvidenceHash = ""
			case "horizon":
				c.Route.ResolveUntil = c.Route.ResolveUntil.Add(time.Second)
			case "signing_key":
				c.Channel.PrivateKey = r.payment.provider
			}
			channel, e := NewAlipayRefund(c)
			if e == nil || channel != nil {
				if channel != nil {
					channel.Close(context.Background())
				}
				t.Fatal("invalid route/credential silently created bridge")
			}
		})
	}
}

func TestOriginalRefundClosedOwnedChannelCannotReachTransport(t *testing.T) {
	r := newRefundFixture(t, nil)
	ctx, end := context.WithTimeout(context.Background(), time.Second)
	defer end()
	if r.channel.Close(ctx) != nil {
		t.Fatal("owned channel not joined")
	}
	o, e := r.channel.Query(context.Background(), r.intent)
	defer o.Close()
	if e == nil || r.calls.Load() != 0 {
		t.Fatal("closed refund channel reused credentials")
	}
}

func refundTestJSONName(v any) string { raw, _ := json.Marshal(v); return string(raw) }
