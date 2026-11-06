-- SOURCE ONLY / UNAPPLIED. Public metadata, not content or execution authority.
-- Signature/JCS/independent trust verification is performed by the application.
SET search_path = p11, public;

CREATE TABLE p11.endpoint_public_bundles (
 recipient_id p11.entity_id PRIMARY KEY REFERENCES p11.recipient_keys(id),
 bundle_hash text NOT NULL UNIQUE CHECK(bundle_hash ~ '^[0-9a-f]{64}$'),
 owner_id p11.entity_id NOT NULL REFERENCES p11.users(id),
 endpoint_id p11.entity_id NOT NULL,
 role text NOT NULL CHECK(role IN ('BUYER','SELLER')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 32),
 device_id p11.entity_id REFERENCES p11.devices(id),
 device_fingerprint text CHECK(device_fingerprint ~ '^[0-9a-f]{64}$'),
 bundle_raw bytea NOT NULL CHECK(octet_length(bundle_raw) BETWEEN 256 AND 8192),
 state text NOT NULL CHECK(state IN ('ACTIVE','RETIRED','REVOKED')),
 previous_recipient_id p11.entity_id UNIQUE,
 previous_bundle_hash text,
 transition_hash text CHECK(transition_hash ~ '^[0-9a-f]{64}$'),
 transition_raw bytea CHECK(octet_length(transition_raw) BETWEEN 256 AND 12000),
 expires_at timestamptz NOT NULL,
 registered_at timestamptz NOT NULL,
 revoked_at timestamptz,
 UNIQUE(owner_id,endpoint_id,role,version),
 UNIQUE(recipient_id,bundle_hash),
 FOREIGN KEY(previous_recipient_id,previous_bundle_hash) REFERENCES p11.endpoint_public_bundles(recipient_id,bundle_hash),
 CHECK((role='SELLER' AND device_id IS NOT NULL AND device_id=endpoint_id AND device_fingerprint IS NOT NULL) OR (role='BUYER' AND device_id IS NULL AND device_fingerprint IS NULL)),
 CHECK((version=1 AND previous_recipient_id IS NULL AND previous_bundle_hash IS NULL AND transition_hash IS NULL AND transition_raw IS NULL) OR (version>1 AND previous_recipient_id IS NOT NULL AND previous_bundle_hash IS NOT NULL AND transition_hash IS NOT NULL AND transition_raw IS NOT NULL)),
 CHECK((state='REVOKED')=(revoked_at IS NOT NULL)),
 CHECK(expires_at>registered_at)
);
CREATE UNIQUE INDEX endpoint_public_one_active ON p11.endpoint_public_bundles(owner_id,endpoint_id,role) WHERE state='ACTIVE';
CREATE INDEX endpoint_public_owner_history ON p11.endpoint_public_bundles(owner_id,role,endpoint_id,version DESC);

CREATE TABLE p11.endpoint_public_key_claims (
 key_id p11.entity_id PRIMARY KEY,
 recipient_id p11.entity_id NOT NULL REFERENCES p11.endpoint_public_bundles(recipient_id),
 owner_id p11.entity_id NOT NULL REFERENCES p11.users(id),
 purpose text NOT NULL CHECK(purpose IN ('IDENTITY','CONTENT','RECEIPT')),
 fingerprint text NOT NULL UNIQUE CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 UNIQUE(recipient_id,purpose)
);

CREATE FUNCTION p11.guard_endpoint_public_bundle() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b jsonb; t jsonb; r record; old_bundle record;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Public endpoint history is retained'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(NEW)-'state'-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'state'-'revoked_at')
    OR (OLD.state='REVOKED' AND NEW IS DISTINCT FROM OLD)
    OR (OLD.state='RETIRED' AND NEW.state NOT IN ('RETIRED','REVOKED'))
    OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
   RAISE EXCEPTION 'Original public endpoint binding and revocation are immutable';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.state<>'ACTIVE' OR NEW.revoked_at IS NOT NULL THEN RAISE EXCEPTION 'New public endpoint must be active'; END IF;
 b:=convert_from(NEW.bundle_raw,'UTF8')::jsonb;
 IF b->>'profile_id' IS DISTINCT FROM 'P11_ENDPOINT_PUBLIC_1'
   OR b->>'owner_id' IS DISTINCT FROM NEW.owner_id::text
   OR b->>'endpoint_id' IS DISTINCT FROM NEW.endpoint_id::text
   OR b->>'role' IS DISTINCT FROM NEW.role
   OR b->>'version' IS DISTINCT FROM NEW.version::text
   OR b#>>'{content_receiving_key,key_id}' IS DISTINCT FROM NEW.recipient_id::text
   -- PostgreSQL parses fractional timestamp input at microsecond precision.
   -- The application checks the exact signed nanosecond value and floors the
   -- DB projection; this structural check only bounds parser rounding.
   OR abs(extract(epoch FROM ((b->>'expires_at')::timestamptz-NEW.expires_at)))>0.000001
   OR (b->>'not_before')::timestamptz>NEW.registered_at+interval '1 microsecond' THEN
  RAISE EXCEPTION 'Original signed public bundle projection required';
 END IF;
 SELECT * INTO r FROM p11.recipient_keys WHERE id=NEW.recipient_id;
 IF NOT FOUND OR r.owner_id IS DISTINCT FROM NEW.owner_id OR r.device_id IS DISTINCT FROM NEW.device_id
   OR r.key_version IS DISTINCT FROM NEW.version OR r.profile_id<>'P11_ENDPOINT_PUBLIC_1'
   OR r.encryption_public_key IS DISTINCT FROM b#>>'{content_receiving_key,public_key}'
   OR r.signing_public_key IS DISTINCT FROM b#>>'{identity_signing_key,public_key}'
   OR r.expires_at IS DISTINCT FROM NEW.expires_at OR r.revoked_at IS NOT NULL OR r.trust_evidence_id IS NOT NULL THEN
  RAISE EXCEPTION 'Original public recipient projection required; directory is not trust evidence';
 END IF;
 IF NEW.role='SELLER' AND NOT EXISTS(SELECT 1 FROM p11.devices WHERE id=NEW.device_id AND seller_id=NEW.owner_id AND auth_key_digest=NEW.device_fingerprint AND identity_profile='ED25519_DEVICE_1' AND state NOT IN ('REVOKED','RETIRED','QUARANTINED')) THEN
  RAISE EXCEPTION 'Original authenticated provider device required';
 END IF;
 IF NEW.version>1 THEN
  SELECT * INTO old_bundle FROM p11.endpoint_public_bundles WHERE recipient_id=NEW.previous_recipient_id;
  IF NOT FOUND OR old_bundle.bundle_hash IS DISTINCT FROM NEW.previous_bundle_hash
    OR (old_bundle.owner_id,old_bundle.endpoint_id,old_bundle.role,old_bundle.version+1,old_bundle.device_id,old_bundle.device_fingerprint)
       IS DISTINCT FROM (NEW.owner_id,NEW.endpoint_id,NEW.role,NEW.version,NEW.device_id,NEW.device_fingerprint)
    OR old_bundle.state<>'RETIRED' OR old_bundle.expires_at<=NEW.registered_at THEN
   RAISE EXCEPTION 'Original retired predecessor and exact non-forking successor required';
  END IF;
  t:=convert_from(NEW.transition_raw,'UTF8')::jsonb;
  IF t->>'profile_id' IS DISTINCT FROM 'P11_ENDPOINT_ROTATION_1' OR t->>'previous_bundle_hash' IS DISTINCT FROM NEW.previous_bundle_hash
    OR t->'next_bundle' IS DISTINCT FROM b THEN RAISE EXCEPTION 'Original signed transition projection required'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER endpoint_public_bundle_immutable BEFORE INSERT OR UPDATE OR DELETE ON p11.endpoint_public_bundles FOR EACH ROW EXECUTE FUNCTION p11.guard_endpoint_public_bundle();

CREATE FUNCTION p11.guard_endpoint_public_claim() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE b jsonb; expected jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Original public key purpose claims are retained and immutable'; END IF;
 SELECT convert_from(bundle_raw,'UTF8')::jsonb INTO b FROM p11.endpoint_public_bundles WHERE recipient_id=NEW.recipient_id AND owner_id=NEW.owner_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'Original public bundle required for key claim'; END IF;
 expected:=CASE NEW.purpose WHEN 'IDENTITY' THEN b->'identity_signing_key' WHEN 'CONTENT' THEN b->'content_receiving_key' WHEN 'RECEIPT' THEN b->'buyer_receipt_key' END;
 IF expected IS NULL OR expected->>'key_id' IS DISTINCT FROM NEW.key_id::text OR expected->>'fingerprint' IS DISTINCT FROM NEW.fingerprint THEN
  RAISE EXCEPTION 'Original public key purpose and fingerprint required';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER endpoint_public_claim_immutable BEFORE INSERT OR UPDATE OR DELETE ON p11.endpoint_public_key_claims FOR EACH ROW EXECUTE FUNCTION p11.guard_endpoint_public_claim();

CREATE FUNCTION p11.guard_endpoint_public_recipient() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.profile_id='P11_ENDPOINT_PUBLIC_1' THEN RAISE EXCEPTION 'Public recipient history is retained'; END IF;
  RETURN OLD;
 END IF;
 IF (OLD.profile_id='P11_ENDPOINT_PUBLIC_1' OR NEW.profile_id='P11_ENDPOINT_PUBLIC_1') AND
    ((to_jsonb(NEW)-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'revoked_at') OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)) THEN
  RAISE EXCEPTION 'Public recipient projection cannot be replaced or unrevoked';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER endpoint_public_recipient_immutable BEFORE UPDATE OR DELETE ON p11.recipient_keys FOR EACH ROW EXECUTE FUNCTION p11.guard_endpoint_public_recipient();

CREATE FUNCTION p11.require_endpoint_public_projection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record; b record; claim_count integer;
BEGIN
 -- Read the final transaction state, not an intermediate deferred trigger row.
 IF TG_TABLE_NAME='recipient_keys' THEN
  SELECT * INTO r FROM p11.recipient_keys WHERE id=NEW.id;
  IF r.profile_id<>'P11_ENDPOINT_PUBLIC_1' THEN RETURN NEW; END IF;
  SELECT * INTO b FROM p11.endpoint_public_bundles WHERE recipient_id=r.id;
 ELSE
  SELECT * INTO b FROM p11.endpoint_public_bundles WHERE recipient_id=NEW.recipient_id;
  SELECT * INTO r FROM p11.recipient_keys WHERE id=b.recipient_id;
 END IF;
 IF b.recipient_id IS NULL OR r.id IS NULL OR r.profile_id<>'P11_ENDPOINT_PUBLIC_1' OR b.revoked_at IS DISTINCT FROM r.revoked_at THEN
  RAISE EXCEPTION 'Original public bundle and recipient must commit together';
 END IF;
 SELECT count(*) INTO claim_count FROM p11.endpoint_public_key_claims WHERE recipient_id=b.recipient_id;
 IF claim_count<>CASE b.role WHEN 'BUYER' THEN 3 ELSE 2 END THEN RAISE EXCEPTION 'All public key purpose claims must commit together'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER endpoint_public_recipient_complete AFTER INSERT OR UPDATE ON p11.recipient_keys DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.require_endpoint_public_projection();
CREATE CONSTRAINT TRIGGER endpoint_public_bundle_complete AFTER INSERT OR UPDATE ON p11.endpoint_public_bundles DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.require_endpoint_public_projection();
