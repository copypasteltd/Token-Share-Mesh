-- R018 forward-only. A delivery claim is metadata, never execution ACK or usage.
ALTER TABLE p11.execution_leases
  ADD COLUMN claim_profile p11.entity_id NOT NULL DEFAULT 'UNBOUND_CLAIM',
  ADD COLUMN claim_device_id p11.entity_id REFERENCES p11.devices(id),
  ADD COLUMN claim_key_digest p11.sha256_hex,
  ADD COLUMN claim_request_digest p11.sha256_hex,
  ADD COLUMN claim_device_version integer,
  ADD COLUMN claimed_at timestamptz,
  ADD CONSTRAINT native_claim_shape CHECK (
    (claim_profile='UNBOUND_CLAIM' AND claim_device_id IS NULL AND claim_key_digest IS NULL
      AND claim_request_digest IS NULL AND claim_device_version IS NULL AND claimed_at IS NULL)
    OR (claim_profile='P11_NATIVE_LEASE_CLAIM_1' AND admission_profile='P11_NATIVE_TEXT_ADMISSION_1'
      AND claim_device_id IS NOT NULL AND claim_key_digest IS NOT NULL AND claim_request_digest IS NOT NULL
      AND claim_device_version IS NOT NULL AND claim_device_version>0 AND claim_device_version<2147483647 AND claimed_at IS NOT NULL));
CREATE UNIQUE INDEX native_claim_original_key ON p11.execution_leases(claim_device_id,claim_key_digest)
  WHERE claim_profile='P11_NATIVE_LEASE_CLAIM_1';

CREATE FUNCTION p11.guard_native_lease_claim() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a p11.attempts%ROWTYPE; j p11.jobs%ROWTYPE; d p11.devices%ROWTYPE; lk p11.resource_locks%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.claim_profile<>'UNBOUND_CLAIM' THEN RAISE EXCEPTION 'original claim must be retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='INSERT' THEN
    IF NEW.claim_profile<>'UNBOUND_CLAIM' THEN RAISE EXCEPTION 'claim follows committed initial issuance'; END IF;
    RETURN NEW;
  END IF;
  IF OLD.claim_profile<>'UNBOUND_CLAIM' THEN
    IF ROW(NEW.claim_profile,NEW.claim_device_id,NEW.claim_key_digest,NEW.claim_request_digest,NEW.claim_device_version,NEW.claimed_at)
      IS DISTINCT FROM ROW(OLD.claim_profile,OLD.claim_device_id,OLD.claim_key_digest,OLD.claim_request_digest,OLD.claim_device_version,OLD.claimed_at)
    THEN RAISE EXCEPTION 'original claim cannot be replaced or cleared'; END IF;
    RETURN NEW;
  END IF;
  IF NEW.claim_profile='UNBOUND_CLAIM' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  SELECT * INTO STRICT j FROM p11.jobs WHERE id=a.job_id;
  SELECT * INTO STRICT d FROM p11.devices WHERE id=a.device_id;
  SELECT * INTO STRICT lk FROM p11.resource_locks WHERE resource_id=a.resource_id;
  IF NEW.admission_profile<>'P11_NATIVE_TEXT_ADMISSION_1' OR NEW.state<>'ISSUED' OR a.state<>'ISSUED'
    OR j.state<>'RUNNING' OR j.current_attempt_id IS DISTINCT FROM a.id OR j.fencing_token<>a.fencing_token
    OR NEW.claim_device_id<>a.device_id OR NEW.claim_device_version<>d.version
    OR d.identity_profile<>'ED25519_DEVICE_1' OR d.seller_id<>a.seller_id OR d.state<>'ONLINE'
    OR lk.phase<>'ACTIVE' OR lk.owner_attempt_id IS DISTINCT FROM a.id OR lk.owner_session_id IS NOT NULL
    OR lk.lock_generation<>a.resource_lock_generation OR lk.expires_at<>NEW.expires_at
    OR NEW.expires_at<=clock_timestamp() OR NEW.hard_deadline<=clock_timestamp()
    OR NEW.claimed_at<NEW.issued_at OR NEW.claimed_at>=NEW.expires_at OR NEW.claimed_at>clock_timestamp()
  THEN RAISE EXCEPTION 'claim requires original current native owner'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER native_claim_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.execution_leases
  FOR EACH ROW EXECUTE FUNCTION p11.guard_native_lease_claim();

CREATE FUNCTION p11.check_native_lease_claim_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a p11.attempts%ROWTYPE;
BEGIN
  IF NEW.claim_profile='UNBOUND_CLAIM' OR OLD.claim_profile<>'UNBOUND_CLAIM' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
  IF NOT EXISTS (SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='EXECUTION_LEASE'
    AND e.aggregate_id=NEW.id AND e.aggregate_version=1 AND e.event_type='ExecutionLeaseClaimed'
    AND e.safe_payload=jsonb_build_object('lease_id',NEW.id,'attempt_id',NEW.attempt_id,'job_id',NEW.job_id,
      'device_id',NEW.claim_device_id,'resource_id',NEW.resource_id,'resource_lock_generation',NEW.resource_lock_generation::text))
  THEN RAISE EXCEPTION 'original claim and safe event must commit together'; END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER native_claim_atomic AFTER UPDATE ON p11.execution_leases
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_lease_claim_atomic();
