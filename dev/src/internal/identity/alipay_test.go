package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func signedNode(t *testing.T, key *rsa.PrivateKey, node string) string {
	t.Helper()
	hash := sha256.Sum256([]byte(node))
	sign, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"alipay_system_oauth_token_response":%s,"sign":%q}`, node, base64.StdEncoding.EncodeToString(sign))
}
func identityResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAlipayVerifiesOriginalBytesAndSignsApplicationRequest(t *testing.T) {
	app, provider := rsaKey(t), rsaKey(t)
	node := `{ "open_id":"synthetic_open_A", "user_id":"synthetic_user_A", "access_token":"TEST_ONLY_NOT_A_PLATFORM_CREDENTIAL", "expires_in":"3600", "extension":"https:\/\/unit.invalid\/a" }`
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.String() != "https://openapi.alipay.com/gateway.do" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded; charset=utf-8" {
			t.Fatal("wrong fixed request target")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("app_id") != "2026000000000001" || form.Get("code") != "fresh-code" || form.Get("grant_type") != "authorization_code" {
			t.Fatal("wrong application or code binding")
		}
		// Independent fixed-field canonical vector, not the production helper.
		canonical := "app_id=2026000000000001&charset=utf-8&code=fresh-code&format=JSON&grant_type=authorization_code&method=alipay.system.oauth.token&sign_type=RSA2&timestamp=" + form.Get("timestamp") + "&version=1.0"
		hash := sha256.Sum256([]byte(canonical))
		sign, err := base64.StdEncoding.DecodeString(form.Get("sign"))
		if err != nil || rsa.VerifyPKCS1v15(&app.PublicKey, crypto.SHA256, hash[:], sign) != nil {
			t.Fatal("application request signature invalid")
		}
		return identityResponse(signedNode(t, provider, node)), nil
	})}
	for _, field := range []string{"OPEN_ID", "USER_ID"} {
		adapter, err := NewAlipay(AlipayConfig{AppID: "2026000000000001", SubjectField: field, PrivateKey: app, PublicKey: &provider.PublicKey, HTTPClient: client})
		if err != nil {
			t.Fatal(err)
		}
		subject, err := adapter.Exchange(context.Background(), "fresh-code")
		if err != nil || subject != map[string]string{"OPEN_ID": "synthetic_open_A", "USER_ID": "synthetic_user_A"}[field] {
			t.Fatal("verified subject mismatch", err)
		}
	}
}
func TestAlipayRejectsUnsignedTamperedOrAmbiguousIdentityResponses(t *testing.T) {
	app, provider := rsaKey(t), rsaKey(t)
	node := `{"open_id":"synthetic_A","access_token":"TEST_ONLY","expires_in":"3600"}`
	valid := signedNode(t, provider, node)
	cases := []string{
		`{"alipay_system_oauth_token_response":` + node + `}`,
		strings.Replace(valid, "synthetic_A", "synthetic_B", 1),
		`{"alipay_system_oauth_token_response":` + node + `,"alipay_system_oauth_token_response":` + node + `,"sign":"invalid"}`,
		strings.TrimSuffix(valid, "}") + `,"error_response":{"code":"40004"}}`,
		signedNode(t, provider, `{"user_id":"no-open-id-fallback","access_token":"TEST_ONLY","expires_in":"3600"}`),
		signedNode(t, provider, `{"open_id":"synthetic_A","access_token":"TEST_ONLY","expires_in":"-1"}`),
		signedNode(t, provider, `{"open_id":"synthetic_A","access_token":"TEST_ONLY","expires_in":3600}`),
		signedNode(t, provider, `{"open_id":"synthetic_A","access_token":"TEST_ONLY","expires_in":"3600","code":"40004"}`),
		signedNode(t, provider, `{"open_id":"synthetic_A","open_id":"synthetic_B","access_token":"TEST_ONLY","expires_in":"3600"}`),
		strings.Repeat("x", 65537),
		"null",
	}
	for _, body := range cases {
		adapter, err := NewAlipay(AlipayConfig{AppID: "2026000000000001", SubjectField: "OPEN_ID", PrivateKey: app, PublicKey: &provider.PublicKey,
			HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return identityResponse(body), nil })}})
		if err != nil {
			t.Fatal(err)
		}
		if subject, err := adapter.Exchange(context.Background(), "fresh-code"); err == nil || subject != "" {
			t.Fatal("invalid response issued identity")
		}
	}
}
func TestAlipayRejectsIncompleteConfigurationAndNeverFollowsRedirects(t *testing.T) {
	app, provider := rsaKey(t), rsaKey(t)
	base := AlipayConfig{AppID: "2026000000000001", SubjectField: "OPEN_ID", PrivateKey: app, PublicKey: &provider.PublicKey}
	cases := []AlipayConfig{base, base, base, base}
	cases[0].SubjectField = ""
	cases[1].PrivateKey = nil
	cases[2].PublicKey = nil
	cases[3].WebRedirect = "https://safe.invalid/callback?next=https://other.invalid"
	for _, c := range cases {
		if _, err := NewAlipay(c); err == nil {
			t.Fatal("incomplete identity configuration accepted")
		}
	}
	calls := 0
	base.HTTPClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "openapi.alipay.com" {
			t.Fatal("authorization code followed redirect")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://other.invalid/steal"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	adapter, err := NewAlipay(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Exchange(context.Background(), "fresh-code"); err == nil || calls != 1 {
		t.Fatal("redirect not rejected")
	}
}
