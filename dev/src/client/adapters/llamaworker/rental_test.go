package llamaworker

import (
	"bytes"
	"context"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalruntime"
	relay "p11.local/compute/gateway/core"
)

// These use synthetic private pipes, not a launched model or a qualified GPU.
func rentalPipe(t *testing.T) (rentaltest.Fixture, *RentalAdapter, *syntheticPipe) {
	t.Helper()
	f := rentaltest.Text(t, relay.ChatText)
	c, _, _, _ := textCase(relay.ChatText, chatRequest)
	c.Resource = f.Expected.Resource
	c.Capability = f.Prepared.Details().Capability
	a, p := synthetic(t, c)
	return f, newRentalAdapter(a, f.Prepared, f.Policy, f.Prepared.Approval().PhysicalHash), p
}
func rentalWork(f rentaltest.Fixture, i *rentalaccess.TextInput) rentalruntime.Work {
	d := i.Descriptor()
	return rentalruntime.Work{Invocation: execution.RentalInvocation{ID: d.ID, Session: d.Session, Resource: d.Resource, LocalGeneration: d.LocalGeneration, Grant: d.Grant, GrantDigest: d.GrantDigest, InputCiphertextDigest: d.InputCiphertextDigest, RecipeDigest: d.RecipeHash, Phase: "SUBMITTING"}, Descriptor: d, Expected: f.Expected}
}

func TestNativeRentalTypedInputAndSubmitContextDoNotBecomeT1(t *testing.T) {
	f, a, p := rentalPipe(t)
	i := f.Input(t, "invocation_native", "1")
	w := rentalWork(f, i)
	ctx, cancel := context.WithCancel(context.Background())
	o, e := a.SubmitText(ctx, w, i)
	cancel()
	if e != nil {
		t.Fatal(e)
	}
	id, raw := p.request(t)
	defer clear(raw)
	if id != o.BackendID || !bytes.Contains(raw, []byte("private rental fixture")) {
		t.Fatal("original typed input was not encoded")
	}
	select {
	case <-p.control:
		t.Fatal("quick submit context canceled full invocation")
	case <-time.After(30 * time.Millisecond):
	}
	if _, e = a.SubmitText(context.Background(), w, i); e == nil {
		t.Fatal("same original invocation submitted twice")
	}
	p.done(id, 1)
	native := awaitState(t, a.native, rentalNativeWork(w), "STOPPED")
	proof, e := a.Cleanup(context.Background(), w, rentalruntime.Observation{RequestID: native.RequestID, BackendID: native.BackendID, State: native.State})
	if e != nil || !proof.InputCleared || !proof.WorkStopped || proof.Invocation != w.Invocation.ID || proof.ManifestHash == f.Prepared.Digest() {
		t.Fatal("child cleanup did not identify actual original execution", e)
	}
}

func TestNativeRentalCancelBeforeSubmitCreatesPermanentOriginalTombstone(t *testing.T) {
	f, a, _ := rentalPipe(t)
	i := f.Input(t, "invocation_revoked", "1")
	w := rentalWork(f, i)
	if e := a.Cancel(context.Background(), w); e != nil {
		t.Fatal(e)
	}
	if _, e := a.SubmitText(context.Background(), w, i); e == nil {
		t.Fatal("future start escaped original cancellation")
	}
	o, e := a.Query(context.Background(), w)
	if e != nil || o.State != "ABSENT" || o.BackendID != "" {
		t.Fatal("known pre-entry rejection not distinguished from unknown", e)
	}
	if _, e = a.Cleanup(context.Background(), w, o); e != nil {
		t.Fatal(e)
	}
	other := w
	other.Invocation.LocalGeneration = "2"
	other.Descriptor.LocalGeneration = "2"
	if _, e = a.Query(context.Background(), other); e == nil {
		t.Fatal("stale local generation rebound original tombstone")
	}
}

func TestNativeRentalCancelUsesSeparatePipeAndExitIsNotCleanup(t *testing.T) {
	f, a, p := rentalPipe(t)
	i := f.Input(t, "invocation_unknown", "1")
	w := rentalWork(f, i)
	o, e := a.SubmitText(context.Background(), w, i)
	if e != nil {
		t.Fatal(e)
	}
	// The content writer remains blocked: nobody has read BEGIN yet.
	if e = a.Cancel(context.Background(), w); e != nil {
		t.Fatal(e)
	}
	select {
	case id := <-p.control:
		if id != o.BackendID {
			t.Fatal("cancel targeted another native owner")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel waited for blocked content")
	}
	p.quit()
	awaitState(t, a.native, rentalNativeWork(w), "UNKNOWN")
	q, e := a.Query(context.Background(), w)
	if e != nil || q.State != "UNKNOWN" {
		t.Fatal("exit fabricated stopped work", e)
	}
	if _, e = a.Cleanup(context.Background(), w, q); e == nil {
		t.Fatal("unknown exit fabricated cleanup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = a.Join(ctx); e != nil {
		t.Fatal("owned process/pipe lifecycle failed to join", e)
	}
}
