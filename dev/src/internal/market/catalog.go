// Package market reads versioned C2C offers. Qualification is a database-backed,
// combination-specific decision; parsing a template never enables a sale.
package market

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"strings"
	"time"
	"unicode/utf8"
)

type Policy struct {
	Mode       string `json:"service_mode"`
	Privacy    string `json:"privacy_profile"`
	Region     string `json:"region"`
	Protocol   string `json:"protocol_profile_id"`
	Acceptance string `json:"acceptance_policy_id"`
	Disclosure string `json:"disclosure_version"`
}

type Spec struct {
	ModelID          string `json:"model_id,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	WorkflowRevision int64  `json:"workflow_revision,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	InputTokenLimit  int64  `json:"input_token_limit,omitempty"`
	OutputTokenLimit int64  `json:"output_token_limit,omitempty"`
}

type Binding struct {
	DeviceID          string `json:"device_id"`
	CapabilityID      string `json:"capability_id"`
	EnvironmentRef    string `json:"kernel_environment_ref"`
	EnvironmentDigest string `json:"kernel_environment_digest"`
}

type DeviceDisplay struct {
	DeviceID  string `json:"device_id"`
	ModelName string `json:"model_name"`
}

// Terms is the approved public template stored in listing_versions. It contains
// public limits and identifiers only, never prompts, secrets or result URLs.
type Terms struct {
	SKU               string          `json:"sku_id"`
	SellerDisplayName string          `json:"seller_display_name"`
	Policy            Policy          `json:"policy"`
	Spec              Spec            `json:"public_spec"`
	Price             pricing.Price   `json:"price"`
	Bindings          []Binding       `json:"kernel_bindings"`
	Devices           []DeviceDisplay `json:"device_displays"`
	DeadlineSec       int64           `json:"deadline_sec"`
}

func label(s string) bool {
	return strings.TrimSpace(s) == s && utf8.ValidString(s) && utf8.RuneCountInString(s) > 0 && utf8.RuneCountInString(s) <= 128 && !strings.ContainsAny(s, "\x00\r\n\t")
}

func (t Terms) Validate() error {
	if !ids.Valid(t.SKU) || !label(t.SellerDisplayName) || !label(t.Policy.Region) || !ids.Valid(t.Policy.Protocol) || !ids.Valid(t.Policy.Acceptance) || !ids.Valid(t.Policy.Disclosure) || t.DeadlineSec < 1 || t.DeadlineSec > 86400 || len(t.Bindings) < 1 || len(t.Bindings) > 50 || len(t.Devices) < 1 || len(t.Devices) > 50 {
		return fault.New(fault.Invalid, "Invalid public offer terms")
	}
	if err := t.Price.Validate(); err != nil {
		return err
	}
	if (t.Policy.Mode == "GPU_RENTAL" && (t.Policy.Privacy != "STANDARD" || t.Price.Unit != pricing.Hour || t.Spec.DurationMS < 1 || t.Spec.DurationMS > t.DeadlineSec*1000)) ||
		(t.Policy.Mode == "PLAINTEXT" && (t.Policy.Privacy != "STANDARD" || t.Price.Unit != pricing.MillionTokens)) ||
		(t.Policy.Mode == "E2E" && (t.Policy.Privacy != "PLATFORM_BLIND" || t.Price.Unit != pricing.MillionTokens)) ||
		(t.Policy.Mode != "PLAINTEXT" && t.Policy.Mode != "E2E" && t.Policy.Mode != "GPU_RENTAL") {
		return fault.New(fault.Invalid, "Unsupported mode, privacy or price combination")
	}
	if t.Price.Unit == pricing.MillionTokens && (!ids.Valid(t.Spec.ModelID) || t.Spec.InputTokenLimit < 1 || t.Spec.OutputTokenLimit < 1 || t.Spec.InputTokenLimit > 9007199254740991 || t.Spec.OutputTokenLimit > 9007199254740991 || t.Spec.DurationMS != 0) {
		return fault.New(fault.Invalid, "Bounded model usage required")
	}
	if t.Price.Unit == pricing.Hour && (t.Spec.InputTokenLimit != 0 || t.Spec.OutputTokenLimit != 0) || t.Spec.ModelID != "" && !ids.Valid(t.Spec.ModelID) || t.Spec.WorkflowID == "" && t.Spec.WorkflowRevision != 0 || t.Spec.WorkflowID != "" && (!ids.Valid(t.Spec.WorkflowID) || t.Spec.WorkflowRevision < 1) || t.Price.Unit == pricing.MillionTokens && t.Spec.WorkflowID != "" {
		return fault.New(fault.Invalid, "Unsupported public model or workflow limits")
	}
	seenDevices := map[string]bool{}
	seenCaps := map[string]bool{}
	for _, d := range t.Devices {
		if !ids.Valid(d.DeviceID) || !label(d.ModelName) || seenDevices[d.DeviceID] || d.ModelName == "Android设备" || d.ModelName == "Android 设备" {
			return fault.New(fault.Invalid, "Specific device models required")
		}
		seenDevices[d.DeviceID] = true
	}
	boundDevices := map[string]bool{}
	for _, b := range t.Bindings {
		if !seenDevices[b.DeviceID] || !ids.Valid(b.CapabilityID) || !ids.Valid(b.EnvironmentRef) || len(b.EnvironmentDigest) != 64 || seenCaps[b.CapabilityID] {
			return fault.New(fault.Invalid, "Invalid or duplicate kernel binding")
		}
		for _, c := range b.EnvironmentDigest {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return fault.New(fault.Invalid, "Invalid environment digest")
			}
		}
		seenCaps[b.CapabilityID] = true
		boundDevices[b.DeviceID] = true
	}
	if len(boundDevices) != len(seenDevices) {
		return fault.New(fault.Invalid, "Unbound public device")
	}
	return nil
}

func DecodeTerms(raw []byte) (Terms, error) {
	var t Terms
	if strictjson.Decode(raw, &t, 65536) != nil {
		return t, fault.New(fault.Invalid, "Unsupported public template")
	}
	return t, t.Validate()
}

type Evidence struct {
	ID         string    `json:"evidence_id"`
	Kind       string    `json:"kind"`
	Digest     string    `json:"digest"`
	ValidUntil time.Time `json:"valid_until"`
	ScopeRef   string    `json:"scope_ref"`
}

type Service struct {
	ListingID string `json:"listing_id"`
	Revision  int64  `json:"listing_revision"`
	SellerID  string `json:"seller_id"`
	Terms
	Availability string     `json:"availability"`
	Evidence     []Evidence `json:"evidence_refs"`
	ValidUntil   time.Time  `json:"-"`
}

type Catalog struct{ DB *persistence.DB }

// LoadTx is also used inside quote/order transactions. Shared row locks make
// revocations and future-version changes serialize with purchase admission.
func (c Catalog) LoadTx(ctx context.Context, tx pgx.Tx, id string, revision int64) (Service, error) {
	return c.loadVersionTx(ctx, tx, id, revision, false)
}

// LoadConfirmedTx reads the immutable version of an already confirmed order.
// A seller's newer price/model must not rewrite or silently cancel that order.
// Callers must first verify the original order and its buyer; this is not a
// purchase path. Current seller/device/qualification/availability still applies.
func (c Catalog) LoadConfirmedTx(ctx context.Context, tx pgx.Tx, id string, revision int64) (Service, error) {
	return c.loadVersionTx(ctx, tx, id, revision, true)
}

func (c Catalog) loadVersionTx(ctx context.Context, tx pgx.Tx, id string, revision int64, confirmed bool) (Service, error) {
	var s Service
	var raw []byte
	var region string
	if !ids.Valid(id) || revision < 1 {
		return s, fault.New(fault.Invalid, "Invalid listing reference")
	}
	err := tx.QueryRow(ctx, `SELECT l.id,l.seller_id,v.revision,v.contract_template,v.region,v.valid_until
FROM p11.listings l JOIN p11.listing_versions v ON v.listing_id=l.id
JOIN p11.seller_profiles sp ON sp.id=l.seller_id JOIN p11.users u ON u.id=sp.id
JOIN p11.role_grants sr ON sr.user_id=u.id AND sr.role_code='SELLER' AND sr.scope_ref='self'
WHERE l.id=$1 AND v.revision=$2 AND (v.revision=l.current_revision OR $3) AND l.state='PUBLISHED' AND sp.state='APPROVED' AND u.state='ACTIVE'
AND sr.revoked_at IS NULL AND (sr.expires_at IS NULL OR sr.expires_at>clock_timestamp()) AND v.price_version='NB_PRICE_1'
AND sp.payee_binding_ref IS NOT NULL AND sp.legal_scope_ref IS NOT NULL AND v.region=ANY(sp.approved_regions)
AND v.valid_from<=clock_timestamp() AND v.valid_until>clock_timestamp() FOR SHARE OF l,v,sp,u,sr`, id, revision, confirmed).Scan(&s.ListingID, &s.SellerID, &s.Revision, &raw, &region, &s.ValidUntil)
	if err == pgx.ErrNoRows {
		return s, fault.New(fault.NotFound, "Offer unavailable")
	}
	if err != nil {
		return s, err
	}
	s.Terms, err = DecodeTerms(raw)
	if err != nil {
		return s, fault.New(fault.Unavailable, "Offer contract requires review")
	}
	if region != s.Policy.Region {
		return s, fault.New(fault.Unavailable, "Offer region requires review")
	}
	var bindings int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM p11.listing_devices WHERE listing_id=$1 AND revision=$2`, id, revision).Scan(&bindings); err != nil {
		return s, err
	}
	if bindings != len(s.Bindings) {
		return s, fault.New(fault.Unavailable, "Offer environment requires review")
	}
	s.Availability = "AVAILABLE"
	s.Evidence = []Evidence{}
	seen := map[string]bool{}
	for _, b := range s.Bindings {
		state, proofs, err := c.qualify(ctx, tx, s, b)
		if err != nil {
			return s, err
		}
		if state == "UNAVAILABLE" || state == "BUSY" && s.Availability == "AVAILABLE" {
			s.Availability = state
		}
		for _, e := range proofs {
			if !seen[e.ID] {
				s.Evidence = append(s.Evidence, e)
				seen[e.ID] = true
			}
		}
	}
	// New authored definitions have exact availability windows. The broad
	// version validity range alone must never sell the gap between two windows.
	var windows []byte
	err = tx.QueryRow(ctx, "SELECT definition->'windows' FROM p11.listing_definitions WHERE listing_id=$1 AND revision=$2", id, revision).Scan(&windows)
	if err != nil && err != pgx.ErrNoRows {
		return s, err
	}
	if err == nil {
		var spans []struct {
			Start time.Time `json:"starts_at"`
			End   time.Time `json:"ends_at"`
		}
		if json.Unmarshal(windows, &spans) != nil {
			return s, fault.New(fault.Unavailable, "Offer windows require review")
		}
		var now time.Time
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return s, err
		}
		active := false
		for _, w := range spans {
			if !now.Before(w.Start) && now.Before(w.End) {
				seconds := int64(w.End.Sub(now) / time.Second)
				if seconds < 1 || s.Spec.DurationMS > seconds*1000 {
					continue
				}
				active = true
				if seconds < s.DeadlineSec {
					s.DeadlineSec = seconds
				}
				if w.End.Before(s.ValidUntil) {
					s.ValidUntil = w.End
				}
				break
			}
		}
		if !active {
			s.Availability = "UNAVAILABLE"
		}
	}
	return s, nil
}

// ValidateCandidateTx checks existing immutable bindings and current approvals.
// It does not change publication state or approve a device/evidence record.
func (c Catalog) ValidateCandidateTx(ctx context.Context, tx pgx.Tx, id string, revision int64, seller string, t Terms) error {
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT contract_template FROM p11.listing_versions WHERE listing_id=$1 AND revision=$2 AND seller_id=$3 FOR SHARE", id, revision, seller).Scan(&raw); err != nil {
		return err
	}
	old, err := DecodeTerms(raw)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(t)
	if string(a) != string(b) {
		return fault.New(fault.Conflict, "Immutable listing environment changed; create a new revision")
	}
	service := Service{ListingID: id, Revision: revision, SellerID: seller, Terms: t}
	for _, binding := range t.Bindings {
		if _, _, err := c.qualify(ctx, tx, service, binding); err != nil {
			return err
		}
	}
	return nil
}

func (c Catalog) qualify(ctx context.Context, tx pgx.Tx, s Service, b Binding) (string, []Evidence, error) {
	scope, _ := json.Marshal(map[string]string{"seller_id": s.SellerID, "device_id": b.DeviceID, "capability_id": b.CapabilityID, "kernel_environment_ref": b.EnvironmentRef, "kernel_environment_digest": b.EnvironmentDigest, "mode": s.Policy.Mode, "privacy": s.Policy.Privacy, "region": s.Policy.Region, "protocol_profile_id": s.Policy.Protocol, "price_contract_version": pricing.Version, "sku_id": s.SKU})
	var evidenceIDs, gateIDs, capIDs []string
	var online, accepting, busy bool
	err := tx.QueryRow(ctx, `SELECT r.evidence_ids,g.evidence_ids,c.evidence_ids,
COALESCE(d.state='ONLINE' AND d.last_seen_at>clock_timestamp()-interval '45 seconds',false),
COALESCE(d.resource_policy->>'accepting_new_jobs'='true' AND (d.reported_resource_state='IDLE' OR d.identity_profile='LEGACY_UNVERIFIED' AND d.reported_resource_state='UNKNOWN'),false),
EXISTS(SELECT 1 FROM p11.resource_locks lk WHERE lk.resource_id=c.resource_id AND lk.phase<>'FREE')
FROM p11.capabilities c JOIN p11.devices d ON d.id=c.device_id AND d.seller_id=c.seller_id
JOIN p11.listing_devices ld ON ld.capability_id=c.id AND ld.device_id=d.id AND ld.seller_id=c.seller_id
JOIN p11.releases r ON r.capability_id=c.id JOIN p11.feature_gates g ON g.id=r.gate_id
WHERE c.id=$1 AND d.id=$2 AND c.seller_id=$3 AND ld.listing_id=$4 AND ld.revision=$5
AND c.kernel_environment_ref=$6 AND c.kernel_environment_digest=$7 AND c.state='QUALIFIED'
AND c.mode=$8 AND c.privacy=$9 AND c.protocol_profile=$10
AND r.state='ACTIVE' AND r.expires_at>clock_timestamp() AND r.approved_by IS NOT NULL
AND r.scope_snapshot @> $11::jsonb AND g.enabled AND g.approved_by IS NOT NULL
ORDER BY r.id LIMIT 1 FOR SHARE OF c,d,r,g`, b.CapabilityID, b.DeviceID, s.SellerID, s.ListingID, s.Revision, b.EnvironmentRef, b.EnvironmentDigest, s.Policy.Mode, s.Policy.Privacy, s.Policy.Protocol, scope).Scan(&evidenceIDs, &gateIDs, &capIDs, &online, &accepting, &busy)
	if err == pgx.ErrNoRows {
		return "", nil, fault.New(fault.NotFound, "Offer qualification unavailable")
	}
	if err != nil {
		return "", nil, err
	}
	set := map[string]bool{}
	all := []string{}
	for _, id := range append(append(evidenceIDs, gateIDs...), capIDs...) {
		if !set[id] {
			all = append(all, id)
			set[id] = true
		}
	}
	if len(all) == 0 || len(all) > 150 {
		return "", nil, fault.New(fault.NotFound, "Offer evidence unavailable")
	}
	rows, err := tx.Query(ctx, `SELECT id,evidence_type,digest,valid_until FROM p11.evidence WHERE id=ANY($1::text[])
AND decision='APPROVED' AND seller_id=$2 AND (device_id IS NULL OR device_id=$3)
AND valid_from<=clock_timestamp() AND valid_until>clock_timestamp() AND scope_snapshot @> $4::jsonb FOR SHARE`, all, s.SellerID, b.DeviceID, scope)
	if err != nil {
		return "", nil, err
	}
	var proofs []Evidence
	kinds := map[string]bool{}
	for rows.Next() {
		var e Evidence
		if err = rows.Scan(&e.ID, &e.Kind, &e.Digest, &e.ValidUntil); err != nil {
			break
		}
		e.ScopeRef = b.CapabilityID
		proofs = append(proofs, e)
		kinds[e.Kind] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	if len(proofs) != len(all) {
		return "", nil, fault.New(fault.NotFound, "Offer evidence expired or revoked")
	}
	required := []string{"LICENSE", "PAYMENT", "SDK", "PHYSICAL_TEST"}
	if s.Price.Unit == pricing.MillionTokens {
		required = append(required, "USAGE")
	}
	if s.Policy.Mode == "E2E" {
		required = append(required, "CRYPTO")
	}
	for _, kind := range required {
		if !kinds[kind] {
			return "", nil, fault.New(fault.NotFound, "Offer evidence incomplete")
		}
	}
	if !online || !accepting {
		return "UNAVAILABLE", proofs, nil
	}
	if busy {
		return "BUSY", proofs, nil
	}
	return "AVAILABLE", proofs, nil
}

type Filter struct {
	Mode   string `json:"mode"`
	Region string `json:"region"`
}
type Page struct {
	Items      []Service `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}
type cursor struct {
	After  string `json:"after"`
	Filter string `json:"filter"`
}

func (c Catalog) List(ctx context.Context, f Filter, after string, limit int) (Page, error) {
	page := Page{Items: []Service{}}
	if c.DB == nil || limit < 1 || limit > 50 || (f.Mode != "" && f.Mode != "PLAINTEXT" && f.Mode != "E2E" && f.Mode != "GPU_RENTAL") || len(f.Region) > 128 {
		return page, fault.New(fault.Invalid, "Invalid market query")
	}
	encoded, _ := json.Marshal(f)
	selector := outbox.Digest(encoded)
	last := ""
	if after != "" {
		var cur cursor
		raw, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || len(after) > 2048 || json.Unmarshal(raw, &cur) != nil || cur.Filter != selector || !ids.Valid(cur.After) {
			return page, fault.New(fault.Invalid, "Invalid market cursor")
		}
		last = cur.After
	}
	err := c.DB.Transaction(ctx, pgx.RepeatableRead, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT l.id,l.current_revision FROM p11.listings l JOIN p11.listing_versions v ON v.listing_id=l.id AND v.revision=l.current_revision WHERE l.state='PUBLISHED' AND ($1='' OR l.id<$1) AND ($2='' OR v.contract_template#>>'{policy,service_mode}'=$2) AND ($3='' OR v.region=$3) ORDER BY l.id DESC LIMIT $4`, last, f.Mode, f.Region, limit*3+1)
		if err != nil {
			return err
		}
		type ref struct {
			id  string
			rev int64
		}
		var candidates []ref
		for rows.Next() {
			var x ref
			if err = rows.Scan(&x.id, &x.rev); err != nil {
				break
			}
			candidates = append(candidates, x)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		visited := ""
		i := 0
		for ; i < len(candidates) && len(page.Items) < limit; i++ {
			x := candidates[i]
			visited = x.id
			s, err := c.LoadTx(ctx, tx, x.id, x.rev)
			if err != nil {
				var safe *fault.Error
				if errors.As(err, &safe) && (safe.Code == fault.NotFound || safe.Code == fault.Unavailable) {
					continue
				}
				return err
			}
			page.Items = append(page.Items, s)
		}
		if visited != "" && (i < len(candidates) || len(candidates) == limit*3+1) {
			raw, _ := json.Marshal(cursor{After: visited, Filter: selector})
			value := base64.RawURLEncoding.EncodeToString(raw)
			page.NextCursor = &value
		}
		return nil
	})
	return page, err
}
