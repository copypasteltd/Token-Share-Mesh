-- Forward-only native execution reports. Device assertions release only their
-- original physical owner; they are never accepted delivery/usage/funds proof.
CREATE TABLE p11.native_execution_reports (
  id p11.entity_id PRIMARY KEY,
  lease_id p11.entity_id NOT NULL REFERENCES p11.execution_leases(id),
  attempt_id p11.entity_id NOT NULL,
  job_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL CHECK(fencing_token>0),
  resource_lock_generation p11.counter NOT NULL CHECK(resource_lock_generation>0),
  kind text NOT NULL CHECK(kind IN ('PROGRESS','CLEANUP')),
  sequence p11.counter NOT NULL,
  key_digest p11.sha256_hex NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  safe_request jsonb NOT NULL CHECK(jsonb_typeof(safe_request)='object'),
  safe_response jsonb NOT NULL CHECK(jsonb_typeof(safe_response)='object'),
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  FOREIGN KEY(attempt_id,resource_id,resource_lock_generation) REFERENCES p11.attempts(id,resource_id,resource_lock_generation),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  UNIQUE(lease_id,kind,sequence),
  UNIQUE(device_id,attempt_id,kind,key_digest),
  CHECK((kind='PROGRESS' AND sequence>0) OR (kind='CLEANUP' AND sequence=0))
);
CREATE FUNCTION p11.guard_native_execution_report() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l p11.execution_leases%ROWTYPE; a p11.attempts%ROWTYPE; k text;
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original execution report is append-only'; END IF;
  SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  IF l.admission_profile<>'P11_NATIVE_TEXT_ADMISSION_1' OR l.claim_profile<>'P11_NATIVE_LEASE_CLAIM_1'
    OR l.claim_device_id IS DISTINCT FROM NEW.device_id OR l.attempt_id<>NEW.attempt_id
    OR ROW(a.job_id,a.seller_id,a.device_id,a.resource_id,a.fencing_token,a.resource_lock_generation)
      IS DISTINCT FROM ROW(NEW.job_id,NEW.seller_id,NEW.device_id,NEW.resource_id,NEW.fencing_token,NEW.resource_lock_generation)
    OR NEW.safe_request->>'lease_id' IS DISTINCT FROM NEW.lease_id::text
    OR NEW.safe_request->>'fencing_token' IS DISTINCT FROM NEW.fencing_token::text
    OR NEW.recorded_at>clock_timestamp()+interval '60 seconds'
  THEN RAISE EXCEPTION 'report must retain original claimed device and both fences'; END IF;
  FOR k IN SELECT jsonb_object_keys(NEW.safe_request) LOOP
    IF (NEW.kind='PROGRESS' AND k NOT IN ('lease_id','fencing_token','sequence','phase','fraction','safe_reason_code'))
      OR (NEW.kind='CLEANUP' AND k NOT IN ('lease_id','fencing_token','cleanup_manifest_hash','state','work_stopped','tokens_revoked','workspace_cleared','recorded_at','signature_profile_id','signature'))
    THEN RAISE EXCEPTION 'report cannot contain customer content or extra fields'; END IF;
  END LOOP;
  RETURN NEW;
END $$;
CREATE TRIGGER native_execution_report_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_execution_reports
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_execution_report();

CREATE FUNCTION p11.check_native_execution_report_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
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
      OR NEW.safe_response->>'action' NOT IN ('STOP','NO_CHANGE') OR NEW.safe_response ? 'lease'
    THEN RAISE EXCEPTION 'initial reports do not mint lease renewal'; END IF;
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
CREATE CONSTRAINT TRIGGER native_execution_report_atomic AFTER INSERT ON p11.native_execution_reports
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_execution_report_atomic();

CREATE FUNCTION p11.check_native_owner_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.owner_attempt_id IS NOT NULL AND
    (NEW.owner_attempt_id IS DISTINCT FROM OLD.owner_attempt_id OR NEW.phase='FREE') AND
    EXISTS(SELECT 1 FROM p11.execution_leases l WHERE l.attempt_id=OLD.owner_attempt_id AND l.claim_profile='P11_NATIVE_LEASE_CLAIM_1') AND
    NOT EXISTS(SELECT 1 FROM p11.native_execution_reports r WHERE r.attempt_id=OLD.owner_attempt_id
      AND r.resource_id=OLD.resource_id AND r.resource_lock_generation=OLD.lock_generation AND r.kind='CLEANUP' AND r.safe_request->>'state'='ACKED')
  THEN RAISE EXCEPTION 'native original owner requires committed acknowledged cleanup'; END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER native_owner_release_guard AFTER UPDATE ON p11.resource_locks
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_owner_release();
