package supervisor

import (
	"context"
	"p11.local/compute/client/execution"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type deadlineBackend struct {
	*backend
	deadline time.Time
}

func (b *deadlineBackend) Submit(ctx context.Context, w Work) (Observation, error) {
	b.deadline, _ = ctx.Deadline()
	return b.backend.Submit(ctx, w)
}

func TestSupervisorAdapterDeadlineCannotExceedSignedExpiry(t *testing.T) {
	f := newFixture(t)
	f.store.Close()
	var e error
	f.store, e = execution.Open(context.Background(), f.path, f.binding, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	f.clock.mu.Lock()
	f.clock.stamp.Wall = time.Now().UTC()
	f.clock.mu.Unlock()
	l := f.lease(t, "lease_one").Details()
	expires := f.clock.Sample().Wall.Add(4 * time.Second)
	l.Expires = expires.Format(time.RFC3339Nano)
	b := &deadlineBackend{backend: newBackend()}
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}, Options{Poll: 10 * time.Millisecond, CallTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	if _, e = s.Submit(context.Background(), "command_one", sign(t, l, f.clock.Sample().Wall)); e != nil {
		t.Fatal(e)
	}
	if b.deadline.IsZero() || b.deadline.After(expires) {
		finish(t, cancel, done, nil)
		t.Fatal("metadata commit renewed the backend deadline", b.deadline.Sub(expires))
	}
	finish(t, cancel, done, nil)
}

type slowMetadataClock struct {
	source execution.Clock
	mu     sync.Mutex
	start  time.Time
	trace  []string
}

func (c *slowMetadataClock) Sample() execution.Stamp {
	stamp := c.source.Sample()
	pc, _, _, _ := runtime.Caller(2)
	c.mu.Lock()
	c.trace = append(c.trace, time.Since(c.start).String()+" "+runtime.FuncForPC(pc).Name())
	c.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	return stamp
}

func TestSupervisorMetadataStepsHaveIndependentBudgets(t *testing.T) {
	f := newFixture(t)
	f.store.Close()
	var e error
	clock := &slowMetadataClock{source: f.clock, start: time.Now()}
	f.store, e = execution.Open(context.Background(), f.path, f.binding, false, clock)
	if e != nil {
		t.Fatal(e)
	}
	b := newBackend()
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}, Options{Poll: time.Second, CallTimeout: 250 * time.Millisecond, MetadataTimeout: time.Second, ShutdownTimeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	joined := false
	defer func() {
		if !joined {
			cancel()
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Error("owned slow-metadata fixture did not exit")
			}
		}
	}()
	if _, e = s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal("individually bounded steps inherited an exhausted earlier budget", e)
	}
	if n, _, _, _ := b.counts(); n != 1 {
		t.Fatal("metadata budgets changed submission count", n)
	}
	cancel()
	select {
	case e = <-done:
		joined = true
		if e != nil {
			clock.mu.Lock()
			t.Log("metadata trace", clock.trace)
			clock.mu.Unlock()
			n, q, c, k := b.counts()
			t.Log("adapter submit/query/cancel/cleanup", n, q, c, k)
			status, stateErr := f.store.Snapshot(context.Background())
			t.Log("remaining metadata", status, stateErr)
			t.Fatal("bounded slow-metadata shutdown", e)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("slow-metadata shutdown exceeded its own fixture bound")
	}
	if f.phase() != "FREE" {
		t.Fatal("stop/cleanup did not finish")
	}
}

// Cleanup can finish between poll ticks. A completed owner must wake shutdown
// so its fixed deadline is not spent waiting for an unrelated next poll.
func TestSupervisorShutdownObservesCleanupBeforeNextPoll(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	s, e := New(f.store, []Registration{{strings.Repeat("2", 64), "cap_test", strings.Repeat("1", 64), b}}, Options{Poll: time.Second, CallTimeout: 150 * time.Millisecond, ShutdownTimeout: 500 * time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	cancel, done := launch(t, s)
	if _, e = s.Submit(context.Background(), "command_one", f.lease(t, "lease_one")); e != nil {
		t.Fatal(e)
	}
	cancel()
	select {
	case e = <-done:
		if e != nil {
			status, stateErr := f.store.Snapshot(context.Background())
			t.Fatal("shutdown missed completed cleanup between polls", e, status, stateErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown exceeded its fixed bound")
	}
	if f.phase() != "FREE" {
		t.Fatal("normal shutdown preceded actual owner cleanup")
	}
}
