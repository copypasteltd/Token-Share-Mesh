package endpoint

// SOURCE_ONLY. Synthetic public identities exercise ownership cancellation;
// they do not qualify the native cipher ABI, device or business execution.
import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestEndpointCipherCustodyCopiesNoSigningContentOrReceiptSecret(t *testing.T) {
	v, _ := testVault(t)
	now := time.Now().UTC()
	k := syntheticLocal(t, "BUYER", 9, now)
	installSynthetic(t, v, k)
	scope := Scope{Owner: k.Bundle.Owner, Endpoint: k.Bundle.Endpoint, Role: "BUYER", BundleHash: k.Bundle.Hash(), Purpose: "CIPHER_CUSTODY"}
	var retained *Material
	if e := v.Use(context.Background(), scope, now, func(_ context.Context, m *Material) error {
		if len(m.Signing) != 0 || len(m.ContentSecret) != 0 || m.Bundle.Hash() != k.Bundle.Hash() {
			t.Fatal("cipher custody obtained private key material or a foreign identity")
		}
		retained = m
		m.Bundle.Owner = "foreign_owner"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if retained.Signing != nil || retained.ContentSecret != nil {
		t.Fatal("owned material remained after exit")
	}
	public, e := v.Public()
	if e != nil || len(public) != 1 || public[0].Bundle.Hash() != k.Bundle.Hash() {
		t.Fatal("public custody changed the original key bundle", e)
	}
}

func TestEndpointBeginCloseCancelsAllOwnersBeforeJoiningAny(t *testing.T) {
	v, _ := testVault(t)
	now := time.Now().UTC()
	k := syntheticLocal(t, "BUYER", 10, now)
	installSynthetic(t, v, k)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	finished := make(chan error, 1)
	go func() {
		finished <- v.Use(context.Background(), Scope{Owner: k.Bundle.Owner, Endpoint: k.Bundle.Endpoint, Role: "BUYER", BundleHash: k.Bundle.Hash(), Purpose: "CIPHER_CUSTODY"}, now, func(ctx context.Context, _ *Material) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
			return ctx.Err()
		})
	}()
	<-entered
	// BeginClose must return while this actual owner is still alive, allowing
	// the parent to cancel a nested spool owner before joining either side.
	v.BeginClose()
	<-canceled
	closed := make(chan struct{})
	go func() { v.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("key root closed before its real owner exited")
	default:
	}
	once.Do(func() { close(release) })
	<-finished
	<-closed
}
