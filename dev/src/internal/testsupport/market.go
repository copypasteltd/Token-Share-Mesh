// Package testsupport contains synthetic disposable-database fixtures only.
// It is never imported by runtime commands and grants no real sellability.
package testsupport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/ids"
	"strings"
	"testing"
)

type Offer struct {
	DeviceKey                                       ed25519.PrivateKey `json:"-"` // Synthetic test-only; never a runtime identity store.
	Seller                                          identity.Principal
	ID, Device, Resource, Capability, Gate, Release string
	Evidence                                        []string
	Terms                                           market.Terms
}

func execDB(t *testing.T, ctx context.Context, db *persistence.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
func newBuyer(t *testing.T, ctx context.Context, db *persistence.DB) identity.Principal {
	t.Helper()
	id := ids.New("usr")
	execDB(t, ctx, db, "INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')", id, outbox.Digest([]byte(id)))
	execDB(t, ctx, db, "INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)", ids.New("grant"), id)
	return identity.Principal{UserID: id, Kind: identity.UserSession, Scopes: []string{"identity:read", "key:manage", "quote:create", "quote:read", "order:create", "order:read"}, Roles: []string{"BUYER"}, Version: 1}
}

func SeedOffer(t *testing.T, ctx context.Context, db *persistence.DB, mode, region string) Offer {
	return SeedOfferForSeller(t, ctx, db, mode, region, newBuyer(t, ctx, db))
}

// SeedOfferForSeller attaches synthetic approvals to an actual test login.
// It is fixture-only and is never imported by a runtime command.
func SeedOfferForSeller(t *testing.T, ctx context.Context, db *persistence.DB, mode, region string, p identity.Principal) Offer {
	return seedOfferProfile(t, ctx, db, mode, region, p, "openai_nb1", false)
}

// SeedOfferForProtocol binds one explicit synthetic protocol across the listing,
// capability, evidence and release. It never upgrades an existing qualification.
func SeedOfferForProtocol(t *testing.T, ctx context.Context, db *persistence.DB, mode, region, protocol string) Offer {
	return seedOfferProfile(t, ctx, db, mode, region, newBuyer(t, ctx, db), protocol, false)
}

// SeedNativeOfferForProtocol inserts synthetic ED25519-shaped identity and an
// explicitly FREE resource. It proves no physical phone/kernel qualification.
// Immutable legacy identities are never upgraded by fixture updates.
func SeedNativeOfferForProtocol(t *testing.T, ctx context.Context, db *persistence.DB, region, protocol string) Offer {
	return seedOfferProfile(t, ctx, db, "PLAINTEXT", region, newBuyer(t, ctx, db), protocol, true)
}

func seedOfferProfile(t *testing.T, ctx context.Context, db *persistence.DB, mode, region string, p identity.Principal, protocol string, native bool) Offer {
	t.Helper()
	f := Offer{Seller: p, ID: ids.New("lst"), Device: ids.New("dev"), Resource: ids.New("res"), Capability: ids.New("cap"), Gate: ids.New("gate"), Release: ids.New("rel")}
	execDB(t, ctx, db, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'SELLER','self',$2)`, ids.New("grant"), f.Seller.UserID)
	execDB(t, ctx, db, `INSERT INTO p11.seller_profiles(id,state,legal_scope_ref,payee_binding_ref,approved_regions,display_name) VALUES($1,'APPROVED',$2,$3,ARRAY[$4],'个人卖家')`, f.Seller.UserID, ids.New("legal"), ids.New("payee"), region)
	f.Seller.Roles = append(f.Seller.Roles, "SELLER")
	bps := 500
	f.Terms = market.Terms{SKU: ids.New("sku"), SellerDisplayName: "星河同学", Policy: market.Policy{Mode: mode, Privacy: "STANDARD", Region: region, Protocol: protocol, Acceptance: "accept_nb1", Disclosure: "disclosure_nb1"}, DeadlineSec: 3600,
		Price:    pricing.Price{Version: pricing.Version, Currency: "CNY", Rounding: pricing.Rounding, CommissionBPS: &bps, ChannelCostOwner: "SELLER", FeePolicyVersion: "fee_nb1"},
		Bindings: []market.Binding{{DeviceID: f.Device, CapabilityID: f.Capability, EnvironmentRef: ids.New("env"), EnvironmentDigest: strings.Repeat("b", 64)}},
		Devices:  []market.DeviceDisplay{{DeviceID: f.Device, ModelName: "RTX 4090 24GB"}}}
	platform, kind, engine := "WINDOWS", "GPU_DEVICE", "COMFYUI_NATIVE"
	kinds := []string{"LICENSE", "PAYMENT", "SDK", "PHYSICAL_TEST"}
	if mode == "GPU_RENTAL" {
		f.Terms.Price.Unit = pricing.Hour
		f.Terms.Price.HourRate = &pricing.Rate{NumeratorFen: 360, DenominatorUnits: 1}
		f.Terms.Spec.DurationMS = 1800000
	} else {
		platform, kind, engine = "ANDROID", "ANDROID_DEVICE", "LMA_NATIVE"
		f.Terms.SellerDisplayName = "阿澄"
		f.Terms.Devices[0].ModelName = "摩托罗拉 X30"
		f.Terms.Price.Unit = pricing.MillionTokens
		f.Terms.Price.InputRate = &pricing.Rate{NumeratorFen: 80, DenominatorUnits: 1}
		f.Terms.Price.OutputRate = &pricing.Rate{NumeratorFen: 160, DenominatorUnits: 1}
		f.Terms.Spec = market.Spec{ModelID: "model_nb1", InputTokenLimit: 1000000, OutputTokenLimit: 250000}
		kinds = append(kinds, "USAGE")
		if mode == "E2E" {
			f.Terms.Policy.Privacy = "PLATFORM_BLIND"
			kinds = append(kinds, "CRYPTO")
		}
	}
	if native {
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		f.DeviceKey = key
		t.Cleanup(func() { clear(key) })
		execDB(t, ctx, db, `INSERT INTO p11.devices(id,seller_id,platform,resource_kind,state,auth_key_digest,client_release,resource_policy,last_seen_at,model_name,
identity_profile,device_public_key,installation_fingerprint,os_build,heartbeat_sequence,reported_resource_state)
VALUES($1,$2,$3,$4,'ONLINE',$5,'SYNTHETIC_TEST','{"accepting_new_jobs":true}',clock_timestamp(),$6,'ED25519_DEVICE_1',$7,$8,'SYNTHETIC_NOT_HARDWARE',1,'IDLE')`, f.Device, f.Seller.UserID, platform, kind, outbox.Digest(pub), f.Terms.Devices[0].ModelName, base64.RawURLEncoding.EncodeToString(pub), outbox.Digest([]byte(f.Device)))
	} else {
		execDB(t, ctx, db, `INSERT INTO p11.devices(id,seller_id,platform,resource_kind,state,auth_key_digest,client_release,resource_policy,last_seen_at,model_name) VALUES($1,$2,$3,$4,'ONLINE',$5,'SYNTHETIC_TEST','{"accepting_new_jobs":true}',clock_timestamp(),$6)`, f.Device, f.Seller.UserID, platform, kind, outbox.Digest([]byte(f.Device)), f.Terms.Devices[0].ModelName)
	}
	execDB(t, ctx, db, `INSERT INTO p11.resources(id,device_id,seller_id,local_resource_key,kind) VALUES($1,$2,$3,'resource0',$4)`, f.Resource, f.Device, f.Seller.UserID, kind)
	if native {
		execDB(t, ctx, db, `INSERT INTO p11.resource_locks(resource_id,phase) VALUES($1,'FREE')`, f.Resource)
	}
	b := f.Terms.Bindings[0]
	scope, err := json.Marshal(map[string]string{"seller_id": f.Seller.UserID, "device_id": f.Device, "capability_id": f.Capability, "kernel_environment_ref": b.EnvironmentRef, "kernel_environment_digest": b.EnvironmentDigest, "mode": mode, "privacy": f.Terms.Policy.Privacy, "region": region, "protocol_profile_id": f.Terms.Policy.Protocol, "price_contract_version": pricing.Version, "sku_id": f.Terms.SKU})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range kinds {
		id := ids.New("evd")
		f.Evidence = append(f.Evidence, id)
		execDB(t, ctx, db, `INSERT INTO p11.evidence(id,evidence_type,seller_id,device_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,$2,$3,$4,'SYNTHETIC_NOT_PUBLIC',repeat('c',64),$5,'APPROVED',$3,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours')`, id, kind, f.Seller.UserID, f.Device, scope)
	}
	execDB(t, ctx, db, `INSERT INTO p11.capabilities(id,device_id,seller_id,resource_id,engine_kind,engine_release,kernel_environment_ref,kernel_environment_digest,model_digest,recipe_digest,protocol_profile,mode,privacy,evidence_ids,state) VALUES($1,$2,$3,$4,$5,'SYNTHETIC_TEST',$6,$7,repeat('d',64),repeat('e',64),$8,$9,$10,$11,'QUALIFIED')`, f.Capability, f.Device, f.Seller.UserID, f.Resource, engine, b.EnvironmentRef, b.EnvironmentDigest, f.Terms.Policy.Protocol, mode, f.Terms.Policy.Privacy, f.Evidence)
	execDB(t, ctx, db, `INSERT INTO p11.feature_gates(id,scope_key,enabled,evidence_ids,approved_by) VALUES($1,$2,true,$3,$4)`, f.Gate, f.Gate, f.Evidence, f.Seller.UserID)
	execDB(t, ctx, db, `INSERT INTO p11.releases(id,capability_id,gate_id,artifact_digest,scope_snapshot,evidence_ids,state,approved_by,expires_at) VALUES($1,$2,$3,repeat('f',64),$4,$5,'ACTIVE',$6,clock_timestamp()+interval '2 hours')`, f.Release, f.Capability, f.Gate, scope, f.Evidence, f.Seller.UserID)
	execDB(t, ctx, db, `INSERT INTO p11.listings(id,seller_id,state) VALUES($1,$2,'PUBLISHED')`, f.ID, f.Seller.UserID)
	f.Version(t, ctx, db, 1)
	return f
}

func (f Offer) Version(t *testing.T, ctx context.Context, db *persistence.DB, revision int64) {
	t.Helper()
	raw, err := json.Marshal(f.Terms)
	if err != nil {
		t.Fatal(err)
	}
	execDB(t, ctx, db, `INSERT INTO p11.listing_versions(listing_id,revision,seller_id,region,contract_template,price_version,valid_from,valid_until) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '2 hours')`, f.ID, revision, f.Seller.UserID, f.Terms.Policy.Region, raw, pricing.Version)
	execDB(t, ctx, db, `INSERT INTO p11.listing_devices(listing_id,revision,seller_id,device_id,capability_id) VALUES($1,$2,$3,$4,$5)`, f.ID, revision, f.Seller.UserID, f.Device, f.Capability)
}

func (f Offer) Input() trade.QuoteInput {
	return trade.QuoteInput{ListingID: f.ID, ListingRevision: 1, SKU: f.Terms.SKU, Policy: f.Terms.Policy, Spec: f.Terms.Spec, BudgetFen: 500, Region: f.Terms.Policy.Region, Quantity: 1}
}
