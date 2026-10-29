// Package seller implements account-owned submissions and future listing
// revisions. No submission grants a role, approves evidence or starts execution.
package seller

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Service struct {
	DB   *persistence.DB
	Fees *FeePolicy
}
type Reason struct {
	Code            string   `json:"reason_code"`
	ExpectedVersion int64    `json:"expected_version"`
	Evidence        []string `json:"evidence_refs,omitempty"`
}
type Application struct {
	Identity   string    `json:"identity_evidence_ref"`
	Rights     []string  `json:"device_rights_evidence_refs"`
	Payee      string    `json:"payee_eligibility_ref"`
	Regions    []string  `json:"regions"`
	Agreement  string    `json:"agreement_version"`
	AcceptedAt time.Time `json:"agreement_accepted_at"`
}
type Profile struct {
	ID          string            `json:"seller_id"`
	DisplayName string            `json:"display_name"`
	State       string            `json:"qualification_state"`
	Notice      string            `json:"public_identity_notice"`
	Regions     []string          `json:"approved_regions"`
	PayeeState  string            `json:"payee_eligibility_state"`
	Version     int64             `json:"version"`
	Evidence    []market.Evidence `json:"evidence_refs"`
	Replayed    bool              `json:"-"`
}
type Consent struct {
	Document    string    `json:"document_version"`
	Purpose     string    `json:"purpose"`
	Scope       string    `json:"scope_ref"`
	Accepted    *bool     `json:"accepted"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

func text(s string, max int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) != s || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func uniqueIDs(a []string, min, max int) bool {
	if len(a) < min || len(a) > max {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		if !ids.Valid(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func recent(t time.Time) bool {
	now := time.Now()
	return !t.IsZero() && t.After(now.Add(-24*time.Hour)) && !t.After(now.Add(5*time.Minute))
}
func validReason(in Reason) bool {
	return ids.Valid(in.Code) && in.ExpectedVersion > 0 && in.ExpectedVersion < 2147483647 && uniqueIDs(in.Evidence, 0, 50)
}

// Current authority is checked before cached replay results. Caller-provided
// profile states, role names and seller identifiers are never accepted.
func (s Service) authorize(ctx context.Context, p identity.Principal, scope, role string) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if err := identity.RequireRoleTx(ctx, tx, p, scope, role, true); err != nil {
			return err
		}
		return nil
	}
}
func accountAuthority(ctx context.Context, p identity.Principal, scope string) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if err := p.RequireUser(scope); err != nil {
			return err
		}
		if err := identity.RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM p11.role_grants WHERE user_id=$1 AND scope_ref='self' AND role_code IN ('BUYER','SELLER') AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 1 FOR SHARE`, p.UserID).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Forbidden, "Current account role required")
		}
		return err
	}
}
func (s Service) command(ctx context.Context, p identity.Principal, op, resource, key string, input any, auth func(pgx.Tx) error, fn func(pgx.Tx) (outbox.StoredResult, error)) (outbox.StoredResult, error) {
	if s.DB == nil {
		return outbox.StoredResult{}, fault.New(fault.Unavailable, "Seller service unavailable")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return outbox.StoredResult{}, err
	}
	return (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: op, Resource: resource, Key: key, Request: raw}, auth, fn)
}
func stored(ref string, status int, v any) (outbox.StoredResult, error) {
	raw, err := json.Marshal(v)
	return outbox.StoredResult{Reference: ref, Body: raw, HTTPStatus: status}, err
}
func event(ctx context.Context, tx pgx.Tx, p identity.Principal, kind, id string, version int64, action string) error {
	return eventFacts(ctx, tx, p, kind, id, version, action, map[string]any{"resource_id": id, "version": version})
}
func reasonEvent(ctx context.Context, tx pgx.Tx, p identity.Principal, kind, id string, version int64, action string, in Reason) error {
	return eventFacts(ctx, tx, p, kind, id, version, action, map[string]any{"resource_id": id, "version": version, "reason_code": in.Code, "evidence_refs": in.Evidence})
}
func eventFacts(ctx context.Context, tx pgx.Tx, p identity.Principal, kind, id string, version int64, action string, safe map[string]any) error {
	facts, _ := json.Marshal(safe)
	if _, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,$3,$4,$5,$6)`, ids.New("aud"), p.UserID, action, id, ids.New("req"), facts); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,$2,$3,$4,$5,'3.0',$6,'PENDING')`, ids.New("evt"), kind, id, version, action, facts)
	return err
}
func profileTx(ctx context.Context, tx pgx.Tx, id string, update bool) (Profile, error) {
	p := Profile{Notice: "个人卖家；申请材料不等于已核验身份或收款资格", Regions: []string{}, Evidence: []market.Evidence{}, PayeeState: "UNVERIFIED"}
	lock := "FOR SHARE"
	if update {
		lock = "FOR UPDATE"
	}
	err := tx.QueryRow(ctx, "SELECT id,display_name,state,approved_regions,version FROM p11.seller_profiles WHERE id=$1 "+lock, id).Scan(&p.ID, &p.DisplayName, &p.State, &p.Regions, &p.Version)
	if err == pgx.ErrNoRows {
		return p, fault.New(fault.NotFound, "Seller profile not found")
	}
	return p, err
}
func (s Service) Profile(ctx context.Context, p identity.Principal) (Profile, error) {
	var v Profile
	if s.DB == nil {
		return v, fault.New(fault.Unavailable, "Seller service unavailable")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := p.RequireUser("seller:read"); err != nil {
			return err
		}
		if err := identity.RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		// Buyer applicants may inspect their own pending profile; no SELLER grant.
		var err error
		v, err = profileTx(ctx, tx, p.UserID, false)
		return err
	})
	return v, err
}
func (s Service) Apply(ctx context.Context, p identity.Principal, in Application, key string) (Profile, error) {
	var v Profile
	if !ids.Valid(in.Identity) || !ids.Valid(in.Payee) || !ids.Valid(in.Agreement) || !uniqueIDs(in.Rights, 1, 50) || len(in.Regions) < 1 || len(in.Regions) > 20 || !recent(in.AcceptedAt) {
		return v, fault.New(fault.Invalid, "Invalid seller application")
	}
	regions := map[string]bool{}
	for _, r := range in.Regions {
		if !text(r, 128) || regions[r] {
			return v, fault.New(fault.Invalid, "Invalid or duplicate application region")
		}
		regions[r] = true
	}
	result, err := s.command(ctx, p, "createSellerApplication", "self", key, in, s.authorize(ctx, p, "seller:apply", "BUYER"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		// Serialize even the first application when a profile does not yet exist.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,5005))", p.UserID); err != nil {
			return outbox.StoredResult{}, err
		}
		_, err := tx.Exec(ctx, "INSERT INTO p11.seller_profiles(id,state) VALUES($1,'DRAFT') ON CONFLICT DO NOTHING", p.UserID)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		profile, err := profileTx(ctx, tx, p.UserID, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if (profile.State != "DRAFT" && profile.State != "NEEDS_EVIDENCE") || profile.Version >= 2147483647 {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Application state requires review")
		}
		profile.State = "SUBMITTED"
		profile.Version++
		claims, _ := json.Marshal(in)
		if _, err = tx.Exec(ctx, "UPDATE p11.seller_profiles SET state='SUBMITTED',version=$2 WHERE id=$1", p.UserID, profile.Version); err != nil {
			return outbox.StoredResult{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO p11.seller_applications(id,seller_id,profile_version,submitted_claims) VALUES($1,$2,$3,$4)`, ids.New("app"), p.UserID, profile.Version, claims); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, "SELLER", p.UserID, profile.Version, "SellerApplicationSubmitted"); err != nil {
			return outbox.StoredResult{}, err
		}
		return stored(p.UserID, 201, profile)
	})
	if err == nil {
		err = json.Unmarshal(result.Body, &v)
		v.Replayed = result.Replayed
	}
	return v, err
}
func (s Service) RecordConsent(ctx context.Context, p identity.Principal, in Consent, key string) (identity.SessionAck, error) {
	var v identity.SessionAck
	allowed := map[string]bool{"TRADE": true, "PRIVACY": true, "LAN": true, "SELECTIVE_DISCLOSURE": true}
	if !ids.Valid(in.Document) || !ids.Valid(in.Scope) || !allowed[in.Purpose] || in.Accepted == nil || !recent(in.ConfirmedAt) {
		return v, fault.New(fault.Invalid, "Explicit versioned consent required")
	}
	result, err := s.command(ctx, p, "recordConsent", in.Scope, key, in, accountAuthority(ctx, p, "identity:consent"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		// Consent is append-only. This receipt alone cannot authorize a trade,
		// selective content disclosure, device access or a channel charge.
		decision := "WITHDRAW"
		if *in.Accepted {
			decision = "ACCEPT"
		}
		v = identity.SessionAck{ID: ids.New("cns"), Version: 1, Status: decision}
		err := tx.QueryRow(ctx, `INSERT INTO p11.consents(id,user_id,policy_id,policy_version,decision,context_ref,purpose,confirmed_at) VALUES($1,$2,$3::text,$3::text,$4,$5,$6,$7) RETURNING created_at`, v.ID, p.UserID, in.Document, decision, in.Scope, in.Purpose, in.ConfirmedAt).Scan(&v.RecordedAt)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, "CONSENT", v.ID, 1, "ConsentRecorded"); err != nil {
			return outbox.StoredResult{}, err
		}
		return stored(v.ID, 200, v)
	})
	if err == nil {
		err = json.Unmarshal(result.Body, &v)
	}
	return v, err
}
func (s Service) Exit(ctx context.Context, p identity.Principal, in Reason, key string) (identity.SessionAck, bool, error) {
	var v identity.SessionAck
	if !validReason(in) {
		return v, false, fault.New(fault.Invalid, "Valid versioned exit reason required")
	}
	result, err := s.command(ctx, p, "requestSellerExit", "self", key, in, accountAuthority(ctx, p, "seller:manage"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		profile, err := profileTx(ctx, tx, p.UserID, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if profile.Version != in.ExpectedVersion || profile.State == "CLOSED" || profile.State == "EXITING" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Seller state changed")
		}
		// Stop NEW admission atomically. Existing orders, money records, leases and
		// resource fences remain intact until their own completion/cleanup flows.
		if _, err = tx.Exec(ctx, "UPDATE p11.seller_profiles SET state='EXITING',version=version+1 WHERE id=$1", p.UserID); err != nil {
			return outbox.StoredResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.listings SET state='PAUSED',version=version+1,updated_at=clock_timestamp() WHERE seller_id=$1 AND state IN ('PUBLISHED','APPROVED','SUBMITTED','REVIEWING')`, p.UserID); err != nil {
			return outbox.StoredResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.devices SET resource_policy=jsonb_set(resource_policy,'{accepting_new_jobs}','false'),version=version+1 WHERE seller_id=$1 AND state NOT IN ('REVOKED','RETIRED')`, p.UserID); err != nil {
			return outbox.StoredResult{}, err
		}
		v = identity.SessionAck{ID: p.UserID, Version: profile.Version + 1, Status: "EXITING", RecordedAt: time.Now().UTC()}
		if err = reasonEvent(ctx, tx, p, "SELLER", p.UserID, v.Version, "SellerExitRequested", in); err != nil {
			return outbox.StoredResult{}, err
		}
		return stored(v.ID, 200, v)
	})
	if err == nil {
		err = json.Unmarshal(result.Body, &v)
	}
	return v, result.Replayed, err
}
