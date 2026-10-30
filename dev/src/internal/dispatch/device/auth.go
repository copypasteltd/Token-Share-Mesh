package device

import (
	"context"
	"crypto/ed25519"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"strconv"
	"time"
)

func authorizeDevice(ctx context.Context, tx pgx.Tx, p identity.Principal, scope string) error {
	if p.Kind != identity.Device {
		return fault.New(fault.Forbidden, "Device signing identity required")
	}
	if err := identity.RequireRoleTx(ctx, tx, p, scope, "SELLER", false); err != nil {
		return err
	}
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM p11.seller_profiles WHERE id=$1 FOR SHARE`, p.UserID).Scan(&state)
	if err == pgx.ErrNoRows || err == nil && state != "APPROVED" && state != "EXITING" {
		return fault.New(fault.Forbidden, "Seller identity no longer valid")
	}
	return err
}

// Authenticate consumes one proof nonce. Business retries sign a fresh nonce but
// retain their original idempotency key and exact body. Nonces are durable across
// API restarts; no nonce or signature is a reusable content key/session token.
func (s Service) Authenticate(ctx context.Context, token string, binding devicecontrol.Binding, body []byte) (identity.Principal, error) {
	var p identity.Principal
	if err := s.configured(false); err != nil {
		return p, err
	}
	bad := fault.New(fault.Unauthenticated, "Invalid, expired or replayed device proof")
	proof, err := devicecontrol.Parse(token, time.Now())
	if err != nil {
		return p, bad
	}
	message, err := devicecontrol.Message(proof, binding, body)
	if err != nil {
		return p, bad
	}
	var public, profile string
	err = s.DB.Pool.QueryRow(ctx, `SELECT d.seller_id,d.auth_key_digest,d.device_public_key,d.identity_profile,u.version FROM p11.devices d JOIN p11.users u ON u.id=d.seller_id WHERE d.id=$1 AND d.state NOT IN ('REVOKED','RETIRED','QUARANTINED') AND u.state='ACTIVE'`, proof.DeviceID).Scan(&p.UserID, &p.DeviceFingerprint, &public, &profile, &p.Version)
	if err == pgx.ErrNoRows {
		return identity.Principal{}, bad
	}
	if err != nil {
		return identity.Principal{}, err
	}
	key, err := devicecontrol.PublicKey(public)
	if err != nil || profile != devicecontrol.KeyProfile || outbox.Digest(key) != p.DeviceFingerprint || !ed25519.Verify(key, message, proof.Signature) {
		return identity.Principal{}, bad
	}
	p.Kind = identity.Device
	p.DeviceID = proof.DeviceID
	p.Scopes = []string{"device:read", "device:heartbeat", "device:capability"}
	err = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := authorizeDevice(ctx, tx, p, "device:read"); err != nil {
			return err
		}
		seconds, _ := strconv.ParseInt(proof.Timestamp, 10, 64)
		var timely bool
		if err := tx.QueryRow(ctx, `SELECT abs(extract(epoch FROM clock_timestamp())-$1::bigint)<=60`, seconds).Scan(&timely); err != nil {
			return err
		}
		if !timely {
			return bad
		}
		// Remove only this device's expired proofs, in a bounded batch. No proof
		// is deleted while its signed timestamp can still be accepted.
		if _, err := tx.Exec(ctx, `DELETE FROM p11.device_request_nonces WHERE device_id=$1 AND nonce_digest IN (SELECT nonce_digest FROM p11.device_request_nonces WHERE device_id=$1 AND expires_at<clock_timestamp() ORDER BY expires_at LIMIT 128)`, p.DeviceID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO p11.device_request_nonces(device_id,nonce_digest,request_digest,expires_at) VALUES($1,$2,$3,to_timestamp($4::bigint)+interval '61 seconds') ON CONFLICT DO NOTHING`, p.DeviceID, outbox.Digest([]byte(proof.Nonce)), outbox.Digest(message), seconds)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return bad
		}
		return nil
	})
	if err != nil {
		return identity.Principal{}, err
	}
	return p, nil
}
