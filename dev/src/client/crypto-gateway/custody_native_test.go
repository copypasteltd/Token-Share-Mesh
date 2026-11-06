//go:build p11_sodium && cgo && (windows || linux)

package cryptogateway

// SOURCE_ONLY. Actual native cipher/key/preflight composition with a test-only
// in-memory metadata journal and root-confined files. This is not a production
// OS-permission qualification, a model execution or buyer acceptance.
import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/crypto-gateway/ciphercustody"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/protocol/cipherstream"
)

type transferFiles struct{ root *os.Root }

func (f transferFiles) Create(name string) (ciphercustody.File, error) {
	if !ciphercustody.FileNameValid(name) {
		return nil, ciphercustody.ErrScope
	}
	return f.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
}
func (f transferFiles) Open(name string) (ciphercustody.File, error) {
	if !ciphercustody.FileNameValid(name) {
		return nil, ciphercustody.ErrScope
	}
	return f.root.Open(name)
}
func (f transferFiles) Remove(name string) error {
	if !ciphercustody.FileNameValid(name) {
		return ciphercustody.ErrScope
	}
	return f.root.Remove(name)
}
func (f transferFiles) Names() ([]string, error) {
	directory, e := f.root.Open(".")
	if e != nil {
		return nil, e
	}
	defer directory.Close()
	entries, e := directory.ReadDir(ciphercustody.MaximumObjects + 1)
	if e != nil && e != io.EOF {
		return nil, e
	}
	names := []string{}
	for _, entry := range entries {
		if !ciphercustody.FileNameValid(entry.Name()) {
			return nil, ciphercustody.ErrState
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

func transferCustody(t *testing.T) *ciphercustody.Store {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing project source location")
	}
	project := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	if _, e := os.Stat(filepath.Join(project, "dev", "src", "go.mod")); e != nil {
		t.Fatal("project runtime location unavailable", e)
	}
	base := filepath.Join(project, "runtime", "work-in-progress", "IC-native-custody-tests")
	if e := os.MkdirAll(base, 0700); e != nil {
		t.Fatal(e)
	}
	directory, e := os.MkdirTemp(base, "case-")
	if e != nil {
		t.Fatal(e)
	}
	// A recursive cleanup is restricted to this exact freshly created case.
	if !filepath.IsAbs(directory) || filepath.Dir(directory) != base || !strings.HasPrefix(filepath.Base(directory), "case-") {
		t.Fatal("test cleanup path escaped project runtime")
	}
	root, e := os.OpenRoot(directory)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		root.Close()
		if e := os.RemoveAll(directory); e != nil {
			t.Error(e)
		}
	})
	policy := ciphercustody.Policy{Profile: ciphercustody.Profile, Bytes: "536870912", Objects: 64}
	store, e := ciphercustody.OpenOwned(&transferJournal{}, transferFiles{root}, bytes.Repeat([]byte{41}, 32), strings.Repeat("9", 64), &policy)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(store.Close)
	return store
}

func custodyHeaders() cipherstream.PrivateHeaders {
	return cipherstream.PrivateHeaders{Profile: "P11_PRIVATE_HEADERS_1", Method: "POST", Path: "/v1/chat/completions", ContentType: "application/json"}
}

type neverReadSource struct{ t *testing.T }

func (s neverReadSource) Read([]byte) (int, error) {
	s.t.Error("READY retry read plaintext again")
	return 0, io.ErrUnexpectedEOF
}

func TestNativeStoredCipherRetriesExactOriginalManifestWithoutReadingPlaintext(t *testing.T) {
	f := newTransfer(t)
	store := transferCustody(t)
	first, e := SealStored(context.Background(), f.buyer, store, f.send, custodyHeaders(), bytes.NewReader(f.body), "SUCCESS", f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	second, e := SealStored(context.Background(), f.buyer, store, f.send, custodyHeaders(), neverReadSource{t}, "SUCCESS", f.now.Add(time.Minute), f.now)
	if e != nil || !second.Replayed || first.Handle != second.Handle || first.Manifest != second.Manifest {
		t.Fatal("original encryption was recreated", e)
	}
	var a, b bytes.Buffer
	size, e := cipherstream.Counter(first.Bytes, false, 63)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = CopyStoredRange(context.Background(), f.buyer, store, f.send, "OUTBOUND", first.Handle, first.Manifest, 0, int64(size), &a, f.now); e != nil {
		t.Fatal(e)
	}
	if _, e = CopyStoredRange(context.Background(), f.buyer, store, f.send, "OUTBOUND", second.Handle, second.Manifest, 17, int64(size)-17, &b, f.now); e != nil || !bytes.Equal(a.Bytes()[17:], b.Bytes()) {
		t.Fatal("cipher range retransmission changed original bytes", e)
	}
}

func TestNativeImportedCipherRequiresCompleteInputAuthenticationBeforeConsumer(t *testing.T) {
	f := newTransfer(t)
	store := transferCustody(t)
	raw, m, e := f.seal(t, f.body)
	if e != nil {
		t.Fatal(e)
	}
	retained, e := ImportStored(context.Background(), f.seller, store, f.receive, bytes.NewReader(raw), m, f.now)
	if e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	final := false
	e = ConsumeStored(context.Background(), f.seller, store, f.receive, retained.Handle, m, f.now, func(_ context.Context, r cipherstream.Record) error {
		if r.Type == cipherstream.DataRecord {
			_, _ = output.Write(r.Payload)
		}
		if r.Type == cipherstream.EndRecord {
			final = true
		}
		return nil
	})
	if e != nil || !final || !bytes.Equal(output.Bytes(), f.body) {
		t.Fatal("stored original input did not cross the authenticated barrier", e)
	}
}

func TestNativeImportedCipherRejectsChangedManifestBeforeAnyInputCallback(t *testing.T) {
	f := newTransfer(t)
	store := transferCustody(t)
	raw, m, e := f.seal(t, f.body)
	if e != nil {
		t.Fatal(e)
	}
	retained, e := ImportStored(context.Background(), f.seller, store, f.receive, bytes.NewReader(raw), m, f.now)
	if e != nil {
		t.Fatal(e)
	}
	m.CipherHash = strings.Repeat("f", 64)
	calls := 0
	e = ConsumeStored(context.Background(), f.seller, store, f.receive, retained.Handle, m, f.now, func(context.Context, cipherstream.Record) error { calls++; return nil })
	if e == nil || calls != 0 {
		t.Fatal("changed final manifest exposed input to a consumer")
	}
}

type cancelableDestination struct {
	ctx              context.Context
	entered, release chan struct{}
	once             sync.Once
}

func (w *cancelableDestination) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.ctx.Done()
	<-w.release
	return 0, w.ctx.Err()
}

func TestNativeCipherRetransmitRevocationCancelsAndActualCloseStillJoins(t *testing.T) {
	f := newTransfer(t)
	store := transferCustody(t)
	retained, e := SealStored(context.Background(), f.buyer, store, f.send, custodyHeaders(), bytes.NewReader(f.body), "SUCCESS", f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	d, e := custodyDescriptor(f.buyer, f.send, "OUTBOUND", f.now.Add(time.Minute), "", f.now)
	if e != nil {
		t.Fatal(e)
	}
	// A nested public-key use owns the actual destination context. Revoking
	// the pinned peer cancels it, but cannot fabricate its eventual exit.
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	copyDone := make(chan error, 1)
	scope := f.send.Local
	scope.Purpose = "CIPHER_CUSTODY"
	scope.PeerBundleHash = f.send.Peer.BundleHash
	go func() {
		copyDone <- f.buyer.Use(context.Background(), scope, f.now, func(ctx context.Context, _ *endpoint.Material) error {
			_, e := store.CopyRange(ctx, d, retained.Handle, 0, 1, &cancelableDestination{ctx: ctx, entered: entered, release: release}, f.now)
			return e
		})
	}()
	<-entered
	if e = f.buyer.RevokePeer(f.send.Peer.BundleHash); e != nil {
		t.Fatal(e)
	}
	closed := make(chan struct{})
	go func() { f.buyer.BeginClose(); store.BeginClose(); store.Close(); f.buyer.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("nested owner was not joined")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	if e = <-copyDone; e == nil {
		t.Fatal("revoked transport completed")
	}
	<-closed
	if _, e = CopyStoredRange(context.Background(), f.buyer, store, f.send, "OUTBOUND", retained.Handle, retained.Manifest, 0, 1, io.Discard, f.now); e == nil || errors.Is(e, io.EOF) {
		t.Fatal("revoked custody reopened", e)
	}
}
