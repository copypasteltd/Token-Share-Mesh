package execution

// SOURCE_ONLY synthetic metadata; no engine, disk cleanup, GPU or qualification.
import (
	"context"
	"strings"
	"testing"
	"time"
)

func mediaMetadata(t *testing.T) (*Store, nativeRentalFixture, Hold, MediaNamespace) {
	t.Helper()
	s, c, _ := newStore(t)
	f := nativeRentalEngine(t, s, c, "COMFYUI")
	h := f.active(t)
	ticket, e := s.BeginMediaNamespace(context.Background(), strings.Repeat("1", 64), strings.Repeat("2", 64))
	if e != nil {
		t.Fatal(e)
	}
	n, e := ticket.Take()
	if e != nil || s.ConfirmMediaNamespace(context.Background(), n) != nil {
		t.Fatal("namespace reservation", e)
	}
	n.Phase = "READY"
	return s, f, h, n
}
func reserveMedia(t *testing.T, s *Store, f nativeRentalFixture, h Hold, n MediaNamespace) (*MediaWorkspaceTicket, MediaWorkspace) {
	t.Helper()
	ctx := context.Background()
	ticket, _, d, e := s.BeginRentalInvocation(ctx, h, "invoke_media", strings.Repeat("3", 64), f.p.Details().RecipeHash)
	if e != nil || d != "SUBMISSION_RESERVED" {
		t.Fatal(d, e)
	}
	if _, e = ticket.Take(); e != nil {
		t.Fatal(e)
	}
	out, w, d, e := s.ReserveRentalMediaWorkspace(ctx, h, n.ID, "invoke_media", f.expected, "ws_"+strings.Repeat("4", 32), strings.Repeat("5", 64), MaximumMediaWorkspaceBytes)
	if e != nil || d != "WORKSPACE_RESERVED" {
		t.Fatal(d, e)
	}
	return out, w
}
func TestMediaNamespaceAndWorkspaceTicketsAreOneUseAcrossAliases(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	ticket, w := reserveMedia(t, s, f, h, n)
	alias := ticket
	got, e := ticket.Take()
	if e != nil || got != w {
		t.Fatal(e)
	}
	if _, e = alias.Take(); e == nil {
		t.Fatal("copied workspace ticket recreated directory permission")
	}
	if _, e = s.BeginMediaNamespace(context.Background(), strings.Repeat("6", 64), strings.Repeat("7", 64)); e == nil {
		t.Fatal("reinitialized anchored namespace")
	}
}
func TestMediaWorkspaceReplayIsQueryOnlyAndCannotReplaceOriginalScope(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	_, w := reserveMedia(t, s, f, h, n)
	ctx := context.Background()
	ticket, got, d, e := s.ReserveRentalMediaWorkspace(ctx, h, n.ID, w.Invocation, f.expected, "ws_"+strings.Repeat("6", 32), strings.Repeat("7", 64), MaximumMediaWorkspaceBytes)
	if e != nil || ticket != nil || d != "QUERY_ONLY" || got != w {
		t.Fatal("replay minted a new path", d, e)
	}
	wrong := f.expected
	wrong.TokenDigest = strings.Repeat("9", 64)
	if _, _, _, e = s.ReserveRentalMediaWorkspace(ctx, h, n.ID, w.Invocation, wrong, w.Basename, w.AnchorHash, w.ReservedBytes); e == nil {
		t.Fatal("substituted original grant")
	}
}
func TestMediaWorkspaceCannotBorrowTextOwnerOrExpiredControl(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	h := f.active(t)
	ctx := context.Background()
	ticket, e := s.BeginMediaNamespace(ctx, strings.Repeat("1", 64), strings.Repeat("2", 64))
	if e != nil {
		t.Fatal(e)
	}
	n, _ := ticket.Take()
	if s.ConfirmMediaNamespace(ctx, n) != nil {
		t.Fatal("namespace")
	}
	if _, _, _, e = s.BeginRentalInvocation(ctx, h, "invoke_media", strings.Repeat("3", 64), f.p.Details().RecipeHash); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.ReserveRentalMediaWorkspace(ctx, h, n.ID, "invoke_media", f.expected, "ws_"+strings.Repeat("4", 32), strings.Repeat("5", 64), MaximumMediaWorkspaceBytes); e == nil {
		t.Fatal("borrowed text owner for media")
	}
}
func TestMediaWorkspaceCleanupMustPrecedeOriginalChildCleanup(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	_, w := reserveMedia(t, s, f, h, n)
	ctx := context.Background()
	if s.ConfirmRentalMediaWorkspace(ctx, h, w) != nil {
		t.Fatal("confirm")
	}
	if _, e := s.ClaimRentalMediaWorkspace(ctx, h, w.Invocation, f.expected); e != nil {
		t.Fatal(e)
	}
	if s.MarkRentalSubmitted(ctx, h, w.Invocation, "comfy_original") != nil || s.StopRentalInvocation(ctx, h, w.Invocation) != nil || s.ObserveRentalInvocationStopped(ctx, h, w.Invocation, Observation{w.Invocation, "comfy_original", "STOPPED"}) != nil {
		t.Fatal("synthetic original stop metadata")
	}
	proof := RentalChildCleanup{Session: w.Session, Invocation: w.Invocation, LocalGeneration: w.LocalGeneration, BackendID: "comfy_original", ManifestHash: strings.Repeat("a", 64), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}
	if s.FinishRentalInvocationCleanup(ctx, h, w.Invocation, proof) == nil {
		t.Fatal("child cleaned with outstanding media directory")
	}
	if s.RetireRentalMediaWorkspace(ctx, h, w.Invocation, proof) != nil {
		t.Fatal("retire")
	}
	ticket, e := s.BeginRentalMediaRemoval(ctx, h, w.Invocation)
	if e != nil {
		t.Fatal(e)
	}
	removing, e := ticket.Take()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginRentalMediaRemoval(ctx, h, w.Invocation); e == nil {
		t.Fatal("reissued recursive removal ticket")
	}
	// Metadata-only synthetic filesystem projection; no actual cleanup qualified.
	if s.FinishRentalMediaRemoval(ctx, h, removing) != nil || s.FinishRentalInvocationCleanup(ctx, h, w.Invocation, proof) != nil {
		t.Fatal("original child metadata cleanup")
	}
	status, e := s.RentalCurrent(ctx, h)
	if e != nil || status.Hold != h || status.Phase != "ACTIVE" {
		t.Fatal("child released parent", e)
	}
}
func TestMediaWorkspaceImmutableSQLAndPhysicalReservationLimits(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	_, w := reserveMedia(t, s, f, h, n)
	for _, column := range []string{"basename", "resource_id", "local_generation", "anchor_hash", "grant_digest", "input_ciphertext_digest"} {
		if _, e := s.db.Exec("UPDATE rental_media_workspaces SET "+column+"=? WHERE invocation_id=?", strings.Repeat("f", 64), w.Invocation); e == nil {
			t.Fatal("changed immutable ownership", column)
		}
	}
	if _, _, _, e := s.ReserveRentalMediaWorkspace(context.Background(), h, n.ID, w.Invocation, f.expected, w.Basename, w.AnchorHash, MaximumMediaWorkspaceBytes+1); e == nil {
		t.Fatal("unbounded physical reservation")
	}
}
func TestMediaReopenQuarantinesWorkspacesWithoutRestoringGrant(t *testing.T) {
	s, c, path := newStore(t)
	f := nativeRentalEngine(t, s, c, "COMFYUI")
	h := f.active(t)
	ctx := context.Background()
	ticket, e := s.BeginMediaNamespace(ctx, strings.Repeat("1", 64), strings.Repeat("2", 64))
	if e != nil {
		t.Fatal(e)
	}
	n, _ := ticket.Take()
	if s.ConfirmMediaNamespace(ctx, n) != nil {
		t.Fatal("namespace")
	}
	n.Phase = "READY"
	_, w := reserveMedia(t, s, f, h, n)
	binding := s.binding
	s.Close()
	reopened, e := Open(ctx, path, binding, false, c)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := reopened.RentalMediaWorkspace(ctx, h, w.Invocation)
	if e != nil || got.Phase != "QUARANTINED" || got.Basename != w.Basename {
		t.Fatal("reopen lost original ownership", e)
	}
	if _, e = reopened.ClaimRentalMediaWorkspace(ctx, h, w.Invocation, f.expected); e == nil {
		t.Fatal("disk record restored execution")
	}
}
func TestInterruptedMediaNamespaceInitializationNeverBecomesReadyOnReopen(t *testing.T) {
	s, c, path := newStore(t)
	ctx := context.Background()
	if _, e := s.BeginMediaNamespace(ctx, strings.Repeat("1", 64), strings.Repeat("2", 64)); e != nil {
		t.Fatal(e)
	}
	binding := s.binding
	s.Close()
	reopened, e := Open(ctx, path, binding, false, c)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	n, e := reopened.MediaNamespace(ctx)
	if e != nil || n.Phase != "QUARANTINED" {
		t.Fatal("interrupted namespace silently initialized", e)
	}
	if reopened.ConfirmMediaNamespace(ctx, MediaNamespace{ID: n.ID, AnchorHash: n.AnchorHash, Phase: "CREATING"}) == nil {
		t.Fatal("old ticket revived interrupted creation")
	}
}
func TestMediaWorkspaceCurrentControlDenialPreventsClaimWithoutChangingIdentity(t *testing.T) {
	s, f, h, n := mediaMetadata(t)
	_, w := reserveMedia(t, s, f, h, n)
	ctx := context.Background()
	if s.ConfirmRentalMediaWorkspace(ctx, h, w) != nil {
		t.Fatal("confirm")
	}
	if e := s.ApplyRentalControl(ctx, h, f.control(t, 2, "STOP", time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ClaimRentalMediaWorkspace(ctx, h, w.Invocation, f.expected); e == nil {
		t.Fatal("control denial entered native execution")
	}
	old, e := s.RentalMediaWorkspace(ctx, h, w.Invocation)
	if e != nil || old.Basename != w.Basename || old.GrantDigest != w.GrantDigest {
		t.Fatal("denial lost original workspace", e)
	}
}
