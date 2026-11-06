package buyerresults

// SOURCE_ONLY: controlled HTTP/crypto/cache definitions; no GPU/model is used.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/results"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/shared/resultwire"
)

type receiveFixture struct {
	head     resultwire.ContentHead
	packet   resultwire.ReadPacket
	text     []byte
	calls    atomic.Int32
	server   *httptest.Server
	change   func(*resultwire.ContentChunk)
	redirect bool
}

func newReceiveFixture(t *testing.T) *receiveFixture {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC()
	f := &receiveFixture{text: bytes.Repeat([]byte("原始输出"), 9000)}
	m := executionlease.Manifest{Profile: executionlease.ManifestProfile, ID: "manifest_original", Job: "job_original", Attempt: "attempt_original", Order: "order_original", Seller: "seller_original", Device: "device_original", Resource: "resource_original", Generation: "8", Fence: "3", ContractHash: strings.Repeat("a", 64), EnvironmentDigest: strings.Repeat("b", 64), Mode: "PLAINTEXT", Privacy: "STANDARD", InputTokenLimit: 64, Spec: executionlease.Spec{Protocol: relay.ChatText, Model: "original_model", MaxOutputTokens: 32}}
	d, e := resultwire.SignDelivery(key, resultwire.Delivery{Profile: resultwire.DeliveryProfile, Scope: resultwire.Scope{Job: m.Job, Attempt: m.Attempt, Lease: "lease_original", Order: m.Order, Seller: m.Seller, Device: m.Device, Resource: m.Resource, Generation: m.Generation, Fence: m.Fence, ContractHash: m.ContractHash, ManifestHash: m.Digest(), Environment: m.EnvironmentDigest}, Result: "result_original", Revision: 1, OutputHash: resultwire.BytesDigest(f.text), OutputBytes: int64(len(f.text)), InputTokens: 3, OutputTokens: 2, Finish: "eog", Completed: at.Format(time.RFC3339Nano)})
	if e != nil {
		t.Fatal(e)
	}
	f.head = resultwire.ContentHead{Profile: resultwire.ContentProfile, Delivery: "delivery_original", Declaration: d, Public: base64.RawURLEncoding.EncodeToString(pub), Manifest: m, ChunkBytes: resultwire.ChunkBytes, Chunks: resultwire.ChunkCount(d.OutputBytes), Expires: at.Add(30 * time.Minute).Format(time.RFC3339Nano), State: "READY", End: &resultwire.End{}}
	g := resultwire.ReadGrant{Profile: resultwire.GrantProfile, ID: "grant_original", Order: m.Order, Job: m.Job, Seller: m.Seller, Delivery: f.head.Delivery, Head: f.head.Digest(), Expires: at.Add(5 * time.Minute).Format(time.RFC3339Nano)}
	f.packet = resultwire.ReadPacket{Grant: g, Secret: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" || r.Header.Get("Authorization") != "Bearer p11r."+g.ID+"."+f.packet.Secret {
			t.Error("receiver sent non-read authority")
			w.WriteHeader(401)
			return
		}
		if f.redirect {
			http.Redirect(w, r, "http://127.0.0.1:1/foreign", 302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/manifest") {
			_ = json.NewEncoder(w).Encode(f.head)
			return
		}
		index, e := strconv.Atoi(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
		if e != nil || index < 0 || index >= f.head.Chunks {
			w.WriteHeader(404)
			return
		}
		begin := index * resultwire.ChunkBytes
		end := begin + resultwire.ChunkBytes
		if end > len(f.text) {
			end = len(f.text)
		}
		c := resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: d.Scope, Delivery: f.head.Delivery, Head: f.packet.Grant.Head, Index: index, Digest: resultwire.BytesDigest(f.text[begin:end]), Content: bytes.Clone(f.text[begin:end])}
		if f.change != nil {
			f.change(&c)
		}
		_ = json.NewEncoder(w).Encode(c)
	}))
	f.packet.Origin = f.server.URL
	t.Cleanup(f.server.Close)
	return f
}
func receiveStore(t *testing.T) *results.Store {
	t.Helper()
	s, e := results.Open(results.DefaultConfig(t.TempDir()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}
func TestReceiveCompleteOriginalCreatesReviewAfterEncryptedImport(t *testing.T) {
	f := newReceiveFixture(t)
	v, e := Receive(context.Background(), f.packet, receiveStore(t))
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	out, e := v.Open(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer out.Close(context.Background())
	var got []byte
	for {
		b, e := out.Read(context.Background())
		got = append(got, b...)
		clear(b)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	if !bytes.Equal(got, f.text) || v.Review().Output != f.head.Declaration.OutputHash || v.Review().Manifest != resultwire.Digest(f.head.Declaration) {
		t.Fatal("wrong original output or receipt")
	}
}
func TestReceiveChangedBytesCannotIssueReviewEvenWithMatchingChunkDigest(t *testing.T) {
	f := newReceiveFixture(t)
	f.change = func(c *resultwire.ContentChunk) { c.Content[0] = 'X'; c.Digest = resultwire.BytesDigest(c.Content) }
	if v, e := Receive(context.Background(), f.packet, receiveStore(t)); e == nil || v != nil {
		t.Fatal("changed aggregate exposed")
	}
}
func TestReceiveCrossAttemptChunkCannotIssueReview(t *testing.T) {
	f := newReceiveFixture(t)
	f.change = func(c *resultwire.ContentChunk) { c.Scope.Attempt = "attempt_foreign" }
	if v, e := Receive(context.Background(), f.packet, receiveStore(t)); e == nil || v != nil {
		t.Fatal("foreign attempt exposed")
	}
}
func TestReceiveIncompleteChunkCannotIssueReview(t *testing.T) {
	f := newReceiveFixture(t)
	f.change = func(c *resultwire.ContentChunk) {
		c.Content = c.Content[:len(c.Content)-1]
		c.Digest = resultwire.BytesDigest(c.Content)
	}
	if v, e := Receive(context.Background(), f.packet, receiveStore(t)); e == nil || v != nil {
		t.Fatal("truncated result exposed")
	}
}
func TestReceiveSignedHeadSubstitutionFailsBeforeChunks(t *testing.T) {
	f := newReceiveFixture(t)
	f.head.Declaration.Scope.Seller = "seller_foreign"
	if v, e := Receive(context.Background(), f.packet, receiveStore(t)); e == nil || v != nil || f.calls.Load() != 1 {
		t.Fatal("substituted signed head accepted")
	}
}
func TestReceiveDoesNotFollowRedirectWithReadCredential(t *testing.T) {
	f := newReceiveFixture(t)
	f.redirect = true
	if v, e := Receive(context.Background(), f.packet, receiveStore(t)); e == nil || v != nil || f.calls.Load() != 1 {
		t.Fatal("read credential followed redirect")
	}
}
func TestReceiveExpiredOrNonNativeOriginNeverSendsRequest(t *testing.T) {
	f := newReceiveFixture(t)
	s := receiveStore(t)
	p := f.packet
	p.Grant.Expires = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if _, e := Receive(context.Background(), p, s); e == nil {
		t.Fatal("expired read accepted")
	}
	p = f.packet
	p.Origin = "http://example.com"
	if _, e := Receive(context.Background(), p, s); e == nil || f.calls.Load() != 0 {
		t.Fatal("insecure remote read attempted")
	}
}
