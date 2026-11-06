package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDirectoryCLIRejectsUnknownActionsAndDoesNotEchoCallerSecrets(t *testing.T) {
	for _, action := range []string{"directory-plaintext", "directory-publish", "directory-revoke", "directory-get", "directory-list", "directory-recover"} {
		var out, diagnostic bytes.Buffer
		code := manageEndpointDirectory(context.Background(), action, []string{"--credential=SECRET_NEVER_ECHO"}, strings.NewReader("{}"), &out, &diagnostic)
		if code == 0 || out.Len() != 0 || strings.Contains(diagnostic.String(), "SECRET_NEVER_ECHO") {
			t.Fatal("invalid directory CLI leaked input or succeeded")
		}
	}
}
