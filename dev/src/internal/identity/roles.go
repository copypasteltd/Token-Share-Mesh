package identity

import (
	"context"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/fault"
)

// RequireCurrentTx locks user before credential, matching login/logout order.
// Internal trusted principals may omit a credential reference; HTTP adapters
// always bind a verified SessionID or KeyID and cannot accept either from JSON.
func RequireCurrentTx(ctx context.Context, tx pgx.Tx, p Principal) error {
	var state string
	var version int64
	err := tx.QueryRow(ctx, `SELECT state,version FROM p11.users WHERE id=$1 FOR SHARE`, p.UserID).Scan(&state, &version)
	if err == pgx.ErrNoRows || err == nil && (state != "ACTIVE" || version != p.Version) {
		return fault.New(fault.Unauthenticated, "Principal no longer valid")
	}
	if err != nil {
		return err
	}
	if p.Kind == UserSession && p.SessionID != "" {
		var id string
		err = tx.QueryRow(ctx, "SELECT id FROM p11.auth_sessions WHERE id=$1 AND user_id=$2 AND user_version=$3 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE", p.SessionID, p.UserID, p.Version).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Session no longer valid")
		}
		if err != nil {
			return err
		}
	}
	if p.Kind == APIKey && p.KeyID != "" {
		var scopes, routes []string
		err = tx.QueryRow(ctx, "SELECT scopes,route_ids FROM p11.api_keys WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FOR SHARE", p.KeyID, p.UserID).Scan(&scopes, &routes)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Credential no longer valid")
		}
		if err != nil {
			return err
		}
		mapped := map[string]bool{}
		for _, scope := range scopes {
			if v, ok := apiScopes[scope]; ok {
				mapped[v] = true
			}
		}
		for _, scope := range p.Scopes {
			if !mapped[scope] {
				return fault.New(fault.Unauthenticated, "Credential scope changed")
			}
		}
		currentRoutes := map[string]bool{}
		for _, route := range routes {
			currentRoutes[route] = true
		}
		for _, route := range p.RouteIDs {
			if !currentRoutes[route] {
				return fault.New(fault.Unauthenticated, "Credential route changed")
			}
		}
	}
	if p.Kind == Device {
		if p.DeviceID == "" || p.DeviceFingerprint == "" {
			return fault.New(fault.Unauthenticated, "Verified device identity required")
		}
		var id string
		// Keep user -> role -> seller -> device order, shared by seller exit and
		// device management. Take the device write lock directly: concurrent
		// heartbeats must not both hold SHARE then deadlock upgrading to UPDATE.
		err = tx.QueryRow(ctx, `SELECT id FROM p11.role_grants WHERE user_id=$1 AND role_code='SELLER' AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, p.UserID).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Device seller role no longer valid")
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT id FROM p11.seller_profiles WHERE id=$1 AND state IN ('APPROVED','EXITING') FOR SHARE`, p.UserID).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Device seller identity no longer valid")
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT id FROM p11.devices WHERE id=$1 AND seller_id=$2 AND auth_key_digest=$3 AND identity_profile='ED25519_DEVICE_1' AND state NOT IN ('REVOKED','RETIRED','QUARANTINED') FOR UPDATE`, p.DeviceID, p.UserID, p.DeviceFingerprint).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Device identity no longer valid")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RequireRoleTx checks current facts before writes and idempotent replays.
func RequireRoleTx(ctx context.Context, tx pgx.Tx, p Principal, scope, role string, userSession bool) error {
	if userSession {
		if err := p.RequireUser(scope); err != nil {
			return err
		}
	} else if err := p.Require(scope, ""); err != nil {
		return err
	}
	if err := RequireCurrentTx(ctx, tx, p); err != nil {
		return err
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM p11.role_grants WHERE user_id=$1 AND role_code=$2 AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, p.UserID, role).Scan(&id)
	if err == pgx.ErrNoRows {
		return fault.New(fault.Forbidden, "Current role required")
	}
	return err
}
