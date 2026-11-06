package integration_test

// SOURCE_ONLY: explicit native host composition and offline signed-archive
// verification. The default network transport is never invoked by this test.
import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p11.local/compute/internal/paymenthost"
	"p11.local/compute/shared/ids"
)

func originalRefundHostFiles(t *testing.T, r *originalRefundFixture, route bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	keys, e := machineKeys()
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{
		"sign.pem":   pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(keys.app)}),
		"alipay.pem": pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&keys.provider.PublicKey)}),
	}
	for name, raw := range files {
		if e = os.WriteFile(filepath.Join(root, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
		clear(raw)
	}
	for _, ring := range []struct {
		file, profile, id string
		value             byte
	}{{"proof.json", "P11_MACHINE_PAY_PROOF_KEYS_1", "machine_test_key", 31}, {"receipt.json", "P11_MACHINE_PAY_RECEIPT_KEYS_1", "machine_receipt_key", 32}} {
		raw, _ := json.Marshal(map[string]any{"profile_id": ring.profile, "active_key_id": ring.id, "keys": map[string]string{ring.id: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{ring.value}, 32))}})
		if e = os.WriteFile(filepath.Join(root, ring.file), raw, 0600); e != nil {
			t.Fatal(e)
		}
		clear(raw)
	}
	spec := map[string]any{"profile_id": "P11_MACHINE_PAY_HOST_1", "environment": "SANDBOX", "proof_keyring_file": "proof.json", "receipt_keyring_file": "receipt.json",
		"registrations": []any{map[string]any{"capability_ref": r.payment.capability, "binding": r.payment.binding, "policy": r.payment.policy, "signing_key_file": "sign.pem", "alipay_key_file": "alipay.pem"}}}
	if route {
		spec["original_refund_routes"] = []any{map[string]any{"refund_route_ref": r.routeRef, "original_capability_ref": r.payment.capability, "route": r.route}}
	}
	raw, _ := json.Marshal(spec)
	if e = os.WriteFile(filepath.Join(root, "host.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	clear(raw)
	return root, "host.json"
}
func TestOriginalRefundHostLoadsActualServiceAndRetainedOriginalArchiveKeys(t *testing.T) {
	r := refundIntegrationFixture(t)
	root, file := originalRefundHostFiles(t, r, true)
	host, e := paymenthost.Open(db, root, file, "SANDBOX")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if host.Close(closeCtx) != nil {
			t.Error("native original refund host not joined")
		}
	}()
	if host.Refunds() == nil || !host.Refunds().UsesMachine(host.Machine()) {
		t.Fatal("native host refund service not bound to original Machine")
	}
	in := r.decision(t, 200, 500)
	value, e := host.Refunds().Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
	if e != nil || value.State != "REQUESTED" {
		t.Fatal("actual host could not reverify and reserve original refund without network", e)
	}
	if r.submits.Load() != 0 || r.queries.Load() != 0 {
		t.Fatal("host initialization/request performed financial IO")
	}
}
func TestOriginalRefundHostDoesNotInstallImplicitRefundCapability(t *testing.T) {
	r := refundIntegrationFixture(t)
	root, file := originalRefundHostFiles(t, r, false)
	host, e := paymenthost.Open(db, root, file, "SANDBOX")
	if e != nil {
		t.Fatal(e)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer host.Close(closeCtx)
	if host.Refunds() != nil {
		t.Fatal("ordinary Machine host invented a refund route")
	}
}
