package comfyenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/client/execution"
)

func TestComfyActivationAndExplicitRollbackUseOriginalPhysicalMaintenanceGate(t *testing.T) {
	f, p, r, j, archives, _ := fixtureInstall(t)
	ctx := context.Background()
	dbPath := filepath.Join(j.root.Name(), "execution.db")
	if e := os.WriteFile(dbPath, nil, 0600); e != nil {
		t.Fatal(e)
	}
	gate, e := execution.Open(ctx, dbPath, execution.Binding{Installation: strings.Repeat("a", 64), LedgerID: strings.Repeat("c", 64), Device: "device_test", Seller: "seller_test"}, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer gate.Close()
	if e = gate.RegisterResource(ctx, "resource_test", strings.Repeat("d", 64)); e != nil {
		t.Fatal(e)
	}
	h, e := gate.AcquireLocal(ctx, "resource_test", "maintenance_test", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	selected, e := r.ActivateLocal(ctx, gate, h, p)
	if e != nil || selected.Current != p.Digest() || selected.LocalGeneration != h.LocalGeneration() {
		t.Fatal(e)
	}
	if r.Delete(ctx, p.Digest()) == nil {
		t.Fatal("selected environment deleted")
	}
	f.c.Revision = "allowlist_synthetic_2"
	next := f.plan(t)
	if _, e = r.Install(ctx, next, archives); e != nil {
		t.Fatal(e)
	}
	selected, e = r.ActivateLocal(ctx, gate, h, next)
	if e != nil || selected.Previous != p.Digest() || selected.Current != next.Digest() {
		t.Fatal("explicit replacement lost original selection", e)
	}
	selected, e = r.ActivateLocal(ctx, gate, h, p)
	if e != nil || selected.Previous != next.Digest() || selected.Current != p.Digest() {
		t.Fatal("explicit approved rollback failed", e)
	}
	status, e := gate.Snapshot(ctx)
	if e != nil || len(status.Resources) != 1 || status.Resources[0].Phase != "PREPARING" || status.Resources[0].LocalHigh != h.LocalGeneration() || status.Resources[0].RemoteHigh != "0" {
		t.Fatal("selection reset fences/released original resource", e)
	}
}
func TestComfyActivationRejectsLocalStoppedAndForeignMaintenanceHolds(t *testing.T) {
	_, p, r, j, _, _ := fixtureInstall(t)
	ctx := context.Background()
	dbPath := filepath.Join(j.root.Name(), "execution.db")
	if e := os.WriteFile(dbPath, nil, 0600); e != nil {
		t.Fatal(e)
	}
	gate, e := execution.Open(ctx, dbPath, execution.Binding{Installation: strings.Repeat("a", 64), LedgerID: strings.Repeat("c", 64), Device: "device_test", Seller: "seller_test"}, true, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer gate.Close()
	for _, resource := range []string{"resource_local", "resource_maintenance"} {
		if e = gate.RegisterResource(ctx, resource, sum([]byte(resource))); e != nil {
			t.Fatal(e)
		}
	}
	local, e := gate.AcquireLocal(ctx, "resource_local", "owner_local", "LOCAL")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.ActivateLocal(ctx, gate, local, p); e == nil {
		t.Fatal("local engine hold authorized environment replacement")
	}
	if _, e = r.ActivateLocal(ctx, gate, execution.Hold{}, p); e == nil {
		t.Fatal("foreign/empty hold accepted")
	}
	h, e := gate.AcquireLocal(ctx, "resource_maintenance", "maintenance_test", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	if e = gate.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	if _, e = r.ActivateLocal(ctx, gate, h, p); e == nil {
		t.Fatal("stopping maintenance owner adopted")
	}
	_, selections, e := r.Snapshot()
	if e != nil || len(selections) != 0 {
		t.Fatal("refusal persisted selection", e)
	}
}
