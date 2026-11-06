package payments

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"sort"
	"strconv"
	"time"
)

type RefundReviewInput struct {
	ExpectedReviewVersion  int64     `json:"expected_review_version"`
	ExpectedOrderVersion   int64     `json:"expected_order_version"`
	ExpectedPaymentVersion int64     `json:"expected_payment_version"`
	Decision               string    `json:"decision"`
	Reason                 string    `json:"review_reason_code"`
	Route                  string    `json:"refund_route_ref,omitempty"`
	Maximum                money.Fen `json:"maximum_refundable_fen,omitempty"`
}
type RefundRevokeInput struct {
	ExpectedReviewVersion int64  `json:"expected_review_version"`
	Reason                string `json:"review_reason_code"`
}
type RefundReviewPage struct {
	Items []RefundApplication `json:"items"`
	Next  *string             `json:"next_cursor"`
}

func normalizeRefundReview(in RefundReviewInput) error {
	if in.ExpectedReviewVersion < 1 || in.ExpectedReviewVersion >= 2147483647 || in.ExpectedOrderVersion < 1 || in.ExpectedOrderVersion > 2147483647 || in.ExpectedPaymentVersion < 1 || in.ExpectedPaymentVersion > 2147483647 || !refundReviewReasons[in.Reason] {
		return ErrIntent
	}
	switch in.Decision {
	case "APPROVED":
		if in.Reason != "UNUSED_BALANCE_CONFIRMED" || !ids.Valid(in.Route) || in.Maximum < 1 || in.Maximum > 5000 {
			return ErrIntent
		}
	case "REJECTED":
		if in.Reason == "UNUSED_BALANCE_CONFIRMED" || in.Route != "" || in.Maximum != 0 {
			return ErrIntent
		}
	default:
		return ErrIntent
	}
	return nil
}
func (s *RefundService) ReviewDetails(ctx context.Context, p identity.Principal, id string) (RefundReview, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundReview{}, e
	}
	defer end()
	if !ids.Valid(id) {
		return RefundReview{}, ErrIntent
	}
	var v RefundReview
	e = s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, paid, a, e := loadRefundApplicationTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = requireRefundReviewerTx(ctx, tx, p, o); e != nil {
			return e
		}
		original, e := s.machine.registration(paid)
		if e != nil {
			return e
		}
		rows, e := originalRefundsTx(ctx, tx, paid.Snapshot)
		if e != nil {
			return e
		}
		held, e := HeldRefunds(paid.Snapshot, rows)
		if e != nil {
			return e
		}
		var consumed money.Fen
		if e = tx.QueryRow(ctx, `SELECT budget_consumed_fen FROM p11.orders WHERE id=$1`, o.ID).Scan(&consumed); e != nil {
			return e
		}
		if consumed < 0 || consumed > paid.Snapshot.Amount {
			return ErrConflict
		}
		routes := []string{}
		for ref, r := range s.registrations {
			if r.OriginalCapability == paid.Capability {
				e = requireRefundRouteTx(ctx, tx, o, original, r, true)
				if e == nil {
					routes = append(routes, ref)
				} else if !refundBusinessDenial(e) {
					return e
				}
			}
		}
		sort.Strings(routes)
		v = RefundReview{RefundApplication: a.RefundApplication, Buyer: a.Buyer, Seller: a.Seller, OrderVersion: o.Version, PaymentVersion: paid.Version, Evidence: append([]string{}, a.Evidence...), Routes: routes, Collected: paid.Snapshot.Amount, Held: held, Maximum: paid.Snapshot.Amount - consumed}
		return nil
	})
	return v, e
}
func (s *RefundService) ListReviews(ctx context.Context, p identity.Principal, after string, limit int) (RefundReviewPage, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundReviewPage{}, e
	}
	defer end()
	if after != "" && !ids.Valid(after) || limit < 1 || limit > 50 {
		return RefundReviewPage{}, ErrIntent
	}
	page := RefundReviewPage{Items: []RefundApplication{}}
	e = s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "refund:review", "PAYMENT_REVIEWER", true); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id FROM p11.refund_applications WHERE id>$1 AND buyer_id<>$2 AND seller_id<>$2 ORDER BY id LIMIT $3`, after, p.UserID, limit+1)
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
			o, _, a, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return e
			}
			if e = requireRefundReviewerTx(ctx, tx, p, o); e != nil {
				return e
			}
			page.Items = append(page.Items, a.RefundApplication)
		}
		return nil
	})
	return page, e
}

// Review authenticates the reviewer as the reviewer. Approval, independently
// authored evidence and native refund reservation/context commit together.
func (s *RefundService) Review(ctx context.Context, p identity.Principal, id string, in RefundReviewInput, key string) (RefundApplication, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundApplication{}, e
	}
	defer end()
	if !ids.Valid(id) || normalizeRefundReview(in) != nil {
		return RefundApplication{}, ErrIntent
	}
	request, _ := json.Marshal(in)
	result, e := (outbox.Idempotency{DB: s.machine.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "decideRefundReview", Resource: id, Key: key, Request: request},
		func(tx pgx.Tx) error {
			o, _, _, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return e
			}
			return requireRefundReviewerTx(ctx, tx, p, o)
		},
		func(tx pgx.Tx) (outbox.StoredResult, error) {
			o, paid, a, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if a.ReviewVersion != in.ExpectedReviewVersion || o.Version != in.ExpectedOrderVersion || paid.Version != in.ExpectedPaymentVersion || a.ReviewState != "PENDING" {
				return outbox.StoredResult{}, ErrConflict
			}
			decision, digest := "", ""
			if in.Decision == "APPROVED" {
				r, ok := s.registrations[in.Route]
				if !ok || r.OriginalCapability != paid.Capability {
					return outbox.StoredResult{}, ErrBinding
				}
				original, e := s.machine.registration(paid)
				if e != nil {
					return outbox.StoredResult{}, e
				}
				if in.Maximum < a.Requested {
					return outbox.StoredResult{}, ErrConflict
				}
				scope := refundRouteScope(original, r)
				for k, v := range map[string]string{"refund_policy": "UNUSED_BALANCE_1", "payment_id": a.Payment, "order_id": a.Order, "buyer_id": a.Buyer, "contract_hash": a.ContractHash, "paid_evidence_hash": paid.Snapshot.EvidenceHash,
					"order_version": strconv.FormatInt(o.Version, 10), "payment_version": strconv.FormatInt(paid.Version, 10), "maximum_refundable_fen": strconv.FormatInt(int64(in.Maximum), 10), "refund_amount_fen": strconv.FormatInt(int64(a.Requested), 10),
					"refund_application_id": id, "application_request_hash": a.RequestHash, "reviewer_id": p.UserID, "review_reason_code": in.Reason} {
					scope[k] = v
				}
				raw := mustRefundScope(scope)
				decision = ids.New("evidence")
				digest = outbox.Digest(raw)
				until := o.Now.Add(10 * time.Minute)
				if r.Channel.route.ValidUntil.Before(until) {
					until = r.Channel.route.ValidUntil
				}
				if !until.After(o.Now) {
					return outbox.StoredResult{}, ErrBinding
				}
				_, e = tx.Exec(ctx, `INSERT INTO p11.evidence(id,evidence_type,seller_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,'MACHINE_PAY_REFUND_DECISION_1',$2,$3,$4,$5,'APPROVED',$6,$7,$8)`, decision, o.Seller, "refund-review:"+id, digest, raw, p.UserID, o.Now, until)
				if e != nil {
					return outbox.StoredResult{}, e
				}
				if _, e = s.createApprovedRefundTx(ctx, tx, o, paid, original, r, id, decision, digest); e != nil {
					return outbox.StoredResult{}, e
				}
			}
			_, e = tx.Exec(ctx, `UPDATE p11.refund_applications SET review_state=$2,review_version=review_version+1,review_reason_code=$3,reviewed_by=$4,decision_evidence_id=NULLIF($5,''),decision_digest=NULLIF($6,''),updated_at=$7 WHERE id=$1`, id, in.Decision, in.Reason, p.UserID, decision, digest, o.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if e = refundApplicationEventTx(ctx, tx, id, a.ReviewVersion+1, "RefundApplication"+in.Decision, p.UserID); e != nil {
				return outbox.StoredResult{}, e
			}
			_, _, updated, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			body, _ := json.Marshal(updated.RefundApplication)
			return outbox.StoredResult{Reference: id, Body: body, HTTPStatus: 200}, nil
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
func (s *RefundService) RevokeReview(ctx context.Context, p identity.Principal, id string, in RefundRevokeInput, key string) (RefundApplication, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return RefundApplication{}, e
	}
	defer end()
	if !ids.Valid(id) || in.ExpectedReviewVersion < 1 || in.ExpectedReviewVersion >= 2147483647 || in.Reason != "AUTHORITY_REVOKED" {
		return RefundApplication{}, ErrIntent
	}
	request, _ := json.Marshal(in)
	result, e := (outbox.Idempotency{DB: s.machine.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "revokeRefundReview", Resource: id, Key: key, Request: request},
		func(tx pgx.Tx) error {
			o, _, _, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return e
			}
			return requireRefundReviewerTx(ctx, tx, p, o)
		},
		func(tx pgx.Tx) (outbox.StoredResult, error) {
			o, _, a, e := loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if a.ReviewState != "APPROVED" || a.ReviewVersion != in.ExpectedReviewVersion {
				return outbox.StoredResult{}, ErrConflict
			}
			tag, e := tx.Exec(ctx, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1 AND digest=$2 AND evidence_type='MACHINE_PAY_REFUND_DECISION_1' AND decision='APPROVED'`, a.DecisionRef, a.DecisionHash)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if tag.RowsAffected() != 1 {
				return outbox.StoredResult{}, ErrConflict
			}
			_, e = tx.Exec(ctx, `UPDATE p11.refund_applications SET review_state='REVOKED',review_version=review_version+1,review_reason_code=$2,revoked_by=$3,updated_at=$4 WHERE id=$1`, id, in.Reason, p.UserID, o.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if e = refundApplicationEventTx(ctx, tx, id, a.ReviewVersion+1, "RefundApplicationApprovalRevoked", p.UserID); e != nil {
				return outbox.StoredResult{}, e
			}
			_, _, a, e = loadRefundApplicationTx(ctx, tx, id)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			body, _ := json.Marshal(a.RefundApplication)
			return outbox.StoredResult{Reference: id, Body: body, HTTPStatus: 200}, nil
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
