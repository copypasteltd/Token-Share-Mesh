package payments

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

// SalePolicy is independently registered public product metadata. It never
// selects a new merchant on behalf of a buyer or accepts a caller's amount.
type SalePolicy struct {
	GoodsName              string `json:"goods_name"`
	SKU                    string `json:"sku_id"`
	Mode                   string `json:"mode"`
	Privacy                string `json:"privacy"`
	Collection             string `json:"collection"`
	MaximumWindowSeconds   int64  `json:"maximum_window_seconds"`
	RefundEvidenceRef      string `json:"refund_evidence_ref"`
	RefundEvidenceHash     string `json:"refund_evidence_hash"`
	CommissionEvidenceRef  string `json:"commission_evidence_ref"`
	CommissionEvidenceHash string `json:"commission_evidence_hash"`
	ReturnTargetID         string `json:"return_target_id,omitempty"`
}

func (p SalePolicy) valid() bool {
	return ids.Valid(p.SKU) && p.GoodsName != "" && len(p.GoodsName) <= 128 && p.Collection == "SELLER_DIRECT_NO_FEE" && p.MaximumWindowSeconds >= 60 && p.MaximumWindowSeconds <= 43200 &&
		ids.Valid(p.RefundEvidenceRef) && hashPattern.MatchString(p.RefundEvidenceHash) && ids.Valid(p.CommissionEvidenceRef) && hashPattern.MatchString(p.CommissionEvidenceHash) &&
		(p.ReturnTargetID == "" || ids.Valid(p.ReturnTargetID)) && ((p.Mode == "PLAINTEXT" && p.Privacy == "STANDARD") || (p.Mode == "GPU_RENTAL" && p.Privacy == "STANDARD") ||
		(p.Mode == "E2E" && (p.Privacy == "PLATFORM_BLIND" || p.Privacy == "ATTESTED_CONFIDENTIAL")))
}

type Registration struct {
	CapabilityRef string
	Channel       *Alipay
	Policy        SalePolicy
}

type machineOrder struct {
	ID, Buyer, Seller, Hash, State string
	Version                        int64
	Maximum                        money.Fen
	Deadline, Now                  time.Time
	Contract                       trade.Contract
}

func machineOrderTx(ctx context.Context, tx pgx.Tx, id string) (machineOrder, error) {
	o := machineOrder{ID: id}
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT buyer_id,seller_id,contract_hash,state,version,max_amount_fen,deadline,contract_snapshot,clock_timestamp()
FROM p11.orders WHERE id=$1 FOR UPDATE`, id).Scan(&o.Buyer, &o.Seller, &o.Hash, &o.State, &o.Version, &o.Maximum, &o.Deadline, &raw, &o.Now)
	if e == pgx.ErrNoRows {
		return o, fault.New(fault.NotFound, "Original payment order not found")
	}
	if e != nil {
		return o, e
	}
	if strictjson.Decode(raw, &o.Contract, 65536) != nil {
		return o, ErrConflict
	}
	digest, e := o.Contract.Digest()
	if e != nil || digest != o.Hash || o.Contract.Hash != o.Hash || o.Contract.SellerID != o.Seller || o.Contract.BudgetFen != o.Maximum || o.Buyer == o.Seller {
		return o, ErrConflict
	}
	o.Now = o.Now.UTC().Round(0)
	o.Deadline = o.Deadline.UTC().Round(0)
	return o, nil
}

func requireMachineAuthorityTx(ctx context.Context, tx pgx.Tx, r Registration, buyer string, now time.Time, sale bool) error {
	b := r.Channel.binding
	if !b.ResolvableAt(now) || sale && !b.ValidAt(now) {
		return ErrBinding
	}
	scope, _ := json.Marshal(map[string]string{"payment_profile": Profile, "seller_id": b.Seller, "app_id": b.AppID, "service_id": b.ServiceID,
		"environment": b.Environment, "revision": b.Revision, "authority_subject": b.AuthoritySubject, "signing_key_hash": b.SigningKeyHash, "alipay_key_hash": b.AlipayKeyHash,
		"sku_id": r.Policy.SKU, "mode": r.Policy.Mode, "privacy": r.Policy.Privacy, "collection": r.Policy.Collection})
	proofs := []struct{ id, digest, kind string }{{b.AuthorizationRef, b.AuthorizationHash, "MACHINE_PAY_AUTHORITY_1"}, {b.QualificationRef, b.QualificationHash, "MACHINE_PAY_PERSONAL_SELLER_1"}}
	if sale {
		proofs = append(proofs, struct{ id, digest, kind string }{r.Policy.RefundEvidenceRef, r.Policy.RefundEvidenceHash, "MACHINE_PAY_REFUND_ROUTE_1"}, struct{ id, digest, kind string }{r.Policy.CommissionEvidenceRef, r.Policy.CommissionEvidenceHash, "MACHINE_PAY_COMMISSION_ROUTE_1"})
	}
	seen := map[string]bool{}
	for _, p := range proofs {
		if !ids.Valid(p.id) || seen[p.id] {
			return ErrBinding
		}
		seen[p.id] = true
		var id string
		e := tx.QueryRow(ctx, `SELECT e.id FROM p11.evidence e JOIN p11.users u ON u.id=e.reviewed_by
JOIN p11.role_grants g ON g.user_id=u.id AND g.role_code='PAYMENT_REVIEWER' AND g.scope_ref='self'
WHERE e.id=$1 AND e.digest=$2 AND e.evidence_type=$3 AND e.seller_id=$4 AND e.device_id IS NULL
AND e.decision='APPROVED' AND e.valid_from<=$5 AND e.valid_until>$5 AND e.scope_snapshot @> $6::jsonb
AND u.state='ACTIVE' AND u.id<>$4 AND u.id<>$7 AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at>$5)
ORDER BY g.id LIMIT 1 FOR SHARE OF e,u,g`, p.id, p.digest, p.kind, b.Seller, now, scope, buyer).Scan(&id)
		if e == pgx.ErrNoRows {
			return ErrBinding
		}
		if e != nil {
			return e
		}
	}
	if sale {
		var id string
		ids := []string{b.AuthorizationRef, b.QualificationRef, r.Policy.RefundEvidenceRef, r.Policy.CommissionEvidenceRef}
		e := tx.QueryRow(ctx, `SELECT f.id FROM p11.feature_gates f JOIN p11.users u ON u.id=f.approved_by
JOIN p11.role_grants g ON g.user_id=u.id AND g.role_code='PAYMENT_REVIEWER' AND g.scope_ref='self'
WHERE f.scope_key=$1 AND f.enabled AND u.state='ACTIVE' AND u.id<>$2 AND u.id<>$3
AND f.evidence_ids @> $4::text[] AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at>$5)
ORDER BY g.id LIMIT 1 FOR SHARE OF f,u,g`, "machine-pay:"+b.Digest(), b.Seller, buyer, ids, now).Scan(&id)
		if e == pgx.ErrNoRows {
			return ErrBinding
		}
		if e != nil {
			return e
		}
	}
	return nil
}

func admitMachineSaleTx(ctx context.Context, tx pgx.Tx, o machineOrder, r Registration) error {
	if o.State != "WAITING_PAYMENT" || !o.Now.Before(o.Deadline) || o.Maximum < 1 || o.Maximum > 5000 || r.Channel.binding.Seller != o.Seller ||
		r.Policy.SKU != o.Contract.SKU || r.Policy.Mode != o.Contract.Policy.Mode || r.Policy.Privacy != o.Contract.Policy.Privacy ||
		o.Contract.BuyerFeeFen != 0 || o.Contract.SellerPriceFen < 1 || o.Contract.SellerPriceFen > o.Maximum || o.Contract.Price.Validate() != nil || o.Contract.Price.CommissionBPS == nil || *o.Contract.Price.CommissionBPS != 0 {
		return ErrUnsupported
	}
	if e := requireMachineAuthorityTx(ctx, tx, r, o.Buyer, o.Now, true); e != nil {
		return e
	}
	offer, e := (market.Catalog{}).LoadConfirmedTx(ctx, tx, o.Contract.ListingID, o.Contract.ListingRevision)
	if e != nil {
		return e
	}
	if offer.SellerID != o.Seller || offer.Availability != "AVAILABLE" {
		return ErrBinding
	}
	return nil
}

type machineRecord struct {
	Intent                       Intent
	Binding                      Binding
	Policy                       SalePolicy
	Capability, PrecreateRequest string
	Snapshot                     Snapshot
	Version                      int64
	Updated                      time.Time
}

func loadMachineTx(ctx context.Context, tx pgx.Tx, payment string) (machineOrder, machineRecord, error) {
	var parent string
	if e := tx.QueryRow(ctx, `SELECT order_id FROM p11.payment_machine_contexts WHERE payment_id=$1`, payment).Scan(&parent); e != nil {
		if e == pgx.ErrNoRows {
			return machineOrder{}, machineRecord{}, fault.New(fault.NotFound, "Original payment not found")
		}
		return machineOrder{}, machineRecord{}, e
	}
	o, e := machineOrderTx(ctx, tx, parent)
	if e != nil {
		return o, machineRecord{}, e
	}
	r := machineRecord{}
	var intent, policy, binding []byte
	var scopeHash, intentHash, policyHash string
	e = tx.QueryRow(ctx, `SELECT c.intent_raw,c.intent_digest,c.policy_raw,c.policy_digest,c.scope_hash,c.capability_ref,c.precreate_request_id,
p.channel_binding_snapshot,p.amount_fen,p.state,COALESCE(p.channel_reference,''),COALESCE(p.prepay_reference,''),
COALESCE(c.last_evidence_hash,''),p.version,c.updated_at FROM p11.payment_machine_contexts c JOIN p11.payments p ON p.id=c.payment_id
WHERE c.payment_id=$1 FOR UPDATE OF p,c`, payment).Scan(&intent, &intentHash, &policy, &policyHash, &scopeHash, &r.Capability, &r.PrecreateRequest,
		&binding, &r.Snapshot.Amount, &r.Snapshot.State, &r.Snapshot.ChannelReference, &r.Snapshot.PrepayReference, &r.Snapshot.EvidenceHash, &r.Version, &r.Updated)
	if e != nil {
		return o, r, e
	}
	if outbox.Digest(intent) != intentHash || outbox.Digest(policy) != policyHash || strictjson.Decode(intent, &r.Intent, 8192) != nil || strictjson.Decode(policy, &r.Policy, 8192) != nil || strictjson.Decode(binding, &r.Binding, 8192) != nil ||
		!r.Intent.BoundTo(r.Binding) || r.Intent.Scope.Digest() != scopeHash || r.Intent.Scope.Payment != payment || r.Intent.Scope.Order != o.ID || r.Intent.Scope.Buyer != o.Buyer || r.Intent.Scope.Seller != o.Seller || r.Intent.Scope.ContractHash != o.Hash || r.Intent.Amount != r.Snapshot.Amount {
		return o, r, ErrConflict
	}
	r.Snapshot.Scope = r.Intent.Scope
	r.Snapshot.ObservedAt = r.Updated
	return o, r, nil
}

type Payment struct {
	ID               string    `json:"payment_id"`
	Order            string    `json:"order_id"`
	Seller           string    `json:"seller_id"`
	Version          int64     `json:"version"`
	State            string    `json:"state"`
	Environment      string    `json:"environment"`
	Amount           money.Fen `json:"amount_fen"`
	Settled          money.Fen `json:"settled_fen"`
	ChannelReference string    `json:"channel_reference,omitempty"`
	ChannelActionRef string    `json:"channel_action_ref,omitempty"`
	Updated          time.Time `json:"updated_at"`
}

func (r machineRecord) public() Payment {
	p := Payment{ID: r.Intent.Scope.Payment, Order: r.Intent.Scope.Order, Seller: r.Intent.Scope.Seller, Version: r.Version, State: r.Snapshot.State, Environment: r.Binding.Environment, Amount: r.Intent.Amount, ChannelReference: r.Snapshot.ChannelReference, Updated: r.Updated}
	if p.State == "SETTLED" {
		p.Settled = p.Amount
	}
	if merchantNumber.MatchString(r.Snapshot.PrepayReference) {
		p.ChannelActionRef = r.Snapshot.PrepayReference
	}
	return p
}
func machineEventTx(ctx context.Context, tx pgx.Tx, id string, version int64, kind, receipt string) (string, error) {
	event := ids.New("evt")
	raw, _ := json.Marshal(map[string]string{"payment_id": id, "receipt_ref": receipt})
	_, e := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'PAYMENT',$2,$3,$4,'3.0',$5,'PENDING')`, event, id, version, kind, raw)
	return event, e
}
func requireMachineBuyer(ctx context.Context, tx pgx.Tx, p identity.Principal, o machineOrder, scope string) error {
	if p.UserID != o.Buyer {
		return fault.New(fault.NotFound, "Original payment not found")
	}
	return identity.RequireRoleTx(ctx, tx, p, scope, "BUYER", true)
}
