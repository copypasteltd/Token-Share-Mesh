package node

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
)

func makePrivateDirectory(path string) error { return os.Mkdir(path, 0700) }
func createPrivateFile(s *Store, name string) (*os.File, error) {
	return s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
}
func checkPath(path string, dir bool) error {
	i, err := os.Lstat(path)
	if err != nil || i.Mode()&os.ModeSymlink != 0 || i.IsDir() != dir || (!dir && !i.Mode().IsRegular()) {
		return ErrState
	}
	return nil
}
func securePath(path string, dir bool) error {
	if checkPath(path, dir) != nil {
		return ErrState
	}
	i, err := os.Lstat(path)
	if err != nil {
		return ErrState
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || i.Mode().Perm()&0077 != 0 {
		return ErrState
	}
	return nil
}
func lockFile(f *os.File) error                               { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func replaceFile(_ string, r *os.Root, from, to string) error { return r.Rename(from, to) }
func syncDirectory(path string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ErrState
	}
	defer f.Close()
	return f.Sync()
}
func loadMaster(ctx context.Context, _ *Store, id string, create bool) ([]byte, error) {
	return desktopMaster(ctx, id, create)
}
