package node

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeExecutionSQLiteInheritsPrivateWindowsACL(t *testing.T) {
	ctx := context.Background()
	s, e := Create(ctx, filepath.Join(t.TempDir(), "native_execution"), testConfig("http://127.0.0.1:1"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	// This is an explicit synthetic identity fixture, not real registration or
	// GPU discovery. DPAPI, ACL, SQLite files and reopen are native and actual.
	s.s.DeviceID = "device_acl_test"
	s.s.SellerID = "seller_acl_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if e = s.save(); e != nil {
		t.Fatal(e)
	}
	store, e := s.InitExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.RegisterResource(ctx, "resource_acl_test", strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	if e = s.checkExecutionFiles(); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{executionFile, executionFile + "-wal", executionFile + "-shm"} {
		path := filepath.Join(s.path, name)
		sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if e != nil {
			t.Fatal(e)
		}
		acl, _, e := sd.DACL()
		if e != nil || acl == nil || acl.AceCount != 2 {
			t.Fatal("execution metadata is not owner/SYSTEM-only", e)
		}
		b, e := os.ReadFile(path)
		if e != nil || bytes.Contains(b, s.s.Seed) || bytes.Contains(b, s.master) {
			t.Fatal("control secret copied into SQLite", e)
		}
	}
	path := s.path
	anchor := s.s.ExecutionID
	fp := s.fingerprint()
	s.Close()
	next, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	store, e = next.OpenExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	status, e := store.Snapshot(ctx)
	if e != nil || len(status.Resources) != 1 || status.ExecutionEnabled || next.s.ExecutionID != anchor || next.fingerprint() != fp {
		t.Fatal("native protected execution store did not reopen", e)
	}
}

func TestNativeNodeDPAPIAndACLProtectControlState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native")
	s, err := Create(context.Background(), path, testConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	fp := s.fingerprint()
	master := bytes.Clone(s.master)
	s.Close()
	defer clear(master)
	sealed, err := os.ReadFile(filepath.Join(path, "master.dpapi"))
	if err != nil || bytes.Contains(sealed, master) {
		t.Fatal("DPAPI master exposed", err)
	}
	s, err = Open(context.Background(), path)
	if err != nil || s.fingerprint() != fp {
		t.Fatal("DPAPI native reopen failed", err)
	}
	s.Close()
	for _, p := range []string{path, filepath.Join(path, "state.enc"), filepath.Join(path, "master.dpapi")} {
		sd, e := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
		if e != nil {
			t.Fatal(e)
		}
		acl, _, e := sd.DACL()
		if e != nil || acl == nil || acl.AceCount != 2 {
			t.Fatal("owner/SYSTEM-only ACL missing", e)
		}
	}
	sealed[len(sealed)-1] ^= 1
	if err = os.WriteFile(filepath.Join(path, "master.dpapi"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(context.Background(), path); !errors.Is(err, ErrVault) {
		t.Fatal("corrupt OS key silently replaced", err)
	}
}

func TestNativeNodeProcessDeathRetainsOriginalCommand(t *testing.T) {
	if path := os.Getenv("P11_TEST_NATIVE_NODE_STATE"); path != "" {
		s, e := Open(context.Background(), path)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		fmt.Println("NODE_LOCKED")
		for {
			time.Sleep(time.Second)
		}
	}
	path := filepath.Join(t.TempDir(), "native")
	s, err := Create(context.Background(), path, testConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	// The endpoint is deliberately unreachable: the encrypted enrollment
	// command must exist on disk before the transport can fail.
	fp := s.fingerprint()
	if e := s.Enroll(context.Background(), pairingToken()); !retryable(e) {
		t.Fatal("expected bounded transport failure", e)
	}
	key := s.s.Pending.Key
	body := bytes.Clone(s.s.Pending.Body)
	s.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestNativeNodeProcessDeathRetainsOriginalCommand$")
	child.Env = append(os.Environ(), "P11_TEST_NATIVE_NODE_STATE="+path)
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	ready := make(chan bool, 1)
	go func() { scan := bufio.NewScanner(output); ready <- scan.Scan() && scan.Text() == "NODE_LOCKED" }()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child could not load native state")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native child startup timed out")
	}
	if _, err = Open(context.Background(), path); !errors.Is(err, ErrLocked) {
		t.Fatal("second process bypassed writer lock", err)
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	s, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.fingerprint() != fp || s.s.Pending == nil || s.s.Pending.Key != key || !bytes.Equal(s.s.Pending.Body, body) {
		t.Fatal("process death lost/replaced original identity or command")
	}
}
