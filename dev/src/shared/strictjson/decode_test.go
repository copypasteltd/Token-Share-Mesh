package strictjson_test

import (
	"p11.local/compute/shared/strictjson"
	"strings"
	"testing"
)

func TestControlJSONRejectsAmbiguityAndBounds(t *testing.T) {
	var target struct {
		Amount int64 `json:"amount"`
	}
	for _, raw := range []string{`{"amount":1,"amount":2}`, `{"amount":1} {}`, `{"amount":1,"extra":true}`, `{"amount":1.5}`, `[1,2]`, "{\"amount\":\"\xff\"}", strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)} {
		if strictjson.Decode([]byte(raw), &target, 512) == nil {
			t.Fatal("invalid control accepted")
		}
	}
	if strictjson.Decode([]byte(`{"amount":1}`), &target, 8) == nil {
		t.Fatal("size limit ignored")
	}
	if strictjson.Decode([]byte(`{"amount":1}`), &target, 512) != nil || target.Amount != 1 {
		t.Fatal("valid control rejected")
	}
	var nested map[string]any
	if strictjson.Decode([]byte(`{"a":[{"x":1,"x":2}]}`), &nested, 512) == nil {
		t.Fatal("nested duplicate accepted")
	}
}
