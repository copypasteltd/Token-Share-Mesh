package execution

// SOURCE_ONLY definitions: no execution or qualification evidence.
import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCommandStatusKeepsOriginalReceiveAcrossSignedRenewalAndCleanup(t *testing.T) {
	s, clock, _ := newStore(t)
	ctx := context.Background()
	l := lease(clock, "lease_original", "job_original", "1", "1")
	original := verified(t, l, clock)
	hold, _, err := s.Receive(ctx, "command_original", original)
	if err != nil {
		t.Fatal(err)
	}
	clock.add(10 * time.Second)
	next := l
	next.Revision = 2
	next.Issued = clock.stamp.Wall.Format(time.RFC3339Nano)
	next.Expires = clock.stamp.Wall.Add(45 * time.Second).Format(time.RFC3339Nano)
	renewal := verified(t, next, clock)
	if err = s.Renew(ctx, "renew_original", hold, renewal); err != nil {
		t.Fatal(err)
	}
	v, err := s.CommandStatus(ctx, "command_original")
	if err != nil || !v.Found || v.MetadataHash != original.Digest() || v.CurrentMetadataHash != renewal.Digest() || v.Revision != 2 {
		t.Fatal("renewal lost immutable receive identity", v, err)
	}
	if _, disposition, e := s.Receive(ctx, "command_original", original); e != nil || disposition != "QUERY_ONLY" {
		t.Fatal("original duplicate became a new submission", disposition, e)
	}
	if err = s.RequestStop(ctx, hold); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveStopped(ctx, hold, Observation{RequestID: hold.Owner(), State: "ABSENT"}); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishCleanup(ctx, hold, Cleanup{ManifestHash: strings.Repeat("f", 64), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}); err != nil {
		t.Fatal(err)
	}
	v, err = s.CommandStatus(ctx, "command_original")
	if err != nil || v.Phase != "CLEANED" || v.MetadataHash != original.Digest() || v.CurrentMetadataHash != renewal.Digest() || v.CleanupHash != strings.Repeat("f", 64) {
		t.Fatal("renewal invalidated original cleanup", v, err)
	}
}

func TestCommandStatusRejectsChangedInboxWithoutRenewReceipt(t *testing.T) {
	s, clock, _ := newStore(t)
	l := lease(clock, "lease_original", "job_original", "1", "1")
	hold := receive(t, s, clock, l)
	l.Revision = 2
	changed := verified(t, l, clock)
	if _, err := s.db.Exec(`UPDATE inbox SET metadata_json=?,metadata_hash=? WHERE lease_id=?`, string(changed.Metadata()), changed.Digest(), hold.Owner()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommandStatus(context.Background(), "command_lease_original"); !errors.Is(err, ErrStorage) {
		t.Fatal("unreceipted metadata accepted", err)
	}
}

func TestCommandStatusRejectsCurrentWireDigestCorruption(t *testing.T) {
	s, clock, _ := newStore(t)
	receive(t, s, clock, lease(clock, "lease_original", "job_original", "1", "1"))
	if _, err := s.db.Exec(`UPDATE inbox SET metadata_hash=?`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommandStatus(context.Background(), "command_lease_original"); !errors.Is(err, ErrStorage) {
		t.Fatal("corrupt current metadata accepted", err)
	}
}
