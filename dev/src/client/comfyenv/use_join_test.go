package comfyenv

// SOURCE_ONLY: lifetime metadata fixture, no installed engine or GPU execution.
import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestComfyUseJoinCannotTreatCancellationAsActualClaimRelease(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	alias := u
	actual := make(chan struct{})
	release := sync.OnceFunc(func() { close(actual) })
	defer release()
	if u.Claim(actual) != nil {
		t.Fatal("claim original reference")
	}
	u.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if alias.Join(ctx) == nil {
		t.Fatal("cancelled context released actual process reference")
	}
	f.registry.mu.Lock()
	held := f.registry.uses[f.hold.Resource()] != nil
	f.registry.mu.Unlock()
	if !held {
		t.Fatal("cancelled reference no longer retained")
	}
	release()
	joined, end := context.WithTimeout(context.Background(), time.Second)
	defer end()
	if alias.Join(joined) != nil {
		t.Fatal("actual process join was not observed")
	}
	f.registry.mu.Lock()
	held = f.registry.uses[f.hold.Resource()] != nil
	f.registry.mu.Unlock()
	if held {
		t.Fatal("joined reference still holds environment directory")
	}
}
func TestComfyUseUnclaimedCloseReleasesJoinAndZeroUseCannotForgeIt(t *testing.T) {
	f := newRuntimeUseFixture(t)
	u := f.open(t)
	u.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if u.Join(ctx) != nil {
		t.Fatal("unclaimed original reference did not finish")
	}
	if (Use{}).Join(ctx) == nil {
		t.Fatal("zero reference manufactured an actual join")
	}
}
