package payments

// SOURCE_ONLY: these definitions exercise synthetic native signatures and
// archive bytes when eventually run; no current compilation/test has occurred.
import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func paidArchiveFixture(t *testing.T) (*payFixture, Snapshot, []byte) {
	t.Helper()
	f := fixture(t, nil)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encrypted, e := v.SealReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encrypted)
	raw, e := v.OpenReceipt(f.intent.Scope, encrypted)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { clear(raw) })
	paid := paidSnapshot(f)
	paid.EvidenceHash = o.evidenceHash
	paid.ObservedAt = o.observed
	return f, paid, raw
}
func TestOriginalPaidArchiveIsReverifiedWithoutChannelIO(t *testing.T) {
	f, paid, raw := paidArchiveFixture(t)
	calls := f.calls.Load()
	o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, raw)
	defer o.Close()
	if e != nil || !o.IsSettled() || o.evidenceHash != paid.EvidenceHash || !o.observed.Equal(paid.ObservedAt) || f.calls.Load() != calls {
		t.Fatal("archive verification used external IO or changed original paid fact")
	}
}
func TestOriginalPaidArchiveRejectsForeignFrozenFinancialMetadata(t *testing.T) {
	f, paid, raw := paidArchiveFixture(t)
	calls := f.calls.Load()
	for _, variant := range []string{"scope", "amount", "trade", "hash", "state", "observation"} {
		t.Run(variant, func(t *testing.T) {
			wrong := paid
			switch variant {
			case "scope":
				wrong.Scope.Buyer = "other_buyer"
			case "amount":
				wrong.Amount++
			case "trade":
				wrong.ChannelReference = strings.Repeat("2", 32)
			case "hash":
				wrong.EvidenceHash = strings.Repeat("0", 64)
			case "state":
				wrong.State = "PENDING"
			case "observation":
				wrong.ObservedAt = f.intent.CreatedAt.Add(-time.Second)
			}
			o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, wrong, raw)
			defer o.Close()
			if e == nil || o.IsSettled() {
				t.Fatal("original archive crossed financial scope")
			}
		})
	}
	if f.calls.Load() != calls {
		t.Fatal("failed offline verification queried a different payment")
	}
}
func TestOriginalPaidArchiveRejectsModifiedRawNodeAndDuplicateKeys(t *testing.T) {
	f, paid, raw := paidArchiveFixture(t)
	for _, variant := range []string{"signature", "amount", "duplicate", "error"} {
		t.Run(variant, func(t *testing.T) {
			changed := string(raw)
			switch variant {
			case "signature":
				changed = strings.Replace(changed, `"sign":"`, `"sign":"A`, 1)
			case "amount":
				changed = strings.Replace(changed, `"0.01"`, `"0.02"`, 1)
			case "duplicate":
				changed = strings.Replace(changed, `"code":"10000"`, `"code":"10000","code":"10000"`, 1)
			case "error":
				changed = `{"error_response":{"code":"10000"},"sign":"AAAA"}`
			}
			o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, []byte(changed))
			defer o.Close()
			if e == nil || o.IsSettled() {
				t.Fatal("tampered raw signature/node/JSON became paid authority")
			}
		})
	}
}
func TestOriginalPaidArchiveDecryptionAloneCannotManufactureSignature(t *testing.T) {
	f, paid, _ := paidArchiveFixture(t)
	v := receiptVault(t)
	forged := Observation{scope: f.intent.Scope, kind: "VERIFIED_PAID", amount: paid.Amount, trade: paid.ChannelReference, evidenceHash: paid.EvidenceHash, observed: paid.ObservedAt,
		receipt: newReceiptState([]byte(`{"alipay_aipay_agent_payment_verify_response":{"code":"10000","active":true},"sign":"AAAA"}`))}
	defer forged.Close()
	encoded, e := v.SealReceipt(forged)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	raw, e := v.OpenReceipt(f.intent.Scope, encoded)
	if e != nil {
		t.Fatal("synthetic encrypted archive not readable")
	}
	defer clear(raw)
	o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, raw)
	defer o.Close()
	if e == nil || o.IsSettled() {
		t.Fatal("AES authentication treated as original Alipay RSA verification")
	}
}
func TestOriginalPaidArchiveCannotBeReverifiedAfterOwnerCloseOrFinancialHorizon(t *testing.T) {
	f, paid, raw := paidArchiveFixture(t)
	f.now = f.binding.ResolveUntil
	o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, raw)
	defer o.Close()
	if e == nil {
		t.Fatal("expired original financial binding admitted archive")
	}
	f.now = f.intent.CreatedAt
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if f.channel.Close(closeCtx) != nil {
		t.Fatal("synthetic native owner did not join")
	}
	o, e = f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, raw)
	defer o.Close()
	if e == nil {
		t.Fatal("closed original verification key reused")
	}
}
func TestOriginalPaidArchiveObservationStillRedactsSignedVendorEnvelope(t *testing.T) {
	f, paid, raw := paidArchiveFixture(t)
	o, e := f.channel.ReverifyPaidReceipt(context.Background(), f.intent, paid, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	output, e := json.Marshal(o)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(output), "sign") || strings.Contains(string(output), paid.ChannelReference) {
		t.Fatal("reverified original envelope leaked into public JSON")
	}
}
