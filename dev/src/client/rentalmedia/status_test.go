package rentalmedia

// SOURCE_ONLY: synthetic original metadata; no hardware/SDK/cleanup proof.
import (
	"context"
	"testing"
	"time"
)

func TestMediaStateSeparatesActualStoppedResultFromDurableExecutionPhase(t *testing.T) {
	f := newMediaRuntime(t, true)
	v := f.invoke(t, "invoke_media")
	state, e := f.runtime.Status(context.Background(), v.ID)
	if e != nil || !state.Stopped || !state.Succeeded || !state.ResultAvailable || state.Invocation.Phase != "EXECUTING" {
		t.Fatal("actual result availability confused with metadata phase", e)
	}
	if f.runtime.Discard(context.Background(), v.ID) != nil {
		t.Fatal("discard original")
	}
	waitMedia(t, func() bool {
		state, e := f.runtime.Status(context.Background(), v.ID)
		return e == nil && state.Invocation.Phase == "CLEANED" && !state.ResultAvailable
	})
}
func TestMediaStateCancelACKCannotFabricateStoppedOrAvailableResult(t *testing.T) {
	f := newMediaRuntime(t, false)
	f.backend.autoStop = false
	f.backend.cancelStops = false
	go f.runtime.Run(context.Background())
	<-f.runtime.Ready()
	v := f.invoke(t, "invoke_media")
	defer f.backend.child.stop()
	if f.runtime.Cancel(context.Background(), v.ID) != nil {
		t.Fatal("cancel original")
	}
	state, e := f.runtime.Status(context.Background(), v.ID)
	if e != nil || state.Stopped || state.Succeeded || state.ResultAvailable {
		t.Fatal("cancel ACK forged stopped output", e)
	}
	f.backend.child.stop()
	ctx, end := context.WithTimeout(context.Background(), 2*time.Second)
	defer end()
	_ = f.runtime.Close(ctx)
}
