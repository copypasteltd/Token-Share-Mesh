package payments

// SOURCE_ONLY: signed synthetic responses and in-memory transports, not real
// Alipay, merchant qualification, funds, refund or payment acceptance evidence.
import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/shared/money"
)

type tripFunc func(*http.Request) (*http.Response, error)

func (f tripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var fixtureKeys = sync.OnceValues(func() (*rsa.PrivateKey, *rsa.PrivateKey, error) {
	a, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return nil, nil, e
	}
	b, e := rsa.GenerateKey(rand.Reader, 2048)
	return a, b, e
})

type payFixture struct {
	now                   time.Time
	binding               Binding
	intent                Intent
	channel               *Alipay
	provider, application *rsa.PrivateKey
	calls                 atomic.Int32
}

func fixture(t *testing.T, transport func(*payFixture, *http.Request) (*http.Response, error)) *payFixture {
	t.Helper()
	app, provider, e := fixtureKeys()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	f := &payFixture{now: now, provider: provider, application: app}
	f.binding = Binding{Profile: Profile, Revision: "machine_1", Provider: "ALIPAY", Environment: "SANDBOX", Seller: "seller_test", AppID: "2026000000000001", ServiceID: "API_test", AuthoritySubject: "seller_subject", CredentialRef: "app_key", AuthorizationRef: "authorization_test", AuthorizationHash: strings.Repeat("a", 64), QualificationRef: "qualification_test", QualificationHash: strings.Repeat("b", 64), ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(24 * time.Hour), ResolveUntil: now.Add(48 * time.Hour)}
	f.binding.SigningKeyHash, _ = PublicKeyHash(&app.PublicKey)
	f.binding.AlipayKeyHash, _ = PublicKeyHash(&provider.PublicKey)
	f.intent = Intent{Scope: Scope{Payment: "payment_test", Order: "order_test", Buyer: "buyer_test", Seller: f.binding.Seller, Purpose: "ORDER", PurposeRef: "order_test", ContractHash: strings.Repeat("c", 64), BindingHash: f.binding.Digest(), OutTradeNo: "merchant_original"}, Amount: 1, GoodsName: "原卖家批准服务", CreatedAt: now, PayBefore: now.Add(30 * time.Minute), ContractDeadline: now.Add(time.Hour)}
	f.channel, e = NewAlipay(AlipayConfig{Binding: f.binding, PrivateKey: app, AlipayPublicKey: &provider.PublicKey, Now: func() time.Time { return f.now }, Transport: tripFunc(func(r *http.Request) (*http.Response, error) {
		f.calls.Add(1)
		if transport != nil {
			return transport(f, r)
		}
		return f.success(t, r)
	})})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
		defer end()
		if f.channel.Close(ctx) != nil {
			t.Error("synthetic channel remained owned")
		}
	})
	return f
}
func (f *payFixture) signed(t *testing.T, method string, node []byte) *http.Response {
	t.Helper()
	sum := sha256.Sum256(node)
	signature, e := rsa.SignPKCS1v15(rand.Reader, f.provider, crypto.SHA256, sum[:])
	if e != nil {
		t.Fatal(e)
	}
	raw := fmt.Sprintf(`{%q:%s,"sign":%q}`, strings.ReplaceAll(method, ".", "_")+"_response", node, base64.StdEncoding.EncodeToString(signature))
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}
}
func (f *payFixture) success(t *testing.T, r *http.Request) (*http.Response, error) {
	t.Helper()
	if r.ParseForm() != nil {
		t.Fatal("synthetic request form")
	}
	method := r.PostForm.Get("method")
	node := []byte(`{"code":"10000","amt_pre_pay_id":"amt_pre_original"}`)
	if method == verifyMethod {
		node = []byte(`{"code":"10000","trade_no":"` + strings.Repeat("1", 32) + `","out_trade_no":"merchant_original","amount":"0.01","active":true}`)
	}
	return f.signed(t, method, node), nil
}
func proofHeader(session string) string {
	payload := map[string]any{"protocol": map[string]any{"payment_proof": strings.Repeat("a", 64), "trade_no": strings.Repeat("1", 32)}}
	if session != "" {
		payload["method"] = map[string]any{"client_session": session}
	}
	raw, _ := json.Marshal(payload)
	return base64.StdEncoding.EncodeToString(raw)
}
func proofFor(t *testing.T) *Proof {
	t.Helper()
	p, e := ParseProof(proofHeader("original_client_session"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p
}
func paidSnapshot(f *payFixture) Snapshot {
	return Snapshot{Scope: f.intent.Scope, Amount: f.intent.Amount, State: "SETTLED", ChannelReference: strings.Repeat("1", 32), EvidenceHash: strings.Repeat("f", 64), ObservedAt: f.now}
}

func TestMachinePayIntegerYuanRejectsRoundingAndOverLimit(t *testing.T) {
	for _, s := range []string{"0.001", "0.00", "50.01", "5e1", "+0.01", "00.01", "NaN", " 0.01"} {
		if _, e := ParseYuan(s); e == nil {
			t.Fatal("unsafe monetary representation", s)
		}
	}
	for _, fen := range []money.Fen{1, 4999, 5000} {
		s, e := Yuan(fen)
		if e != nil {
			t.Fatal(e)
		}
		v, e := ParseYuan(s)
		if e != nil || v != fen {
			t.Fatal("integer amount changed")
		}
	}
}
func TestMachinePayOriginalNumberIsNotTruncatedOrderID(t *testing.T) {
	a, e := NewOutTradeNo()
	if e != nil {
		t.Fatal(e)
	}
	b, e := NewOutTradeNo()
	if e != nil || a == b || len(a) != 32 || !merchantNumber.MatchString(a) {
		t.Fatal("invalid independently generated original number")
	}
}
func TestMachinePayBindingRequiresActualSellerAuthorityAndEvidence(t *testing.T) {
	f := fixture(t, nil)
	for _, change := range []func(*Binding){func(b *Binding) { b.QualificationHash = "" }, func(b *Binding) { b.AuthorizationRef = "" }, func(b *Binding) { b.Seller = "" }, func(b *Binding) { b.AppAuthTokenRef = "buyer_supplied" }, func(b *Binding) { b.ResolveUntil = b.ValidUntil.Add(31 * 24 * time.Hour) }} {
		b := f.binding
		change(&b)
		if b.ValidAt(f.now) {
			t.Fatal("incomplete binding admitted")
		}
	}
}
func TestMachinePayPrecreateChecksOriginalContractBeforeExternalCall(t *testing.T) {
	f := fixture(t, nil)
	for _, change := range []func(*Intent){func(i *Intent) { i.Amount = 5001 }, func(i *Intent) { i.Scope.Seller = "seller_foreign" }, func(i *Intent) { i.Scope.BindingHash = strings.Repeat("d", 64) }, func(i *Intent) { i.PayBefore = i.CreatedAt.Add(13 * time.Hour) }, func(i *Intent) { i.Scope.PurposeRef = "order_foreign" }} {
		i := f.intent
		change(&i)
		if _, e := f.channel.Precreate(context.Background(), i); e == nil {
			t.Fatal("invalid original intent sent")
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid intent reached channel")
	}
}
func TestMachinePayPrecreateProducesPendingNotSettled(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e != nil || o.IsSettled() {
		t.Fatal("precreate fabricated paid fact", e)
	}
	v, e := Merge(Snapshot{Scope: f.intent.Scope, Amount: 1, State: "CREATED"}, o)
	if e != nil || v.State != "PENDING" || v.PrepayReference != "amt_pre_original" || v.ChannelReference != "" {
		t.Fatal("prepay mixed with actual trade")
	}
}
func TestMachinePaySignedFormBindsServiceCurrencyAndOriginalNumber(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://openapi-sandbox.dl.alipaydev.com/gateway.do" || r.ParseForm() != nil {
			t.Fatal("wrong environment")
		}
		signature, e := base64.StdEncoding.DecodeString(r.PostForm.Get("sign"))
		sum := sha256.Sum256([]byte(canonical(r.PostForm)))
		if e != nil || rsa.VerifyPKCS1v15(&f.application.PublicKey, crypto.SHA256, sum[:], signature) != nil {
			t.Fatal("request signature mismatch")
		}
		var biz map[string]any
		if json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz) != nil || biz["amount"] != "0.01" || biz["service_id"] != f.binding.ServiceID || biz["out_trade_no"] != f.intent.Scope.OutTradeNo || biz["currency"] != "CNY" || r.PostForm.Get("app_auth_token") != "" {
			t.Fatal("request lost original C2C binding")
		}
		return f.success(t, r)
	})
	if _, e := f.channel.Precreate(context.Background(), f.intent); e != nil {
		t.Fatal(e)
	}
}
func TestMachinePayVerifyRequiresSignedActiveAmountAndOriginalNumber(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil || !o.IsSettled() {
		t.Fatal("synthetic signed payment unavailable", e)
	}
	v, e := Merge(Snapshot{Scope: f.intent.Scope, Amount: 1, State: "UNKNOWN"}, o)
	if e != nil || v.State != "SETTLED" || v.ChannelReference != strings.Repeat("1", 32) {
		t.Fatal("original paid observation not applied")
	}
}
func TestMachinePayVerifyDoesNotRequireDeprecatedResourceID(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil || !o.IsSettled() {
		t.Fatal("deprecated missing resource ID rejected")
	}
}
func TestMachinePayResponseSignatureUsesOriginalNodeBytes(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		return f.signed(t, verifyMethod, []byte("{\n \"code\":\"10000\",\"msg\":\"a\\/b\",\"active\":true,\"trade_no\":\""+strings.Repeat("1", 32)+"\",\"amount\":\"0.01\",\"out_trade_no\":\"merchant_original\" }")), nil
	})
	if o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t)); e != nil || !o.IsSettled() {
		t.Fatal("signed node was reserialized before verification", e)
	}
}
func TestMachinePayValidSignatureWrongAmountCannotSettle(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		node := `{"code":"10000","trade_no":"` + strings.Repeat("1", 32) + `","out_trade_no":"merchant_original","amount":"0.02","active":true}`
		return f.signed(t, verifyMethod, []byte(node)), nil
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.IsSettled() {
		t.Fatal("foreign amount accepted")
	}
}
func TestMachinePayCrossOrderSignedProofCannotSettle(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		node := `{"code":"10000","trade_no":"` + strings.Repeat("1", 32) + `","out_trade_no":"merchant_foreign","amount":"0.01","active":true}`
		return f.signed(t, verifyMethod, []byte(node)), nil
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.IsSettled() {
		t.Fatal("foreign original number accepted")
	}
}
func TestMachinePayInactiveProofCannotSettle(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		node := `{"code":"10000","trade_no":"` + strings.Repeat("1", 32) + `","out_trade_no":"merchant_original","amount":"0.01","active":false}`
		return f.signed(t, verifyMethod, []byte(node)), nil
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.IsSettled() {
		t.Fatal("inactive proof accepted")
	}
}
func TestMachinePayDuplicateResponseKeysCannotSettle(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		node := `{"code":"10000","trade_no":"` + strings.Repeat("1", 32) + `","out_trade_no":"merchant_original","amount":"0.01","amount":"0.02","active":true}`
		return f.signed(t, verifyMethod, []byte(node)), nil
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.IsSettled() {
		t.Fatal("ambiguous signed response accepted")
	}
}
func TestMachinePayForeignSignatureCannotSettle(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		resp, e := f.success(t, r)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var env map[string]json.RawMessage
		json.Unmarshal(raw, &env)
		node := env["alipay_aipay_agent_payment_verify_response"]
		sum := sha256.Sum256(node)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, f.application, crypto.SHA256, sum[:])
		bad, _ := json.Marshal(base64.StdEncoding.EncodeToString(sig))
		env["sign"] = bad
		encoded, _ := json.Marshal(env)
		resp.Body = io.NopCloser(bytes.NewReader(encoded))
		return resp, e
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.IsSettled() {
		t.Fatal("wrong Alipay public key accepted")
	}
}
func TestMachinePayTransportFailureIsUnknownWithoutInternalRetry(t *testing.T) {
	f := fixture(t, func(*payFixture, *http.Request) (*http.Response, error) {
		return nil, errors.New("sensitive upstream body")
	})
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e == nil || o.kind != "UNRESOLVED" || f.calls.Load() != 1 || strings.Contains(e.Error(), "sensitive") {
		t.Fatal("transport failure retried or leaked details")
	}
}
func TestMachinePaySignedBusinessFailureRetainsUnknownAndSafeError(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		return f.signed(t, verifyMethod, []byte(`{"code":"20000","sub_msg":"private-proof-secret"}`)), nil
	})
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e == nil || o.kind != "UNRESOLVED" || o.SafeCode() != "SIGNED_BUSINESS_UNRESOLVED" || strings.Contains(e.Error(), "private") {
		t.Fatal("business uncertainty became success or leaked upstream body")
	}
}
func TestMachinePaySignedErrorEnvelopeCannotActAsSuccessfulPrecreate(t *testing.T) {
	f := fixture(t, func(f *payFixture, _ *http.Request) (*http.Response, error) {
		node := []byte(`{"code":"10000","amt_pre_pay_id":"untrusted_error_id"}`)
		sum := sha256.Sum256(node)
		signature, e := rsa.SignPKCS1v15(rand.Reader, f.provider, crypto.SHA256, sum[:])
		if e != nil {
			t.Fatal(e)
		}
		raw := fmt.Sprintf(`{"error_response":%s,"sign":%q}`, node, base64.StdEncoding.EncodeToString(signature))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})
	observation, e := f.channel.Precreate(context.Background(), f.intent)
	if !errors.Is(e, ErrUnknown) || observation.kind != "UNRESOLVED" || observation.prepay != "" || observation.IsSettled() {
		t.Fatal("signed error envelope became a successful payment challenge")
	}
}
func TestMachinePayRedirectNeverForwardsSignedCredentials(t *testing.T) {
	f := fixture(t, func(*payFixture, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://foreign.example/collect"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e == nil || o.IsSettled() || f.calls.Load() != 1 {
		t.Fatal("redirect followed or paid status invented")
	}
}
func TestMachinePayOverLimitResponseIsUnknown(t *testing.T) {
	f := fixture(t, func(*payFixture, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaximumChannelResponse+1)))}, nil
	})
	if _, e := f.channel.Precreate(context.Background(), f.intent); e == nil {
		t.Fatal("unbounded response admitted")
	}
}

// Remaining tests below exercise original-state and secret-recovery boundaries.
func TestMachinePayChannelKeyCannotDepartFromApprovedBinding(t *testing.T) {
	f := fixture(t, nil)
	b := f.binding
	b.AlipayKeyHash = strings.Repeat("0", 64)
	channel, e := NewAlipay(AlipayConfig{Binding: b, PrivateKey: f.application, AlipayPublicKey: &f.provider.PublicKey, Now: func() time.Time { return f.now }})
	if e == nil {
		channel.Close(context.Background())
		t.Fatal("different provider key pin admitted")
	}
	b = f.binding
	b.SigningKeyHash = strings.Repeat("0", 64)
	channel, e = NewAlipay(AlipayConfig{Binding: b, PrivateKey: f.application, AlipayPublicKey: &f.provider.PublicKey, Now: func() time.Time { return f.now }})
	if e == nil {
		channel.Close(context.Background())
		t.Fatal("different app signing key pin admitted")
	}
}
func TestMachinePayA402UsesPrepayReferenceAndCannotChargeAutomatically(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e != nil {
		t.Fatal(e)
	}
	s, e := Merge(Snapshot{Scope: f.intent.Scope, Amount: 1, State: "CREATED"}, o)
	if e != nil {
		t.Fatal(e)
	}
	c, e := NewChallenge(s, f.intent, f.binding, f.now)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	if c.WriteChallenge(w, f.now) != nil || w.Code != 402 || w.Header().Get("amtPaymentLink") != s.PrepayReference || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "payment_proof") || f.calls.Load() != 1 {
		t.Fatal("A402 mixed prepay/proof/trade or retried payment")
	}
}
func TestMachinePayUnknownCannotBecomeFreshPaymentChallenge(t *testing.T) {
	f := fixture(t, nil)
	s := Snapshot{Scope: f.intent.Scope, Amount: 1, State: "UNKNOWN", PrepayReference: "amt_pre_original"}
	if _, e := NewChallenge(s, f.intent, f.binding, f.now); e == nil {
		t.Fatal("unknown state became a fresh charge")
	}
}
func TestMachinePayProofHeaderRejectsDuplicateCaseAliases(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header["Payment-Proof"] = []string{proofHeader("")}
	r.Header["payment-proof"] = []string{proofHeader("")}
	if p, e := ProofFromRequest(r); e == nil {
		p.Close()
		t.Fatal("duplicate proof headers admitted")
	}
}
func TestMachinePayPublicCapabilitiesDoNotInventRefundOrSettlement(t *testing.T) {
	f := fixture(t, nil)
	for _, cap := range f.channel.Capabilities() {
		if cap != "PRECREATE" && cap != "VERIFY_SAME_PROOF" {
			t.Fatal("unsupported channel side effect advertised")
		}
	}
}
func TestMachinePayCanonicalFormSignsRawValuesBeforeURLEncoding(t *testing.T) {
	v := url.Values{"z": []string{"尾"}, "a": []string{`{"literal":"a&b=c"}`}, "empty": []string{""}, "sign": []string{"ignored"}}
	if canonical(v) != `a={"literal":"a&b=c"}&z=尾` {
		t.Fatal("independent signing vector changed")
	}
	decoded, e := url.ParseQuery(v.Encode())
	if e != nil || canonical(decoded) != canonical(v) {
		t.Fatal("transport encoding changed signed values")
	}
}
func TestMachinePayProofParserRejectsAmbiguityAndOversizedSecrets(t *testing.T) {
	good := `{"protocol":{"payment_proof":"` + strings.Repeat("a", 64) + `","trade_no":"` + strings.Repeat("1", 32) + `"}}`
	for _, raw := range []string{strings.Replace(good, `"trade_no":`, `"trade_no":"`+strings.Repeat("2", 32)+`","trade_no":`, 1), strings.Replace(good, `"protocol":`, `"unknown":true,"protocol":`, 1), strings.Replace(good, strings.Repeat("a", 64), strings.Repeat("a", 65), 1), strings.Replace(good, strings.Repeat("1", 32), strings.Repeat("1", 31), 1)} {
		p, e := ParseProof(base64.StdEncoding.EncodeToString([]byte(raw)))
		if e == nil {
			p.Close()
			t.Fatal("ambiguous proof admitted")
		}
	}
	for _, raw := range []string{"not-base64", strings.Repeat("a", MaximumProofHeader+1), proofHeader("") + "\n"} {
		p, e := ParseProof(raw)
		if e == nil {
			p.Close()
			t.Fatal("unbounded/noncanonical header admitted")
		}
	}
}
func TestMachinePayOptionalClientSessionIsNotManufactured(t *testing.T) {
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		if r.ParseForm() != nil {
			t.Fatal("form")
		}
		var biz map[string]any
		json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
		if _, ok := biz["client_session"]; ok {
			t.Fatal("client session invented")
		}
		return f.success(t, r)
	})
	p, e := ParseProof(proofHeader(""))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if _, e = f.channel.Verify(context.Background(), f.intent, p); e != nil {
		t.Fatal(e)
	}
}
func TestMachinePayClientSessionIsPreservedWithoutDecodingOrReplacement(t *testing.T) {
	session := "opaque_original_+/="
	f := fixture(t, func(f *payFixture, r *http.Request) (*http.Response, error) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
		if biz["client_session"] != session {
			t.Fatal("original session changed")
		}
		return f.success(t, r)
	})
	p, e := ParseProof(proofHeader(session))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if _, e = f.channel.Verify(context.Background(), f.intent, p); e != nil {
		t.Fatal(e)
	}
}
func TestMachinePayClosedProofCannotBeReusedThroughCopy(t *testing.T) {
	p := proofFor(t)
	copy := *p
	p.Close()
	if _, e := copy.payload(); e == nil {
		t.Fatal("copied proof survived revocation")
	}
}
func TestMachinePayLatePaidFactDoesNotResurrectExpiredContract(t *testing.T) {
	f := fixture(t, nil)
	f.now = f.now.Add(2 * time.Hour)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil {
		t.Fatal(e)
	}
	v, e := Merge(Snapshot{Scope: f.intent.Scope, Amount: 1, State: "CLOSED"}, o)
	if e != nil || v.State != "SETTLED" {
		t.Fatal("actual late funds lost")
	}
	r, e := Reconcile(v, o, true)
	if e != nil || !r.BlockNewWork || r.Classification != "LATE_PAID_RESOLUTION" {
		t.Fatal("late payment revived execution")
	}
	if f.intent.ContractDeadline.After(f.now) {
		t.Fatal("fixture must retain expired contract")
	}
}
func TestMachinePayResolutionHorizonDoesNotAuthorizeNewSale(t *testing.T) {
	f := fixture(t, nil)
	f.now = f.binding.ValidUntil.Add(time.Minute)
	if f.binding.ValidAt(f.now) || !f.binding.ResolvableAt(f.now) {
		t.Fatal("sales and resolution authorities conflated")
	}
	if _, e := f.channel.Precreate(context.Background(), f.intent); e == nil {
		t.Fatal("expired sales authority admitted new payment")
	}
	if _, e := f.channel.Verify(context.Background(), f.intent, proofFor(t)); e != nil {
		t.Fatal("approved original financial resolution unavailable", e)
	}
}
func TestMachinePayUnknownOrPendingCannotOverwriteSettled(t *testing.T) {
	f := fixture(t, nil)
	old := paidSnapshot(f)
	for _, kind := range []string{"UNRESOLVED", "PRECREATED"} {
		o := Observation{scope: old.Scope, amount: 1, kind: kind, prepay: "amt_old", observed: f.now.Add(time.Minute)}
		next, e := Merge(old, o)
		if e != nil || next != old {
			t.Fatal("older uncertainty overwrote actual funds")
		}
	}
}
func TestMachinePayCrossSellerObservationCannotMutateOriginalFunds(t *testing.T) {
	f := fixture(t, nil)
	o := Observation{scope: f.intent.Scope, amount: 1, kind: "VERIFIED_PAID", trade: strings.Repeat("1", 32), evidenceHash: strings.Repeat("a", 64), observed: f.now}
	o.scope.Seller = "seller_foreign"
	old := Snapshot{Scope: f.intent.Scope, Amount: 1, State: "UNKNOWN"}
	next, e := Merge(old, o)
	if e == nil || next != old {
		t.Fatal("cross-seller fact mutated original payment")
	}
}
func TestMachinePayFundingRequiresCurrentPartiesAndResolvedRefunds(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	check := FundingCheck{Scope: v.Scope, Minimum: 1, HardDeadline: f.now.Add(time.Minute), CurrentBuyer: true, CurrentSeller: true, CurrentDevice: true, OriginalContract: true, RefundsResolved: true}
	if !CanFund(v, f.binding, check, f.now) {
		t.Fatal("synthetic current original fact rejected")
	}
	for _, change := range []func(*FundingCheck){func(c *FundingCheck) { c.RefundsResolved = false }, func(c *FundingCheck) { c.RefundHeld = 1 }, func(c *FundingCheck) { c.CurrentSeller = false }, func(c *FundingCheck) { c.CurrentDevice = false }, func(c *FundingCheck) { c.HardDeadline = f.now }, func(c *FundingCheck) { c.Scope.Buyer = "buyer_foreign" }} {
		c := check
		change(&c)
		if CanFund(v, f.binding, c, f.now) {
			t.Fatal("incomplete current funding gate admitted")
		}
	}
}
func TestMachinePayUnknownRefundRetainsCapacity(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	v.Amount = 5000
	rows := []Refund{{ID: "refund_unknown", Payment: v.Scope.Payment, Order: v.Scope.Order, OriginalRequestKey: "refund_original", Amount: 3000, State: "UNKNOWN"}, {ID: "refund_success", Payment: v.Scope.Payment, Order: v.Scope.Order, OriginalRequestKey: "refund_success", Amount: 1000, State: "SUCCEEDED"}, {ID: "refund_failed", Payment: v.Scope.Payment, Order: v.Scope.Order, OriginalRequestKey: "refund_failed", Amount: 1000, State: "FAILED"}}
	held, e := HeldRefunds(v, rows)
	if e != nil || held != 4000 {
		t.Fatal("unknown refunds freed capacity")
	}
	a := RefundAuthority{Payment: v.Scope.Payment, Order: v.Scope.Order, Reference: "policy_original", EvidenceHash: strings.Repeat("a", 64), Maximum: 5000}
	if _, _, e := ReserveRefund(v, rows, a, "refund_extra", "refund_extra", 1001); e == nil {
		t.Fatal("cumulative refund exceeded actual collected amount")
	}
}
func TestMachinePayRefundReplayReturnsSameUnknownOriginal(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	old := Refund{ID: "refund_original", Payment: v.Scope.Payment, Order: v.Scope.Order, OriginalRequestKey: "refund_original", Amount: 1, State: "UNKNOWN"}
	a := RefundAuthority{Payment: v.Scope.Payment, Order: v.Scope.Order, Reference: "policy_original", EvidenceHash: strings.Repeat("a", 64), Maximum: 1}
	got, replayed, e := ReserveRefund(v, []Refund{old}, a, old.ID, old.OriginalRequestKey, 1)
	if e != nil || !replayed || got != old {
		t.Fatal("unknown refund was recreated")
	}
	if _, _, e = ReserveRefund(v, []Refund{old}, a, old.ID, "different_original", 1); e == nil {
		t.Fatal("refund original key replaced")
	}
}
func TestMachinePayUnpaidClosureCannotBecomeRefundedMoney(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	v.State = "CLOSED"
	a := RefundAuthority{Payment: v.Scope.Payment, Order: v.Scope.Order, Reference: "policy_original", EvidenceHash: strings.Repeat("a", 64), Maximum: 1}
	if _, _, e := ReserveRefund(v, nil, a, "refund_original", "refund_original", 1); e == nil {
		t.Fatal("unpaid closure created refund obligation")
	}
}
func TestMachinePayDamagedRefundRowsNeverClampToFreeCapacity(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	r := Refund{ID: "refund_original", Payment: v.Scope.Payment, Order: v.Scope.Order, OriginalRequestKey: "refund_original", Amount: 1, State: "UNKNOWN"}
	for _, rows := range [][]Refund{{r, r}, {{ID: r.ID, Payment: r.Payment, Order: r.Order, OriginalRequestKey: r.OriginalRequestKey, Amount: 2, State: "UNKNOWN"}}, {{ID: r.ID, Payment: "payment_foreign", Order: r.Order, OriginalRequestKey: r.OriginalRequestKey, Amount: 1, State: "UNKNOWN"}}} {
		if _, e := HeldRefunds(v, rows); e == nil {
			t.Fatal("damaged refund history treated as available money")
		}
	}
}
func TestMachinePayReconciliationMismatchBlocksAndPreservesOriginal(t *testing.T) {
	f := fixture(t, nil)
	v := paidSnapshot(f)
	o := Observation{scope: v.Scope, amount: 2, kind: "VERIFIED_PAID", trade: v.ChannelReference, evidenceHash: v.EvidenceHash, observed: f.now}
	r, e := Reconcile(v, o, false)
	if e == nil || r.ApplyOriginalObservation || !r.BlockNewWork || r.Classification != "MISMATCH" {
		t.Fatal("mismatched payment automatically repaired")
	}
}

func TestMachinePayProofVaultBindsOriginalOrderAndSeller(t *testing.T) {
	f := fixture(t, nil)
	v, e := NewVault("key_1", map[string][]byte{"key_1": bytes.Repeat([]byte{1}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	encoded, e := v.Seal(f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	if bytes.Contains(encoded, []byte(strings.Repeat("a", 64))) || bytes.Contains(encoded, []byte("original_client_session")) {
		t.Fatal("proof stored as plaintext")
	}
	scope := f.intent.Scope
	scope.Order = "order_foreign"
	scope.PurposeRef = scope.Order
	if p, e := v.Open(scope, encoded, f.now); e == nil {
		p.Close()
		t.Fatal("cross-order cipher proof opened")
	}
	scope = f.intent.Scope
	scope.Seller = "seller_foreign"
	if p, e := v.Open(scope, encoded, f.now); e == nil {
		p.Close()
		t.Fatal("cross-seller cipher proof opened")
	}
}
func TestMachinePayProofVaultRejectsTamperAndExpiredRecovery(t *testing.T) {
	f := fixture(t, nil)
	v, e := NewVault("key_1", map[string][]byte{"key_1": bytes.Repeat([]byte{2}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	encoded, e := v.Seal(f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	if p, e := v.Open(f.intent.Scope, encoded, f.now.Add(time.Minute)); e == nil {
		p.Close()
		t.Fatal("recovery expiry reanchored")
	}
	var record cipherProof
	json.Unmarshal(encoded, &record)
	record.Ciphertext[len(record.Ciphertext)-1] ^= 1
	bad, _ := json.Marshal(record)
	defer clear(bad)
	if p, e := v.Open(f.intent.Scope, bad, f.now); e == nil {
		p.Close()
		t.Fatal("modified cipher proof opened")
	}
}
func TestMachinePayProofVaultRotationRetainsOnlyExplicitOldKey(t *testing.T) {
	f := fixture(t, nil)
	oldKey, newKey := bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32)
	v, e := NewVault("old", map[string][]byte{"old": oldKey})
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := v.Seal(f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	v.Close()
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	rotated, e := NewVault("new", map[string][]byte{"old": oldKey, "new": newKey})
	if e != nil {
		t.Fatal(e)
	}
	p, e := rotated.Open(f.intent.Scope, encoded, f.now)
	rotated.Close()
	if e != nil {
		t.Fatal("explicit retained key cannot resolve original")
	}
	p.Close()
	missing, e := NewVault("new", map[string][]byte{"new": newKey})
	if e != nil {
		t.Fatal(e)
	}
	defer missing.Close()
	if p, e := missing.Open(f.intent.Scope, encoded, f.now); e == nil {
		p.Close()
		t.Fatal("missing original key silently replaced")
	}
}
func TestMachinePaySecretsAreRedactedFromDiagnosticFormatting(t *testing.T) {
	f := fixture(t, nil)
	p := proofFor(t)
	v, e := NewVault("key_1", map[string][]byte{"key_1": bytes.Repeat([]byte{9}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	config := AlipayConfig{Binding: f.binding, PrivateKey: f.application, AlipayPublicKey: &f.provider.PublicKey, AppAuthToken: []byte("private-raw")}
	for _, value := range []any{config, f.channel, p, v, Observation{safeCode: "private-raw"}} {
		view := fmt.Sprintf("%+v %#v", value, value)
		encoded, e := json.Marshal(value)
		if e != nil || !strings.Contains(string(encoded), "redacted") || strings.Contains(view, "private-raw") || strings.Contains(view, "original_client_session") {
			t.Fatal("sensitive payment diagnostics")
		}
	}
}

type ignoredCloseBody struct {
	entered, release chan struct{}
	once             sync.Once
}

func (b *ignoredCloseBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}
func (b *ignoredCloseBody) Close() error { return nil }
func TestMachinePayCloseTimeoutRetainsOwnerUntilActualResponseReadReturns(t *testing.T) {
	body := &ignoredCloseBody{entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(body.release) })
	defer release()
	f := fixture(t, func(*payFixture, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
	})
	returned := make(chan struct{})
	go func() { defer close(returned); _, _ = f.channel.Precreate(context.Background(), f.intent) }()
	<-body.entered
	ctx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if f.channel.Close(ctx) == nil {
		t.Fatal("live response operation falsely joined")
	}
	select {
	case <-f.channel.done:
		t.Fatal("owner released before actual body return")
	default:
	}
	release()
	<-returned
	joined, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if f.channel.Close(joined) != nil {
		t.Fatal("actual response completion did not join")
	}
}
