package integration_test

// SOURCE_ONLY: native SQL + synthetic signed transport definitions. These do
// not prove merchant qualification, real Alipay/refunds, funds or user devices.
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
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/payments"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/ids"
)

type machineTestTransport func(*http.Request) (*http.Response, error)

func (f machineTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type machineTestKeys struct{ app, provider *rsa.PrivateKey }

var machineKeys = sync.OnceValues(func() (machineTestKeys, error) {
	a, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return machineTestKeys{}, e
	}
	b, e := rsa.GenerateKey(rand.Reader, 2048)
	return machineTestKeys{a, b}, e
})

type machinePaymentFixture struct {
	machine               *payments.Machine
	channel               *payments.Alipay
	vault                 *payments.Vault
	receipts              *payments.Vault
	buyer                 identity.Principal
	order                 trade.Order
	binding               payments.Binding
	policy                payments.SalePolicy
	capability, trade     string
	calls                 atomic.Int32
	failPrecreate         bool
	observedCommittedSent atomic.Bool
}

func machineFixture(t *testing.T) *machinePaymentFixture {
	t.Helper()
	requireDB(t)
	return machineFixtureOffer(t, testsupport.SeedOffer(t, ctx, db, "PLAINTEXT", ids.New("region")))
}

func machineFixtureOffer(t *testing.T, offer testsupport.Offer) *machinePaymentFixture {
	t.Helper()
	requireDB(t)
	f := &machinePaymentFixture{buyer: buyer(t), capability: ids.New("pcap")}
	f.buyer.Scopes = append(f.buyer.Scopes, "payment:create", "payment:read")
	zero := 0
	offer.Terms.Price.CommissionBPS = &zero
	offer.Version(t, ctx, db, 2)
	exec(t, `UPDATE p11.listings SET current_revision=2 WHERE id=$1`, offer.ID)
	in := offer.Input()
	in.ListingRevision = 2
	q, e := (trade.Service{DB: db}).CreateQuote(ctx, f.buyer, in, ids.New("quote"))
	if e != nil {
		t.Fatal(e)
	}
	f.order, e = (trade.Service{DB: db}).CreateOrder(ctx, f.buyer, confirm(q), ids.New("order"))
	if e != nil {
		t.Fatal(e)
	}
	keys, e := machineKeys()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.binding = payments.Binding{Profile: payments.Profile, Revision: ids.New("rev"), Provider: "ALIPAY", Environment: "SANDBOX", Seller: offer.Seller.UserID, AppID: "2026000000000001", ServiceID: ids.New("svc"), AuthoritySubject: ids.New("subject"), CredentialRef: ids.New("secret"), AuthorizationRef: ids.New("ev"), AuthorizationHash: strings.Repeat("a", 64), QualificationRef: ids.New("ev"), QualificationHash: strings.Repeat("b", 64), ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), ResolveUntil: now.Add(2 * time.Hour)}
	// Alipay service_id is independently registered and bounded to 32 chars.
	f.binding.ServiceID = "API_" + f.order.ID[len(f.order.ID)-20:]
	f.binding.SigningKeyHash, _ = payments.PublicKeyHash(&keys.app.PublicKey)
	f.binding.AlipayKeyHash, _ = payments.PublicKeyHash(&keys.provider.PublicKey)
	f.policy = payments.SalePolicy{GoodsName: "合成个人卖家服务", SKU: offer.Terms.SKU, Mode: "PLAINTEXT", Privacy: "STANDARD", Collection: "SELLER_DIRECT_NO_FEE", MaximumWindowSeconds: 600, RefundEvidenceRef: ids.New("ev"), RefundEvidenceHash: strings.Repeat("c", 64), CommissionEvidenceRef: ids.New("ev"), CommissionEvidenceHash: strings.Repeat("d", 64)}
	reviewer := user(t)
	exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'PAYMENT_REVIEWER','self',$2)`, ids.New("role"), reviewer.UserID)
	scope, _ := json.Marshal(map[string]string{"payment_profile": payments.Profile, "seller_id": f.binding.Seller, "app_id": f.binding.AppID, "service_id": f.binding.ServiceID, "environment": f.binding.Environment, "revision": f.binding.Revision, "authority_subject": f.binding.AuthoritySubject, "signing_key_hash": f.binding.SigningKeyHash, "alipay_key_hash": f.binding.AlipayKeyHash, "sku_id": f.policy.SKU, "mode": f.policy.Mode, "privacy": f.policy.Privacy, "collection": f.policy.Collection})
	proofs := []struct{ id, digest, kind string }{{f.binding.AuthorizationRef, f.binding.AuthorizationHash, "MACHINE_PAY_AUTHORITY_1"}, {f.binding.QualificationRef, f.binding.QualificationHash, "MACHINE_PAY_PERSONAL_SELLER_1"}, {f.policy.RefundEvidenceRef, f.policy.RefundEvidenceHash, "MACHINE_PAY_REFUND_ROUTE_1"}, {f.policy.CommissionEvidenceRef, f.policy.CommissionEvidenceHash, "MACHINE_PAY_COMMISSION_ROUTE_1"}}
	var evidence []string
	for _, p := range proofs {
		evidence = append(evidence, p.id)
		exec(t, `INSERT INTO p11.evidence(id,evidence_type,seller_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until)
VALUES($1,$2,$3,$1,$4,$5,'APPROVED',$6,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '3 hours')`, p.id, p.kind, f.binding.Seller, p.digest, scope, reviewer.UserID)
	}
	exec(t, `INSERT INTO p11.feature_gates(id,scope_key,enabled,evidence_ids,approved_by) VALUES($1,$2,true,$3,$4)`, ids.New("gate"), "machine-pay:"+f.binding.Digest(), evidence, reviewer.UserID)
	digest := sha256.Sum256([]byte(f.order.ID))
	digits := new(big.Int).SetBytes(digest[:13]).String()
	f.trade = strings.Repeat("0", 32-len(digits)) + digits
	f.channel, e = payments.NewAlipay(payments.AlipayConfig{Binding: f.binding, PrivateKey: keys.app, AlipayPublicKey: &keys.provider.PublicKey, Transport: machineTestTransport(func(r *http.Request) (*http.Response, error) {
		f.calls.Add(1)
		if r.ParseForm() != nil {
			return nil, errors.New("synthetic form")
		}
		method := r.PostForm.Get("method")
		var number, state string
		if e := db.Pool.QueryRow(ctx, `SELECT p.original_request_key,c.state FROM p11.payments p JOIN p11.payment_machine_contexts x ON x.payment_id=p.id JOIN p11.channel_requests c ON c.id=x.precreate_request_id WHERE p.order_id=$1`, f.order.ID).Scan(&number, &state); e != nil {
			return nil, e
		}
		if method == "alipay.aipay.agent.payment.precreate" {
			f.observedCommittedSent.Store(state == "SENT")
			if f.failPrecreate {
				return nil, errors.New("synthetic uncertain response")
			}
		}
		node := []byte(`{"code":"10000","amt_pre_pay_id":"amt_test_original"}`)
		if method == "alipay.aipay.agent.payment.verify" {
			node = []byte(fmt.Sprintf(`{"code":"10000","trade_no":%q,"out_trade_no":%q,"amount":"5.00","active":true}`, f.trade, number))
		}
		sum := sha256.Sum256(node)
		signature, e := rsa.SignPKCS1v15(rand.Reader, keys.provider, crypto.SHA256, sum[:])
		if e != nil {
			return nil, e
		}
		envelope := fmt.Sprintf(`{%q:%s,"sign":%q}`, strings.ReplaceAll(method, ".", "_")+"_response", node, base64.StdEncoding.EncodeToString(signature))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope))}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	f.vault, e = payments.NewVault("machine_test_key", map[string][]byte{"machine_test_key": bytes.Repeat([]byte{31}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	f.receipts, e = payments.NewVault("machine_receipt_key", map[string][]byte{"machine_receipt_key": bytes.Repeat([]byte{32}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	f.machine, e = payments.NewMachine(db, f.vault, f.receipts, []payments.Registration{{CapabilityRef: f.capability, Channel: f.channel, Policy: f.policy}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		closeCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		if f.machine.Close(closeCtx) != nil {
			t.Error("machine source owner not joined")
		}
		if f.channel.Close(closeCtx) != nil {
			t.Error("channel source owner not joined")
		}
		f.vault.Close()
		f.receipts.Close()
	})
	return f
}
func (f *machinePaymentFixture) create(t *testing.T) payments.Payment {
	t.Helper()
	p, e := f.machine.CreateIntent(ctx, f.buyer, f.order.ID, payments.IntentInput{ExpectedOrderVersion: f.order.Version, ChannelCapabilityRef: f.capability}, ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func (f *machinePaymentFixture) pending(t *testing.T) payments.Payment {
	t.Helper()
	p := f.create(t)
	value, e := f.machine.Precreate(ctx, f.buyer, p.ID)
	if e != nil || value.State != "PENDING" {
		t.Fatal("original synthetic precreate", e, value.State)
	}
	return value
}
func (f *machinePaymentFixture) proof(t *testing.T, session string) *payments.Proof {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"protocol": map[string]string{"payment_proof": strings.Repeat("z", 64), "trade_no": f.trade}, "method": map[string]string{"client_session": session}})
	p, e := payments.ParseProof(base64.StdEncoding.EncodeToString(raw))
	clear(raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p
}

func TestMachinePaymentIntentCommitsOriginalScopeWithoutChannelIO(t *testing.T) {
	f := machineFixture(t)
	key := ids.New("idem")
	in := payments.IntentInput{ExpectedOrderVersion: f.order.Version, ChannelCapabilityRef: f.capability}
	p, e := f.machine.CreateIntent(ctx, f.buyer, f.order.ID, in, key)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.machine.CreateIntent(ctx, f.buyer, f.order.ID, in, key)
	if e != nil || p.ID != replay.ID || f.calls.Load() != 0 {
		t.Fatal("intent replay created external operation", e)
	}
	var count int
	if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.payment_machine_contexts c JOIN p11.channel_requests q ON q.id=c.precreate_request_id JOIN p11.outbox_events e ON e.aggregate_id=c.payment_id WHERE c.payment_id=$1 AND q.state='INTENT' AND e.event_type='PaymentMachineIntentCreated'`, p.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("intent/outbox not committed together", e, count)
	}
}
func TestMachinePaymentUnknownPrecreateNeverRepeatsFinancialCreate(t *testing.T) {
	f := machineFixture(t)
	p := f.create(t)
	f.failPrecreate = true
	value, e := f.machine.Precreate(ctx, f.buyer, p.ID)
	if !errors.Is(e, payments.ErrUnknown) || value.State != "UNKNOWN" || !f.observedCommittedSent.Load() {
		t.Fatal("external call preceded durable original dispatch", e)
	}
	if _, e = f.machine.Precreate(ctx, f.buyer, p.ID); e == nil || f.calls.Load() != 1 {
		t.Fatal("unknown financial create repeated", e)
	}
}
func TestMachinePaymentProofCiphertextAndOriginalReplayShareOneDurableJob(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	proof := f.proof(t, "original_private_session")
	key := ids.New("idem")
	r, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, proof, key)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, proof, ids.New("idem"))
	if e != nil || again.ID != r.ID {
		t.Fatal("same proof created another recovery", e)
	}
	var cipher, safe []byte
	var count int
	if e = db.Pool.QueryRow(ctx, `SELECT x.proof_ciphertext,(SELECT safe_response::text FROM p11.idempotency_records WHERE response_reference=x.id LIMIT 1),
(SELECT count(*) FROM p11.durable_jobs WHERE kind='VERIFY_MACHINE_PAYMENT' AND target_ref=x.id) FROM p11.payment_machine_recoveries x WHERE x.id=$1`, r.ID).Scan(&cipher, &safe, &count); e != nil {
		t.Fatal(e)
	}
	defer clear(cipher)
	defer clear(safe)
	if count != 1 || bytes.Contains(cipher, []byte("original_private_session")) || bytes.Contains(safe, []byte("original_private_session")) || f.calls.Load() != 1 {
		t.Fatal("proof leaked or verification started before committed job")
	}
	if _, e = f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "changed_private_session"), key); !errors.Is(e, payments.ErrConflict) {
		t.Fatal("same original key accepted changed proof", e)
	}
}
func TestMachinePaymentRecoveryArchivesVerifiedPaidWithoutStartingOrder(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	r, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "original_private_session"), ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	worked, e := f.machine.RecoverOne(ctx)
	if e != nil || !worked {
		t.Fatal("same-proof source worker", e)
	}
	paid, e := f.machine.Get(ctx, f.buyer, p.ID)
	if e != nil || paid.State != "SETTLED" || paid.ChannelReference != f.trade || paid.Settled != 500 {
		t.Fatal("verified original payment not persisted", e)
	}
	var state, recovery string
	var count int
	if e = db.Pool.QueryRow(ctx, `SELECT o.state,x.state,(SELECT count(*) FROM p11.payment_machine_receipts WHERE payment_id=$1)
FROM p11.orders o JOIN p11.payment_machine_recoveries x ON x.payment_id=$1 WHERE o.id=$2 AND x.id=$3`, p.ID, f.order.ID, r.ID).Scan(&state, &recovery, &count); e != nil || state != "WAITING_PAYMENT" || recovery != "VERIFIED" || count != 2 {
		t.Fatal("payment was treated as order execution or signed archive missing", e, state, recovery, count)
	}
	if _, e = db.Pool.Exec(ctx, `UPDATE p11.payments SET state='UNKNOWN' WHERE id=$1`, p.ID); e == nil {
		t.Fatal("paid fact downgraded")
	}
	if _, e = db.Pool.Exec(ctx, `DELETE FROM p11.payment_machine_receipts WHERE payment_id=$1`, p.ID); e == nil {
		t.Fatal("signed financial history deleted")
	}
}
func TestMachinePaymentCannotSettleWithoutSameTransactionSignedReceipt(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.payments SET state='SETTLED',channel_reference=$2 WHERE id=$1`, p.ID, f.trade); e == nil {
		t.Fatal("unsigned database flag became a paid fact")
	}
}
func TestMachinePaymentRevokedRefundEvidenceBlocksBeforeCreatingIntent(t *testing.T) {
	f := machineFixture(t)
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.policy.RefundEvidenceRef)
	if _, e := f.machine.CreateIntent(ctx, f.buyer, f.order.ID, payments.IntentInput{ExpectedOrderVersion: f.order.Version, ChannelCapabilityRef: f.capability}, ids.New("idem")); !errors.Is(e, payments.ErrBinding) {
		t.Fatal("unapproved refund route admitted payment", e)
	}
	var count int
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.payments WHERE order_id=$1`, f.order.ID).Scan(&count); e != nil || count != 0 || f.calls.Load() != 0 {
		t.Fatal("revoked registration created payment or external request", e)
	}
}
func TestMachinePaymentOtherBuyerCannotReadOrSubmitOriginalProof(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	other := buyer(t)
	other.Scopes = append(other.Scopes, "payment:read", "payment:create")
	if _, e := f.machine.Get(ctx, other, p.ID); e == nil {
		t.Fatal("other buyer read original payment")
	}
	if _, e := f.machine.SubmitProof(ctx, other, p.ID, f.proof(t, "other_private_session"), ids.New("idem")); e == nil {
		t.Fatal("other buyer registered original proof recovery")
	}
	if f.calls.Load() != 1 {
		t.Fatal("unauthorized proof reached native channel")
	}
}
func TestMachinePaymentWorkerCannotClaimAnotherRegisteredCapability(t *testing.T) {
	first := machineFixture(t)
	pending := first.pending(t)
	if _, e := first.machine.SubmitProof(ctx, first.buyer, pending.ID, first.proof(t, "first_original_session"), ids.New("idem")); e != nil {
		t.Fatal(e)
	}
	second := machineFixture(t)
	worked, e := second.machine.RecoverOne(ctx)
	if e != nil || worked || second.calls.Load() != 0 || first.calls.Load() != 1 {
		t.Fatal("worker claimed or killed a different capability's original query", e)
	}
	value, e := first.machine.Get(ctx, first.buyer, pending.ID)
	if e != nil || value.State != "PENDING" {
		t.Fatal("other worker changed original funds", e)
	}
}
