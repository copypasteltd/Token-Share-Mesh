package llamaworker

import (
	"os"
	"os/exec"
	"path/filepath"
)

func launch(c Config) (child, error) {
	ir, iw, e := os.Pipe()
	if e != nil {
		return child{}, e
	}
	or, ow, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		return child{}, e
	}
	cr, cw, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		or.Close()
		ow.Close()
		return child{}, e
	}
	files := []*os.File{ir, iw, or, ow, cr, cw}
	closeAll := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}
	cmd := exec.Command(c.Executable.Path)
	cmd.Dir = filepath.Dir(c.Library)
	cmd.Stdin = ir
	cmd.Stdout = ow
	cmd.Env = childEnvironment() // no inherited service credentials or content keys
	if e = configureProcess(cmd, cr); e != nil {
		closeAll()
		return child{}, e
	}
	if e = cmd.Start(); e != nil {
		closeAll()
		return child{}, e
	}
	owner, e := ownProcess(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeAll()
		return child{}, e
	}
	_ = ir.Close()
	_ = ow.Close()
	_ = cr.Close()
	wait := make(chan error, 1)
	go func() { err := cmd.Wait(); owner(); wait <- err }()
	return child{input: iw, control: cw, output: or, kill: func() { _ = cmd.Process.Kill() }, wait: wait, pid: cmd.Process.Pid}, nil
}
