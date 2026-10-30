//go:build linux

package llamaworker

import (
	"os"
	"os/exec"
	"syscall"
)

func childEnvironment() []string { return []string{"LANG=C.UTF-8"} }
func configureProcess(cmd *exec.Cmd, control *os.File) error {
	cmd.ExtraFiles = []*os.File{control}
	cmd.Args = append(cmd.Args, "--control-fd", "3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return nil
}
func ownProcess(*exec.Cmd) (func(), error) { return func() {}, nil }
