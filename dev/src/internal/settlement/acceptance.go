// Package settlement records non-custodial, append-only trade mirrors. An
// earned/provisional amount is never a wallet balance or channel payout.
package settlement

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

type Outcome struct {
	State             string    `json:"state"`
	Qualification     string    `json:"qualification_id,omitempty"`
	Journal           string    `json:"journal_id,omitempty"`
	Gross             money.Fen `json:"gross_fen"`
	Commission        money.Fen `json:"commission_fen"`
	ProvisionalSeller money.Fen `json:"provisional_seller_fen"`
}

// AcceptanceTx uses only server-read original facts. No amount or trust boolean
// enters from a caller. Missing evidence holds the full original reservation.
// The parent order must already be locked before its original job.
func AcceptanceTx(ctx context.Context, tx pgx.Tx, job string) (Outcome, error) {
	out := Outcome{State: "HELD_PENDING_EVIDENCE"}
	var order, buyer, seller, contractHash, accept, usage, deliveryDigest, usageDigest, acceptHash string
	var raw []byte
	var input, output int64
	e := tx.QueryRow(ctx, `SELECT o.id,o.buyer_id,o.seller_id,o.contract_hash,o.contract_snapshot,a.id,u.id,n.delivery_digest,n.statement_digest,a.evidence_hash,o2.input_tokens,o2.output_tokens
FROM p11.jobs j JOIN p11.orders o ON o.id=j.order_id JOIN p11.acceptances a ON a.job_id=j.id AND a.decision='ACCEPT'
JOIN p11.native_result_deliveries d ON d.delivery_id=a.delivery_id AND d.attempt_id=j.current_attempt_id
JOIN p11.native_usage_verifications n ON n.job_id=j.id AND n.attempt_id=j.current_attempt_id AND n.delivery_digest=d.payload_digest
JOIN p11.usage_evidence u ON u.id=n.usage_id AND u.status='VERIFIED' JOIN p11.native_usage_verifications o2 ON o2.usage_id=u.id
WHERE j.id=$1 AND j.state='ACCEPTED' AND j.accepted_delivery_id=a.delivery_id`, job).Scan(&order, &buyer, &seller, &contractHash, &raw, &accept, &usage, &deliveryDigest, &usageDigest, &acceptHash, &input, &output)
	if e == pgx.ErrNoRows {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	var old []byte
	e = tx.QueryRow(ctx, `SELECT safe_outcome FROM p11.accepted_usage_settlements WHERE job_id=$1`, job).Scan(&old)
	if e == nil {
		if strictjson.Decode(old, &out, 4096) != nil {
			return Outcome{}, fault.New(fault.Unavailable, "Original accepted billing outcome requires recovery")
		}
		return out, nil
	}
	if e != pgx.ErrNoRows {
		return out, e
	}
	// Revocation cannot delete a historical billing fact, but an unbilled result
	// must still have current independent metering authority before consuming.
	var evidenceID, authorityID, meterKeyID string
	var approvalRaw, statementRaw []byte
	e = tx.QueryRow(ctx, `SELECT e.id,e.scope_snapshot,n.statement_raw,n.authority_id,n.meter_key_id FROM p11.native_usage_verifications n
 JOIN p11.evidence e ON e.id=n.authority_evidence_id AND e.digest=n.authority_evidence_digest
 JOIN p11.users a ON a.id=n.authority_id AND a.state='ACTIVE'
 JOIN p11.users r ON r.id=n.reviewer_id AND r.state='ACTIVE'
 JOIN p11.role_grants ma ON ma.user_id=a.id AND ma.role_code='METER_AUTHORITY' AND ma.scope_ref='self' AND ma.revoked_at IS NULL AND (ma.expires_at IS NULL OR ma.expires_at>clock_timestamp())
 JOIN p11.role_grants mr ON mr.user_id=r.id AND mr.role_code='METER_REVIEWER' AND mr.scope_ref='self' AND mr.revoked_at IS NULL AND (mr.expires_at IS NULL OR mr.expires_at>clock_timestamp())
 WHERE n.usage_id=$1 AND e.evidence_type='TOKEN_METER_AUTHORITY_1' AND e.reviewed_by=r.id AND e.decision='APPROVED'
 AND e.seller_id IS NULL AND e.device_id IS NULL AND n.authority_id NOT IN ($2,$3)
 AND n.reviewer_id NOT IN ($2,$3,n.authority_id)
 AND e.valid_from<=clock_timestamp() AND e.valid_until>clock_timestamp() FOR SHARE OF e,a,r,ma,mr`, usage, buyer, seller).Scan(&evidenceID, &approvalRaw, &statementRaw, &authorityID, &meterKeyID)
	if e == pgx.ErrNoRows {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	// An approval's scope can change without changing its identifier or digest.
	// Reverify the archived signature and all ten current scope fields before a
	// new charge; historical settlements above remain immutable on revocation.
	var statement resultwire.Statement
	var approval map[string]string
	if strictjson.Decode(statementRaw, &statement, 16384) != nil || strictjson.Decode(approvalRaw, &approval, 16384) != nil || len(approval) != 10 || statement.KeyID != meterKeyID || statement.Evidence != evidenceID || statement.Scope.Job != job || statement.Scope.Order != order || statement.Scope.Seller != seller || statement.DeliveryHash != deliveryDigest || resultwire.Digest(statement) != usageDigest || len(approval["public_key"]) != 43 {
		return out, nil
	}
	public, keyErr := base64.RawURLEncoding.Strict().DecodeString(approval["public_key"])
	if keyErr != nil || len(public) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(public) != approval["public_key"] || statement.Verify(ed25519.PublicKey(public)) != nil {
		return out, nil
	}
	expected := map[string]string{"profile_id": resultwire.UsageProfile, "key_id": meterKeyID, "public_key": approval["public_key"], "authority_id": authorityID, "model_digest": statement.Candidate.Model, "tokenizer_digest": statement.Candidate.Tokenizer, "template_digest": statement.Candidate.Template, "backend_id": statement.Candidate.Backend, "kernel_environment_digest": statement.Scope.Environment, "counter_profile_id": statement.Candidate.CounterProfile}
	for key, value := range expected {
		if approval[key] != value {
			return out, nil
		}
	}
	totals, counterErr := statement.Candidate.Normalize()
	if counterErr != nil || totals.Input != input || totals.Output != output {
		return out, nil
	}
	var c trade.Contract
	if strictjson.Decode(raw, &c, 65536) != nil || c.Hash != contractHash || c.Price.Validate() != nil || c.Price.Unit != pricing.MillionTokens || c.Policy.Mode != "PLAINTEXT" || c.Policy.Privacy != "STANDARD" || c.BuyerFeeFen != 0 {
		return out, nil
	}
	// Financial recovery uses original verified allocated facts; gate expiry does
	// not erase earned work. A pending/unknown refund blocks new consumption.
	var funded bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.order_fundings f JOIN p11.payments p ON p.id=f.payment_id
WHERE f.order_id=$1 AND f.buyer_id=$2 AND f.seller_id=$3 AND f.contract_hash=$4 AND p.state='SETTLED'
AND NOT EXISTS(SELECT 1 FROM p11.refunds r WHERE r.order_id=$1 AND r.state<>'FAILED'))`, order, buyer, seller, contractHash).Scan(&funded); e != nil {
		return out, e
	}
	if !funded {
		out.State = "HELD_FINANCIAL_REVIEW"
		return out, nil
	}
	var previousInput, previousOutput, previousGross int64
	var mixed bool
	if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(input_tokens),0)::bigint,COALESCE(sum(output_tokens),0)::bigint,COALESCE(sum(gross_fen),0)::bigint FROM p11.accepted_usage_settlements WHERE order_id=$1`, order).Scan(&previousInput, &previousOutput, &previousGross); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.trade_qualifications q WHERE q.order_id=$1 AND NOT EXISTS(SELECT 1 FROM p11.accepted_usage_settlements s WHERE s.qualification_id=q.id))`, order).Scan(&mixed); e != nil {
		return out, e
	}
	if mixed {
		return out, nil
	}
	if input < 0 || output < 0 || previousInput < 0 || previousOutput < 0 || input > money.MaxFen-previousInput || output > money.MaxFen-previousOutput {
		return out, fault.New(fault.Conflict, "Original cumulative usage exceeds safe arithmetic bounds")
	}
	totalInput, totalOutput := input+previousInput, output+previousOutput
	total, e := c.Price.Amount(pricing.Usage{InputTokens: &totalInput, OutputTokens: &totalOutput})
	if e != nil {
		return out, e
	}
	if int64(total) < previousGross {
		return out, fault.New(fault.Conflict, "Original cumulative charge cannot decrease")
	}
	gross := money.Fen(int64(total) - previousGross)
	// Commission also uses the order cumulative rounding, preventing per-call
	// rounding from manufacturing extra fees. Channel costs remain UNKNOWN.
	split, e := c.Price.SplitRetained(total)
	if e != nil {
		return out, e
	}
	previousSplit, e := c.Price.SplitRetained(money.Fen(previousGross))
	if e != nil {
		return out, e
	}
	commission := split.CommissionFen - previousSplit.CommissionFen
	if commission < 0 || commission > gross {
		return out, fault.New(fault.Conflict, "Original cumulative commission is not conserved")
	}
	out = Outcome{State: "QUALIFIED_CHANNEL_COST_UNKNOWN", Qualification: ids.New("qual"), Gross: gross, Commission: commission, ProvisionalSeller: gross - commission}
	evidence := resultwire.Digest(struct{ Acceptance, Delivery, Usage, Contract string }{acceptHash, deliveryDigest, usageDigest, contractHash})
	_, e = tx.Exec(ctx, `INSERT INTO p11.trade_qualifications(id,order_id,source_type,source_id,gross_fen,evidence_hash) VALUES($1,$2,'ACCEPTANCE',$3,$4,$5)`, out.Qualification, order, accept, int64(gross), evidence)
	if e != nil {
		return Outcome{}, e
	}
	if gross > 0 {
		out.Journal = ids.New("journal")
		_, e = tx.Exec(ctx, `INSERT INTO p11.journal_transactions(id,order_id,source_type,source_id) VALUES($1,$2,'P11_ACCEPTED_USAGE_1',$3)`, out.Journal, order, out.Qualification)
		if e != nil {
			return Outcome{}, e
		}
		lines := []struct {
			code   string
			amount money.Fen
		}{{"SERVICE_CONSUMED_MIRROR", gross}, {"SELLER_EARNED_PROVISIONAL_MIRROR", -(gross - commission)}, {"PLATFORM_COMMISSION_ACCRUED_MIRROR", -commission}}
		number := 0
		for _, line := range lines {
			if line.amount == 0 {
				continue
			}
			number++
			var account string
			e = tx.QueryRow(ctx, `INSERT INTO p11.ledger_accounts(id,account_code,classification) VALUES($1,$2,'NON_CUSTODIAL_MIRROR') ON CONFLICT(account_code) DO NOTHING RETURNING id`, ids.New("account"), line.code).Scan(&account)
			if e == pgx.ErrNoRows {
				e = tx.QueryRow(ctx, `SELECT id FROM p11.ledger_accounts WHERE account_code=$1 AND classification='NON_CUSTODIAL_MIRROR' AND currency='CNY' FOR SHARE`, line.code).Scan(&account)
			}
			if e != nil {
				return Outcome{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO p11.journal_entries(id,journal_id,account_id,amount_fen,line_number) VALUES($1,$2,$3,$4,$5)`, ids.New("entry"), out.Journal, account, int64(line.amount), number); e != nil {
				return Outcome{}, e
			}
		}
	}
	safe, _ := json.Marshal(out)
	var journal *string
	if out.Journal != "" {
		journal = &out.Journal
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.accepted_usage_settlements(id,job_id,order_id,acceptance_id,usage_id,qualification_id,journal_id,input_tokens,output_tokens,gross_fen,commission_fen,provisional_seller_fen,evidence_hash,safe_outcome) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, ids.New("bill"), job, order, accept, usage, out.Qualification, journal, input, output, int64(gross), int64(commission), int64(out.ProvisionalSeller), evidence, safe)
	if e != nil {
		return Outcome{}, e
	}
	_, e = budget.CompleteTx(ctx, tx, budget.Scope{OrderID: order, BuyerID: buyer, SellerID: seller, ContractHash: contractHash}, job, out.Qualification)
	return out, e
}
