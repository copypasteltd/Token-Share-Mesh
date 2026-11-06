package endpointregistry

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

// selector returns no key bytes and takes no directory lock. The immutable
// selector is used only to choose the current role lock, before any row lock.
func (s *Service) selector(ctx context.Context, p identity.Principal, recipient string) (endpointwire.Bundle, error) {
	var b endpointwire.Bundle
	if e := p.Require("recipient:read", ""); e != nil {
		return b, e
	}
	if !ids.Valid(recipient) {
		return b, fault.New(fault.Invalid, "Original public recipient required")
	}
	e := s.DB.Pool.QueryRow(ctx, `SELECT owner_id,endpoint_id,role FROM p11.endpoint_public_bundles WHERE recipient_id=$1 AND owner_id=$2`, recipient, p.UserID).Scan(&b.Owner, &b.Endpoint, &b.Role)
	if e == pgx.ErrNoRows {
		return b, fault.New(fault.NotFound, "Public endpoint not found")
	}
	return b, e
}

// verifyLineage validates historical signatures at the original registration
// time. A past valid rotation remains historical proof after either key expires;
// current usability is separately represented by summary and future grants.
func verifyLineage(ctx context.Context, tx pgx.Tx, row stored) error {
	for n := 0; n < MaximumVersions; n++ {
		if row.view.Previous == "" {
			if row.view.Bundle.Version != "1" {
				break
			}
			return nil
		}
		if row.view.Rotation == nil {
			break
		}
		old, e := load(ctx, tx, row.view.Previous, false)
		if e != nil {
			return e
		}
		if row.view.Rotation.Verify(old.view.Bundle, row.registered) != nil || old.registered.After(row.registered) || old.deviceFingerprint != nil && (row.deviceFingerprint == nil || *old.deviceFingerprint != *row.deviceFingerprint) {
			break
		}
		row = old
	}
	return fault.New(fault.Unavailable, "Original signed endpoint history requires recovery")
}

func (s *Service) Get(ctx context.Context, p identity.Principal, recipient string) (Record, error) {
	var result Record
	if e := configured(s); e != nil {
		return result, e
	}
	selector, e := s.selector(ctx, p, recipient)
	if e != nil {
		return result, e
	}
	e = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := authorize(ctx, tx, p, "recipient:read"); e != nil {
			return e
		}
		if e := authorizeBundle(ctx, tx, p, selector, false); e != nil {
			return e
		}
		row, e := load(ctx, tx, recipient, false)
		if e != nil {
			return e
		}
		if row.view.Bundle.Owner != selector.Owner || row.view.Bundle.Endpoint != selector.Endpoint || row.view.Bundle.Role != selector.Role {
			return fault.New(fault.Unavailable, "Original public endpoint selector requires recovery")
		}
		if e = verifyLineage(ctx, tx, row); e != nil {
			return e
		}
		now, e := dbTime(ctx, tx)
		if e != nil {
			return e
		}
		result = summary(row, now)
		return nil
	})
	return result, e
}

func (s *Service) List(ctx context.Context, p identity.Principal, cursor string) (Page, error) {
	result := Page{Items: []Record{}}
	if e := configured(s); e != nil {
		return result, e
	}
	if cursor != "" && !ids.Valid(cursor) {
		return result, fault.New(fault.Invalid, "Original endpoint cursor required")
	}
	e := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := authorize(ctx, tx, p, "recipient:read"); e != nil {
			return e
		}
		allowed := []string{}
		if p.Kind == identity.Device {
			allowed = append(allowed, "SELLER")
		} else {
			// Lock actual roles in a stable order before reading any endpoint rows.
			rows, e := tx.Query(ctx, `SELECT role_code FROM p11.role_grants WHERE user_id=$1 AND role_code IN ('BUYER','SELLER') AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY role_code,id FOR SHARE`, p.UserID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var r string
				if e = rows.Scan(&r); e != nil {
					rows.Close()
					return e
				}
				allowed = append(allowed, r)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		if len(allowed) == 0 {
			return fault.New(fault.Forbidden, "Current endpoint role required")
		}
		// Latest means maximal retained version, including REVOKED. It must never
		// select an older ACTIVE row as a fallback after revocation.
		rows, e := tx.Query(ctx, `SELECT b.recipient_id FROM p11.endpoint_public_bundles b WHERE b.owner_id=$1 AND b.role=ANY($2::text[]) AND ($3::text='' OR (b.role='SELLER' AND b.endpoint_id=$3)) AND b.recipient_id>$4 AND NOT EXISTS(SELECT 1 FROM p11.endpoint_public_bundles n WHERE n.owner_id=b.owner_id AND n.endpoint_id=b.endpoint_id AND n.role=b.role AND n.version>b.version) ORDER BY b.recipient_id LIMIT $5`, p.UserID, allowed, p.DeviceID, cursor, PageSize+1)
		if e != nil {
			return e
		}
		idsFound := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			idsFound = append(idsFound, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(idsFound) > PageSize {
			idsFound = idsFound[:PageSize]
			last := idsFound[len(idsFound)-1]
			result.Next = &last
		}
		now, e := dbTime(ctx, tx)
		if e != nil {
			return e
		}
		for _, id := range idsFound {
			row, e := load(ctx, tx, id, false)
			if e != nil {
				return e
			}
			if e = verifyLineage(ctx, tx, row); e != nil {
				return e
			}
			result.Items = append(result.Items, summary(row, now))
		}
		return nil
	})
	return result, e
}

// GetForOrder exposes only public metadata for the original seller/device of a
// frozen C2C E2E order. This is discovery, not a payment or content-plane grant.
func (s *Service) GetForOrder(ctx context.Context, p identity.Principal, order, recipient string) (Record, error) {
	var result Record
	if e := configured(s); e != nil {
		return result, e
	}
	if !ids.Valid(order) || !ids.Valid(recipient) || p.Kind != identity.UserSession || !ids.Valid(p.SessionID) {
		return result, fault.New(fault.Invalid, "Current buyer session and original order/recipient required")
	}
	e := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "recipient:read", "BUYER", true); e != nil {
			return e
		}
		var seller, state, hash string
		var raw []byte
		var deadline time.Time
		e := tx.QueryRow(ctx, `SELECT seller_id,state,contract_hash,contract_snapshot,deadline FROM p11.orders WHERE id=$1 AND buyer_id=$2 FOR SHARE`, order, p.UserID).Scan(&seller, &state, &hash, &raw, &deadline)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original order not found")
		}
		if e != nil {
			return e
		}
		var c trade.Contract
		if strictjson.Decode(raw, &c, 65536) != nil {
			return fault.New(fault.Unavailable, "Original C2C contract requires recovery")
		}
		digest, e := c.Digest()
		if e != nil || digest != hash || c.Hash != hash || c.SellerID != seller || seller == p.UserID {
			return fault.New(fault.Unavailable, "Original C2C contract binding requires recovery")
		}
		now, e := dbTime(ctx, tx)
		if e != nil {
			return e
		}
		if c.Policy.Mode != "E2E" || c.Policy.Privacy != "PLATFORM_BLIND" || !now.Before(deadline) || (state != "WAITING_PAYMENT" && state != "READY" && state != "FULFILLING" && state != "DELIVERY_PENDING") {
			return fault.New(fault.Conflict, "Current original platform-blind order required for endpoint discovery")
		}
		var deviceID, fingerprint string
		e = tx.QueryRow(ctx, `SELECT device_id,device_fingerprint FROM p11.endpoint_public_bundles WHERE recipient_id=$1 AND owner_id=$2 AND role='SELLER'`, recipient, seller).Scan(&deviceID, &fingerprint)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original seller endpoint not found")
		}
		if e != nil {
			return e
		}
		if !contractDevice(c, deviceID) {
			return fault.New(fault.NotFound, "Endpoint is outside the original confirmed devices")
		}
		// Follow current seller -> role -> profile -> device order. Directory locks
		// come last; neither listing identity nor a caller-provided seller can swap it.
		var valid bool
		e = tx.QueryRow(ctx, `SELECT state='ACTIVE' FROM p11.users WHERE id=$1 FOR SHARE`, seller).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return fault.New(fault.Conflict, "Original seller is no longer current")
		}
		var role string
		e = tx.QueryRow(ctx, `SELECT role_code FROM p11.role_grants WHERE user_id=$1 AND role_code='SELLER' AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, seller).Scan(&role)
		if e == pgx.ErrNoRows {
			return fault.New(fault.Conflict, "Original seller role is no longer current")
		}
		if e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `SELECT state='APPROVED' FROM p11.seller_profiles WHERE id=$1 FOR SHARE`, seller).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return fault.New(fault.Conflict, "Original seller is no longer approved")
		}
		e = tx.QueryRow(ctx, `SELECT auth_key_digest=$3 AND identity_profile='ED25519_DEVICE_1' AND state NOT IN ('REVOKED','RETIRED','QUARANTINED') FROM p11.devices WHERE id=$1 AND seller_id=$2 FOR SHARE`, deviceID, seller, fingerprint).Scan(&valid)
		if e == pgx.ErrNoRows || e == nil && !valid {
			return fault.New(fault.Conflict, "Original provider device identity is no longer current")
		}
		if e != nil {
			return e
		}
		row, e := load(ctx, tx, recipient, false)
		if e != nil {
			return e
		}
		if e = verifyLineage(ctx, tx, row); e != nil {
			return e
		}
		if row.view.Bundle.Owner != seller || row.view.Bundle.Role != "SELLER" || row.view.Bundle.Endpoint != deviceID {
			return fault.New(fault.Unavailable, "Original seller public bundle requires recovery")
		}
		result = summary(row, now)
		return nil
	})
	return result, e
}

func contractDevice(c trade.Contract, device string) bool {
	listed, bound := false, false
	for _, id := range c.DeviceIDs {
		if id == device {
			listed = true
		}
	}
	for _, binding := range c.Bindings {
		if binding.DeviceID == device {
			bound = true
		}
	}
	return listed && bound
}
