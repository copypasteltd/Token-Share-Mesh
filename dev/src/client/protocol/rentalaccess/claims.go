// Package rentalaccess defines bounded signed rental-session authority. A
// verified token is not a physical resource ticket, ready confirmation, live
// control acknowledgement, availability evidence or authority to charge.
package rentalaccess

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

const Profile = "P11_RENTAL_ACCESS_EDDSA_1"
const Type = "p11-rental-access+jwt"
const MaximumTokenBytes = 4096

var ErrAccess = errors.New("original rental access authority unavailable")

// Claims has integer NumericDates and exactly one audience. The strict decoder
// rejects floats, duplicate/unknown fields and noncanonical counters before JWT
// parsing. None of these fields contains a prompt, output or signing key.
type Claims struct {
	Profile         string   `json:"profile_id"`
	Issuer          string   `json:"iss"`
	Subject         string   `json:"sub"`
	Audience        string   `json:"aud"`
	ID              string   `json:"jti"`
	Issued          int64    `json:"iat"`
	NotBefore       int64    `json:"nbf"`
	Expires         int64    `json:"exp"`
	Order           string   `json:"order_id"`
	Session         string   `json:"session_id"`
	Seller          string   `json:"seller_id"`
	Device          string   `json:"device_id"`
	Resource        string   `json:"resource_id"`
	Generation      string   `json:"resource_lock_generation"`
	Terms           string   `json:"terms_revision"`
	TermsHash       string   `json:"terms_hash"`
	Environment     string   `json:"environment_revision"`
	EnvironmentHash string   `json:"kernel_environment_digest"`
	Manifest        string   `json:"manifest_ref"`
	ManifestHash    string   `json:"manifest_hash"`
	Scopes          []string `json:"scope"`
	HardDeadline    string   `json:"hard_deadline"`
	Mode            string   `json:"mode"`
	Privacy         string   `json:"privacy"`
}

func (c Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Expires, 0)), nil
}
func (c Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Issued, 0)), nil
}
func (c Claims) GetNotBefore() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.NotBefore, 0)), nil
}
func (c Claims) GetIssuer() (string, error)             { return c.Issuer, nil }
func (c Claims) GetSubject() (string, error)            { return c.Subject, nil }
func (c Claims) GetAudience() (jwt.ClaimStrings, error) { return jwt.ClaimStrings{c.Audience}, nil }

func positive(s string) bool {
	n, e := strconv.ParseUint(s, 10, 64)
	return e == nil && n > 0 && strconv.FormatUint(n, 10) == s
}
func utc(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || t.Location() != time.UTC || t.Year() < 2000 || t.Year() > 2100 || t.Format(time.RFC3339Nano) != s {
		return time.Time{}, ErrAccess
	}
	return t, nil
}
func identifier(s string) bool { return len(s) <= 128 && ids.Valid(s) }
func validScopes(scopes []string) bool {
	if len(scopes) < 1 || len(scopes) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope] || (scope != "INVOKE" && scope != "QUEUE_SELF" && scope != "CANCEL_SELF" && scope != "EXPORT_SELF") {
			return false
		}
		seen[scope] = true
	}
	return true
}
func (c Claims) shape() error {
	for _, v := range []string{c.Issuer, c.Subject, c.Audience, c.ID, c.Order, c.Session, c.Seller, c.Device, c.Resource, c.Environment, c.Manifest} {
		if !identifier(v) {
			return ErrAccess
		}
	}
	hard, e := utc(c.HardDeadline)
	if e != nil || c.Profile != Profile || c.Mode != "GPU_RENTAL" || c.Privacy != "STANDARD" || !positive(c.Generation) || !positive(c.Terms) || !validScopes(c.Scopes) ||
		!executionlease.Hash(c.TermsHash) || !executionlease.Hash(c.EnvironmentHash) || !executionlease.Hash(c.ManifestHash) ||
		c.Issued < 946684800 || c.NotBefore < 946684800 || c.NotBefore > c.Issued || c.Expires <= c.Issued || c.Expires > hard.Unix() {
		return ErrAccess
	}
	return nil
}
func (c Claims) validAt(now time.Time) bool {
	return c.shape() == nil && !now.IsZero() && now.Unix() >= c.Issued && now.Unix() >= c.NotBefore && now.Unix() < c.Expires
}
func (c Claims) clone() Claims { c.Scopes = slices.Clone(c.Scopes); return c }

// Expected is provisioned from the current original session and independently
// approved endpoint/environment state. Never populate it from the received JWT.
// Grant ID and terms must be current; cryptographic verification alone cannot
// discover revocation or take a session's local/remote resource lock.
type Expected struct {
	TokenDigest                                                                                      string
	Issuer, Subject, Audience, Grant, Order, Session, Seller, Device, Resource                       string
	Generation, Terms, TermsHash, Environment, EnvironmentHash, Manifest, ManifestHash, HardDeadline string
}

func (e Expected) shape() error {
	for _, v := range []string{e.Issuer, e.Subject, e.Audience, e.Grant, e.Order, e.Session, e.Seller, e.Device, e.Resource, e.Environment, e.Manifest} {
		if !identifier(v) {
			return ErrAccess
		}
	}
	if _, err := utc(e.HardDeadline); err != nil || !executionlease.Hash(e.TokenDigest) || !positive(e.Generation) || !positive(e.Terms) || !executionlease.Hash(e.TermsHash) || !executionlease.Hash(e.EnvironmentHash) || !executionlease.Hash(e.ManifestHash) {
		return ErrAccess
	}
	return nil
}
func (e Expected) matches(c Claims) bool {
	return e.shape() == nil && c.Issuer == e.Issuer && c.Subject == e.Subject && c.Audience == e.Audience && c.ID == e.Grant && c.Order == e.Order && c.Session == e.Session && c.Seller == e.Seller && c.Device == e.Device && c.Resource == e.Resource && c.Generation == e.Generation && c.Terms == e.Terms && c.TermsHash == e.TermsHash && c.Environment == e.Environment && c.EnvironmentHash == e.EnvironmentHash && c.Manifest == e.Manifest && c.ManifestHash == e.ManifestHash && c.HardDeadline == e.HardDeadline
}

func (e Expected) binds(v Verified) bool {
	return v.valid && e.TokenDigest == v.digest && e.matches(v.claims)
}

type Verified struct {
	claims        Claims
	keyID, digest string
	valid         bool
}

func (v Verified) Details() Claims {
	if !v.valid {
		return Claims{}
	}
	return v.claims.clone()
}
func (v Verified) ValidAt(now time.Time) bool { return v.valid && v.claims.validAt(now) }
func (v Verified) Allows(scope string, now time.Time) bool {
	return v.ValidAt(now) && slices.Contains(v.claims.Scopes, scope)
}
func (v Verified) Digest() string {
	if !v.valid {
		return ""
	}
	return v.digest
}
func (v Verified) KeyID() string {
	if !v.valid {
		return ""
	}
	return v.keyID
}
