package core

// SOURCE_ONLY: the R026 code-first gate has not opened compilation/testing.
import (
	"bytes"
	"strings"
	"testing"
)

func TestInspectUsesExactNativeBodyWithoutPrivateFields(t *testing.T) {
	for _, tc := range []struct {
		p    Profile
		body string
	}{
		{ChatText, `{"model":"model_original","messages":[{"role":"user","content":"private synthetic"}],"max_tokens":8}`},
		{ResponsesText, `{"model":"model_original","input":"private synthetic","store":false,"max_output_tokens":8}`},
		{MessagesText, `{"model":"model_original","messages":[{"role":"user","content":"private synthetic"}],"max_tokens":8}`},
	} {
		b := []byte(tc.body)
		original := append([]byte(nil), b...)
		model, meta, err := Inspect(tc.p, b)
		if err != nil || model != "model_original" || meta.MaxOutputTokens != 8 || meta.Stream || !bytes.Equal(b, original) {
			t.Fatal(model, meta, err)
		}
		want, err := Preflight(tc.p, model, b)
		if err != nil || want != meta {
			t.Fatal("different admission/forwarding parser", err)
		}
	}
}
func TestInspectRejectsAmbiguousModelsAndNonNativeFields(t *testing.T) {
	good := `{"model":"model_original","messages":[{"role":"user","content":"synthetic"}],"max_tokens":8}`
	for _, body := range []string{
		strings.Replace(good, `"model":`, `"model":"model_other","model":`, 1),
		strings.Replace(good, `"model_original"`, `null`, 1),
		strings.Replace(good, `"model_original"`, `"\ud800"`, 1),
		strings.Replace(good, `"max_tokens":8`, `"max_tokens":8,"seller_id":"seller_other"`, 1),
		good + good, `[]`, strings.Repeat(" ", (256<<10)+1) + good,
	} {
		if _, _, err := Inspect(ChatText, []byte(body)); err == nil {
			t.Fatal("ambiguous/native boundary accepted")
		}
	}
}
