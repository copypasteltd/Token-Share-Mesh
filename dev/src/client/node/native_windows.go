package node

import (
	"context"
	"crypto/rand"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

func ownerDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	t, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, ErrState
	}
	defer t.Close()
	u, err := t.GetTokenUser()
	if err != nil {
		return nil, ErrState
	}
	sid := u.User.Sid.String()
	return windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)")
}
func makePrivateDirectory(path string) error {
	sd, err := ownerDescriptor()
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrState
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	return windows.CreateDirectory(p, &sa)
}
func createPrivateFile(s *Store, name string) (*os.File, error) {
	sd, err := ownerDescriptor()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.path, name)
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrState
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, ErrState
	}
	return os.NewFile(uintptr(h), path), nil
}
func checkPath(path string, dir bool) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrState
	}
	a, err := windows.GetFileAttributes(p)
	if err != nil || a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (a&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != dir {
		return ErrState
	}
	return nil
}
func securePath(path string, dir bool) error {
	if checkPath(path, dir) != nil {
		return ErrState
	}
	t, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return ErrState
	}
	defer t.Close()
	u, err := t.GetTokenUser()
	if err != nil {
		return ErrState
	}
	current, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return ErrState
	}
	owner, _, err := current.Owner()
	if err != nil || !owner.Equals(u.User.Sid) {
		return ErrState
	}
	sd, err := ownerDescriptor()
	if err != nil {
		return ErrState
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return ErrState
	}
	if windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil) != nil {
		return ErrState
	}
	return nil
}
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func replaceFile(path string, _ *os.Root, from, to string) error {
	a, err := windows.UTF16PtrFromString(filepath.Join(path, from))
	if err != nil {
		return ErrState
	}
	b, err := windows.UTF16PtrFromString(filepath.Join(path, to))
	if err != nil {
		return ErrState
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
func syncDirectory(string) error { return nil } // File Sync + write-through replace; no power-loss acceptance claim.
func dpapi(input []byte, installation string, open bool) ([]byte, error) {
	if len(input) == 0 || len(input) > 16384 {
		return nil, ErrVault
	}
	entropy := []byte(stateProfile + "\n" + installation)
	in := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	var err error
	if open {
		err = windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, ErrVault
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if out.Data == nil || out.Size == 0 || out.Size > 16384 {
		return nil, ErrVault
	}
	raw := unsafe.Slice(out.Data, int(out.Size))
	defer clear(raw)
	return append([]byte{}, raw...), nil
}
func loadMaster(_ context.Context, s *Store, id string, create bool) ([]byte, error) {
	if create {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, ErrVault
		}
		sealed, err := dpapi(key, id, false)
		if err != nil || s.writeNew("master.dpapi", sealed) != nil {
			clear(key)
			return nil, ErrVault
		}
		return key, nil
	}
	sealed, err := s.read("master.dpapi", 16384)
	if err != nil {
		return nil, ErrVault
	}
	key, err := dpapi(sealed, id, true)
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, ErrVault
	}
	return key, nil
}
