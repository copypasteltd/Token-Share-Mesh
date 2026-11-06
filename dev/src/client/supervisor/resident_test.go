package supervisor

// SOURCE_ONLY: trusted adapter and native SQLite policy fixtures, no inference.
import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResidentManifestBindingNeverInstallsAnotherBackend(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	env, manifest := strings.Repeat("2", 64), strings.Repeat("1", 64)
	s, err := NewResident(f.store, []KernelRegistration{{Environment: env, Capability: "cap_test", Backend: b}}, options())
	if err != nil {
		t.Fatal(err)
	}
	if s.lookup(env, "cap_test", manifest) != nil {
		t.Fatal("wildcard adapter fallback")
	}
	if err = s.BindManifest(env, "cap_test", manifest); err != nil {
		t.Fatal(err)
	}
	if s.lookup(env, "cap_test", manifest) != b {
		t.Fatal("original resident adapter changed")
	}
	if err = s.BindManifest(strings.Repeat("3", 64), "cap_test", manifest); err == nil {
		t.Fatal("foreign environment bound")
	}
	if err = s.BindManifest(env, "cap_other", manifest); err == nil {
		t.Fatal("foreign capability bound")
	}
	if err = s.BindManifest(env, "cap_test", manifest); err != nil {
		t.Fatal("same immutable binding not idempotent", err)
	}
	if n, _, _, _ := b.counts(); n != 0 {
		t.Fatal("metadata binding submitted work")
	}
	status, err := f.store.Snapshot(context.Background())
	if err != nil || status.Resources[0].Phase != "FREE" {
		t.Fatal("binding created an owner", err)
	}
}

func TestResidentRegistrySupportsConcurrentBoundedOriginalManifests(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	env := strings.Repeat("2", 64)
	s, err := NewResident(f.store, []KernelRegistration{{env, "cap_test", b}}, options())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 128; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			manifest := fmt.Sprintf("%064x", n)
			if e := s.BindManifest(env, "cap_test", manifest); e != nil {
				t.Error(e)
				return
			}
			if s.lookup(env, "cap_test", manifest) != b {
				t.Error("binding lost under concurrent reads")
			}
		}(n)
	}
	wg.Wait()
	s.Drain()
	if err = s.BindManifest(env, "cap_test", strings.Repeat("4", 64)); err != ErrClosed {
		t.Fatal("closed registry admitted new manifest", err)
	}
	if s.lookup(env, "cap_test", fmt.Sprintf("%064x", 0)) != b {
		t.Fatal("drain removed original cleanup binding")
	}
}

func TestResidentBindingCapacityDoesNotEvictOriginalCleanup(t *testing.T) {
	f := newFixture(t)
	b := newBackend()
	env := strings.Repeat("2", 64)
	s, err := NewResident(f.store, []KernelRegistration{{env, "cap_test", b}}, options())
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < MaximumManifestBindings; n++ {
		if e := s.BindManifest(env, "cap_test", fmt.Sprintf("%064x", n)); e != nil {
			t.Fatal(e)
		}
	}
	if err = s.BindManifest(env, "cap_test", fmt.Sprintf("%064x", MaximumManifestBindings)); err == nil {
		t.Fatal("unbounded registry")
	}
	if err = s.BindManifest(env, "cap_test", fmt.Sprintf("%064x", 0)); err != nil {
		t.Fatal("capacity refused exact original binding", err)
	}
	if s.lookup(env, "cap_test", fmt.Sprintf("%064x", 0)) != b {
		t.Fatal("original cleanup adapter evicted")
	}
}

func TestLegacyRegistryDoesNotAcquireResidentWildcardAuthority(t *testing.T) {
	f := newFixture(t)
	s := f.supervisor(t, newBackend())
	if err := s.BindManifest(strings.Repeat("2", 64), "cap_test", strings.Repeat("3", 64)); err == nil {
		t.Fatal("legacy exact registry became extensible")
	}
}

func TestResidentRejectsConflictingKernelSelection(t *testing.T) {
	f := newFixture(t)
	env := strings.Repeat("2", 64)
	if _, err := NewResident(f.store, []KernelRegistration{{env, "cap_test", newBackend()}, {env, "cap_test", newBackend()}}, options()); err == nil {
		t.Fatal("duplicate kernel environment accepted")
	}
}

// The late return deliberately violates the adapter cancellation contract. A
// timeout must retain the original ledger rather than signal lifecycle Done
// while Submit can still record its original unknown outcome.
type lateResidentBackend struct {
	*backend
	resume chan struct{}
}

func (b *lateResidentBackend) Submit(ctx context.Context, w Work) (Observation, error) {
	if _, err := b.backend.Submit(ctx, w); err != nil {
		return Observation{}, err
	}
	<-ctx.Done()
	<-b.resume
	return Observation{}, ctx.Err()
}

func TestResidentShutdownJoinsLateOriginalOperationBeforeDone(t *testing.T) {
	f := newFixture(t)
	b := &lateResidentBackend{backend: newBackend(), resume: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(b.resume) }) }
	t.Cleanup(unblock)
	s := f.supervisor(t, b)
	cancel, done := launch(t, s)
	joined := false
	t.Cleanup(func() {
		cancel()
		unblock()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("late original operation did not join")
			}
		}
	})
	submitted := make(chan error, 1)
	v := f.lease(t, "lease_late_original")
	go func() {
		_, err := s.Submit(context.Background(), "command_late_original", v)
		submitted <- err
	}()
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatal("original submission did not start")
	}
	cancel()
	await(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.joining
	})
	select {
	case <-s.Done():
		t.Fatal("Done allowed the original ledger to close before late return")
	default:
	}
	if snapshot, err := f.store.Snapshot(context.Background()); err != nil || snapshot.Resources[0].Phase == "FREE" {
		t.Fatal("shutdown timeout lost the original occupied owner", err)
	}
	unblock()
	select {
	case <-submitted:
	case <-time.After(time.Second):
		t.Fatal("original submission did not return")
	}
	select {
	case <-done:
		joined = true
	case <-time.After(time.Second):
		t.Fatal("watchdog did not join the original late operation")
	}
	if s.busy() {
		t.Fatal("Done left an original metadata operation active")
	}
}
