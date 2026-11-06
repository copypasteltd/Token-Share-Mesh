package rentalaccess_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
	relay "p11.local/compute/gateway/core"
)

func TestRentalTextEnvelopeAuthenticatesAllThreeOriginalProtocols(t *testing.T) {
	for _, tc := range []struct {
		profile relay.Profile
		raw     string
	}{
		{relay.ChatText, rentaltest.Body},
		{relay.ResponsesText, `{"model":"public_model","input":"private rental fixture","max_output_tokens":32,"store":false,"stream":true}`},
		{relay.MessagesText, `{"model":"public_model","messages":[{"role":"user","content":[{"type":"text","text":"private rental fixture"}]}],"max_tokens":32,"stream":true}`},
	} {
		t.Run(string(tc.profile), func(t *testing.T) {
			f := rentaltest.Text(t, tc.profile)
			raw := []byte(tc.raw)
			i, e := rentalaccess.SealTextInput(f.Prepared, f.Verifier, f.Credential, f.Expected, "invocation_text", "1", f.Policy, raw, f.Now)
			if e != nil {
				t.Fatal(e)
			}
			defer i.Close()
			d := i.Descriptor()
			plain := sha256.Sum256(raw)
			if d.InputCiphertextDigest == hex.EncodeToString(plain[:]) || d.Session != f.Expected.Session || d.GrantDigest != f.Expected.TokenDigest || d.Spec.Protocol != tc.profile {
				t.Fatal("cipher/authority binding lost")
			}
			j, e := rentalaccess.SealTextInput(f.Prepared, f.Verifier, f.Credential, f.Expected, "invocation_text", "1", f.Policy, raw, f.Now)
			if e != nil {
				t.Fatal(e)
			}
			defer j.Close()
			if j.Descriptor().InputCiphertextDigest == d.InputCiphertextDigest {
				t.Fatal("reused nonce/key or plaintext fingerprint")
			}
			for _, v := range []any{i, *i} {
				b, e := json.Marshal(v)
				if e != nil || bytes.Contains(b, []byte("private rental fixture")) || strings.Contains(fmt.Sprintf("%+v %#v", v, v), f.Credential) {
					t.Fatal("input formatting exposed content/key material", e)
				}
			}
			got, e := i.Take(context.Background(), f.Prepared, f.Expected, f.Now)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatal("original body changed", e)
			}
			clear(got)
			if got, e = i.Take(context.Background(), f.Prepared, f.Expected, f.Now); e == nil || len(got) != 0 {
				clear(got)
				t.Fatal("input consumed twice")
			}
			if i.Descriptor() != d {
				t.Fatal("query metadata changed after key destruction")
			}
		})
	}
}

func TestRentalTextEnvelopeRejectsSelfApprovedAuthorityAndUnsupportedSemantics(t *testing.T) {
	f := rentaltest.Text(t, relay.ChatText)
	for _, tc := range []struct {
		name                    string
		expected                rentalaccess.Expected
		policy                  rentalaccess.TextPolicy
		body, credential, local string
	}{
		{"wrong digest", func() rentalaccess.Expected { e := f.Expected; e.TokenDigest = strings.Repeat("0", 64); return e }(), f.Policy, rentaltest.Body, f.Credential, "1"},
		{"wrong seller", func() rentalaccess.Expected { e := f.Expected; e.Seller = "seller_other"; return e }(), f.Policy, rentaltest.Body, f.Credential, "1"},
		{"wrong model mapping", f.Expected, func() rentalaccess.TextPolicy { p := f.Policy; p.ModelRef = "model_other"; return p }(), rentaltest.Body, f.Credential, "1"},
		{"unapproved protocol", f.Expected, func() rentalaccess.TextPolicy { p := f.Policy; p.Protocol = relay.MessagesText; return p }(), rentaltest.Body, f.Credential, "1"},
		{"requested model override", f.Expected, f.Policy, strings.Replace(rentaltest.Body, "public_model", "other_model", 1), f.Credential, "1"},
		{"unsupported tools", f.Expected, f.Policy, strings.Replace(rentaltest.Body, `"max_tokens":32`, `"tools":[],"max_tokens":32`, 1), f.Credential, "1"},
		{"output over approved ceiling", f.Expected, f.Policy, strings.Replace(rentaltest.Body, `"max_tokens":32`, `"max_tokens":33`, 1), f.Credential, "1"},
		{"missing bearer", f.Expected, f.Policy, rentaltest.Body, "", "1"},
		{"noncanonical local fence", f.Expected, f.Policy, rentaltest.Body, f.Credential, "01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, e := rentalaccess.SealTextInput(f.Prepared, f.Verifier, tc.credential, tc.expected, "invocation_text", tc.local, tc.policy, []byte(tc.body), f.Now)
			if i != nil {
				i.Close()
			}
			if e == nil {
				t.Fatal("unapproved input accepted")
			}
		})
	}
	t.Run("valid current grant without INVOKE scope", func(t *testing.T) {
		claims := f.Metadata.Claims
		claims.Scopes = []string{"CANCEL_SELF"}
		issued, e := f.Signer.Issue(claims, f.Now)
		if e != nil {
			t.Fatal(e)
		}
		defer issued.Close()
		expected := f.Expected
		expected.TokenDigest = issued.Metadata().Digest
		credential, e := issued.TakeCredential()
		if e != nil {
			t.Fatal(e)
		}
		i, e := rentalaccess.SealTextInput(f.Prepared, f.Verifier, credential, expected, "invocation_no_scope", "1", f.Policy, []byte(rentaltest.Body), f.Now)
		if i != nil {
			i.Close()
		}
		if e == nil {
			t.Fatal("valid original credential elevated its scopes")
		}
	})
}

func TestRentalTextEnvelopeCloseInvalidatesCopiesAndExpiredGrant(t *testing.T) {
	f := rentaltest.Text(t, relay.ChatText)
	i := f.Input(t, "invocation_text", "1")
	alias := *i
	alias.Close()
	if raw, e := i.Take(context.Background(), f.Prepared, f.Expected, time.Now()); e == nil {
		clear(raw)
		t.Fatal("value copy reopened destroyed keys")
	}
	j := f.Input(t, "invocation_late", "1")
	if raw, e := j.Take(context.Background(), f.Prepared, f.Expected, f.Now.Add(3*time.Minute)); e == nil {
		clear(raw)
		t.Fatal("paid hard deadline bypassed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, e := j.Take(ctx, f.Prepared, f.Expected, f.Now); e == nil {
		clear(raw)
		t.Fatal("cancelled input opened")
	}
	j.Close()
}
