package execution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// A cancelled waiter has not touched SQLite. It must fail without destroying
// the original owner's only metadata needed for later stop/cleanup.
func TestNativeCancelledMetadataDoesNotPoisonOriginalOwner(t *testing.T) {
	s, _, _ := newStore(t)
	h, e := s.AcquireLocal(context.Background(), "resource_test", "local_test", "LOCAL")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.Snapshot(ctx); !errors.Is(e, ErrStorage) {
		t.Fatal("cancelled metadata operation accepted", e)
	}
	status, e := s.Snapshot(context.Background())
	if e != nil || len(status.Resources) != 1 || status.Resources[0].Phase != "PREPARING" || status.Resources[0].LocalHigh != h.LocalGeneration() {
		t.Fatal("cancelled pre-SQL operation poisoned or released original owner", e)
	}
}

func TestNativeCancelledMetadataWaitReturnsBeforeOwnerTransaction(t *testing.T) {
	s, _, _ := newStore(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	owner := make(chan error, 1)
	go func() {
		owner <- s.transaction(context.Background(), func(*sql.Tx, Stamp) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan error, 1)
	go func() { _, e := s.Snapshot(ctx); waiter <- e }()
	cancel()
	select {
	case e := <-waiter:
		if !errors.Is(e, ErrStorage) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		unblock()
		<-waiter
		<-owner
		t.Fatal("cancelled metadata waiter blocked behind another transaction")
	}
	unblock()
	if e := <-owner; e != nil {
		t.Fatal("unrelated original transaction interrupted", e)
	}
	if _, e := s.Snapshot(context.Background()); e != nil {
		t.Fatal("cancelled waiter poisoned the ledger", e)
	}
}

type blockedStamp struct {
	stamp   Stamp
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockedStamp) Sample() Stamp {
	c.once.Do(func() { close(c.entered); <-c.release })
	return c.stamp
}

func TestNativeMetadataLatencyCannotExtendExecutionAuthority(t *testing.T) {
	s, c, _ := newStore(t)
	l := lease(c, "lease_one", "job_one", "1", "1")
	l.Expires = c.stamp.Wall.Add(100 * time.Millisecond).Format(time.RFC3339Nano)
	v := verified(t, l, c)
	clock := &blockedStamp{stamp: c.stamp, entered: make(chan struct{}), release: make(chan struct{})}
	s.clock = clock
	type result struct {
		h Hold
		e error
	}
	done := make(chan result, 1)
	go func() { h, _, e := s.Receive(context.Background(), "command_one", v); done <- result{h, e} }()
	<-clock.entered
	time.Sleep(150 * time.Millisecond)
	close(clock.release)
	r := <-done
	if r.e != nil {
		t.Fatal(r.e)
	}
	a, e := s.Current(context.Background(), r.h)
	if e != nil || a.Authorized || a.Remaining > 0 {
		t.Fatal("metadata latency granted time beyond the original lease observation", e, a.Authorized, a.Remaining)
	}
	if phase(t, s).Phase == "FREE" {
		t.Fatal("expiry released an uncleaned owner")
	}
}
