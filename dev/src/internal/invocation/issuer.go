package invocation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const ManifestProfile = executionlease.ManifestProfile

type Manifest = executionlease.Manifest

type Issuance struct {
	Preparation                Preparation
	AttemptID, LeaseID, Status string
	Lease                      *executionlease.Lease
	Manifest                   *Manifest
	Replayed                   bool
}

type candidate struct{ device, resource, capability, generation, env, environment, engine, release, model, recipe string }

func (e *Execution) candidate(ctx context.Context, tx pgx.Tx, c routecontract.Contract) (candidate, error) {
	var v candidate
	var original trade.Contract
	if strictjson.Decode(c.OrderContract, &original, 65536) != nil {
		return v, fault.New(fault.Unavailable, "Original kernel bindings unavailable")
	}
	caps := make([]string, 0, len(original.Bindings))
	for _, b := range original.Bindings {
		caps = append(caps, b.CapabilityID)
	}
	err := tx.QueryRow(ctx, `SELECT d.id,r.id,c.id,rl.lock_generation::text,c.kernel_environment_ref,c.kernel_environment_digest,c.engine_kind,c.engine_release,c.model_digest,c.recipe_digest
FROM p11.capabilities c JOIN p11.resources r ON r.id=c.resource_id AND r.device_id=c.device_id AND r.seller_id=c.seller_id
JOIN p11.devices d ON d.id=c.device_id AND d.seller_id=c.seller_id
JOIN p11.resource_locks rl ON rl.resource_id=r.id
WHERE c.id=ANY($1::text[]) AND c.seller_id=$2 AND c.state='QUALIFIED'
AND d.identity_profile='ED25519_DEVICE_1' AND d.state='ONLINE' AND d.reported_resource_state='IDLE'
AND d.last_seen_at>clock_timestamp()-interval '45 seconds'
AND d.resource_policy->'accepting_new_jobs'='true'::jsonb
AND rl.phase='FREE' AND rl.owner_attempt_id IS NULL AND rl.owner_session_id IS NULL
ORDER BY r.id FOR UPDATE OF rl SKIP LOCKED LIMIT 1`, caps, c.Definition.SellerID).
		Scan(&v.device, &v.resource, &v.capability, &v.generation, &v.env, &v.environment, &v.engine, &v.release, &v.model, &v.recipe)
	if err == pgx.ErrNoRows {
		return v, fault.New(fault.Conflict, "No confirmed native device with explicit free resource")
	}
	return v, err
}

func (e *Execution) Issue(ctx context.Context, p identity.Principal, in Request) (Issuance, error) {
	var result Issuance
	if e == nil || e.invocations == nil {
		return result, fault.New(fault.Unavailable, "Execution issuer unavailable")
	}
	err := e.invocations.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error { var err error; result, err = e.IssueTx(ctx, tx, p, in); return err })
	if err != nil {
		return Issuance{}, err
	}
	if result.Lease != nil {
		end, _ := time.Parse(time.RFC3339Nano, result.Lease.Expires)
		if !time.Now().Before(end) {
			result.Lease = nil
			result.Manifest = nil
			result.Status = "RECOVERED_UNAVAILABLE"
		}
	}
	return result, nil
}

func (e *Execution) recoverIssued(ctx context.Context, tx pgx.Tx, c routecontract.Contract, p Preparation, attempt string, in Request, now time.Time) (Issuance, error) {
	out := Issuance{Preparation: p, AttemptID: attempt, Status: "RECOVERED_UNAVAILABLE", Replayed: true}
	var payload, manifest []byte
	var signerID, resource, generation, fence, capability, device, environment, manifestID, manifestHash, contractHash string
	var current bool
	var hard time.Time
	err := tx.QueryRow(ctx, `SELECT l.id,l.signed_payload,l.execution_manifest,l.issuer_key_id,a.resource_id,a.resource_lock_generation::text,a.fencing_token::text,
a.capability_id,a.device_id,c.kernel_environment_digest,l.execution_manifest_ref,l.execution_manifest_hash,o.contract_hash,l.hard_deadline,
(l.state IN ('ISSUED','ACKED') AND a.state IN ('ISSUED','ACKED','RUNNING') AND l.expires_at>clock_timestamp() AND l.hard_deadline>clock_timestamp()
 AND rl.phase='ACTIVE' AND rl.owner_attempt_id=a.id AND rl.lock_generation=a.resource_lock_generation AND rl.expires_at=l.expires_at
 AND j.current_attempt_id=a.id AND j.fencing_token=a.fencing_token AND j.state='RUNNING')
FROM p11.execution_leases l JOIN p11.attempts a ON a.id=l.attempt_id JOIN p11.jobs j ON j.id=a.job_id
JOIN p11.orders o ON o.id=j.order_id JOIN p11.capabilities c ON c.id=a.capability_id
JOIN p11.resource_locks rl ON rl.resource_id=a.resource_id
WHERE l.attempt_id=$1 AND l.admission_profile=$2 FOR SHARE OF l,a,rl`, attempt, AdmissionProfile).
		Scan(&out.LeaseID, &payload, &manifest, &signerID, &resource, &generation, &fence, &capability, &device, &environment, &manifestID, &manifestHash, &contractHash, &hard, &current)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.Conflict, "Original attempt requires recovery")
	}
	if err != nil {
		return out, err
	}
	if !current || !c.ExistingAdmission || p.BudgetState != "RESERVED" {
		return out, nil
	}
	a, err := e.loadInput(ctx, tx, p)
	if err != nil {
		return out, err
	}
	if !a.expires.After(now) {
		clear(a.ciphertext)
		return out, nil
	}
	if err = e.open(a, in); err != nil {
		return out, err
	}
	var m Manifest
	if strictjson.Decode(manifest, &m, 16384) != nil || m.Digest() != manifestHash || m.InputAsset != a.id || m.InputDigest != a.digest || m.InputBytes != a.bytes {
		return out, fault.New(fault.Unavailable, "Original execution manifest requires recovery")
	}
	// The verification plan comes from authoritative SQL facts, not payload JSON.
	plan := executionlease.Plan{Job: p.JobID, Attempt: attempt, Order: p.OrderID, Seller: p.SellerID, Device: device, Resource: resource, Generation: generation, Capability: capability,
		Manifest: manifestID, ManifestHash: manifestHash, Environment: environment, Fence: fence, ContractHash: contractHash, HardDeadline: hard}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.signer) != ed25519.PrivateKeySize {
		return out, fault.New(fault.Unavailable, "Execution issuer closed")
	}
	v, err := executionlease.Verify(payload, e.verifiers[signerID], plan, now)
	if err != nil {
		return out, nil
	}
	l := v.Details()
	out.Lease = &l
	out.Manifest = &m
	out.Status = "ISSUED"
	return out, nil
}

// IssueTx performs SQL/finite local crypto only. Its return is NOT deliverable
// until the caller confirms commit. Unknown commit must recover the same key.
// Only first issuance is implemented; no automatic replacement or renewal.
func (e *Execution) IssueTx(ctx context.Context, tx pgx.Tx, p identity.Principal, in Request) (Issuance, error) {
	var result Issuance
	if e == nil || e.invocations == nil || tx == nil {
		return result, fault.New(fault.Unavailable, "Execution issuer unavailable")
	}
	prepared, err := e.invocations.PrepareTx(ctx, tx, p, in)
	if err != nil {
		return result, err
	}
	c, err := e.invocations.routes.LockForPreparationTx(ctx, tx, p, in.Model, in.Protocol)
	if err != nil {
		return result, err
	}
	var attempt *string
	var fence string
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT current_attempt_id,fencing_token::text,clock_timestamp() FROM p11.jobs WHERE id=$1`, prepared.JobID).Scan(&attempt, &fence, &now); err != nil {
		return result, err
	}
	if attempt != nil {
		return e.recoverIssued(ctx, tx, c, prepared, *attempt, in, now)
	}
	if !c.ExistingAdmission || prepared.State != "QUEUED" || prepared.BudgetState != "RESERVED" || prepared.ReservedFen != c.Definition.PerCallFen || !prepared.Deadline.After(now) || fence != "0" {
		return result, fault.New(fault.Conflict, "Original input, funding, hold or qualification not ready")
	}
	a, err := e.loadInput(ctx, tx, prepared)
	if err != nil {
		return result, err
	}
	if !a.expires.After(now) {
		clear(a.ciphertext)
		return result, fault.New(fault.Conflict, "Original input expired")
	}
	if err = e.open(a, in); err != nil {
		return result, err
	}
	v, err := e.candidate(ctx, tx, c)
	if err != nil {
		return result, err
	}
	g, err := executionlease.Counter(v.generation)
	// Published SQL counters are signed bigint. The native wire preserves full
	// uint64 strings, but this issuer must stop at its narrower persisted bound.
	if err != nil || g >= math.MaxInt64 {
		return result, fault.New(fault.Conflict, "Physical generation exhausted; manual recovery required")
	}
	generation := strconv.FormatUint(g+1, 10)
	var original trade.Contract
	if strictjson.Decode(c.OrderContract, &original, 65536) != nil {
		return result, fault.New(fault.Unavailable, "Original bounds unavailable")
	}
	hard := prepared.Deadline
	for _, end := range []time.Time{c.CurrentDeadline, a.expires} {
		if end.Before(hard) {
			hard = end
		}
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, err
	}
	if !now.Before(hard) {
		return result, fault.New(fault.Conflict, "Original admission deadline passed")
	}
	expires := now.Add(executionlease.MaximumTerm)
	if hard.Before(expires) {
		expires = hard
	}
	attemptID, leaseID := ids.New("att"), ids.New("lease")
	m := Manifest{Profile: ManifestProfile, ID: ids.New("manifest"), Job: prepared.JobID, Attempt: attemptID, Order: prepared.OrderID, Route: c.ID, Seller: prepared.SellerID,
		Device: v.device, Resource: v.resource, Capability: v.capability, Generation: generation, Fence: "1", ContractHash: c.ContractHash, EnvironmentRef: v.env, EnvironmentDigest: v.environment,
		EngineKind: v.engine, EngineRelease: v.release, ModelDigest: v.model, RecipeDigest: v.recipe, Spec: Spec{Protocol: in.Protocol, Model: in.Model, MaxOutputTokens: in.MaxOutputTokens, Stream: in.Stream},
		InputTokenLimit: original.Spec.InputTokenLimit, InputAsset: a.id, InputDigest: a.digest, InputBytes: a.bytes, Mode: "PLAINTEXT", Privacy: "STANDARD", HardDeadline: hard.UTC().Format(time.RFC3339Nano)}
	l := executionlease.Lease{ID: leaseID, Job: m.Job, Attempt: attemptID, Order: m.Order, Seller: m.Seller, Device: m.Device, Resource: m.Resource, Generation: generation, Capability: m.Capability,
		Manifest: m.ID, ManifestHash: m.Digest(), Environment: m.EnvironmentDigest, Revision: 1, Issued: now.UTC().Format(time.RFC3339Nano), NotBefore: now.UTC().Format(time.RFC3339Nano),
		HardDeadline: m.HardDeadline, Fence: "1", Expires: expires.UTC().Format(time.RFC3339Nano), ContractHash: m.ContractHash, InputAssets: []string{a.id}, Profile: executionlease.Profile}
	message, err := executionlease.Message(l)
	if err != nil {
		return result, fault.New(fault.Unavailable, "Execution lease validation failed")
	}
	e.mu.RLock()
	if len(e.signer) != ed25519.PrivateKeySize {
		e.mu.RUnlock()
		return result, fault.New(fault.Unavailable, "Execution issuer closed")
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(e.signer, message))
	e.mu.RUnlock()
	_, err = tx.Exec(ctx, `INSERT INTO p11.attempts(id,job_id,order_id,seller_id,device_id,resource_id,capability_id,fencing_token,resource_lock_generation,state)
VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,'ISSUED')`, attemptID, m.Job, m.Order, m.Seller, m.Device, m.Resource, m.Capability, generation)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE p11.resource_locks SET lock_generation=$2,phase='ACTIVE',owner_attempt_id=$3,expires_at=$4,version=version+1,updated_at=clock_timestamp() WHERE resource_id=$1`, m.Resource, generation, attemptID, expires)
	if err != nil {
		return result, err
	}
	payload, _ := json.Marshal(l)
	manifest, _ := json.Marshal(m)
	_, err = tx.Exec(ctx, `INSERT INTO p11.execution_leases(id,attempt_id,job_id,fencing_token,resource_id,resource_lock_generation,issued_at,not_before,hard_deadline,execution_manifest_ref,execution_manifest_hash,state,expires_at,signed_payload,signature_profile_id,signature,admission_profile,issuer_key_id,execution_manifest)
VALUES($1,$2,$3,1,$4,$5,$6,$6,$7,$8,$9,'ISSUED',$10,$11,$12,$13,$14,$15,$16)`, leaseID, attemptID, m.Job, m.Resource, generation, now, hard, m.ID, l.ManifestHash, expires, payload, l.Profile, l.Signature, AdmissionProfile, e.signerID, manifest)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE p11.jobs SET current_attempt_id=$2,fencing_token=1,state='RUNNING',version=version+1 WHERE id=$1`, m.Job, attemptID)
	if err != nil {
		return result, err
	}
	safe, _ := json.Marshal(map[string]string{"job_id": m.Job, "attempt_id": attemptID, "lease_id": leaseID, "resource_id": m.Resource, "resource_lock_generation": generation, "contract_hash": m.ContractHash})
	_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'ROUTE_INVOCATION',$2,3,'RouteInvocationAttemptIssued','3.0',$3,'PENDING')`, ids.New("evt"), m.Job, safe)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO p11.durable_jobs(id,kind,dedupe_key,target_ref,state,run_at) VALUES($1,'DELIVER_EXECUTION_LEASE',$2::text,$2::text,'PENDING',clock_timestamp())`, ids.New("task"), leaseID)
	if err != nil {
		return result, err
	}
	prepared.State = "RUNNING"
	return Issuance{Preparation: prepared, AttemptID: attemptID, LeaseID: leaseID, Status: "ISSUED", Lease: &l, Manifest: &m}, nil
}
