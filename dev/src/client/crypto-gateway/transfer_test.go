package cryptogateway

// SOURCE_ONLY: bound/deadline policy definitions; no native library or model.
import (
	"context"
	"math"
	"p11.local/compute/client/protocol/cipherstream"
	"testing"
	"time"
)

func TestCryptoTransferBoundsPrecedeInputAllocation(t *testing.T) {
	now := time.Now().UTC()
	b := Binding{Context: cipherstream.Context{Purpose: "INPUT", Deadline: now.Add(time.Minute).Format(time.RFC3339Nano)}, Limits: cipherstream.DefaultLimits("INPUT")}
	for _, limit := range []int64{-1, (2 << 20) + 1, math.MaxInt64} {
		b.Limits.DataBytes = limit
		if ctx, cancel, e := transferContext(context.Background(), b, now); e == nil || ctx != nil || cancel != nil {
			t.Fatal("unbounded input could reach allocation")
		}
	}
}

func TestCryptoTransferUsesTransportDeadlineAndOriginalParentCancellation(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(time.Minute)
	b := Binding{Context: cipherstream.Context{Purpose: "OUTPUT", Deadline: deadline.Format(time.RFC3339Nano), HardDeadline: now.Add(-time.Minute).Format(time.RFC3339Nano)}, Limits: cipherstream.DefaultLimits("OUTPUT")}
	parent, stop := context.WithCancel(context.Background())
	defer stop()
	ctx, cancel, e := transferContext(parent, b, now)
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	until, ok := ctx.Deadline()
	if !ok || !until.Equal(deadline) {
		t.Fatal("output transfer incorrectly used compute expiry")
	}
	stop()
	if ctx.Err() == nil {
		t.Fatal("original parent cancellation lost")
	}
	if _, _, e = transferContext(context.Background(), b, deadline); e == nil {
		t.Fatal("expired transfer admitted")
	}
}
