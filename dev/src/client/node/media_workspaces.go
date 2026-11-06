package node

import (
	"context"
	"path/filepath"

	"p11.local/compute/client/mediaworkspace"
)

// These methods use the SAME already OS-locked node root and original SQLite
// ledger. No second GPU gate, private database, adopted directory or key vault.
// Initialization is explicit before execution loops, not a restart repair.
func (s *Store) InitMediaWorkspaces(ctx context.Context) (*mediaworkspace.Owner, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenKernels() || s.execution == nil || s.media != nil || s.s.Pending != nil || s.host != nil || len(s.nativeRentals) != 0 {
		return nil, ErrState
	}
	return s.openMediaWorkspacesLocked(ctx, true)
}
func (s *Store) OpenMediaWorkspaces(ctx context.Context) (*mediaworkspace.Owner, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenKernels() || s.execution == nil {
		return nil, ErrState
	}
	if s.media != nil {
		return s.media, nil
	}
	return s.openMediaWorkspacesLocked(ctx, false)
}
func (s *Store) openMediaWorkspacesLocked(ctx context.Context, create bool) (*mediaworkspace.Owner, error) {
	config := mediaworkspace.Config{Root: s.root, Ledger: s.execution,
		Protect: func(relative string, dir bool) error {
			return securePath(filepath.Join(s.path, filepath.FromSlash(relative)), dir)
		},
		Sync: func(relative string) error { return syncDirectory(filepath.Join(s.path, filepath.FromSlash(relative))) },
	}
	owner, e := mediaworkspace.OpenOwned(ctx, config, create)
	if e != nil {
		return nil, e
	}
	s.media = owner
	return owner, nil
}
