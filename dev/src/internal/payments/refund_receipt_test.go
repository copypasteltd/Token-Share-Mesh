package payments

// SOURCE_ONLY: encrypted archive and synthetic RSA envelope definitions. These
// are not executed and do not establish fresh RSA reverification or real funds.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOriginalRefundArchiveRetainsCompleteSignedEnvelopeOnlyAsCiphertext(t *testing.T) {
	var exact []byte
	r := newRefundFixture(t, func(r *refundFixture, _ *http.Request) (*http.Response, error) {
		response := r.payment.signed(t, refundQueryMethod, []byte(`{ "code":"10000", "trade_no":"`+r.intent.OriginalTrade+`", "out_trade_no":"merchant_original", "out_request_no":"RF_original", "refund_amount":"5.00", "total_amount":"50.00", "refund_status":"REFUND_SUCCESS", "buyer_open_id":"private_vendor_buyer_value", "refund_detail_item_list":[{"fund_channel":"private_channel_detail"}] }`))
		raw, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		exact = append([]byte{}, raw...)
		response.Body = io.NopCloser(bytes.NewReader(raw))
		return response, nil
	})
	defer clear(exact)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	for _, private := range []string{`"sign":`, `"refund_status":`, `private_vendor_buyer_value`, `private_channel_detail`} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatal("complete vendor response escaped encrypted archive")
		}
	}
	raw, e := v.OpenRefundReceipt(r.intent, encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	if !bytes.Equal(raw, exact) {
		t.Fatal("original signature, spaces or optional response fields lost")
	}
}

func TestOriginalRefundArchiveAuthenticatesAllOriginalScopeAndFinancialMetadata(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	for _, variant := range []string{"payment", "order", "buyer", "seller", "refund", "request", "route", "decision", "decision_hash", "intent", "kind", "method", "amount", "cumulative", "time", "nonce", "ciphertext", "key"} {
		t.Run(variant, func(t *testing.T) {
			var record encryptedRefundReceipt
			if json.Unmarshal(encoded, &record) != nil {
				t.Fatal("synthetic archive")
			}
			defer clear(record.Ciphertext)
			switch variant {
			case "payment":
				record.Metadata.Scope.Original.Payment = "other_payment"
			case "order":
				record.Metadata.Scope.Original.Order = "other_order"
			case "buyer":
				record.Metadata.Scope.Original.Buyer = "other_buyer"
			case "seller":
				record.Metadata.Scope.Original.Seller = "other_seller"
			case "refund":
				record.Metadata.Scope.Refund = "other_refund"
			case "request":
				record.Metadata.Scope.OriginalRequestKey = "RF_other"
			case "route":
				record.Metadata.Scope.RouteHash = strings.Repeat("0", 64)
			case "decision":
				record.Metadata.Scope.DecisionRef = "other_policy"
			case "decision_hash":
				record.Metadata.Scope.DecisionHash = strings.Repeat("0", 64)
			case "intent":
				record.Metadata.IntentHash = strings.Repeat("0", 64)
			case "kind":
				record.Metadata.Kind = "REFUND_QUERY_UNRESOLVED"
			case "method":
				record.Metadata.Method = refundMethod
			case "amount":
				record.Metadata.Amount++
			case "cumulative":
				record.Metadata.Cumulative = 1
			case "time":
				record.Metadata.Observed = record.Metadata.Observed.Add(time.Second)
			case "nonce":
				record.Nonce[0] ^= 1
			case "ciphertext":
				record.Ciphertext[0] ^= 1
			case "key":
				record.KeyID = "missing_key"
			}
			tampered, e := json.Marshal(record)
			if e != nil {
				t.Fatal(e)
			}
			defer clear(tampered)
			if raw, e := v.OpenRefundReceipt(r.intent, tampered); e == nil {
				clear(raw)
				t.Fatal("tampered original refund archive authenticated")
			}
		})
	}
}

func TestOriginalRefundArchiveCannotBeOpenedUnderAnotherIntent(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	for _, variant := range []string{"collected", "authority", "trade", "paid_evidence", "deadline"} {
		t.Run(variant, func(t *testing.T) {
			i := r.intent
			switch variant {
			case "collected":
				i.Collected--
			case "authority":
				i.Authority.Maximum--
			case "trade":
				i.OriginalTrade = strings.Repeat("2", 32)
			case "paid_evidence":
				i.PaidEvidenceHash = strings.Repeat("0", 64)
			case "deadline":
				i.ResolveUntil = i.ResolveUntil.Add(time.Second)
			}
			if raw, e := v.OpenRefundReceipt(i, encoded); e == nil {
				clear(raw)
				t.Fatal("original refund receipt crossed frozen intent")
			}
		})
	}
}

func TestOriginalRefundArchiveCannotSealUnverifiedOrClosedObservation(t *testing.T) {
	r := newRefundFixture(t, nil)
	v := receiptVault(t)
	fake := RefundObservation{scope: r.intent.Scope, intentHash: r.intent.Digest(), amount: r.intent.Amount, kind: "REFUND_CONFIRMED", method: refundQueryMethod, evidenceHash: strings.Repeat("f", 64), observed: r.payment.now}
	if raw, e := v.SealRefundReceipt(fake); e == nil {
		clear(raw)
		t.Fatal("metadata without actual native signed envelope became an archive")
	}
	unknown := r.channel.unknown(r.intent, "TRANSPORT_UNKNOWN")
	defer unknown.Close()
	if raw, e := v.SealRefundReceipt(unknown); e == nil {
		clear(raw)
		t.Fatal("unresolved channel result became signed financial evidence")
	}
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	copy := o
	o.Close()
	if !copy.IsSucceeded() {
		t.Fatal("closing owned envelope reversed verified original fact")
	}
	if raw, e := v.SealRefundReceipt(copy); e == nil {
		clear(raw)
		t.Fatal("copied observation retained closed plaintext envelope")
	}
}

func TestOriginalRefundArchiveKeyRotationRequiresExplicitOriginalKey(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	encoded, e := receiptVault(t).SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	wrong, e := NewVault("receipt_key", map[string][]byte{"receipt_key": bytes.Repeat([]byte{19}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer wrong.Close()
	if raw, e := wrong.OpenRefundReceipt(r.intent, encoded); e == nil {
		clear(raw)
		t.Fatal("same key label substituted different cryptographic key")
	}
	rotated, e := NewVault("new_key", map[string][]byte{"new_key": bytes.Repeat([]byte{19}, 32), "receipt_key": bytes.Repeat([]byte{17}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer rotated.Close()
	raw, e := rotated.OpenRefundReceipt(r.intent, encoded)
	if e != nil {
		t.Fatal("explicitly retained original receipt key not usable")
	}
	clear(raw)
	rotated.Close()
	if raw, e := rotated.OpenRefundReceipt(r.intent, encoded); e == nil {
		clear(raw)
		t.Fatal("closed archive vault reused key")
	}
}

func TestOriginalRefundArchiveCannotBecomePaymentProofOrPaidReceipt(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Query(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	if raw, e := v.OpenReceipt(r.intent.Scope.Original, encoded); e == nil {
		clear(raw)
		t.Fatal("refund archive interpreted as paid signed receipt")
	}
	if proof, e := v.Open(r.intent.Scope.Original, encoded, r.payment.now); e == nil {
		proof.Close()
		t.Fatal("refund archive interpreted as spending credential")
	}
	raw, e := v.OpenRefundReceipt(r.intent, encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	var observation RefundObservation
	if json.Unmarshal(raw, &observation) != nil {
		t.Fatal("synthetic envelope JSON")
	}
	if observation.IsSucceeded() {
		t.Fatal("authenticated archive decryption manufactured fresh verified observation")
	}
}

func TestOriginalRefundPendingArchiveCannotBecomeSuccessByRetagging(t *testing.T) {
	r := newRefundFixture(t, nil)
	o, e := r.channel.Submit(context.Background(), r.intent)
	if e != nil {
		t.Fatal(e)
	}
	defer o.Close()
	v := receiptVault(t)
	encoded, e := v.SealRefundReceipt(o)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	var record encryptedRefundReceipt
	if json.Unmarshal(encoded, &record) != nil {
		t.Fatal("synthetic archive")
	}
	defer clear(record.Ciphertext)
	record.Metadata.Kind = "REFUND_CONFIRMED"
	record.Metadata.Method = refundQueryMethod
	record.Metadata.Cumulative = 0
	tampered, e := json.Marshal(record)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(tampered)
	if raw, e := v.OpenRefundReceipt(r.intent, tampered); e == nil {
		clear(raw)
		t.Fatal("pending submit archive retagged into query-confirmed refund")
	}
}

func TestOriginalRefundOwnerDoesNotJoinBeforeActualBodyReadReturns(t *testing.T) {
	body := &ignoredCloseBody{entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(body.release) })
	defer release()
	r := newRefundFixture(t, func(*refundFixture, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
	})
	returned := make(chan struct{})
	go func() { defer close(returned); o, _ := r.channel.Query(context.Background(), r.intent); o.Close() }()
	<-body.entered
	ctx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if r.channel.Close(ctx) == nil {
		t.Fatal("unreturned refund response falsely released original credential owner")
	}
	select {
	case <-r.channel.channel.done:
		t.Fatal("owned native channel released before read completion")
	default:
	}
	release()
	<-returned
	joined, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if r.channel.Close(joined) != nil {
		t.Fatal("actual original refund body completion did not join")
	}
}
