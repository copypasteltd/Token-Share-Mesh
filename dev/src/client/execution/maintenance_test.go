package execution

import (
	"context"
	"errors"
	"testing"
)

func TestMaintenanceRequiresOriginalPreparingOwnerAndNeverAcquiresAnotherMode(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	h, e := s.AcquireLocal(ctx, "resource_test", "maintenance_test", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	called := 0
	if e = s.WithMaintenance(ctx, h, func() error { called++; return nil }); e != nil || called != 1 {
		t.Fatal(e, called)
	}
	for _, changed := range []Hold{{}, {h.resource, "other_owner", h.local, h.mode}, {h.resource, h.owner, "2", h.mode}, {h.resource, h.owner, h.local, "LOCAL"}, {h.resource, h.owner, h.local, "RENTAL"}, {h.resource, h.owner, h.local, "JOB"}} {
		if e = s.WithMaintenance(ctx, changed, func() error { called++; return nil }); e == nil {
			t.Fatal("foreign owner or mode accepted")
		}
	}
	if called != 1 {
		t.Fatal("rejected hold entered metadata operation")
	}
	if _, e = s.AcquireLocal(ctx, h.resource, "other_local", "LOCAL"); !errors.Is(e, ErrBusy) {
		t.Fatal("maintenance admitted local engine", e)
	}
	l := lease(c, "lease_maintenance", "job_maintenance", "1", "1")
	if _, _, e = s.Receive(ctx, "command_maintenance", verified(t, l, c)); !errors.Is(e, ErrBusy) {
		t.Fatal("maintenance admitted job", e)
	}
	if e = s.RequestStop(ctx, h); e != nil {
		t.Fatal(e)
	}
	if s.CheckMaintenance(ctx, h) == nil {
		t.Fatal("stopping owner reused as maintenance permission")
	}
}
func TestMaintenanceHoldsPhysicalGateAcrossLocalMetadataCommit(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	h, e := s.AcquireLocal(ctx, "resource_test", "maintenance_test", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	entered, leave, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { finished <- s.WithMaintenance(ctx, h, func() error { close(entered); <-leave; return nil }) }()
	<-entered
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.AcquireLocal(cancelled, h.resource, "other_local", "LOCAL"); e == nil {
		t.Fatal("parallel metadata switch bypassed shared gate")
	}
	close(leave)
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMaintenance(ctx, h); e != nil {
		t.Fatal("cancelled waiter destroyed original owner", e)
	}
}
func TestMaintenanceMetadataFailureDoesNotManufactureCleanupOrFree(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()
	h, e := s.AcquireLocal(ctx, "resource_test", "maintenance_test", "MAINTENANCE")
	if e != nil {
		t.Fatal(e)
	}
	failure := errors.New("synthetic metadata commit unknown")
	if e = s.WithMaintenance(ctx, h, func() error { return failure }); !errors.Is(e, failure) {
		t.Fatal(e)
	}
	status := phase(t, s)
	if status.Phase != "PREPARING" || status.Mode != "MAINTENANCE" {
		t.Fatal("failed metadata operation released original physical owner")
	}
	if s.CheckMaintenance(ctx, Hold{}) == nil {
		t.Fatal("empty hold granted recovery")
	}
}

func TestMaintenanceCannotBorrowRentalSessionPhysicalOwner(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.reserve(t)
	entered := false
	if e := s.WithMaintenance(context.Background(), h, func() error { entered = true; return nil }); e == nil || entered {
		t.Fatal("rental owner accepted as maintenance")
	}
	status := phase(t, s)
	if status.Mode != "RENTAL" || status.Phase == "FREE" {
		t.Fatal("rental owner released by failed environment switch")
	}
}
