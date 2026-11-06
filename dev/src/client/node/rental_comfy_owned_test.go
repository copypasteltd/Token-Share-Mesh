package node

// SOURCE_ONLY: original local ownership/metadata, not a native media runtime.
import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
)

func comfyEnvelope(t *testing.T, f rentaltest.Fixture, id string) *rentalaccess.MediaInput {
	t.Helper()
	m := f.Prepared.Details()
	policy := rentalaccess.MediaPolicy{Profile: rentalaccess.MediaPolicyProfile, Workflow: m.PublicSpec.WorkflowID, Revision: m.PublicSpec.WorkflowRevision, Protocol: m.Protocol, Kind: "PNG", MaximumOutputBytes: 4096}
	input, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, id, "1", policy, []byte(`{"prompt":"synthetic owned input"}`), nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(input.Close)
	return input
}
func TestNodeComfyInputDiscardKeepsForeignEnvelopeAndRevokesOriginal(t *testing.T) {
	f := rentaltest.Media(t)
	owned := comfyEnvelope(t, f, "invoke_owned")
	foreign := comfyEnvelope(t, f, "invoke_foreign")
	s := &ComfyRental{inputs: map[*rentalaccess.MediaInput]bool{owned: true}}
	s.DiscardInput(foreign)
	plain, e := foreign.Take(context.Background(), f.Prepared, f.Expected, time.Now())
	if e != nil {
		t.Fatal("foreign envelope destroyed", e)
	}
	plain.Close()
	s.DiscardInput(owned)
	if len(s.inputs) != 0 {
		t.Fatal("pending original input capacity retained")
	}
	if plain, e = owned.Take(context.Background(), f.Prepared, f.Expected, time.Now()); e == nil {
		plain.Close()
		t.Fatal("discard left original prompt key usable")
	}
}
func TestNodeComfyStopInputsWaitsEnteredLocalSetupBeforeJoin(t *testing.T) {
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &ComfyRental{life: life, cancel: cancel, inputsDone: make(chan struct{}), inputs: map[*rentalaccess.MediaInput]bool{}}
	s.operations.Add(1)
	release := sync.OnceFunc(s.operations.Done)
	defer release()
	s.StopInputs()
	s.StopInputs()
	ctx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if s.JoinInputs(ctx) == nil {
		t.Fatal("input cancellation skipped an entered setup call")
	}
	if s.enterAccess() {
		s.operations.Done()
		t.Fatal("late local API admitted after stop latch")
	}
	release()
	joined, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	if s.JoinInputs(joined) != nil {
		t.Fatal("actual entered setup completion did not join")
	}
}
func TestNodeCloseJoinsBothNativeAndComfyOwnersBeforeMasterRelease(t *testing.T) {
	s := testStore(t, "http://127.0.0.1:1")
	key := s.master
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	open := make(chan struct{})
	close(open)
	nativeDone, mediaDone := make(chan struct{}), make(chan struct{})
	releaseNative := sync.OnceFunc(func() { close(nativeDone) })
	releaseMedia := sync.OnceFunc(func() { close(mediaDone) })
	defer releaseNative()
	defer releaseMedia()
	native := &NativeRental{life: life, cancel: cancel, openDone: open, done: nativeDone, inputs: map[*rentalaccess.TextInput]bool{}}
	media := &ComfyRental{life: life, cancel: cancel, openDone: open, done: mediaDone, inputsDone: make(chan struct{}), inputs: map[*rentalaccess.MediaInput]bool{}}
	s.nativeRentals = map[string]*NativeRental{"resource_text": native}
	s.comfyRentals = map[string]*ComfyRental{"resource_media": media}
	ctx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if s.closeNativeRentalsWithin(ctx) {
		t.Fatal("unjoined text/media owners reported closed")
	}
	releaseNative()
	if bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("native owner join prematurely released media master")
	}
	if competing, e := openWith(context.Background(), s.path, testLoader); !errors.Is(e, ErrLocked) {
		if competing != nil {
			competing.Close()
		}
		t.Fatal("OS lock released before original media join", e)
	}
	releaseMedia()
	s.finishClose()
	if !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("all joined owners did not release master")
	}
}
