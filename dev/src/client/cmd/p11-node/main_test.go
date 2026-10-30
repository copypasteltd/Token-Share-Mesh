package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCLIRejectsCredentialArgumentsAndUnboundedInput(t *testing.T) {
	args, stderr, stdin := os.Args, os.Stderr, os.Stdin
	defer func() { os.Args = args; os.Stderr = stderr; os.Stdin = stdin }()
	capture, err := os.Create(filepath.Join(t.TempDir(), "safe-errors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	os.Stderr = capture
	secret := "SYNTHETIC_PAIRING_CREDENTIAL_MUST_NOT_BE_ECHOED"
	for _, bad := range [][]string{{"p11-node", "enroll", "--token", secret}, {"p11-node", secret}, {"p11-node", "status", "--state", "missing", secret}} {
		os.Args = bad
		if run() != 2 {
			t.Fatal("credential-bearing arguments accepted")
		}
	}
	_, _ = capture.Seek(0, 0)
	output, _ := io.ReadAll(capture)
	if bytes.Contains(output, []byte(secret)) {
		t.Fatal("credential echoed by flag diagnostic")
	}
	input, err := os.Create(filepath.Join(t.TempDir(), "oversize.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_, _ = input.WriteString(strings.Repeat("x", 513))
	_, _ = input.Seek(0, 0)
	os.Stdin = input
	if raw, e := readInput(512); e == nil || len(raw) != 0 {
		t.Fatal("oversize secret input accepted")
	}
}
