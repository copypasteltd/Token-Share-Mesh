package payments

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

// PaymentOption discloses only an independently registered original seller
// capability. No credential reference, authority proof, token or key is public.
type PaymentOption struct {
	Capability string `json:"channel_capability_ref"`
	GoodsName  string `json:"goods_name"`
}
type PaymentOptions struct {
	Order        string          `json:"order_id"`
	Seller       string          `json:"seller_id"`
	ContractHash string          `json:"contract_hash"`
	Version      int64           `json:"expected_order_version"`
	Amount       money.Fen       `json:"amount_fen"`
	Currency     string          `json:"currency"`
	Environment  string          `json:"environment"`
	Items        []PaymentOption `json:"items"`
}

// Options never creates an intent or selects on behalf of the buyer. Retained
// expired registrations remain available to original recovery, not new sale.
// An existing payment requires original lookup instead of a second purchase.
func (s *Machine) Options(ctx context.Context, p identity.Principal, order string) (PaymentOptions, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return PaymentOptions{}, e
	}
	defer end()
	if !ids.Valid(order) {
		return PaymentOptions{}, ErrIntent
	}
	var result PaymentOptions
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:read", "BUYER", true); e != nil {
			return e
		}
		o, e := machineOrderTx(ctx, tx, order)
		if e != nil {
			return e
		}
		if o.Buyer != p.UserID {
			return fault.New(fault.NotFound, "Original payment order not found")
		}
		if o.State != "WAITING_PAYMENT" || !o.Now.Before(o.Deadline) {
			return ErrConflict
		}
		var previous bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.payments WHERE order_id=$1 AND purpose='ORDER' AND purpose_ref=$1)`, order).Scan(&previous); e != nil {
			return e
		}
		if previous {
			return ErrConflict
		}
		result = PaymentOptions{Order: o.ID, Seller: o.Seller, ContractHash: o.Hash, Version: o.Version, Amount: o.Maximum, Currency: "CNY", Items: []PaymentOption{}}
		candidates := make([]string, 0, len(s.registrations))
		for id, r := range s.registrations {
			b := r.Channel.binding
			if b.Seller == o.Seller && b.ValidAt(o.Now) && r.Policy.SKU == o.Contract.SKU && r.Policy.Mode == o.Contract.Policy.Mode && r.Policy.Privacy == o.Contract.Policy.Privacy {
				candidates = append(candidates, id)
			}
		}
		sort.Strings(candidates)
		// Fail boundedly on a misconfigured registry, rather than evaluating an
		// unbounded number of four-proof sale paths while holding the parent lock.
		if len(candidates) > 8 {
			return ErrBinding
		}
		for _, id := range candidates {
			r := s.registrations[id]
			if e = admitMachineSaleTx(ctx, tx, o, r); e != nil {
				var safe *fault.Error
				if errors.Is(e, ErrBinding) || errors.Is(e, ErrUnsupported) || (errors.As(e, &safe) && (safe.Code == fault.Conflict || safe.Code == fault.NotFound)) {
					continue
				}
				return e
			}
			if result.Environment != "" && result.Environment != r.Channel.binding.Environment {
				return ErrBinding
			}
			result.Environment = r.Channel.binding.Environment
			result.Items = append(result.Items, PaymentOption{Capability: id, GoodsName: r.Policy.GoodsName})
		}
		if len(result.Items) == 0 {
			return ErrBinding
		}
		return nil
	})
	return result, e
}

// GetForOrder is read-only original recovery after a lost creation response or
// restart. Absence is NOT permission to create with a new idempotency key.
func (s *Machine) GetForOrder(ctx context.Context, p identity.Principal, order string) (Payment, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Payment{}, e
	}
	defer end()
	if !ids.Valid(order) {
		return Payment{}, ErrIntent
	}
	var result Payment
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:read", "BUYER", true); e != nil {
			return e
		}
		o, e := machineOrderTx(ctx, tx, order)
		if e != nil {
			return e
		}
		if o.Buyer != p.UserID {
			return fault.New(fault.NotFound, "Original payment order not found")
		}
		var payment string
		e = tx.QueryRow(ctx, `SELECT c.payment_id FROM p11.payment_machine_contexts c JOIN p11.payments p ON p.id=c.payment_id
 WHERE c.order_id=$1 AND p.purpose='ORDER' AND p.purpose_ref=$1`, order).Scan(&payment)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original payment not found")
		}
		if e != nil {
			return e
		}
		_, r, e := loadMachineTx(ctx, tx, payment)
		if e == nil {
			result = r.public()
		}
		return e
	})
	return result, e
}

// Challenge returns the SAME committed prepay reference after current sale
// eligibility is checked. GET performs no precreate, query or model request.
func (s *Machine) Challenge(ctx context.Context, p identity.Principal, payment string) (Challenge, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Challenge{}, e
	}
	defer end()
	if !ids.Valid(payment) {
		return Challenge{}, ErrIntent
	}
	var result Challenge
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:create", "BUYER", true); e != nil {
			return e
		}
		o, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		if e = requireMachineBuyer(ctx, tx, p, o, "payment:create"); e != nil {
			return e
		}
		reg, e := s.registration(r)
		if e != nil {
			return e
		}
		if e = admitMachineSaleTx(ctx, tx, o, reg); e != nil {
			return e
		}
		var now time.Time
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return e
		}
		result, e = NewChallenge(r.Snapshot, r.Intent, r.Binding, now)
		return e
	})
	return result, e
}

// GetRecovery returns metadata for this original payment's latest submitted
// proof. It never returns/decrypts a credential, verifies remotely, reopens a
// sale gate or creates another query. Revoked sales remain financially readable.
func (s *Machine) GetRecovery(ctx context.Context, p identity.Principal, payment string) (Recovery, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Recovery{}, e
	}
	defer end()
	if !ids.Valid(payment) {
		return Recovery{}, ErrIntent
	}
	var result Recovery
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:read", "BUYER", true); e != nil {
			return e
		}
		o, _, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		if e = requireMachineBuyer(ctx, tx, p, o, "payment:read"); e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `SELECT id,payment_id,state,expires_at FROM p11.payment_machine_recoveries WHERE payment_id=$1
 ORDER BY created_at DESC,id DESC LIMIT 1 FOR SHARE`, payment).Scan(&result.ID, &result.Payment, &result.State, &result.Expires)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original proof recovery not found")
		}
		return e
	})
	return result, e
}
