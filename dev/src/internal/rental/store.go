package rental

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

type originalOrder struct {
	ID, Buyer, Seller, Hash, State, Mode, Privacy string
	Version                                       int64
	Maximum                                       money.Fen
	Deadline, Now                                 time.Time
	Contract                                      trade.Contract
}

func lockOrder(ctx context.Context, tx pgx.Tx, id, buyer string) (originalOrder, error) {
	o := originalOrder{ID: id}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT buyer_id,seller_id,contract_hash,state,mode,privacy,version,max_amount_fen,deadline,contract_snapshot,clock_timestamp()
FROM p11.orders WHERE id=$1 AND ($2='' OR buyer_id=$2) FOR UPDATE`, id, buyer).Scan(&o.Buyer, &o.Seller, &o.Hash, &o.State, &o.Mode, &o.Privacy, &o.Version, &o.Maximum, &o.Deadline, &raw, &o.Now)
	if err == pgx.ErrNoRows {
		return o, fault.New(fault.NotFound, "Original rental order not found")
	}
	if err != nil {
		return o, err
	}
	if strictjson.Decode(raw, &o.Contract, 65536) != nil {
		return o, fault.New(fault.Unavailable, "Original rental contract requires recovery")
	}
	digest, err := o.Contract.Digest()
	if err != nil || digest != o.Hash || o.Contract.Hash != o.Hash || o.Contract.SellerID != o.Seller || o.Mode != "GPU_RENTAL" || o.Privacy != "STANDARD" || o.Contract.Policy.Mode != o.Mode || o.Contract.Policy.Privacy != o.Privacy || o.Contract.Price.Validate() != nil || o.Contract.Price.Unit != pricing.Hour || o.Maximum != o.Contract.BudgetFen || o.Buyer == o.Seller {
		return o, fault.New(fault.Unavailable, "Original hourly contract is not supported by this authority profile")
	}
	o.Now = o.Now.Round(0).UTC()
	return o, nil
}

func (o originalOrder) admit() error {
	if (o.State != "READY" && o.State != "FULFILLING") || !o.Now.Before(o.Deadline) {
		return fault.New(fault.Conflict, "Original rental order is not eligible for work")
	}
	return nil
}

func (o originalOrder) binding(device, environment string) (market.Binding, error) {
	var result market.Binding
	n := 0
	for _, b := range o.Contract.Bindings {
		if b.DeviceID == device && b.EnvironmentRef == environment {
			result = b
			n++
		}
	}
	allowed := false
	for _, d := range o.Contract.DeviceIDs {
		if d == device {
			allowed = true
		}
	}
	if n != 1 || !allowed {
		return market.Binding{}, fault.New(fault.Conflict, "Device and environment must be explicitly bound by the original contract")
	}
	return result, nil
}

// This preliminary lookup only finds the immutable parent. It grants nothing;
// all authorization is repeated after the parent WRITE lock, before any child
// lock. A device SHARE lock is never held while waiting on this order lock.
func lockSessionOrder(ctx context.Context, tx pgx.Tx, id string, p identity.Principal, reader bool) (originalOrder, error) {
	var order, buyer, seller, device string
	err := tx.QueryRow(ctx, `SELECT order_id,buyer_id,seller_id,device_id FROM p11.rental_authority_contexts WHERE session_id=$1`, id).Scan(&order, &buyer, &seller, &device)
	if err == pgx.ErrNoRows {
		return originalOrder{}, fault.New(fault.NotFound, "Original rental session not found")
	}
	if err != nil {
		return originalOrder{}, err
	}
	if p.Kind == identity.Device {
		if p.UserID != seller || p.DeviceID != device {
			return originalOrder{}, fault.New(fault.NotFound, "Original rental session not found")
		}
	} else if p.Kind != identity.UserSession || (p.UserID != buyer && (!reader || p.UserID != seller)) {
		return originalOrder{}, fault.New(fault.NotFound, "Original rental session not found")
	}
	o, err := lockOrder(ctx, tx, order, "")
	if err == nil && (o.Buyer != buyer || o.Seller != seller) {
		err = fault.New(fault.Unavailable, "Original rental ownership requires recovery")
	}
	return o, err
}

func requireReaderRoleTx(ctx context.Context, tx pgx.Tx, p identity.Principal, session string) error {
	var buyer, seller string
	err := tx.QueryRow(ctx, `SELECT buyer_id,seller_id FROM p11.rental_authority_contexts WHERE session_id=$1`, session).Scan(&buyer, &seller)
	if err == pgx.ErrNoRows {
		return fault.New(fault.NotFound, "Original rental session not found")
	}
	if err != nil {
		return err
	}
	role := "BUYER"
	if p.UserID == seller {
		role = "SELLER"
	} else if p.UserID != buyer {
		return fault.New(fault.NotFound, "Original rental session not found")
	}
	return identity.RequireRoleTx(ctx, tx, p, "rental:read", role, true)
}

func (s *Service) authorizeSession(ctx context.Context, p identity.Principal, id, scope string, device bool) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if !device {
			if err := identity.RequireRoleTx(ctx, tx, p, scope, "BUYER", true); err != nil {
				return err
			}
		} else if p.Kind != identity.Device || p.Require(scope, "") != nil {
			return fault.New(fault.Forbidden, "Original signed rental device required")
		}
		if _, err := lockSessionOrder(ctx, tx, id, p, false); err != nil {
			return err
		}
		if device {
			return identity.RequireDeviceReadTx(ctx, tx, p, scope)
		}
		return nil
	}
}

func (s *Service) fundingTx(ctx context.Context, tx pgx.Tx, o originalOrder, payment string, now time.Time) (Funding, error) {
	f := Funding{Order: o.ID, Buyer: o.Buyer, Seller: o.Seller, Environment: s.environment}
	var expires *time.Time
	err := tx.QueryRow(ctx, `SELECT id,amount_fen,funds_expires_at FROM p11.payments
WHERE order_id=$1 AND purpose='ORDER' AND purpose_ref=$1 AND provider='ALIPAY' AND environment=$2
AND ($3='' OR id=$3) AND state='SETTLED' AND amount_fen>=$4 AND length(channel_reference)>0
AND length(product_profile_revision)>0 AND jsonb_typeof(channel_binding_snapshot)='object'
AND channel_binding_snapshot<>'{}'::jsonb AND (funds_expires_at IS NULL OR funds_expires_at>clock_timestamp()) FOR SHARE`, o.ID, s.environment, payment, o.Maximum).Scan(&f.Payment, &f.Amount, &expires)
	if err == pgx.ErrNoRows {
		return f, fault.New(fault.Conflict, "Resolved original Alipay funds required; query the original payment")
	}
	if err != nil {
		return f, err
	}
	var refund bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.refunds WHERE payment_id=$1 AND order_id=$2 AND state<>'FAILED')`, f.Payment, o.ID).Scan(&refund); err != nil {
		return f, err
	}
	if refund {
		return f, fault.New(fault.Conflict, "Original payment refund requires reconciliation before more work")
	}
	// The reference names the independently persisted channel result, not a
	// Boolean supplied by this request. Actual channel verification precedes it.
	f.Evidence = f.Payment
	f.ValidUntil = o.Deadline
	if expires != nil {
		f.ValidUntil = minTime(f.ValidUntil, *expires)
	}
	if !now.Before(f.ValidUntil) {
		return f, fault.New(fault.Conflict, "Original rental funding expired")
	}
	return f, nil
}

func loadCapabilityTx(ctx context.Context, tx pgx.Tx, b market.Binding, seller string, m *Manifest) error {
	var profile, state, reported string
	var accepting, online bool
	err := tx.QueryRow(ctx, `SELECT c.resource_id,c.engine_kind,c.engine_release,c.model_digest,c.recipe_digest,
d.identity_profile,d.state,d.reported_resource_state,COALESCE(d.resource_policy->>'accepting_new_jobs'='true',false),
COALESCE(d.last_seen_at>clock_timestamp()-interval '45 seconds',false)
FROM p11.capabilities c JOIN p11.devices d ON d.id=c.device_id AND d.seller_id=c.seller_id
WHERE c.id=$1 AND c.device_id=$2 AND c.seller_id=$3 AND c.kernel_environment_ref=$4
AND c.kernel_environment_digest=$5 AND c.state='QUALIFIED' AND c.mode='GPU_RENTAL'
AND c.privacy='STANDARD' AND c.protocol_profile=$6 FOR SHARE OF c,d`, b.CapabilityID, b.DeviceID, seller, b.EnvironmentRef, b.EnvironmentDigest, m.Protocol).
		Scan(&m.Resource, &m.Engine, &m.Release, &m.ModelHash, &m.RecipeHash, &profile, &state, &reported, &accepting, &online)
	if err == pgx.ErrNoRows {
		return fault.New(fault.Conflict, "Original rental capability unavailable")
	}
	if err != nil {
		return err
	}
	if profile != "ED25519_DEVICE_1" || state != "ONLINE" || reported != "IDLE" || !accepting || !online {
		return fault.New(fault.Conflict, "Current signed idle rental device required")
	}
	return nil
}

func lockResource(ctx context.Context, tx pgx.Tx, resource, seller, device string) (ResourceSnapshot, error) {
	r := ResourceSnapshot{Resource: resource, Seller: seller, Device: device}
	err := tx.QueryRow(ctx, `SELECT l.lock_generation::text,l.phase,coalesce(l.owner_session_id,''),coalesce(l.owner_attempt_id,'')
FROM p11.resource_locks l JOIN p11.resources r ON r.id=l.resource_id
WHERE l.resource_id=$1 AND r.seller_id=$2 AND r.device_id=$3 FOR UPDATE OF l`, resource, seller, device).
		Scan(&r.Generation, &r.Phase, &r.SessionOwner, &r.AttemptOwner)
	if err == pgx.ErrNoRows {
		return r, fault.New(fault.Conflict, "Explicit original physical resource record required")
	}
	return r, err
}

type sessionRecord struct {
	Session                         Session
	Capability, Connection, Payment string
	ManifestRaw                     []byte
}

func loadSessionTx(ctx context.Context, tx pgx.Tx, id string, o originalOrder) (sessionRecord, error) {
	var r sessionRecord
	var raw, contractRaw []byte
	err := tx.QueryRow(ctx, `SELECT s.authority_snapshot,c.capability_id,c.connection_ref,c.funding_payment_id,c.manifest_raw,c.initial_contract_raw
FROM p11.rental_sessions s JOIN p11.rental_authority_contexts c ON c.session_id=s.id
WHERE s.id=$1 AND s.order_id=$2 AND c.buyer_id=$3 AND c.seller_id=$4 AND s.authority_profile=$5 FOR UPDATE OF s`, id, o.ID, o.Buyer, o.Seller, AuthorityProfile).
		Scan(&raw, &r.Capability, &r.Connection, &r.Payment, &r.ManifestRaw, &contractRaw)
	if err == pgx.ErrNoRows {
		return r, fault.New(fault.NotFound, "Original rental authority record not found")
	}
	if err != nil {
		return r, err
	}
	if strictjson.Decode(raw, &r.Session, 65536) != nil {
		return r, fault.New(fault.Unavailable, "Rental snapshot requires original recovery")
	}
	a, _ := json.Marshal(r.Session.Contract)
	// Raw contract was generated once by this profile and is immutable. Do not
	// substitute JSONB-serialized manifest/contract bytes for its original hash.
	if !bytes.Equal(a, contractRaw) || r.Session.ID != id || r.Session.Contract.Order != o.ID || r.Session.Contract.Buyer != o.Buyer || r.Session.Contract.Seller != o.Seller || r.Session.Contract.ContractHash != o.Hash || outbox.Digest(r.ManifestRaw) != r.Session.Contract.ManifestHash || r.Session.revision(r.Session.Version) != nil {
		return r, fault.New(fault.Unavailable, "Original rental context requires recovery")
	}
	return r, nil
}

func insertSessionTx(ctx context.Context, tx pgx.Tx, v Session, b market.Binding, manifest []byte, payment, connection string) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	price, err := json.Marshal(v.Contract.Price)
	if err != nil {
		return err
	}
	contract, err := json.Marshal(v.Contract)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.rental_sessions(id,order_id,seller_id,device_id,resource_id,resource_lock_generation,state,cleanup_state,
rate_fen_per_minute,budget_fen,terms_revision,reserved_until,version,created_at,authority_profile,authority_snapshot,hourly_price_snapshot)
VALUES($1,$2,$3,$4,$5,$6::bigint,$7,$8,NULL,$9,$10,$11,$12,$13,$14,$15,$16)`, v.ID, v.Contract.Order, v.Contract.Seller, v.Contract.Device, v.Contract.Resource, v.Generation, v.State, v.Cleanup, v.Budget, v.Terms, v.ReservedUntil, v.Version, v.Created, AuthorityProfile, raw, price)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.rental_authority_contexts(session_id,order_id,buyer_id,seller_id,device_id,resource_id,resource_lock_generation,
capability_id,environment_revision,kernel_environment_digest,manifest_ref,manifest_raw,manifest_hash,connection_ref,initial_contract_raw,
initial_contract_digest,order_contract_hash,funding_payment_id,payment_environment,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7::bigint,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, v.ID, v.Contract.Order, v.Contract.Buyer, v.Contract.Seller, v.Contract.Device, v.Contract.Resource, v.Generation, b.CapabilityID, v.Contract.Environment, v.Contract.EnvironmentHash, v.Contract.Manifest, manifest, v.Contract.ManifestHash, connection, contract, outbox.Digest(contract), v.Contract.ContractHash, payment, v.Contract.PaymentEnvironment, v.Created)
	return err
}

func reservePhysicalTx(ctx context.Context, tx pgx.Tx, v Session, previous string) error {
	tag, err := tx.Exec(ctx, `UPDATE p11.resource_locks SET phase='RESERVED',owner_session_id=$2,owner_attempt_id=NULL,
lock_generation=$3::bigint,expires_at=$4,version=version+1 WHERE resource_id=$1 AND phase='FREE'
AND owner_session_id IS NULL AND owner_attempt_id IS NULL AND lock_generation=$5::bigint`, v.Contract.Resource, v.ID, v.Generation, v.ReservedUntil, previous)
	if err == nil && tag.RowsAffected() != 1 {
		err = fault.New(fault.Conflict, "Original physical slot changed before reservation")
	}
	return err
}

func updateSessionTx(ctx context.Context, tx pgx.Tx, old, next Session) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE p11.rental_sessions SET state=$3,cleanup_state=$4,budget_fen=$5,terms_revision=$6,
prepare_until=$7,buyer_ready_until=$8,started_at=$9,ends_at=$10,stopped_at=$11,version=$12,authority_snapshot=$13
WHERE id=$1 AND version=$2 AND authority_profile=$14`, old.ID, old.Version, next.State, next.Cleanup, next.Budget, next.Terms, timePointer(next.PrepareUntil), timePointer(next.BuyerReadyUntil), timePointer(next.Started), timePointer(next.HardDeadline), timePointer(next.Stopped), next.Version, raw, AuthorityProfile)
	if err == nil && tag.RowsAffected() != 1 {
		err = fault.New(fault.Conflict, "Original rental version changed")
	}
	return err
}

func updatePhysicalTx(ctx context.Context, tx pgx.Tx, v Session) error {
	phase := "QUARANTINED"
	expires := v.Stopped
	switch v.State {
	case "RESERVED":
		phase = "RESERVED"
		expires = v.ReservedUntil
	case "PREPARING":
		phase = "RESERVED"
		expires = v.PrepareUntil
	case "READY_WAIT_BUYER":
		phase = "RESERVED"
		expires = v.BuyerReadyUntil
	case "ACTIVE":
		phase = "ACTIVE"
		expires = v.HardDeadline
	case "STOPPING", "FAULTED":
		phase = "STOPPING"
	case "CLEANING":
		phase = "CLEANING"
	case "QUARANTINED":
	default:
		return fault.New(fault.Conflict, "Physical release requires separate native cleanup authority")
	}
	tag, err := tx.Exec(ctx, `UPDATE p11.resource_locks SET phase=$4,expires_at=$5,version=version+1
WHERE resource_id=$1 AND owner_session_id=$2 AND owner_attempt_id IS NULL AND lock_generation=$3::bigint`, v.Contract.Resource, v.ID, v.Generation, phase, timePointer(expires))
	if err == nil && tag.RowsAffected() != 1 {
		err = fault.New(fault.Conflict, "Original rental physical owner changed")
	}
	return err
}

func recordHistoryTx(ctx context.Context, tx pgx.Tx, v Session, actor, operation, event string) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	id := ids.New("evt")
	payload, err := json.Marshal(map[string]any{"session_id": v.ID, "order_id": v.Contract.Order, "device_id": v.Contract.Device, "resource_id": v.Contract.Resource, "resource_lock_generation": v.Generation, "state": v.State, "cleanup_state": v.Cleanup, "state_digest": outbox.Digest(raw), "manifest_hash": v.Contract.ManifestHash, "terms_revision": strconv.FormatInt(v.Terms, 10)})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'RENTAL_SESSION',$2,$3,$4,'1.0',$5,'PENDING')`, id, v.ID, v.Version, event, payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.rental_session_history(session_id,version,operation_id,actor_id,state_raw,state_digest,event_id,recorded_at)
VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp())`, v.ID, v.Version, operation, actor, raw, outbox.Digest(raw), id)
	return err
}

func (s *Service) currentWorkTx(ctx context.Context, tx pgx.Tx, o originalOrder, r sessionRecord) (Funding, ResourceSnapshot, error) {
	if err := o.admit(); err != nil {
		return Funding{}, ResourceSnapshot{}, err
	}
	f, err := s.fundingTx(ctx, tx, o, r.Payment, o.Now)
	if err != nil {
		return Funding{}, ResourceSnapshot{}, err
	}
	_, err = (market.Catalog{DB: s.db}).LoadRentalConfirmedTx(ctx, tx, o.Contract.ListingID, o.Contract.ListingRevision,
		market.RentalOccupancy{Session: r.Session.ID, Order: o.ID, Buyer: o.Buyer, Seller: o.Seller, Device: r.Session.Contract.Device, Resource: r.Session.Contract.Resource, Generation: r.Session.Generation})
	if err != nil {
		return Funding{}, ResourceSnapshot{}, err
	}
	resource, err := lockResource(ctx, tx, r.Session.Contract.Resource, o.Seller, r.Session.Contract.Device)
	if err != nil {
		return Funding{}, resource, err
	}
	if !resource.owned(r.Session) {
		return Funding{}, resource, fault.New(fault.Conflict, "Original rental physical owner required")
	}
	var held bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.budget_reservations WHERE order_id=$1 AND purpose_ref=$2
AND accounting_profile='ORDER_CAPACITY_1' AND purpose_kind='RENTAL_SESSION' AND state='RESERVED' AND amount_fen=$3)`, o.ID, r.Session.ID, r.Session.Budget).Scan(&held)
	if err == nil && !held {
		err = fault.New(fault.Conflict, "Original full rental budget hold required")
	}
	return f, resource, err
}

// Readiness references are not trusted merely because they look like IDs.
// They must already be independently corroborated, approved, current evidence
// for this exact nonce/session/environment/generation and actor. Device
// submissions alone never create an APPROVED row or an operator role here.
func readinessTx(ctx context.Context, tx pgx.Tx, r sessionRecord, actor, connection, readiness, kind string, now time.Time) (ReadyProof, error) {
	v := r.Session
	if !ids.Valid(connection) || !ids.Valid(readiness) || connection == readiness {
		return ReadyProof{}, fault.New(fault.Invalid, "Two distinct original readiness evidence records required")
	}
	scope, _ := json.Marshal(map[string]string{"profile_id": "P11_RENTAL_READINESS_EVIDENCE_1", "session_id": v.ID, "order_id": v.Contract.Order, "buyer_id": v.Contract.Buyer, "seller_id": v.Contract.Seller, "device_id": v.Contract.Device, "resource_id": v.Contract.Resource, "resource_lock_generation": v.Generation, "environment_revision": v.Contract.Environment, "kernel_environment_digest": v.Contract.EnvironmentHash, "manifest_hash": v.Contract.ManifestHash, "connection_ref": r.Connection, "actor_id": actor})
	for _, proof := range []struct{ id, kind string }{{connection, "RENTAL_CONNECTION"}, {readiness, kind}} {
		var id string
		err := tx.QueryRow(ctx, `SELECT e.id FROM p11.evidence e JOIN p11.users u ON u.id=e.reviewed_by
JOIN p11.role_grants g ON g.user_id=u.id AND g.role_code='OPERATOR' AND g.scope_ref='self'
WHERE e.id=$1 AND e.evidence_type=$2 AND e.decision='APPROVED' AND e.seller_id=$3 AND e.device_id=$4
AND e.scope_snapshot @> $5::jsonb AND e.created_at>=$6 AND e.valid_from<=$7 AND e.valid_until>$7
AND u.state='ACTIVE' AND u.id<>$8 AND u.id<>$9 AND g.revoked_at IS NULL
AND (g.expires_at IS NULL OR g.expires_at>$7) ORDER BY g.id LIMIT 1 FOR SHARE OF e,u,g`, proof.id, proof.kind, v.Contract.Seller, v.Contract.Device, scope, v.Created, now, v.Contract.Buyer, v.Contract.Seller).Scan(&id)
		if err == pgx.ErrNoRows {
			return ReadyProof{}, fault.New(fault.Conflict, "Independently corroborated original readiness evidence required")
		}
		if err != nil {
			return ReadyProof{}, err
		}
	}
	return ReadyProof{Environment: v.Contract.Environment, Generation: v.Generation, Connection: connection, Readiness: readiness, At: now}, nil
}
