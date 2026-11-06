package mediaworkspace_test

// SOURCE_ONLY synthetic directories/metadata, no native engine or GPU proof.
import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/mediaworkspace"
)

type ownedFixture struct {
	owner  *mediaworkspace.Owner
	ledger *execution.Store
	hold   execution.Hold
	f      rentaltest.Fixture
	root   *os.Root
	path   string
}

func owned(t *testing.T) ownedFixture {
	t.Helper()
	f := rentaltest.Media(t)
	ledger, hold := f.Start(t, true)
	path := t.TempDir()
	root, e := os.OpenRoot(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	config := mediaworkspace.Config{Root: root, Ledger: ledger, Protect: func(string, bool) error { return nil }, Sync: func(string) error { return nil }}
	owner, e := mediaworkspace.OpenOwned(context.Background(), config, true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(owner.Close)
	return ownedFixture{owner, ledger, hold, f, root, path}
}
func (f ownedFixture) create(t *testing.T) mediaworkspace.Workspace {
	t.Helper()
	ctx := context.Background()
	ticket, _, d, e := f.ledger.BeginRentalInvocation(ctx, f.hold, "invoke_media", strings.Repeat("9", 64), f.f.Prepared.Details().RecipeHash)
	if e != nil || d != "SUBMISSION_RESERVED" {
		t.Fatal(d, e)
	}
	if _, e = ticket.Take(); e != nil {
		t.Fatal(e)
	}
	w, e := f.owner.Create(ctx, f.hold, "invoke_media", f.f.Expected)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(w.Close)
	return w
}
func (f ownedFixture) proof(t *testing.T, w mediaworkspace.Workspace) execution.RentalChildCleanup {
	t.Helper()
	ctx := context.Background()
	id := w.Record().Invocation
	if f.ledger.MarkRentalSubmitted(ctx, f.hold, id, "comfy_original") != nil || f.ledger.StopRentalInvocation(ctx, f.hold, id) != nil || f.ledger.ObserveRentalInvocationStopped(ctx, f.hold, id, execution.Observation{RequestID: id, BackendID: "comfy_original", State: "STOPPED"}) != nil {
		t.Fatal("synthetic stop metadata")
	}
	return execution.RentalChildCleanup{Session: f.hold.Owner(), Invocation: id, LocalGeneration: f.hold.LocalGeneration(), BackendID: "comfy_original", ManifestHash: strings.Repeat("a", 64), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}
}
func TestMediaWorkspaceReservedBeforeDirectoryAndReplayNeverRecreates(t *testing.T) {
	f := owned(t)
	w := f.create(t)
	record := w.Record()
	persisted, e := f.ledger.RentalMediaWorkspace(context.Background(), f.hold, record.Invocation)
	if e != nil || persisted.Basename != record.Basename || persisted.Phase != "READY" {
		t.Fatal("unregistered directory", e)
	}
	if _, e = f.owner.Create(context.Background(), f.hold, record.Invocation, f.f.Expected); e == nil {
		t.Fatal("recreated original workspace")
	}
	if _, e = os.Stat(filepath.Join(f.path, mediaworkspace.Directory, record.Basename, "ownership.anchor")); e != nil {
		t.Fatal(e)
	}
}
func TestMediaWorkspaceCannotDeleteUntilActualOriginalJoin(t *testing.T) {
	f := owned(t)
	w := f.create(t)
	joined := make(chan struct{})
	if w.Claim(context.Background(), f.f.Expected, joined) != nil {
		close(joined)
		t.Fatal("claim")
	}
	proof := f.proof(t, w)
	if w.RetireAndRemove(context.Background(), proof) == nil {
		close(joined)
		t.Fatal("deleted while original work remains")
	}
	close(joined)
	if e := w.RetireAndRemove(context.Background(), proof); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(f.path, mediaworkspace.Directory, w.Record().Basename)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("owned workspace retained after qualified synthetic removal")
	}
	status, e := f.ledger.RentalCurrent(context.Background(), f.hold)
	if e != nil || status.Hold != f.hold || status.Phase != "ACTIVE" {
		t.Fatal("child cleanup freed parent", e)
	}
}
func TestMediaWorkspaceAnchorDamageRetainsUnknownOwnerAndForeignFiles(t *testing.T) {
	f := owned(t)
	w := f.create(t)
	joined := make(chan struct{})
	if w.Claim(context.Background(), f.f.Expected, joined) != nil {
		close(joined)
		t.Fatal("claim")
	}
	close(joined)
	proof := f.proof(t, w)
	directory := filepath.Join(f.path, mediaworkspace.Directory, w.Record().Basename)
	if os.WriteFile(filepath.Join(directory, "ownership.anchor"), make([]byte, 32), 0600) != nil {
		t.Fatal("tamper fixture")
	}
	if e := w.RetireAndRemove(context.Background(), proof); e == nil {
		t.Fatal("adopted damaged anchor for deletion")
	}
	record, e := f.ledger.RentalMediaWorkspace(context.Background(), f.hold, "invoke_media")
	if e != nil || record.Phase != "QUARANTINED" {
		t.Fatal("lost unknown original ownership", e)
	}
	if _, e = os.Stat(directory); e != nil {
		t.Fatal("removed unconfirmed original directory", e)
	}
}
func TestMediaWorkspaceForeignFileStopsRecursiveRemoval(t *testing.T) {
	f := owned(t)
	w := f.create(t)
	joined := make(chan struct{})
	if w.Claim(context.Background(), f.f.Expected, joined) != nil {
		close(joined)
		t.Fatal("claim")
	}
	close(joined)
	proof := f.proof(t, w)
	path := filepath.Join(f.path, mediaworkspace.Directory, w.Record().Basename, "unowned.txt")
	if os.WriteFile(path, []byte("foreign fixture"), 0600) != nil {
		t.Fatal("fixture")
	}
	if w.RetireAndRemove(context.Background(), proof) == nil {
		t.Fatal("removed foreign file")
	}
	if _, e := os.Stat(path); e != nil {
		t.Fatal("foreign file lost", e)
	}
}

type delayedCloser struct {
	entered, release chan struct{}
	once             sync.Once
}

func (c *delayedCloser) Close() error {
	c.once.Do(func() { close(c.entered); <-c.release })
	return nil
}
func TestMediaWorkspaceReaderCloseAndOwnerShutdownWaitForActualHandles(t *testing.T) {
	f := owned(t)
	w := f.create(t)
	joined := make(chan struct{})
	if w.Claim(context.Background(), f.f.Expected, joined) != nil {
		close(joined)
		t.Fatal("claim")
	}
	close(joined)
	lease, e := w.AcquireRead(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	reader := &delayedCloser{entered: make(chan struct{}), release: make(chan struct{})}
	if lease.Open(func(*os.Root) (io.Closer, error) { return reader, nil }) != nil {
		t.Fatal("reader initialization")
	}
	first := make(chan struct{})
	go func() { lease.Close(); close(first) }()
	<-reader.entered
	shutdown := make(chan struct{})
	go func() { f.owner.Close(); close(shutdown) }()
	select {
	case <-shutdown:
		t.Fatal("original node root released during reader Close")
	default:
	}
	close(reader.release)
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("reader close not joined")
	}
	select {
	case <-shutdown:
	case <-time.After(2 * time.Second):
		t.Fatal("owner shutdown not joined")
	}
}
