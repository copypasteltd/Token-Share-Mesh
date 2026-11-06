package canonicaljson

import (
	"bytes"
	"testing"
)

func TestCanonicalUsesRFC8785NumbersAndUTF16Ordering(t *testing.T) {
	for _, v := range [][2]string{
		{`{"b":1.0,"a":"\u20ac","z":-0}`, `{"a":"€","b":1,"z":0}`},
		{`{"\ue000":1,"😀":2}`, `{"😀":2,"":1}`},
		{`{"s":"e\u0301","n":1e+09}`, `{"n":1000000000,"s":"é"}`},
	} {
		raw, e := Normalize([]byte(v[0]), 4096)
		if e != nil || string(raw) != v[1] {
			t.Fatal("reference canonicalization mismatch", e)
		}
	}
}
func TestCanonicalRejectsAmbiguousUnicodeKeysAndExcessiveDepth(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"a":1,"a":2}`), []byte(`{"a":1,"\u0061":2}`),
		[]byte(`{"x":"\ud800"}`), []byte(`{"x":"\udc00"}`),
		[]byte(`{"x":NaN}`), []byte(`{"x":1e999}`),
		append([]byte(`{"x":"`), []byte{255, '"', '}'}...),
		[]byte(string(bytes.Repeat([]byte("["), 34)) + string(bytes.Repeat([]byte("]"), 34))),
	} {
		if _, e := Normalize(raw, 4096); e == nil {
			t.Fatal("ambiguous canonical document accepted")
		}
	}
}
func TestCanonicalExactRejectsAliasesAndNonCanonicalWireBytes(t *testing.T) {
	type item struct {
		Value string `json:"value"`
	}
	for _, raw := range []string{`{ "value":"x"}`, `{"Value":"x"}`, `{"value":"x","unknown":1}`, `{"value":null}`} {
		var v item
		if DecodeExact([]byte(raw), &v, 4096) == nil {
			t.Fatal("noncanonical or ambiguous wire document accepted")
		}
	}
	var v item
	if DecodeExact([]byte(`{"value":"x"}`), &v, 4096) != nil {
		t.Fatal("canonical wire document rejected")
	}
}
