package paymenthost

// SOURCE_ONLY. These definitions have not run; they do not prove qualification.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/internal/payments"
)

func TestPaymentHostPathsCannotEscapeExplicitRoot(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../outside", "/absolute", "C:/outside", "secret\\key", "a/../secret", "a//secret", "secret\x00key", "https://other/key"} {
		if relativeFile(name) {
			t.Fatal("unsafe payment path accepted")
		}
	}
	if !relativeFile("secrets/original-key.pem") {
		t.Fatal("canonical relative key path rejected")
	}
}
func TestPaymentHostReadsOnlyBoundedRegularOriginalFiles(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "key"), bytes.Repeat([]byte{1}, 33), 0600); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if raw, e := readFile(root, "key", 32); e == nil {
		clear(raw)
		t.Fatal("oversized secret accepted")
	}
	if raw, e := readFile(root, "../key", 100); e == nil {
		clear(raw)
		t.Fatal("outside secret accepted")
	}
	raw, e := readFile(root, "key", 33)
	if e != nil || len(raw) != 33 {
		t.Fatal("bounded original key unavailable")
	}
	clear(raw)
}
func TestPaymentHostKeyringRejectsDuplicateOrWrongProfile(t *testing.T) {
	dir := t.TempDir()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{11}, 32))
	for _, raw := range []string{
		`{"profile_id":"P11_MACHINE_PAY_RECEIPT_KEYS_1","active_key_id":"key_one","keys":{"key_one":"` + key + `"}}`,
		`{"profile_id":"P11_MACHINE_PAY_PROOF_KEYS_1","active_key_id":"key_one","keys":{"key_one":"` + key + `","key_one":"` + key + `"}}`,
		`{"profile_id":"P11_MACHINE_PAY_PROOF_KEYS_1","active_key_id":"key_one","keys":{"key_one":"` + key + `="}}`,
	} {
		if e = os.WriteFile(filepath.Join(dir, "ring.json"), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if v, e := readVault(root, "ring.json", "P11_MACHINE_PAY_PROOF_KEYS_1"); e == nil {
			v.Close()
			t.Fatal("ambiguous keyring accepted")
		}
	}
}
func TestPaymentHostAcceptsExplicitOldAndNewKeyEntries(t *testing.T) {
	dir := t.TempDir()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	spec := map[string]any{"profile_id": "P11_MACHINE_PAY_PROOF_KEYS_1", "active_key_id": "new_key", "keys": map[string]string{"old_key": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "new_key": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))}}
	raw, _ := json.Marshal(spec)
	if e = os.WriteFile(filepath.Join(dir, "ring.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	clear(raw)
	v, e := readVault(root, "ring.json", "P11_MACHINE_PAY_PROOF_KEYS_1")
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	// Old-key decryption itself is defined by the existing Vault tests. This
	// boundary only proves both explicitly retained entries are accepted.
}
func TestPaymentHostCloseDoesNotPretendWorkerJoined(t *testing.T) {
	life, cancel := context.WithCancel(context.Background())
	h := &Host{life: life, cancel: cancel, workerStarted: true, workerDone: make(chan struct{})}
	ctx, end := context.WithCancel(context.Background())
	end()
	if h.Close(ctx) == nil || h.closed {
		t.Fatal("unjoined worker released host ownership")
	}
	close(h.workerDone)
	if e := h.Close(context.Background()); e != nil || !h.closed {
		t.Fatal("joined original host could not close")
	}
}
func TestPaymentHostDiagnosticsDoNotRevealRegistry(t *testing.T) {
	h := &Host{capabilities: []string{"private_original_registration"}}
	raw, _ := json.Marshal(h)
	if strings.Contains(string(raw), "private_original_registration") || strings.Contains(fmt.Sprintf("%+v", h), "private_original_registration") {
		t.Fatal("registry escaped diagnostics")
	}
	if _, e := Open(nil, "not_absolute", "registry.json", "SANDBOX"); e == nil {
		t.Fatal("unconfigured host opened")
	}
	if e := h.RunRecovery(context.Background()); e == nil {
		t.Fatal("unconfigured worker started")
	}
	if e := h.Close(nil); e != payments.ErrUnknown {
		t.Fatal("nil close scope accepted")
	}
}
