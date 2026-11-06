package integration_test

// SOURCE_ONLY: original checkout recovery definitions. No financial channel,
// database or test is executed while the full source phase remains open.
import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/ids"
)

func TestCheckoutOptionsRemainOriginalAndPerformNoFinancialIO(t *testing.T) {
	f := machineFixture(t)
	var contract struct {
		Hash string `json:"contract_hash"`
	}
	if json.Unmarshal(f.order.Contract, &contract) != nil {
		t.Fatal("original contract unreadable")
	}
	options, e := f.machine.Options(ctx, f.buyer, f.order.ID)
	if e != nil || options.Order != f.order.ID || options.Seller != f.order.SellerID || options.ContractHash != contract.Hash || options.Version != f.order.Version || options.Amount != 500 || options.Currency != "CNY" || options.Environment != "SANDBOX" || len(options.Items) != 1 || options.Items[0].Capability != f.capability || f.calls.Load() != 0 {
		t.Fatal("original sale options changed scope or performed financial IO", e)
	}
	var count int
	if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.payments WHERE order_id=$1`, f.order.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("options created an intent", e)
	}
}
func TestCheckoutOriginalLookupIsNotNewPurchasePermission(t *testing.T) {
	f := machineFixture(t)
	if _, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID); e == nil {
		t.Fatal("absent original payment returned an invented intent")
	}
	p := f.create(t)
	recovered, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID)
	if e != nil || recovered.ID != p.ID || recovered.State != "CREATED" || f.calls.Load() != 0 {
		t.Fatal("lost creation result could not recover the same intent", e)
	}
	if _, e = f.machine.Options(ctx, f.buyer, f.order.ID); !errors.Is(e, payments.ErrConflict) {
		t.Fatal("existing intent allowed a fresh sale choice", e)
	}
	if _, e = f.machine.CreateIntent(ctx, f.buyer, f.order.ID, payments.IntentInput{ExpectedOrderVersion: f.order.Version, ChannelCapabilityRef: f.capability}, ids.New("other")); !errors.Is(e, payments.ErrConflict) {
		t.Fatal("new key created a second original payment", e)
	}
}
func TestCheckoutOptionsAndRecoveryNeverDiscloseAnotherBuyer(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	other := buyer(t)
	other.Scopes = append(other.Scopes, "payment:read", "payment:create")
	if _, e := f.machine.Options(ctx, other, f.order.ID); e == nil {
		t.Fatal("other buyer read sale scope")
	}
	if _, e := f.machine.GetForOrder(ctx, other, f.order.ID); e == nil {
		t.Fatal("other buyer found original payment")
	}
	if _, e := f.machine.Challenge(ctx, other, p.ID); e == nil {
		t.Fatal("other buyer read original prepay challenge")
	}
	if _, e := f.machine.GetRecovery(ctx, other, p.ID); e == nil {
		t.Fatal("other buyer read proof metadata")
	}
	if f.calls.Load() != 1 {
		t.Fatal("read-only rejection reached financial channel")
	}
}
func TestCheckoutChallengeReusesOnlyCommittedOriginalReference(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	for range 2 {
		challenge, e := f.machine.Challenge(ctx, f.buyer, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		redacted, _ := json.Marshal(challenge)
		if bytes.Contains(redacted, []byte(p.ChannelActionRef)) {
			t.Fatal("ordinary serialization disclosed prepay reference")
		}
		w := httptest.NewRecorder()
		if e = challenge.WriteChallenge(w, time.Now()); e != nil || w.Code != 402 || w.Header().Get("amtPaymentLink") != p.ChannelActionRef {
			t.Fatal("challenge changed original prepay identity", e)
		}
		var body struct {
			Payment  string `json:"payment_id"`
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Payment != p.ID || body.Amount != "5.00" || body.Currency != "CNY" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("invalid original payment challenge")
		}
	}
	if f.calls.Load() != 1 {
		t.Fatal("reading a challenge repeated precreation")
	}
}
func TestCheckoutUnknownDispatchCannotBecomeFreshChallenge(t *testing.T) {
	f := machineFixture(t)
	p := f.create(t)
	f.failPrecreate = true
	if _, e := f.machine.Precreate(ctx, f.buyer, p.ID); !errors.Is(e, payments.ErrUnknown) {
		t.Fatal(e)
	}
	if _, e := f.machine.Challenge(ctx, f.buyer, p.ID); !errors.Is(e, payments.ErrUnknown) {
		t.Fatal("unknown dispatch returned a chargeable challenge", e)
	}
	same, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID)
	if e != nil || same.ID != p.ID || same.State != "UNKNOWN" || f.calls.Load() != 1 {
		t.Fatal("unknown original payment was replaced", e)
	}
}
func TestCheckoutRevokedNewSaleKeepsOriginalPaymentReadable(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.policy.RefundEvidenceRef)
	if _, e := f.machine.Challenge(ctx, f.buyer, p.ID); !errors.Is(e, payments.ErrBinding) {
		t.Fatal("revoked sale route returned fresh payment action", e)
	}
	same, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID)
	if e != nil || same.ID != p.ID || same.State != "PENDING" {
		t.Fatal("sales revocation hid original financial obligation", e)
	}
	r, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "checkout_private_session"), ids.New("idem"))
	if e != nil {
		t.Fatal("revoked sales blocked original financial resolution", e)
	}
	got, e := f.machine.GetRecovery(ctx, f.buyer, p.ID)
	if e != nil || got.ID != r.ID || got.Payment != p.ID || got.State != "PENDING" || f.calls.Load() != 1 {
		t.Fatal("original proof recovery unavailable", e)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"checkout_private_session", "proof_ciphertext", "Payment-Proof", p.ChannelActionRef} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private financial input escaped recovery DTO")
		}
	}
}
func TestCheckoutSubmittedRecoveryCannotClaimPaidOrExecution(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	if _, e := f.machine.GetRecovery(ctx, f.buyer, p.ID); e == nil {
		t.Fatal("unsubmitted proof received invented recovery")
	}
	r, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "original_checkout_session"), ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	got, e := f.machine.GetRecovery(ctx, f.buyer, p.ID)
	if e != nil || got.ID != r.ID || got.State != "PENDING" {
		t.Fatal("submitted original recovery lost", e)
	}
	original, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID)
	if e != nil || original.State != "PENDING" || original.Settled != 0 {
		t.Fatal("proof upload was treated as paid", e)
	}
	var state string
	if e = db.Pool.QueryRow(ctx, `SELECT state FROM p11.orders WHERE id=$1`, f.order.ID).Scan(&state); e != nil || state != "WAITING_PAYMENT" {
		t.Fatal("proof upload authorized execution", e)
	}
}
func TestCheckoutVerificationRemainsIndependentOfFundingAndExecution(t *testing.T) {
	f := machineFixture(t)
	p := f.pending(t)
	if _, e := f.machine.SubmitProof(ctx, f.buyer, p.ID, f.proof(t, "verified_checkout_session"), ids.New("idem")); e != nil {
		t.Fatal(e)
	}
	if worked, e := f.machine.RecoverOne(ctx); e != nil || !worked {
		t.Fatal(e)
	}
	got, e := f.machine.GetRecovery(ctx, f.buyer, p.ID)
	if e != nil || got.State != "VERIFIED" {
		t.Fatal("original verify outcome unavailable", e)
	}
	paid, e := f.machine.GetForOrder(ctx, f.buyer, f.order.ID)
	if e != nil || paid.ID != p.ID || paid.State != "SETTLED" || paid.ChannelReference != f.trade {
		t.Fatal("original paid fact missing", e)
	}
	var state string
	var allocated bool
	if e = db.Pool.QueryRow(ctx, `SELECT state,EXISTS(SELECT 1 FROM p11.order_fundings WHERE order_id=$1) FROM p11.orders WHERE id=$1`, f.order.ID).Scan(&state, &allocated); e != nil || state != "WAITING_PAYMENT" || allocated {
		t.Fatal("verification silently performed funds admission", e)
	}
}
