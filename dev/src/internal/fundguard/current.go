// Package fundguard reads current original-order allocations. It imports no
// identity, payment adapter, model or trading service, avoiding authority cycles.
package fundguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const Profile = "P11_ORIGINAL_ORDER_FUNDS_1"
const ChannelProfile = "P11_MACHINE_PAY_PREPAID_1"

type Evidence struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Kind   string `json:"kind"`
}
type Approval struct {
	Scope    map[string]string `json:"scope"`
	Evidence []Evidence        `json:"evidence"`
}
type Grant struct {
	ID, Order, Payment, Buyer, Seller, ContractHash, ScopeHash, BindingHash, Receipt, Environment string
	Amount                                                                                        money.Fen
	ValidUntil                                                                                    time.Time
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func unavailable() error {
	return fault.New(fault.Conflict, "Current verified original order funds required; retain original obligations")
}

// CurrentTx grants no GPU/keys/execution and performs no financial call. Callers
// retain their original order lock and still check device/contract permissions.
// Receipt and allocation are immutable; current accounts, reviewer grants,
// evidence, sale gate, refunds and deadlines are checked again on EVERY use.
func CurrentTx(ctx context.Context, tx pgx.Tx, order, payment, environment string) (Grant, error) {
	var g Grant
	if tx == nil || !ids.Valid(order) || (payment != "" && !ids.Valid(payment)) || (environment != "SANDBOX" && environment != "PRODUCTION") {
		return g, unavailable()
	}
	// A separate parent-lock statement is intentional. In READ COMMITTED the
	// following receipt/refund query gets a new snapshot AFTER any original
	// refund writer has committed. A joined predicate evaluated before waiting
	// on a lock cannot itself rule out an inserted refund (a phantom).
	var parent, isolation string
	e := tx.QueryRow(ctx, `SELECT id,current_setting('transaction_isolation') FROM p11.orders WHERE id=$1 FOR SHARE`, order).Scan(&parent, &isolation)
	if e == pgx.ErrNoRows {
		return g, unavailable()
	}
	if e != nil {
		return g, e
	}
	if isolation != "read committed" {
		return g, unavailable()
	}
	var raw []byte
	var hash string
	var now time.Time
	e = tx.QueryRow(ctx, `SELECT f.id,f.order_id,f.payment_id,f.buyer_id,f.seller_id,f.contract_hash,f.scope_hash,
 f.binding_hash,f.receipt_id,f.environment,f.amount_fen,f.valid_until,f.approval_raw,f.approval_digest,clock_timestamp()
 FROM p11.order_fundings f JOIN p11.orders o ON o.id=f.order_id
 JOIN p11.payments p ON p.id=f.payment_id AND p.order_id=o.id
 JOIN p11.payment_machine_contexts c ON c.payment_id=p.id
 JOIN p11.payment_machine_receipts r ON r.id=f.receipt_id AND r.payment_id=p.id
 WHERE f.order_id=$1 AND ($2='' OR f.payment_id=$2) AND f.environment=$3
 AND f.profile='P11_ORIGINAL_ORDER_FUNDS_1' AND f.scope_hash=c.scope_hash
 AND f.buyer_id=o.buyer_id AND f.seller_id=o.seller_id AND f.contract_hash=o.contract_hash
 AND f.binding_hash=(convert_from(c.intent_raw,'UTF8')::jsonb)->'Scope'->>'BindingHash'
 AND o.state IN ('READY','FULFILLING') AND o.deadline>clock_timestamp() AND f.valid_until>clock_timestamp()
 AND p.state='SETTLED' AND p.provider='ALIPAY' AND p.environment=f.environment
 AND p.purpose='ORDER' AND p.purpose_ref=o.id AND p.product_profile_revision='P11_MACHINE_PAY_PREPAID_1'
 AND p.amount_fen=f.amount_fen AND f.amount_fen=o.max_amount_fen
 AND (p.funds_expires_at IS NULL OR p.funds_expires_at>clock_timestamp())
 AND r.kind='VERIFIED_PAID' AND r.scope_hash=f.scope_hash AND r.channel_reference=p.channel_reference
 AND NOT EXISTS(SELECT 1 FROM p11.refunds x WHERE x.payment_id=p.id AND x.state<>'FAILED')
 FOR SHARE OF f,o,p,c,r`, order, payment, environment).Scan(&g.ID, &g.Order, &g.Payment, &g.Buyer, &g.Seller, &g.ContractHash, &g.ScopeHash, &g.BindingHash, &g.Receipt, &g.Environment, &g.Amount, &g.ValidUntil, &raw, &hash, &now)
	if e == pgx.ErrNoRows {
		return g, unavailable()
	}
	if e != nil {
		return g, e
	}
	var a Approval
	if digest(raw) != hash || strictjson.Decode(raw, &a, 8192) != nil || len(a.Scope) != 13 || a.Scope["payment_profile"] != ChannelProfile || a.Scope["seller_id"] != g.Seller || a.Scope["environment"] != environment || a.Scope["collection"] != "SELLER_DIRECT_NO_FEE" || len(a.Evidence) != 4 {
		return Grant{}, unavailable()
	}
	if e = partiesTx(ctx, tx, g.Buyer, g.Seller, now, &g.ValidUntil); e != nil {
		return Grant{}, e
	}
	scope, _ := json.Marshal(a.Scope)
	refs := make([]string, 0, 4)
	seen := map[string]bool{}
	kinds := []string{"MACHINE_PAY_AUTHORITY_1", "MACHINE_PAY_PERSONAL_SELLER_1", "MACHINE_PAY_REFUND_ROUTE_1", "MACHINE_PAY_COMMISSION_ROUTE_1"}
	for i, proof := range a.Evidence {
		if !ids.Valid(proof.ID) || seen[proof.ID] || proof.Kind != kinds[i] || len(proof.Digest) != 64 {
			return Grant{}, unavailable()
		}
		seen[proof.ID] = true
		refs = append(refs, proof.ID)
		var id string
		var until time.Time
		var roleUntil *time.Time
		e = tx.QueryRow(ctx, `SELECT e.id,e.valid_until,v.expires_at FROM p11.evidence e JOIN p11.users u ON u.id=e.reviewed_by
 JOIN p11.role_grants v ON v.user_id=u.id AND v.role_code='PAYMENT_REVIEWER' AND v.scope_ref='self'
 WHERE e.id=$1 AND e.digest=$2 AND e.evidence_type=$3 AND e.seller_id=$4 AND e.device_id IS NULL
 AND e.decision='APPROVED' AND e.valid_from<=clock_timestamp() AND e.valid_until>clock_timestamp() AND e.scope_snapshot @> $5::jsonb
 AND u.state='ACTIVE' AND u.id<>$4 AND u.id<>$6 AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>clock_timestamp())
 ORDER BY v.id LIMIT 1 FOR SHARE OF e,u,v`, proof.ID, proof.Digest, proof.Kind, g.Seller, scope, g.Buyer).Scan(&id, &until, &roleUntil)
		if e == pgx.ErrNoRows {
			return Grant{}, unavailable()
		}
		if e != nil {
			return Grant{}, e
		}
		clip(&g.ValidUntil, until)
		if roleUntil != nil {
			clip(&g.ValidUntil, *roleUntil)
		}
	}
	var gate string
	var reviewerUntil *time.Time
	e = tx.QueryRow(ctx, `SELECT f.id,v.expires_at FROM p11.feature_gates f JOIN p11.users u ON u.id=f.approved_by
 JOIN p11.role_grants v ON v.user_id=u.id AND v.role_code='PAYMENT_REVIEWER' AND v.scope_ref='self'
 WHERE f.scope_key=$1 AND f.enabled AND f.evidence_ids @> $2::text[] AND u.state='ACTIVE'
 AND u.id<>$3 AND u.id<>$4 AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>clock_timestamp())
 ORDER BY v.id LIMIT 1 FOR SHARE OF f,u,v`, "machine-pay:"+g.BindingHash, refs, g.Buyer, g.Seller).Scan(&gate, &reviewerUntil)
	if e == pgx.ErrNoRows {
		return Grant{}, unavailable()
	}
	if e != nil {
		return Grant{}, e
	}
	if reviewerUntil != nil {
		clip(&g.ValidUntil, *reviewerUntil)
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return Grant{}, e
	}
	if !now.Before(g.ValidUntil) {
		return Grant{}, unavailable()
	}
	return g, nil
}

func clip(bound *time.Time, until time.Time) {
	if bound != nil && until.Before(*bound) {
		*bound = until
	}
}

// PartiesTx verifies existing contract parties; it never fabricates a user
// Principal or credential from stored role data for an external request.
func PartiesTx(ctx context.Context, tx pgx.Tx, buyer, seller string, now time.Time) error {
	return partiesTx(ctx, tx, buyer, seller, now, nil)
}
func partiesTx(ctx context.Context, tx pgx.Tx, buyer, seller string, now time.Time, bound *time.Time) error {
	if tx == nil || now.IsZero() || buyer == seller || !ids.Valid(buyer) || !ids.Valid(seller) {
		return unavailable()
	}
	for i, id := range []string{buyer, seller} {
		role := "BUYER"
		if i == 1 {
			role = "SELLER"
		}
		var found string
		var until *time.Time
		e := tx.QueryRow(ctx, `SELECT u.id,g.expires_at FROM p11.users u JOIN p11.role_grants g ON g.user_id=u.id
 WHERE u.id=$1 AND u.state='ACTIVE' AND g.role_code=$2 AND g.scope_ref='self'
 AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())
 ORDER BY g.id LIMIT 1 FOR SHARE OF u,g`, id, role).Scan(&found, &until)
		if e == pgx.ErrNoRows {
			return unavailable()
		}
		if e != nil {
			return e
		}
		if until != nil {
			clip(bound, *until)
		}
	}
	var id string
	e := tx.QueryRow(ctx, `SELECT id FROM p11.seller_profiles WHERE id=$1 AND state='APPROVED' FOR SHARE`, seller).Scan(&id)
	if e == pgx.ErrNoRows {
		return unavailable()
	}
	return e
}
