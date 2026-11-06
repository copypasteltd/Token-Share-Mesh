package node

// SOURCE_ONLY. Signed HTTP/SQLite/supervisor definitions use a synthetic
// inference backend. They are not real device, payment or privacy evidence.
import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/resultwire"
)

type contentAuthority struct {
	mu                 sync.Mutex
	declaration        resultwire.Delivery
	head               string
	ready              bool
	chunks             [][]byte
	loseSeal, wrongAck atomic.Bool
	requests           atomic.Int32
}

func (a *contentAuthority) serve(t *testing.T, f *hostFixture, w http.ResponseWriter, r *http.Request, body []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests.Add(1)
	if strings.HasSuffix(r.URL.Path, "/deliveries") {
		var d resultwire.Delivery
		key := f.node.key()
		defer clear(key)
		if json.Unmarshal(body, &d) != nil || d.Verify(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)) != nil || d.Scope != resultwire.Original(f.lease) {
			t.Error("not the original signed report")
			w.WriteHeader(409)
			return
		}
		a.declaration = d
		at, _ := time.Parse(time.RFC3339Nano, d.Completed)
		writeReply(w, 200, deliveryAck{ID: "delivery_original", Attempt: d.Scope.Attempt, Revision: d.Revision, ManifestHash: resultwire.Digest(d), OutputHash: d.OutputHash, OutputBytes: d.OutputBytes, State: "WAITING_BUYER", Deadline: at.Add(time.Minute), Usage: "UNVERIFIED"})
		return
	}
	state := "UPLOADING"
	if a.ready {
		state = "READY"
	}
	ack := resultwire.ContentAck{Delivery: "delivery_original", Head: a.head, State: state}
	switch {
	case strings.HasSuffix(r.URL.Path, "/begin"):
		var in resultwire.BeginContent
		if json.Unmarshal(body, &in) != nil || in.Scope != a.declaration.Scope {
			w.WriteHeader(409)
			return
		}
		at, _ := time.Parse(time.RFC3339Nano, a.declaration.Completed)
		h := resultwire.ContentHead{Profile: resultwire.ContentProfile, Delivery: ack.Delivery, Declaration: a.declaration, Public: nodePublic(f.node), Manifest: f.manifest, IncludeUsage: in.IncludeUsage, ChunkBytes: resultwire.ChunkBytes, Chunks: resultwire.ChunkCount(a.declaration.OutputBytes), Expires: at.Add(30 * time.Minute).Format(time.RFC3339Nano), State: "UPLOADING"}
		a.head = h.Digest()
		ack.Head = a.head
	case strings.HasSuffix(r.URL.Path, "/chunks"):
		var in resultwire.ContentChunk
		if json.Unmarshal(body, &in) != nil || in.Validate() != nil || in.Scope != a.declaration.Scope || in.Head != a.head {
			w.WriteHeader(409)
			return
		}
		a.chunks = append(a.chunks, append([]byte(nil), in.Content...))
		ack.Index = &in.Index
		ack.Digest = in.Digest
	case strings.HasSuffix(r.URL.Path, "/seal"):
		var in resultwire.SealContent
		if json.Unmarshal(body, &in) != nil || in.Head != a.head || in.Scope != a.declaration.Scope {
			w.WriteHeader(409)
			return
		}
		a.ready = true
		ack.State = "READY"
		if a.loseSeal.Swap(false) {
			drop(w)
			return
		}
	case strings.HasSuffix(r.URL.Path, "/status"):
	default:
		w.WriteHeader(404)
		return
	}
	if a.wrongAck.Load() {
		ack.Head = strings.Repeat("0", 64)
	}
	writeReply(w, 200, ack)
}
func nodePublic(s *Store) string {
	key := s.key()
	defer clear(key)
	return base64.RawURLEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey))
}
func contentExecution(t *testing.T) (*hostFixture, *PreparedExecution, *custodyBackend, *contentAuthority) {
	t.Helper()
	f := newHostFixture(t)
	a := &contentAuthority{}
	f.resultHandler = func(w http.ResponseWriter, r *http.Request, b []byte) { a.serve(t, f, w, r, b) }
	ctx := context.Background()
	f.begin(t, "command_content")
	p, e := f.host.Prepare(ctx, "command_content")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	spool, e := results.Open(results.DefaultConfig(t.TempDir()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = spool.Close(context.Background()) })
	backend := &custodyBackend{}
	adapter, e := results.New(backend, spool)
	if e != nil {
		t.Fatal(e)
	}
	approved := f.config.Rules[0].Approval
	watch, e := f.host.NewSupervisor(ctx, []HostKernel{{Environment: approved.EnvironmentDigest, Capability: approved.Capability, EngineKind: approved.EngineKind, EngineRelease: approved.EngineRelease, ModelDigest: approved.ModelDigest, RecipeDigest: approved.RecipeDigest, Backend: adapter}}, supervisor.Options{})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := runHostWatch(t, watch)
	if _, e = p.Start(ctx, watch); e != nil {
		stopHostWatch(t, cancel, done, nil)
		t.Fatal(e)
	}
	stopHostWatch(t, cancel, done, nil)
	if e = f.host.ReconcileCleanup(ctx, "command_content"); e != nil {
		t.Fatal(e)
	}
	return f, p, backend, a
}
func TestHostPublishesOriginalContentAfterComputeCleanupWithoutResubmit(t *testing.T) {
	_, p, b, a := contentExecution(t)
	ack, e := p.PublishResult(context.Background())
	if e != nil || ack.State != "READY" {
		t.Fatal(ack, e)
	}
	if b.inputCalls.Load() != 1 || len(a.chunks) != 1 || string(a.chunks[0]) != "original text custody" {
		t.Fatal("publication reran or changed output")
	}
	requests := a.requests.Load()
	if _, e = p.PublishResult(context.Background()); e != nil || a.requests.Load() != requests {
		t.Fatal("ready checkpoint unnecessarily retransmitted", e)
	}
}
func TestHostUnknownSealRecoversByOriginalStatusAcrossJournalReopen(t *testing.T) {
	f, p, b, a := contentExecution(t)
	a.loseSeal.Store(true)
	if _, e := p.PublishResult(context.Background()); e == nil {
		t.Fatal("lost seal response reported confirmed")
	}
	p.Close()
	f.reopen(t)
	ack, e := f.host.RecoverResultPublication(context.Background(), "command_content")
	if e != nil || ack.State != "READY" || b.inputCalls.Load() != 1 {
		t.Fatal("status recovery lost original or resubmitted", e)
	}
}
func TestHostChangedContentAckNeverMarksReady(t *testing.T) {
	f, p, b, a := contentExecution(t)
	a.wrongAck.Store(true)
	if _, e := p.PublishResult(context.Background()); e == nil {
		t.Fatal("foreign head acknowledged")
	}
	c, e := f.host.command(context.Background(), "command_content")
	if e != nil || c.ResultContent == nil || c.ResultContent.Ready || b.inputCalls.Load() != 1 {
		t.Fatal("foreign ack became ready", e)
	}
}
