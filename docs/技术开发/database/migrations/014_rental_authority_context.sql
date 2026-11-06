-- R026 source-only forward migration. Never rewrite published 001-013.
-- SQL proves atomic metadata, not hardware readiness, payment signatures,
-- native stopped-work, encrypted journals or cryptographic issuer trust.
ALTER TABLE p11.rental_sessions
  ADD COLUMN authority_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED'
    CHECK(authority_profile IN ('LEGACY_UNVERIFIED','P11_RENTAL_SESSION_AUTHORITY_1')),
  ADD COLUMN authority_snapshot jsonb,
  ADD COLUMN hourly_price_snapshot jsonb,
  ALTER COLUMN rate_fen_per_minute DROP NOT NULL,
  ADD CONSTRAINT rental_hourly_authority_shape CHECK ((
    (authority_profile='LEGACY_UNVERIFIED' AND authority_snapshot IS NULL
      AND hourly_price_snapshot IS NULL AND rate_fen_per_minute IS NOT NULL)
    OR (authority_profile='P11_RENTAL_SESSION_AUTHORITY_1'
      AND rate_fen_per_minute IS NULL AND authority_snapshot IS NOT NULL
      AND jsonb_typeof(authority_snapshot)='object' AND hourly_price_snapshot IS NOT NULL
      AND jsonb_typeof(hourly_price_snapshot)='object'
      AND hourly_price_snapshot->>'price_contract_version'='NB_PRICE_1'
      AND hourly_price_snapshot->>'unit'='AVAILABLE_HOUR'
      AND hourly_price_snapshot->>'currency'='CNY'
      AND hourly_price_snapshot->>'rounding_profile'='ORDER_CEIL_FEN_1'
      AND jsonb_typeof(hourly_price_snapshot->'hour_rate')='object'
      AND hourly_price_snapshot->'input_rate' IS NULL
      AND hourly_price_snapshot->'output_rate' IS NULL)) IS TRUE);

CREATE TABLE p11.rental_authority_contexts (
  session_id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  resource_lock_generation p11.counter NOT NULL CHECK(resource_lock_generation>0),
  capability_id p11.entity_id NOT NULL,
  environment_revision p11.entity_id NOT NULL,
  kernel_environment_digest p11.sha256_hex NOT NULL,
  manifest_ref p11.entity_id NOT NULL UNIQUE,
  manifest_raw bytea NOT NULL CHECK(octet_length(manifest_raw) BETWEEN 1 AND 16384),
  manifest_hash p11.sha256_hex NOT NULL,
  connection_ref p11.entity_id NOT NULL UNIQUE,
  initial_contract_raw bytea NOT NULL CHECK(octet_length(initial_contract_raw) BETWEEN 1 AND 32768),
  initial_contract_digest p11.sha256_hex NOT NULL,
  order_contract_hash p11.sha256_hex NOT NULL,
  funding_payment_id p11.entity_id NOT NULL,
  payment_environment text NOT NULL CHECK(payment_environment IN ('SANDBOX','PRODUCTION')),
  created_at timestamptz NOT NULL,
  FOREIGN KEY(session_id,order_id) REFERENCES p11.rental_sessions(id,order_id),
  FOREIGN KEY(session_id,resource_id) REFERENCES p11.rental_sessions(id,resource_id),
  FOREIGN KEY(order_id,buyer_id,seller_id) REFERENCES p11.orders(id,buyer_id,seller_id),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  FOREIGN KEY(capability_id,device_id,seller_id) REFERENCES p11.capabilities(id,device_id,seller_id),
  FOREIGN KEY(funding_payment_id,order_id) REFERENCES p11.payments(id,order_id),
  CHECK(manifest_hash=encode(sha256(manifest_raw),'hex')),
  CHECK(initial_contract_digest=encode(sha256(initial_contract_raw),'hex')),
  CHECK(jsonb_typeof(convert_from(initial_contract_raw,'UTF8')::jsonb)='object'),
  CHECK(jsonb_typeof(convert_from(manifest_raw,'UTF8')::jsonb)='object')
);
CREATE TRIGGER rental_authority_context_immutable BEFORE UPDATE OR DELETE ON p11.rental_authority_contexts
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE TABLE p11.rental_session_history (
  session_id p11.entity_id NOT NULL REFERENCES p11.rental_sessions(id),
  version p11.counter NOT NULL CHECK(version>0),
  operation_id text NOT NULL CHECK(operation_id IN
    ('createRentalSession','beginRentalPreparation','recordRentalDeviceReady',
     'confirmRentalReady','stopRentalSession','createRentalAccessGrant',
     'expireRentalSession','recordRentalCleanup','createRentalRenewal')),
  actor_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  state_raw bytea NOT NULL CHECK(octet_length(state_raw) BETWEEN 1 AND 65536),
  state_digest p11.sha256_hex NOT NULL,
  event_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.outbox_events(id) DEFERRABLE INITIALLY DEFERRED,
  writer_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY(session_id,version),
  CHECK(state_digest=encode(sha256(state_raw),'hex')),
  CHECK(jsonb_typeof(convert_from(state_raw,'UTF8')::jsonb)='object')
);
CREATE TRIGGER rental_session_history_immutable BEFORE UPDATE OR DELETE ON p11.rental_session_history
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

ALTER TABLE p11.rental_access_grants
  ADD COLUMN authority_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED'
    CHECK(authority_profile IN ('LEGACY_UNVERIFIED','P11_RENTAL_ACCESS_EDDSA_1')),
  ADD COLUMN authority_session_version p11.counter,
  ADD CONSTRAINT rental_access_authority_shape CHECK (
    (authority_profile='LEGACY_UNVERIFIED' AND authority_session_version IS NULL)
    OR (authority_profile='P11_RENTAL_ACCESS_EDDSA_1' AND authority_session_version IS NOT NULL
      AND authority_session_version>1));
CREATE UNIQUE INDEX rental_one_current_subject_grant ON p11.rental_access_grants(session_id,subject_id)
  WHERE revoked_at IS NULL AND authority_profile='P11_RENTAL_ACCESS_EDDSA_1';

CREATE FUNCTION p11.guard_rental_access_authority() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s p11.rental_sessions%ROWTYPE; c p11.rental_authority_contexts%ROWTYPE; j jsonb;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.authority_profile='P11_RENTAL_ACCESS_EDDSA_1' THEN RAISE EXCEPTION 'rental grant history is retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='UPDATE' AND OLD.authority_profile='P11_RENTAL_ACCESS_EDDSA_1' THEN
    IF ROW(NEW.id,NEW.session_id,NEW.subject_id,NEW.digest,NEW.resource_lock_generation,
      NEW.terms_revision,NEW.terms_hash,NEW.safe_claims,NEW.scopes,NEW.expires_at,
      NEW.authority_profile,NEW.authority_session_version) IS DISTINCT FROM
      ROW(OLD.id,OLD.session_id,OLD.subject_id,OLD.digest,OLD.resource_lock_generation,
      OLD.terms_revision,OLD.terms_hash,OLD.safe_claims,OLD.scopes,OLD.expires_at,
      OLD.authority_profile,OLD.authority_session_version)
      OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL THEN
      RAISE EXCEPTION 'original rental grant only admits one permanent revocation';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.authority_profile='LEGACY_UNVERIFIED' THEN
    IF EXISTS(SELECT 1 FROM p11.rental_sessions WHERE id=NEW.session_id
      AND authority_profile='P11_RENTAL_SESSION_AUTHORITY_1') THEN
      RAISE EXCEPTION 'current rental cannot accept a legacy grant';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'legacy grant cannot be promoted'; END IF;
  SELECT * INTO STRICT s FROM p11.rental_sessions WHERE id=NEW.session_id;
  SELECT * INTO STRICT c FROM p11.rental_authority_contexts WHERE session_id=s.id;
  j:=NEW.safe_claims->'safe_claims';
  IF (s.authority_profile='P11_RENTAL_SESSION_AUTHORITY_1' AND s.state='ACTIVE'
    AND NEW.revoked_at IS NULL AND NEW.subject_id=c.buyer_id
    AND NEW.resource_lock_generation=s.resource_lock_generation AND NEW.terms_revision=s.terms_revision
    AND NEW.authority_session_version=s.version+1 AND NEW.terms_hash=s.authority_snapshot->>'terms_hash'
    AND NEW.expires_at>clock_timestamp() AND NEW.expires_at<=s.ends_at
    AND NEW.safe_claims->>'profile_id'=NEW.authority_profile
    AND NEW.safe_claims->>'digest'=NEW.digest
    AND NEW.safe_claims->>'issuer_key_id'~'^[A-Za-z][A-Za-z0-9_-]{2,127}$'
    AND j->>'profile_id'=NEW.authority_profile AND j->>'jti'=NEW.id
    AND j->>'iss'~'^[A-Za-z][A-Za-z0-9_-]{2,127}$'
    AND j->>'sub'=c.buyer_id AND j->>'aud'=c.device_id
    AND j->>'order_id'=c.order_id AND j->>'session_id'=s.id AND j->>'seller_id'=c.seller_id
    AND j->>'device_id'=c.device_id AND j->>'resource_id'=c.resource_id
    AND j->>'resource_lock_generation'=s.resource_lock_generation::text
    AND j->>'terms_revision'=s.terms_revision::text AND j->>'terms_hash'=NEW.terms_hash
    AND j->>'environment_revision'=c.environment_revision
    AND j->>'kernel_environment_digest'=c.kernel_environment_digest
    AND j->>'manifest_ref'=c.manifest_ref AND j->>'manifest_hash'=c.manifest_hash
    AND (j->>'hard_deadline')::timestamptz=s.ends_at
    AND to_timestamp((j->>'exp')::bigint)=NEW.expires_at
    AND (j->>'nbf')::bigint<=(j->>'iat')::bigint AND (j->>'iat')::bigint<(j->>'exp')::bigint
    AND j->>'mode'='GPU_RENTAL' AND j->>'privacy'='STANDARD'
    AND j->'scope'=to_jsonb(NEW.scopes) AND cardinality(NEW.scopes) BETWEEN 1 AND 4
    AND NEW.scopes<@ARRAY['INVOKE','QUEUE_SELF','CANCEL_SELF','EXPORT_SELF']::text[]
    AND NOT (NEW.safe_claims ?| ARRAY['credential_once','signature','private_key'])) IS NOT TRUE THEN
    RAISE EXCEPTION 'rental grant must bind the actual original live authority';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER rental_access_authority_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.rental_access_grants
  FOR EACH ROW EXECUTE FUNCTION p11.guard_rental_access_authority();

CREATE FUNCTION p11.check_rental_access_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.authority_profile='P11_RENTAL_ACCESS_EDDSA_1' AND NOT EXISTS(
    SELECT 1 FROM p11.rental_session_history h JOIN p11.outbox_events e ON e.id=h.event_id
    WHERE h.session_id=NEW.session_id AND h.version=NEW.authority_session_version
      AND h.operation_id='createRentalAccessGrant' AND h.writer_xact=pg_current_xact_id()
      AND e.event_type='RentalAccessIssued' AND e.aggregate_id=NEW.session_id
      AND e.aggregate_version=NEW.authority_session_version) THEN
    RAISE EXCEPTION 'rental grant and original session event must commit atomically';
  END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER rental_access_authority_atomic AFTER INSERT ON p11.rental_access_grants
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_rental_access_atomic();

CREATE FUNCTION p11.guard_rental_session_authority() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j jsonb;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.authority_profile='P11_RENTAL_SESSION_AUTHORITY_1' THEN
      RAISE EXCEPTION 'original rental authority requires append-only recovery';
    END IF;
    RETURN OLD;
  END IF;
  IF NEW.authority_profile='LEGACY_UNVERIFIED' THEN
    IF TG_OP='UPDATE' AND OLD.authority_profile<>'LEGACY_UNVERIFIED' THEN
      RAISE EXCEPTION 'rental authority cannot become legacy';
    END IF;
    RETURN NEW;
  END IF;
  j:=NEW.authority_snapshot;
  IF TG_OP='INSERT' THEN
    IF NEW.version<>1 OR NEW.state<>'RESERVED' OR NEW.cleanup_state<>'NOT_STARTED'
      OR NEW.terms_revision<>1 OR NEW.started_at IS NOT NULL OR NEW.ends_at IS NOT NULL
      OR NEW.stopped_at IS NOT NULL OR NEW.reserved_until<=clock_timestamp()
      OR NEW.prepare_until IS NOT NULL OR NEW.buyer_ready_until IS NOT NULL THEN
      RAISE EXCEPTION 'new rental must be an unstarted original reservation';
    END IF;
  ELSE
    IF OLD.authority_profile<>NEW.authority_profile OR NEW.version<>OLD.version+1
      OR ROW(NEW.id,NEW.order_id,NEW.seller_id,NEW.device_id,NEW.resource_id,
        NEW.resource_lock_generation,NEW.hourly_price_snapshot,NEW.created_at)
      IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.seller_id,OLD.device_id,OLD.resource_id,
        OLD.resource_lock_generation,OLD.hourly_price_snapshot,OLD.created_at)
      OR j->'contract' IS DISTINCT FROM OLD.authority_snapshot->'contract'
      OR NEW.reserved_until IS DISTINCT FROM OLD.reserved_until
      OR (OLD.started_at IS NOT NULL AND NEW.started_at IS DISTINCT FROM OLD.started_at)
      OR (OLD.stopped_at IS NOT NULL AND NEW.stopped_at IS DISTINCT FROM OLD.stopped_at)
      OR OLD.state='CLOSED' THEN
      RAISE EXCEPTION 'original rental tuple, clock or price cannot be rewritten';
    END IF;
    IF NOT (
      (OLD.state='RESERVED' AND NEW.state IN ('PREPARING','STOPPING','QUARANTINED')) OR
      (OLD.state='PREPARING' AND NEW.state IN ('READY_WAIT_BUYER','STOPPING','QUARANTINED')) OR
      (OLD.state='READY_WAIT_BUYER' AND NEW.state IN ('ACTIVE','STOPPING','QUARANTINED')) OR
      (OLD.state='ACTIVE' AND NEW.state IN ('ACTIVE','STOPPING','FAULTED','QUARANTINED')) OR
      (OLD.state='FAULTED' AND NEW.state IN ('STOPPING','QUARANTINED')) OR
      (OLD.state='STOPPING' AND NEW.state IN ('CLEANING','QUARANTINED','CLOSED')) OR
      (OLD.state='CLEANING' AND NEW.state IN ('QUARANTINED','CLOSED')) OR
      (OLD.state='QUARANTINED' AND NEW.state IN ('CLEANING','CLOSED'))
    ) THEN RAISE EXCEPTION 'rental transition cannot revive stopped work'; END IF;
    -- Current authority version admits the initial paid term only. Paid-term
    -- application will require its own forward atomic-funds/history guard.
    IF NEW.terms_revision<>OLD.terms_revision OR NEW.budget_fen<>OLD.budget_fen
      OR j->>'terms_hash' IS DISTINCT FROM OLD.authority_snapshot->>'terms_hash'
      OR j->>'terms_ref' IS DISTINCT FROM OLD.authority_snapshot->>'terms_ref'
      OR (OLD.ends_at IS NOT NULL AND NEW.ends_at IS DISTINCT FROM OLD.ends_at) THEN
      RAISE EXCEPTION 'paid renewal requires independently applied original terms';
    END IF;
  END IF;
  IF (j->>'session_id'=NEW.id AND j->>'resource_lock_generation'=NEW.resource_lock_generation::text
    AND (j->>'version')::bigint=NEW.version AND j->>'state'=NEW.state
    AND j->>'cleanup_state'=NEW.cleanup_state AND (j->>'terms_revision')::bigint=NEW.terms_revision
    AND (j->>'confirmed_budget_fen')::bigint=NEW.budget_fen
    AND j#>>'{contract,order_id}'=NEW.order_id AND j#>>'{contract,seller_id}'=NEW.seller_id
    AND j#>>'{contract,device_id}'=NEW.device_id AND j#>>'{contract,resource_id}'=NEW.resource_id
    AND j#>'{contract,price}'=NEW.hourly_price_snapshot
    AND (j->>'created_at')::timestamptz=NEW.created_at
    AND nullif(j->>'reserved_until','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.reserved_until
    AND nullif(j->>'prepare_until','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.prepare_until
    AND nullif(j->>'buyer_ready_until','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.buyer_ready_until
    AND nullif(j->>'started_at','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.started_at
    AND nullif(j->>'hard_deadline_at','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.ends_at
    AND nullif(j->>'stopped_at','0001-01-01T00:00:00Z')::timestamptz IS NOT DISTINCT FROM NEW.stopped_at
  ) IS NOT TRUE THEN RAISE EXCEPTION 'rental snapshot and physical row differ'; END IF;
  IF NEW.state='ACTIVE' AND (NEW.started_at IS NULL OR NEW.ends_at IS NULL
      OR NEW.ends_at<=NEW.started_at OR j->'device_ready' IS NULL OR j->'buyer_ready' IS NULL
      OR j->'device_ready'='null'::jsonb OR j->'buyer_ready'='null'::jsonb) THEN
    RAISE EXCEPTION 'active rental requires both original readiness facts';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER rental_session_authority_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.rental_sessions
  FOR EACH ROW EXECUTE FUNCTION p11.guard_rental_session_authority();

CREATE FUNCTION p11.check_rental_session_authority_atomic() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s p11.rental_sessions%ROWTYPE; c p11.rental_authority_contexts%ROWTYPE;
        r p11.resource_locks%ROWTYPE; h p11.rental_session_history%ROWTYPE;
        original_contract jsonb; m jsonb;
BEGIN
  SELECT * INTO STRICT s FROM p11.rental_sessions WHERE id=NEW.id;
  IF s.authority_profile<>'P11_RENTAL_SESSION_AUTHORITY_1' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT c FROM p11.rental_authority_contexts WHERE session_id=s.id;
  SELECT * INTO STRICT r FROM p11.resource_locks WHERE resource_id=s.resource_id;
  SELECT * INTO STRICT h FROM p11.rental_session_history WHERE session_id=s.id AND version=s.version;
  original_contract:=convert_from(c.initial_contract_raw,'UTF8')::jsonb;
  m:=convert_from(c.manifest_raw,'UTF8')::jsonb;
  IF ROW(c.order_id,c.seller_id,c.device_id,c.resource_id,c.resource_lock_generation,c.created_at)
      IS DISTINCT FROM ROW(s.order_id,s.seller_id,s.device_id,s.resource_id,s.resource_lock_generation,s.created_at)
    OR original_contract IS DISTINCT FROM s.authority_snapshot->'contract'
    OR (original_contract->>'order_id'=c.order_id AND original_contract->>'buyer_id'=c.buyer_id
      AND original_contract->>'seller_id'=c.seller_id AND original_contract->>'device_id'=c.device_id
      AND original_contract->>'resource_id'=c.resource_id
      AND original_contract->>'contract_hash'=c.order_contract_hash
      AND original_contract->>'environment_revision'=c.environment_revision
      AND original_contract->>'kernel_environment_digest'=c.kernel_environment_digest
      AND original_contract->>'manifest_ref'=c.manifest_ref
      AND original_contract->>'manifest_hash'=c.manifest_hash
      AND original_contract->>'payment_environment'=c.payment_environment) IS NOT TRUE
    OR h.writer_xact<>pg_current_xact_id()
    OR convert_from(h.state_raw,'UTF8')::jsonb IS DISTINCT FROM s.authority_snapshot
    OR NOT EXISTS(SELECT 1 FROM p11.orders o WHERE o.id=c.order_id AND o.buyer_id=c.buyer_id
      AND o.seller_id=c.seller_id AND o.mode='GPU_RENTAL' AND o.privacy='STANDARD'
      AND o.contract_hash=c.order_contract_hash)
    OR NOT EXISTS(SELECT 1 FROM p11.capabilities p WHERE p.id=c.capability_id
      AND p.resource_id=c.resource_id AND p.device_id=c.device_id AND p.seller_id=c.seller_id
      AND p.kernel_environment_ref=c.environment_revision
      AND p.kernel_environment_digest=c.kernel_environment_digest)
    OR (m->>'profile_id'='P11_RENTAL_EXECUTION_MANIFEST_1'
      AND m->>'manifest_ref'=c.manifest_ref AND m->>'session_id'=s.id
      AND m->>'order_id'=s.order_id AND m->>'buyer_id'=c.buyer_id
      AND m->>'seller_id'=s.seller_id AND m->>'device_id'=s.device_id
      AND m->>'resource_id'=s.resource_id
      AND m->>'resource_lock_generation'=s.resource_lock_generation::text
      AND m->>'capability_id'=c.capability_id AND m->>'environment_revision'=c.environment_revision
      AND m->>'kernel_environment_digest'=c.kernel_environment_digest
      AND m->>'contract_hash'=c.order_contract_hash
      AND m->>'service_mode'='GPU_RENTAL' AND m->>'privacy_profile'='STANDARD') IS NOT TRUE
    OR NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.id=h.event_id
      AND e.aggregate_type='RENTAL_SESSION' AND e.aggregate_id=s.id
      AND e.aggregate_version=s.version AND e.safe_payload->>'session_id'=s.id
      AND e.safe_payload->>'state_digest'=h.state_digest) THEN
    RAISE EXCEPTION 'original rental context, history and event must commit together';
  END IF;
  IF s.state='CLOSED' THEN
    -- Native cleanup evidence/release has a separate authority. This migration
    -- never makes a closed snapshot itself sufficient to free physical work.
    IF r.owner_session_id=s.id THEN RAISE EXCEPTION 'closed rental retains original physical owner'; END IF;
  ELSIF r.owner_session_id IS DISTINCT FROM s.id OR r.owner_attempt_id IS NOT NULL
    OR r.lock_generation<>s.resource_lock_generation OR r.phase IS DISTINCT FROM CASE
      WHEN s.state IN ('RESERVED','PREPARING','READY_WAIT_BUYER') THEN 'RESERVED'
      WHEN s.state='ACTIVE' THEN 'ACTIVE' WHEN s.state IN ('STOPPING','FAULTED') THEN 'STOPPING'
      WHEN s.state='CLEANING' THEN 'CLEANING' ELSE 'QUARANTINED' END THEN
    RAISE EXCEPTION 'rental session cannot lose its original physical owner';
  END IF;
  IF s.state IN ('RESERVED','PREPARING','READY_WAIT_BUYER','ACTIVE') AND NOT EXISTS(
    SELECT 1 FROM p11.budget_reservations b WHERE b.order_id=s.order_id AND b.purpose_ref=s.id
      AND b.purpose_kind='RENTAL_SESSION' AND b.accounting_profile='ORDER_CAPACITY_1'
      AND b.state='RESERVED' AND b.amount_fen=s.budget_fen) THEN
    RAISE EXCEPTION 'live rental requires the original full budget hold';
  END IF;
  IF s.state IN ('STOPPING','FAULTED','CLEANING','CLOSED','QUARANTINED') AND EXISTS(
    SELECT 1 FROM p11.rental_access_grants g WHERE g.session_id=s.id AND g.revoked_at IS NULL) THEN
    RAISE EXCEPTION 'stopped rental cannot retain live application grants';
  END IF;
  RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER rental_session_authority_atomic AFTER INSERT OR UPDATE ON p11.rental_sessions
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_rental_session_authority_atomic();

CREATE FUNCTION p11.retain_rental_physical_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.owner_session_id IS NOT NULL AND EXISTS(SELECT 1 FROM p11.rental_sessions s
      WHERE s.id=OLD.owner_session_id AND s.authority_profile='P11_RENTAL_SESSION_AUTHORITY_1') THEN
      RAISE EXCEPTION 'original rental physical owner cannot be deleted';
    END IF;
    RETURN OLD;
  END IF;
  IF OLD.owner_session_id IS NOT NULL
    AND EXISTS(SELECT 1 FROM p11.rental_sessions s WHERE s.id=OLD.owner_session_id
      AND s.authority_profile='P11_RENTAL_SESSION_AUTHORITY_1')
    AND ROW(NEW.owner_session_id,NEW.owner_attempt_id,NEW.lock_generation)
      IS DISTINCT FROM ROW(OLD.owner_session_id,OLD.owner_attempt_id,OLD.lock_generation) THEN
    -- A later forward migration will permit release only with the matching
    -- independently verified native cleanup record, never a TTL/state boolean.
    RAISE EXCEPTION 'original rental physical owner requires actual cleanup authority';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER rental_retain_physical_owner BEFORE UPDATE OR DELETE ON p11.resource_locks
  FOR EACH ROW EXECUTE FUNCTION p11.retain_rental_physical_owner();
