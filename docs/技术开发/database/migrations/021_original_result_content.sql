-- SOURCE ONLY / UNAPPLIED. Original PLAINTEXT/STANDARD result transport.
-- No execution, metering, acceptance, wallet or payout authority is created.
SET search_path = p11, public;

CREATE TABLE p11.native_result_content (
 delivery_id text PRIMARY KEY REFERENCES p11.native_result_deliveries(delivery_id),
 job_id text NOT NULL REFERENCES p11.jobs(id),
 order_id text NOT NULL REFERENCES p11.orders(id),
 head_raw bytea NOT NULL CHECK(octet_length(head_raw) BETWEEN 64 AND 24576),
 head_digest text NOT NULL CHECK(head_digest ~ '^[0-9a-f]{64}$'),
 key_version text NOT NULL CHECK(key_version ~ '^[A-Za-z0-9_-]{1,128}$'),
 wrapped_key bytea CHECK(octet_length(wrapped_key)=60),
 terminal_ciphertext bytea CHECK(octet_length(terminal_ciphertext) BETWEEN 28 AND 2048),
 state text NOT NULL CHECK(state IN ('UPLOADING','READY','RETIRED')),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((state='RETIRED' AND wrapped_key IS NULL AND terminal_ciphertext IS NULL) OR
       (state='UPLOADING' AND wrapped_key IS NOT NULL AND terminal_ciphertext IS NULL) OR
       (state='READY' AND wrapped_key IS NOT NULL AND terminal_ciphertext IS NOT NULL))
);
CREATE INDEX native_result_content_expiry ON p11.native_result_content(expires_at,delivery_id) WHERE state<>'RETIRED';

CREATE TABLE p11.native_result_content_chunks (
 delivery_id text NOT NULL REFERENCES p11.native_result_content(delivery_id),
 chunk_index integer NOT NULL CHECK(chunk_index BETWEEN 0 AND 1023),
 plaintext_bytes integer NOT NULL CHECK(plaintext_bytes BETWEEN 1 AND 32768),
 content_digest text NOT NULL CHECK(content_digest ~ '^[0-9a-f]{64}$'),
 ciphertext bytea NOT NULL CHECK(octet_length(ciphertext)=plaintext_bytes+28),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(delivery_id,chunk_index)
);
CREATE TABLE p11.native_result_read_grants (
 id text PRIMARY KEY,
 delivery_id text NOT NULL REFERENCES p11.native_result_content(delivery_id),
 job_id text NOT NULL REFERENCES p11.jobs(id),
 order_id text NOT NULL REFERENCES p11.orders(id),
 buyer_id text NOT NULL REFERENCES p11.users(id),
 session_id text NOT NULL REFERENCES p11.auth_sessions(id),
 buyer_version bigint NOT NULL CHECK(buyer_version>0),
 head_digest text NOT NULL CHECK(head_digest ~ '^[0-9a-f]{64}$'),
 token_digest text NOT NULL CHECK(token_digest ~ '^[0-9a-f]{64}$'),
 key_version text NOT NULL CHECK(key_version ~ '^[A-Za-z0-9_-]{1,128}$'),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(delivery_id,token_digest)
);
CREATE INDEX native_result_read_grant_expiry ON p11.native_result_read_grants(delivery_id,expires_at) WHERE revoked_at IS NULL;

CREATE FUNCTION p11.guard_original_result_content() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE h jsonb; d jsonb; m jsonb; original record; expected_count integer; expected_bytes bigint; actual_count bigint; actual_bytes bigint; low_index integer; high_index integer;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Original result content metadata is retained'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.delivery_id,NEW.job_id,NEW.order_id,NEW.head_raw,NEW.head_digest,NEW.key_version,NEW.expires_at,NEW.created_at)
     IS DISTINCT FROM (OLD.delivery_id,OLD.job_id,OLD.order_id,OLD.head_raw,OLD.head_digest,OLD.key_version,OLD.expires_at,OLD.created_at)
     OR (OLD.state='RETIRED' AND NEW IS DISTINCT FROM OLD)
     OR (OLD.state='READY' AND NEW.state NOT IN ('READY','RETIRED'))
     OR (NEW.state<>'RETIRED' AND NEW.wrapped_key IS DISTINCT FROM OLD.wrapped_key)
     OR (OLD.state='READY' AND NEW.state='READY' AND NEW.terminal_ciphertext IS DISTINCT FROM OLD.terminal_ciphertext) THEN
   RAISE EXCEPTION 'Original content binding or terminal is immutable';
  END IF;
 ELSE
  h:=convert_from(NEW.head_raw,'UTF8')::jsonb;
  SELECT n.payload_raw,n.output_bytes,n.job_id,j.order_id,j.current_attempt_id,j.state AS job_state,dv.state AS delivery_state,l.execution_manifest,dev.device_public_key
   INTO original FROM p11.native_result_deliveries n JOIN p11.deliveries dv ON dv.id=n.delivery_id
   JOIN p11.jobs j ON j.id=n.job_id JOIN p11.execution_leases l ON l.id=n.lease_id
   JOIN p11.attempts a ON a.id=n.attempt_id JOIN p11.devices dev ON dev.id=a.device_id
   WHERE n.delivery_id=NEW.delivery_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'Original declared delivery required'; END IF;
  d:=convert_from(original.payload_raw,'UTF8')::jsonb;
  m:=original.execution_manifest;
  IF NEW.state<>'UPLOADING' OR NEW.job_id<>original.job_id OR NEW.order_id<>original.order_id
   OR original.current_attempt_id<>(d#>>'{scope,attempt_id}') OR original.job_state NOT IN ('WAITING_BUYER','ACCEPTED')
   OR original.delivery_state NOT IN ('WAITING_BUYER','VALID')
   OR h->>'profile_id'<>'P11_ORIGINAL_TEXT_CONTENT_1' OR h->>'delivery_id'<>NEW.delivery_id
   OR h->'declaration' IS DISTINCT FROM d OR h->'execution_manifest' IS DISTINCT FROM m
   OR h->>'provider_public_key'<>original.device_public_key OR m->>'mode'<>'PLAINTEXT' OR m->>'privacy'<>'STANDARD'
   OR (h->>'chunk_bytes')::integer<>32768 OR (h->>'chunk_count')::integer<>((original.output_bytes+32767)/32768)::integer
   OR h->>'state'<>'UPLOADING' OR h ? 'terminal'
   OR NEW.head_digest<>encode(sha256(NEW.head_raw),'hex')
   OR NEW.expires_at<>(d->>'completed_at')::timestamptz+interval '30 minutes'
   OR (h->>'expires_at')::timestamptz<>NEW.expires_at OR NEW.expires_at<=clock_timestamp() THEN
    RAISE EXCEPTION 'Content must bind the exact original plaintext declaration';
  END IF;
 END IF;
 IF NEW.state='READY' THEN
  SELECT n.output_bytes INTO expected_bytes FROM p11.native_result_deliveries n WHERE n.delivery_id=NEW.delivery_id;
  expected_count:=((expected_bytes+32767)/32768)::integer;
  SELECT count(*),COALESCE(sum(plaintext_bytes),0),min(chunk_index),max(chunk_index) INTO actual_count,actual_bytes,low_index,high_index FROM p11.native_result_content_chunks WHERE delivery_id=NEW.delivery_id;
  IF actual_count<>expected_count OR actual_bytes<>expected_bytes OR (expected_count>0 AND (low_index<>0 OR high_index<>expected_count-1)) THEN RAISE EXCEPTION 'Complete original content required'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_result_content_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_result_content FOR EACH ROW EXECUTE FUNCTION p11.guard_original_result_content();

CREATE FUNCTION p11.guard_original_result_chunk() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent record; n bigint; parts integer; expected integer;
BEGIN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'Original content chunk is immutable'; END IF;
 SELECT c.state,d.output_bytes INTO parent FROM p11.native_result_content c JOIN p11.native_result_deliveries d USING(delivery_id) WHERE c.delivery_id=COALESCE(NEW.delivery_id,OLD.delivery_id) FOR UPDATE OF c;
 IF TG_OP='DELETE' THEN
  IF parent.state<>'RETIRED' THEN RAISE EXCEPTION 'Retire the exact content before deleting encrypted chunks'; END IF;
  RETURN OLD;
 END IF;
 IF parent.state<>'UPLOADING' THEN RAISE EXCEPTION 'Original upload is closed'; END IF;
 n:=parent.output_bytes; parts:=((n+32767)/32768)::integer;
 expected:=LEAST(32768,n-NEW.chunk_index::bigint*32768)::integer;
 IF NEW.chunk_index>=parts OR NEW.plaintext_bytes<>expected THEN RAISE EXCEPTION 'Fixed original content chunk boundary required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_result_chunk_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_result_content_chunks FOR EACH ROW EXECUTE FUNCTION p11.guard_original_result_chunk();

CREATE FUNCTION p11.guard_original_result_read_grant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent record; active bigint;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Read grant metadata is retained'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(NEW)-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'revoked_at') OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN RAISE EXCEPTION 'Read grant scope is immutable'; END IF;
  RETURN NEW;
 END IF;
 SELECT c.job_id,c.order_id,c.head_digest,c.key_version,c.state,c.expires_at,o.buyer_id,u.version INTO parent
 FROM p11.native_result_content c JOIN p11.orders o ON o.id=c.order_id JOIN p11.users u ON u.id=o.buyer_id WHERE c.delivery_id=NEW.delivery_id FOR UPDATE OF c;
 IF NOT FOUND OR parent.state<>'READY' OR (NEW.job_id,NEW.order_id,NEW.buyer_id,NEW.buyer_version,NEW.head_digest,NEW.key_version) IS DISTINCT FROM (parent.job_id,parent.order_id,parent.buyer_id,parent.version,parent.head_digest,parent.key_version)
  OR NEW.expires_at>LEAST(parent.expires_at,clock_timestamp()+interval '5 minutes') OR NEW.expires_at<=clock_timestamp()
  OR NOT EXISTS(SELECT 1 FROM p11.auth_sessions s WHERE s.id=NEW.session_id AND s.user_id=NEW.buyer_id AND s.user_version=NEW.buyer_version AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'Current original buyer read-only delegation required'; END IF;
 SELECT count(*) INTO active FROM p11.native_result_read_grants WHERE delivery_id=NEW.delivery_id AND revoked_at IS NULL AND expires_at>clock_timestamp();
 IF active>=8 THEN RAISE EXCEPTION 'Original bounded read grant capacity reached'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_result_read_grant_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.native_result_read_grants FOR EACH ROW EXECUTE FUNCTION p11.guard_original_result_read_grant();
