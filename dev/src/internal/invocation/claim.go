package invocation

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const ClaimProfile = "P11_NATIVE_LEASE_CLAIM_1"

// Claims is a verifier-only delivery service. No signer, content/fingerprint
// keys, decryption, engine or automatic issue/renewal path is available here.
type Claims struct {
	db        *persistence.DB
	routes    routecontract.Service
	mu        sync.RWMutex
	verifiers map[string]ed25519.PublicKey
}

func NewClaims(db *persistence.DB, environment string, keys map[string]ed25519.PublicKey) (*Claims, error) {
	if db == nil || (environment != "SANDBOX" && environment != "PRODUCTION") || len(keys) < 1 || len(keys) > 16 {
		return nil, fault.New(fault.Unavailable, "Explicit claim database, payment environment and verifiers required")
	}
	s := &Claims{db: db, routes: routecontract.Service{DB: db, PaymentEnvironment: environment}, verifiers: map[string]ed25519.PublicKey{}}
	for id, key := range keys {
		if !ids.Valid(id) || len(key) != ed25519.PublicKeySize {
			s.Close()
			return nil, fault.New(fault.Invalid, "Invalid claim verifier configuration")
		}
		s.verifiers[id] = append(ed25519.PublicKey(nil), key...)
	}
	return s, nil
}

func (s *Claims) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, k := range s.verifiers {
		clear(k)
		delete(s.verifiers, id)
	}
}

type ClaimResult struct {
	Lease    executionlease.Lease
	Replayed bool
}

// Claim commits an immutable original-command binding and safe event. A lost
// response MUST be recovered with the same key/body and a fresh device proof.
// No lease/attempt ACK, execution, usage, task completion or release is implied.
func (s *Claims) Claim(ctx context.Context, p identity.Principal, in devicecontrol.LeaseClaim, key string) (ClaimResult, error) {
	if s == nil || s.db == nil {
		return ClaimResult{}, fault.New(fault.Unavailable, "Native lease claims unavailable")
	}
	if p.Kind != identity.Device || p.DeviceID != in.Device {
		return ClaimResult{}, fault.New(fault.Forbidden, "This device's signed identity required")
	}
	if err := p.Require("device:execute", ""); err != nil {
		return ClaimResult{}, err
	}
	if in.Validate(key) != nil {
		return ClaimResult{}, fault.New(fault.Invalid, "Original claim key and bounded device context required")
	}
	var out ClaimResult
	err := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		out, err = s.claimTx(ctx, tx, p, in)
		return err
	})
	if err != nil {
		return ClaimResult{}, err
	}
	expires, _ := time.Parse(time.RFC3339Nano, out.Lease.Expires)
	if !time.Now().Before(expires) {
		return ClaimResult{}, fault.New(fault.Conflict, "Original claim expired; no replacement or renewal")
	}
	return out, nil
}

func (s *Claims) claimTx(ctx context.Context, tx pgx.Tx, p identity.Principal, in devicecontrol.LeaseClaim) (ClaimResult, error) {
	var out ClaimResult
	// Resolve immutable IDs without device/seller row locks. Current device
	// authentication occurs after the original order/catalog locks below.
	body, _ := json.Marshal(in)
	digest, command := outbox.Digest(body), outbox.Digest([]byte(in.ClaimKey))
	// Serialize the original device/command before looking up its durable
	// binding, including concurrent bodies targeting different capabilities.
	// The SQL unique index remains authoritative; a hash collision only queues.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "P11_CLAIM_COMMAND_1:"+p.DeviceID+":"+command); err != nil {
		return out, err
	}
	var leaseID string
	err := tx.QueryRow(ctx, `SELECT id FROM p11.execution_leases WHERE claim_device_id=$1 AND claim_key_digest=$2 AND claim_profile=$3`, p.DeviceID, command, ClaimProfile).Scan(&leaseID)
	if err == pgx.ErrNoRows {
		rows, e := tx.Query(ctx, `SELECT l.id FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id
WHERE a.device_id=$1 AND a.seller_id=$2 AND a.capability_id=$3 AND j.current_attempt_id=a.id
AND l.admission_profile=$4 AND l.state='ISSUED' AND a.state='ISSUED' AND l.expires_at>clock_timestamp()
ORDER BY l.id LIMIT 2`, p.DeviceID, p.UserID, in.Capability, AdmissionProfile)
		if e != nil {
			return out, e
		}
		var candidates []string
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, e
			}
			candidates = append(candidates, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if len(candidates) == 0 {
			return out, fault.New(fault.NotFound, "No current original lease for this device and capability")
		}
		if len(candidates) != 1 {
			return out, fault.New(fault.Conflict, "Ambiguous device capability; recover the original claim")
		}
		leaseID = candidates[0]
	} else if err != nil {
		return out, err
	}

	v, err := s.authorizeTx(ctx, tx, p, leaseID, in.Capability, in.Version)
	if err != nil {
		return out, err
	}
	if v.state != "UNBOUND_CLAIM" && (v.command != command || v.digest != digest) {
		return out, fault.New(fault.Conflict, "Original lease is bound to another claim or body")
	}
	out = v.ClaimResult
	if v.state == ClaimProfile {
		out.Replayed = true
		return out, nil
	}
	_, err = tx.Exec(ctx, `UPDATE p11.execution_leases SET claim_profile=$2,claim_device_id=$3,claim_key_digest=$4,claim_request_digest=$5,claim_device_version=$6,claimed_at=clock_timestamp() WHERE id=$1`, leaseID, ClaimProfile, p.DeviceID, command, digest, in.Version)
	if err != nil {
		return ClaimResult{}, err
	}
	safe, _ := json.Marshal(map[string]string{"lease_id": leaseID, "attempt_id": v.manifest.Attempt, "job_id": v.manifest.Job, "device_id": p.DeviceID, "resource_id": v.manifest.Resource, "resource_lock_generation": v.manifest.Generation})
	_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'EXECUTION_LEASE',$2,1,'ExecutionLeaseClaimed','3.0',$3,'PENDING')`, ids.New("evt"), leaseID, safe)
	if err != nil {
		return ClaimResult{}, err
	}
	return out, nil
}

// claimAuthority is internal, transaction-scoped current authority. It carries
// no content or keys and is shared by claim and both authenticated reads.
type claimAuthority struct {
	ClaimResult
	manifest               Manifest
	buyer                  identity.Principal
	state, command, digest string
}

func (s *Claims) authorizeTx(ctx context.Context, tx pgx.Tx, p identity.Principal, leaseID, capID string, version int64) (claimAuthority, error) {
	var out claimAuthority
	var buyer, originalKey, routeID, orderID, jobID string
	err := tx.QueryRow(ctx, `SELECT o.buyer_id,j.invoker_key_id,j.route_contract_id,j.order_id,j.id
FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id JOIN p11.orders o ON o.id=j.order_id
WHERE l.id=$1 AND a.device_id=$2 AND a.seller_id=$3`, leaseID, p.DeviceID, p.UserID).Scan(&buyer, &originalKey, &routeID, &orderID, &jobID)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.NotFound, "Original claim unavailable")
	}
	if err != nil {
		return out, err
	}
	// This is the original immutable job's key/route, not caller-supplied buyer
	// authority. Revoke, scope removal and original-route removal all close it.
	bp := identity.Principal{UserID: buyer, Kind: identity.APIKey, KeyID: originalKey, RouteIDs: []string{routeID}, Scopes: []string{"route:invoke"}}
	if err = tx.QueryRow(ctx, `SELECT version FROM p11.users WHERE id=$1`, buyer).Scan(&bp.Version); err != nil {
		return out, err
	}
	if err = identity.RequireRoleTx(ctx, tx, bp, "route:invoke", "BUYER", false); err != nil {
		return out, err
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM p11.orders WHERE id=$1 AND buyer_id=$2 AND seller_id=$3 FOR UPDATE`, orderID, buyer, p.UserID).Scan(&locked); err != nil {
		return out, err
	}
	c, err := s.routes.ReadAdmittedTx(ctx, tx, bp, routeID)
	if err != nil {
		return out, err
	}
	if !c.ExistingAdmission || c.Definition.OrderID != orderID || c.Definition.SellerID != p.UserID {
		return out, fault.New(fault.Conflict, "Original route, funding or qualification unavailable")
	}

	// Match issuer order -> catalog -> device SHARE, including queued writers.
	// Taking even SHARE before order can deadlock behind a waiting heartbeat
	// WRITE. This delivery never writes the device; heartbeat keeps WRITE auth.
	if err := identity.RequireDeviceReadTx(ctx, tx, p, "device:execute"); err != nil {
		return out, err
	}

	var payload, manifest, spec, ciphertext []byte
	var signer, resource, generation, fence, capability, environment, manifestID, manifestHash, contractHash, asset, assetDigest, inputProfile, assetState, state, originalCommand, originalDigest string
	var hard, expires, jobDeadline, assetExpiry, now time.Time
	var deviceVersion, inputBytes int64
	var current bool
	err = tx.QueryRow(ctx, `SELECT l.signed_payload,l.execution_manifest,l.issuer_key_id,a.resource_id,a.resource_lock_generation::text,a.fencing_token::text,a.capability_id,
c.kernel_environment_digest,l.execution_manifest_ref,l.execution_manifest_hash,o.contract_hash,l.hard_deadline,l.expires_at,j.deadline,j.invocation_spec,d.version,
ast.id,ast.storage_digest,ast.bytes,ast.input_profile,ast.state,ast.expires_at,ast.input_ciphertext,l.claim_profile,COALESCE(l.claim_key_digest::text,''),COALESCE(l.claim_request_digest::text,''),clock_timestamp(),
(l.admission_profile=$4 AND l.state='ISSUED' AND a.state='ISSUED' AND j.invocation_profile=$5 AND j.state='RUNNING'
 AND j.current_attempt_id=a.id AND j.fencing_token=a.fencing_token AND j.route_contract_id=$6 AND j.invoker_key_id=$7
 AND a.device_id=$2 AND a.seller_id=$3 AND a.capability_id=$8 AND d.state='ONLINE'
 AND d.last_seen_at>clock_timestamp()-interval '45 seconds' AND d.resource_policy->'accepting_new_jobs'='true'::jsonb
 AND rl.phase='ACTIVE' AND rl.owner_attempt_id=a.id AND rl.owner_session_id IS NULL
 AND rl.lock_generation=a.resource_lock_generation AND rl.expires_at=l.expires_at
 AND ast.owner_id=o.buyer_id AND ast.job_id=j.id AND ast.order_id=o.id AND ast.visibility='PLAINTEXT' AND ast.media_type='application/json')
FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id JOIN p11.orders o ON o.id=j.order_id
JOIN p11.devices d ON d.id=a.device_id JOIN p11.capabilities c ON c.id=a.capability_id
JOIN p11.resource_locks rl ON rl.resource_id=a.resource_id JOIN p11.assets ast ON ast.id=j.input_asset_id
WHERE l.id=$1 FOR UPDATE OF l FOR SHARE OF j,a,rl,ast`, leaseID, p.DeviceID, p.UserID, AdmissionProfile, Profile, routeID, originalKey, capID).
		Scan(&payload, &manifest, &signer, &resource, &generation, &fence, &capability, &environment, &manifestID, &manifestHash, &contractHash, &hard, &expires, &jobDeadline, &spec, &deviceVersion,
			&asset, &assetDigest, &inputBytes, &inputProfile, &assetState, &assetExpiry, &ciphertext, &state, &originalCommand, &originalDigest, &now, &current)
	defer clear(ciphertext)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.Conflict, "Original execution owner unavailable")
	}
	if err != nil {
		return out, err
	}
	if !current || deviceVersion != version || !now.Before(expires) || !now.Before(hard) || !now.Before(jobDeadline) || !now.Before(assetExpiry) || !now.Before(c.CurrentDeadline) || expires.After(c.CurrentDeadline) {
		return out, fault.New(fault.Conflict, "Original claim authority, owner, version or deadline changed")
	}
	if inputProfile != InputProfile || assetState != "READY" || inputBytes < 1 || inputBytes > 256<<10 || len(ciphertext) != int(inputBytes)+28 || outbox.Digest(ciphertext) != assetDigest {
		return out, fault.New(fault.Conflict, "Original encrypted input metadata unavailable")
	}
	b, err := budget.LookupTx(ctx, tx, scope(c), jobID)
	if err != nil {
		return out, err
	}
	if b.Reservation.State != "RESERVED" || b.Reservation.MaximumFen != c.Definition.PerCallFen {
		return out, fault.New(fault.Conflict, "Original full call budget unavailable")
	}
	var m Manifest
	var frozenSpec Spec
	if strictjson.Decode(manifest, &m, 16384) != nil || strictjson.Decode(spec, &frozenSpec, 16384) != nil || m.Digest() != manifestHash || m.Profile != ManifestProfile ||
		m.ID != manifestID || m.Job != jobID || m.Order != orderID || m.Route != routeID || m.Seller != p.UserID || m.Device != p.DeviceID || m.Capability != capability || m.Resource != resource || m.Generation != generation || m.Fence != fence ||
		m.ContractHash != contractHash || m.EnvironmentDigest != environment || m.Spec != frozenSpec || m.InputAsset != asset || m.InputDigest != assetDigest || m.InputBytes != inputBytes || m.Mode != "PLAINTEXT" || m.Privacy != "STANDARD" || m.HardDeadline != hard.UTC().Format(time.RFC3339Nano) {
		return out, fault.New(fault.Conflict, "Original manifest requires recovery")
	}
	plan := executionlease.Plan{Job: jobID, Attempt: m.Attempt, Order: orderID, Seller: p.UserID, Device: p.DeviceID, Resource: resource, Generation: generation, Capability: capability,
		Manifest: manifestID, ManifestHash: manifestHash, Environment: environment, Fence: fence, ContractHash: contractHash, HardDeadline: hard}
	// Attempt is checked against authoritative SQL, not accepted from manifest.
	if err = tx.QueryRow(ctx, `SELECT attempt_id FROM p11.execution_leases WHERE id=$1`, leaseID).Scan(&plan.Attempt); err != nil {
		return out, err
	}
	if m.Attempt != plan.Attempt {
		return out, fault.New(fault.Conflict, "Original attempt mismatch")
	}
	s.mu.RLock()
	verified, verifyErr := executionlease.Verify(payload, s.verifiers[signer], plan, now)
	s.mu.RUnlock()
	if verifyErr != nil {
		return out, fault.New(fault.Unavailable, "Original lease verifier or signature unavailable")
	}
	out.Lease = verified.Details()
	if out.Lease.ID != leaseID {
		return claimAuthority{}, fault.New(fault.Conflict, "Original signed lease ID mismatch")
	}
	out.manifest = m
	out.buyer = bp
	out.state, out.command, out.digest = state, originalCommand, originalDigest
	return out, nil
}
