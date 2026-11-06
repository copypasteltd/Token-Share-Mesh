package resultwire

// SOURCE_ONLY: no protocol, crypto or native verification has been executed.
import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
)

func contentHead(t *testing.T) ContentHead {
	t.Helper()
	d, k := testDeclaration(t)
	s := d.Scope
	m := executionlease.Manifest{Profile: executionlease.ManifestProfile, ID: "manifest_original", Job: s.Job, Attempt: s.Attempt, Order: s.Order, Seller: s.Seller, Device: s.Device, Resource: s.Resource, Generation: s.Generation, Fence: s.Fence, ContractHash: s.ContractHash, EnvironmentDigest: s.Environment, Mode: "PLAINTEXT", Privacy: "STANDARD", InputTokenLimit: 64, Spec: executionlease.Spec{Protocol: relay.ChatText, Model: "original_model", MaxOutputTokens: 32}}
	d.Scope.ManifestHash = m.Digest()
	d.OutputHash = BytesDigest([]byte("text"))
	var e error
	d, e = SignDelivery(k, d)
	if e != nil {
		t.Fatal(e)
	}
	at, _ := time.Parse(time.RFC3339Nano, d.Completed)
	return ContentHead{Profile: ContentProfile, Delivery: "delivery_original", Declaration: d, Public: base64.RawURLEncoding.EncodeToString(k.Public().(ed25519.PublicKey)), Manifest: m, ChunkBytes: ChunkBytes, Chunks: 1, Expires: at.Add(30 * time.Minute).UTC().Format(time.RFC3339Nano), State: "UPLOADING"}
}
func TestContentHeadBindsOriginalManifestAndSignedDeclaration(t *testing.T) {
	h := contentHead(t)
	if e := h.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*ContentHead){func(h *ContentHead) { h.Manifest.Seller = "seller_other" }, func(h *ContentHead) { h.Declaration.Scope.Fence = "2" }, func(h *ContentHead) { h.Declaration.OutputHash = strings.Repeat("f", 64) }, func(h *ContentHead) { h.Public = base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }} {
		other := h
		change(&other)
		if other.Validate() == nil {
			t.Fatal("substituted original content validated")
		}
	}
}
func TestContentHeadNeverAcceptsPrivacyDowngradeOrExtendedRetention(t *testing.T) {
	h := contentHead(t)
	h.Manifest.Privacy = "LOCAL_HARDENED"
	if h.Validate() == nil {
		t.Fatal("plain transport claimed privacy")
	}
	h = contentHead(t)
	at, _ := time.Parse(time.RFC3339Nano, h.Expires)
	h.Expires = at.Add(time.Second).Format(time.RFC3339Nano)
	if h.Validate() == nil {
		t.Fatal("retention renewed")
	}
}
func TestContentHeadDigestIsStableAcrossReadyAndTerminal(t *testing.T) {
	h := contentHead(t)
	digest := h.Digest()
	h.State = "READY"
	h.End = &End{}
	if h.Validate() != nil || h.Digest() != digest {
		t.Fatal("ready changed immutable content identity")
	}
	h.IncludeUsage = true
	if h.Digest() == digest {
		t.Fatal("protocol option not frozen")
	}
}
func TestContentChunkRejectsWrongBytesIndexAndHead(t *testing.T) {
	h := contentHead(t)
	c := ContentChunk{Profile: ContentProfile, Scope: h.Declaration.Scope, Delivery: h.Delivery, Head: h.Digest(), Index: 0, Content: []byte("text"), Digest: BytesDigest([]byte("text"))}
	if c.Validate() != nil {
		t.Fatal("original chunk rejected")
	}
	for _, change := range []func(*ContentChunk){func(c *ContentChunk) { c.Content = []byte("fake") }, func(c *ContentChunk) { c.Index = 1024 }, func(c *ContentChunk) { c.Head = "invalid" }, func(c *ContentChunk) { c.Content = make([]byte, ChunkBytes+1) }} {
		x := c
		change(&x)
		if x.Validate() == nil {
			t.Fatal("malformed chunk accepted")
		}
	}
}
func TestContentTerminalRequiresActualBoundedMatchedStop(t *testing.T) {
	for _, s := range []string{"", strings.Repeat("x", 257), "x\x00y", string([]byte{0xff})} {
		if (End{Stop: s}).Validate("stop") == nil {
			t.Fatal("invalid stop accepted")
		}
	}
	if (End{Stop: "done"}).Validate("eog") == nil || (End{Stop: "done"}).Validate("stop") != nil {
		t.Fatal("terminal boundary lost")
	}
}
func TestRawContentDigestDoesNotHashJSONBase64(t *testing.T) {
	b := []byte("text")
	if BytesDigest(b) == Digest(b) {
		t.Fatal("raw digest aliases JSON digest")
	}
	if ChunkCount(0) != 0 || ChunkCount(32768) != 1 || ChunkCount(32769) != 2 {
		t.Fatal("fixed chunk boundary lost")
	}
}
