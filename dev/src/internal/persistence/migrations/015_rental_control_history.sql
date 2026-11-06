-- R026 SOURCE_ONLY: append-only signed control metadata; no Bearer or content.
-- Published 001-014 and the existing client DDL are immutable. This migration
-- does not qualify evidence, renew paid terms or release a physical owner.
CREATE TABLE p11.rental_control_history (
  grant_id p11.entity_id NOT NULL REFERENCES p11.rental_access_grants(id),
  sequence p11.counter NOT NULL CHECK(sequence>0),
  session_id p11.entity_id NOT NULL REFERENCES p11.rental_authority_contexts(session_id),
  device_id p11.entity_id NOT NULL REFERENCES p11.devices(id),
  grant_digest p11.sha256_hex NOT NULL,
  grant_session_version p11.counter NOT NULL CHECK(grant_session_version>0),
  resource_lock_generation p11.counter NOT NULL CHECK(resource_lock_generation>0),
  terms_revision p11.counter NOT NULL CHECK(terms_revision>0),
  terms_hash p11.sha256_hex NOT NULL,
  request_raw bytea NOT NULL CHECK(octet_length(request_raw) BETWEEN 1 AND 2048),
  request_digest p11.sha256_hex NOT NULL,
  reply_raw bytea NOT NULL CHECK(octet_length(reply_raw) BETWEEN 1 AND 8192),
  reply_digest p11.sha256_hex NOT NULL,
  decision text NOT NULL CHECK(decision IN ('ALLOW','STOP')),
  issued_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  event_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.outbox_events(id) DEFERRABLE INITIALLY DEFERRED,
  writer_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(grant_id,sequence),
  CHECK(request_digest=encode(sha256(request_raw),'hex')),
  CHECK(reply_digest=encode(sha256(reply_raw),'hex')),
  CHECK(expires_at>issued_at AND expires_at<=issued_at+interval '45 seconds'),
  CHECK(jsonb_typeof(convert_from(request_raw,'UTF8')::jsonb)='object'),
  CHECK(jsonb_typeof(convert_from(reply_raw,'UTF8')::jsonb)='object')
);
CREATE INDEX rental_control_session_sequence ON p11.rental_control_history(session_id,grant_id,sequence DESC);

CREATE FUNCTION p11.guard_rental_control_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  parent p11.entity_id;
  s p11.rental_sessions%ROWTYPE;
  c p11.rental_authority_contexts%ROWTYPE;
  g p11.rental_access_grants%ROWTYPE;
  request jsonb;
  reply jsonb;
  claims jsonb;
  high bigint;
  stopped boolean;
BEGIN
  SELECT order_id INTO parent FROM p11.rental_authority_contexts WHERE session_id=NEW.session_id;
  IF parent IS NULL THEN RAISE EXCEPTION 'original rental context required'; END IF;
  PERFORM id FROM p11.orders WHERE id=parent FOR UPDATE;
  SELECT * INTO s FROM p11.rental_sessions WHERE id=NEW.session_id FOR UPDATE;
  SELECT * INTO c FROM p11.rental_authority_contexts WHERE session_id=NEW.session_id;
  SELECT * INTO g FROM p11.rental_access_grants WHERE id=NEW.grant_id FOR SHARE;
  request := convert_from(NEW.request_raw,'UTF8')::jsonb;
  reply := convert_from(NEW.reply_raw,'UTF8')::jsonb;
  claims := g.safe_claims->'safe_claims';
  IF (s.authority_profile='P11_RENTAL_SESSION_AUTHORITY_1'
    AND g.authority_profile='P11_RENTAL_ACCESS_EDDSA_1'
    AND g.session_id=s.id AND g.subject_id=c.buyer_id
    AND NEW.device_id=c.device_id AND NEW.grant_digest=g.digest
    AND NEW.grant_session_version=g.authority_session_version
    AND NEW.resource_lock_generation=s.resource_lock_generation
    AND NEW.terms_revision=s.terms_revision AND NEW.terms_hash=s.authority_snapshot->>'terms_hash'
    AND g.terms_revision=NEW.terms_revision AND g.terms_hash=NEW.terms_hash
    AND request->>'grant_id'=g.id AND request->>'grant_digest'=g.digest
    AND request->>'expected_version'=NEW.grant_session_version::text
    AND request->>'sequence'=NEW.sequence::text
    AND request->>'resource_lock_generation'=NEW.resource_lock_generation::text
    AND request->>'terms_revision'=NEW.terms_revision::text AND request->>'terms_hash'=NEW.terms_hash
    AND request-ARRAY['expected_version','grant_id','grant_digest','sequence','resource_lock_generation','terms_revision','terms_hash']='{}'::jsonb
    AND reply->>'profile_id'='P11_RENTAL_CONTROL_ED25519_1'
    AND reply->>'grant_id'=g.id AND reply->>'grant_digest'=g.digest
    AND reply->>'order_id'=c.order_id AND reply->>'session_id'=s.id
    AND reply->>'seller_id'=c.seller_id AND reply->>'device_id'=c.device_id
    AND reply->>'resource_id'=c.resource_id
    AND reply->>'resource_lock_generation'=NEW.resource_lock_generation::text
    AND reply->>'terms_revision'=NEW.terms_revision::text AND reply->>'terms_hash'=NEW.terms_hash
    AND reply->>'kernel_environment_digest'=c.kernel_environment_digest
    AND reply->>'manifest_hash'=c.manifest_hash AND reply->>'hard_deadline'=claims->>'hard_deadline'
    AND reply->>'sequence'=NEW.sequence::text AND reply->>'decision'=NEW.decision
    AND reply->>'issuer_key_id'=g.safe_claims->>'issuer_key_id'
    AND length(reply->>'signature')=86
    AND (reply->>'issued_at')::timestamptz=NEW.issued_at
    AND (reply->>'expires_at')::timestamptz=NEW.expires_at
    AND NEW.issued_at>=to_timestamp((claims->>'iat')::bigint)
    AND NEW.issued_at<g.expires_at AND NEW.issued_at<=clock_timestamp()
    AND NEW.expires_at<=g.expires_at AND NEW.expires_at<=s.ends_at
    AND NEW.writer_xact=pg_current_xact_id()
    AND reply-ARRAY['profile_id','grant_id','grant_digest','order_id','session_id','seller_id','device_id','resource_id',
      'resource_lock_generation','terms_revision','terms_hash','kernel_environment_digest','manifest_hash','hard_deadline',
      'sequence','decision','issued_at','expires_at','issuer_key_id','signature']='{}'::jsonb) IS NOT TRUE THEN
    RAISE EXCEPTION 'original bounded rental control metadata required';
  END IF;
  SELECT COALESCE(max(sequence),0),COALESCE(bool_or(decision='STOP'),false) INTO high,stopped
    FROM p11.rental_control_history WHERE grant_id=NEW.grant_id;
  IF NEW.sequence<=high THEN RAISE EXCEPTION 'rental control sequence cannot move backwards'; END IF;
  IF NEW.decision='ALLOW' AND ((NOT stopped AND g.revoked_at IS NULL AND s.state='ACTIVE'
    AND s.version=NEW.grant_session_version AND NEW.expires_at>clock_timestamp()
    AND EXISTS(SELECT 1 FROM p11.resource_locks l WHERE l.resource_id=c.resource_id AND l.phase='ACTIVE'
      AND l.owner_session_id=s.id AND l.owner_attempt_id IS NULL AND l.lock_generation=c.resource_lock_generation)
    AND EXISTS(SELECT 1 FROM p11.budget_reservations b WHERE b.order_id=c.order_id AND b.purpose_ref=s.id
      AND b.purpose_kind='RENTAL_SESSION' AND b.accounting_profile='ORDER_CAPACITY_1' AND b.state='RESERVED' AND b.amount_fen=s.budget_fen)
    AND EXISTS(SELECT 1 FROM p11.users u JOIN p11.role_grants r ON r.user_id=u.id WHERE u.id=c.buyer_id
      AND u.state='ACTIVE' AND r.role_code='BUYER' AND r.scope_ref='self' AND r.revoked_at IS NULL
      AND (r.expires_at IS NULL OR r.expires_at>clock_timestamp()))) IS NOT TRUE) THEN
    RAISE EXCEPTION 'current original rental control permission required';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER rental_control_insert_guard BEFORE INSERT ON p11.rental_control_history
  FOR EACH ROW EXECUTE FUNCTION p11.guard_rental_control_insert();

CREATE FUNCTION p11.guard_rental_control_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'rental control history is append-only'; END $$;
CREATE TRIGGER rental_control_immutable BEFORE UPDATE OR DELETE ON p11.rental_control_history
  FOR EACH ROW EXECUTE FUNCTION p11.guard_rental_control_immutable();

CREATE FUNCTION p11.guard_rental_control_commit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.writer_xact<>pg_current_xact_id() OR NOT EXISTS(
    SELECT 1 FROM p11.outbox_events e WHERE e.id=NEW.event_id AND e.aggregate_type='RENTAL_CONTROL'
      AND e.aggregate_id=NEW.grant_id AND e.aggregate_version=NEW.sequence AND e.event_type='RentalControlConfirmed'
      AND e.safe_payload->>'profile_id'='P11_RENTAL_CONTROL_HISTORY_1'
      AND e.safe_payload->>'session_id'=NEW.session_id AND e.safe_payload->>'grant_id'=NEW.grant_id
      AND e.safe_payload->>'sequence'=NEW.sequence::text AND e.safe_payload->>'decision'=NEW.decision
      AND e.safe_payload->>'reply_digest'=NEW.reply_digest
      AND e.safe_payload->>'resource_lock_generation'=NEW.resource_lock_generation::text
      AND e.safe_payload->>'terms_revision'=NEW.terms_revision::text) THEN
    RAISE EXCEPTION 'atomic original rental control event required';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER rental_control_commit_guard AFTER INSERT ON p11.rental_control_history
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.guard_rental_control_commit();
