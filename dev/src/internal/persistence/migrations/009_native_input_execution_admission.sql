-- R017 forward-only. SQL checks atomic metadata, NOT signatures, HMAC,
-- tokenizer bounds, current user authorization or hardware/qualification truth.
-- Application admission must perform those checks in the same transaction.
ALTER TABLE p11.assets
  ADD COLUMN input_profile text NOT NULL DEFAULT 'UNBOUND_ASSET'
    CHECK(input_profile IN ('UNBOUND_ASSET','P11_NATIVE_TEXT_INPUT_AESGCM_1')),
  ADD COLUMN input_key_version text,
  ADD COLUMN input_ciphertext bytea,
  ADD CONSTRAINT native_input_shape CHECK
    ((input_profile='UNBOUND_ASSET' AND input_key_version IS NULL AND input_ciphertext IS NULL) OR
     (input_profile='P11_NATIVE_TEXT_INPUT_AESGCM_1' AND job_id IS NOT NULL AND attempt_id IS NULL
      AND visibility='PLAINTEXT' AND state IN ('READY','QUARANTINED') AND deleted_at IS NULL
      AND media_type='application/json' AND storage_digest IS NOT NULL
      AND bytes BETWEEN 1 AND 262144 AND octet_length(input_ciphertext)=bytes+28
      AND input_key_version IS NOT NULL AND input_key_version ~ '^[A-Za-z][A-Za-z0-9_-]{2,127}$'
      AND input_ciphertext IS NOT NULL));
CREATE UNIQUE INDEX native_input_one_per_job ON p11.assets(job_id)
  WHERE input_profile='P11_NATIVE_TEXT_INPUT_AESGCM_1';
ALTER TABLE p11.jobs ADD COLUMN input_asset_id p11.entity_id REFERENCES p11.assets(id);
CREATE UNIQUE INDEX native_input_bound_once ON p11.jobs(input_asset_id) WHERE input_asset_id IS NOT NULL;
ALTER TABLE p11.execution_leases
  ADD COLUMN admission_profile text NOT NULL DEFAULT 'UNBOUND_LEASE'
    CHECK(admission_profile IN ('UNBOUND_LEASE','P11_NATIVE_TEXT_ADMISSION_1')),
  ADD COLUMN issuer_key_id text,
  ADD COLUMN execution_manifest jsonb,
  ADD CONSTRAINT native_admission_shape CHECK
    ((admission_profile='UNBOUND_LEASE' AND issuer_key_id IS NULL AND execution_manifest IS NULL) OR
     (admission_profile='P11_NATIVE_TEXT_ADMISSION_1' AND issuer_key_id IS NOT NULL
      AND issuer_key_id ~ '^[A-Za-z][A-Za-z0-9_-]{2,127}$'
      AND execution_manifest IS NOT NULL AND jsonb_typeof(execution_manifest)='object'
      AND signature_profile_id='P11_EXECUTION_LEASE_ED25519_1'));

CREATE FUNCTION p11.guard_native_input() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j p11.jobs%ROWTYPE; o p11.orders%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.input_profile<>'UNBOUND_ASSET' THEN RAISE EXCEPTION 'original native input is retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='UPDATE' THEN
    IF NEW.input_profile IS DISTINCT FROM OLD.input_profile OR
      (OLD.input_profile<>'UNBOUND_ASSET' AND
       (ROW(NEW.id,NEW.order_id,NEW.owner_id,NEW.job_id,NEW.attempt_id,NEW.visibility,NEW.storage_key,
          NEW.storage_digest,NEW.bytes,NEW.media_type,NEW.expires_at,NEW.deleted_at,NEW.input_key_version,NEW.input_ciphertext,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.owner_id,OLD.job_id,OLD.attempt_id,OLD.visibility,OLD.storage_key,
          OLD.storage_digest,OLD.bytes,OLD.media_type,OLD.expires_at,OLD.deleted_at,OLD.input_key_version,OLD.input_ciphertext,OLD.created_at)
        OR OLD.state='QUARANTINED' AND NEW.state<>'QUARANTINED')) THEN
      RAISE EXCEPTION 'native input cannot be replaced or retrofitted';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.input_profile='UNBOUND_ASSET' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.job_id FOR SHARE;
  SELECT * INTO STRICT o FROM p11.orders WHERE id=j.order_id FOR SHARE;
  IF j.invocation_profile<>'ROUTE_PREPARE_1' OR j.state<>'INPUT_PENDING' OR j.input_asset_id IS NOT NULL
    OR NEW.order_id<>j.order_id OR NEW.owner_id<>o.buyer_id OR NEW.expires_at>j.deadline
    OR NEW.expires_at<=clock_timestamp() OR NEW.state<>'READY' OR NEW.version<>1 OR NEW.storage_key<>NEW.id THEN
    RAISE EXCEPTION 'native input requires the original pending buyer call';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER native_input_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.assets
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_input();

CREATE FUNCTION p11.guard_native_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.invocation_profile='UNBOUND_JOB' THEN
    IF NEW.input_asset_id IS NOT NULL THEN RAISE EXCEPTION 'unbound history has no native input'; END IF;
    RETURN NEW;
  END IF;
  IF TG_OP='INSERT' THEN
    IF NEW.input_asset_id IS NOT NULL THEN RAISE EXCEPTION 'native call starts input pending'; END IF;
  ELSE
    IF OLD.input_asset_id IS DISTINCT FROM NEW.input_asset_id AND
      (OLD.input_asset_id IS NOT NULL OR NEW.input_asset_id IS NULL OR OLD.state<>'INPUT_PENDING' OR NEW.state<>'QUEUED') THEN
      RAISE EXCEPTION 'original input binds once before queueing';
    END IF;
    IF OLD.current_attempt_id IS NOT NULL AND
      ROW(OLD.current_attempt_id,OLD.fencing_token) IS DISTINCT FROM ROW(NEW.current_attempt_id,NEW.fencing_token) THEN
      RAISE EXCEPTION 'native initial attempt cannot be replaced';
    END IF;
    IF OLD.state NOT IN ('INPUT_PENDING','QUEUED','RUNNING') AND NEW.state IN ('INPUT_PENDING','QUEUED','RUNNING')
      OR OLD.state='RUNNING' AND NEW.state IN ('INPUT_PENDING','QUEUED') THEN
      RAISE EXCEPTION 'native call cannot be reissued';
    END IF;
    IF OLD.current_attempt_id IS NULL AND NEW.current_attempt_id IS NOT NULL AND
      (OLD.state<>'QUEUED' OR NEW.state<>'RUNNING' OR NEW.fencing_token<>1) THEN
      RAISE EXCEPTION 'native first attempt requires queued input and first fence';
    END IF;
  END IF;
  IF NEW.state IN ('QUEUED','RUNNING') AND NEW.input_asset_id IS NULL OR
    NEW.state='INPUT_PENDING' AND NEW.input_asset_id IS NOT NULL OR
    NEW.state='RUNNING' AND NEW.current_attempt_id IS NULL OR
    NEW.current_attempt_id IS NULL AND NEW.fencing_token<>0 OR
    NEW.current_attempt_id IS NOT NULL AND NEW.fencing_token<>1 THEN
    RAISE EXCEPTION 'native call state requires original input and attempt';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER native_job_guard BEFORE INSERT OR UPDATE ON p11.jobs
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_job();

CREATE FUNCTION p11.guard_native_lease() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a p11.attempts%ROWTYPE; j p11.jobs%ROWTYPE; c p11.capabilities%ROWTYPE; o p11.orders%ROWTYPE;
  m jsonb; p jsonb; ast p11.assets%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.admission_profile<>'UNBOUND_LEASE' THEN RAISE EXCEPTION 'original issued lease is retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='UPDATE' THEN
    IF NEW.admission_profile IS DISTINCT FROM OLD.admission_profile OR
      (OLD.admission_profile<>'UNBOUND_LEASE' AND
       ROW(NEW.id,NEW.attempt_id,NEW.job_id,NEW.fencing_token,NEW.resource_id,NEW.resource_lock_generation,
          NEW.lease_revision,NEW.issued_at,NEW.not_before,NEW.hard_deadline,NEW.execution_manifest_ref,
          NEW.execution_manifest_hash,NEW.expires_at,NEW.signed_payload,NEW.signature_profile_id,NEW.signature,
          NEW.issuer_key_id,NEW.execution_manifest,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.attempt_id,OLD.job_id,OLD.fencing_token,OLD.resource_id,OLD.resource_lock_generation,
          OLD.lease_revision,OLD.issued_at,OLD.not_before,OLD.hard_deadline,OLD.execution_manifest_ref,
          OLD.execution_manifest_hash,OLD.expires_at,OLD.signed_payload,OLD.signature_profile_id,OLD.signature,
          OLD.issuer_key_id,OLD.execution_manifest,OLD.created_at)) THEN
      RAISE EXCEPTION 'native initial lease cannot be rewritten or retrofitted';
    END IF;
    IF OLD.admission_profile<>'UNBOUND_LEASE' AND OLD.state IN ('REVOKED','EXPIRED','CLOSED') AND NEW.state<>OLD.state THEN
      RAISE EXCEPTION 'native revoked lease cannot be restored';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.admission_profile='UNBOUND_LEASE' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT j FROM p11.jobs WHERE id=a.job_id;
  SELECT * INTO STRICT o FROM p11.orders WHERE id=j.order_id;
  SELECT * INTO STRICT c FROM p11.capabilities WHERE id=a.capability_id;
  SELECT * INTO STRICT ast FROM p11.assets WHERE id=j.input_asset_id;
  m:=NEW.execution_manifest; p:=NEW.signed_payload;
  IF j.invocation_profile<>'ROUTE_PREPARE_1' OR j.state<>'QUEUED' OR a.state<>'ISSUED' OR NEW.state<>'ISSUED'
    OR NEW.lease_revision<>1 OR NEW.last_progress_sequence<>0 OR NEW.issued_at>clock_timestamp()
    OR NEW.expires_at-NEW.issued_at>interval '45 seconds' OR NEW.hard_deadline>j.deadline
    OR NEW.hard_deadline>ast.expires_at OR ast.state<>'READY' OR ast.input_profile<>'P11_NATIVE_TEXT_INPUT_AESGCM_1'
    OR m IS DISTINCT FROM jsonb_build_object(
      'profile_id','P11_NATIVE_TEXT_MANIFEST_1','execution_manifest_ref',NEW.execution_manifest_ref::text,
      'job_id',j.id::text,'attempt_id',a.id::text,'order_id',o.id::text,'route_id',j.route_contract_id::text,
      'seller_id',o.seller_id::text,'device_id',a.device_id::text,'resource_id',a.resource_id::text,
      'capability_id',a.capability_id::text,'resource_lock_generation',a.resource_lock_generation::text,
      'fencing_token',a.fencing_token::text,'contract_hash',o.contract_hash::text,
      'kernel_environment_ref',c.kernel_environment_ref::text,'kernel_environment_digest',c.kernel_environment_digest::text,
      'engine_kind',c.engine_kind,'engine_release',c.engine_release,'model_digest',c.model_digest::text,'recipe_digest',c.recipe_digest::text,
      'request_spec',j.invocation_spec,'input_token_limit',(o.contract_snapshot->'public_spec'->>'input_token_limit')::bigint,
      'input_asset_ref',ast.id::text,'input_ciphertext_digest',ast.storage_digest::text,'input_bytes',ast.bytes,
      'mode','PLAINTEXT','privacy','STANDARD','hard_deadline',m->>'hard_deadline')
    OR (m->>'hard_deadline')::timestamptz IS DISTINCT FROM NEW.hard_deadline
    OR (SELECT count(*) FROM jsonb_object_keys(p))<>22
    OR p->>'lease_id' IS DISTINCT FROM NEW.id::text OR p->>'job_id' IS DISTINCT FROM j.id::text
    OR p->>'attempt_id' IS DISTINCT FROM a.id::text OR p->>'order_id' IS DISTINCT FROM o.id::text
    OR p->>'seller_id' IS DISTINCT FROM o.seller_id::text OR p->>'device_id' IS DISTINCT FROM a.device_id::text
    OR p->>'resource_id' IS DISTINCT FROM a.resource_id::text OR p->>'capability_id' IS DISTINCT FROM a.capability_id::text
    OR p->>'resource_lock_generation' IS DISTINCT FROM NEW.resource_lock_generation::text OR p->>'fencing_token' IS DISTINCT FROM NEW.fencing_token::text
    OR p->>'execution_manifest_ref' IS DISTINCT FROM NEW.execution_manifest_ref::text OR p->>'execution_manifest_hash' IS DISTINCT FROM NEW.execution_manifest_hash::text
    OR p->>'kernel_environment_digest' IS DISTINCT FROM c.kernel_environment_digest::text
    OR p->'lease_revision' IS DISTINCT FROM '1'::jsonb OR p->>'contract_hash' IS DISTINCT FROM o.contract_hash::text
    OR p->'input_asset_refs' IS DISTINCT FROM jsonb_build_array(ast.id::text)
    OR p->>'signature_profile_id' IS DISTINCT FROM NEW.signature_profile_id OR p->>'signature' IS DISTINCT FROM NEW.signature
    OR (p->>'issued_at')::timestamptz IS DISTINCT FROM NEW.issued_at OR (p->>'not_before')::timestamptz IS DISTINCT FROM NEW.not_before
    OR (p->>'expires_at')::timestamptz IS DISTINCT FROM NEW.expires_at OR (p->>'hard_deadline')::timestamptz IS DISTINCT FROM NEW.hard_deadline THEN
    RAISE EXCEPTION 'native lease must bind exact original input, kernel, plan and both fences';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER native_lease_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.execution_leases
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_lease();

CREATE FUNCTION p11.verify_native_admission() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j p11.jobs%ROWTYPE; r p11.route_contracts%ROWTYPE; a p11.attempts%ROWTYPE; l p11.execution_leases%ROWTYPE;
BEGIN
  IF NEW.invocation_profile<>'ROUTE_PREPARE_1' THEN RETURN NULL; END IF;
  -- Read final row, not a queued intermediate snapshot from this transaction.
  SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.id;
  SELECT * INTO STRICT r FROM p11.route_contracts WHERE id=j.route_contract_id;
  IF j.input_asset_id IS NOT NULL AND
    (NOT EXISTS(SELECT 1 FROM p11.assets ast WHERE ast.id=j.input_asset_id AND ast.job_id=j.id AND ast.order_id=j.order_id
       AND ast.input_profile='P11_NATIVE_TEXT_INPUT_AESGCM_1') OR
     NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='ROUTE_INVOCATION' AND e.aggregate_id=j.id
       AND e.aggregate_version=2 AND e.event_type='RouteInvocationInputReady' AND e.schema_version='3.0'
       AND e.safe_payload=jsonb_build_object('job_id',j.id::text,'asset_id',j.input_asset_id::text,'order_id',j.order_id::text,'contract_hash',r.contract_hash::text))) THEN
    RAISE EXCEPTION 'native input and safe readiness event commit together';
  END IF;
  IF TG_OP='INSERT' OR NEW.current_attempt_id IS NOT DISTINCT FROM OLD.current_attempt_id THEN RETURN NULL; END IF;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=j.current_attempt_id;
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE attempt_id=a.id AND admission_profile='P11_NATIVE_TEXT_ADMISSION_1';
  IF NOT EXISTS(SELECT 1 FROM p11.resource_locks rl WHERE rl.resource_id=a.resource_id AND rl.phase='ACTIVE'
      AND rl.owner_attempt_id=a.id AND rl.lock_generation=a.resource_lock_generation AND rl.expires_at=l.expires_at) OR
    NOT EXISTS(SELECT 1 FROM p11.budget_reservations b WHERE b.id=j.reservation_id AND b.order_id=j.order_id
      AND b.purpose_ref=j.id AND b.state='RESERVED' AND b.accounting_profile='ORDER_CAPACITY_1'
      AND b.amount_fen=(r.definition->>'budget_per_call_fen')::bigint) OR
    NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='ROUTE_INVOCATION' AND e.aggregate_id=j.id
      AND e.aggregate_version=3 AND e.event_type='RouteInvocationAttemptIssued' AND e.schema_version='3.0'
      AND e.safe_payload=jsonb_build_object('job_id',j.id::text,'attempt_id',a.id::text,'lease_id',l.id::text,
        'resource_id',a.resource_id::text,'resource_lock_generation',a.resource_lock_generation::text,'contract_hash',r.contract_hash::text)) OR
    NOT EXISTS(SELECT 1 FROM p11.durable_jobs d WHERE d.kind='DELIVER_EXECUTION_LEASE' AND d.dedupe_key=l.id
      AND d.target_ref=l.id AND d.state='PENDING') THEN
    RAISE EXCEPTION 'native initial attempt, hold, physical lock, lease, event and delivery intent commit together';
  END IF;
  RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER native_admission_atomic AFTER INSERT OR UPDATE ON p11.jobs
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.verify_native_admission();

-- Prevent committing orphan native input/lease/attempt rows without the job
-- transition that schedules their stronger aggregate checks above.
CREATE FUNCTION p11.verify_native_children() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j p11.jobs%ROWTYPE;
BEGIN
  IF TG_TABLE_NAME='assets' THEN
    IF NEW.input_profile='UNBOUND_ASSET' THEN RETURN NULL; END IF;
    SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.job_id;
    IF j.input_asset_id IS DISTINCT FROM NEW.id THEN RAISE EXCEPTION 'native input must bind original job atomically'; END IF;
  ELSIF TG_TABLE_NAME='execution_leases' THEN
    IF NEW.admission_profile='UNBOUND_LEASE' THEN RETURN NULL; END IF;
    SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.job_id;
    IF j.current_attempt_id IS DISTINCT FROM NEW.attempt_id OR j.state<>'RUNNING' THEN
      RAISE EXCEPTION 'native lease must bind current initial job atomically';
    END IF;
  ELSE
    SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.job_id;
    IF j.invocation_profile='ROUTE_PREPARE_1' AND (j.current_attempt_id IS DISTINCT FROM NEW.id OR
      NOT EXISTS(SELECT 1 FROM p11.execution_leases l WHERE l.attempt_id=NEW.id AND l.admission_profile='P11_NATIVE_TEXT_ADMISSION_1')) THEN
      RAISE EXCEPTION 'native attempt must bind current initial lease atomically';
    END IF;
  END IF;
  RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER native_input_atomic AFTER INSERT ON p11.assets
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.verify_native_children();
CREATE CONSTRAINT TRIGGER native_lease_atomic AFTER INSERT ON p11.execution_leases
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.verify_native_children();
CREATE CONSTRAINT TRIGGER native_attempt_atomic AFTER INSERT ON p11.attempts
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.verify_native_children();
