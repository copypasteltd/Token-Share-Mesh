//go:build p11_sodium && cgo && (windows || linux)

package cipherstream

// SOURCE_ONLY: real pinned libsodium ABI tests, authored but not compiled/run.
import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"
	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/shared/canonicaljson"
	"strings"
	"testing"
	"time"
)

type nativeFixture struct {
	expected Expected
	signer   ed25519.PrivateKey
	secret   []byte
	now      time.Time
}

func streamFixture(t *testing.T) nativeFixture {
	t.Helper()
	now := time.Now().UTC()
	public, secret, e := sodium.Keypair()
	if e != nil {
		t.Fatal(e)
	}
	signer := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{41}, 32))
	c := Context{Profile: Profile, Suite: sodium.Suite, Canonicalization: canonicaljson.Profile, Contract: strings.Repeat("1", 64), Order: "order_original", Job: "job_original", Attempt: "attempt_original", Fence: "1", Seller: "seller_original", Buyer: "buyer_original", SellerEndpoint: "seller_endpoint", BuyerEndpoint: "buyer_endpoint", BuyerResultKey: "buyer_content_key", BuyerResultFingerprint: Hash(public), Sender: "seller_endpoint", SenderKey: "seller_identity_key", Receiver: "buyer_endpoint", ReceiverKey: "buyer_content_key", ReceiverFingerprint: Hash(public), Environment: strings.Repeat("2", 64), LocalPolicy: "local_policy_original", Mode: "E2E", Privacy: "PLATFORM_BLIND", APIProfile: "P11_CHAT_TEXT_NATIVE_1", Direction: "SELLER_TO_BUYER", Purpose: "OUTPUT", Object: "object_original", Stream: "stream_original", Epoch: "1", Deadline: now.Add(6 * time.Minute).Format(time.RFC3339Nano), HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano), Delivery: "delivery_original", DeliveryRevision: "1"}
	return nativeFixture{Expected{Context: c, SenderPublic: signer.Public().(ed25519.PublicKey), RecipientPublic: public, Limits: DefaultLimits("OUTPUT")}, signer, secret, now}
}
func (f nativeFixture) encode(t *testing.T, data []byte) ([]byte, Manifest) {
	t.Helper()
	var out bytes.Buffer
	w, e := NewWriter(&out, f.expected.Context, f.expected.RecipientPublic, f.signer, f.expected.Limits, f.now)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if e = w.Headers(PrivateHeaders{Profile: "P11_PRIVATE_HEADERS_1", Status: 200, ContentType: "application/json"}); e != nil {
		t.Fatal(e)
	}
	if len(data) > 0 {
		if e = w.Data(data); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.End("SUCCESS"); e != nil {
		t.Fatal(e)
	}
	m, e := w.Manifest(f.now.Add(5*time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	return out.Bytes(), m
}
func consume(t *testing.T, f nativeFixture, raw []byte, m Manifest) ([]byte, error) {
	t.Helper()
	r, e := NewReader(bytes.NewReader(raw), f.expected, f.secret, f.now)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out bytes.Buffer
	for {
		record, e := r.Next()
		if e != nil {
			return nil, e
		}
		last := record.Type == EndRecord
		if record.Type == DataRecord {
			_, _ = out.Write(record.Payload)
		}
		record.Clear()
		if last {
			break
		}
	}
	if e = r.Finish(m, f.now); e != nil {
		return nil, e
	}
	if !r.Complete() {
		t.Fatal("valid EOF/manifest not marked cryptographically complete")
	}
	return out.Bytes(), nil
}
func TestCipherNativeRoundTripAndBlindManifestContainNoPlaintextHash(t *testing.T) {
	f := streamFixture(t)
	data := bytes.Repeat([]byte("private prompt and content\n"), 4096)
	raw, m := f.encode(t, data)
	if bytes.Contains(raw, []byte("private prompt")) || strings.Contains(stringMustManifest(t, m), Hash(data)) {
		t.Fatal("private content/digest escaped")
	}
	out, e := consume(t, f, raw, m)
	if e != nil || !bytes.Equal(out, data) {
		t.Fatal("authenticated round trip failed", e)
	}
	summary, e := InspectOpaque(bytes.NewReader(raw), f.expected, m, f.now)
	if e != nil || summary.CipherBytes != int64(len(raw)) || summary.ManifestHash != m.Hash() {
		t.Fatal("blind original object mismatch", e)
	}
}
func stringMustManifest(t *testing.T, m Manifest) string {
	t.Helper()
	b, e := m.Bytes()
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestCipherNativeRejectsOtherIdentityRecipientAndOriginalAttempt(t *testing.T) {
	f := streamFixture(t)
	raw, m := f.encode(t, []byte("original"))
	for _, change := range []func(*nativeFixture){
		func(x *nativeFixture) { x.expected.Context.Attempt = "attempt_other" },
		func(x *nativeFixture) { x.expected.Context.Fence = "2" },
		func(x *nativeFixture) { x.expected.Context.Privacy = "ATTESTED_CONFIDENTIAL" },
		func(x *nativeFixture) {
			p := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, 32))
			x.expected.SenderPublic = p.Public().(ed25519.PublicKey)
		},
		func(x *nativeFixture) { x.secret = bytes.Repeat([]byte{9}, 32) },
	} {
		other := f
		change(&other)
		if _, e := consume(t, other, raw, m); e == nil {
			t.Fatal("foreign original/identity accepted")
		}
	}
}
func TestCipherNativeTruncationAppendAndTamperingNeverComplete(t *testing.T) {
	f := streamFixture(t)
	raw, m := f.encode(t, bytes.Repeat([]byte{23}, 70000))
	for _, n := range []int{0, 1, 3, 4, 20, len(raw) / 2, len(raw) - 1} {
		if _, e := consume(t, f, raw[:n], m); e == nil {
			t.Fatal("truncated ciphertext accepted")
		}
	}
	extra := append(bytes.Clone(raw), 0)
	if _, e := consume(t, f, extra, m); e == nil {
		t.Fatal("bytes after FINAL accepted")
	}
	bad := bytes.Clone(raw)
	bad[len(bad)-4] ^= 1
	if _, e := consume(t, f, bad, m); e == nil {
		t.Fatal("tampered FINAL accepted")
	}
}
func TestCipherNativeChecksSequenceAndDeclaredLengthBeforeAllocation(t *testing.T) {
	f := streamFixture(t)
	raw, m := f.encode(t, []byte("original"))
	headerEnd := 4 + int(binary.BigEndian.Uint32(raw[:4]))
	bad := bytes.Clone(raw)
	binary.BigEndian.PutUint32(bad[headerEnd:headerEnd+4], ^uint32(0))
	if _, e := consume(t, f, bad, m); e == nil {
		t.Fatal("oversized record accepted")
	}
	bad = bytes.Clone(raw)
	binary.BigEndian.PutUint64(bad[headerEnd+4:headerEnd+12], 1)
	if _, e := consume(t, f, bad, m); e == nil {
		t.Fatal("sequence skipped")
	}
	bad = bytes.Clone(raw)
	binary.BigEndian.PutUint32(bad[:4], ^uint32(0))
	if _, e := consume(t, f, bad, m); e == nil {
		t.Fatal("oversized header accepted")
	}
}
func TestCipherNativeBlindScanDoesNotProveEncryptedEND(t *testing.T) {
	f := streamFixture(t)
	raw, m := f.encode(t, []byte("prefix"))
	offset := 4 + int(binary.BigEndian.Uint32(raw[:4]))
	last := offset
	for offset < len(raw) {
		last = offset
		offset += 4 + int(binary.BigEndian.Uint32(raw[offset:offset+4]))
	}
	prefix := raw[:last]
	m.CipherBytes = decimalBytes(int64(len(prefix)))
	m.CipherHash = Hash(prefix)
	msg, _ := signedBytes("MANIFEST_SIGNATURE", m.unsigned(), 8192)
	m.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.signer, msg))
	if _, e := InspectOpaque(bytes.NewReader(prefix), f.expected, m, f.now); e != nil {
		t.Fatal("valid signed opaque prefix should remain only structural evidence", e)
	}
	if _, e := consume(t, f, prefix, m); e == nil {
		t.Fatal("structural scan manufactured private END completeness")
	}
}

type failedSink struct{ count int }

func (s *failedSink) Write(raw []byte) (int, error) {
	s.count++
	if s.count > 2 {
		return 0, io.ErrClosedPipe
	}
	return len(raw), nil
}
func TestCipherNativeFailedSinkCannotResumePushOrCreateManifest(t *testing.T) {
	f := streamFixture(t)
	w, e := NewWriter(&failedSink{}, f.expected.Context, f.expected.RecipientPublic, f.signer, f.expected.Limits, f.now)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if w.Headers(PrivateHeaders{Profile: "P11_PRIVATE_HEADERS_1", Status: 200, ContentType: "application/json"}) == nil {
		t.Fatal("failed sink accepted")
	}
	if w.Data([]byte("retry")) == nil || w.End("SUCCESS") == nil {
		t.Fatal("poisoned stream resumed")
	}
	if _, e = w.Manifest(f.now.Add(time.Minute), f.now); e == nil {
		t.Fatal("failed stream signed complete")
	}
}
func TestCipherNativeHeaderSignatureCoversSealedMaterialAndMode(t *testing.T) {
	f := streamFixture(t)
	raw, _ := f.encode(t, []byte("data"))
	n := int(binary.BigEndian.Uint32(raw[:4]))
	var h Header
	if canonicaljson.DecodeExact(raw[4:4+n], &h, MaximumHeaderBytes) != nil {
		t.Fatal("fixture header invalid")
	}
	h.SealedHash = hex.EncodeToString(make([]byte, 32))
	changed, _ := canonicaljson.Encode(h, MaximumHeaderBytes)
	if _, e := ParseHeader(changed, f.expected, f.now); e == nil {
		t.Fatal("changed sealed binding accepted")
	}
	h.Context.Mode = "PLAINTEXT"
	changed, _ = canonicaljson.Encode(h, MaximumHeaderBytes)
	if _, e := ParseHeader(changed, f.expected, f.now); e == nil {
		t.Fatal("silent downgrade accepted")
	}
}
