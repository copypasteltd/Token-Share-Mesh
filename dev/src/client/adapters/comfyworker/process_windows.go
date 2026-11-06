//go:build windows

package comfyworker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validWindowsDriver(path string) bool {
	system := os.Getenv("SystemRoot")
	return filepath.IsAbs(system) && filepath.Clean(system) == system && strings.EqualFold(path, filepath.Join(system, "System32", "nvcuda.dll"))
}
func nativeEnvironment(uuid string) []string {
	return []string{"SystemRoot=" + os.Getenv("SystemRoot"), "CUDA_VISIBLE_DEVICES=" + uuid, "CUDA_DEVICE_ORDER=PCI_BUS_ID", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1"}
}
func configureNative(cmd *exec.Cmd, control *os.File) error {
	h := windows.Handle(control.Fd())
	if e := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); e != nil {
		return e
	}
	cmd.Args = append(cmd.Args, "--control-handle", strconv.FormatUint(uint64(h), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW, AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(h)}}
	return nil
}

// A native Job owns process lifetime, not a VM or a low-privilege/TEE proof.
// Independent service identity and ACL qualification remain a separate gate.
func ownNative(cmd *exec.Cmd) (func(), error) {
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, e
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	process, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	e = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	return func() { windows.CloseHandle(job) }, nil
}
