package rentalaccess_test

import (
	"encoding/json"
	"math"
	"testing"

	"p11.local/compute/client/protocol/rentalaccess"
)

func TestRentalControlSQLTransportRejectsCounterAliasesAndOverflow(t *testing.T) {
	f := fixture(t)
	in := rentalaccess.ControlRequest{ExpectedVersion: math.MaxInt64, Grant: f.claims.ID, Digest: f.metadata.Digest, Generation: f.claims.Generation, Terms: f.claims.Terms, TermsHash: f.claims.TermsHash, Sequence: "9223372036854775807"}
	if n, err := in.SequenceNumber(); err != nil || n != math.MaxInt64 {
		t.Fatal("valid final SQL sequence/version rejected", err)
	}
	for _, seq := range []string{"", "0", "00", "01", "+1", "-1", "1.0", "1e1", " 1", "9223372036854775808", "18446744073709551615"} {
		bad := in
		bad.Sequence = seq
		if _, err := bad.SequenceNumber(); err == nil {
			t.Fatalf("counter alias/overflow accepted: %q", seq)
		}
	}
	for _, field := range []string{"generation", "terms", "digest"} {
		bad := in
		switch field {
		case "generation":
			bad.Generation = "9223372036854775808"
		case "terms":
			bad.Terms = "01"
		case "digest":
			bad.Digest = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}
		if _, err := bad.SequenceNumber(); err == nil {
			t.Fatal("invalid original control binding accepted", field)
		}
	}
}

func TestRentalStoredAnchorCannotOmitStopOrChangeOriginalContext(t *testing.T) {
	f := fixture(t)
	a := rentalaccess.Anchor{Profile: rentalaccess.AnchorProfile, GrantDigest: f.expected.TokenDigest, Session: f.claims.Session, Generation: f.claims.Generation, Terms: f.claims.Terms, Requested: "1", Accepted: "0", Stopped: true}
	raw, _ := json.Marshal(a)
	if got, err := rentalaccess.DecodeStoredAnchor(raw, f.expected); err != nil || !got.Stopped {
		t.Fatal("valid stopped anchor rejected", err)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	delete(fields, "stopped")
	missing, _ := json.Marshal(fields)
	if _, err := rentalaccess.DecodeStoredAnchor(missing, f.expected); err == nil {
		t.Fatal("omitted stop tombstone became false")
	}
	wrong := f.expected
	wrong.Generation = "13"
	if _, err := rentalaccess.DecodeStoredAnchor(raw, wrong); err == nil {
		t.Fatal("foreign original generation accepted")
	}
	fields["stopped"] = json.RawMessage("null")
	ambiguous, _ := json.Marshal(fields)
	if _, err := rentalaccess.DecodeStoredAnchor(ambiguous, f.expected); err == nil {
		t.Fatal("null stop tombstone became false")
	}
}
