package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestEndpointCLIRejectsUnknownActionsAndSecretFlagsWithoutEcho(t *testing.T) {
	for _, action := range []string{"crypto-private-export", "crypto-public"} {
		var output, diagnostic bytes.Buffer
		if manageCrypto(context.Background(), action, []string{"--state", "missing", "--private-key", "SYNTHETIC_SECRET"}, strings.NewReader("{}"), &output, &diagnostic) == 0 || output.Len() != 0 || strings.Contains(diagnostic.String(), "SYNTHETIC_SECRET") {
			t.Fatal("key command accepted secret option or echoed it")
		}
	}
}
func TestEndpointCLIStrictInputRejectsUnknownDuplicateAndOversize(t *testing.T) {
	for _, raw := range []string{`{"owner_id":"one","owner_id":"two"}`, `{"private_key":"forbidden"}`, `{"role":"BUYER"}{}`, strings.Repeat(" ", 16385)} {
		var in cryptoCreate
		if decodeCrypto(strings.NewReader(raw), &in) == nil {
			t.Fatal("invalid key command accepted")
		}
	}
}
