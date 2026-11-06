package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCipherCLIRejectsExportSecretAndACKActionsWithoutEcho(t *testing.T) {
	for _, action := range []string{"cipher-export", "cipher-accept", "cipher-status"} {
		var output, diagnostic bytes.Buffer
		if manageCipherCustody(context.Background(), action, []string{"--state", "missing", "--content-key", "SYNTHETIC_SECRET"}, strings.NewReader("{}"), &output, &diagnostic) == 0 || output.Len() != 0 || strings.Contains(diagnostic.String(), "SYNTHETIC_SECRET") {
			t.Fatal("custody metadata command accepted a secret or echoed it")
		}
	}
	var output, diagnostic bytes.Buffer
	if manageCipherCustody(context.Background(), "cipher-accept", []string{"--state", "missing"}, strings.NewReader(`{"ack":"SYNTHETIC_SECRET"}`), &output, &diagnostic) == 0 || output.Len() != 0 || strings.Contains(diagnostic.String(), "SYNTHETIC_SECRET") {
		t.Fatal("caller ACK was accepted")
	}
}

func TestCipherCLIRejectsInvalidPolicyBeforeOpeningOriginalState(t *testing.T) {
	for _, raw := range []string{`{"profile_id":"P11_ORIGINAL_CIPHER_CUSTODY_1","capacity_bytes":"1","maximum_objects":64}`, `{"profile_id":"one","profile_id":"two"}`, `{"private_key":"SYNTHETIC_SECRET"}`, `{}{}`, strings.Repeat(" ", 16385)} {
		var output, diagnostic bytes.Buffer
		if manageCipherCustody(context.Background(), "cipher-init", []string{"--state", "missing"}, strings.NewReader(raw), &output, &diagnostic) == 0 || output.Len() != 0 || strings.Contains(diagnostic.String(), "SYNTHETIC_SECRET") {
			t.Fatal("invalid custody policy accepted or echoed")
		}
	}
}
