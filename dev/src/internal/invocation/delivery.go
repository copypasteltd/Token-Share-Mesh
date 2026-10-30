package invocation

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// Delivery decrypts only the currently authorized original PLAINTEXT input.
// It has no issuer private key, engine, renewal or content-logging path.
type Delivery struct {
	claims      *Claims
	invocations *Service
	mu          sync.RWMutex
	keys        map[string][]byte
}
type DeliveryConfig struct {
	Claims      *Claims
	Invocations *Service
	InputKeys   map[string][]byte
}

func NewDelivery(c DeliveryConfig) (*Delivery, error) {
	if c.Claims == nil || c.Invocations == nil || c.Claims.db == nil || c.Claims.db != c.Invocations.db || c.Claims.routes.PaymentEnvironment != c.Invocations.routes.PaymentEnvironment || len(c.InputKeys) < 1 || len(c.InputKeys) > 16 {
		return nil, fault.New(fault.Invalid, "Explicit same-domain claims, fingerprints and input keys required")
	}
	d := &Delivery{claims: c.Claims, invocations: c.Invocations, keys: map[string][]byte{}}
	c.Invocations.mu.RLock()
	defer c.Invocations.mu.RUnlock()
	if len(c.Invocations.keys) == 0 {
		return nil, fault.New(fault.Unavailable, "Fingerprint keys unavailable")
	}
	for id, key := range c.InputKeys {
		if !ids.Valid(id) || len(key) != 32 {
			d.Close()
			return nil, fault.New(fault.Invalid, "Invalid bounded input-key configuration")
		}
		for _, fp := range c.Invocations.keys {
			if bytes.Equal(key, fp) {
				d.Close()
				return nil, fault.New(fault.Invalid, "Input and fingerprint purposes require separate keys")
			}
		}
		d.keys[id] = append([]byte(nil), key...)
	}
	return d, nil
}

func (d *Delivery) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, k := range d.keys {
		clear(k)
		delete(d.keys, id)
	}
}
func (d *Delivery) aead(version string) (cipher.AEAD, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	key := d.keys[version]
	if len(key) != 32 {
		return nil, fault.New(fault.Unavailable, "Original input key unavailable")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}

// readTx never claims a lease implicitly. Its immutable original binding must
// already exist for this signed device/version; authorizeTx rechecks all live
// original-order authority using the same order -> catalog -> device lock order.
func (s *Claims) readTx(ctx context.Context, tx pgx.Tx, p identity.Principal, id string) (claimAuthority, error) {
	var zero claimAuthority
	if s == nil || s.db == nil {
		return zero, fault.New(fault.Unavailable, "Execution delivery unavailable")
	}
	if p.Kind != identity.Device || !ids.Valid(p.DeviceID) {
		return zero, fault.New(fault.Forbidden, "Original signed device required")
	}
	if e := p.Require("device:execute", ""); e != nil {
		return zero, e
	}
	if !ids.Valid(id) {
		return zero, fault.New(fault.Invalid, "Invalid lease reference")
	}
	var cap string
	var version int64
	e := tx.QueryRow(ctx, `SELECT a.capability_id,l.claim_device_version FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id
 WHERE l.id=$1 AND l.claim_profile=$2 AND l.claim_device_id=$3 AND a.device_id=$3 AND a.seller_id=$4`, id, ClaimProfile, p.DeviceID, p.UserID).Scan(&cap, &version)
	if e == pgx.ErrNoRows {
		return zero, fault.New(fault.NotFound, "Original claimed lease unavailable")
	}
	if e != nil {
		return zero, e
	}
	v, e := s.authorizeTx(ctx, tx, p, id, cap, version)
	if e != nil {
		return zero, e
	}
	if v.state != ClaimProfile {
		return zero, fault.New(fault.Conflict, "Original claim unavailable")
	}
	return v, nil
}

// Manifest returns metadata only; no decryption configuration is needed.
func (s *Claims) Manifest(ctx context.Context, p identity.Principal, id string) (Manifest, error) {
	if s == nil || s.db == nil {
		return Manifest{}, fault.New(fault.Unavailable, "Execution delivery unavailable")
	}
	var v claimAuthority
	e := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error { var e error; v, e = s.readTx(ctx, tx, p, id); return e })
	if e != nil {
		return Manifest{}, e
	}
	expiry, _ := time.Parse(time.RFC3339Nano, v.Lease.Expires)
	if !time.Now().Before(expiry) {
		return Manifest{}, fault.New(fault.Conflict, "Original delivery expired")
	}
	return v.manifest, nil
}

// Input returns exact original bytes only after authenticated storage AND the
// independently keyed, original job HMAC agree. The caller owns/clears the
// returned buffer. This is a trusted plaintext hop, never E2E or provider blind.
func (d *Delivery) Input(ctx context.Context, p identity.Principal, id string) ([]byte, error) {
	if d == nil || d.claims == nil {
		return nil, fault.New(fault.Unavailable, "Input delivery unavailable")
	}
	var body []byte
	var expiry time.Time
	e := d.claims.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		v, e := d.claims.readTx(ctx, tx, p, id)
		if e != nil {
			return e
		}
		expiry, _ = time.Parse(time.RFC3339Nano, v.Lease.Expires)
		a, e := loadStoredInput(ctx, tx, Preparation{JobID: v.manifest.Job})
		if e != nil {
			return e
		}
		defer clear(a.ciphertext)
		gcm, e := d.aead(a.version)
		if e != nil {
			return e
		}
		plain, e := openStoredInput(a, gcm)
		if e != nil {
			return e
		}
		defer clear(plain)
		meta, e := relay.Preflight(v.manifest.Spec.Protocol, v.manifest.Spec.Model, plain)
		if e != nil || meta.MaxOutputTokens != v.manifest.Spec.MaxOutputTokens || meta.Stream != v.manifest.Spec.Stream || int64(len(plain)) != v.manifest.InputBytes {
			return fault.New(fault.Conflict, "Original input protocol or bounds changed")
		}
		var version, digest string
		if e = tx.QueryRow(ctx, `SELECT fingerprint_version,request_digest FROM p11.jobs WHERE id=$1`, v.manifest.Job).Scan(&version, &digest); e != nil {
			return e
		}
		expected, e := d.invocations.fingerprint(version, v.buyer, v.manifest.Route, Request{Protocol: v.manifest.Spec.Protocol, Model: v.manifest.Spec.Model, RequestDigest: meta.Digest, MaxOutputTokens: meta.MaxOutputTokens, Stream: meta.Stream})
		if e != nil {
			return e
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(digest)) != 1 {
			return fault.New(fault.Conflict, "Original input fingerprint differs")
		}
		body = append([]byte(nil), plain...)
		return nil
	})
	if e != nil {
		clear(body)
		return nil, e
	}
	if !time.Now().Before(expiry) {
		clear(body)
		return nil, fault.New(fault.Conflict, "Original input delivery expired")
	}
	return body, nil
}
