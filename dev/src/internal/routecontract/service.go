// Package routecontract freezes a buyer's original order for standard SDK model
// selection. Resolution is configuration only: it grants no execution or funds.
package routecontract

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
	"reflect"
	"time"
)

const Profile = "CONFIRMED_ROUTE_1"

type Write struct {
	OrderID       string        `json:"confirmed_order_id"`
	SellerID      string        `json:"seller_id"`
	ListingID     string        `json:"listing_id"`
	Revision      int64         `json:"listing_revision"`
	Policy        market.Policy `json:"policy"`
	PerCallFen    money.Fen     `json:"budget_per_call_fen"`
	TotalFen      money.Fen     `json:"total_budget_fen"`
	MaxCalls      int64         `json:"max_call_count"`
	ExpiresAt     time.Time     `json:"expires_at"`
	PaymentID     string        `json:"purchase_authorization_ref"`
	RecipientID   string        `json:"recipient_id,omitempty"`
	RecipientHash string        `json:"recipient_key_hash,omitempty"`
}

type Contract struct {
	ID              string          `json:"route_id"`
	BuyerID         string          `json:"buyer_id"`
	Version         int64           `json:"version"`
	Definition      Write           `json:"definition"`
	State           string          `json:"state"`
	RemainingFen    money.Fen       `json:"remaining_budget_fen"`
	UsedCalls       int64           `json:"used_calls"`
	ContractHash    string          `json:"contract_hash"`
	PublicModel     string          `json:"public_model"`
	ProfileRevision string          `json:"protocol_profile_revision"`
	OrderContract   json.RawMessage `json:"order_contract"`
	Replayed        bool            `json:"-"`
	CurrentDeadline time.Time       `json:"-"`
	// ExistingAdmission checks current original eligibility before NEW-call
	// capacity limits. It is not an execution grant or reservation validation.
	ExistingAdmission bool `json:"-"`
}

type Page struct {
	Items      []Contract `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

type Service struct {
	DB *persistence.DB
	// Must be configured explicitly; no SANDBOX/PRODUCTION fallback or mixing.
	PaymentEnvironment string
}

type facts struct {
	contract       trade.Contract
	raw            []byte
	hash           string
	state          string
	deadline       time.Time
	maximum        money.Fen
	remaining      money.Fen
	paymentCurrent bool
}

func supported(p string) bool {
	return p == string(relay.ChatText) || p == string(relay.ResponsesText) || p == string(relay.MessagesText)
}

func matches(offer market.Service, frozen trade.Contract) bool {
	return offer.SellerID == frozen.SellerID && offer.ListingID == frozen.ListingID && offer.Revision == frozen.ListingRevision &&
		offer.SKU == frozen.SKU && offer.Policy == frozen.Policy && offer.Spec.ModelID == frozen.Spec.ModelID &&
		offer.Spec.WorkflowID == frozen.Spec.WorkflowID && frozen.Spec.InputTokenLimit <= offer.Spec.InputTokenLimit &&
		frozen.Spec.OutputTokenLimit <= offer.Spec.OutputTokenLimit && reflect.DeepEqual(offer.Bindings, frozen.Bindings) &&
		reflect.DeepEqual(offer.Price, frozen.Price)
}

func validate(in Write) error {
	if !ids.Valid(in.OrderID) || !ids.Valid(in.SellerID) || !ids.Valid(in.ListingID) || !ids.Valid(in.PaymentID) || in.Revision < 1 || in.Revision > 2147483647 ||
		in.PerCallFen < 1 || int64(in.PerCallFen) > money.MaxFen || in.TotalFen < in.PerCallFen || int64(in.TotalFen) > money.MaxFen || in.MaxCalls < 1 || in.MaxCalls > 2147483647 || in.ExpiresAt.IsZero() {
		return fault.New(fault.Invalid, "Explicit original order, payment and bounded route limits required")
	}
	// Other modes remain in the design schema, but have no trusted recipient or
	// rental access-grant integration yet. Never downgrade them to plaintext.
	if in.Policy.Mode != "PLAINTEXT" || in.Policy.Privacy != "STANDARD" || !supported(in.Policy.Protocol) || in.RecipientID != "" || in.RecipientHash != "" {
		return fault.New(fault.Invalid, "Route mode or protocol is not implemented")
	}
	return nil
}

func (s Service) original(ctx context.Context, tx pgx.Tx, buyer string, in Write) (facts, error) {
	var f facts
	var seller string
	err := tx.QueryRow(ctx, `SELECT seller_id,state,contract_snapshot,contract_hash,deadline,max_amount_fen,
max_amount_fen-budget_reserved_fen-budget_consumed_fen-budget_unverified_fen
FROM p11.orders WHERE id=$1 AND buyer_id=$2 FOR SHARE`, in.OrderID, buyer).Scan(&seller, &f.state, &f.raw, &f.hash, &f.deadline, &f.maximum, &f.remaining)
	if err == pgx.ErrNoRows {
		return f, fault.New(fault.NotFound, "Original order not found")
	}
	if err != nil {
		return f, err
	}
	if strictjson.Decode(f.raw, &f.contract, 65536) != nil {
		return f, fault.New(fault.Unavailable, "Original contract requires recovery")
	}
	hash, err := f.contract.Digest()
	if err != nil || hash != f.hash || f.contract.Hash != f.hash || f.contract.SellerID != seller || f.contract.BudgetFen != f.maximum {
		return f, fault.New(fault.Unavailable, "Original contract binding requires recovery")
	}
	if seller == buyer || seller != in.SellerID || f.contract.ListingID != in.ListingID || f.contract.ListingRevision != in.Revision || f.contract.Policy != in.Policy || f.maximum != in.TotalFen ||
		f.contract.Price.Unit != pricing.MillionTokens || !ids.Valid(f.contract.Spec.ModelID) || len(f.contract.Bindings) == 0 {
		return f, fault.New(fault.Conflict, "Route must match the original confirmed seller, model, price and policy")
	}
	if s.PaymentEnvironment != "SANDBOX" && s.PaymentEnvironment != "PRODUCTION" {
		return f, fault.New(fault.Unavailable, "Explicit payment environment required for route resolution")
	}
	// This consumes a trusted, persisted channel fact; it never accepts payment
	// state, channel references or endpoint addresses from the HTTP request.
	// AUTHORIZED is deliberately unsupported until the real freeze profile exists.
	var payment string
	var fundsUntil *time.Time
	err = tx.QueryRow(ctx, `SELECT id,funds_expires_at FROM p11.payments WHERE id=$1 AND order_id=$2
AND purpose='ORDER' AND purpose_ref=$2 AND provider='ALIPAY' AND environment=$3
AND state='SETTLED' AND amount_fen>=$4 AND length(channel_reference)>0
AND length(product_profile_revision)>0 AND jsonb_typeof(channel_binding_snapshot)='object'
AND channel_binding_snapshot<>'{}'::jsonb AND (funds_expires_at IS NULL OR funds_expires_at>clock_timestamp())
FOR SHARE`, in.PaymentID, in.OrderID, s.PaymentEnvironment, int64(f.maximum)).Scan(&payment, &fundsUntil)
	if err != nil && err != pgx.ErrNoRows {
		return f, err
	}
	f.paymentCurrent = err == nil
	if fundsUntil != nil && fundsUntil.Before(f.deadline) {
		f.deadline = *fundsUntil
	}
	var refund bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.refunds WHERE payment_id=$1 AND state<>'FAILED')`, in.PaymentID).Scan(&refund); err != nil {
		return f, err
	}
	f.paymentCurrent = f.paymentCurrent && !refund
	return f, nil
}

func (s Service) Create(ctx context.Context, p identity.Principal, in Write, key string) (Contract, error) {
	var c Contract
	if s.DB == nil {
		return c, fault.New(fault.Unavailable, "Route service unavailable")
	}
	if err := p.RequireUser("route:create"); err != nil {
		return c, err
	}
	if err := validate(in); err != nil {
		return c, err
	}
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	request, _ := json.Marshal(in)
	r, err := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createRouteContract", Resource: p.UserID, Key: key, Request: request},
		func(tx pgx.Tx) error { return identity.RequireRoleTx(ctx, tx, p, "route:create", "BUYER", true) },
		func(tx pgx.Tx) (outbox.StoredResult, error) {
			f, err := s.original(ctx, tx, p.UserID, in)
			if err != nil {
				return outbox.StoredResult{}, err
			}
			var now time.Time
			if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
				return outbox.StoredResult{}, err
			}
			if (f.state != "READY" && f.state != "FULFILLING") || !f.paymentCurrent || f.remaining < in.PerCallFen || !in.ExpiresAt.After(now) || in.ExpiresAt.After(f.deadline) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original order, settled funds, capacity or deadline not ready")
			}
			offer, err := (market.Catalog{DB: s.DB}).LoadConfirmedTx(ctx, tx, in.ListingID, in.Revision)
			if err != nil {
				return outbox.StoredResult{}, err
			}
			if !matches(offer, f.contract) || offer.Availability == "UNAVAILABLE" || in.ExpiresAt.After(offer.ValidUntil) {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Confirmed offer qualification or validity changed")
			}
			for _, proof := range offer.Evidence {
				if in.ExpiresAt.After(proof.ValidUntil) {
					return outbox.StoredResult{}, fault.New(fault.Conflict, "Route cannot outlive its qualification evidence")
				}
			}
			c = Contract{ID: ids.New("route"), BuyerID: p.UserID, Version: 1, Definition: in, State: "ACTIVE", RemainingFen: f.remaining, ContractHash: f.hash, PublicModel: f.contract.Spec.ModelID, ProfileRevision: "1", OrderContract: f.raw}
			_, err = tx.Exec(ctx, `INSERT INTO p11.route_contracts(id,order_id,buyer_id,seller_id,contract_hash,protocol_profile,state,expires_at,
binding_profile,definition,order_snapshot,payment_id,created_at) VALUES($1,$2,$3,$4,$5,$6,'ACTIVE',$7,$8,$9,$10,$11,clock_timestamp())`,
				c.ID, in.OrderID, p.UserID, in.SellerID, f.hash, in.Policy.Protocol, in.ExpiresAt, Profile, request, f.raw, in.PaymentID)
			if err != nil {
				var pg *pgconn.PgError
				if errors.As(err, &pg) && pg.Code == "23505" {
					return outbox.StoredResult{}, fault.New(fault.Conflict, "Original order already has a route; query it")
				}
				return outbox.StoredResult{}, err
			}
			facts, _ := json.Marshal(map[string]string{"route_id": c.ID, "order_id": in.OrderID, "seller_id": in.SellerID, "contract_hash": f.hash})
			_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'ROUTE_CONTRACT',$2,1,'RouteContractCreated','3.0',$3,'PENDING')`, ids.New("evt"), c.ID, facts)
			if err != nil {
				return outbox.StoredResult{}, err
			}
			body, err := json.Marshal(c)
			return outbox.StoredResult{Reference: c.ID, Body: body, HTTPStatus: 201}, err
		})
	if err != nil {
		return Contract{}, err
	}
	// Replays re-read current state rather than returning an obsolete ACTIVE view.
	c, err = s.Get(ctx, p, r.Reference)
	c.Replayed = r.Replayed
	return c, err
}

func (s Service) readTx(ctx context.Context, tx pgx.Tx, buyer, id string) (Contract, error) {
	var c Contract
	var definition, snapshot []byte
	var expires time.Time
	err := tx.QueryRow(ctx, `SELECT id,buyer_id,version,state,definition,order_snapshot,contract_hash,expires_at
FROM p11.route_contracts WHERE id=$1 AND buyer_id=$2 AND binding_profile=$3 FOR SHARE`, id, buyer, Profile).
		Scan(&c.ID, &c.BuyerID, &c.Version, &c.State, &definition, &snapshot, &c.ContractHash, &expires)
	if err == pgx.ErrNoRows {
		return c, fault.New(fault.NotFound, "Confirmed route not found")
	}
	if err != nil {
		return c, err
	}
	if strictjson.Decode(definition, &c.Definition, 65536) != nil || validate(c.Definition) != nil {
		return c, fault.New(fault.Unavailable, "Route definition requires recovery")
	}
	f, err := s.original(ctx, tx, buyer, c.Definition)
	if err != nil {
		return c, err
	}
	// Compare semantic JSON because PostgreSQL jsonb rewrites key order/spacing.
	var frozen trade.Contract
	if strictjson.Decode(snapshot, &frozen, 65536) != nil || frozen.Hash != c.ContractHash || f.hash != c.ContractHash {
		return c, fault.New(fault.Unavailable, "Route snapshot requires recovery")
	}
	hash, err := frozen.Digest()
	if err != nil || hash != c.ContractHash {
		return c, fault.New(fault.Unavailable, "Route snapshot digest mismatch")
	}
	c.OrderContract, c.PublicModel, c.ProfileRevision = snapshot, frozen.Spec.ModelID, "1"
	c.CurrentDeadline = expires
	if f.deadline.Before(c.CurrentDeadline) {
		c.CurrentDeadline = f.deadline
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM p11.jobs WHERE route_contract_id=$1 AND invocation_profile='ROUTE_PREPARE_1'`, id).Scan(&c.UsedCalls); err != nil {
		return c, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return c, err
	}
	if c.State == "ACTIVE" && (!expires.After(now) || !f.deadline.After(now)) {
		c.State = "EXPIRED"
	}
	if c.State == "ACTIVE" {
		if !f.paymentCurrent || (f.state != "READY" && f.state != "FULFILLING") {
			c.State = "SUSPENDED"
		} else {
			offer, e := (market.Catalog{DB: s.DB}).LoadConfirmedTx(ctx, tx, c.Definition.ListingID, c.Definition.Revision)
			if e != nil {
				if fault.Public(e).Code != fault.NotFound {
					return c, e
				}
				c.State = "SUSPENDED"
			} else if !matches(offer, frozen) || offer.Availability == "UNAVAILABLE" {
				c.State = "SUSPENDED"
			} else {
				if offer.ValidUntil.Before(c.CurrentDeadline) {
					c.CurrentDeadline = offer.ValidUntil
				}
				for _, proof := range offer.Evidence {
					if proof.ValidUntil.Before(c.CurrentDeadline) {
						c.CurrentDeadline = proof.ValidUntil
					}
				}
			}
		}
	}
	// Exhausted NEW-call capacity must not revoke a prepared call's own hold.
	// Its issuer still rechecks that exact reservation, input and resource owner.
	if c.State == "ACTIVE" {
		c.ExistingAdmission = true
		if f.remaining < c.Definition.PerCallFen || c.UsedCalls >= c.Definition.MaxCalls {
			c.State = "SUSPENDED"
		}
	}
	if c.State == "ACTIVE" {
		c.RemainingFen = f.remaining
	}
	return c, nil
}

func (s Service) Get(ctx context.Context, p identity.Principal, id string) (Contract, error) {
	var c Contract
	if s.DB == nil {
		return c, fault.New(fault.Unavailable, "Route service unavailable")
	}
	if !ids.Valid(id) {
		return c, fault.New(fault.Invalid, "Invalid route reference")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := identity.RequireRoleTx(ctx, tx, p, "route:read", "BUYER", true); err != nil {
			return err
		}
		var err error
		c, err = s.readTx(ctx, tx, p.UserID, id)
		return err
	})
	return c, err
}

func (s Service) List(ctx context.Context, p identity.Principal, cursor string, limit int) (Page, error) {
	page := Page{Items: []Contract{}}
	if s.DB == nil {
		return page, fault.New(fault.Unavailable, "Route service unavailable")
	}
	if (cursor != "" && !ids.Valid(cursor)) || limit < 1 || limit > 100 {
		return page, fault.New(fault.Invalid, "Invalid route page")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := identity.RequireRoleTx(ctx, tx, p, "route:read", "BUYER", true); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM p11.route_contracts WHERE buyer_id=$1 AND binding_profile=$2 AND id>$3 ORDER BY id LIMIT $4`, p.UserID, Profile, cursor, limit+1)
		if err != nil {
			return err
		}
		var list []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			list = append(list, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(list) > limit {
			list = list[:limit]
			page.NextCursor = &list[len(list)-1]
		}
		for _, id := range list {
			c, e := s.readTx(ctx, tx, p.UserID, id)
			if e != nil {
				return e
			}
			page.Items = append(page.Items, c)
		}
		return nil
	})
	return page, err
}

// ResolveTx selects exactly one key-bound original contract. The caller must
// still persist invocation intent and revalidate funds, deadline, budget and
// execution owner in its admission transaction; this value is not a relay Grant.
func (s Service) ResolveTx(ctx context.Context, tx pgx.Tx, p identity.Principal, model string, protocol relay.Profile) (Contract, error) {
	return s.resolveTx(ctx, tx, p, model, protocol, false)
}

// LockForPreparationTx follows identity -> order WRITE -> route/payment/offer.
// Inactive original configuration is returned only so the preparation service
// can resolve an already committed original request, never reissue execution.
func (s Service) LockForPreparationTx(ctx context.Context, tx pgx.Tx, p identity.Principal, model string, protocol relay.Profile) (Contract, error) {
	return s.resolveTx(ctx, tx, p, model, protocol, true)
}

func (s Service) resolveTx(ctx context.Context, tx pgx.Tx, p identity.Principal, model string, protocol relay.Profile, preparation bool) (Contract, error) {
	var c Contract
	if p.Kind != identity.APIKey || p.KeyID == "" {
		return c, fault.New(fault.Forbidden, "Scoped SDK key required")
	}
	if err := identity.RequireRoleTx(ctx, tx, p, "route:invoke", "BUYER", false); err != nil {
		return c, err
	}
	if !ids.Valid(model) || !supported(string(protocol)) || len(p.RouteIDs) < 1 || len(p.RouteIDs) > 50 {
		return c, fault.New(fault.Invalid, "Explicit model and implemented protocol required")
	}
	rows, err := tx.Query(ctx, `SELECT id FROM p11.route_contracts WHERE buyer_id=$1 AND id::text=ANY($2::text[]) AND binding_profile=$3
AND order_snapshot->'public_spec'->>'model_id'=$4 AND protocol_profile=$5 ORDER BY id`, p.UserID, p.RouteIDs, Profile, model, string(protocol))
	if err != nil {
		return c, err
	}
	var candidates []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return c, err
		}
		candidates = append(candidates, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	if len(candidates) == 0 {
		return c, fault.New(fault.NotFound, "No key-bound route for this model and protocol")
	}
	// Include suspended/expired matches in ambiguity detection. Never silently
	// switch to a different original order or seller when one route loses access.
	if len(candidates) != 1 {
		return c, fault.New(fault.Conflict, "Ambiguous model; configure a key for one original route")
	}
	if preparation {
		var order string
		if err = tx.QueryRow(ctx, `SELECT o.id FROM p11.orders o JOIN p11.route_contracts r ON r.order_id=o.id
WHERE r.id=$1 AND r.buyer_id=$2 FOR UPDATE OF o`, candidates[0], p.UserID).Scan(&order); err != nil {
			return c, err
		}
	}
	c, err = s.readTx(ctx, tx, p.UserID, candidates[0])
	if err == nil && !preparation && c.State != "ACTIVE" {
		err = fault.New(fault.Conflict, "Original route is not currently eligible")
	}
	return c, err
}
