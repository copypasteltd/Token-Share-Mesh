package endpointregistry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

func hashValid(value string) bool {
	raw, e := hex.DecodeString(value)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
func conflict(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fault.New(fault.Conflict, "Original public key or endpoint version is already claimed; query the original registration")
	}
	return err
}
func dbTime(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, e
}
func reference(id string) (outbox.StoredResult, error) {
	raw, e := json.Marshal(map[string]string{"recipient_id": id})
	return outbox.StoredResult{Reference: id, Body: raw, HTTPStatus: 200}, e
}
func (s *Service) mutate(ctx context.Context, p identity.Principal, b endpointwire.Bundle, operation, resource, key string, in any, write bool, fn func(pgx.Tx, time.Time) (string, error)) (Record, error) {
	var empty Record
	if e := configured(s); e != nil {
		return empty, e
	}
	if !safeKey(key) {
		return empty, fault.New(fault.Invalid, "Original bounded idempotency key required")
	}
	raw, e := canonicaljson.Encode(in, 16384)
	if e != nil {
		return empty, fault.New(fault.Invalid, "Bounded public endpoint command required")
	}
	r, e := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: operation, Resource: resource, Key: key, Request: raw}, func(tx pgx.Tx) error {
		if e := authorize(ctx, tx, p, "recipient:manage"); e != nil {
			return e
		}
		return authorizeBundle(ctx, tx, p, b, write)
	}, func(tx pgx.Tx) (outbox.StoredResult, error) {
		if e := ownerLock(ctx, tx, p.UserID); e != nil {
			return outbox.StoredResult{}, e
		}
		now, e := dbTime(ctx, tx)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		id, e := fn(tx, now)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		return reference(id)
	})
	if e != nil {
		return empty, conflict(e)
	}
	// Never replay a saved ACTIVE view after expiry, rotation or revocation. The
	// safe idempotency row contains a reference only; query current authority.
	result, e := s.Get(ctx, p, r.Reference)
	result.Replayed = r.Replayed
	return result, e
}

func (s *Service) Register(ctx context.Context, p identity.Principal, in Registration, key string) (Record, error) {
	if e := validateBundle(p, in.Bundle); e != nil {
		return Record{}, e
	}
	if in.Bundle.Version != "1" {
		return Record{}, fault.New(fault.Invalid, "Initial endpoint version must be one; use a signed original rotation")
	}
	b := in.Bundle
	return s.mutate(ctx, p, b, "registerEndpointBundle", b.Endpoint, key, in, true, func(tx pgx.Tx, now time.Time) (string, error) {
		var existing string
		e := tx.QueryRow(ctx, `SELECT recipient_id FROM p11.endpoint_public_bundles WHERE owner_id=$1 AND endpoint_id=$2 AND role=$3 AND version=1`, b.Owner, b.Endpoint, b.Role).Scan(&existing)
		if e == nil {
			row, e := load(ctx, tx, existing, false)
			if e != nil {
				return "", e
			}
			if row.view.BundleHash != b.Hash() {
				return "", fault.New(fault.Conflict, "Original endpoint already exists; replacement requires its old identity signature")
			}
			return existing, nil
		}
		if e != pgx.ErrNoRows {
			return "", e
		}
		if b.Current(now) != nil {
			return "", fault.New(fault.Invalid, "Endpoint bundle is outside its current validity window")
		}
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(DISTINCT endpoint_id) FROM p11.endpoint_public_bundles WHERE owner_id=$1 AND role=$2`, b.Owner, b.Role).Scan(&count); e != nil {
			return "", e
		}
		if count >= MaximumEndpoints {
			return "", fault.New(fault.Conflict, "Public endpoint history capacity reached; no history is evicted")
		}
		return insert(ctx, tx, p, b, nil, now)
	})
}

func (s *Service) Rotate(ctx context.Context, p identity.Principal, recipient string, in Rotation, key string) (Record, error) {
	if !ids.Valid(recipient) || !hashValid(in.Transition.Previous) {
		return Record{}, fault.New(fault.Invalid, "Original recipient and signed rotation required")
	}
	if e := validateBundle(p, in.Transition.Next); e != nil {
		return Record{}, e
	}
	b := in.Transition.Next
	return s.mutate(ctx, p, b, "rotateEndpointBundle", recipient, key, in, true, func(tx pgx.Tx, now time.Time) (string, error) {
		old, e := load(ctx, tx, recipient, true)
		if e != nil {
			return "", e
		}
		if e = authorizeBundle(ctx, tx, p, old.view.Bundle, true); e != nil {
			return "", e
		}
		if old.view.BundleHash != in.Transition.Previous {
			return "", fault.New(fault.Conflict, "Rotation must retain the original public bundle")
		}
		if old.deviceFingerprint != nil && *old.deviceFingerprint != p.DeviceFingerprint {
			return "", fault.New(fault.Conflict, "Original device credential changed; explicit recovery is required")
		}
		// A second idempotency key can recover exactly the same successor. It cannot
		// fork a retired predecessor or resurrect a revoked version.
		var next string
		e = tx.QueryRow(ctx, `SELECT recipient_id FROM p11.endpoint_public_bundles WHERE previous_recipient_id=$1`, recipient).Scan(&next)
		if e == nil {
			row, e := load(ctx, tx, next, false)
			if e != nil {
				return "", e
			}
			if row.view.TransitionHash != in.Transition.Hash() || row.view.BundleHash != b.Hash() {
				return "", fault.New(fault.Conflict, "Original endpoint already rotated; query its original successor")
			}
			return next, nil
		}
		if e != pgx.ErrNoRows {
			return "", e
		}
		if old.view.State != "ACTIVE" || old.view.Bundle.Current(now) != nil || in.Transition.Verify(old.view.Bundle, now) != nil {
			return "", fault.New(fault.Conflict, "Current old identity and its valid signed successor are required")
		}
		n, _ := strconv.Atoi(b.Version)
		if n < 2 || n > MaximumVersions {
			return "", fault.New(fault.Conflict, "Endpoint version history capacity reached")
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.endpoint_public_bundles SET state='RETIRED' WHERE recipient_id=$1 AND state='ACTIVE'`, recipient); e != nil {
			return "", e
		}
		return insert(ctx, tx, p, b, &in.Transition, now)
	})
}

func insert(ctx context.Context, tx pgx.Tx, p identity.Principal, b endpointwire.Bundle, transition *endpointwire.Transition, now time.Time) (string, error) {
	raw, e := b.Bytes()
	if e != nil {
		return "", e
	}
	expires, _ := time.Parse(time.RFC3339Nano, b.Expires)
	expires = expires.Truncate(time.Microsecond)
	version, _ := strconv.ParseInt(b.Version, 10, 64)
	var device, fingerprint *string
	if b.Role == "SELLER" {
		device = &p.DeviceID
		fingerprint = &p.DeviceFingerprint
	}
	var previous, previousHash, rotationHash *string
	var rotation []byte
	if transition != nil {
		previousHash = &transition.Previous
		hash := transition.Hash()
		rotationHash = &hash
		var id string
		if e = tx.QueryRow(ctx, `SELECT recipient_id FROM p11.endpoint_public_bundles WHERE bundle_hash=$1`, transition.Previous).Scan(&id); e != nil {
			return "", e
		}
		previous = &id
		rotation, e = canonicaljson.Encode(*transition, 12000)
		if e != nil {
			return "", e
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.recipient_keys(id,owner_id,device_id,key_version,encryption_public_key,signing_public_key,profile_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, b.Content.ID, b.Owner, device, version, b.Content.Public, b.Identity.Public, endpointwire.BundleProfile, expires)
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.endpoint_public_bundles(recipient_id,bundle_hash,owner_id,endpoint_id,role,version,device_id,device_fingerprint,bundle_raw,state,previous_recipient_id,previous_bundle_hash,transition_hash,transition_raw,expires_at,registered_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'ACTIVE',$10,$11,$12,$13,$14,$15)`, b.Content.ID, b.Hash(), b.Owner, b.Endpoint, b.Role, version, device, fingerprint, raw, previous, previousHash, rotationHash, rotation, expires, now)
	if e != nil {
		return "", e
	}
	for purpose, k := range publicKeyClaims(b) {
		if _, e = tx.Exec(ctx, `INSERT INTO p11.endpoint_public_key_claims(key_id,recipient_id,owner_id,purpose,fingerprint) VALUES($1,$2,$3,$4,$5)`, k.ID, b.Content.ID, b.Owner, purpose, k.Fingerprint); e != nil {
			return "", e
		}
	}
	return b.Content.ID, nil
}

func (s *Service) Revoke(ctx context.Context, p identity.Principal, recipient string, in Revocation, key string) (Record, error) {
	if e := configured(s); e != nil {
		return Record{}, e
	}
	if !ids.Valid(recipient) || !hashValid(in.BundleHash) {
		return Record{}, fault.New(fault.Invalid, "Exact original recipient and bundle hash required")
	}
	// Read only the immutable selector before authorization; do not take a
	// directory SHARE lock and then try upgrading it under an owner lock.
	b, e := s.selector(ctx, p, recipient)
	if e != nil {
		return Record{}, e
	}
	return s.mutate(ctx, p, b, "revokeEndpointBundle", recipient, key, in, false, func(tx pgx.Tx, now time.Time) (string, error) {
		row, e := load(ctx, tx, recipient, true)
		if e != nil {
			return "", e
		}
		if row.view.BundleHash != in.BundleHash {
			return "", fault.New(fault.Conflict, "Revocation must name the original public bundle")
		}
		if row.view.State == "REVOKED" {
			return recipient, nil
		}
		if _, e = tx.Exec(ctx, `UPDATE p11.recipient_keys SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, recipient, now); e != nil {
			return "", e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.endpoint_public_bundles SET state='REVOKED',revoked_at=$2 WHERE recipient_id=$1`, recipient, now)
		return recipient, e
	})
}
