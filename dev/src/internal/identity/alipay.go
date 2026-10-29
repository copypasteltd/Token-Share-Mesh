package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/strictjson"
	"sort"
	"strings"
	"time"
)

// LoginProvider returns only an independently verified subject. Access and
// refresh tokens never become platform sessions or persistent user fields.
type LoginProvider interface {
	Name() string
	Exchange(context.Context, string) (string, error)
	AuthorizationURL(state string) (string, error)
}

type AlipayConfig struct {
	AppID, SubjectField, WebRedirect string
	PrivateKey                       *rsa.PrivateKey
	PublicKey                        *rsa.PublicKey
	HTTPClient                       *http.Client
}

type Alipay struct {
	appID, subjectField, webRedirect string
	privateKey                       *rsa.PrivateKey
	publicKey                        *rsa.PublicKey
	client                           *http.Client
}

func NewAlipay(c AlipayConfig) (*Alipay, error) {
	if len(c.AppID) < 8 || len(c.AppID) > 32 || (c.SubjectField != "OPEN_ID" && c.SubjectField != "USER_ID") ||
		c.PrivateKey == nil || c.PrivateKey.N == nil || c.PrivateKey.N.BitLen() < 2048 ||
		c.PublicKey == nil || c.PublicKey.N == nil || c.PublicKey.N.BitLen() < 2048 {
		return nil, fault.New(fault.Unavailable, "Alipay identity configuration required")
	}
	for _, r := range c.AppID {
		if r < '0' || r > '9' {
			return nil, fault.New(fault.Unavailable, "Invalid application identifier")
		}
	}
	if err := c.PrivateKey.Validate(); err != nil {
		return nil, fault.New(fault.Unavailable, "Invalid application signing key")
	}
	if c.WebRedirect != "" {
		u, err := url.Parse(c.WebRedirect)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" ||
			(u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			return nil, fault.New(fault.Unavailable, "Invalid registered authorization return URL")
		}
	}
	client := &http.Client{Timeout: 8 * time.Second}
	if c.HTTPClient != nil {
		*client = *c.HTTPClient
	}
	// Never forward a signed request or authorization code to a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout <= 0 || client.Timeout > 8*time.Second {
		client.Timeout = 8 * time.Second
	}
	return &Alipay{appID: c.AppID, subjectField: c.SubjectField, webRedirect: c.WebRedirect,
		privateKey: c.PrivateKey, publicKey: c.PublicKey, client: client}, nil
}

func (a *Alipay) Name() string { return "ALIPAY:" + a.appID + ":" + a.subjectField }

func (a *Alipay) AuthorizationURL(state string) (string, error) {
	if a.webRedirect == "" {
		return "", fault.New(fault.Unavailable, "Web authorization return URL not configured")
	}
	v := url.Values{"app_id": {a.appID}, "scope": {"auth_base"}, "redirect_uri": {a.webRedirect}, "state": {state}}
	return "https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?" + v.Encode(), nil
}

func oauthCodeValid(code string) bool {
	if len(code) < 1 || len(code) > 512 {
		return false
	}
	for _, r := range code {
		if r < 33 || r > 126 || strings.ContainsRune("&=", r) {
			return false
		}
	}
	return true
}

func canonicalForm(v url.Values) string {
	keys := make([]string, 0, len(v))
	for key := range v {
		if key != "sign" && v.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+v.Get(key))
	}
	return strings.Join(pairs, "&")
}

func (a *Alipay) Exchange(ctx context.Context, code string) (string, error) {
	if !oauthCodeValid(code) {
		return "", fault.New(fault.Invalid, "Invalid authorization code")
	}
	v := url.Values{"app_id": {a.appID}, "method": {"alipay.system.oauth.token"},
		"format": {"JSON"}, "charset": {"utf-8"}, "sign_type": {"RSA2"},
		"timestamp": {time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")},
		"version":   {"1.0"}, "grant_type": {"authorization_code"}, "code": {code}}
	digest := sha256.Sum256([]byte(canonicalForm(v)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fault.New(fault.Unavailable, "Identity signing unavailable")
	}
	v.Set("sign", base64.StdEncoding.EncodeToString(signature))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openapi.alipay.com/gateway.do", strings.NewReader(v.Encode()))
	if err != nil {
		return "", fault.New(fault.Unavailable, "Identity request unavailable")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return "", fault.New(fault.Unavailable, "Identity exchange outcome unknown; start fresh authorization")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fault.New(fault.Unavailable, "Identity exchange unavailable; start fresh authorization")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return "", fault.New(fault.Unavailable, "Invalid identity response")
	}
	var envelope map[string]json.RawMessage
	if strictjson.Decode(raw, &envelope, 65536) != nil || envelope == nil || envelope["error_response"] != nil {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	node := envelope["alipay_system_oauth_token_response"]
	var sign string
	if len(node) == 0 || node[0] != '{' || json.Unmarshal(envelope["sign"], &sign) != nil {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	decoded, err := base64.StdEncoding.DecodeString(sign)
	h := sha256.Sum256(node)
	if err != nil || rsa.VerifyPKCS1v15(a.publicKey, crypto.SHA256, h[:], decoded) != nil {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	// Do not re-serialize node before checking the signature: slash escapes,
	// whitespace and field order are part of the signed bytes.
	var fields map[string]json.RawMessage
	if strictjson.Decode(node, &fields, 65536) != nil {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	read := func(key string) string { var value string; _ = json.Unmarshal(fields[key], &value); return value }
	status := read("code")
	if fields["code"] != nil && status != "10000" {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	subject := read(strings.ToLower(a.subjectField))
	var seconds int64
	expiry := read("expires_in")
	if expiry != "" {
		for _, c := range expiry {
			if c < '0' || c > '9' || seconds > 31536000 {
				return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
			}
			seconds = seconds*10 + int64(c-'0')
		}
	}
	if subject == "" || len(subject) > 128 || read("access_token") == "" || seconds < 1 || seconds > 315360000 {
		return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	for _, r := range subject {
		if r < 33 || r > 126 {
			return "", fault.New(fault.Unauthenticated, "Authorization verification failed")
		}
	}
	return subject, nil
}
