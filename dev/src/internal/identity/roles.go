package identity

import (
	"context"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/fault"
)

// RequireRoleTx checks current database facts rather than trusting the role
// names carried by an earlier session. Call it on replays as well as writes.
func RequireRoleTx(ctx context.Context, tx pgx.Tx, p Principal, scope, role string, userSession bool) error {
	if userSession {
		if err := p.RequireUser(scope); err != nil {
			return err
		}
	} else {
		if err := p.Require(scope, ""); err != nil {
			return err
		}
	}
	var state string
	var version int64
	err := tx.QueryRow(ctx, `SELECT state,version FROM p11.users WHERE id=$1 FOR SHARE`, p.UserID).Scan(&state, &version)
	if err == pgx.ErrNoRows || err == nil && (state != "ACTIVE" || version != p.Version) {
		return fault.New(fault.Unauthenticated, "Principal no longer valid")
	}
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM p11.role_grants WHERE user_id=$1 AND role_code=$2 AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, p.UserID, role).Scan(&id)
	if err == pgx.ErrNoRows {
		return fault.New(fault.Forbidden, "Current role required")
	}
	return err
}
