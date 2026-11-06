package payments

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"sort"
	"time"
)

type RefundApplicationInput struct {
	ExpectedOrderVersion int64     `json:"expected_order_version"`
	Reason               string    `json:"reason_code"`
	Requested            money.Fen `json:"requested_fen"`
	Evidence             []string  `json:"evidence_refs,omitempty"`
}
type RefundApplication struct {
	Replayed       bool      `json:"-"`
	ID             string    `json:"refund_id"`
	Order          string    `json:"order_id"`
	Payment        string    `json:"payment_id"`
	Version        int64     `json:"version"`
	State          string    `json:"state"`
	Requested      money.Fen `json:"requested_fen"`
	Confirmed      money.Fen `json:"confirmed_fen"`
	Updated        time.Time `json:"updated_at"`
	ReviewState    string    `json:"review_state"`
	ReviewVersion  int64     `json:"review_version"`
	Reason         string    `json:"reason_code"`
	ReviewReason   string    `json:"review_reason_code,omitempty"`
	ReviewRequired bool      `json:"review_required"`
}
type RefundApplicationPage struct {
	Items []RefundApplication `json:"items"`
	Next  *string             `json:"next_cursor"`
}
type RefundReview struct {
	RefundApplication
	Buyer          string    `json:"buyer_id"`
	Seller         string    `json:"seller_id"`
	OrderVersion   int64     `json:"order_version"`
	PaymentVersion int64     `json:"payment_version"`
	Evidence       []string  `json:"evidence_refs"`
	Routes         []string  `json:"refund_route_refs"`
	Collected      money.Fen `json:"collected_fen"`
	Held           money.Fen `json:"held_refund_fen"`
	Maximum        money.Fen `json:"maximum_refundable_fen"`
}
type refundApplicationRecord struct {
	RefundApplication
	Buyer, Seller, ContractHash, RequestHash, DecisionRef, DecisionHash string
	Evidence                                                            []string
	Created                                                             time.Time
}

var refundReasons = map[string]bool{"NO_LONGER_NEEDED": true, "SERVICE_UNAVAILABLE": true, "BILLING_REVIEW": true}
var refundReviewReasons = map[string]bool{"UNUSED_BALANCE_CONFIRMED": true, "INSUFFICIENT_EVIDENCE": true, "OUTSIDE_CONTRACT": true, "DUPLICATE_APPLICATION": true, "REQUIRES_FURTHER_REVIEW": true, "AUTHORITY_REVOKED": true}

func normalizeRefundApplication(in RefundApplicationInput) (RefundApplicationInput, error) {
	if in.ExpectedOrderVersion < 1 || in.ExpectedOrderVersion > 2147483647 || !refundReasons[in.Reason] || in.Requested < 1 || in.Requested > 5000 || len(in.Evidence) > 50 {
		return in, ErrIntent
	}
	in.Evidence = append([]string{}, in.Evidence...)
	sort.Strings(in.Evidence)
	for n, id := range in.Evidence {
		if !ids.Valid(id) || n > 0 && id == in.Evidence[n-1] {
			return in, ErrIntent
		}
	}
	return in, nil
}
func originalRefundPaymentTx(ctx context.Context, tx pgx.Tx, order string) (machineOrder, machineRecord, error) {
	// Lock the parent before selecting its financial child. A subsequent statement
	// sees a refund committed while the caller was waiting for this parent lock.
	o, e := machineOrderTx(ctx, tx, order)
	if e != nil {
		return o, machineRecord{}, e
	}
	var payment string
	e = tx.QueryRow(ctx, `SELECT p.id FROM p11.payments p JOIN p11.payment_machine_contexts c ON c.payment_id=p.id WHERE p.order_id=$1 AND p.purpose='ORDER' AND p.purpose_ref=$1 AND p.state='SETTLED'`, order).Scan(&payment)
	if e == pgx.ErrNoRows {
		return o, machineRecord{}, ErrConflict
	}
	if e != nil {
		return o, machineRecord{}, e
	}
	return loadMachineTx(ctx, tx, payment)
}
func requireRefundParticipantTx(ctx context.Context, tx pgx.Tx, p identity.Principal, o machineOrder) error {
	if p.UserID == o.Buyer {
		return requireMachineBuyer(ctx, tx, p, o, "refund:read")
	}
	if p.UserID == o.Seller {
		return identity.RequireRoleTx(ctx, tx, p, "refund:read", "SELLER", true)
	}
	return fault.New(fault.NotFound, "Original refund not found")
}
func requireRefundReviewerTx(ctx context.Context, tx pgx.Tx, p identity.Principal, o machineOrder) error {
	if e := identity.RequireRoleTx(ctx, tx, p, "refund:review", "PAYMENT_REVIEWER", true); e != nil {
		return e
	}
	if p.UserID == o.Buyer || p.UserID == o.Seller {
		return fault.New(fault.Forbidden, "Independent financial reviewer required")
	}
	return nil
}

// Apply records an aftersale request, not a financial action or approval.
// There is no destination, account, prompt or arbitrary free-text field.
func (s *RefundService) Apply(ctx context.Context, p identity.Principal, order string, input RefundApplicationInput, key string) (RefundApplication, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundApplication{}, e
	}
	defer end()
	if e = p.RequireUser("refund:create"); e != nil {
		return RefundApplication{}, e
	}
	in, e := normalizeRefundApplication(input)
	if e != nil || !ids.Valid(order) {
		return RefundApplication{}, ErrIntent
	}
	request, _ := json.Marshal(in)
	result, e := (outbox.Idempotency{DB: s.machine.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createRefund", Resource: order, Key: key, Request: request},
		func(tx pgx.Tx) error {
			o, e := machineOrderTx(ctx, tx, order)
			if e != nil {
				return e
			}
			return requireMachineBuyer(ctx, tx, p, o, "refund:create")
		},
		func(tx pgx.Tx) (outbox.StoredResult, error) {
			o, paid, e := originalRefundPaymentTx(ctx, tx, order)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if o.Version != in.ExpectedOrderVersion || in.Requested > paid.Snapshot.Amount {
				return outbox.StoredResult{}, ErrConflict
			}
			// One pending review per original order. A lost response/new key returns the
			// same identical application; it cannot create an application flood.
			var existing string
			e = tx.QueryRow(ctx, `SELECT id FROM p11.refund_applications WHERE order_id=$1 AND review_state='PENDING'`, order).Scan(&existing)
			if e == nil {
				_, _, old, e := loadRefundApplicationTx(ctx, tx, existing)
				if e != nil {
					return outbox.StoredResult{}, e
				}
				if old.RequestHash != outbox.Digest(request) {
					return outbox.StoredResult{}, ErrConflict
				}
				body, _ := json.Marshal(old.RefundApplication)
				return outbox.StoredResult{Reference: old.ID, Body: body, HTTPStatus: 201}, nil
			}
			if e != pgx.ErrNoRows {
				return outbox.StoredResult{}, e
			}
			for _, ref := range in.Evidence {
				var found string
				e = tx.QueryRow(ctx, `SELECT id FROM p11.evidence WHERE id=$1 AND scope_snapshot @> $2::jsonb FOR SHARE`, ref, mustRefundScope(map[string]string{"order_id": o.ID, "buyer_id": o.Buyer})).Scan(&found)
				if e == pgx.ErrNoRows {
					return outbox.StoredResult{}, ErrBinding
				}
				if e != nil {
					return outbox.StoredResult{}, e
				}
			}
			id := ids.New("refund")
			refs, _ := json.Marshal(in.Evidence)
			_, e = tx.Exec(ctx, `INSERT INTO p11.refund_applications(id,order_id,payment_id,buyer_id,seller_id,contract_hash,request_raw,request_digest,requested_fen,reason_code,evidence_refs,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`, id, o.ID, paid.Intent.Scope.Payment, o.Buyer, o.Seller, o.Hash, request, outbox.Digest(request), in.Requested, in.Reason, refs, o.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if e = refundApplicationEventTx(ctx, tx, id, 1, "RefundApplicationCreated", p.UserID); e != nil {
				return outbox.StoredResult{}, e
			}
			v := RefundApplication{ID: id, Order: o.ID, Payment: paid.Intent.Scope.Payment, Version: 1, State: "REQUESTED", Requested: in.Requested, Updated: o.Now, ReviewState: "PENDING", ReviewVersion: 1, Reason: in.Reason}
			body, _ := json.Marshal(v)
			return outbox.StoredResult{Reference: id, Body: body, HTTPStatus: 201}, nil
		})
	if e != nil {
		return RefundApplication{}, e
	}
	var v RefundApplication
	if json.Unmarshal(result.Body, &v) != nil {
		return v, ErrConflict
	}
	v.Replayed = result.Replayed
	return v, nil
}
func mustRefundScope(scope map[string]string) []byte { raw, _ := json.Marshal(scope); return raw }
func (s *RefundService) Application(ctx context.Context, p identity.Principal, id string) (RefundApplication, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundApplication{}, e
	}
	defer end()
	if !ids.Valid(id) {
		return RefundApplication{}, ErrIntent
	}
	var v RefundApplication
	e = s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, _, a, e := loadRefundApplicationTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = requireRefundParticipantTx(ctx, tx, p, o); e != nil {
			return e
		}
		v = a.RefundApplication
		return nil
	})
	return v, e
}
func (s *RefundService) ListApplications(ctx context.Context, p identity.Principal, order, after string, limit int) (RefundApplicationPage, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundApplicationPage{}, e
	}
	defer end()
	if !ids.Valid(order) || after != "" && !ids.Valid(after) || limit < 1 || limit > 50 {
		return RefundApplicationPage{}, ErrIntent
	}
	page := RefundApplicationPage{Items: []RefundApplication{}}
	e = s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, e := machineOrderTx(ctx, tx, order)
		if e != nil {
			return e
		}
		if e = requireRefundParticipantTx(ctx, tx, p, o); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id FROM p11.refund_applications WHERE order_id=$1 AND id>$2 ORDER BY id LIMIT $3`, order, after, limit+1)
		if e != nil {
			return e
		}
		var refs []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			refs = append(refs, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(refs) > limit {
			next := refs[limit-1]
			page.Next = &next
			refs = refs[:limit]
		}
		for _, id := range refs {
			_, _, a, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return e
			}
			page.Items = append(page.Items, a.RefundApplication)
		}
		return nil
	})
	return page, e
}
