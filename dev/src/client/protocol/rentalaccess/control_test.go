package rentalaccess_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

func controlBytes(t *testing.T, f accessFixture, sequence uint64, decision string, issued, expires time.Time) []byte {
	t.Helper()
	c, e := f.signer.SignControl(f.metadata, sequence, decision, issued, expires)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestControlSignatureDomainAndImmutableGrantBoundary(t *testing.T) {
	f := fixture(t)
	raw := controlBytes(t, f, 1, "ALLOW", f.now, f.now.Add(45*time.Second))
	c, e := f.verifier.VerifyControl(raw, f.verified, f.expected, f.now)
	if e != nil {
		t.Fatal(e)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		t.Fatal("fixture")
	}
	pretty, _ := json.MarshalIndent(obj, "", " ")
	reordered, e := f.verifier.VerifyControl(pretty, f.verified, f.expected, f.now)
	if e != nil || reordered.Identity() != c.Identity() {
		t.Fatal("control identity depends on JSON layout")
	}
	message, e := c.Details().Message()
	if e != nil {
		t.Fatal(e)
	}
	want := strings.Join([]string{"P11_RENTAL_CONTROL_ED25519_1", "grant_original", f.metadata.Digest, "order_original", "session_original", "seller_original", "device_original", "resource_original", "12", "3", strings.Repeat("a", 64), strings.Repeat("a", 64), strings.Repeat("a", 64), "2026-11-06T00:10:00Z", "1", "ALLOW", "2026-11-06T00:00:00Z", "2026-11-06T00:00:45Z", "issuer_key_original"}, "\n") + "\n"
	if string(message) != want {
		t.Fatal("signature transcript changed")
	}
	for _, field := range []string{"grant_id", "grant_digest", "order_id", "session_id", "seller_id", "device_id", "resource_id", "resource_lock_generation", "terms_revision", "terms_hash", "kernel_environment_digest", "manifest_hash", "hard_deadline", "sequence", "decision", "issued_at", "expires_at", "issuer_key_id", "signature", "profile_id"} {
		t.Run(field, func(t *testing.T) {
			var changed map[string]any
			json.Unmarshal(raw, &changed)
			changed[field] = "other_value"
			body, _ := json.Marshal(changed)
			if _, e := f.verifier.VerifyControl(body, f.verified, f.expected, f.now); e == nil {
				t.Fatal("accepted changed signed context")
			}
		})
	}
	for _, body := range [][]byte{append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"sequence":"1"}`)...), bytes.Replace(raw, []byte(`"sequence":"1"`), []byte(`"sequence":1`), 1), append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"jku":"https://untrusted.invalid"}`)...)} {
		if _, e := f.verifier.VerifyControl(body, f.verified, f.expected, f.now); e == nil {
			t.Fatal("ambiguous control accepted")
		}
	}
	for _, until := range []time.Time{f.now, f.now.Add(45*time.Second + time.Nanosecond), f.now.Add(6 * time.Minute)} {
		if _, e := f.signer.SignControl(f.metadata, 1, "ALLOW", f.now, until); e == nil {
			t.Fatal("control extended term")
		}
	}
	if _, e := f.signer.SignControl(f.metadata, 0, "ALLOW", f.now, f.now.Add(time.Second)); e == nil {
		t.Fatal("zero sequence")
	}
	if _, e := f.signer.SignControl(f.metadata, 1, "EXTEND", f.now, f.now.Add(time.Second)); e == nil {
		t.Fatal("unsigned renewal decision")
	}
	if _, e := f.verifier.VerifyControl(raw, f.verified, f.expected, f.now.Add(45*time.Second)); e == nil {
		t.Fatal("expired control")
	}
}

func TestControlWindowDuplicatesCannotExtendAndLateRepliesCannotRevive(t *testing.T) {
	f := fixture(t)
	var saved []rentalaccess.Anchor
	save := func(a rentalaccess.Anchor) error { saved = append(saved, a); return nil }
	w, e := rentalaccess.NewWindow(f.verified, f.expected, f.now, save)
	if e != nil {
		t.Fatal(e)
	}
	if len(saved) != 1 || w.Check(f.expected, f.now) == nil {
		t.Fatal("initial watermark permitted work")
	}
	seq, e := w.Request(f.expected, f.now)
	if e != nil || seq != "1" || len(saved) != 2 {
		t.Fatal("request was not durably frozen")
	}
	if retry, e := w.Request(f.expected, f.now.Add(time.Second)); e != nil || retry != "1" || len(saved) != 2 {
		t.Fatal("transport retry advanced watermark")
	}
	raw := controlBytes(t, f, 1, "ALLOW", f.now, f.now.Add(30*time.Second))
	if e = w.Accept(f.verifier, raw, f.expected, f.now.Add(2*time.Second)); e != nil || w.Check(f.expected, f.now.Add(3*time.Second)) != nil {
		t.Fatal("valid original control did not permit liveness")
	}
	if e = w.Accept(f.verifier, raw, f.expected, f.now.Add(20*time.Second)); e != nil || len(saved) != 3 {
		t.Fatal("duplicate re-committed or failed")
	}
	if seq, e = w.Request(f.expected, f.now.Add(21*time.Second)); e != nil || seq != "2" {
		t.Fatal("next request")
	}
	late := controlBytes(t, f, 2, "ALLOW", f.now.Add(25*time.Second), f.now.Add(60*time.Second))
	if e = w.Accept(f.verifier, late, f.expected, f.now.Add(31*time.Second)); e == nil || !w.Snapshot().Stopped {
		t.Fatal("late reply revived expired prior window")
	}
	if _, e = w.Request(f.expected, f.now.Add(32*time.Second)); e == nil {
		t.Fatal("stopped window requested new work")
	}
}

func TestRestoredControlNeedsNewDurableSequenceAndExplicitStopIsTerminal(t *testing.T) {
	f := fixture(t)
	save := func(rentalaccess.Anchor) error { return nil }
	w, e := rentalaccess.NewWindow(f.verified, f.expected, f.now, save)
	if e != nil {
		t.Fatal(e)
	}
	w.Request(f.expected, f.now)
	old := controlBytes(t, f, 1, "ALLOW", f.now, f.now.Add(40*time.Second))
	// Crash before the first reply: an old outstanding response cannot restore
	// execution. A fresh higher sequence has to be persisted before new I/O.
	restored, e := rentalaccess.RestoreWindow(f.verified, f.expected, f.now.Add(time.Second), w.Snapshot(), save)
	if e != nil {
		t.Fatal(e)
	}
	if restored.Accept(f.verifier, old, f.expected, f.now.Add(2*time.Second)) == nil {
		t.Fatal("old outstanding response resumed after crash")
	}
	seq, e := restored.Request(f.expected, f.now.Add(3*time.Second))
	if e != nil || seq != "2" {
		t.Fatal("restore reused outstanding sequence")
	}
	if restored.Accept(f.verifier, old, f.expected, f.now.Add(4*time.Second)) == nil {
		t.Fatal("older request receipt accepted")
	}
	fresh := controlBytes(t, f, 2, "ALLOW", f.now.Add(3*time.Second), f.now.Add(43*time.Second))
	if e = restored.Accept(f.verifier, fresh, f.expected, f.now.Add(5*time.Second)); e != nil {
		t.Fatal(e)
	}
	checkpoint := restored.Snapshot()
	second, e := rentalaccess.RestoreWindow(f.verified, f.expected, f.now.Add(6*time.Second), checkpoint, save)
	if e != nil {
		t.Fatal(e)
	}
	if second.Check(f.expected, f.now.Add(7*time.Second)) == nil {
		t.Fatal("durable ALLOW restored a live monotonic window")
	}
	seq, e = second.Request(f.expected, f.now.Add(8*time.Second))
	if e != nil || seq != "3" {
		t.Fatal("restore did not advance accepted watermark")
	}
	stop := controlBytes(t, f, 3, "STOP", f.now.Add(8*time.Second), f.now.Add(40*time.Second))
	if second.Accept(f.verifier, stop, f.expected, f.now.Add(9*time.Second)) == nil || !second.Snapshot().Stopped {
		t.Fatal("STOP did not persist and terminate")
	}
	if second.Check(f.expected, f.now.Add(10*time.Second)) == nil || second.Accept(f.verifier, fresh, f.expected, f.now.Add(11*time.Second)) == nil {
		t.Fatal("stopped session revived")
	}
}

func TestControlDiskFailuresContextRevocationClockRollbackAndExhaustion(t *testing.T) {
	f := fixture(t)
	disk := errors.New("synthetic disk unavailable")
	if _, e := rentalaccess.NewWindow(f.verified, f.expected, f.now, func(rentalaccess.Anchor) error { return disk }); e == nil {
		t.Fatal("failed initial journal accepted")
	}
	for _, failAt := range []int{2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			calls := 0
			save := func(rentalaccess.Anchor) error {
				calls++
				if calls == failAt {
					return disk
				}
				return nil
			}
			w, e := rentalaccess.NewWindow(f.verified, f.expected, f.now, save)
			if e != nil {
				t.Fatal(e)
			}
			_, e = w.Request(f.expected, f.now)
			if failAt == 3 {
				if e != nil {
					t.Fatal(e)
				}
				e = w.Accept(f.verifier, controlBytes(t, f, 1, "ALLOW", f.now, f.now.Add(45*time.Second)), f.expected, f.now)
			}
			if e == nil || w.Check(f.expected, f.now.Add(time.Second)) == nil || !w.Snapshot().Stopped {
				t.Fatal("uncertain durable write did not poison execution")
			}
		})
	}
	for _, kind := range []string{"revoked", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			w, _ := rentalaccess.NewWindow(f.verified, f.expected, f.now, func(rentalaccess.Anchor) error { return nil })
			w.Request(f.expected, f.now)
			if e := w.Accept(f.verifier, controlBytes(t, f, 1, "ALLOW", f.now, f.now.Add(45*time.Second)), f.expected, f.now.Add(10*time.Second)); e != nil {
				t.Fatal(e)
			}
			current := f.expected
			at := f.now.Add(11 * time.Second)
			if kind == "revoked" {
				current.Grant = "grant_replacement"
			} else {
				at = f.now.Add(9 * time.Second)
			}
			if w.Check(current, at) == nil || !w.Snapshot().Stopped || w.Check(f.expected, f.now.Add(12*time.Second)) == nil {
				t.Fatal("revocation/clock rollback was reversible")
			}
		})
	}
	a := rentalaccess.Anchor{Profile: rentalaccess.AnchorProfile, GrantDigest: f.metadata.Digest, Session: f.claims.Session, Generation: f.claims.Generation, Terms: f.claims.Terms, Requested: "18446744073709551615", Accepted: "18446744073709551614", ReplyIdentity: strings.Repeat("b", 64)}
	w, e := rentalaccess.RestoreWindow(f.verified, f.expected, f.now, a, func(rentalaccess.Anchor) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Request(f.expected, f.now); e == nil || !w.Snapshot().Stopped {
		t.Fatal("sequence wrapped")
	}
	a.Generation = "13"
	if _, e = rentalaccess.RestoreWindow(f.verified, f.expected, f.now, a, func(rentalaccess.Anchor) error { return nil }); e == nil {
		t.Fatal("foreign resource anchor")
	}
}

func TestControlAnchorCodecRejectsAmbiguityWithoutClaimingEncryption(t *testing.T) {
	f := fixture(t)
	w, e := rentalaccess.NewWindow(f.verified, f.expected, f.now, func(rentalaccess.Anchor) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	w.Request(f.expected, f.now)
	raw, e := rentalaccess.EncodeAnchor(w.Snapshot(), f.verified, f.expected)
	if e != nil {
		t.Fatal(e)
	}
	a, e := rentalaccess.DecodeAnchor(raw, f.verified, f.expected)
	if e != nil || a != w.Snapshot() {
		t.Fatal("anchor metadata roundtrip", e)
	}
	for _, bad := range [][]byte{
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"requested_sequence":"1"}`)...),
		bytes.Replace(raw, []byte(`"requested_sequence":"1"`), []byte(`"requested_sequence":null`), 1),
		bytes.Replace(raw, []byte(`"requested_sequence":"1"`), []byte(`"Requested_sequence":"1"`), 1),
		bytes.Replace(raw, []byte(`"requested_sequence":"1"`), []byte(`"requested_sequence":"01"`), 1),
		bytes.Replace(raw, []byte(`"stopped":false`), []byte(`"stopped":null`), 1),
		bytes.Replace(raw, []byte(`,"stopped":false`), []byte(``), 1),
		append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"credential_once":"secret"}`)...),
	} {
		if bytes.Equal(raw, bad) {
			t.Fatal("fixture unchanged")
		}
		if _, e = rentalaccess.DecodeAnchor(bad, f.verified, f.expected); e == nil {
			t.Fatal("ambiguous durable watermark")
		}
	}
	current := f.expected
	current.Terms = "4"
	if _, e = rentalaccess.DecodeAnchor(raw, f.verified, current); e == nil {
		t.Fatal("old terms anchor reused")
	}
}
