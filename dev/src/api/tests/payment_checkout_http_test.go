package api_test

// SOURCE_ONLY. Synthetic original-payment HTTP definitions, not executed and
// not evidence of a qualified miniapp wallet bridge or real payment acceptance.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/ids"
)

func checkoutHTTPProof(t *testing.T, session string) string {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"protocol": map[string]string{"payment_proof": strings.Repeat("p", 64), "trade_no": strings.Repeat("7", 32)}, "method": map[string]string{"client_session": session}})
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	return base64.StdEncoding.EncodeToString(raw)
}
func TestHTTPCheckoutOptionsAndLookupBindOnlyOriginalOrder(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	o := f.order(t, headers)
	var contract struct {
		Hash string `json:"contract_hash"`
	}
	if json.Unmarshal(o.Contract, &contract) != nil {
		t.Fatal("original contract unreadable")
	}
	options := decode[payments.PaymentOptions](t, f.h.request(t, "GET", "/orders/"+o.ID+"/payment-options", nil, headers), 200)
	if options.Order != o.ID || options.Seller != o.SellerID || options.ContractHash != contract.Hash || len(options.Items) != 1 || options.Items[0].Capability != f.capability || f.calls.Load() != 0 {
		t.Fatal("HTTP options changed original sale or initiated payment")
	}
	expectError(t, f.h.request(t, "GET", "/orders/"+o.ID+"/payment", nil, headers), 404, "NOT_FOUND")
	headers.Set("Idempotency-Key", ids.New("pay"))
	p := decode[payments.Payment](t, f.h.request(t, "POST", "/orders/"+o.ID+"/payment-intents", payments.IntentInput{ExpectedOrderVersion: o.Version, ChannelCapabilityRef: f.capability}, headers), 201)
	same := decode[payments.Payment](t, f.h.request(t, "GET", "/orders/"+o.ID+"/payment", nil, headers), 200)
	if same.ID != p.ID || same.State != "PENDING" || f.calls.Load() != 1 {
		t.Fatal("original lookup created another payment")
	}
	expectError(t, f.h.request(t, "GET", "/orders/"+o.ID+"/payment-options", nil, headers), 409, "CONFLICT")
	other, _ := f.h.login(t, "MINIAPP")
	expectError(t, f.h.request(t, "GET", "/orders/"+o.ID+"/payment", nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
}
func TestHTTPCheckoutOriginal402IsNotOrdinarySDKOrNativeTradePay(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	h := bearer(s.AccessToken)
	o := f.order(t, h)
	h.Set("Idempotency-Key", ids.New("pay"))
	p := decode[payments.Payment](t, f.h.request(t, "POST", "/orders/"+o.ID+"/payment-intents", payments.IntentInput{ExpectedOrderVersion: o.Version, ChannelCapabilityRef: f.capability}, h), 201)
	for range 2 {
		r := f.h.request(t, "GET", "/payments/"+p.ID+"/challenge", nil, h)
		body := decode[map[string]any](t, r, 402)
		if r.header.Get("amtPaymentLink") != p.ChannelActionRef || body["payment_id"] != p.ID || body["amount"] != "5.00" || body["currency"] != "CNY" || r.header.Get("Cache-Control") != "no-store" {
			t.Fatal("402 challenge did not preserve original payment")
		}
		for _, forbidden := range []string{"tradePay", "https://", "client_session", "payment_proof"} {
			if bytes.Contains(r.body, []byte(forbidden)) {
				t.Fatal("wallet execution or private proof invented in 402 body")
			}
		}
	}
	if f.calls.Load() != 1 {
		t.Fatal("challenge issued financial IO")
	}
}
func TestHTTPCheckoutProof202IsDurableMetadataAndReplayOnly(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	h := bearer(s.AccessToken)
	o := f.order(t, h)
	h.Set("Idempotency-Key", ids.New("pay"))
	p := decode[payments.Payment](t, f.h.request(t, "POST", "/orders/"+o.ID+"/payment-intents", payments.IntentInput{ExpectedOrderVersion: o.Version, ChannelCapabilityRef: f.capability}, h), 201)
	expectError(t, f.h.request(t, "GET", "/payments/"+p.ID+"/proof-recovery", nil, h), 404, "NOT_FOUND")
	h.Set("Idempotency-Key", ids.New("proof"))
	h.Set("Payment-Proof", checkoutHTTPProof(t, "checkout_http_private"))
	path := "/payments/" + p.ID + "/proofs"
	first := f.h.request(t, "POST", path, map[string]any{}, h)
	r := decode[payments.Recovery](t, first, 202)
	replay := f.h.request(t, "POST", path, map[string]any{}, h)
	again := decode[payments.Recovery](t, replay, 202)
	if r.Payment != p.ID || r.State != "PENDING" || again.ID != r.ID || first.header.Get("Retry-After") != "2" || replay.header.Get("Idempotency-Replayed") != "true" || f.calls.Load() != 1 {
		t.Fatal("proof upload verified inline or lost original replay")
	}
	h.Del("Payment-Proof")
	got := f.h.request(t, "GET", "/payments/"+p.ID+"/proof-recovery", nil, h)
	if decode[payments.Recovery](t, got, 200).ID != r.ID {
		t.Fatal("original recovery metadata unavailable")
	}
	for _, response := range []response{first, replay, got} {
		for _, secret := range []string{"checkout_http_private", "proof_ciphertext", "payment_proof", p.ChannelActionRef} {
			if bytes.Contains(response.body, []byte(secret)) {
				t.Fatal("secret escaped safe recovery response")
			}
		}
	}
	if decode[payments.Payment](t, f.h.request(t, "GET", "/payments/"+p.ID, nil, h), 200).State != "PENDING" {
		t.Fatal("202 became paid")
	}
	h.Set("Payment-Proof", checkoutHTTPProof(t, "changed_checkout_http_private"))
	expectError(t, f.h.request(t, "POST", path, map[string]any{}, h), 409, "CONFLICT")
}
func TestHTTPCheckoutProofHeaderCannotEnterOtherOperationsOrBody(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	h := bearer(s.AccessToken)
	h.Set("Idempotency-Key", ids.New("proof"))
	h.Set("Payment-Proof", checkoutHTTPProof(t, "not_a_real_wallet_secret"))
	expectError(t, f.h.request(t, "GET", "/orders/original_order/payment", nil, h), 400, "INVALID_REQUEST")
	h.Add("Payment-Proof", h.Get("Payment-Proof"))
	expectError(t, f.h.request(t, "POST", "/payments/original_payment/proofs", map[string]any{}, h), 400, "INVALID_REQUEST")
	h.Del("Payment-Proof")
	for _, body := range []string{`null`, `[]`, `{"Payment-Proof":"disallowed"}`, `{"proof":"disallowed"}`, `{} {}`} {
		expectError(t, f.h.request(t, "POST", "/payments/original_payment/proofs", body, h), 400, "INVALID_REQUEST")
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid credential reached financial channel")
	}
}
func TestHTTPCheckoutWebCSRFAndProofCORSRemainScoped(t *testing.T) {
	f := paymentHarness(t)
	_, cookie := f.h.login(t, "WEB")
	path := "/payments/original_payment/proofs"
	h := http.Header{"Origin": []string{f.h.origin}, "Idempotency-Key": []string{ids.New("proof")}, "Payment-Proof": []string{checkoutHTTPProof(t, "csrf_private")}}
	expectError(t, f.h.request(t, "POST", path, map[string]any{}, h, cookie), 403, "FORBIDDEN")
	preflight := http.Header{"Origin": []string{f.h.origin}, "Access-Control-Request-Method": []string{"POST"}, "Access-Control-Request-Headers": []string{"content-type,x-csrf-token,idempotency-key,payment-proof"}}
	r := f.h.request(t, "OPTIONS", path, nil, preflight)
	if r.status != 204 || !strings.Contains(r.header.Get("Access-Control-Allow-Headers"), "Payment-Proof") {
		t.Fatal("dedicated proof preflight not allowed")
	}
	expectError(t, f.h.request(t, "OPTIONS", "/orders", nil, preflight), 403, "FORBIDDEN")
	if f.calls.Load() != 0 {
		t.Fatal("CSRF/preflight initiated payment")
	}
}
func TestHTTPCheckoutAllNewHandlersRequireConfiguredPaymentOwner(t *testing.T) {
	h := newHarness(t, 100, 100)
	s, _ := h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	for _, path := range []string{"/orders/original_order/payment-options", "/orders/original_order/payment", "/payments/original_payment/challenge", "/payments/original_payment/proof-recovery"} {
		expectError(t, h.request(t, "GET", path, nil, headers), 503, "UNAVAILABLE")
	}
	headers.Set("Idempotency-Key", ids.New("proof"))
	headers.Set("Payment-Proof", checkoutHTTPProof(t, "unconfigured_private"))
	expectError(t, h.request(t, "POST", "/payments/original_payment/proofs", map[string]any{}, headers), 503, "UNAVAILABLE")
}
