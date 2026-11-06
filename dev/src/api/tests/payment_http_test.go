package api_test

// SOURCE_ONLY. Native SQL + in-memory synthetic RSA2 responses, never Alipay.
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/internal/payments"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/ids"
)

type paymentHTTPFixture struct {
	h          *harness
	offer      testsupport.Offer
	capability string
	calls      atomic.Int32
	fail       bool
	committed  atomic.Bool
}

func paymentHarness(t *testing.T) *paymentHTTPFixture {
	t.Helper()
	if db == nil {
		t.Skip("native PostgreSQL required for future payment HTTP validation")
	}
	f := &paymentHTTPFixture{capability: ids.New("pcap"), offer: testsupport.SeedOffer(t, ctx, db, "PLAINTEXT", ids.New("region"))}
	zero := 0
	f.offer.Terms.Price.CommissionBPS = &zero
	f.offer.Version(t, ctx, db, 2)
	execPayment := func(query string, args ...any) {
		if _, e := db.Pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	execPayment(`UPDATE p11.listings SET current_revision=2 WHERE id=$1`, f.offer.ID)
	f.h = newHarnessConfigured(t, 1000, 100, func(c *controlhttp.Config) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		b := payments.Binding{Profile: payments.Profile, Revision: ids.New("rev"), Provider: "ALIPAY", Environment: "SANDBOX", Seller: f.offer.Seller.UserID, AppID: "2026000000000001", ServiceID: "synthetic_http_service", AuthoritySubject: ids.New("subject"), CredentialRef: ids.New("secret"), AuthorizationRef: ids.New("ev"), AuthorizationHash: strings.Repeat("a", 64), QualificationRef: ids.New("ev"), QualificationHash: strings.Repeat("b", 64), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), ResolveUntil: now.Add(2 * time.Hour)}
		b.SigningKeyHash, _ = payments.PublicKeyHash(&fixtureKeys.application.PublicKey)
		b.AlipayKeyHash, _ = payments.PublicKeyHash(&fixtureKeys.platform.PublicKey)
		policy := payments.SalePolicy{GoodsName: "合成原个人卖家服务", SKU: f.offer.Terms.SKU, Mode: "PLAINTEXT", Privacy: "STANDARD", Collection: "SELLER_DIRECT_NO_FEE", MaximumWindowSeconds: 600, RefundEvidenceRef: ids.New("ev"), RefundEvidenceHash: strings.Repeat("c", 64), CommissionEvidenceRef: ids.New("ev"), CommissionEvidenceHash: strings.Repeat("d", 64)}
		reviewer := ids.New("usr")
		digest := sha256.Sum256([]byte(reviewer))
		execPayment(`INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, reviewer, fmt.Sprintf("%x", digest))
		execPayment(`INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'PAYMENT_REVIEWER','self',$2)`, ids.New("role"), reviewer)
		scope, _ := json.Marshal(map[string]string{"payment_profile": payments.Profile, "seller_id": b.Seller, "app_id": b.AppID, "service_id": b.ServiceID, "environment": b.Environment, "revision": b.Revision, "authority_subject": b.AuthoritySubject, "signing_key_hash": b.SigningKeyHash, "alipay_key_hash": b.AlipayKeyHash, "sku_id": policy.SKU, "mode": policy.Mode, "privacy": policy.Privacy, "collection": policy.Collection})
		refs := []string{b.AuthorizationRef, b.QualificationRef, policy.RefundEvidenceRef, policy.CommissionEvidenceRef}
		kinds := []string{"MACHINE_PAY_AUTHORITY_1", "MACHINE_PAY_PERSONAL_SELLER_1", "MACHINE_PAY_REFUND_ROUTE_1", "MACHINE_PAY_COMMISSION_ROUTE_1"}
		hashes := []string{b.AuthorizationHash, b.QualificationHash, policy.RefundEvidenceHash, policy.CommissionEvidenceHash}
		for i, id := range refs {
			execPayment(`INSERT INTO p11.evidence(id,evidence_type,seller_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,$2,$3,$1,$4,$5,'APPROVED',$6,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '3 hours')`, id, kinds[i], b.Seller, hashes[i], scope, reviewer)
		}
		execPayment(`INSERT INTO p11.feature_gates(id,scope_key,enabled,evidence_ids,approved_by) VALUES($1,$2,true,$3,$4)`, ids.New("gate"), "machine-pay:"+b.Digest(), refs, reviewer)
		channel, e := payments.NewAlipay(payments.AlipayConfig{Binding: b, PrivateKey: fixtureKeys.application, AlipayPublicKey: &fixtureKeys.platform.PublicKey, Transport: transport(func(r *http.Request) (*http.Response, error) {
			f.calls.Add(1)
			if r.ParseForm() != nil || r.PostForm.Get("method") != "alipay.aipay.agent.payment.precreate" {
				return nil, errors.New("synthetic payment method")
			}
			var committed bool
			if e := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.channel_requests WHERE operation='PAY' AND state='SENT' AND original_request_key=$1)`, func() string {
				var biz struct {
					Number string `json:"out_trade_no"`
				}
				_ = json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
				return biz.Number
			}()).Scan(&committed); e != nil {
				return nil, e
			}
			f.committed.Store(committed)
			if f.fail {
				return nil, errors.New("synthetic missing response")
			}
			node := []byte(`{"code":"10000","amt_pre_pay_id":"amt_original_http"}`)
			sum := sha256.Sum256(node)
			sign, e := rsa.SignPKCS1v15(rand.Reader, fixtureKeys.platform, crypto.SHA256, sum[:])
			if e != nil {
				return nil, e
			}
			envelope := fmt.Sprintf(`{"alipay_aipay_agent_payment_precreate_response":%s,"sign":%q}`, node, base64.StdEncoding.EncodeToString(sign))
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope))}, nil
		})})
		if e != nil {
			t.Fatal(e)
		}
		proof, e := payments.NewVault("proof_test", map[string][]byte{"proof_test": bytes.Repeat([]byte{41}, 32)})
		if e != nil {
			t.Fatal(e)
		}
		receipts, e := payments.NewVault("receipt_test", map[string][]byte{"receipt_test": bytes.Repeat([]byte{42}, 32)})
		if e != nil {
			t.Fatal(e)
		}
		machine, e := payments.NewMachine(db, proof, receipts, []payments.Registration{{CapabilityRef: f.capability, Channel: channel, Policy: policy}})
		if e != nil {
			t.Fatal(e)
		}
		c.Payments = machine
		t.Cleanup(func() {
			if machine.Close(context.Background()) != nil {
				t.Error("original payment operation did not join")
				return
			}
			if channel.Close(context.Background()) != nil {
				t.Error("original payment channel did not join")
				return
			}
			proof.Close()
			receipts.Close()
		})
	}, nil)
	return f
}
func (f *paymentHTTPFixture) order(t *testing.T, headers http.Header) trade.Order {
	t.Helper()
	in := f.offer.Input()
	in.ListingRevision = 2
	headers = headers.Clone()
	headers.Set("Idempotency-Key", ids.New("quote"))
	q := decode[trade.Quote](t, f.h.request(t, "POST", "/quotes", in, headers), 201)
	headers.Set("Idempotency-Key", ids.New("order"))
	return decode[trade.Order](t, f.h.request(t, "POST", "/orders", trade.OrderInput{QuoteID: q.ID, ContractHash: q.Contract.Hash, DisclosureVersion: q.Contract.Policy.Disclosure, DisclosureAcceptedAt: q.CreatedAt, CallerKey: ids.New("caller")}, headers), 201)
}
func TestHTTPMachinePayCreatesAndReadsOnlyOriginalPayment(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	o := f.order(t, headers)
	headers.Set("Idempotency-Key", ids.New("pay"))
	input := payments.IntentInput{ExpectedOrderVersion: o.Version, ChannelCapabilityRef: f.capability}
	url := "/orders/" + o.ID + "/payment-intents"
	p := decode[payments.Payment](t, f.h.request(t, "POST", url, input, headers), 201)
	if p.State != "PENDING" || p.ChannelActionRef != "amt_original_http" || f.calls.Load() != 1 || !f.committed.Load() {
		t.Fatal("original HTTP payment did not commit before channel IO")
	}
	replay := f.h.request(t, "POST", url, input, headers)
	if decode[payments.Payment](t, replay, 201).ID != p.ID || replay.header.Get("Idempotency-Replayed") != "true" || f.calls.Load() != 1 {
		t.Fatal("HTTP replay created another payment")
	}
	response := f.h.request(t, "GET", "/payments/"+p.ID, nil, headers)
	if decode[payments.Payment](t, response, 200).ID != p.ID || response.header.Get("ETag") == "" {
		t.Fatal("original payment could not be read")
	}
	other, _ := f.h.login(t, "MINIAPP")
	expectError(t, f.h.request(t, "GET", "/payments/"+p.ID, nil, bearer(other.AccessToken)), 404, "NOT_FOUND")
	for _, secret := range []string{"private_key", "scope_hash", "payment_proof", "channel_binding_snapshot"} {
		if bytes.Contains(response.body, []byte(secret)) {
			t.Fatal("private channel data escaped payment DTO")
		}
	}
}
func TestHTTPMachinePayUnknownDispatchReplaysWithoutFinancialRetry(t *testing.T) {
	f := paymentHarness(t)
	s, _ := f.h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	o := f.order(t, headers)
	headers.Set("Idempotency-Key", ids.New("pay"))
	f.fail = true
	input := payments.IntentInput{ExpectedOrderVersion: o.Version, ChannelCapabilityRef: f.capability}
	url := "/orders/" + o.ID + "/payment-intents"
	expectError(t, f.h.request(t, "POST", url, input, headers), 503, "UNAVAILABLE")
	p := decode[payments.Payment](t, f.h.request(t, "POST", url, input, headers), 201)
	if p.State != "UNKNOWN" || f.calls.Load() != 1 {
		t.Fatal("unknown HTTP payment was retried or claimed paid")
	}
}
func TestHTTPMachinePayRejectsUnregisteredBuyerFieldsAndBrowserCSRF(t *testing.T) {
	f := paymentHarness(t)
	s, cookie := f.h.login(t, "WEB")
	headers := http.Header{"Origin": []string{f.h.origin}, "Idempotency-Key": []string{ids.New("pay")}}
	url := "/orders/original_order/payment-intents"
	input := map[string]any{"expected_order_version": 1, "channel_capability_ref": f.capability, "app_id": "buyer_cannot_choose_merchant"}
	expectError(t, f.h.request(t, "POST", url, input, headers, cookie), 403, "FORBIDDEN")
	headers.Set("X-CSRF-Token", s.CSRF)
	expectError(t, f.h.request(t, "POST", url, input, headers, cookie), 400, "INVALID_REQUEST")
	if f.calls.Load() != 0 {
		t.Fatal("unapproved HTTP body reached original channel")
	}
}
func TestHTTPMachinePayRemainsUnavailableWithoutExplicitHost(t *testing.T) {
	h := newHarness(t, 100, 100)
	s, _ := h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	headers.Set("Idempotency-Key", ids.New("pay"))
	expectError(t, h.request(t, "POST", "/orders/original_order/payment-intents", payments.IntentInput{ExpectedOrderVersion: 1, ChannelCapabilityRef: "cap_original"}, headers), 503, "UNAVAILABLE")
	expectError(t, h.request(t, "GET", "/payments/original_payment", nil, headers), 503, "UNAVAILABLE")
}
