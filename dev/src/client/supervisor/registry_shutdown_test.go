package supervisor

import (
	"context"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	"strings"
	"testing"
)

func TestSupervisorUnavailableRegistryStillCancelsOtherOriginalOwners(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if e := f.store.RegisterResource(ctx, "resource_missing", strings.Repeat("b", 64)); e != nil {
		t.Fatal(e)
	}
	known := f.lease(t, "lease_known")
	missing := f.lease(t, "lease_missing").Details()
	missing.Resource = "resource_missing"
	missing.Environment = strings.Repeat("4", 64)
	unknown := sign(t, missing, f.clock.Sample().Wall)
	var missingHold execution.Hold
	for _, item := range []struct {
		command string
		lease   executionlease.Verified
	}{{"command_known", known}, {"command_missing", unknown}} {
		h, _, e := f.store.Receive(ctx, item.command, item.lease)
		if e != nil {
			t.Fatal(e)
		}
		if item.command == "command_missing" {
			missingHold = h
		}
		if _, e = f.store.BeginSubmit(ctx, h); e != nil {
			t.Fatal(e)
		}
	}
	f.store.Close()
	var e error
	f.store, e = execution.Open(ctx, f.path, f.binding, false, f.clock)
	if e != nil {
		t.Fatal(e)
	}
	activities, e := f.store.Activities(ctx)
	if e != nil || len(activities) != 2 || activities[0].Hold.Resource() != "resource_missing" {
		t.Fatal("fixture must visit missing binding before matching owner", activities, e)
	}
	b := newBackend()
	b.state = "RUNNING"
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	finish(t, cancel, done, ErrShutdown)
	await(t, func() bool { return !s.busy() })
	n, _, cancels, cleaned := b.counts()
	if n != 0 || cancels == 0 {
		t.Fatal("missing binding prevented original-owner cancellation", n, cancels)
	}
	snapshot, e := f.store.Snapshot(ctx)
	if e != nil || len(snapshot.Resources) != 2 {
		t.Fatal("shutdown made original metadata unavailable", snapshot, e)
	}
	for _, resource := range snapshot.Resources {
		if resource.Resource == "resource_missing" && resource.Phase != "QUARANTINED" {
			t.Fatal("unknown binding lost occupied owner", resource)
		}
		if resource.Resource == "resource_test" && resource.Phase == "FREE" && cleaned == 0 {
			t.Fatal("matching owner released without cleanup evidence", resource)
		}
	}
	remaining, e := f.store.Activities(ctx)
	if e != nil {
		t.Fatal(e)
	}
	retained := false
	for _, a := range remaining {
		if a.Hold.Resource() == "resource_missing" {
			retained = a.Hold == missingHold && !a.Authorized && a.Phase == "QUARANTINED"
		}
	}
	if !retained {
		t.Fatal("missing binding did not retain the exact original occupied owner", remaining)
	}
}
