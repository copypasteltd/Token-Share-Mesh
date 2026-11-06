package node

// SOURCE_ONLY definitions. The issuer/HTTP authority and execution observations
// are synthetic. No test in this file demonstrates a model, GPU or paid usage.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
)

func receivedProgressFixture(t *testing.T) (*hostFixture, string) {
	t.Helper()
	f := newHostFixture(t)
	id := "command_progress_original"
	f.begin(t, id)
	p, err := f.host.Prepare(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, _, err = f.host.ledger.Receive(context.Background(), id, p.accepted.Verified()); err != nil {
		t.Fatal(err)
	}
	if err = f.host.update(context.Background(), id, func(c *hostCommand) error { c.Phase = "SUBMITTING"; return nil }); err != nil {
		t.Fatal(err)
	}
	return f, id
}

func signedProgressContinuation(t *testing.T, f *hostFixture, change func(*executionlease.Lease)) executionlease.Lease {
	t.Helper()
	l := f.lease
	l.Revision = 2
	l.Issued = time.Now().UTC().Format(time.RFC3339Nano)
	l.Expires = time.Now().UTC().Add(44 * time.Second).Format(time.RFC3339Nano)
	if change != nil {
		change(&l)
	}
	message, err := executionlease.Message(l)
	if err != nil {
		t.Fatal(err)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, message))
	return l
}

func progressAck(action string, l *executionlease.Lease) devicecontrol.ProgressAck {
	return devicecontrol.ProgressAck{Ack: devicecontrol.Ack{ID: "report_original", Version: 1, Status: "RECORDED", Recorded: time.Now().UTC()}, Action: action, Lease: l}
}

func TestHostProgressUnknownReplaysExactWriteBeforeChangingPhase(t *testing.T) {
	f, id := receivedProgressFixture(t)
	next := signedProgressContinuation(t, f, nil)
	var mu sync.Mutex
	var bodies [][]byte
	var keys []string
	f.progressHandler = func(w http.ResponseWriter, r *http.Request, body []byte) {
		mu.Lock()
		bodies, keys = append(bodies, bytes.Clone(body)), append(keys, r.Header.Get("Idempotency-Key"))
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			drop(w)
			return
		}
		writeReply(w, 200, progressAck("CONTINUE", &next))
	}
	if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); !retryable(err) {
		t.Fatal("lost receipt did not remain unknown", err)
	}
	v, err := f.host.ReportProgress(context.Background(), id, "FAILED")
	if err != nil || v.Sequence != "1" || v.Disposition != "RENEWED" || v.Revision != 2 {
		t.Fatal("original renewal not restored", v, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) || keys[0] != keys[1] {
		t.Fatal("unknown report replaced its original body/key")
	}
	c, err := f.host.command(context.Background(), id)
	if err != nil || c.Progress.Write.Phase != "RUNNING" || !bytes.Equal(c.Lease, canonicalHostJSON(f.lease)) {
		t.Fatal("phase request or continuation overwrote original authority", err)
	}
	s, err := f.host.ledger.CommandStatus(context.Background(), id)
	if err != nil || s.MetadataHash != hostDigest(c.Lease) || s.CurrentMetadataHash == s.MetadataHash || s.Revision != 2 {
		t.Fatal("original receive projection lost", s, err)
	}
}

func TestHostProgressRejectsForeignContinuationBeforeSavingAck(t *testing.T) {
	for name, change := range map[string]func(*executionlease.Lease){
		"device":              func(l *executionlease.Lease) { l.Device = "foreign_device" },
		"resource_generation": func(l *executionlease.Lease) { l.Generation = "2" },
		"job_fence":           func(l *executionlease.Lease) { l.Fence = "2" },
		"environment":         func(l *executionlease.Lease) { l.Environment = strings.Repeat("b", 64) },
		"input":               func(l *executionlease.Lease) { l.InputAssets = []string{"foreign_input"} },
		"revision":            func(l *executionlease.Lease) { l.Revision = 1 },
		"not_before": func(l *executionlease.Lease) {
			old, _ := time.Parse(time.RFC3339Nano, l.NotBefore)
			l.NotBefore = old.Add(time.Nanosecond).Format(time.RFC3339Nano)
		},
		"hard_deadline": func(l *executionlease.Lease) {
			old, _ := time.Parse(time.RFC3339Nano, l.HardDeadline)
			l.HardDeadline = old.Add(time.Minute).Format(time.RFC3339Nano)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, id := receivedProgressFixture(t)
			next := signedProgressContinuation(t, f, change)
			f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeReply(w, 200, progressAck("CONTINUE", &next))
			}
			if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); !errors.Is(err, ErrHostPolicy) {
				t.Fatal("foreign continuation applied", err)
			}
			c, err := f.host.command(context.Background(), id)
			if err != nil || c.Progress == nil || c.Progress.Ack != nil || c.Progress.Applied {
				t.Fatal("bad continuation persisted", err)
			}
			status, err := f.host.ledger.CommandStatus(context.Background(), id)
			if err != nil || status.Revision != 1 {
				t.Fatal("bad continuation changed live metadata", err)
			}
		})
	}
}

func TestHostProgressRejectsWrongIndependentSigner(t *testing.T) {
	f, id := receivedProgressFixture(t)
	next := signedProgressContinuation(t, f, nil)
	message, _ := executionlease.Message(next)
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, 32))
	next.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(wrong, message))
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeReply(w, 200, progressAck("CONTINUE", &next))
	}
	if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); !errors.Is(err, ErrHostPolicy) {
		t.Fatal("task response supplied its own trust", err)
	}
}

func TestHostProgressStopLatchesWithoutReleasingResource(t *testing.T) {
	f, id := receivedProgressFixture(t)
	var calls atomic.Int32
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		calls.Add(1)
		writeReply(w, 200, progressAck("STOP", nil))
	}
	v, err := f.host.ReportProgress(context.Background(), id, "RUNNING")
	if err != nil || !v.Stopped || v.Disposition != "STOPPED" {
		t.Fatal(v, err)
	}
	if _, err = f.host.ReportProgress(context.Background(), id, "RUNNING"); err != nil || calls.Load() != 1 {
		t.Fatal("stopped report reopened renewal", err)
	}
	s, err := f.host.ledger.Snapshot(context.Background())
	if err != nil || s.Resources[0].Phase == "FREE" {
		t.Fatal("STOP became cleanup proof", err)
	}
	if err = f.host.Begin(context.Background(), "new_command_forbidden", "cap_test", 1); !errors.Is(err, ErrHostPending) {
		t.Fatal("unclean original replaced", err)
	}
}

func TestHostProgressRestartCannotRestoreGuardFromLateReceipt(t *testing.T) {
	f, id := receivedProgressFixture(t)
	next := signedProgressContinuation(t, f, nil)
	var calls atomic.Int32
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if calls.Add(1) == 1 {
			drop(w)
			return
		}
		writeReply(w, 200, progressAck("CONTINUE", &next))
	}
	if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); !retryable(err) {
		t.Fatal(err)
	}
	f.reopen(t)
	v, err := f.host.ReportProgress(context.Background(), id, "RUNNING")
	if err != nil || v.Disposition != "NOT_APPLIED" || !v.Stopped || v.Revision != 1 {
		t.Fatal("restart granted a renewed guard", v, err)
	}
	status, err := f.host.ledger.CommandStatus(context.Background(), id)
	if err != nil || status.Revision != 1 {
		t.Fatal("reopened ledger metadata renewed", err)
	}
}

func TestHostProgressRenewalPreservesOriginalCleanupProjection(t *testing.T) {
	f, id := receivedProgressFixture(t)
	next := signedProgressContinuation(t, f, nil)
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeReply(w, 200, progressAck("CONTINUE", &next))
	}
	if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); err != nil {
		t.Fatal(err)
	}
	r, err := f.host.ledger.Recovery(context.Background(), f.lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.host.ledger.RequestStop(context.Background(), r.Hold); err != nil {
		t.Fatal(err)
	}
	if err = f.host.ledger.ObserveStopped(context.Background(), r.Hold, execution.Observation{RequestID: r.Hold.Owner(), State: "ABSENT"}); err != nil {
		t.Fatal(err)
	}
	if err = f.host.ledger.FinishCleanup(context.Background(), r.Hold, execution.Cleanup{ManifestHash: strings.Repeat("a", 64), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}); err != nil {
		t.Fatal(err)
	}
	if err = f.host.ReconcileCleanup(context.Background(), id); err != nil {
		t.Fatal("signed renewal prevented original cleanup", err)
	}
	c, err := f.host.command(context.Background(), id)
	if err != nil || c.Phase != "CLEANED" || !bytes.Equal(c.Lease, canonicalHostJSON(f.lease)) {
		t.Fatal("cleanup changed original claim", err)
	}
}

func TestHostProgressJournalRejectsUnsignedAppliedState(t *testing.T) {
	f, id := receivedProgressFixture(t)
	if err := f.host.update(context.Background(), id, func(c *hostCommand) error {
		c.Progress = &hostProgress{Write: devicecontrol.ProgressWrite{Lease: f.lease.ID, Fence: f.lease.Fence, Sequence: "1", Phase: "RUNNING"}, Applied: true, Disposition: "RENEWED"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.host.command(context.Background(), id); !errors.Is(err, ErrHost) {
		t.Fatal("unsigned renewal marker accepted", err)
	}
}

func TestHostProgressSequenceCapacityDoesNotWrap(t *testing.T) {
	f, id := receivedProgressFixture(t)
	ack := progressAck("NO_CHANGE", nil)
	if err := f.host.update(context.Background(), id, func(c *hostCommand) error {
		c.Progress = &hostProgress{Write: devicecontrol.ProgressWrite{Lease: f.lease.ID, Fence: f.lease.Fence, Sequence: "9223372036854775807", Phase: "RUNNING"}, Ack: &ack, Applied: true, Disposition: "RECORDED"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.host.ReportProgress(context.Background(), id, "RUNNING"); !errors.Is(err, ErrCounter) {
		t.Fatal("progress sequence wrapped", err)
	}
}

func TestHostProgressTerminalCannotReopenExecution(t *testing.T) {
	f, id := receivedProgressFixture(t)
	f.progressHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeReply(w, 200, progressAck("NO_CHANGE", nil))
	}
	v, err := f.host.ReportProgress(context.Background(), id, "FAILED")
	if err != nil || !v.Stopped || v.Disposition != "LOCAL_STOPPED" {
		t.Fatal("local failure did not stop independent of receipt", v, err)
	}
	v, err = f.host.ReportProgress(context.Background(), id, "RUNNING")
	if err != nil || !v.Stopped || v.Sequence != "1" {
		t.Fatal("terminal progress reopened execution", v, err)
	}
}

func TestHostEmptyOfferRetainsOriginalClaimAndVersion(t *testing.T) {
	f := newHostFixture(t)
	f.claimHandler = func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeReply(w, 404, map[string]string{"code": "NOT_FOUND"})
	}
	c, err := f.host.providerCommand(context.Background(), "cap_test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := f.host.Prepare(context.Background(), c.ID); p != nil || !errors.Is(err, ErrHostNoOffer) {
		t.Fatal("empty poll treated as execution or revoked authority", err)
	}
	other, err := f.host.providerCommand(context.Background(), "cap_test", 2)
	if err != nil || other.ID != c.ID || other.Claim.Version != 1 || other.Phase != "CLAIM_PENDING" {
		t.Fatal("empty poll changed original key/body", err)
	}
	if err = f.host.Begin(context.Background(), "replacement_claim_denied", "cap_test", 2); !errors.Is(err, ErrHostPending) {
		t.Fatal("pending empty claim replaced", err)
	}
}

func TestHostProviderSelectionAuthenticatesAllRows(t *testing.T) {
	f := newHostFixture(t)
	f.begin(t, "command_original_selected")
	if _, err := f.host.db.Exec(`UPDATE commands SET capability_id='cap_other'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.host.providerCommand(context.Background(), "cap_test", 1); !errors.Is(err, ErrHost) {
		t.Fatal("query hint hid original command", err)
	}
}
