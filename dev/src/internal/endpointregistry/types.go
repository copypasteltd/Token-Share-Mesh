// Package endpointregistry binds public endpoint bundles to a currently
// authenticated account/device. It contains no content keys or decryptor.
// Directory registration is not independent endpoint trust or execution proof.
package endpointregistry

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

const Profile = endpointdirectory.Profile
const MaximumEndpoints = endpointdirectory.MaximumEndpoints
const MaximumVersions = endpointdirectory.MaximumVersions
const PageSize = endpointdirectory.PageSize

type Service struct{ DB *persistence.DB }

func (s *Service) UsesDB(db *persistence.DB) bool { return s != nil && s.DB != nil && s.DB == db }

type Registration = endpointdirectory.Registration
type Rotation = endpointdirectory.Rotation
type Revocation = endpointdirectory.Revocation
type Record = endpointdirectory.Record
type Page = endpointdirectory.Page
type stored struct {
	view                Record
	raw, transition     []byte
	registered, expires time.Time
	deviceFingerprint   *string
	device              *string
	revoked             *time.Time
}

func configured(s *Service) error {
	if s == nil || s.DB == nil {
		return fault.New(fault.Unavailable, "Public endpoint directory unavailable")
	}
	return nil
}
func safeKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
func publicKeyClaims(b endpointwire.Bundle) map[string]endpointwire.PublicKey {
	keys := map[string]endpointwire.PublicKey{"IDENTITY": b.Identity, "CONTENT": b.Content}
	if b.Receipt != nil {
		keys["RECEIPT"] = *b.Receipt
	}
	return keys
}
func separateDeviceAuth(b endpointwire.Bundle, fingerprint string) bool {
	for _, key := range publicKeyClaims(b) {
		if key.Fingerprint == fingerprint {
			return false
		}
	}
	return true
}
func validateBundle(p identity.Principal, b endpointwire.Bundle) error {
	v, e := strconv.ParseInt(b.Version, 10, 63)
	if b.Validate() != nil || b.Owner != p.UserID || e != nil || v < 1 || v > MaximumVersions {
		return fault.New(fault.Invalid, "Original signed public endpoint bundle required")
	}
	if p.Kind == identity.Device {
		if b.Role != "SELLER" || b.Endpoint != p.DeviceID || !separateDeviceAuth(b, p.DeviceFingerprint) {
			return fault.New(fault.Forbidden, "Public endpoint must match the authenticated provider device")
		}
	} else if p.Kind != identity.UserSession || p.SessionID == "" || b.Role != "BUYER" {
		return fault.New(fault.Forbidden, "Buyer session or signed provider device required for public-key registration")
	}
	return nil
}
func summary(row stored, now time.Time) Record {
	v := row.view
	v.Profile = Profile
	v.IndependentTrustRequired = true
	if v.State != "REVOKED" && v.Bundle.Current(now) != nil {
		v.State = "EXPIRED"
	}
	return v
}

// Authorize the current credential before any directory/advisory lock. Device
// reads use SHARE directly and never upgrade after taking the directory lock.
func authorize(ctx context.Context, tx pgx.Tx, p identity.Principal, scope string) error {
	if p.Kind == identity.Device {
		return identity.RequireDeviceReadTx(ctx, tx, p, scope)
	}
	if p.Kind != identity.UserSession || !ids.Valid(p.SessionID) {
		return fault.New(fault.Forbidden, "Current account session or signed device required")
	}
	if e := p.RequireUser(scope); e != nil {
		return e
	}
	return identity.RequireCurrentTx(ctx, tx, p)
}
func authorizeBundle(ctx context.Context, tx pgx.Tx, p identity.Principal, b endpointwire.Bundle, write bool) error {
	if b.Owner != p.UserID {
		return fault.New(fault.NotFound, "Public endpoint not found")
	}
	if p.Kind == identity.Device {
		if b.Role != "SELLER" || b.Endpoint != p.DeviceID {
			return fault.New(fault.NotFound, "Public endpoint not found")
		}
		if write {
			var allowed bool
			if e := tx.QueryRow(ctx, `SELECT state='APPROVED' FROM p11.seller_profiles WHERE id=$1 FOR SHARE`, p.UserID).Scan(&allowed); e != nil {
				return e
			}
			if !allowed {
				return fault.New(fault.Forbidden, "Current provider approval required for new public-key registration")
			}
		}
		return nil
	}
	var role string
	e := tx.QueryRow(ctx, `SELECT role_code FROM p11.role_grants WHERE user_id=$1 AND role_code=$2 AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, p.UserID, b.Role).Scan(&role)
	if e == pgx.ErrNoRows {
		return fault.New(fault.Forbidden, "Current endpoint account role required")
	}
	return e
}
func ownerLock(ctx context.Context, tx pgx.Tx, owner string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, Profile+":"+owner)
	return e
}
func load(ctx context.Context, tx pgx.Tx, recipient string, update bool) (stored, error) {
	var row stored
	var previous, rotation *string
	query := `SELECT b.recipient_id,b.bundle_hash,b.bundle_raw,b.state,b.previous_recipient_id,b.transition_hash,b.transition_raw,b.registered_at,b.expires_at,b.device_id,b.device_fingerprint,b.revoked_at,
r.owner_id,r.device_id,r.key_version::text,r.encryption_public_key,r.signing_public_key,r.profile_id,r.expires_at,r.revoked_at
FROM p11.endpoint_public_bundles b JOIN p11.recipient_keys r ON r.id=b.recipient_id WHERE b.recipient_id=$1`
	if update {
		query += " FOR UPDATE OF b,r"
	} else {
		query += " FOR SHARE OF b,r"
	}
	var owner, version, content, signer, profile string
	var device *string
	var expires time.Time
	var revoked *time.Time
	e := tx.QueryRow(ctx, query, recipient).Scan(&row.view.Recipient, &row.view.BundleHash, &row.raw, &row.view.State, &previous, &rotation, &row.transition, &row.registered, &row.expires, &row.device, &row.deviceFingerprint, &row.revoked, &owner, &device, &version, &content, &signer, &profile, &expires, &revoked)
	if e == pgx.ErrNoRows {
		return row, fault.New(fault.NotFound, "Public endpoint not found")
	}
	if e != nil {
		return row, e
	}
	b := &row.view.Bundle
	if canonicaljson.DecodeExact(row.raw, b, 8192) != nil || b.Validate() != nil || b.Hash() != row.view.BundleHash || b.Content.ID != recipient || b.Owner != owner || b.Version != version || b.Content.Public != content || b.Identity.Public != signer || profile != endpointwire.BundleProfile || !row.expires.Equal(expires) {
		return row, fault.New(fault.Unavailable, "Original public endpoint binding requires recovery")
	}
	stamp, _ := time.Parse(time.RFC3339Nano, b.Expires)
	if !stamp.Truncate(time.Microsecond).Equal(expires) || ((row.revoked == nil) != (revoked == nil)) || row.revoked != nil && !row.revoked.Equal(*revoked) {
		return row, fault.New(fault.Unavailable, "Original public endpoint lifetime requires recovery")
	}
	if b.Role == "SELLER" && (device == nil || row.device == nil || *device != b.Endpoint || *row.device != b.Endpoint || row.deviceFingerprint == nil) || b.Role == "BUYER" && (device != nil || row.device != nil || row.deviceFingerprint != nil) {
		return row, fault.New(fault.Unavailable, "Original endpoint device binding requires recovery")
	}
	if row.deviceFingerprint != nil && !separateDeviceAuth(*b, *row.deviceFingerprint) {
		return row, fault.New(fault.Unavailable, "Device authentication and content identity purposes must remain separate")
	}
	if previous != nil {
		row.view.Previous = *previous
	}
	if rotation != nil {
		row.view.TransitionHash = *rotation
	}
	if row.view.State != "ACTIVE" && row.view.State != "RETIRED" && row.view.State != "REVOKED" || (row.view.State == "REVOKED") != (row.revoked != nil) {
		return row, fault.New(fault.Unavailable, "Original endpoint state requires recovery")
	}
	if row.view.Previous != "" {
		var proof endpointwire.Transition
		if canonicaljson.DecodeExact(row.transition, &proof, 12000) != nil || proof.Hash() != row.view.TransitionHash || proof.Next.Hash() != b.Hash() {
			return row, fault.New(fault.Unavailable, "Original endpoint rotation requires recovery")
		}
		row.view.Rotation = &proof
	} else if b.Version != "1" || len(row.transition) != 0 || row.view.TransitionHash != "" {
		return row, fault.New(fault.Unavailable, "Original endpoint lineage requires recovery")
	}
	for purpose, key := range publicKeyClaims(*b) {
		var valid bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.endpoint_public_key_claims WHERE key_id=$1 AND recipient_id=$2 AND owner_id=$3 AND purpose=$4 AND fingerprint=$5)`, key.ID, recipient, owner, purpose, key.Fingerprint).Scan(&valid)
		if e != nil {
			return row, e
		}
		if !valid {
			return row, fault.New(fault.Unavailable, "Original public key purpose requires recovery")
		}
	}
	return row, nil
}
