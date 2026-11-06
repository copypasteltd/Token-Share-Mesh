//go:build linux

package comfyworker

import (
	"os"
	"os/exec"
	"syscall"
)

func validWindowsDriver(string) bool { return false }
func nativeEnvironment(uuid string) []string {
	return []string{"LANG=C.UTF-8", "CUDA_VISIBLE_DEVICES=" + uuid, "CUDA_DEVICE_ORDER=PCI_BUS_ID", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1"}
}
func configureNative(cmd *exec.Cmd, control *os.File) error {
	cmd.ExtraFiles = []*os.File{control}
	cmd.Args = append(cmd.Args, "--control-fd", "3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return nil
}
func ownNative(*exec.Cmd) (func(), error) { return func() {}, nil }
