package node

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"p11.local/compute/client/comfyenv"
)

func TestNodeComfyRegistryOriginalAnchorEncryptedReopenAndIndependentKey(t *testing.T) {
	s := registeredStore(t)
	registry, e := s.InitKernelRegistry()
	if e != nil {
		t.Fatal(e)
	}
	anchor := s.s.KernelID
	if len(anchor) != 64 {
		t.Fatal("missing original registry anchor")
	}
	sealed, e := s.read(kernelRegistryFile, comfyenv.MaximumRegistryBytes)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(sealed, []byte(s.kernelBinding())) || bytes.Contains(sealed, []byte(comfyenv.RegistryProfile)) {
		t.Fatal("registry metadata not encrypted")
	}
	if _, e = s.InitKernelRegistry(); e == nil {
		t.Fatal("registered original registry replaced")
	}
	if _, e = registry.Install(nil, comfyenv.Plan{}, nil); e == nil {
		t.Fatal("empty approval installed")
	}
	s = reopen(t, s)
	next, e := s.OpenKernelRegistry()
	if e != nil || s.s.KernelID != anchor {
		t.Fatal("original registry not reopened", e)
	}
	candidates, selections, e := next.Snapshot()
	if e != nil || len(candidates) != 0 || len(selections) != 0 {
		t.Fatal("reopen invented selections or execution", e)
	}
	otherKey := bytes.Repeat([]byte{77}, 32)
	if r, e := comfyenv.OpenOwned(kernelJournal{s}, otherKey, s.kernelBinding(), false); e == nil {
		r.Close()
		t.Fatal("unrelated key decrypted original registry")
	}
}
func TestNodeComfyRegistryMissingAnchoredJournalCannotBeReinitialized(t *testing.T) {
	s := registeredStore(t)
	if _, e := s.InitKernelRegistry(); e != nil {
		t.Fatal(e)
	}
	if e := s.root.Remove(kernelRegistryFile); e != nil {
		t.Fatal(e)
	}
	s = reopen(t, s)
	if _, e := s.OpenKernelRegistry(); e == nil {
		t.Fatal("missing original journal reopened")
	}
	if _, e := s.InitKernelRegistry(); e == nil {
		t.Fatal("missing anchored journal recreated")
	}
	if _, e := s.root.Lstat(kernelRegistryFile); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("refusal wrote a new journal", e)
	}
}
func TestNodeComfyRegistryRejectsForeignRootAndPreservesOSLockUntilClose(t *testing.T) {
	s := registeredStore(t)
	registry, e := s.InitKernelRegistry()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = openWith(t.Context(), s.path, testLoader); !errors.Is(e, ErrLocked) {
		t.Fatal("second process lock bypass", e)
	}
	sealed, e := s.read(kernelRegistryFile, comfyenv.MaximumRegistryBytes)
	if e != nil {
		t.Fatal(e)
	}
	other := registeredStore(t)
	if _, e = other.InitKernelRegistry(); e != nil {
		t.Fatal(e)
	}
	other.kernels.Close()
	other.kernels = nil
	if e = (kernelJournal{other}).Commit(sealed); e != nil {
		t.Fatal(e)
	}
	if _, e = other.OpenKernelRegistry(); e == nil {
		t.Fatal("foreign installation journal adopted")
	}
	if s.s.KernelID == strings.Repeat("0", 64) {
		t.Fatal("constant registry anchor")
	}
	s.Close()
	if _, _, e = registry.Snapshot(); e == nil {
		t.Fatal("registry survived parent key/root close")
	}
}
