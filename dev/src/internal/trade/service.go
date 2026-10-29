// Package trade freezes C2C quotes and consumes them into immutable orders.
// These control-plane operations do not reserve GPUs, charge or start inference.
package trade

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"time"
)

type QuoteInput struct {
	ListingID       string        `json:"listing_id"`
	ListingRevision int64         `json:"listing_revision"`
	SKU             string        `json:"sku_id"`
	Policy          market.Policy `json:"policy"`
	Spec            market.Spec   `json:"public_spec"`
	BudgetFen       money.Fen     `json:"budget_fen"`
	Region          string        `json:"desired_region"`
	Quantity        int           `json:"quantity"`
}

type Contract struct {
	SellerID        string            `json:"seller_id"`
	ListingID       string            `json:"listing_id"`
	ListingRevision int64             `json:"listing_revision"`
	DeviceIDs       []string          `json:"device_ids"`
	SKU             string            `json:"sku_id"`
	Policy          market.Policy     `json:"policy"`
	Spec            market.Spec       `json:"public_spec"`
	Price           pricing.Price     `json:"price"`
	Bindings        []market.Binding  `json:"kernel_bindings"`
	BudgetFen       money.Fen         `json:"total_budget_fen"`
	SellerPriceFen  money.Fen         `json:"seller_price_fen"`
	BuyerFeeFen     money.Fen         `json:"buyer_fee_fen"`
	DeadlineSec     int64             `json:"deadline_sec"`
	Retries         int               `json:"same_seller_retries"`
	Evidence        []market.Evidence `json:"evidence_refs"`
	Hash            string            `json:"contract_hash,omitempty"`
	SchemaVersion   string            `json:"contract_schema_version"`
}

func (c Contract) Digest() (string, error) {
	c.Hash = ""
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return outbox.Digest(data), nil
}

type Quote struct {
	ID        string    `json:"quote_id"`
	State     string    `json:"state"`
	Version   int64     `json:"version"`
	Contract  Contract  `json:"contract"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Replayed  bool      `json:"-"`
}

type OrderInput struct {
	QuoteID              string    `json:"quote_id"`
	ContractHash         string    `json:"contract_hash"`
	DisclosureVersion    string    `json:"disclosure_version"`
	DisclosureAcceptedAt time.Time `json:"disclosure_accepted_at"`
	CallerKey            string    `json:"caller_key"`
	ReplacesOrderID      string    `json:"replaces_order_id,omitempty"`
}

type Order struct {
	ID       string `json:"order_id"`
	BuyerID  string `json:"buyer_id"`
	SellerID string `json:"seller_id"`
	Version  int64  `json:"version"`
	State    string `json:"state"`
	// Preserve historical snapshots verbatim as JSON, including legacy price
	// variants. Decoding them into today's new-sale struct would drop fields.
	Contract  json.RawMessage `json:"contract"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Replayed  bool            `json:"-"`
}

type Service struct{ DB *persistence.DB }

func (s Service) authorize(ctx context.Context, p identity.Principal, scope string) func(pgx.Tx) error {
	return func(tx pgx.Tx) error { return identity.RequireRoleTx(ctx, tx, p, scope, "BUYER", true) }
}

func (s Service) CreateQuote(ctx context.Context, p identity.Principal, in QuoteInput, key string) (Quote, error) {
	var quote Quote
	if s.DB == nil {
		return quote, fault.New(fault.Unavailable, "Trade service unavailable")
	}
	if err := p.RequireUser("quote:create"); err != nil {
		return quote, err
	}
	if !ids.Valid(in.ListingID) || in.ListingRevision < 1 || in.Quantity != 1 {
		return quote, fault.New(fault.Invalid, "Invalid quote reference or quantity")
	}
	if _, err := money.New(int64(in.BudgetFen)); err != nil {
		return quote, fault.New(fault.Invalid, "Invalid budget")
	}
	request, _ := json.Marshal(in)
	r, err := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createQuote", Resource: in.ListingID, Key: key, Request: request}, s.authorize(ctx, p, "quote:create"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		offer, err := (market.Catalog{DB: s.DB}).LoadTx(ctx, tx, in.ListingID, in.ListingRevision)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if offer.Availability != "AVAILABLE" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Offer currently unavailable")
		}
		if offer.SellerID == p.UserID {
			return outbox.StoredResult{}, fault.New(fault.Forbidden, "Self-purchase is not supported")
		}
		if in.SKU != offer.SKU || in.Policy != offer.Policy || in.Region != offer.Policy.Region {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Offer policy changed; confirm a new quote")
		}
		if in.Spec.ModelID != offer.Spec.ModelID || in.Spec.WorkflowID != offer.Spec.WorkflowID || in.Spec.WorkflowRevision != offer.Spec.WorkflowRevision {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Model or workflow outside approved offer")
		}
		u := pricing.Usage{}
		if offer.Price.Unit == pricing.Hour {
			if in.Spec.DurationMS < 1 || in.Spec.DurationMS > offer.Spec.DurationMS || in.Spec.InputTokenLimit != 0 || in.Spec.OutputTokenLimit != 0 {
				return outbox.StoredResult{}, fault.New(fault.Invalid, "Invalid rental duration or extra token charge")
			}
			u.BillableMS = &in.Spec.DurationMS
		} else {
			if in.Spec.DurationMS != 0 || in.Spec.InputTokenLimit < 1 || in.Spec.InputTokenLimit > offer.Spec.InputTokenLimit || in.Spec.OutputTokenLimit < 1 || in.Spec.OutputTokenLimit > offer.Spec.OutputTokenLimit {
				return outbox.StoredResult{}, fault.New(fault.Invalid, "Invalid bounded token usage")
			}
			u.InputTokens = &in.Spec.InputTokenLimit
			u.OutputTokens = &in.Spec.OutputTokenLimit
		}
		amount, err := offer.Price.Amount(u)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if amount > in.BudgetFen {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Worst-case charge exceeds confirmed budget")
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return outbox.StoredResult{}, err
		}
		expires := now.Add(15 * time.Minute)
		if offer.ValidUntil.Before(expires) {
			expires = offer.ValidUntil
		}
		for _, e := range offer.Evidence {
			if e.ValidUntil.Before(expires) {
				expires = e.ValidUntil
			}
		}
		if !expires.After(now) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Offer evidence expired")
		}
		devices := make([]string, 0, len(offer.Devices))
		for _, d := range offer.Devices {
			devices = append(devices, d.DeviceID)
		}
		contract := Contract{SellerID: offer.SellerID, ListingID: offer.ListingID, ListingRevision: offer.Revision, DeviceIDs: devices, SKU: offer.SKU, Policy: offer.Policy, Spec: in.Spec, Price: offer.Price, Bindings: offer.Bindings, BudgetFen: in.BudgetFen, SellerPriceFen: amount, BuyerFeeFen: 0, DeadlineSec: offer.DeadlineSec, Retries: 1, Evidence: offer.Evidence, SchemaVersion: "3.0-draft"}
		contract.Hash, err = contract.Digest()
		if err != nil {
			return outbox.StoredResult{}, err
		}
		quote = Quote{ID: ids.New("quo"), State: "VALID", Version: 1, Contract: contract, CreatedAt: now, ExpiresAt: expires}
		body, err := json.Marshal(contract)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO p11.quotes(id,buyer_id,seller_id,listing_id,listing_revision,contract_snapshot,contract_hash,max_amount_fen,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, quote.ID, p.UserID, offer.SellerID, offer.ListingID, offer.Revision, body, contract.Hash, int64(in.BudgetFen), expires, now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		body, err = json.Marshal(quote)
		return outbox.StoredResult{Reference: quote.ID, Body: body, HTTPStatus: 201}, err
	})
	if err != nil {
		return Quote{}, err
	}
	if err = json.Unmarshal(r.Body, &quote); err != nil {
		return Quote{}, err
	}
	quote.Replayed = r.Replayed
	return quote, nil
}

func loadQuote(ctx context.Context, tx pgx.Tx, id, buyer string) (Quote, error) {
	var q Quote
	var raw []byte
	var consumed *string
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT id,contract_snapshot,created_at,expires_at,consumed_by,clock_timestamp() FROM p11.quotes WHERE id=$1 AND buyer_id=$2 FOR UPDATE`, id, buyer).Scan(&q.ID, &raw, &q.CreatedAt, &q.ExpiresAt, &consumed, &now)
	if err == pgx.ErrNoRows {
		return q, fault.New(fault.NotFound, "Quote not found")
	}
	if err != nil {
		return q, err
	}
	if err = json.Unmarshal(raw, &q.Contract); err != nil {
		return q, fault.New(fault.Unavailable, "Quote contract requires recovery")
	}
	if q.Contract.Price.Validate() != nil {
		return q, fault.New(fault.Unavailable, "Quote requires a compatible historical contract reader")
	}
	q.Version = 1
	q.State = "VALID"
	if consumed != nil {
		q.State = "CONSUMED"
	} else if !q.ExpiresAt.After(now) {
		q.State = "EXPIRED"
	}
	return q, nil
}

func (s Service) GetQuote(ctx context.Context, p identity.Principal, id string) (Quote, error) {
	var q Quote
	if s.DB == nil || !ids.Valid(id) {
		return q, fault.New(fault.Invalid, "Invalid quote")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := identity.RequireRoleTx(ctx, tx, p, "quote:read", "BUYER", true); err != nil {
			return err
		}
		var err error
		q, err = loadQuote(ctx, tx, id, p.UserID)
		return err
	})
	return q, err
}

func (s Service) CreateOrder(ctx context.Context, p identity.Principal, in OrderInput, key string) (Order, error) {
	var order Order
	if s.DB == nil {
		return order, fault.New(fault.Unavailable, "Trade service unavailable")
	}
	if err := p.RequireUser("order:create"); err != nil {
		return order, err
	}
	if !ids.Valid(in.QuoteID) || !ids.Valid(in.CallerKey) || in.DisclosureAcceptedAt.IsZero() || in.ReplacesOrderID != "" {
		return order, fault.New(fault.Invalid, "Invalid confirmation or unsupported replacement flow")
	}
	request, _ := json.Marshal(in)
	r, err := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createOrder", Resource: in.QuoteID, Key: key, Request: request}, s.authorize(ctx, p, "order:create"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		q, err := loadQuote(ctx, tx, in.QuoteID, p.UserID)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if q.State != "VALID" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Quote expired or consumed; query the original order")
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return outbox.StoredResult{}, err
		}
		hash, err := q.Contract.Digest()
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if in.ContractHash != q.Contract.Hash || hash != q.Contract.Hash || in.DisclosureVersion != q.Contract.Policy.Disclosure || in.DisclosureAcceptedAt.Before(q.CreatedAt) || in.DisclosureAcceptedAt.After(now) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Confirm the exact current contract and disclosure")
		}
		offer, err := (market.Catalog{DB: s.DB}).LoadTx(ctx, tx, q.Contract.ListingID, q.Contract.ListingRevision)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if offer.Availability != "AVAILABLE" || offer.SellerID != q.Contract.SellerID {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Offer admission changed")
		}
		body, err := json.Marshal(q.Contract)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		order = Order{ID: ids.New("ord"), BuyerID: p.UserID, SellerID: q.Contract.SellerID, Version: 1, State: "WAITING_PAYMENT", Contract: body, CreatedAt: now, UpdatedAt: now}
		_, err = tx.Exec(ctx, `INSERT INTO p11.orders(id,quote_id,buyer_id,seller_id,state,mode,privacy,max_amount_fen,fee_bps,contract_snapshot,contract_hash,deadline,created_at) VALUES($1,$2,$3,$4,'WAITING_PAYMENT',$5,$6,$7,$8,$9,$10,$11,$12)`, order.ID, q.ID, p.UserID, order.SellerID, q.Contract.Policy.Mode, q.Contract.Policy.Privacy, int64(q.Contract.BudgetFen), *q.Contract.Price.CommissionBPS, body, q.Contract.Hash, now.Add(time.Duration(q.Contract.DeadlineSec)*time.Second), now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		facts, _ := json.Marshal(map[string]string{"order_id": order.ID, "seller_id": order.SellerID, "quote_id": q.ID, "contract_hash": q.Contract.Hash})
		_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'ORDER',$2,1,'OrderCreated','3.0',$3,'PENDING')`, ids.New("evt"), order.ID, facts)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		consent, _ := json.Marshal(map[string]any{"disclosure_version": in.DisclosureVersion, "accepted_at": in.DisclosureAcceptedAt, "caller_key": in.CallerKey, "contract_hash": q.Contract.Hash})
		_, err = tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,'ORDER_CONTRACT_CONFIRMED',$3,$4,$5)`, ids.New("audit"), p.UserID, order.ID, ids.New("req"), consent)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		body, err = json.Marshal(order)
		return outbox.StoredResult{Reference: order.ID, Body: body, HTTPStatus: 201}, err
	})
	if err != nil {
		return Order{}, err
	}
	if err = json.Unmarshal(r.Body, &order); err != nil {
		return Order{}, err
	}
	order.Replayed = r.Replayed
	return order, nil
}

func (s Service) GetOrder(ctx context.Context, p identity.Principal, id string) (Order, error) {
	var o Order
	if s.DB == nil || !ids.Valid(id) {
		return o, fault.New(fault.Invalid, "Invalid order reference")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := p.Require("order:read", ""); err != nil {
			return err
		}
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT id,buyer_id,seller_id,version,state,contract_snapshot,created_at FROM p11.orders WHERE id=$1 AND (buyer_id=$2 OR seller_id=$2)`, id, p.UserID).Scan(&o.ID, &o.BuyerID, &o.SellerID, &o.Version, &o.State, &raw, &o.CreatedAt)
		if err == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Order not found")
		}
		if err != nil {
			return err
		}
		role := "BUYER"
		if p.UserID == o.SellerID {
			role = "SELLER"
		}
		if err = identity.RequireRoleTx(ctx, tx, p, "order:read", role, false); err != nil {
			return err
		}
		if !json.Valid(raw) {
			return fault.New(fault.Unavailable, "Order snapshot requires recovery")
		}
		o.Contract = append(json.RawMessage(nil), raw...)
		o.UpdatedAt = o.CreatedAt
		return nil
	})
	return o, err
}
