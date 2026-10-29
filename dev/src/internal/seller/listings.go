package seller

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"time"
)

type FeePolicy struct {
	BPS                       int
	Version, ChannelCostOwner string
}
type Window struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}
type Definition struct {
	Title    string        `json:"title"`
	SKU      string        `json:"sku_id"`
	Devices  []string      `json:"device_ids"`
	Policy   market.Policy `json:"policy"`
	Spec     market.Spec   `json:"public_spec"`
	Price    pricing.Price `json:"price"`
	Windows  []Window      `json:"windows"`
	Capacity int           `json:"capacity"`
	Evidence []string      `json:"evidence_refs"`
}
type Patch struct {
	Title    *string        `json:"title,omitempty"`
	SKU      *string        `json:"sku_id,omitempty"`
	Devices  *[]string      `json:"device_ids,omitempty"`
	Policy   *market.Policy `json:"policy,omitempty"`
	Spec     *market.Spec   `json:"public_spec,omitempty"`
	Price    *pricing.Price `json:"price,omitempty"`
	Windows  *[]Window      `json:"windows,omitempty"`
	Capacity *int           `json:"capacity,omitempty"`
	Evidence *[]string      `json:"evidence_refs,omitempty"`
}

func (p *Patch) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, v := range fields {
		if string(v) == "null" {
			return fault.New(fault.Invalid, "Null listing patch fields are not supported")
		}
	}
	type plain Patch
	return strictjson.Decode(raw, (*plain)(p), 65536)
}
func (p Patch) apply(d *Definition) bool {
	changed := false
	if p.Title != nil {
		d.Title = *p.Title
		changed = true
	}
	if p.SKU != nil {
		d.SKU = *p.SKU
		changed = true
	}
	if p.Devices != nil {
		d.Devices = *p.Devices
		changed = true
	}
	if p.Policy != nil {
		d.Policy = *p.Policy
		changed = true
	}
	if p.Spec != nil {
		d.Spec = *p.Spec
		changed = true
	}
	if p.Price != nil {
		d.Price = *p.Price
		changed = true
	}
	if p.Windows != nil {
		d.Windows = *p.Windows
		changed = true
	}
	if p.Capacity != nil {
		d.Capacity = *p.Capacity
		changed = true
	}
	if p.Evidence != nil {
		d.Evidence = *p.Evidence
		changed = true
	}
	return changed
}

type Listing struct {
	ID         string     `json:"listing_id"`
	SellerID   string     `json:"seller_id"`
	Revision   int64      `json:"revision"`
	State      string     `json:"state"`
	Definition Definition `json:"definition"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Version    int64      `json:"-"`
	Replayed   bool       `json:"-"`
}
type listingResult struct {
	Listing Listing `json:"listing"`
	Version int64   `json:"version"`
}
type ListingPage struct {
	Items      []Listing `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

func (s Service) sellerAuthority(ctx context.Context, p identity.Principal, scope string, approved bool) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if err := identity.RequireRoleTx(ctx, tx, p, scope, "SELLER", true); err != nil {
			return err
		}
		profile, err := profileTx(ctx, tx, p.UserID, false)
		if err != nil {
			return err
		}
		if approved && profile.State != "APPROVED" {
			return fault.New(fault.Forbidden, "Approved seller required for new supply")
		}
		return nil
	}
}
func loadListing(ctx context.Context, tx pgx.Tx, p identity.Principal, id string, update bool) (Listing, error) {
	var l Listing
	var raw []byte
	lock := "FOR SHARE"
	if update {
		lock = "FOR UPDATE"
	}
	// Lock the mutable head before selecting its immutable definition. A joined
	// FOR UPDATE can lose a row after waiting for a concurrent head revision.
	err := tx.QueryRow(ctx, `SELECT id,seller_id,current_revision,state,version,updated_at
 FROM p11.listings WHERE id=$1 AND seller_id=$2 `+lock, id, p.UserID).Scan(&l.ID, &l.SellerID, &l.Revision, &l.State, &l.Version, &l.UpdatedAt)
	if err == pgx.ErrNoRows {
		return l, fault.New(fault.NotFound, "Owned versioned listing not found")
	}
	if err != nil {
		return l, err
	}
	if err = tx.QueryRow(ctx, "SELECT definition FROM p11.listing_definitions WHERE listing_id=$1 AND revision=$2 AND seller_id=$3", id, l.Revision, p.UserID).Scan(&raw); err == pgx.ErrNoRows {
		return l, fault.New(fault.Unavailable, "Historical listing definition requires migration")
	} else if err != nil {
		return l, err
	}
	if err = strictjson.Decode(raw, &l.Definition, 65536); err != nil {
		return l, fault.New(fault.Unavailable, "Listing definition requires review")
	}
	return l, nil
}

// buildTerms derives device models and immutable capability bindings from
// authoritative owned rows; they cannot be supplied by a market-card label.
func (s Service) buildTerms(ctx context.Context, tx pgx.Tx, p identity.Principal, d Definition) (market.Terms, time.Time, time.Time, error) {
	var t market.Terms
	var from, until time.Time
	invalid := fault.New(fault.Invalid, "Invalid or unsupported future listing definition")
	if !text(d.Title, 128) || !ids.Valid(d.SKU) || !uniqueIDs(d.Devices, 1, 50) || !uniqueIDs(d.Evidence, 1, 50) || d.Capacity < 1 || d.Capacity > len(d.Devices) || len(d.Windows) < 1 || len(d.Windows) > 100 {
		return t, from, until, invalid
	}
	deadline := int64(86400)
	for i, w := range d.Windows {
		if w.StartsAt.IsZero() || !w.EndsAt.After(w.StartsAt) || !w.EndsAt.After(time.Now()) || w.EndsAt.Sub(w.StartsAt) < time.Second || w.EndsAt.Sub(w.StartsAt) > 30*24*time.Hour || i > 0 && w.StartsAt.Before(d.Windows[i-1].EndsAt) {
			return t, from, until, invalid
		}
		if i == 0 {
			from = w.StartsAt
		}
		until = w.EndsAt
		seconds := int64(w.EndsAt.Sub(w.StartsAt) / time.Second)
		if seconds < deadline {
			deadline = seconds
		}
	}
	if until.Sub(from) > 90*24*time.Hour {
		return t, from, until, invalid
	}
	if s.Fees == nil || s.Fees.BPS < 0 || s.Fees.BPS > 10000 || !ids.Valid(s.Fees.Version) || (s.Fees.ChannelCostOwner != "SELLER" && s.Fees.ChannelCostOwner != "PLATFORM") {
		return t, from, until, fault.New(fault.Unavailable, "An explicit platform fee policy is required")
	}
	if d.Price.CommissionBPS == nil || *d.Price.CommissionBPS != s.Fees.BPS || d.Price.FeePolicyVersion != s.Fees.Version || d.Price.ChannelCostOwner != s.Fees.ChannelCostOwner {
		return t, from, until, fault.New(fault.Conflict, "Platform fee policy changed")
	}
	profile, err := profileTx(ctx, tx, p.UserID, false)
	if err != nil {
		return t, from, until, err
	}
	allowed := false
	for _, region := range profile.Regions {
		if region == d.Policy.Region {
			allowed = true
		}
	}
	if !allowed {
		return t, from, until, fault.New(fault.Forbidden, "Seller region is not approved")
	}
	t = market.Terms{SKU: d.SKU, SellerDisplayName: profile.DisplayName, Policy: d.Policy, Spec: d.Spec, Price: d.Price, DeadlineSec: deadline, Bindings: []market.Binding{}, Devices: []market.DeviceDisplay{}}
	// A submitted evidence identifier must belong to this seller and remain
	// live; PENDING is allowed in a draft, but never grants sale qualification.
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM p11.evidence WHERE id=ANY($1::text[]) AND seller_id=$2 AND decision IN ('PENDING','APPROVED') AND valid_until>clock_timestamp()`, d.Evidence, p.UserID).Scan(&count); err != nil {
		return t, from, until, err
	}
	if count != len(d.Evidence) {
		return t, from, until, fault.New(fault.NotFound, "Owned evidence references unavailable")
	}
	resources := map[string]bool{}
	for _, device := range d.Devices {
		var model, state string
		err = tx.QueryRow(ctx, `SELECT COALESCE(model_name,''),state FROM p11.devices WHERE id=$1 AND seller_id=$2 FOR SHARE`, device, p.UserID).Scan(&model, &state)
		if err == pgx.ErrNoRows {
			return t, from, until, fault.New(fault.NotFound, "Owned device not found")
		}
		if err != nil {
			return t, from, until, err
		}
		if !text(model, 128) || state == "REVOKED" || state == "RETIRED" || state == "QUARANTINED" {
			return t, from, until, fault.New(fault.Conflict, "Concrete approved device model required")
		}
		rows, err := tx.Query(ctx, `SELECT id,kernel_environment_ref,kernel_environment_digest,resource_id FROM p11.capabilities
   WHERE device_id=$1 AND seller_id=$2 AND mode=$3 AND privacy=$4 AND protocol_profile=$5 AND state='QUALIFIED' ORDER BY id LIMIT 2 FOR SHARE`, device, p.UserID, d.Policy.Mode, d.Policy.Privacy, d.Policy.Protocol)
		if err != nil {
			return t, from, until, err
		}
		matches := 0
		var binding market.Binding
		var resource string
		for rows.Next() {
			matches++
			err = rows.Scan(&binding.CapabilityID, &binding.EnvironmentRef, &binding.EnvironmentDigest, &resource)
			if err != nil {
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return t, from, until, err
		}
		if matches != 1 || resources[resource] {
			return t, from, until, fault.New(fault.Conflict, "Device capability must resolve to one distinct physical resource")
		}
		resources[resource] = true
		binding.DeviceID = device
		t.Bindings = append(t.Bindings, binding)
		t.Devices = append(t.Devices, market.DeviceDisplay{DeviceID: device, ModelName: model})
	}
	return t, from, until, t.Validate()
}
func saveVersion(ctx context.Context, tx pgx.Tx, l Listing, t market.Terms, from, until time.Time) error {
	template, _ := json.Marshal(t)
	definition, _ := json.Marshal(l.Definition)
	if _, err := tx.Exec(ctx, `INSERT INTO p11.listing_versions(listing_id,revision,seller_id,region,contract_template,price_version,valid_from,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, l.ID, l.Revision, l.SellerID, t.Policy.Region, template, pricing.Version, from, until); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO p11.listing_definitions(listing_id,revision,seller_id,definition) VALUES($1,$2,$3,$4)`, l.ID, l.Revision, l.SellerID, definition); err != nil {
		return err
	}
	for _, b := range t.Bindings {
		if _, err := tx.Exec(ctx, `INSERT INTO p11.listing_devices(listing_id,revision,seller_id,device_id,capability_id) VALUES($1,$2,$3,$4,$5)`, l.ID, l.Revision, l.SellerID, b.DeviceID, b.CapabilityID); err != nil {
			return err
		}
	}
	return nil
}
func listingStored(l Listing, status int) (outbox.StoredResult, error) {
	return stored(l.ID, status, listingResult{Listing: l, Version: l.Version})
}
func decodeListing(r outbox.StoredResult, err error) (Listing, error) {
	var value listingResult
	if err == nil {
		err = json.Unmarshal(r.Body, &value)
	}
	value.Listing.Version = value.Version
	value.Listing.Replayed = r.Replayed
	return value.Listing, err
}
func (s Service) CreateListing(ctx context.Context, p identity.Principal, in Definition, key string) (Listing, error) {
	return decodeListing(s.command(ctx, p, "createListing", "self", key, in, s.sellerAuthority(ctx, p, "listing:write", true), func(tx pgx.Tx) (outbox.StoredResult, error) {
		terms, from, until, err := s.buildTerms(ctx, tx, p, in)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		l := Listing{ID: ids.New("lst"), SellerID: p.UserID, Revision: 1, Version: 1, State: "DRAFT", Definition: in}
		if err = tx.QueryRow(ctx, "INSERT INTO p11.listings(id,seller_id,state) VALUES($1,$2,'DRAFT') RETURNING updated_at", l.ID, p.UserID).Scan(&l.UpdatedAt); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = saveVersion(ctx, tx, l, terms, from, until); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, "LISTING", l.ID, l.Version, "ListingCreated"); err != nil {
			return outbox.StoredResult{}, err
		}
		return listingStored(l, 201)
	}))
}
func (s Service) UpdateListing(ctx context.Context, p identity.Principal, id string, version int64, in Patch, key string) (Listing, error) {
	if !ids.Valid(id) || version < 1 || version >= 2147483647 {
		return Listing{}, fault.New(fault.Invalid, "A current listing ETag is required")
	}
	request := struct {
		ExpectedVersion int64 `json:"expected_version"`
		Patch           Patch `json:"patch"`
	}{version, in}
	return decodeListing(s.command(ctx, p, "updateFutureListing", id, key, request, s.sellerAuthority(ctx, p, "listing:write", true), func(tx pgx.Tx) (outbox.StoredResult, error) {
		l, err := loadListing(ctx, tx, p, id, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if l.Version != version || l.Revision >= 2147483647 || l.State == "REVOKED" || l.State == "ARCHIVED" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Listing version or state changed")
		}
		if !in.apply(&l.Definition) {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "A non-null supported patch field is required")
		}
		terms, from, until, err := s.buildTerms(ctx, tx, p, l.Definition)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		l.Revision++
		l.Version++
		l.State = "DRAFT"
		// Every change, including a title edit, creates a future immutable version.
		// Old quotes/orders continue to reference their original snapshot.
		if err = saveVersion(ctx, tx, l, terms, from, until); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = tx.QueryRow(ctx, "UPDATE p11.listings SET current_revision=$2,state='DRAFT',version=$3,updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at", id, l.Revision, l.Version).Scan(&l.UpdatedAt); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, "LISTING", id, l.Version, "ListingFutureRevisionCreated"); err != nil {
			return outbox.StoredResult{}, err
		}
		return listingStored(l, 200)
	}))
}
func (s Service) ChangeListingState(ctx context.Context, p identity.Principal, id string, in Reason, key string, submit bool) (Listing, error) {
	if !ids.Valid(id) || !validReason(in) {
		return Listing{}, fault.New(fault.Invalid, "A versioned listing reason is required")
	}
	op, action, state := "pauseListing", "ListingPaused", "PAUSED"
	if submit {
		op, action, state = "submitListing", "ListingSubmitted", "SUBMITTED"
	}
	return decodeListing(s.command(ctx, p, op, id, key, in, s.sellerAuthority(ctx, p, "listing:write", submit), func(tx pgx.Tx) (outbox.StoredResult, error) {
		l, err := loadListing(ctx, tx, p, id, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if l.Version != in.ExpectedVersion || l.State == "REVOKED" || l.State == "ARCHIVED" || l.State == "EXPIRED" || l.State == state {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Listing version or state changed")
		}
		if submit {
			if l.State != "DRAFT" && l.State != "PAUSED" && l.State != "REJECTED" {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Listing cannot be submitted from this state")
			}
			terms, _, _, err := s.buildTerms(ctx, tx, p, l.Definition)
			if err != nil {
				return outbox.StoredResult{}, err
			}
			var approved int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM p11.evidence WHERE id=ANY($1::text[]) AND seller_id=$2 AND decision='APPROVED' AND reviewed_by IS NOT NULL AND valid_from<=clock_timestamp() AND valid_until>clock_timestamp()`, l.Definition.Evidence, p.UserID).Scan(&approved); err != nil {
				return outbox.StoredResult{}, err
			}
			if approved != len(l.Definition.Evidence) {
				return outbox.StoredResult{}, fault.New(fault.NotFound, "Listing evidence awaits approval or has expired")
			}
			if err = (market.Catalog{DB: s.DB}).ValidateCandidateTx(ctx, tx, l.ID, l.Revision, p.UserID, terms); err != nil {
				return outbox.StoredResult{}, err
			}
		}
		l.Version++
		l.State = state
		if err = tx.QueryRow(ctx, "UPDATE p11.listings SET state=$2,version=$3,updated_at=clock_timestamp() WHERE id=$1 RETURNING updated_at", id, state, l.Version).Scan(&l.UpdatedAt); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = reasonEvent(ctx, tx, p, "LISTING", id, l.Version, action, in); err != nil {
			return outbox.StoredResult{}, err
		}
		return listingStored(l, 200)
	}))
}
func (s Service) GetListing(ctx context.Context, p identity.Principal, id string) (Listing, error) {
	var l Listing
	if s.DB == nil {
		return l, fault.New(fault.Unavailable, "Seller service unavailable")
	}
	if !ids.Valid(id) {
		return l, fault.New(fault.Invalid, "Invalid listing reference")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := s.sellerAuthority(ctx, p, "seller:read", false)(tx); err != nil {
			return err
		}
		var err error
		l, err = loadListing(ctx, tx, p, id, false)
		return err
	})
	return l, err
}
func (s Service) ListListings(ctx context.Context, p identity.Principal, after string, limit int) (ListingPage, error) {
	page := ListingPage{Items: []Listing{}}
	if s.DB == nil {
		return page, fault.New(fault.Unavailable, "Seller service unavailable")
	}
	if limit < 1 || limit > 50 || after != "" && !ids.Valid(after) {
		return page, fault.New(fault.Invalid, "Invalid listing page")
	}
	err := s.DB.Transaction(ctx, pgx.RepeatableRead, func(tx pgx.Tx) error {
		if err := s.sellerAuthority(ctx, p, "seller:read", false)(tx); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT l.id FROM p11.listings l JOIN p11.listing_definitions d ON d.listing_id=l.id AND d.revision=l.current_revision WHERE l.seller_id=$1 AND ($2='' OR l.id<$2) ORDER BY l.id DESC LIMIT $3`, p.UserID, after, limit+1)
		if err != nil {
			return err
		}
		refs := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			refs = append(refs, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		for i, id := range refs {
			if i == limit {
				cursor := refs[i-1]
				page.NextCursor = &cursor
				break
			}
			l, err := loadListing(ctx, tx, p, id, false)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, l)
		}
		return nil
	})
	return page, err
}
