-- Forward-only signed continuation. 001-011 remain immutable. No new job,
-- attempt, budget, usage, delivery acceptance or payment authority is created.
CREATE TABLE p11.native_lease_continuations (
  id p11.entity_id PRIMARY KEY,
  report_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.native_execution_reports(id) DEFERRABLE INITIALLY DEFERRED,
  lease_id p11.entity_id NOT NULL REFERENCES p11.execution_leases(id),
  attempt_id p11.entity_id NOT NULL,
  job_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL CHECK(fencing_token>0),
  resource_lock_generation p11.counter NOT NULL CHECK(resource_lock_generation>0),
  previous_revision integer NOT NULL CHECK(previous_revision>0 AND previous_revision<2147483647),
  lease_revision integer NOT NULL CHECK(lease_revision::bigint=previous_revision::bigint+1),
  previous_identity p11.sha256_hex NOT NULL,
  signed_identity p11.sha256_hex NOT NULL CHECK(signed_identity<>previous_identity),
  previous_payload jsonb NOT NULL CHECK(jsonb_typeof(previous_payload)='object'),
  signed_payload jsonb NOT NULL CHECK(jsonb_typeof(signed_payload)='object'),
  previous_expires_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK(expires_at>previous_expires_at),
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  FOREIGN KEY(attempt_id,resource_id,resource_lock_generation) REFERENCES p11.attempts(id,resource_id,resource_lock_generation),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  UNIQUE(lease_id,lease_revision)
);

CREATE FUNCTION p11.guard_native_lease_continuation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l p11.execution_leases%ROWTYPE; a p11.attempts%ROWTYPE; lk p11.resource_locks%ROWTYPE;
  mutable text[]:=ARRAY['lease_revision','issued_at','expires_at','signature'];
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'signed continuation history is append-only'; END IF;
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=NEW.resource_id;
  IF l.admission_profile<>'P11_NATIVE_TEXT_ADMISSION_1' OR l.claim_profile<>'P11_NATIVE_LEASE_CLAIM_1'
    OR l.claim_device_id IS DISTINCT FROM NEW.device_id OR l.state<>'ACKED'
    OR ROW(l.attempt_id,l.job_id,l.resource_id,l.fencing_token,l.resource_lock_generation,l.lease_revision,l.expires_at,l.signed_payload)
      IS DISTINCT FROM ROW(NEW.attempt_id,NEW.job_id,NEW.resource_id,NEW.fencing_token,NEW.resource_lock_generation,NEW.previous_revision,NEW.previous_expires_at,NEW.previous_payload)
    OR a.device_id<>NEW.device_id OR a.seller_id<>NEW.seller_id OR a.state NOT IN ('ACKED','RUNNING')
    OR lk.phase<>'ACTIVE' OR lk.owner_attempt_id IS DISTINCT FROM NEW.attempt_id OR lk.owner_session_id IS NOT NULL
    OR lk.lock_generation<>NEW.resource_lock_generation OR lk.expires_at<>NEW.previous_expires_at
    OR NEW.previous_expires_at<=clock_timestamp() OR NEW.recorded_at<l.issued_at OR NEW.recorded_at>=NEW.previous_expires_at
    OR NEW.recorded_at>clock_timestamp() OR NEW.expires_at>l.hard_deadline OR NEW.expires_at-NEW.recorded_at>interval '45 seconds'
    OR (NEW.signed_payload-mutable) IS DISTINCT FROM (NEW.previous_payload-mutable)
    OR NEW.signed_payload->'lease_revision' IS DISTINCT FROM to_jsonb(NEW.lease_revision)
    OR (NEW.signed_payload->>'issued_at')::timestamptz IS DISTINCT FROM NEW.recorded_at
    OR (NEW.signed_payload->>'expires_at')::timestamptz IS DISTINCT FROM NEW.expires_at
    OR NEW.signed_payload->>'signature' IS NULL OR NEW.signed_payload->>'signature' !~ '^[A-Za-z0-9_-]{86}$'
  THEN RAISE EXCEPTION 'continuation requires the same live claimed owner, plan and deadline'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER native_lease_continuation_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_lease_continuations
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_lease_continuation();

CREATE FUNCTION p11.check_native_lease_continuation_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l p11.execution_leases%ROWTYPE; lk p11.resource_locks%ROWTYPE; r p11.native_execution_reports%ROWTYPE;
BEGIN
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=NEW.resource_id;
  SELECT * INTO STRICT r FROM p11.native_execution_reports WHERE id=NEW.report_id;
  IF l.lease_revision<>NEW.lease_revision OR l.signed_payload IS DISTINCT FROM NEW.signed_payload
    OR l.expires_at<>NEW.expires_at OR l.state<>'ACKED'
    OR lk.phase<>'ACTIVE' OR lk.owner_attempt_id IS DISTINCT FROM NEW.attempt_id OR lk.owner_session_id IS NOT NULL
    OR lk.lock_generation<>NEW.resource_lock_generation OR lk.expires_at<>NEW.expires_at
    OR ROW(r.lease_id,r.attempt_id,r.job_id,r.device_id,r.seller_id,r.resource_id,r.fencing_token,r.resource_lock_generation)
      IS DISTINCT FROM ROW(NEW.lease_id,NEW.attempt_id,NEW.job_id,NEW.device_id,NEW.seller_id,NEW.resource_id,NEW.fencing_token,NEW.resource_lock_generation)
    OR r.kind<>'PROGRESS' OR r.safe_request->>'phase' NOT IN ('ACKED','RUNNING')
    OR r.safe_response->>'action' IS DISTINCT FROM 'CONTINUE' OR r.safe_response->'lease' IS DISTINCT FROM NEW.signed_payload
  THEN RAISE EXCEPTION 'signed continuation, progress receipt and physical expiry must commit together'; END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER native_lease_continuation_atomic AFTER INSERT ON p11.native_lease_continuations
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_lease_continuation_atomic();

-- Preserve all original insert checks; only recorded live continuations may change time.
CREATE OR REPLACE FUNCTION p11.guard_native_lease() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a p11.attempts%ROWTYPE; j p11.jobs%ROWTYPE; c p11.capabilities%ROWTYPE; o p11.orders%ROWTYPE;
  m jsonb; p jsonb; ast p11.assets%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.admission_profile<>'UNBOUND_LEASE' THEN RAISE EXCEPTION 'original issued lease is retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='UPDATE' THEN
    IF NEW.admission_profile IS DISTINCT FROM OLD.admission_profile THEN RAISE EXCEPTION 'native admission cannot be retrofitted'; END IF;
    IF OLD.admission_profile<>'UNBOUND_LEASE' THEN
      IF ROW(NEW.id,NEW.attempt_id,NEW.job_id,NEW.fencing_token,NEW.resource_id,NEW.resource_lock_generation,
        NEW.not_before,NEW.hard_deadline,NEW.execution_manifest_ref,NEW.execution_manifest_hash,NEW.signature_profile_id,
        NEW.issuer_key_id,NEW.execution_manifest,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.attempt_id,OLD.job_id,OLD.fencing_token,OLD.resource_id,OLD.resource_lock_generation,
        OLD.not_before,OLD.hard_deadline,OLD.execution_manifest_ref,OLD.execution_manifest_hash,OLD.signature_profile_id,
        OLD.issuer_key_id,OLD.execution_manifest,OLD.created_at)
      THEN RAISE EXCEPTION 'continuation cannot rewrite original native authority'; END IF;
      IF OLD.state IN ('REVOKED','EXPIRED','CLOSED') AND NEW.state<>OLD.state THEN RAISE EXCEPTION 'native revoked lease cannot be restored'; END IF;
      IF NEW.last_progress_sequence<OLD.last_progress_sequence THEN RAISE EXCEPTION 'native progress cannot rewind'; END IF;
      IF ROW(NEW.lease_revision,NEW.issued_at,NEW.expires_at,NEW.signed_payload,NEW.signature)
        IS DISTINCT FROM ROW(OLD.lease_revision,OLD.issued_at,OLD.expires_at,OLD.signed_payload,OLD.signature) THEN
        IF OLD.state NOT IN ('ISSUED','ACKED') OR NEW.state<>'ACKED' OR OLD.expires_at<=clock_timestamp()
          OR NOT EXISTS(SELECT 1 FROM p11.native_lease_continuations c WHERE c.lease_id=OLD.id
            AND c.previous_revision=OLD.lease_revision AND c.lease_revision=NEW.lease_revision
            AND c.previous_payload=OLD.signed_payload AND c.signed_payload=NEW.signed_payload
            AND c.previous_expires_at=OLD.expires_at AND c.expires_at=NEW.expires_at AND c.recorded_at=NEW.issued_at
            AND NEW.signature=NEW.signed_payload->>'signature')
        THEN RAISE EXCEPTION 'native lease extension requires its original append-only continuation'; END IF;
      END IF;
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

CREATE OR REPLACE FUNCTION p11.check_native_execution_report_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l p11.execution_leases%ROWTYPE; a p11.attempts%ROWTYPE; lk p11.resource_locks%ROWTYPE;
BEGIN
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=NEW.resource_id;
  IF NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='EXECUTION_REPORT' AND e.aggregate_id=NEW.id
    AND e.aggregate_version=1 AND e.event_type='ExecutionReportRecorded' AND e.safe_payload=jsonb_build_object(
      'report_id',NEW.id,'lease_id',NEW.lease_id,'attempt_id',NEW.attempt_id,'job_id',NEW.job_id,'device_id',NEW.device_id,
      'resource_id',NEW.resource_id,'fencing_token',NEW.fencing_token::text,'resource_lock_generation',NEW.resource_lock_generation::text,
      'kind',NEW.kind,'sequence',NEW.sequence::text))
  THEN RAISE EXCEPTION 'execution report and bounded safe event must commit together'; END IF;
  IF NEW.kind='PROGRESS' THEN
    IF NEW.safe_request->>'sequence' IS DISTINCT FROM NEW.sequence::text OR l.last_progress_sequence<NEW.sequence
      OR NEW.safe_response->>'action' IS NULL OR NEW.safe_response->>'action' NOT IN ('STOP','NO_CHANGE','CONTINUE')
    THEN RAISE EXCEPTION 'original progress sequence/action mismatch'; END IF;
    IF NEW.safe_response->>'action'='CONTINUE' THEN
      IF NOT EXISTS(SELECT 1 FROM p11.native_lease_continuations c WHERE c.report_id=NEW.id AND c.lease_id=NEW.lease_id
        AND c.signed_payload=NEW.safe_response->'lease' AND c.lease_revision=l.lease_revision)
      THEN RAISE EXCEPTION 'CONTINUE requires its atomically signed original lease'; END IF;
    ELSIF NEW.safe_response ? 'lease' THEN RAISE EXCEPTION 'non-continuation progress cannot carry a lease'; END IF;
  ELSIF NEW.safe_request->>'state'='ACKED' THEN
    IF NEW.safe_request->'work_stopped' IS DISTINCT FROM 'true'::jsonb OR NEW.safe_request->'tokens_revoked' IS DISTINCT FROM 'true'::jsonb
      OR NEW.safe_request->'workspace_cleared' IS DISTINCT FROM 'true'::jsonb
      OR a.state<>'CLEANED' OR a.cleanup_state<>'ACKED' OR l.state<>'CLOSED'
      OR lk.phase<>'FREE' OR lk.owner_attempt_id IS NOT NULL OR lk.owner_session_id IS NOT NULL OR lk.expires_at IS NOT NULL
      OR lk.lock_generation<>NEW.resource_lock_generation OR NEW.safe_response->>'status'<>'CLEANED'
    THEN RAISE EXCEPTION 'acknowledged cleanup and exact resource release must commit together'; END IF;
  ELSE
    IF NEW.safe_request->>'state'<>'FAILED' OR a.state<>'QUARANTINED' OR a.cleanup_state<>'FAILED' OR l.state<>'REVOKED'
      OR lk.phase<>'QUARANTINED' OR lk.owner_attempt_id IS DISTINCT FROM NEW.attempt_id
      OR lk.lock_generation<>NEW.resource_lock_generation OR NEW.safe_response->>'status'<>'QUARANTINED'
    THEN RAISE EXCEPTION 'failed cleanup must retain quarantined original owner'; END IF;
  END IF;
  RETURN NEW;
END $$;
