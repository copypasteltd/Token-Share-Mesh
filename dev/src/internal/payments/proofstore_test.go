package payments

// SOURCE_ONLY: native file test definitions. The future test harness must keep
// temporary paths beneath this project's runtime. No tests have been executed.
import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newProofStoreFixture(t *testing.T) (*payFixture, *Vault, *ProofStore, string) {
	t.Helper()
	f := fixture(t, nil)
	v, e := NewVault("store_key", map[string][]byte{"store_key": bytes.Repeat([]byte{11}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	root := t.TempDir()
	s, e := OpenProofStore(root, v)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, end := context.WithTimeout(context.Background(), time.Second)
		defer end()
		if s.Close(ctx) != nil {
			t.Error("proof store not joined")
		}
	})
	return f, v, s, root
}
func TestMachinePayProofStoreWritesCiphertextAndCanReopenOriginal(t *testing.T) {
	f, v, s, root := newProofStoreFixture(t)
	ref, e := s.Put(context.Background(), f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "payments", "proofs", ref))
	if e != nil || bytes.Contains(raw, []byte(strings.Repeat("a", 64))) || bytes.Contains(raw, []byte("original_client_session")) {
		t.Fatal("plaintext or missing proof artifact")
	}
	clear(raw)
	if s.Close(context.Background()) != nil {
		t.Fatal("close original proof store")
	}
	reopened, e := OpenProofStore(root, v)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close(context.Background())
	p, e := reopened.Get(context.Background(), f.intent.Scope, ref, f.now)
	if e != nil {
		t.Fatal("original proof not recovered", e)
	}
	defer p.Close()
	original, e := p.payload()
	if e != nil || original.Method == nil || original.Method.ClientSession != "original_client_session" {
		t.Fatal("recovered proof lost original client session")
	}
}
func TestMachinePayProofStoreRefCannotCrossOrderOrTraverseRoot(t *testing.T) {
	f, _, s, _ := newProofStoreFixture(t)
	ref, e := s.Put(context.Background(), f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	other := f.intent.Scope
	other.Order = "order_other"
	other.PurposeRef = other.Order
	for _, candidate := range []string{ref, "../" + ref, "..\\" + ref, ref + "/extra"} {
		scope := f.intent.Scope
		if candidate == ref {
			scope = other
		}
		if p, e := s.Get(context.Background(), scope, candidate, f.now); e == nil {
			p.Close()
			t.Fatal("foreign/traversal proof opened")
		}
	}
	if s.Retire(context.Background(), other, ref) == nil {
		t.Fatal("foreign scope retired original proof")
	}
}
func TestMachinePayProofStoreTamperKeepsFinancialOutcomeUnresolved(t *testing.T) {
	f, _, s, root := newProofStoreFixture(t)
	ref, e := s.Put(context.Background(), f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(filepath.Join(root, "payments", "proofs", ref), []byte(`{"profile":"tampered"}`), 0600) != nil {
		t.Fatal("synthetic tamper write")
	}
	if p, e := s.Get(context.Background(), f.intent.Scope, ref, f.now); e == nil {
		p.Close()
		t.Fatal("tampered recovery proof opened")
	}
	if f.calls.Load() != 0 {
		t.Fatal("recovery failure initiated a payment")
	}
}
func TestMachinePayProofStoreRetirementIsOriginalAndIdempotent(t *testing.T) {
	f, _, s, _ := newProofStoreFixture(t)
	ref, e := s.Put(context.Background(), f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if s.Retire(context.Background(), f.intent.Scope, ref) != nil {
			t.Fatal("original cleanup not idempotent")
		}
	}
	if p, e := s.Get(context.Background(), f.intent.Scope, ref, f.now); e == nil {
		p.Close()
		t.Fatal("retired proof resurrected")
	}
	if len(s.known) != 0 {
		t.Fatal("retired artifact quota not released")
	}
}
func TestMachinePayProofStoreCloseDoesNotDestroyExternalKeyring(t *testing.T) {
	f, v, s, _ := newProofStoreFixture(t)
	encoded, e := v.Seal(f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(encoded)
	if s.Close(context.Background()) != nil {
		t.Fatal("close store")
	}
	p, e := v.Open(f.intent.Scope, encoded, f.now)
	if e != nil {
		t.Fatal("store destroyed keyring owned elsewhere")
	}
	p.Close()
	if _, e := s.Put(context.Background(), f.intent.Scope, proofFor(t), f.now.Add(time.Minute), f.now); e == nil {
		t.Fatal("closed store admitted new artifacts")
	}
}
