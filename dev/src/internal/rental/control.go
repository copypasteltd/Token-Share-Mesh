package rental

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const ControlHistoryProfile = "P11_RENTAL_CONTROL_HISTORY_1"

type ControlResult struct {
	Receipt  json.RawMessage
	Replayed bool
}

type controlGrant struct {
	Metadata rentalaccess.Metadata
	Version  int64
	Revoked  bool
}

func loadControlGrantTx(ctx context.Context, tx pgx.Tx, r sessionRecord, in rentalaccess.ControlRequest) (controlGrant, error) {
	var g controlGrant
	var raw []byte
	var digest string
	var expires time.Time
	err := tx.QueryRow(ctx, `SELECT safe_claims,digest,authority_session_version,revoked_at IS NOT NULL,expires_at
FROM p11.rental_access_grants WHERE id=$1 AND session_id=$2 AND subject_id=$3 AND authority_profile=$4 FOR SHARE`, in.Grant, r.Session.ID, r.Session.Contract.Buyer, rentalaccess.Profile).
		Scan(&raw, &digest, &g.Version, &g.Revoked, &expires)
	if err == pgx.ErrNoRows {
		return g, fault.New(fault.NotFound, "Original rental grant not found")
	}
	if err != nil {
		return g, err
	}
	v := r.Session
	if strictjson.Decode(raw, &g.Metadata, 8192) != nil {
		return g, fault.New(fault.Unavailable, "Original rental grant metadata requires recovery")
	}
	m, c := g.Metadata, g.Metadata.Claims
	if m.Profile != rentalaccess.Profile || m.Digest != digest || digest != in.Digest || g.Version != in.ExpectedVersion ||
		c.Profile != rentalaccess.Profile || c.ID != in.Grant || c.Order != v.Contract.Order || c.Session != v.ID || c.Subject != v.Contract.Buyer || c.Seller != v.Contract.Seller ||
		c.Device != v.Contract.Device || c.Audience != v.Contract.Device || c.Resource != v.Contract.Resource || c.Generation != v.Generation || c.Generation != in.Generation ||
		c.Terms != strconv.FormatInt(v.Terms, 10) || c.Terms != in.Terms || c.TermsHash != v.TermsHash || c.TermsHash != in.TermsHash ||
		c.Environment != v.Contract.Environment || c.EnvironmentHash != v.Contract.EnvironmentHash || c.Manifest != v.Contract.Manifest || c.ManifestHash != v.Contract.ManifestHash ||
		c.HardDeadline != v.HardDeadline.UTC().Format(time.RFC3339Nano) || !expires.Equal(time.Unix(c.Expires, 0).UTC()) {
		return g, fault.New(fault.Conflict, "Original stored grant, session and terms must match")
	}
	return g, nil
}

// controlDenied accepts only explicit domain denial. A database, storage,
// transaction or corrupt-evidence error is never converted into trusted STOP
// metadata, nor into ALLOW. The native hard cutoff remains independent.
func controlDenied(err error) bool {
	var e *fault.Error
	return errors.As(err, &e) && (e.Code == fault.Conflict || e.Code == fault.Forbidden || e.Code == fault.NotFound)
}

func controlBuyerTx(ctx context.Context, tx pgx.Tx, buyer string) (bool, error) {
	var state, role string
	err := tx.QueryRow(ctx, `SELECT state FROM p11.users WHERE id=$1 FOR SHARE`, buyer).Scan(&state)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil || state != "ACTIVE" {
		return false, err
	}
	err = tx.QueryRow(ctx, `SELECT id FROM p11.role_grants WHERE user_id=$1 AND role_code='BUYER' AND scope_ref='self'
AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, buyer).Scan(&role)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ConfirmControl is invoked only by the original signed provider device. Every
// fresh sequence is decided under the original order lock. SQL/idempotency may
// retain these signed metadata receipts; neither contains a rental Bearer.
func (s *Service) ConfirmControl(ctx context.Context, p identity.Principal, id string, in rentalaccess.ControlRequest, key string) (ControlResult, error) {
	var result ControlResult
	if err := s.configured(); err != nil {
		return result, err
	}
	sequence, err := in.SequenceNumber()
	if err != nil || !ids.Valid(id) || p.Kind != identity.Device || p.Require("device:rental", "") != nil {
		return result, fault.New(fault.Invalid, "Original signed device and bounded rental control request required")
	}
	c, err := command(p, "confirmRentalControl", id, key, in)
	if err != nil {
		return result, err
	}
	rowReplayed := false
	stored, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "device:rental", true), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		g, err := loadControlGrantTx(ctx, tx, r, in)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		m, v := g.Metadata, r.Session
		// One configured private key cannot impersonate an old rotated key. An
		// explicit replacement grant is required; no auto-generated key/upgrade.
		if m.KeyID != s.keyID || m.Claims.Issuer != s.issuer || !o.Now.Before(time.Unix(m.Claims.Expires, 0)) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original rental issuing key and live credential required")
		}
		requestRaw, err := json.Marshal(in)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		requestDigest := outbox.Digest(requestRaw)
		var previousRequest string
		var previousReply []byte
		err = tx.QueryRow(ctx, `SELECT request_digest,reply_raw FROM p11.rental_control_history WHERE grant_id=$1 AND sequence=$2`, in.Grant, sequence).Scan(&previousRequest, &previousReply)
		if err == nil {
			if previousRequest != requestDigest {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original control sequence cannot change its request")
			}
			rowReplayed = true
			return outbox.StoredResult{Reference: in.Grant, Body: previousReply, HTTPStatus: 200}, nil
		}
		if err != pgx.ErrNoRows {
			return outbox.StoredResult{}, err
		}
		var high int64
		var stopped bool
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0),COALESCE(bool_or(decision='STOP'),false) FROM p11.rental_control_history WHERE grant_id=$1`, in.Grant).Scan(&high, &stopped); err != nil {
			return outbox.StoredResult{}, err
		}
		if sequence <= high {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Control sequence is behind its committed high water")
		}
		decision := "STOP"
		if !g.Revoked && !stopped && v.State == "ACTIVE" && v.Version == in.ExpectedVersion && o.Now.Before(v.HardDeadline) {
			buyer, e := controlBuyerTx(ctx, tx, o.Buyer)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if buyer {
				funds, resource, e := s.currentWorkTx(ctx, tx, o, r)
				if e != nil && !controlDenied(e) {
					return outbox.StoredResult{}, e
				}
				if e == nil && resource.Phase == "ACTIVE" && funds.covers(v.Contract, v.Budget, o.Now, v.HardDeadline) {
					decision = "ALLOW"
				}
			}
		}
		expires := minTime(o.Now.Add(rentalaccess.MaximumControlTerm), v.HardDeadline, time.Unix(m.Claims.Expires, 0).UTC())
		reply, err := s.signer.SignControl(m, uint64(sequence), decision, o.Now, expires)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Bounded original rental control receipt unavailable")
		}
		raw, err := json.Marshal(reply)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		event := ids.New("event")
		safe, _ := json.Marshal(map[string]any{"profile_id": ControlHistoryProfile, "session_id": id, "grant_id": in.Grant, "sequence": in.Sequence, "decision": decision, "reply_digest": outbox.Digest(raw), "resource_lock_generation": v.Generation, "terms_revision": in.Terms})
		_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'RENTAL_CONTROL',$2,$3,'RentalControlConfirmed','1.0',$4,'PENDING')`, event, in.Grant, sequence, safe)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO p11.rental_control_history(grant_id,sequence,session_id,device_id,grant_digest,grant_session_version,
resource_lock_generation,terms_revision,terms_hash,request_raw,request_digest,reply_raw,reply_digest,decision,issued_at,expires_at,event_id)
VALUES($1,$2,$3,$4,$5,$6,$7::bigint,$8::bigint,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, in.Grant, sequence, id, p.DeviceID, in.Digest, in.ExpectedVersion, in.Generation, in.Terms, in.TermsHash, requestRaw, requestDigest, raw, outbox.Digest(raw), decision, o.Now, expires, event)
		return outbox.StoredResult{Reference: in.Grant, Body: raw, HTTPStatus: 200}, err
	})
	if err != nil {
		return result, err
	}
	return ControlResult{Receipt: append(json.RawMessage(nil), stored.Body...), Replayed: stored.Replayed || rowReplayed}, nil
}
