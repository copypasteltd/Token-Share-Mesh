-- Separate append-only recovery of an original FAILED cleanup. A revoked
-- execution lease stays revoked. Historical failure, budget and delivery stay.
CREATE TABLE p11.native_cleanup_recoveries (
  id p11.entity_id PRIMARY KEY,
  original_report_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.native_execution_reports(id),
  lease_id p11.entity_id NOT NULL REFERENCES p11.execution_leases(id),
  attempt_id p11.entity_id NOT NULL,
  job_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL CHECK(fencing_token>0),
  resource_lock_generation p11.counter NOT NULL CHECK(resource_lock_generation>0),
  key_digest p11.sha256_hex NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  cleanup_manifest_hash p11.sha256_hex NOT NULL,
  safe_request jsonb NOT NULL CHECK(jsonb_typeof(safe_request)='object'),
  safe_response jsonb NOT NULL CHECK(jsonb_typeof(safe_response)='object'),
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  FOREIGN KEY(attempt_id,resource_id,resource_lock_generation) REFERENCES p11.attempts(id,resource_id,resource_lock_generation),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  UNIQUE(device_id,attempt_id,key_digest)
);
CREATE FUNCTION p11.guard_native_cleanup_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r p11.native_execution_reports%ROWTYPE; l p11.execution_leases%ROWTYPE;
  a p11.attempts%ROWTYPE; lk p11.resource_locks%ROWTYPE; k text;
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'cleanup recovery history is append-only'; END IF;
  SELECT * INTO STRICT r FROM p11.native_execution_reports WHERE id=NEW.original_report_id;
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=NEW.resource_id;
  IF ROW(r.lease_id,r.attempt_id,r.job_id,r.device_id,r.seller_id,r.resource_id,r.fencing_token,r.resource_lock_generation)
    IS DISTINCT FROM ROW(NEW.lease_id,NEW.attempt_id,NEW.job_id,NEW.device_id,NEW.seller_id,NEW.resource_id,NEW.fencing_token,NEW.resource_lock_generation)
    OR r.kind<>'CLEANUP' OR r.sequence<>0 OR r.safe_request->>'state' IS DISTINCT FROM 'FAILED'
    OR r.safe_response->>'status' IS DISTINCT FROM 'QUARANTINED'
    OR l.admission_profile<>'P11_NATIVE_TEXT_ADMISSION_1' OR l.claim_profile<>'P11_NATIVE_LEASE_CLAIM_1'
    OR l.state<>'REVOKED' OR l.claim_device_id IS DISTINCT FROM NEW.device_id OR l.attempt_id<>NEW.attempt_id
    OR a.state<>'QUARANTINED' OR a.cleanup_state<>'FAILED'
    OR lk.phase<>'QUARANTINED' OR lk.owner_attempt_id IS DISTINCT FROM NEW.attempt_id OR lk.owner_session_id IS NOT NULL
    OR lk.lock_generation<>NEW.resource_lock_generation OR NEW.recorded_at<r.recorded_at OR NEW.recorded_at>clock_timestamp()
    OR NEW.safe_request->>'lease_id' IS DISTINCT FROM NEW.lease_id::text
    OR NEW.safe_request->>'fencing_token' IS DISTINCT FROM NEW.fencing_token::text
    OR NEW.safe_request->>'recovery_of' IS DISTINCT FROM NEW.original_report_id::text
    OR NEW.safe_request->>'cleanup_manifest_hash' IS DISTINCT FROM NEW.cleanup_manifest_hash::text
    OR NEW.safe_request->>'state' IS DISTINCT FROM 'ACKED'
    OR NEW.safe_request->>'signature_profile_id' IS DISTINCT FROM 'P11_DEVICE_CLEANUP_RECOVERY_1'
    OR NEW.safe_request->'work_stopped' IS DISTINCT FROM 'true'::jsonb
    OR NEW.safe_request->'tokens_revoked' IS DISTINCT FROM 'true'::jsonb
    OR NEW.safe_request->'workspace_cleared' IS DISTINCT FROM 'true'::jsonb
    OR NEW.safe_request->>'signature' IS NULL OR NEW.safe_request->>'signature' !~ '^[A-Za-z0-9_-]{86}$'
    OR (NEW.safe_request->>'recorded_at')::timestamptz<l.issued_at
    OR (NEW.safe_request->>'recorded_at')::timestamptz>NEW.recorded_at+interval '60 seconds'
    OR NEW.safe_response->>'id' IS DISTINCT FROM NEW.id::text OR NEW.safe_response->>'status' IS DISTINCT FROM 'CLEANED'
  THEN RAISE EXCEPTION 'recovery must prove all cleanup facts for its original failed occupant'; END IF;
  FOR k IN SELECT jsonb_object_keys(NEW.safe_request) LOOP
    IF k NOT IN ('lease_id','fencing_token','cleanup_manifest_hash','state','work_stopped','tokens_revoked','workspace_cleared','recorded_at','signature_profile_id','signature','recovery_of')
    THEN RAISE EXCEPTION 'cleanup recovery cannot contain customer content or extra fields'; END IF;
  END LOOP;
  RETURN NEW;
END $$;
CREATE TRIGGER native_cleanup_recovery_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_cleanup_recoveries
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_cleanup_recovery();

CREATE FUNCTION p11.check_native_cleanup_recovery_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l p11.execution_leases%ROWTYPE; a p11.attempts%ROWTYPE; lk p11.resource_locks%ROWTYPE;
BEGIN
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=NEW.resource_id;
  IF l.state<>'REVOKED' OR a.state<>'CLEANED' OR a.cleanup_state<>'ACKED'
    OR lk.phase<>'FREE' OR lk.owner_attempt_id IS NOT NULL OR lk.owner_session_id IS NOT NULL OR lk.expires_at IS NOT NULL
    OR lk.lock_generation<>NEW.resource_lock_generation
    OR NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='EXECUTION_CLEANUP_RECOVERY' AND e.aggregate_id=NEW.id
      AND e.aggregate_version=1 AND e.event_type='ExecutionCleanupRecovered' AND e.safe_payload=jsonb_build_object(
        'recovery_id',NEW.id,'original_report_id',NEW.original_report_id,'lease_id',NEW.lease_id,'attempt_id',NEW.attempt_id,'job_id',NEW.job_id,
        'device_id',NEW.device_id,'resource_id',NEW.resource_id,'fencing_token',NEW.fencing_token::text,'resource_lock_generation',NEW.resource_lock_generation::text))
  THEN RAISE EXCEPTION 'recovery, exact physical release and safe event must commit together'; END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER native_cleanup_recovery_atomic AFTER INSERT ON p11.native_cleanup_recoveries
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_cleanup_recovery_atomic();

CREATE OR REPLACE FUNCTION p11.check_native_owner_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.owner_attempt_id IS NOT NULL AND
    (NEW.owner_attempt_id IS DISTINCT FROM OLD.owner_attempt_id OR NEW.phase='FREE') AND
    EXISTS(SELECT 1 FROM p11.execution_leases l WHERE l.attempt_id=OLD.owner_attempt_id AND l.claim_profile='P11_NATIVE_LEASE_CLAIM_1') AND
    NOT EXISTS(SELECT 1 FROM p11.native_execution_reports r WHERE r.attempt_id=OLD.owner_attempt_id
      AND r.resource_id=OLD.resource_id AND r.resource_lock_generation=OLD.lock_generation AND r.kind='CLEANUP' AND r.safe_request->>'state'='ACKED') AND
    NOT EXISTS(SELECT 1 FROM p11.native_cleanup_recoveries r WHERE r.attempt_id=OLD.owner_attempt_id
      AND r.resource_id=OLD.resource_id AND r.resource_lock_generation=OLD.lock_generation)
  THEN RAISE EXCEPTION 'native owner release requires original cleanup or a separate original failed-cleanup recovery'; END IF;
  RETURN NEW;
END $$;
