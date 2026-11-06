package execution

// SOURCE_ONLY: native metadata assertions, not physical stopped-work evidence.
import (
	"context"
	"strings"
	"testing"
)

func TestRentalOutstandingBindsOriginalOwnerAndKeepsStoppingChildren(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	_, _ = reserveMedia(t, s, f, h, n)
	outstanding, e := s.RentalOutstanding(context.Background(), h)
	if e != nil || outstanding.Invocations != 1 || outstanding.Workspaces != 1 {
		t.Fatal("original outstanding metadata", e)
	}
	foreign := h
	foreign.local = "9999"
	if _, e = s.RentalOutstanding(context.Background(), foreign); e == nil {
		t.Fatal("foreign local generation borrowed original counters")
	}
	if s.StopRental(context.Background(), h) != nil {
		t.Fatal("stop original")
	}
	outstanding, e = s.RentalOutstanding(context.Background(), h)
	if e != nil || outstanding.Invocations != 1 || outstanding.Workspaces != 1 {
		t.Fatal("stop request erased outstanding work", e)
	}
}
func TestRentalOutstandingZeroDoesNotCreateSessionStopEvidence(t *testing.T) {
	s, _, h, _ := mediaMetadata(t)
	outstanding, e := s.RentalOutstanding(context.Background(), h)
	if e != nil || outstanding.Invocations != 0 || outstanding.Workspaces != 0 {
		t.Fatal("original empty counters", e)
	}
	e = s.FinishRentalSessionCleanup(context.Background(), h, RentalSessionCleanup{Session: h.Owner(), LocalGeneration: h.LocalGeneration(), ManifestHash: strings.Repeat("b", 64), WorkStopped: true, AllGrantsRevoked: true, WorkspaceCleared: true, OutputKeysHandled: true})
	if e == nil {
		t.Fatal("zero child count bypassed actual stop projection")
	}
	history, e := s.RentalHistory(context.Background(), h)
	if e != nil || history.Phase != "ACTIVE" {
		t.Fatal("count query changed original owner", e)
	}
}
