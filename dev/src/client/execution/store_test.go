package execution

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"p11.local/compute/client/protocol/executionlease"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testClock struct{ stamp Stamp }

func (c *testClock) Sample() Stamp { return c.stamp }
func newClock() *testClock {
	return &testClock{Stamp{time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), 0}}
}
func (c *testClock) add(d time.Duration) { c.stamp.Wall = c.stamp.Wall.Add(d); c.stamp.Elapsed += d }
func testBinding() Binding {
	return Binding{strings.Repeat("a", 64), strings.Repeat("d", 64), "device_test", "seller_test"}
}
func newStore(t *testing.T) (*Store, *testClock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "execution.db")
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	c := newClock()
	s, e := Open(context.Background(), path, testBinding(), true, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if e = s.RegisterResource(context.Background(), "resource_test", strings.Repeat("e", 64)); e != nil {
		t.Fatal(e)
	}
	return s, c, path
}
func lease(c *testClock, id, job, generation, fence string) executionlease.Lease {
	now := c.stamp.Wall
	return executionlease.Lease{ID: id, Job: job, Attempt: "attempt_" + id, Order: "order_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: generation, Capability: "cap_test", Manifest: "manifest_test", ManifestHash: strings.Repeat("1", 64), Environment: strings.Repeat("2", 64), Revision: 1, Issued: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), HardDeadline: now.Add(2 * time.Minute).Format(time.RFC3339Nano), Fence: fence, Expires: now.Add(45 * time.Second).Format(time.RFC3339Nano), ContractHash: strings.Repeat("3", 64), Profile: executionlease.Profile}
}
func verified(t *testing.T, l executionlease.Lease, c *testClock) executionlease.Verified {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{31}, 32)) // synthetic only
	m, e := executionlease.Message(l)
	if e != nil {
		t.Fatal(e)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, m))
	raw, _ := json.Marshal(l)
	hard, _ := time.Parse(time.RFC3339Nano, l.HardDeadline)
	p := executionlease.Plan{Job: l.Job, Attempt: l.Attempt, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Capability: l.Capability, Manifest: l.Manifest, ManifestHash: l.ManifestHash, Environment: l.Environment, Fence: l.Fence, ContractHash: l.ContractHash, HardDeadline: hard}
	v, e := executionlease.Verify(raw, key.Public().(ed25519.PublicKey), p, c.stamp.Wall)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func receive(t *testing.T, s *Store, c *testClock, l executionlease.Lease) Hold {
	t.Helper()
	h, d, e := s.Receive(context.Background(), "command_"+l.ID, verified(t, l, c))
	if e != nil || d != "RECEIVED" {
		t.Fatal("receive failed", e, d)
	}
	return h
}
func phase(t *testing.T, s *Store) ResourceStatus {
	t.Helper()
	r, e := s.Snapshot(context.Background())
	if e != nil || len(r.Resources) != 1 || r.ExecutionEnabled {
		t.Fatal("bad safe status", e)
	}
	return r.Resources[0]
}
func clean(t *testing.T, s *Store, h Hold, backend string) {
	t.Helper()
	ctx := context.Background()
	if e := s.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	state := "STOPPED"
	if backend == "" {
		state = "ABSENT"
	}
	if e := s.ObserveStopped(ctx, h, Observation{h.Owner(), backend, state}); e != nil {
		t.Fatal(e)
	}
	if e := s.FinishCleanup(ctx, h, Cleanup{true, true, true, strings.Repeat("f", 64)}); e != nil {
		t.Fatal(e)
	}
}

func TestNativeInboxOriginalCommandAndSingleSubmit(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	l := lease(c, "lease_one", "job_one", "1", "1")
	v := verified(t, l, c)
	h, d, e := s.Receive(ctx, "command_original", v)
	if e != nil || d != "RECEIVED" {
		t.Fatal(e, d)
	}
	for _, command := range []string{"command_original", "command_duplicate_delivery"} {
		other, d, e := s.Receive(ctx, command, v)
		if e != nil || d != "QUERY_ONLY" || other != h {
			t.Fatal("duplicate changed owner", e, d)
		}
	}
	changed := l
	changed.ID = "lease_other"
	if _, _, e = s.Receive(ctx, "command_original", verified(t, changed, c)); !errors.Is(e, ErrFence) {
		t.Fatal("command collision accepted", e)
	}
	key, e := s.BeginSubmit(ctx, h)
	if e != nil || key != l.ID {
		t.Fatal(e, key)
	}
	if _, e = s.BeginSubmit(ctx, h); !errors.Is(e, ErrQuery) {
		t.Fatal("second submit reserved", e)
	}
	if e = s.RecordBackend(ctx, h, "backend_one"); e != nil {
		t.Fatal(e)
	}
	r, e := s.Recovery(ctx, l.ID)
	if e != nil || r.RequestID != key || r.BackendID != "backend_one" || r.Phase != "EXECUTING" {
		t.Fatal("original backend mapping missing", e)
	}
	if _, e = s.BeginSubmit(ctx, h); !errors.Is(e, ErrQuery) {
		t.Fatal("executing owner resubmitted", e)
	}
}
func TestNativePhysicalGateUnifiesEnginesAndMaintenance(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	if e := s.RegisterResource(ctx, "resource_comfy_alias", strings.Repeat("e", 64)); !errors.Is(e, ErrFence) {
		t.Fatal("physical GPU registered twice", e)
	}
	h, e := s.AcquireLocal(ctx, "resource_test", "local_lmstudio", "LOCAL")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AcquireLocal(ctx, "resource_test", "maintenance_comfy", "MAINTENANCE"); !errors.Is(e, ErrBusy) {
		t.Fatal("maintenance bypassed local use", e)
	}
	if _, _, e = s.Receive(ctx, "command_one", verified(t, lease(c, "lease_one", "job_one", "1", "1"), c)); !errors.Is(e, ErrBusy) {
		t.Fatal("job bypassed local use", e)
	}
	if _, e = s.AcquireLocal(ctx, "resource_test", "rental_fake", "RENTAL"); !errors.Is(e, ErrFence) {
		t.Fatal("unsigned rental manufactured", e)
	}
	clean(t, s, h, "")
	m, e := s.AcquireLocal(ctx, "resource_test", "maintenance_comfy", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	if m.LocalGeneration() == h.LocalGeneration() {
		t.Fatal("local generation did not advance")
	}
	if _, e = s.AcquireLocal(ctx, "resource_test", "local_llama", "LOCAL"); !errors.Is(e, ErrBusy) {
		t.Fatal("local engine bypassed maintenance", e)
	}
	clean(t, s, m, "")
	_ = receive(t, s, c, lease(c, "lease_one", "job_one", "1", "1"))
	if phase(t, s).LocalHigh != "3" {
		t.Fatal("owners do not share one generation")
	}
}
func TestNativeDualFencesPreserveFullUint64HighWater(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	h := receive(t, s, c, lease(c, "lease_one", "job_one", "18446744073709551614", "18446744073709551614"))
	clean(t, s, h, "")
	for _, l := range []executionlease.Lease{lease(c, "lease_old", "job_other", "9007199254740993", "1"), lease(c, "lease_same", "job_one", "18446744073709551615", "18446744073709551614")} {
		if _, _, e := s.Receive(ctx, "command_"+l.ID, verified(t, l, c)); !errors.Is(e, ErrFence) {
			t.Fatal("stale job/resource fence accepted", e)
		}
	}
	h = receive(t, s, c, lease(c, "lease_last", "job_one", "18446744073709551615", "18446744073709551615"))
	clean(t, s, h, "")
	if phase(t, s).RemoteHigh != "18446744073709551615" {
		t.Fatal("uint64 high water rounded or reset")
	}
	if _, e := s.db.Exec(`UPDATE resources SET local_high='18446744073709551615'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AcquireLocal(ctx, "resource_test", "local_overflow", "LOCAL"); !errors.Is(e, ErrCapacity) {
		t.Fatal("local fence wrapped", e)
	}
}
func TestNativeRenewalRequiresNewSignedImmutableRevision(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	l := lease(c, "lease_one", "job_one", "1", "1")
	h := receive(t, s, c, l)
	c.add(10 * time.Second)
	next := l
	next.Revision = 2
	next.Issued = c.stamp.Wall.Format(time.RFC3339Nano)
	next.Expires = c.stamp.Wall.Add(45 * time.Second).Format(time.RFC3339Nano)
	if e := s.Renew(ctx, "renew_two", h, verified(t, next, c)); e != nil {
		t.Fatal(e)
	}
	changed := next
	changed.Revision = 3
	changed.Environment = strings.Repeat("4", 64)
	if e := s.Renew(ctx, "renew_changed", h, verified(t, changed, c)); !errors.Is(e, executionlease.ErrLease) {
		t.Fatal("renewal changed approved environment", e)
	}
	if e := s.Renew(ctx, "renew_old", h, verified(t, l, c)); !errors.Is(e, executionlease.ErrLease) {
		t.Fatal("old revision accepted", e)
	}
	if e := s.Renew(ctx, "renew_two", h, verified(t, next, c)); e != nil {
		t.Fatal("same renewal not idempotent", e)
	}
	c.add(36 * time.Second)
	if e := s.Tick(ctx); e != nil || phase(t, s).Phase != "PREPARING" {
		t.Fatal("valid renewal lost", e)
	}
	c.add(10 * time.Second)
	if e := s.Tick(ctx); e != nil || phase(t, s).Phase != "QUARANTINED" {
		t.Fatal("ordinary tick extended authority", e)
	}
	late := next
	late.Revision = 3
	late.Issued = c.stamp.Wall.Format(time.RFC3339Nano)
	late.Expires = c.stamp.Wall.Add(45 * time.Second).Format(time.RFC3339Nano)
	if e := s.Renew(ctx, "renew_late", h, verified(t, late, c)); !errors.Is(e, ErrRecovery) {
		t.Fatal("expired owner resurrected", e)
	}
}
func TestNativeDeadlineAndClockRollbackNeverFreeResource(t *testing.T) {
	for _, mode := range []string{"wall", "monotonic", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			s, c, _ := newStore(t)
			ctx := context.Background()
			h := receive(t, s, c, lease(c, "lease_one", "job_one", "1", "1"))
			switch mode {
			case "wall":
				c.stamp.Wall = c.stamp.Wall.Add(46 * time.Second)
			case "monotonic":
				c.stamp.Elapsed = 46 * time.Second
			case "rollback":
				c.stamp.Wall = c.stamp.Wall.Add(-time.Second)
				c.stamp.Elapsed = time.Second
			}
			e := s.Tick(ctx)
			if mode == "rollback" && !errors.Is(e, ErrRecovery) || mode != "rollback" && e != nil {
				t.Fatal(e)
			}
			if phase(t, s).Phase != "QUARANTINED" {
				t.Fatal("expired/rollback owner freed")
			}
			if _, e = s.BeginSubmit(ctx, h); !errors.Is(e, ErrRecovery) {
				t.Fatal("expired owner granted submit", e)
			}
			if _, e = s.AcquireLocal(ctx, "resource_test", "local_new", "LOCAL"); e == nil {
				t.Fatal("resource reused before stop/cleanup")
			}
			clean(t, s, h, "") // cleanup/query remain usable even with a broken wall clock
		})
	}
}
func TestNativeReopenQuarantinesWithoutResubmit(t *testing.T) {
	s, c, path := newStore(t)
	ctx := context.Background()
	l := lease(c, "lease_one", "job_one", "1", "1")
	h := receive(t, s, c, l)
	if _, e := s.BeginSubmit(ctx, h); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordBackend(ctx, h, "backend_one"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	next, e := Open(ctx, path, testBinding(), false, c)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if phase(t, next).Phase != "QUARANTINED" {
		t.Fatal("disk execution trusted after reopen")
	}
	r, e := next.Recovery(ctx, l.ID)
	if e != nil || r.Hold != h || r.BackendID != "backend_one" {
		t.Fatal("original owner/backend lost", e)
	}
	if _, e = next.BeginSubmit(ctx, h); !errors.Is(e, ErrRecovery) {
		t.Fatal("reopened owner resubmitted", e)
	}
	renew := l
	renew.Revision = 2
	if e = next.Renew(ctx, "renew_restart", h, verified(t, renew, c)); !errors.Is(e, ErrRecovery) {
		t.Fatal("renewal bypassed restart recovery", e)
	}
	clean(t, next, h, "backend_one")
	if phase(t, next).RemoteHigh != "1" {
		t.Fatal("cleanup reset remote generation")
	}
}
func TestNativeStopCleanupEvidenceAndOldOwnerCAS(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	h := receive(t, s, c, lease(c, "lease_one", "job_one", "1", "1"))
	_, _ = s.BeginSubmit(ctx, h)
	if e := s.RecordBackend(ctx, h, "backend_one"); e != nil {
		t.Fatal(e)
	}
	full := Cleanup{true, true, true, strings.Repeat("f", 64)}
	if e := s.FinishCleanup(ctx, h, full); !errors.Is(e, ErrEvidence) {
		t.Fatal("cleanup skipped actual stop", e)
	}
	if e := s.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Observation{{h.Owner(), "backend_one", "RUNNING"}, {h.Owner(), "", "UNKNOWN"}, {h.Owner(), "", "ABSENT"}, {h.Owner(), "backend_other", "STOPPED"}} {
		if e := s.ObserveStopped(ctx, h, o); !errors.Is(e, ErrEvidence) {
			t.Fatal("unknown/wrong backend treated as stopped", e)
		}
	}
	if e := s.ObserveStopped(ctx, h, Observation{h.Owner(), "backend_one", "STOPPED"}); e != nil {
		t.Fatal(e)
	}
	partial := full
	partial.TokensRevoked = false
	if e := s.FinishCleanup(ctx, h, partial); !errors.Is(e, ErrEvidence) {
		t.Fatal("partial cleanup freed resource", e)
	}
	if e := s.FinishCleanup(ctx, h, full); e != nil {
		t.Fatal(e)
	}
	next := receive(t, s, c, lease(c, "lease_two", "job_two", "2", "1"))
	if e := s.FinishCleanup(ctx, h, full); !errors.Is(e, ErrFence) {
		t.Fatal("old cleanup freed next owner", e)
	}
	if e := s.RequestStop(ctx, h); !errors.Is(e, ErrFence) {
		t.Fatal("old stop targeted next owner", e)
	}
	if _, e := s.BeginSubmit(ctx, next); e != nil {
		t.Fatal("next owner damaged", e)
	}
}
func TestNativeInboxSQLFailureRollsBackAndPoisonsGate(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	if _, e := s.db.Exec(`CREATE TRIGGER injected_failure BEFORE INSERT ON job_fences BEGIN SELECT RAISE(ABORT,'synthetic'); END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Receive(ctx, "command_one", verified(t, lease(c, "lease_one", "job_one", "1", "1"), c)); !errors.Is(e, ErrStorage) {
		t.Fatal("failed durable transaction granted authority", e)
	}
	var n int
	var phase, high string
	if e := s.db.QueryRow(`SELECT count(*) FROM inbox`).Scan(&n); e != nil || n != 0 {
		t.Fatal("partial inbox committed", e, n)
	}
	if e := s.db.QueryRow(`SELECT phase,remote_high FROM resources`).Scan(&phase, &high); e != nil || phase != "FREE" || high != "0" {
		t.Fatal("partial resource owner committed", e)
	}
	if _, e := s.AcquireLocal(ctx, "resource_test", "local_after_failure", "LOCAL"); !errors.Is(e, ErrStorage) {
		t.Fatal("poisoned gate reopened", e)
	}
}
func TestNativeStoreRejectsMissingCorruptionBindingAndSchemaDrift(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "binding", "schema", "version"} {
		t.Run(mode, func(t *testing.T) {
			s, c, path := newStore(t)
			b := testBinding()
			if mode == "schema" {
				if _, e := s.db.Exec(`CREATE TABLE unexpected(value TEXT)`); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "version" {
				if _, e := s.db.Exec(`PRAGMA user_version=2`); e != nil {
					t.Fatal(e)
				}
			}
			s.Close()
			switch mode {
			case "missing":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			case "corrupt":
				if e := os.WriteFile(path, []byte("SYNTHETIC_CORRUPT_DB"), 0600); e != nil {
					t.Fatal(e)
				}
			case "binding":
				b.LedgerID = strings.Repeat("b", 64)
			}
			if next, e := Open(context.Background(), path, b, false, c); e == nil {
				next.Close()
				t.Fatal("bad/lost store accepted")
			}
			if mode == "missing" {
				if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("lost database silently recreated", e)
				}
			}
		})
	}
}
func TestNativeConcurrentReceiveAndSubmitReserveOnlyOnce(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var holds []Hold
	var failures []error
	for i := 0; i < 20; i++ {
		l := lease(c, fmt.Sprintf("lease_%d", i), fmt.Sprintf("job_%d", i), "1", "1")
		v := verified(t, l, c)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, _, e := s.Receive(ctx, fmt.Sprintf("command_%d", i), v)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				holds = append(holds, h)
			} else {
				failures = append(failures, e)
			}
		}(i)
	}
	wg.Wait()
	if len(holds) != 1 {
		t.Fatal("concurrent owners admitted", len(holds))
	}
	for _, e := range failures {
		if !errors.Is(e, ErrBusy) {
			t.Fatal(e)
		}
	}
	count := 0
	failures = nil
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.BeginSubmit(ctx, holds[0])
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				count++
			} else {
				failures = append(failures, e)
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatal("duplicate backend submissions reserved", count)
	}
	for _, e := range failures {
		if !errors.Is(e, ErrQuery) {
			t.Fatal(e)
		}
	}
}
func TestNativeAbruptProcessExitRetainsAmbiguousSubmission(t *testing.T) {
	ctx := context.Background()
	if path := os.Getenv("P11_TEST_EXECUTION_CRASH_DB"); path != "" {
		c := newClock()
		s, e := Open(ctx, path, testBinding(), false, c)
		if e != nil {
			t.Fatal(e)
		}
		h := receive(t, s, c, lease(c, "lease_crash", "job_crash", "1", "1"))
		if _, e = s.BeginSubmit(ctx, h); e != nil {
			t.Fatal(e)
		}
		os.Exit(23) // intentional real process exit: no deferred Close/checkpoint
	}
	s, c, path := newStore(t)
	s.Close()
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	deadline, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	child := exec.CommandContext(deadline, executable, "-test.run=^TestNativeAbruptProcessExitRetainsAmbiguousSubmission$")
	child.Env = append(os.Environ(), "P11_TEST_EXECUTION_CRASH_DB="+path)
	output, e := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(e, &exited) || exited.ExitCode() != 23 {
		t.Fatal("synthetic crash helper did not exit as intended", e, string(output))
	}
	next, e := Open(ctx, path, testBinding(), false, c)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	r, e := next.Recovery(ctx, "lease_crash")
	if e != nil || r.Phase != "QUARANTINED" || r.RequestID != "lease_crash" || r.BackendID != "" {
		t.Fatal("ambiguous submission lost", e)
	}
	if _, e = next.BeginSubmit(ctx, r.Hold); !errors.Is(e, ErrRecovery) {
		t.Fatal("crash replay granted another submission", e)
	}
	if phase(t, next).Phase != "QUARANTINED" {
		t.Fatal("crash resource implicitly free")
	}
}
func TestNativeLateBackendQueryDuringStopNeverRestoresExecution(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	l := lease(c, "lease_one", "job_one", "1", "1")
	h := receive(t, s, c, l)
	_, _ = s.BeginSubmit(ctx, h)
	if e := s.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordBackend(ctx, h, "backend_late"); e != nil {
		t.Fatal(e)
	}
	if phase(t, s).Phase != "STOPPING" {
		t.Fatal("late backend response restored execution")
	}
	if e := s.RecordBackend(ctx, h, "backend_other"); !errors.Is(e, ErrFence) {
		t.Fatal("original backend replaced", e)
	}
	clean(t, s, h, "backend_late")
	_, d, e := s.Receive(ctx, "command_"+l.ID, verified(t, l, c))
	if e != nil || d != "QUERY_ONLY" || phase(t, s).Phase != "FREE" {
		t.Fatal("old delivery resurrected cleaned execution", e, d)
	}
}
func TestNativeMetadataCapacityAndSQLiteBounds(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	h := receive(t, s, c, lease(c, "lease_one", "job_one", "1", "1"))
	clean(t, s, h, "")
	for name, want := range map[string]int{"page_size": 4096, "foreign_keys": 1, "trusted_schema": 0, "cache_size": -2048, "mmap_size": 0, "synchronous": 2, "max_page_count": 16384} {
		var got int
		if s.db.QueryRow("PRAGMA "+name).Scan(&got) != nil || got != want {
			t.Fatal("SQLite bound differs", name, got)
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	for i := 1; i < MaximumReceipts; i++ {
		if _, e = tx.Exec(`INSERT INTO receipts(command_id,lease_id,metadata_hash,kind) VALUES(?,?,?,'RECEIVE')`, fmt.Sprintf("capacity_%d", i), h.Owner(), strings.Repeat("a", 64)); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Receive(ctx, "command_full", verified(t, lease(c, "lease_two", "job_two", "2", "1"), c)); !errors.Is(e, ErrCapacity) {
		t.Fatal("unbounded inbox accepted", e)
	}
	if phase(t, s).Phase != "FREE" || phase(t, s).RemoteHigh != "1" {
		t.Fatal("capacity refusal mutated resource")
	}
	r, e := s.Snapshot(ctx)
	if e != nil || r.Receipts != MaximumReceipts {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r)
	for _, forbidden := range []string{"metadata_json", "signature", "backend_id", "prompt", "input_asset_refs"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("safe status contains execution data", forbidden)
		}
	}
}
