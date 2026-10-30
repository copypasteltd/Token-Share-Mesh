package node

import (
	"bytes"
	"context"
	"errors"
	"os"
	"p11.local/compute/client/execution"
	"path/filepath"
	"strings"
	"testing"
)

func registeredStore(t *testing.T) *Store {
	t.Helper()
	s := testStore(t, "http://127.0.0.1:1")
	s.s.DeviceID = "device_test"
	s.s.SellerID = "seller_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestNodeExecutionAnchorSingleInitAndSharedProcessLock(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, "http://127.0.0.1:1")
	if _, e := s.InitExecution(ctx); !errors.Is(e, ErrState) {
		t.Fatal("unenrolled identity initialized execution metadata", e)
	}
	s.s.DeviceID = "device_test"
	s.s.SellerID = "seller_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	ledger, e := s.InitExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id := s.s.ExecutionID
	fp := s.fingerprint()
	if _, e = s.InitExecution(ctx); !errors.Is(e, ErrState) {
		t.Fatal("execution ledger reinitialized", e)
	}
	if _, e = openWith(ctx, s.path, testLoader); !errors.Is(e, ErrLocked) {
		t.Fatal("second node bypassed shared process lock", e)
	}
	if e = ledger.RegisterResource(ctx, "resource_test", strings.Repeat("e", 64)); e != nil {
		t.Fatal(e)
	}
	if _, e = ledger.AcquireLocal(ctx, "resource_test", "local_test", "LOCAL"); e != nil {
		t.Fatal(e)
	}
	raw, e := s.read("state.enc", stateLimit)
	if e != nil || bytes.Contains(raw, []byte(id)) {
		t.Fatal("execution anchor exposed outside protected journal", e)
	}
	s = reopen(t, s)
	if s.s.ExecutionID != id || s.fingerprint() != fp {
		t.Fatal("node or ledger identity changed")
	}
	ledger, e = s.OpenExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	status, e := ledger.Snapshot(ctx)
	if e != nil || len(status.Resources) != 1 || status.Resources[0].Phase != "QUARANTINED" || status.ExecutionEnabled {
		t.Fatal("reopen restored local execution authority", e)
	}
}
func TestNodeExecutionMissingDatabaseNeverResetsIdentity(t *testing.T) {
	ctx := context.Background()
	s := registeredStore(t)
	ledger, e := s.InitExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_ = ledger
	path := s.path
	id := s.s.ExecutionID
	fp := s.fingerprint()
	s.Close()
	if e = os.Remove(filepath.Join(path, executionFile)); e != nil {
		t.Fatal(e)
	}
	s, e = openWith(ctx, path, testLoader)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.OpenExecution(ctx); !errors.Is(e, ErrState) {
		t.Fatal("missing database recreated", e)
	}
	if _, e = s.InitExecution(ctx); !errors.Is(e, ErrState) {
		t.Fatal("anchor reset after losing database", e)
	}
	if s.s.ExecutionID != id || s.fingerprint() != fp {
		t.Fatal("lost execution metadata reset control identity")
	}
	if _, e = os.Stat(filepath.Join(path, executionFile)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("missing store silently created", e)
	}
}
func TestNodeExecutionInterruptedInitAndBindingFailClosed(t *testing.T) {
	ctx := context.Background()
	t.Run("preexisting_file", func(t *testing.T) {
		s := registeredStore(t)
		if e := s.writeNew(executionFile, nil); e != nil {
			t.Fatal(e)
		}
		if _, e := s.InitExecution(ctx); !errors.Is(e, ErrState) || s.s.ExecutionID != "" {
			t.Fatal("unanchored existing file adopted", e)
		}
	})
	t.Run("anchor_before_file", func(t *testing.T) {
		s := registeredStore(t)
		s.s.ExecutionID = strings.Repeat("a", 64)
		if e := s.save(); e != nil {
			t.Fatal(e)
		}
		s = reopen(t, s)
		if _, e := s.InitExecution(ctx); !errors.Is(e, ErrState) {
			t.Fatal("interrupted initialization reset", e)
		}
		if _, e := s.OpenExecution(ctx); !errors.Is(e, ErrState) {
			t.Fatal("interrupted initialization opened", e)
		}
	})
	t.Run("binding", func(t *testing.T) {
		s := registeredStore(t)
		if _, e := s.InitExecution(ctx); e != nil {
			t.Fatal(e)
		}
		s.execution.Close()
		s.execution = nil
		s.s.ExecutionID = strings.Repeat("b", 64)
		if e := s.save(); e != nil {
			t.Fatal(e)
		}
		s = reopen(t, s)
		if _, e := s.OpenExecution(ctx); !errors.Is(e, execution.ErrStorage) {
			t.Fatal("wrong protected anchor accepted", e)
		}
	})
}
