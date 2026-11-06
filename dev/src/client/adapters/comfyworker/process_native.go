package comfyworker

import (
	"os"
	"os/exec"
)

func launchNative(s launchSpec) (nativeChild, error) {
	ir, iw, e := os.Pipe()
	if e != nil {
		return nativeChild{}, e
	}
	or, ow, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		return nativeChild{}, e
	}
	cr, cw, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		or.Close()
		ow.Close()
		return nativeChild{}, e
	}
	files := []*os.File{ir, iw, or, ow, cr, cw}
	closeAll := func() {
		for _, f := range files {
			f.Close()
		}
	}
	// Fixed native executable and flags. No shell, user PATH/PYTHONPATH/.pth,
	// existing ComfyUI server, public listener, inherited keys or logging pipe.
	cmd := exec.Command(s.python, "-I", "-B", "-S", s.bridge)
	cmd.Dir = s.root
	cmd.Stdin = ir
	cmd.Stdout = ow
	cmd.Env = nativeEnvironment(s.uuid)
	if e = configureNative(cmd, cr); e != nil {
		closeAll()
		return nativeChild{}, e
	}
	if e = cmd.Start(); e != nil {
		closeAll()
		return nativeChild{}, e
	}
	owner, e := ownNative(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeAll()
		return nativeChild{}, e
	}
	ir.Close()
	ow.Close()
	cr.Close()
	wait := make(chan error, 1)
	go func() { e := cmd.Wait(); owner(); wait <- e }()
	return nativeChild{input: iw, output: or, control: cw, wait: wait, kill: func() { _ = cmd.Process.Kill() }}, nil
}
