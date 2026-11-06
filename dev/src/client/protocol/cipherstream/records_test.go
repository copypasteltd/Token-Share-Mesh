package cipherstream

import (
	"p11.local/compute/shared/canonicaljson"
	"testing"
)

func TestCipherRecordOrderAndUsageCannotClaimTrustedMetering(t *testing.T) {
	c := Context{Purpose: "OUTPUT"}
	h, _ := canonicaljson.Encode(PrivateHeaders{Profile: "P11_PRIVATE_HEADERS_1", Status: 200, ContentType: "text/event-stream"}, 4096)
	s := sequenceState{}
	if _, e := s.accept(c, DataRecord, []byte("early"), false); e == nil {
		t.Fatal("data before headers accepted")
	}
	if _, e := s.accept(c, HeadersRecord, h, false); e != nil {
		t.Fatal(e)
	}
	u, _ := canonicaljson.Encode(PrivateUsage{Profile: "P11_PRIVATE_USAGE_1", InputTokens: "1", OutputTokens: "2", Source: "APPROVED_METER", Evidence: "evidence_claimed"}, 4096)
	if _, e := s.accept(c, UsageRecord, u, false); e == nil {
		t.Fatal("transport promoted seller report to trusted usage")
	}
	if _, e := s.accept(c, HeadersRecord, h, false); e == nil {
		t.Fatal("duplicate headers accepted")
	}
}
func TestCipherRecordFinalTagMustBeTheBusinessEND(t *testing.T) {
	c := Context{Purpose: "OUTPUT"}
	s := sequenceState{headers: true}
	if _, e := s.accept(c, DataRecord, []byte("data"), true); e == nil {
		t.Fatal("FINAL without END accepted")
	}
	end, _ := canonicaljson.Encode(End{Profile: "P11_PRIVATE_END_1", Outcome: "SUCCESS", Records: "1", DataRecords: "0", DataBytes: "0", DataHash: Hash(nil)}, 4096)
	if _, e := s.accept(c, EndRecord, end, false); e == nil {
		t.Fatal("END without FINAL accepted")
	}
	if _, e := s.accept(c, EndRecord, end, true); e != nil {
		t.Fatal(e)
	}
	if _, e := s.accept(c, DataRecord, []byte("after"), false); e == nil {
		t.Fatal("record after END accepted")
	}
}
