package comfyworker

// SOURCE_ONLY: synthetic pipes/state only; no Python, model or physical proof.
import (
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

func TestComfyMediaStopLatchesBeforeControlAndCannotExtendDeadline(t *testing.T) {
	p, _, _, control := transportProcess(t)
	defer close(p.done)
	p.mu.Lock()
	p.authorized = true
	p.mu.Unlock()
	p.RequestStop()
	p.RequestStop()
	if p.setCutoff(time.Now().Add(time.Hour)) {
		t.Fatal("stopped admission revived by a later deadline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if p.writeContent(ctx, Invoke, []byte("must never reach the content pipe")) == nil {
		t.Fatal("stop latch admitted content")
	}
	observed := make(chan error, 1)
	go func() {
		var h [5]byte
		if _, e := io.ReadFull(control, h[:]); e != nil {
			observed <- e
			return
		}
		n := binary.LittleEndian.Uint32(h[:4])
		if Kind(h[4]) != StopSession || n < 1 || n > 4097 {
			observed <- ErrOutput
			return
		}
		_, e := io.CopyN(io.Discard, control, int64(n-1))
		observed <- e
	}()
	select {
	case e := <-observed:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("independent original stop control did not arrive")
	}
	p.mu.Lock()
	closed := p.closedFrame
	p.mu.Unlock()
	if closed {
		t.Fatal("sending stop manufactured native CLOSED")
	}
}
func TestComfyMediaChildSuccessRequiresActualStopAndSenderJoin(t *testing.T) {
	adapter := &RentalMediaAdapter{}
	c := &MediaCall{adapter: adapter, result: true, senderDone: true}
	if c.Succeeded() {
		t.Fatal("result frame implied stopped GPU work")
	}
	c.stopped = true
	c.senderDone = false
	if c.Succeeded() {
		t.Fatal("stop frame ignored entered content sender")
	}
	c.senderDone = true
	if !c.Succeeded() {
		t.Fatal("original synthetic stopped/result state missing")
	}
	c.unknown = true
	if c.Succeeded() {
		t.Fatal("unknown frame state manufactured successful media")
	}
}
func TestComfyMediaEndRefusesUncleanedOriginalChildBeforeProcessClose(t *testing.T) {
	adapter := &RentalMediaAdapter{active: &MediaCall{}}
	if _, e := adapter.End(context.Background()); e == nil {
		t.Fatal("uncleaned child manufactured session cleanup")
	}
}
