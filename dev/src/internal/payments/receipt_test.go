package payments

// SOURCE_ONLY: no tests or synthetic key generation executed in CODE_FIRST.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func receiptVault(t *testing.T) *Vault {
	t.Helper()
	v, e := NewVault("receipt_key", map[string][]byte{"receipt_key": bytes.Repeat([]byte{17}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	return v
}
func TestMachinePayArchivePreservesCompleteOriginalSignedEnvelope(t *testing.T) {
	var exact []byte
	f := fixture(t, func(f *payFixture, _ *http.Request) (*http.Response, error) {
		response := f.signed(t, verifyMethod, []byte(`{ "code":"10000", "trade_no":"`+strings.Repeat("1", 32)+`", "out_trade_no":"merchant_original", "amount":"0.01", "active":true }`))
		raw, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		exact = append([]byte{}, raw...)
		response.Body = io.NopCloser(bytes.NewReader(raw))
		return response, nil
	})
	defer clear(exact)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	if bytes.Contains(encoded, []byte(`"sign":`)) || bytes.Contains(encoded, []byte(`"active":true`)) {
		t.Fatal("signed original response escaped ciphertext")
	}
	raw, e := v.OpenReceipt(f.intent.Scope, encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	if !bytes.Equal(raw, exact) {
		t.Fatal("signed envelope was dropped or reserialized")
	}
}
func TestMachinePayReceiptCannotCrossOriginalBuyerSellerOrPurpose(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	for _, field := range []string{"buyer", "seller", "payment", "purpose"} {
		scope := f.intent.Scope
		switch field {
		case "buyer":
			scope.Buyer = "buyer_other"
		case "seller":
			scope.Seller = "seller_other"
		case "payment":
			scope.Payment = "payment_other"
		case "purpose":
			scope.Purpose = "RENTAL_RENEWAL"
			scope.PurposeRef = "renewal_other"
		}
		if raw, e := v.OpenReceipt(scope, encoded); e == nil {
			clear(raw)
			t.Fatal("foreign original scope opened signed receipt", field)
		}
	}
}
func TestMachinePayReceiptTamperCannotBecomeFinancialEvidence(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	var record encryptedReceipt
	if json.Unmarshal(encoded, &record) != nil {
		t.Fatal("synthetic receipt")
	}
	record.Metadata.Amount++
	tampered, _ := json.Marshal(record)
	defer clear(tampered)
	if raw, e := v.OpenReceipt(f.intent.Scope, tampered); e == nil {
		clear(raw)
		t.Fatal("tampered amount authenticated")
	}
}
func TestMachinePayReceiptRequiresActualVerifiedEnvelopeNotConstructedSnapshot(t *testing.T) {
	f := fixture(t, nil)
	v := receiptVault(t)
	for _, o := range []Observation{{scope: f.intent.Scope, kind: "VERIFIED_PAID", amount: f.intent.Amount, trade: strings.Repeat("1", 32), evidenceHash: strings.Repeat("a", 64), observed: f.now}, {scope: f.intent.Scope, kind: "UNRESOLVED", amount: f.intent.Amount, observed: f.now}} {
		if raw, e := v.SealReceipt(o); e == nil {
			clear(raw)
			t.Fatal("caller-created metadata became archived signed proof")
		}
	}
}
func TestMachinePayClosingObservationRevokesAllCopiesButNotPaidFact(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Verify(context.Background(), f.intent, proofFor(t))
	if e != nil {
		t.Fatal(e)
	}
	copy := o
	o.Close()
	if !copy.IsSettled() {
		t.Fatal("closing owned envelope rewrote a financial fact")
	}
	if raw, e := receiptVault(t).SealReceipt(copy); e == nil {
		clear(raw)
		t.Fatal("closed original envelope remained readable through copy")
	}
}
func TestMachinePayArchivedReceiptRequiresExplicitRetainedOriginalKey(t *testing.T) {
	f := fixture(t, nil)
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	wrong, e := NewVault("receipt_key", map[string][]byte{"receipt_key": bytes.Repeat([]byte{19}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer wrong.Close()
	if raw, e := wrong.OpenReceipt(f.intent.Scope, encoded); e == nil {
		clear(raw)
		t.Fatal("wrong key opened old signed receipt")
	}
	rotated, e := NewVault("new_key", map[string][]byte{"new_key": bytes.Repeat([]byte{19}, 32), "receipt_key": bytes.Repeat([]byte{17}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer rotated.Close()
	raw, e := rotated.OpenReceipt(f.intent.Scope, encoded)
	if e != nil {
		t.Fatal("explicit old receipt key was ignored")
	}
	clear(raw)
}
func TestMachinePayObservedTimeMatchesDatabaseMicrosecondPrecision(t *testing.T) {
	f := fixture(t, nil)
	f.now = f.now.Add(999 * time.Nanosecond)
	o, e := f.channel.Precreate(context.Background(), f.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	if o.observed.Nanosecond()%1000 != 0 {
		t.Fatal("signed archive metadata would disagree with database timestamp precision")
	}
}
func TestMachinePayShortProofAndLongReceiptKeysCannotOverlap(t *testing.T) {
	proof := receiptVault(t)
	sameBytes, e := NewVault("different_id", map[string][]byte{"different_id": bytes.Repeat([]byte{17}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer sameBytes.Close()
	if separateMachineVaults(proof, proof) || separateMachineVaults(proof, sameBytes) {
		t.Fatal("key aliases bypassed distinct proof/receipt retention")
	}
	separate, e := NewVault("separate_id", map[string][]byte{"separate_id": bytes.Repeat([]byte{21}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer separate.Close()
	if !separateMachineVaults(proof, separate) {
		t.Fatal("independent receipt keyring rejected")
	}
	separate.Close()
	if separateMachineVaults(proof, separate) {
		t.Fatal("closed keyring accepted for future archive writes")
	}
}
