package payments

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"p11.local/compute/shared/strictjson"
)

const precreateMethod = "alipay.aipay.agent.payment.precreate"
const verifyMethod = "alipay.aipay.agent.payment.verify"

type AlipayConfig struct {
	Binding         Binding
	PrivateKey      *rsa.PrivateKey
	AlipayPublicKey *rsa.PublicKey
	// AppAuthToken is obtained through the exact independently approved
	// AppAuthTokenRef. It is never accepted from payment/model request JSON.
	AppAuthToken []byte
	Transport    http.RoundTripper
	Now          func() time.Time
}

func (c AlipayConfig) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[Machine Pay configuration redacted]")
}
func (c AlipayConfig) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

type Alipay struct {
	mu             sync.Mutex
	binding        Binding
	private        *rsa.PrivateKey
	public         *rsa.PublicKey
	authorization  []byte
	client         *http.Client
	ownedTransport *http.Transport
	gateway        string
	now            func() time.Time
	life           context.Context
	cancel         context.CancelFunc
	operations     sync.WaitGroup
	closed         bool
	done           chan struct{}
}

func (a *Alipay) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[Machine Pay channel redacted]")
}
func (a *Alipay) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func PublicKeyHash(key *rsa.PublicKey) (string, error) {
	if key == nil || key.N == nil || key.N.Sign() <= 0 || key.N.BitLen() < 2048 || key.N.BitLen() > 4096 || key.E < 3 || key.E%2 == 0 {
		return "", ErrBinding
	}
	sum := sha256.Sum256(x509.MarshalPKCS1PublicKey(key))
	return hex.EncodeToString(sum[:]), nil
}

func NewAlipay(c AlipayConfig) (*Alipay, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	if !c.Binding.ResolvableAt(c.Now()) || c.PrivateKey == nil || c.PrivateKey.N == nil || c.PrivateKey.N.BitLen() < 2048 || c.PrivateKey.N.BitLen() > 4096 || c.PrivateKey.Validate() != nil ||
		c.AlipayPublicKey == nil || c.AlipayPublicKey.N == nil || c.AlipayPublicKey.N.BitLen() < 2048 || c.AlipayPublicKey.N.BitLen() > 4096 || c.AlipayPublicKey.E < 3 || c.AlipayPublicKey.E%2 == 0 {
		return nil, ErrBinding
	}
	signingHash, e := PublicKeyHash(&c.PrivateKey.PublicKey)
	if e != nil || signingHash != c.Binding.SigningKeyHash {
		return nil, ErrBinding
	}
	alipayHash, e := PublicKeyHash(c.AlipayPublicKey)
	if e != nil || alipayHash != c.Binding.AlipayKeyHash {
		return nil, ErrBinding
	}
	if c.Binding.ThirdParty {
		if len(c.AppAuthToken) < 1 || len(c.AppAuthToken) > 4096 {
			return nil, ErrBinding
		}
		for _, b := range c.AppAuthToken {
			if b < 33 || b > 126 {
				return nil, ErrBinding
			}
		}
	} else if len(c.AppAuthToken) != 0 {
		return nil, ErrBinding
	}
	// Copy the actual cryptographic keys; later caller mutation cannot silently
	// replace the application or Alipay verification identity of this channel.
	der := x509.MarshalPKCS1PrivateKey(c.PrivateKey)
	defer clear(der)
	private, e := x509.ParsePKCS1PrivateKey(der)
	if e != nil {
		return nil, ErrBinding
	}
	public, e := x509.ParsePKCS1PublicKey(x509.MarshalPKCS1PublicKey(c.AlipayPublicKey))
	if e != nil {
		return nil, ErrBinding
	}
	var owned *http.Transport
	transport := c.Transport
	if transport == nil {
		owned = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
			MaxResponseHeaderBytes: 16384, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, DisableCompression: true}
		transport = owned
	}
	gateway := "https://openapi.alipay.com/gateway.do"
	if c.Binding.Environment == "SANDBOX" {
		gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	}
	life, cancel := context.WithCancel(context.Background())
	return &Alipay{binding: c.Binding, private: private, public: public, authorization: append([]byte{}, c.AppAuthToken...),
		client:         &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		ownedTransport: owned, gateway: gateway, now: c.Now, life: life, cancel: cancel, done: make(chan struct{})}, nil
}
func (a *Alipay) Close(ctx context.Context) error {
	if a == nil || ctx == nil {
		return ErrBinding
	}
	a.mu.Lock()
	first := !a.closed
	if first {
		a.closed = true
		a.cancel()
	}
	a.mu.Unlock()
	if first {
		go func() {
			a.operations.Wait()
			if a.ownedTransport != nil {
				a.ownedTransport.CloseIdleConnections()
			}
			a.mu.Lock()
			clear(a.authorization)
			a.authorization = nil
			a.private = nil
			a.public = nil
			a.mu.Unlock()
			close(a.done)
		}()
	}
	select {
	case <-ctx.Done():
		return ErrUnknown
	case <-a.done:
		return nil
	}
}
func canonical(v url.Values) string {
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
func (a *Alipay) unknown(i Intent, code string) Observation {
	return Observation{scope: i.Scope, amount: i.Amount, kind: "UNRESOLVED", safeCode: code, observed: a.now().UTC()}
}

func (a *Alipay) call(ctx context.Context, method string, biz any) (json.RawMessage, string, error) {
	if a == nil || ctx == nil || ctx.Err() != nil || method != precreateMethod && method != verifyMethod {
		return nil, "LOCAL_ADMISSION_CLOSED", ErrBinding
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, "LOCAL_ADMISSION_CLOSED", ErrBinding
	}
	a.operations.Add(1)
	a.mu.Unlock()
	defer a.operations.Done()
	bounded, end := context.WithTimeout(ctx, 8*time.Second)
	defer end()
	stop := context.AfterFunc(a.life, end)
	defer stop()
	raw, e := json.Marshal(biz)
	if e != nil {
		return nil, "LOCAL_ADMISSION_CLOSED", ErrIntent
	}
	defer clear(raw)
	form := url.Values{"app_id": {a.binding.AppID}, "method": {method}, "format": {"JSON"}, "charset": {"utf-8"}, "sign_type": {"RSA2"},
		"timestamp": {a.now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")}, "version": {"1.0"}, "biz_content": {string(raw)}}
	if a.binding.ThirdParty {
		form.Set("app_auth_token", string(a.authorization))
	}
	digest := sha256.Sum256([]byte(canonical(form)))
	signature, e := rsa.SignPKCS1v15(rand.Reader, a.private, crypto.SHA256, digest[:])
	if e != nil {
		return nil, "LOCAL_SIGNING_UNAVAILABLE", ErrUnknown
	}
	defer clear(signature)
	form.Set("sign", base64.StdEncoding.EncodeToString(signature))
	request, e := http.NewRequestWithContext(bounded, http.MethodPost, a.gateway, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, "LOCAL_ADMISSION_CLOSED", ErrIntent
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, e := a.client.Do(request)
	if e != nil {
		return nil, "TRANSPORT_UNKNOWN", ErrUnknown
	}
	if response == nil || response.Body == nil {
		return nil, "TRANSPORT_UNKNOWN", ErrUnknown
	}
	var bodyClose sync.Once
	closeBody := func() { bodyClose.Do(func() { _ = response.Body.Close() }) }
	defer closeBody()
	// Own and join the actual body-close cancellation callback. A transport
	// that ignores cancellation remains owned; Close cannot report joined.
	joined := make(chan struct{})
	stopBody := context.AfterFunc(bounded, func() { defer close(joined); closeBody() })
	defer func() {
		if !stopBody() {
			<-joined
		}
	}()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, "TRANSPORT_UNKNOWN", ErrUnknown
	}
	encoded, e := io.ReadAll(io.LimitReader(response.Body, MaximumChannelResponse+1))
	defer clear(encoded)
	if e != nil || len(encoded) > MaximumChannelResponse || bounded.Err() != nil {
		return nil, "TRANSPORT_UNKNOWN", ErrUnknown
	}
	var envelope map[string]json.RawMessage
	if strictjson.Decode(encoded, &envelope, MaximumChannelResponse) != nil || len(envelope) != 2 {
		return nil, "SIGNATURE_UNVERIFIED", ErrUnknown
	}
	node := envelope[strings.ReplaceAll(method, ".", "_")+"_response"]
	isError := node == nil
	if node == nil {
		node = envelope["error_response"]
	}
	var sign string
	if len(node) < 2 || node[0] != '{' || json.Unmarshal(envelope["sign"], &sign) != nil {
		return nil, "SIGNATURE_UNVERIFIED", ErrUnknown
	}
	signed, e := base64.StdEncoding.Strict().DecodeString(sign)
	defer clear(signed)
	sum := sha256.Sum256(node)
	if e != nil || rsa.VerifyPKCS1v15(a.public, crypto.SHA256, sum[:], signed) != nil {
		return nil, "SIGNATURE_UNVERIFIED", ErrUnknown
	}
	if isError {
		return nil, "SIGNED_BUSINESS_UNRESOLVED", ErrUnknown
	}
	var fields map[string]json.RawMessage
	if strictjson.Decode(node, &fields, MaximumChannelResponse) != nil {
		return nil, "SIGNATURE_UNVERIFIED", ErrUnknown
	}
	var code string
	if json.Unmarshal(fields["code"], &code) != nil || code != "10000" {
		return nil, "SIGNED_BUSINESS_UNRESOLVED", ErrUnknown
	}
	return append([]byte{}, node...), "", nil
}

func (a *Alipay) Precreate(ctx context.Context, i Intent) (Observation, error) {
	if a == nil {
		return Observation{}, ErrBinding
	}
	if !i.ValidAt(a.binding, a.now()) {
		return a.unknown(i, "LOCAL_ADMISSION_CLOSED"), ErrIntent
	}
	yuan, _ := Yuan(i.Amount)
	biz := struct {
		PayBefore string `json:"pay_before"`
		Amount    string `json:"amount"`
		Goods     string `json:"goods_name"`
		Service   string `json:"service_id"`
		Number    string `json:"out_trade_no"`
		Currency  string `json:"currency"`
	}{i.PayBefore.UTC().Format(time.RFC3339Nano), yuan, i.GoodsName, a.binding.ServiceID, i.Scope.OutTradeNo, "CNY"}
	node, code, e := a.call(ctx, precreateMethod, biz)
	if e != nil {
		return a.unknown(i, code), e
	}
	defer clear(node)
	var fields map[string]json.RawMessage
	_ = strictjson.Decode(node, &fields, MaximumChannelResponse)
	var prepay string
	if json.Unmarshal(fields["amt_pre_pay_id"], &prepay) != nil || !merchantNumber.MatchString(prepay) {
		return a.unknown(i, "SIGNED_RESPONSE_MISMATCH"), ErrUnknown
	}
	sum := sha256.Sum256(node)
	return Observation{scope: i.Scope, amount: i.Amount, kind: "PRECREATED", prepay: prepay, evidenceHash: hex.EncodeToString(sum[:]), observed: a.now().UTC()}, nil
}
func (a *Alipay) Verify(ctx context.Context, i Intent, p *Proof) (Observation, error) {
	if a == nil {
		return Observation{}, ErrBinding
	}
	if !i.BoundTo(a.binding) || !a.binding.ResolvableAt(a.now()) || a.now().Before(i.CreatedAt) {
		return a.unknown(i, "LOCAL_RESOLUTION_CLOSED"), ErrBinding
	}
	v, e := p.payload()
	if e != nil {
		return a.unknown(i, "PROOF_UNAVAILABLE"), ErrProof
	}
	clientSession := ""
	if v.Method != nil {
		clientSession = v.Method.ClientSession
	}
	biz := struct {
		Trade   string `json:"trade_no"`
		Proof   string `json:"payment_proof"`
		Session string `json:"client_session,omitempty"`
	}{v.Protocol.TradeNo, v.Protocol.PaymentProof, clientSession}
	node, code, e := a.call(ctx, verifyMethod, biz)
	if e != nil {
		return a.unknown(i, code), e
	}
	defer clear(node)
	var fields map[string]json.RawMessage
	_ = strictjson.Decode(node, &fields, MaximumChannelResponse)
	var trade, amount, number string
	var active bool
	if json.Unmarshal(fields["trade_no"], &trade) != nil || json.Unmarshal(fields["amount"], &amount) != nil || json.Unmarshal(fields["out_trade_no"], &number) != nil ||
		json.Unmarshal(fields["active"], &active) != nil || !active || trade != v.Protocol.TradeNo || number != i.Scope.OutTradeNo {
		return a.unknown(i, "SIGNED_RESPONSE_MISMATCH"), ErrUnknown
	}
	fen, e := ParseYuan(amount)
	if e != nil || fen != i.Amount {
		return a.unknown(i, "SIGNED_RESPONSE_MISMATCH"), ErrUnknown
	}
	// Deprecated resource_id is deliberately not a binding source. The
	// immutable local original number binds service/app/seller/contract.
	sum := sha256.Sum256(node)
	return Observation{scope: i.Scope, amount: fen, kind: "VERIFIED_PAID", trade: trade, evidenceHash: hex.EncodeToString(sum[:]), observed: a.now().UTC()}, nil
}

// Public Machine Pay sources currently prove these two methods only. This
// adapter does not manufacture capture/refund/close/settlement endpoints.
func (a *Alipay) Capabilities() []string { return []string{"PRECREATE", "VERIFY_SAME_PROOF"} }
