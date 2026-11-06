// Package paymenthost owns an explicitly configured native Machine Pay host.
// Loading configuration never registers a merchant, approves evidence or pays.
package paymenthost

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/strictjson"
)

var ErrConfiguration = errors.New("explicit original Machine Pay host configuration required")

type registrationFile struct {
	Capability  string              `json:"capability_ref"`
	Binding     payments.Binding    `json:"binding"`
	Policy      payments.SalePolicy `json:"policy"`
	PrivateFile string              `json:"signing_key_file"`
	PublicFile  string              `json:"alipay_key_file"`
	TokenFile   string              `json:"app_auth_token_file,omitempty"`
}
type hostFile struct {
	Profile       string                   `json:"profile_id"`
	Environment   string                   `json:"environment"`
	ProofKeys     string                   `json:"proof_keyring_file"`
	ReceiptKeys   string                   `json:"receipt_keyring_file"`
	Registrations []registrationFile       `json:"registrations"`
	Refunds       []refundRegistrationFile `json:"original_refund_routes,omitempty"`
}
type refundRegistrationFile struct {
	Ref                string               `json:"refund_route_ref"`
	OriginalCapability string               `json:"original_capability_ref"`
	Route              payments.RefundRoute `json:"route"`
}

// Only canonical relative slash paths are accepted beneath the supplied root.
// os.Root also blocks symlink escapes. The enclosing app supplies a trusted,
// project-owned runtime root, never a path or a key from HTTP/model requests.
func relativeFile(name string) bool {
	return name != "" && len(name) <= 240 && !strings.ContainsAny(name, "\\:\x00\r\n") &&
		!path.IsAbs(name) && path.Clean(name) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../") && filepath.IsLocal(filepath.FromSlash(name))
}
func readFile(root *os.Root, name string, maximum int) ([]byte, error) {
	if root == nil || !relativeFile(name) || maximum < 1 {
		return nil, ErrConfiguration
	}
	f, e := root.Open(filepath.FromSlash(name))
	if e != nil {
		return nil, ErrConfiguration
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > int64(maximum) {
		return nil, ErrConfiguration
	}
	raw, e := io.ReadAll(io.LimitReader(f, int64(maximum)+1))
	if e != nil || len(raw) > maximum {
		clear(raw)
		return nil, ErrConfiguration
	}
	return raw, nil
}
func privateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || len(rest) != 0 {
		return nil, ErrConfiguration
	}
	defer clear(block.Bytes)
	if block.Type == "RSA PRIVATE KEY" {
		key, e := x509.ParsePKCS1PrivateKey(block.Bytes)
		if e == nil {
			return key, nil
		}
	}
	if block.Type == "PRIVATE KEY" {
		key, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e == nil {
			if value, ok := key.(*rsa.PrivateKey); ok {
				return value, nil
			}
		}
	}
	return nil, ErrConfiguration
}
func publicKey(raw []byte) (*rsa.PublicKey, error) {
	block, rest := pem.Decode(raw)
	if block == nil || len(rest) != 0 {
		return nil, ErrConfiguration
	}
	defer clear(block.Bytes)
	if block.Type == "RSA PUBLIC KEY" {
		key, e := x509.ParsePKCS1PublicKey(block.Bytes)
		if e == nil {
			return key, nil
		}
	}
	if block.Type == "PUBLIC KEY" {
		key, e := x509.ParsePKIXPublicKey(block.Bytes)
		if e == nil {
			if value, ok := key.(*rsa.PublicKey); ok {
				return value, nil
			}
		}
	}
	return nil, ErrConfiguration
}
func readVault(root *os.Root, name, profile string) (*payments.Vault, error) {
	raw, e := readFile(root, name, 8192)
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	var spec struct {
		Profile string            `json:"profile_id"`
		Active  string            `json:"active_key_id"`
		Keys    map[string]string `json:"keys"`
	}
	if strictjson.Decode(raw, &spec, 8192) != nil || spec.Profile != profile || len(spec.Keys) < 1 || len(spec.Keys) > 8 {
		return nil, ErrConfiguration
	}
	keys := map[string][]byte{}
	defer func() {
		for _, key := range keys {
			clear(key)
		}
	}()
	for id, encoded := range spec.Keys {
		key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
			clear(key)
			return nil, ErrConfiguration
		}
		keys[id] = key
	}
	v, e := payments.NewVault(spec.Active, keys)
	if e != nil {
		return nil, ErrConfiguration
	}
	return v, nil
}
func readChannel(root *os.Root, spec registrationFile) (*payments.Alipay, error) {
	var result *payments.Alipay
	e := readChannelWith(root, spec, func(c payments.AlipayConfig) error { var e error; result, e = payments.NewAlipay(c); return e })
	return result, e
}
func readRefundChannel(root *os.Root, spec registrationFile, route payments.RefundRoute) (*payments.AlipayRefund, error) {
	var result *payments.AlipayRefund
	e := readChannelWith(root, spec, func(c payments.AlipayConfig) error {
		var e error
		result, e = payments.NewAlipayRefund(payments.AlipayRefundConfig{Channel: c, Route: route})
		return e
	})
	return result, e
}
func readChannelWith(root *os.Root, spec registrationFile, construct func(payments.AlipayConfig) error) error {
	raw, e := readFile(root, spec.PrivateFile, 16384)
	if e != nil {
		return e
	}
	defer clear(raw)
	private, e := privateKey(raw)
	if e != nil {
		return e
	}
	publicRaw, e := readFile(root, spec.PublicFile, 8192)
	if e != nil {
		return e
	}
	defer clear(publicRaw)
	public, e := publicKey(publicRaw)
	if e != nil {
		return e
	}
	var token []byte
	if spec.Binding.ThirdParty {
		token, e = readFile(root, spec.TokenFile, 4096)
		if e != nil {
			return e
		}
		defer clear(token)
	} else if spec.TokenFile != "" {
		return ErrConfiguration
	}
	e = construct(payments.AlipayConfig{Binding: spec.Binding, PrivateKey: private, AlipayPublicKey: public, AppAuthToken: token})
	if e != nil {
		return ErrConfiguration
	}
	return nil
}
