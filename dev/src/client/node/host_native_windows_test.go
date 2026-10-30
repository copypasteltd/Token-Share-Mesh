package node

import (
	"bytes"
	"context"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestHostNativeDPAPIACLAndEncryptedJournalReopen(t *testing.T) {
	f := newHostFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native_host")
	s, e := Create(ctx, path, testConfig(f.server.URL))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// Identity/order/kernel facts are explicitly synthetic. Current-user DPAPI,
	// owner/SYSTEM ACLs, AES-GCM journals and SQLite reopen are actual Windows.
	s.s.DeviceID = f.host.device
	s.s.SellerID = f.host.seller
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if e = s.save(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.InitExecution(ctx); e != nil {
		t.Fatal(e)
	}
	h, e := s.InitHost(ctx, f.config)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Begin(ctx, "command_native_original", "cap_test", 1); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{hostFile, hostFile + "-wal", hostFile + "-shm"} {
		file := filepath.Join(path, name)
		sd, e := windows.GetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if e != nil {
			t.Fatal(e)
		}
		acl, _, e := sd.DACL()
		if e != nil || acl == nil || acl.AceCount != 2 {
			t.Fatal("host journal is not owner/SYSTEM-only", e)
		}
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		for _, secret := range [][]byte{s.master, s.s.Seed, []byte(f.config.IssuerPin), []byte(f.config.Rules[0].Approval.Order)} {
			if bytes.Contains(raw, secret) {
				t.Fatal("protected host configuration or node key is plaintext")
			}
		}
	}
	anchor, executionAnchor, fp := s.s.HostID, s.s.ExecutionID, s.fingerprint()
	s.Close()
	s, e = Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h, e = s.OpenHost(ctx)
	if e != nil {
		t.Fatal(e)
	}
	status, e := h.Snapshot(ctx)
	if e != nil || s.s.HostID != anchor || s.s.ExecutionID != executionAnchor || s.fingerprint() != fp || len(status.Commands) != 1 || status.Commands[0].ID != "command_native_original" || status.Commands[0].Phase != "CLAIM_PENDING" || status.ExecutionEnabled {
		t.Fatal("native host originals did not reopen", e)
	}
}
