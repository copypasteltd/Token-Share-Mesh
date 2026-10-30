package supervisor

import (
	"context"
	"strings"
	"testing"

	"p11.local/compute/client/execution"
)

// Deterministically reproduce the interleaving between a sweep snapshot and
// another worker's successful owner-scoped cleanup. No timing limit is relaxed.
func TestSupervisorStaleSweepAfterCleanupDoesNotFailOrTouchNextOwner(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s := f.supervisor(t, b)
	ctx := context.Background()
	h, _, e := f.store.Receive(ctx, "command_one", f.lease(t, "lease_one"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.BeginSubmit(ctx, h); e != nil {
		t.Fatal(e)
	}
	if e = f.store.RecordBackend(ctx, h, "backend_one"); e != nil {
		t.Fatal(e)
	}
	if e = f.store.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	if e = f.store.ObserveStopped(ctx, h, execution.Observation{RequestID: h.Owner(), BackendID: "backend_one", State: "STOPPED"}); e != nil {
		t.Fatal(e)
	}
	stale, e := f.store.Activities(ctx)
	if e != nil || len(stale) != 1 {
		t.Fatal("snapshot", e)
	}
	if e = f.store.FinishCleanup(ctx, h, execution.Cleanup{WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, ManifestHash: strings.Repeat("f", 64)}); e != nil {
		t.Fatal(e)
	}
	for _, shutdown := range []bool{false, true} {
		if shutdown {
			s.Drain()
		}
		if e = s.sweepActivities(ctx, stale, shutdown); e != nil {
			t.Fatal("completed owner treated as a shutdown/storage failure", e)
		}
	}
	if f.phase() != "FREE" {
		t.Fatal("cleanup was reversed")
	}
	next, e := f.store.AcquireLocal(ctx, "resource_test", "local_two", "LOCAL")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.sweepActivities(ctx, stale, true); e != nil {
		t.Fatal("old snapshot failed after next owner", e)
	}
	status, e := f.store.Snapshot(ctx)
	if e != nil || status.Resources[0].Phase != "PREPARING" || next.LocalGeneration() <= h.LocalGeneration() {
		t.Fatal("stale sweep changed next owner", e, status)
	}
	_, _, c, k := b.counts()
	if c != 0 || k != 0 {
		t.Fatal("old snapshot invoked backend against next owner")
	}
}
