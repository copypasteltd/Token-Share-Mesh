package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComfyCLIRejectsAmbiguousOwnerOptionsBeforeOpeningNode(t *testing.T) {
	for _, test := range []struct {
		action string
		args   []string
	}{
		{"comfy-run", []string{"--state", "never_open"}},
		{"comfy-init", []string{"--state", "never_open", "--digest", strings.Repeat("a", 64)}},
		{"comfy-install", []string{"--state", "never_open"}},
		{"comfy-install", []string{"--state", "never_open", "--trust", "network_request_not_local_consent"}},
		{"comfy-delete", []string{"--state", "never_open"}},
		{"comfy-status", []string{"--state", "never_open", "--consent", "unexpected"}},
	} {
		var output, diagnostic bytes.Buffer
		if manageComfy(context.Background(), test.action, test.args, strings.NewReader("BUYER_CONTENT_MUST_NOT_BE_READ"), &output, &diagnostic) == 0 || output.Len() != 0 {
			t.Fatal("invalid owner operation reached node state")
		}
		for _, text := range []string{"never_open", "network_request_not_local_consent", "BUYER_CONTENT"} {
			if strings.Contains(diagnostic.String(), text) {
				t.Fatal("arguments/content leaked in diagnostics")
			}
		}
	}
}
func TestComfyOwnerFilesStrictBoundedAndSelectedByAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "trust.json")
	valid := []byte(`{"issuer_id":"issuer_synthetic","ed25519_pins":{},"revoked_catalog_digests":[]}`)
	if e := os.WriteFile(name, valid, 0600); e != nil {
		t.Fatal(e)
	}
	var trust comfyTrust
	if readComfyOwnerFile(name, &trust) != nil || trust.Issuer != "issuer_synthetic" {
		t.Fatal("explicit local trust fixture not readable")
	}
	if readComfyOwnerFile("trust.json", &trust) == nil {
		t.Fatal("relative trust path accepted")
	}
	for _, b := range [][]byte{[]byte(`{"issuer_id":"a","issuer_id":"b","ed25519_pins":{},"revoked_catalog_digests":[]}`), []byte(`{"issuer_id":"a","unknown":"network-pins"}`), bytes.Repeat([]byte("x"), 65537)} {
		if e := os.WriteFile(name, b, 0600); e != nil {
			t.Fatal(e)
		}
		if readComfyOwnerFile(name, &trust) == nil {
			t.Fatal("ambiguous/unbounded local trust file accepted")
		}
	}
}
