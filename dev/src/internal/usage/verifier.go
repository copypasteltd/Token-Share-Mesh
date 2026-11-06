// Package usage verifies a separately signed meter statement. Engine/device
// counters alone remain unverified. No model, tokenizer or content is loaded.
package usage

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

type Pin struct {
	KeyID        string `json:"key_id"`
	Public       string `json:"public_key"`
	Authority    string `json:"authority_id"`
	Evidence     string `json:"evidence_ref"`
	EvidenceHash string `json:"evidence_digest"`
}
type Verifier struct {
	pins map[string]Pin
	keys map[string]ed25519.PublicKey
}

func New(pins []Pin) (*Verifier, error) {
	if len(pins) < 1 || len(pins) > 16 {
		return nil, fault.New(fault.Invalid, "Explicit independent metering pins required")
	}
	v := &Verifier{pins: map[string]Pin{}, keys: map[string]ed25519.PublicKey{}}
	for _, p := range pins {
		k, e := base64.RawURLEncoding.Strict().DecodeString(p.Public)
		if !ids.Valid(p.KeyID) || !ids.Valid(p.Authority) || !ids.Valid(p.Evidence) || !executionlease.Hash(p.EvidenceHash) || e != nil || len(k) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(k) != p.Public || v.pins[p.KeyID].KeyID != "" {
			return nil, fault.New(fault.Invalid, "Bounded canonical independent metering pin required")
		}
		v.pins[p.KeyID] = p
		v.keys[p.KeyID] = ed25519.PublicKey(append([]byte(nil), k...))
	}
	return v, nil
}

type Verified struct {
	ID     string
	Hash   string
	Totals resultwire.Totals
}

// VerifyTx runs after the original order/job lock. Approval is checked now,
// including both authority and independent reviewer's current roles. Pins
// select keys; they do not create evidence or grant trading permission.
func (v *Verifier) VerifyTx(ctx context.Context, tx pgx.Tx, s resultwire.Statement, d resultwire.Delivery, m executionlease.Manifest, buyer string) (Verified, error) {
	var out Verified
	if v == nil {
		return out, fault.New(fault.Unavailable, "Independent metering is not configured; usage remains unknown")
	}
	p, ok := v.pins[s.KeyID]
	if !ok || s.Evidence != p.Evidence || s.Scope != d.Scope || s.DeliveryHash != resultwire.Digest(d) || s.Candidate.Model != m.ModelDigest || s.Candidate.Template != m.RecipeDigest || s.Candidate.Backend != m.EngineRelease || p.Authority == buyer || p.Authority == m.Seller || s.Verify(v.keys[s.KeyID]) != nil {
		return out, fault.New(fault.Forbidden, "Original independent metering binding rejected")
	}
	var approved, now time.Time
	var reviewer, hash string
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT e.reviewed_by,e.digest,e.scope_snapshot,e.valid_from,clock_timestamp()
FROM p11.evidence e JOIN p11.users a ON a.id=$2 AND a.state='ACTIVE'
JOIN p11.users r ON r.id=e.reviewed_by AND r.state='ACTIVE'
JOIN p11.role_grants ma ON ma.user_id=a.id AND ma.role_code='METER_AUTHORITY' AND ma.scope_ref='self' AND ma.revoked_at IS NULL AND (ma.expires_at IS NULL OR ma.expires_at>clock_timestamp())
JOIN p11.role_grants mr ON mr.user_id=r.id AND mr.role_code='METER_REVIEWER' AND mr.scope_ref='self' AND mr.revoked_at IS NULL AND (mr.expires_at IS NULL OR mr.expires_at>clock_timestamp())
WHERE e.id=$1 AND e.evidence_type='TOKEN_METER_AUTHORITY_1' AND e.decision='APPROVED'
AND e.seller_id IS NULL AND e.device_id IS NULL AND e.valid_from<=clock_timestamp() AND e.valid_until>clock_timestamp()
AND NOT EXISTS(SELECT 1 FROM p11.devices d WHERE d.id=$3 AND d.device_public_key=$4)
FOR SHARE OF e,a,r,ma,mr`, p.Evidence, p.Authority, m.Device, p.Public).Scan(&reviewer, &hash, &raw, &approved, &now)
	if e == pgx.ErrNoRows {
		return out, fault.New(fault.Unavailable, "Current independent metering authority unavailable")
	}
	if e != nil {
		return out, e
	}
	expected := map[string]string{"profile_id": resultwire.UsageProfile, "key_id": p.KeyID, "public_key": p.Public, "authority_id": p.Authority, "model_digest": m.ModelDigest, "tokenizer_digest": s.Candidate.Tokenizer, "template_digest": m.RecipeDigest, "backend_id": m.EngineRelease, "kernel_environment_digest": m.EnvironmentDigest, "counter_profile_id": s.Candidate.CounterProfile}
	var actual map[string]string
	if strictjson.Decode(raw, &actual, 16384) != nil || len(actual) != len(expected) || hash != p.EvidenceHash || reviewer == buyer || reviewer == m.Seller || reviewer == p.Authority {
		return out, fault.New(fault.Forbidden, "Independent metering approval does not match original engine")
	}
	for k, w := range expected {
		if actual[k] != w {
			return out, fault.New(fault.Forbidden, "Independent metering scope differs")
		}
	}
	at, _ := time.Parse(time.RFC3339Nano, s.Recorded)
	end, _ := time.Parse(time.RFC3339Nano, m.HardDeadline)
	complete, _ := time.Parse(time.RFC3339Nano, d.Completed)
	if at.Before(approved) || at.Before(complete) || at.After(now.Add(30*time.Second)) || at.After(end.Add(5*time.Minute)) {
		return out, fault.New(fault.Invalid, "Independent metering timestamp outside original window")
	}
	totals, e := s.Candidate.Normalize()
	if e != nil {
		return out, fault.New(fault.Invalid, "Unsupported original metering counters")
	}
	// Exact equality prevents a meter signature for unrelated output from being
	// attached to an original device's reported result. It is not the trust root.
	if totals.Input != d.InputTokens || totals.Output != d.OutputTokens || totals.Input > m.InputTokenLimit || totals.Output > int64(m.Spec.MaxOutputTokens) {
		return out, fault.New(fault.Conflict, "Verified totals differ from original bounded result")
	}
	out = Verified{ID: ids.New("usage"), Hash: resultwire.Digest(s), Totals: totals}
	statement, _ := json.Marshal(s)
	_, e = tx.Exec(ctx, `INSERT INTO p11.usage_evidence(id,job_id,attempt_id,fencing_token,sequence,tokenizer_profile,model_digest,input_tokens,output_tokens,cached_tokens,status,evidence_hash)
VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,$9,'VERIFIED',$10)`, out.ID, m.Job, m.Attempt, m.Fence, s.Candidate.CounterProfile, m.ModelDigest, totals.Input, totals.Output, s.Candidate.Raw.Cached, out.Hash)
	if e != nil {
		return Verified{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO p11.native_usage_verifications(usage_id,job_id,attempt_id,delivery_digest,statement_raw,statement_digest,authority_evidence_id,authority_evidence_digest,meter_key_id,authority_id,reviewer_id,input_tokens,output_tokens)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, out.ID, m.Job, m.Attempt, s.DeliveryHash, statement, out.Hash, p.Evidence, p.EvidenceHash, p.KeyID, p.Authority, reviewer, totals.Input, totals.Output)
	return out, e
}
