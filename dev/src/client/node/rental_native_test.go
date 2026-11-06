package node

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
	relay "p11.local/compute/gateway/core"
)

func TestNodeRentalDiscardInputClearsOnlyItsOwnEnvelopeAndCapacity(t *testing.T) {
	f := rentaltest.Text(t, relay.ChatText)
	owned := f.Input(t, "invocation_owned", "1")
	other := f.Input(t, "invocation_other", "1")
	s := &NativeRental{inputs: map[*rentalaccess.TextInput]bool{owned: true}}
	s.DiscardInput(other)
	if _, e := s.Invoke(context.Background(), other); e == nil {
		t.Fatal("foreign envelope admitted")
	}
	raw, e := other.Take(context.Background(), f.Prepared, f.Expected, time.Now())
	if e != nil || !bytes.Contains(raw, []byte("private rental fixture")) {
		t.Fatal("foreign envelope was destroyed", e)
	}
	clear(raw)
	s.DiscardInput(owned)
	if len(s.inputs) != 0 {
		t.Fatal("discard leaked pending-envelope capacity")
	}
	if raw, e = owned.Take(context.Background(), f.Prepared, f.Expected, time.Now()); e == nil {
		clear(raw)
		t.Fatal("discard retained usable input key")
	}
}
func TestNodeRentalTimedCloseRetainsOSLockAndMasterUntilActualJoin(t *testing.T) {
	s := testStore(t, "http://127.0.0.1:1")
	key := s.master
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	openDone := make(chan struct{})
	close(openDone)
	done := make(chan struct{})
	n := &NativeRental{life: life, cancel: cancel, openDone: openDone, done: done, inputs: map[*rentalaccess.TextInput]bool{}}
	s.nativeRentals = map[string]*NativeRental{"resource_synthetic": n}
	ctx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if s.closeNativeRentalsWithin(ctx) {
		t.Fatal("unjoined owner reported closed")
	}
	if bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("master cleared before owned join")
	}
	if competing, e := openWith(context.Background(), s.path, testLoader); !errors.Is(e, ErrLocked) {
		if competing != nil {
			competing.Close()
		}
		t.Fatal("OS owner lock released before actual join", e)
	}
	close(done)
	s.finishClose() // sync.Once joins the same finalizer, without a second close.
	if !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("joined owner did not clear master")
	}
}
