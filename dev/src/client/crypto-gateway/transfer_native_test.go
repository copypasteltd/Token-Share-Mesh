//go:build p11_sodium && cgo && (windows || linux)

package cryptogateway

// SOURCE_ONLY: actual endpoint/secretstream/preflight composition. These tests
// grant no model, device, payment, qualification or hardware privacy evidence.
import (
	"bytes"
	"context"
	"os"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/client/protocol/cipherstream"
	"p11.local/compute/shared/canonicaljson"
	"strings"
	"testing"
	"time"
)

type transferJournal struct{ raw []byte }

func (j *transferJournal) Read() ([]byte, error) {
	if j.raw == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(j.raw), nil
}
func (j *transferJournal) Commit(raw []byte) error { j.raw = bytes.Clone(raw); return nil }

type transferFixture struct {
	buyer, seller *endpoint.Vault
	send, receive Binding
	now           time.Time
	body          []byte
}

func newTransfer(t *testing.T) transferFixture {
	t.Helper()
	now := time.Now().UTC()
	newVault := func(binding string) *endpoint.Vault {
		v, e := endpoint.OpenOwned(&transferJournal{}, bytes.Repeat([]byte{39}, 32), binding, true)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(v.Close)
		return v
	}
	buyer, seller := newVault(strings.Repeat("1", 64)), newVault(strings.Repeat("2", 64))
	b, e := buyer.CreateLocal("buyer_account", "buyer_endpoint", "BUYER", now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	s, e := seller.CreateLocal("seller_account", "seller_endpoint", "SELLER", now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if buyer.PinInitial(s, endpoint.Verification{Source: "OUT_OF_BAND", Fingerprint: s.Identity.Fingerprint, Reference: "verified_seller"}, now) != nil || seller.PinInitial(b, endpoint.Verification{Source: "OUT_OF_BAND", Fingerprint: b.Identity.Fingerprint, Reference: "verified_buyer"}, now) != nil {
		t.Fatal("independent pin failed")
	}
	c := cipherstream.Context{Profile: cipherstream.Profile, Suite: sodium.Suite, Canonicalization: canonicaljson.Profile, Contract: strings.Repeat("a", 64), Order: "order_original", Job: "job_original", Attempt: "attempt_original", Fence: "1", Seller: s.Owner, Buyer: b.Owner, SellerEndpoint: s.Endpoint, BuyerEndpoint: b.Endpoint, BuyerResultKey: b.Content.ID, BuyerResultFingerprint: b.Content.Fingerprint, Sender: b.Endpoint, SenderKey: b.Identity.ID, Receiver: s.Endpoint, ReceiverKey: s.Content.ID, ReceiverFingerprint: s.Content.Fingerprint, Environment: strings.Repeat("b", 64), LocalPolicy: "local_policy_original", Mode: "E2E", Privacy: "PLATFORM_BLIND", APIProfile: "P11_CHAT_TEXT_NATIVE_1", Direction: "BUYER_TO_SELLER", Purpose: "INPUT", Object: "object_original", Stream: "stream_original", Epoch: "1", Deadline: now.Add(time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano)}
	send := Binding{Local: endpoint.Scope{Owner: b.Owner, Endpoint: b.Endpoint, Role: "BUYER", BundleHash: b.Hash(), Purpose: "SIGN_STREAM"}, Peer: endpoint.PeerScope{Owner: s.Owner, Endpoint: s.Endpoint, Role: "SELLER", BundleHash: s.Hash()}, Context: c, Limits: cipherstream.DefaultLimits("INPUT"), PublicModel: "public_model"}
	receive := Binding{Local: endpoint.Scope{Owner: s.Owner, Endpoint: s.Endpoint, Role: "SELLER", BundleHash: s.Hash(), Purpose: "OPEN_CONTENT"}, Peer: endpoint.PeerScope{Owner: b.Owner, Endpoint: b.Endpoint, Role: "BUYER", BundleHash: b.Hash()}, Context: c, Limits: cipherstream.DefaultLimits("INPUT"), PublicModel: "public_model"}
	return transferFixture{buyer, seller, send, receive, now, []byte(`{"model":"public_model","messages":[{"role":"user","content":"private request"}],"max_tokens":16}`)}
}
func (f transferFixture) seal(t *testing.T, body []byte) ([]byte, cipherstream.Manifest, error) {
	t.Helper()
	var out bytes.Buffer
	m, e := SealObject(context.Background(), f.buyer, f.send, cipherstream.PrivateHeaders{Profile: "P11_PRIVATE_HEADERS_1", Method: "POST", Path: "/v1/chat/completions", ContentType: "application/json"}, bytes.NewReader(body), &out, "SUCCESS", f.now.Add(time.Minute), f.now)
	return out.Bytes(), m, e
}
func TestCryptoTransferChecksOriginalKeysAndPrivateRequestBeforeConsumer(t *testing.T) {
	f := newTransfer(t)
	raw, m, e := f.seal(t, f.body)
	if e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	final := false
	e = ConsumeObject(context.Background(), f.seller, f.receive, bytes.NewReader(raw), m, f.now, func(_ context.Context, r cipherstream.Record) error {
		if r.Type == cipherstream.DataRecord {
			_, _ = body.Write(r.Payload)
		}
		if r.Type == cipherstream.EndRecord {
			final = true
		}
		return nil
	})
	if e != nil || !final || !bytes.Equal(body.Bytes(), f.body) {
		t.Fatal("original private transfer incomplete", e)
	}
}
func TestCryptoTransferInvalidFinalManifestExposesNoInputPrefix(t *testing.T) {
	f := newTransfer(t)
	raw, m, e := f.seal(t, f.body)
	if e != nil {
		t.Fatal(e)
	}
	m.CipherHash = strings.Repeat("f", 64)
	calls := 0
	e = ConsumeObject(context.Background(), f.seller, f.receive, bytes.NewReader(raw), m, f.now, func(context.Context, cipherstream.Record) error { calls++; return nil })
	if e == nil || calls != 0 {
		t.Fatal("unverified INPUT prefix reached execution consumer")
	}
}
func TestCryptoTransferPreflightAndPeerOwnershipRejectBeforeCipherOutput(t *testing.T) {
	f := newTransfer(t)
	if raw, _, e := f.seal(t, []byte(`{"model":"other_model","messages":[{"role":"user","content":"private"}]}`)); e == nil || len(raw) != 0 {
		t.Fatal("unconfirmed model produced input ciphertext")
	}
	f.send.Peer.Owner = "seller_foreign"
	if raw, _, e := f.seal(t, f.body); e == nil || len(raw) != 0 {
		t.Fatal("foreign seller received sealed input")
	}
}
