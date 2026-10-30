package node

import (
	"context"
	"errors"
	"os"
	"p11.local/compute/client/execution"
	"path/filepath"
)

const executionFile = "execution.db"

func (s *Store) checkExecutionFiles() error {
	for _, name := range []string{executionFile, executionFile + "-wal", executionFile + "-shm", executionFile + "-journal"} {
		if s.checkFile(name, name != executionFile) != nil {
			return ErrState
		}
	}
	return nil
}

// InitExecution is explicit and once-only. Save a random anchor in the OS-key
// protected journal BEFORE creating SQLite. An interrupted initialization,
// missing file, corrupt DB or mismatched binding is never silently recreated.
// There is deliberately no reset/force/free command.
func (s *Store) InitExecution(ctx context.Context) (*execution.Store, error) {
	if s.root == nil || s.poisoned || s.s.DeviceID == "" || s.s.Pending != nil || s.s.ExecutionID != "" || s.s.RemoteState == "REVOKED" {
		return nil, ErrState
	}
	for _, name := range []string{executionFile, executionFile + "-wal", executionFile + "-shm", executionFile + "-journal"} {
		if _, err := s.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrState
		}
	}
	id, e := randomHex(32)
	if e != nil {
		return nil, e
	}
	s.s.ExecutionID = id
	if e = s.save(); e != nil {
		return nil, e
	}
	f, e := createPrivateFile(s, executionFile)
	if e != nil {
		return nil, ErrState
	}
	err := f.Sync()
	closeErr := f.Close()
	if err != nil || closeErr != nil || s.checkExecutionFiles() != nil || syncDirectory(s.path) != nil {
		return nil, ErrState
	}
	return s.openExecution(ctx, true)
}
func (s *Store) OpenExecution(ctx context.Context) (*execution.Store, error) {
	return s.openExecution(ctx, false)
}
func (s *Store) openExecution(ctx context.Context, create bool) (*execution.Store, error) {
	if s.root == nil || s.poisoned || s.s.ExecutionID == "" || s.s.DeviceID == "" {
		return nil, ErrState
	}
	if s.execution != nil {
		return s.execution, nil
	}
	if s.checkExecutionFiles() != nil {
		return nil, ErrState
	}
	b := execution.Binding{Installation: s.s.Installation, LedgerID: s.s.ExecutionID, Device: s.s.DeviceID, Seller: s.s.SellerID}
	store, e := execution.Open(ctx, filepath.Join(s.path, executionFile), b, create, nil)
	if e != nil {
		return nil, e
	}
	if s.checkExecutionFiles() != nil {
		store.Close()
		return nil, ErrState
	}
	s.execution = store
	return store, nil
}
