//go:build p11_sodium && cgo && (windows || linux)

package endpoint

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestEndpointLocalRotationReturnsOriginalDurableTransitionAfterLostReply(t *testing.T) {
	now := time.Now().UTC()
	v, j := testVault(t)
	old, e := v.CreateLocal("buyer_original", "buyer_endpoint", "BUYER", now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	transition, e := v.RotateLocal(old.Hash(), now, now.Add(2*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	v.Close()
	reopened, e := OpenOwned(j, bytes.Repeat([]byte{19}, 32), strings.Repeat("a", 64), false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	original, e := reopened.RotateLocal(old.Hash(), now.Add(time.Second), now.Add(2*time.Hour))
	if e != nil || original.Hash() != transition.Hash() {
		t.Fatal("unknown rotation minted another identity", e)
	}
	if _, e = reopened.RotateLocal(old.Hash(), now, now.Add(3*time.Hour)); e == nil {
		t.Fatal("original rotation expiry silently changed")
	}
}
