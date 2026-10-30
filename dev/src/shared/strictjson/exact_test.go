package strictjson

import "testing"

func TestControlJSONRequiresExactDeclaredKeys(t *testing.T) {
	var input struct {
		ID     string `json:"listing_id"`
		Nested struct {
			Budget int64 `json:"budget_fen"`
		} `json:"policy"`
	}
	for _, raw := range []string{
		`{"Listing_ID":"changed"}`,
		`{"listing_id":"first","Listing_ID":"second"}`,
		`{"policy":{"Budget_Fen":42}}`,
		`{"listing_id":null}`,
		`{"policy":{"budget_fen":null}}`,
		"null",
		"[]",
	} {
		if Decode([]byte(raw), &input, 4096) == nil {
			t.Fatal("ambiguous or non-object control DTO accepted")
		}
	}
	if Decode([]byte(`{"listing_id":"correct","policy":{"budget_fen":42}}`), &input, 4096) != nil || input.Nested.Budget != 42 {
		t.Fatal("exact control DTO rejected")
	}
}
