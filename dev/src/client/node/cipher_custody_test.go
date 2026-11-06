package node

// SOURCE_ONLY: real native file/AEAD/lock boundaries with the existing
// test-only OS master loader. No native secretstream, engine or device runs.
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/client/crypto-gateway/ciphercustody"
)

func custodyPolicy() ciphercustody.Policy {
	return ciphercustody.Policy{Profile: ciphercustody.Profile, Bytes: "536870912", Objects: 64}
}

func TestNodeCipherCustodyRequiresOriginalKeyAnchorAndValidPolicyBeforeMutation(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitCipherCustody(custodyPolicy()); e == nil || s.s.CipherID != "" {
		t.Fatal("spool initialized without the original endpoint root")
	}
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	policy := custodyPolicy()
	policy.Bytes = "1"
	if _, e := s.InitCipherCustody(policy); e == nil || s.s.CipherID != "" {
		t.Fatal("invalid policy persisted a custody anchor")
	}
	if _, e := s.root.Lstat(cipherCustodyFile); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("rejected policy created a journal", e)
	}
}

func TestNodeCipherCustodyUsesOriginalRootLockAndAnchorAcrossReopen(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.InitCipherCustody(custodyPolicy()); e != nil {
		t.Fatal(e)
	}
	anchor, crypto := s.s.CipherID, s.s.CryptoID
	if _, e := openWith(context.Background(), s.path, testLoader); !errors.Is(e, ErrLocked) {
		t.Fatal("spool opened a second writer", e)
	}
	s = reopen(t, s)
	store, e := s.OpenCipherCustody()
	if e != nil || s.s.CipherID != anchor || s.s.CryptoID != crypto {
		t.Fatal("original custody identity changed", e)
	}
	view, e := store.Snapshot()
	if e != nil || view.Policy != custodyPolicy() || len(view.Objects) != 0 || view.Reserved != "0" {
		t.Fatal("original policy did not survive", e)
	}
}

func TestNodeMissingAnchoredCipherJournalNeverReinitializes(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.InitCipherCustody(custodyPolicy()); e != nil {
		t.Fatal(e)
	}
	path := s.path
	s.Close()
	if e := os.Remove(filepath.Join(path, cipherCustodyFile)); e != nil {
		t.Fatal(e)
	}
	restored, e := openWith(context.Background(), path, testLoader)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if _, e = restored.OpenCipherCustody(); e == nil {
		t.Fatal("missing original cipher journal was recreated")
	}
	if _, e = restored.InitCipherCustody(custodyPolicy()); e == nil {
		t.Fatal("original anchor was replaced")
	}
}

func TestNodeUnknownCipherFileIsPreservedAndCannotBeAdopted(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.InitCipherCustody(custodyPolicy()); e != nil {
		t.Fatal(e)
	}
	name := "cipher-object-" + strings.Repeat("1", 32) + ".bin"
	f, e := createPrivateFile(s, name)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	s = reopen(t, s)
	if _, e = s.OpenCipherCustody(); e == nil {
		t.Fatal("unknown original cipher file adopted outside quota")
	}
	if _, e = s.root.Lstat(name); e != nil {
		t.Fatal("unknown original file silently removed", e)
	}
}
