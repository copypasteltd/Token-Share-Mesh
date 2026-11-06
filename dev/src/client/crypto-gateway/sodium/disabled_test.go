//go:build !p11_sodium || !cgo || (!windows && !linux)

package sodium

import (
	"errors"
	"testing"
)

func TestCipherUnavailableBuildNeverSubstitutesAnotherSuite(t *testing.T) {
	if !errors.Is(Available(), ErrUnavailable) {
		t.Fatal("control-only build claimed native cipher")
	}
	if p, s, e := Keypair(); p != nil || s != nil || !errors.Is(e, ErrUnavailable) {
		t.Fatal("fallback keys generated")
	}
	if b, e := Seal([]byte("private"), make([]byte, 32)); b != nil || !errors.Is(e, ErrUnavailable) {
		t.Fatal("fallback encryption returned bytes")
	}
	if s, h, e := NewPush(make([]byte, 32)); s != nil || h != nil || !errors.Is(e, ErrUnavailable) {
		t.Fatal("fallback stream created")
	}
}
