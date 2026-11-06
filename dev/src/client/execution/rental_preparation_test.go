package execution

// SOURCE_ONLY: metadata fixtures, no kernel/driver/device or qualification.
import (
	"context"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

func TestRentalPreparationCallbackUsesOriginalOwnerWithoutInvokeOrRelease(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.reserve(t)
	called := 0
	deadline, e := s.WithRentalPreparation(context.Background(), h, f.p, func() error { called++; return nil })
	if e != nil || called != 1 || deadline.IsZero() || !deadline.Before(time.Now().Add(2*time.Minute)) {
		t.Fatal("original metadata callback/deadline missing", e)
	}
	if s.CheckRental(context.Background(), h, "INVOKE") == nil {
		t.Fatal("metadata reference created invoke authority")
	}
	status, e := s.RentalCurrent(context.Background(), h)
	if e != nil || status.Phase != "PREPARING" || status.Hold != h || status.Authorized {
		t.Fatal("metadata reference changed original physical occupant", e)
	}
	if e = s.BindRentalGrant(context.Background(), "bind_rental", h, f.p, f.g, f.expected); e != nil {
		t.Fatal(e)
	}
	if _, e = s.WithRentalPreparation(context.Background(), h, f.p, func() error { called++; return nil }); e == nil || called != 1 {
		t.Fatal("active grant reopened preparation callback")
	}
}
func TestRentalPreparationRejectsForeignApprovalAndExpiredClockBeforeCallback(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.reserve(t)
	approval := f.p.Approval()
	approval.PhysicalHash = strings.Repeat("9", 64)
	other, e := rentalaccess.VerifyPreparation(f.p.Raw(), approval, c.stamp.Wall)
	if e != nil {
		t.Fatal(e)
	}
	called := 0
	action := func() error { called++; return nil }
	if _, e = s.WithRentalPreparation(context.Background(), h, other, action); e == nil || called != 0 {
		t.Fatal("foreign approval reached local reference registration")
	}
	c.add(61 * time.Second)
	if _, e = s.WithRentalPreparation(context.Background(), h, f.p, action); e == nil || called != 0 {
		t.Fatal("expired original clock reached callback")
	}
}
func TestRentalPreparationReopenNeverRestoresPreparationClock(t *testing.T) {
	s, c, path := newStore(t)
	f := nativeRental(t, s, c)
	h := f.reserve(t)
	s.Close()
	reopened, e := Open(context.Background(), path, testBinding(), false, c)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	called := false
	if _, e = reopened.WithRentalPreparation(context.Background(), h, f.p, func() error { called = true; return nil }); e == nil || called {
		t.Fatal("persisted metadata recreated a live preparation clock")
	}
}
func TestRentalPreparationCancelledQueueNeverCallsRegistration(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.reserve(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if _, e := s.WithRentalPreparation(ctx, h, f.p, func() error { called = true; return nil }); e == nil || called {
		t.Fatal("cancelled request entered metadata callback")
	}
}
func TestRentalExpectedScopeCannotBorrowReplacementGrantAndKeepsOriginalDeadline(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.active(t)
	deadline, e := s.CheckRentalExpected(context.Background(), h, f.expected, "INVOKE")
	if e != nil || deadline.IsZero() {
		t.Fatal("current original grant denied", e)
	}
	foreign := f.expected
	foreign.Grant = "grant_foreign"
	if _, e = s.CheckRentalExpected(context.Background(), h, foreign, "INVOKE"); e == nil {
		t.Fatal("scope borrowed a different grant")
	}
	foreign = f.expected
	foreign.TokenDigest = strings.Repeat("9", 64)
	if _, e = s.CheckRentalExpected(context.Background(), h, foreign, "INVOKE"); e == nil {
		t.Fatal("scope borrowed a different credential digest")
	}
	again, e := s.CheckRentalExpected(context.Background(), h, f.expected, "INVOKE")
	if e != nil || !again.Equal(deadline) {
		t.Fatal("scope check extended original control clock", e)
	}
}
