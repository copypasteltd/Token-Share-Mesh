package rentalaccess

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"p11.local/compute/shared/strictjson"
)

type protected struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	Key       string `json:"kid"`
}

type Signer struct {
	mu            sync.RWMutex
	issuer, keyID string
	key           ed25519.PrivateKey
}

func NewSigner(issuer, keyID string, key ed25519.PrivateKey) (*Signer, error) {
	if !identifier(issuer) || !identifier(keyID) || len(key) != ed25519.PrivateKeySize {
		return nil, ErrAccess
	}
	derived := ed25519.NewKeyFromSeed(key.Seed())
	defer clear(derived)
	if !bytes.Equal(key, derived) {
		return nil, ErrAccess
	}
	return &Signer{issuer: issuer, keyID: keyID, key: append(ed25519.PrivateKey(nil), key...)}, nil
}
func (s *Signer) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.key)
	s.key = nil
}

// Metadata is safe to persist in the existing grant/idempotency records. It
// excludes the credential, signature and private key. It is not a paid/ready or
// accepted availability statement and does not establish a physical occupant.
type Metadata struct {
	Profile string `json:"profile_id"`
	KeyID   string `json:"issuer_key_id"`
	Digest  string `json:"digest"`
	Claims  Claims `json:"safe_claims"`
}

// Issued does not serialize or format the Bearer. A response writer may take it
// once after the original grant transaction commits; replays return Metadata.
// The service must not store TakeCredential's result in an outbox/body/log.
type Issued struct {
	mu         sync.Mutex
	metadata   Metadata
	credential string
}

func (i *Issued) Metadata() Metadata {
	if i == nil {
		return Metadata{}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	m := i.metadata
	m.Claims = m.Claims.clone()
	return m
}
func (i *Issued) TakeCredential() (string, error) {
	if i == nil {
		return "", ErrAccess
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.credential == "" {
		return "", ErrAccess
	}
	token := i.credential
	i.credential = ""
	return token, nil
}
func (i *Issued) Close() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.credential = ""
}
func (i *Issued) String() string               { return "[REDACTED rental credential]" }
func (i *Issued) GoString() string             { return i.String() }
func (i *Issued) MarshalJSON() ([]byte, error) { return json.Marshal(i.Metadata()) }

func (s *Signer) Issue(c Claims, now time.Time) (*Issued, error) {
	if s == nil || !c.validAt(now) || c.Issuer != s.issuer {
		return nil, ErrAccess
	}
	c = c.clone()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	token.Header = map[string]any{"alg": "EdDSA", "typ": Type, "kid": s.keyID}
	s.mu.RLock()
	if len(s.key) != ed25519.PrivateKeySize {
		s.mu.RUnlock()
		return nil, ErrAccess
	}
	credential, err := token.SignedString(s.key)
	s.mu.RUnlock()
	if err != nil || len(credential) > MaximumTokenBytes {
		return nil, ErrAccess
	}
	h := sha256.Sum256([]byte(credential))
	return &Issued{metadata: Metadata{Profile: Profile, KeyID: s.keyID, Digest: hex.EncodeToString(h[:]), Claims: c}, credential: credential}, nil
}

type Verifier struct {
	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey
}

func NewVerifier(pins map[string]ed25519.PublicKey) (*Verifier, error) {
	if len(pins) < 1 || len(pins) > 16 {
		return nil, ErrAccess
	}
	v := &Verifier{keys: map[string]ed25519.PublicKey{}}
	for id, key := range pins {
		if !identifier(id) || len(key) != ed25519.PublicKeySize {
			v.Close()
			return nil, ErrAccess
		}
		v.keys[id] = append(ed25519.PublicKey(nil), key...)
	}
	return v, nil
}
func (v *Verifier) Close() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, key := range v.keys {
		clear(key)
		delete(v.keys, id)
	}
}
func segment(s string, maximum int) ([]byte, error) {
	if len(s) == 0 || len(s) > (maximum*4+2)/3 {
		return nil, ErrAccess
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	if e != nil || len(b) > maximum || base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, ErrAccess
	}
	return b, nil
}

func (v *Verifier) Verify(credential string, expected Expected, now time.Time) (Verified, error) {
	if v == nil || len(credential) < 3 || len(credential) > MaximumTokenBytes || expected.shape() != nil || now.IsZero() || strings.Count(credential, ".") != 2 {
		return Verified{}, ErrAccess
	}
	digest := sha256.Sum256([]byte(credential))
	if hex.EncodeToString(digest[:]) != expected.TokenDigest {
		return Verified{}, ErrAccess
	}
	parts := strings.Split(credential, ".")
	header, e := segment(parts[0], 512)
	if e != nil {
		return Verified{}, ErrAccess
	}
	payload, e := segment(parts[1], 3072)
	if e != nil {
		return Verified{}, ErrAccess
	}
	signature, e := segment(parts[2], ed25519.SignatureSize)
	if e != nil || len(signature) != ed25519.SignatureSize {
		return Verified{}, ErrAccess
	}
	var h protected
	var claimed Claims
	if strictjson.Decode(header, &h, 512) != nil || strictjson.Decode(payload, &claimed, 3072) != nil || h.Algorithm != "EdDSA" || h.Type != Type || !identifier(h.Key) || !claimed.validAt(now) || !expected.matches(claimed) {
		return Verified{}, ErrAccess
	}
	v.mu.RLock()
	key := append(ed25519.PublicKey(nil), v.keys[h.Key]...)
	v.mu.RUnlock()
	defer clear(key)
	if len(key) != ed25519.PublicKeySize {
		return Verified{}, ErrAccess
	}
	var verified Claims
	parsed, e := jwt.ParseWithClaims(credential, &verified, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA || token.Header["alg"] != "EdDSA" || token.Header["typ"] != Type || token.Header["kid"] != h.Key || len(token.Header) != 3 {
			return nil, ErrAccess
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(expected.Issuer), jwt.WithSubject(expected.Subject), jwt.WithAudience(expected.Audience), jwt.WithIssuedAt(), jwt.WithExpirationRequired(), jwt.WithNotBeforeRequired(), jwt.WithStrictDecoding(), jwt.WithJSONNumber(), jwt.WithLeeway(0), jwt.WithTimeFunc(func() time.Time { return now }))
	if e != nil || parsed == nil || !parsed.Valid || !verified.validAt(now) || !expected.matches(verified) {
		return Verified{}, ErrAccess
	}
	return Verified{claims: verified.clone(), keyID: h.Key, digest: hex.EncodeToString(digest[:]), valid: true}, nil
}
