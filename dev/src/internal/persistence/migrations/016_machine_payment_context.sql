-- R026 CODE_FIRST / SOURCE_ONLY. This migration has not been executed.
-- Published 001-015 and all native migrations retain their original bytes.
CREATE TABLE p11.payment_machine_contexts (
  payment_id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  capability_ref p11.entity_id NOT NULL,
  scope_hash p11.sha256_hex NOT NULL,
  intent_raw bytea NOT NULL CHECK(octet_length(intent_raw) BETWEEN 1 AND 8192),
  intent_digest p11.sha256_hex NOT NULL,
  policy_raw bytea NOT NULL CHECK(octet_length(policy_raw) BETWEEN 1 AND 8192),
  policy_digest p11.sha256_hex NOT NULL,
  precreate_request_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.channel_requests(id),
  last_receipt_id p11.entity_id,
  last_evidence_hash p11.sha256_hex,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
  FOREIGN KEY(order_id,buyer_id,seller_id) REFERENCES p11.orders(id,buyer_id,seller_id),
  UNIQUE(payment_id,scope_hash),
  CHECK(intent_digest=encode(sha256(intent_raw),'hex')),
  CHECK(policy_digest=encode(sha256(policy_raw),'hex')),
  CHECK((last_receipt_id IS NULL)=(last_evidence_hash IS NULL))
);
CREATE TABLE p11.payment_machine_receipts (
  id p11.entity_id PRIMARY KEY,
  payment_id p11.entity_id NOT NULL,
  scope_hash p11.sha256_hex NOT NULL,
  kind text NOT NULL CHECK(kind IN ('PRECREATED','VERIFIED_PAID')),
  evidence_hash p11.sha256_hex NOT NULL,
  channel_reference text,
  prepay_reference text,
  encrypted_raw bytea NOT NULL CHECK(octet_length(encrypted_raw) BETWEEN 1 AND 100000),
  encrypted_digest p11.sha256_hex NOT NULL,
  observed_at timestamptz NOT NULL,
  event_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.outbox_events(id) DEFERRABLE INITIALLY DEFERRED,
  writer_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(payment_id,scope_hash) REFERENCES p11.payment_machine_contexts(payment_id,scope_hash),
  UNIQUE(payment_id,kind,evidence_hash),
  UNIQUE(id,payment_id),
  CHECK(encrypted_digest=encode(sha256(encrypted_raw),'hex')),
  CHECK((kind='VERIFIED_PAID' AND channel_reference IS NOT NULL AND channel_reference ~ '^[0-9]{32}$') OR
        (kind='PRECREATED' AND prepay_reference IS NOT NULL AND prepay_reference ~ '^[A-Za-z0-9_]{1,32}$'))
);
ALTER TABLE p11.payment_machine_contexts ADD CONSTRAINT machine_last_receipt
  FOREIGN KEY(last_receipt_id,payment_id) REFERENCES p11.payment_machine_receipts(id,payment_id)
  DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE p11.payment_machine_recoveries (
  id p11.entity_id PRIMARY KEY,
  payment_id p11.entity_id NOT NULL,
  scope_hash p11.sha256_hex NOT NULL,
  buyer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  proof_ciphertext bytea,
  proof_cipher_digest p11.sha256_hex NOT NULL,
  expires_at timestamptz NOT NULL,
  channel_request_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.channel_requests(id),
  durable_job_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.durable_jobs(id),
  state text NOT NULL CHECK(state IN ('PENDING','VERIFYING','VERIFIED','UNKNOWN','RETIRED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(payment_id,scope_hash) REFERENCES p11.payment_machine_contexts(payment_id,scope_hash),
  CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 minutes'),
  CHECK((state='RETIRED' AND proof_ciphertext IS NULL) OR
    (state<>'RETIRED' AND proof_ciphertext IS NOT NULL AND octet_length(proof_ciphertext) BETWEEN 1 AND 8192
      AND proof_cipher_digest=encode(sha256(proof_ciphertext),'hex')))
);
CREATE UNIQUE INDEX machine_one_live_proof ON p11.payment_machine_recoveries(payment_id)
  WHERE state IN ('PENDING','VERIFYING','UNKNOWN');

CREATE FUNCTION p11.guard_machine_context() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p p11.payments%ROWTYPE; o p11.orders%ROWTYPE; i jsonb; s jsonb; c p11.channel_requests%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original payment context cannot be deleted'; END IF;
  IF TG_OP='UPDATE' THEN
    IF OLD.last_receipt_id IS NOT NULL AND NEW.last_receipt_id IS NULL THEN
      RAISE EXCEPTION 'original signed financial receipt cannot be cleared';
    END IF;
    IF (to_jsonb(NEW)-ARRAY['last_receipt_id','last_evidence_hash','updated_at']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['last_receipt_id','last_evidence_hash','updated_at']) THEN
      RAISE EXCEPTION 'original payment context is immutable';
    END IF;
    IF NEW.last_receipt_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM p11.payment_machine_receipts r
      WHERE r.id=NEW.last_receipt_id AND r.payment_id=NEW.payment_id AND r.evidence_hash=NEW.last_evidence_hash) THEN
      RAISE EXCEPTION 'original signed receipt required';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO p FROM p11.payments WHERE id=NEW.payment_id FOR UPDATE;
  SELECT * INTO o FROM p11.orders WHERE id=NEW.order_id;
  SELECT * INTO c FROM p11.channel_requests WHERE id=NEW.precreate_request_id;
  i:=convert_from(NEW.intent_raw,'UTF8')::jsonb; s:=i->'Scope';
  IF NOT (p.id=NEW.payment_id AND p.order_id=o.id AND o.buyer_id=NEW.buyer_id AND o.seller_id=NEW.seller_id
    AND o.buyer_id<>o.seller_id AND p.product_profile_revision='P11_MACHINE_PAY_PREPAID_1'
    AND p.provider='ALIPAY' AND p.amount_fen BETWEEN 1 AND 5000 AND p.state='CREATED'
    AND s->>'Payment'=p.id AND s->>'Order'=o.id AND s->>'Buyer'=o.buyer_id AND s->>'Seller'=o.seller_id
    AND s->>'Purpose'=p.purpose AND s->>'PurposeRef'=p.purpose_ref AND s->>'OutTradeNo'=p.original_request_key
    AND s->>'ContractHash'=o.contract_hash AND (i->>'Amount')::bigint=p.amount_fen
    AND (i->>'ContractDeadline')::timestamptz=o.deadline AND (i->>'CreatedAt')::timestamptz=p.created_at
    AND c.target_ref=p.id AND c.operation='PAY' AND c.provider=p.provider AND c.environment=p.environment
    AND c.original_request_key=p.original_request_key AND c.request_digest=NEW.intent_digest AND c.state='INTENT'
    AND p.channel_binding_snapshot->>'Seller'=o.seller_id) IS TRUE THEN
    RAISE EXCEPTION 'original Machine Pay intent does not match';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_context_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.payment_machine_contexts
  FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_context();

CREATE FUNCTION p11.guard_machine_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m p11.payment_machine_contexts%ROWTYPE; r jsonb;
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'signed financial receipts are append only'; END IF;
  SELECT * INTO m FROM p11.payment_machine_contexts WHERE payment_id=NEW.payment_id;
  r:=convert_from(NEW.encrypted_raw,'UTF8')::jsonb;
  IF NOT (NEW.writer_xact=pg_current_xact_id() AND m.scope_hash=NEW.scope_hash
    AND r->>'profile'='P11_MACHINE_PAY_SIGNED_RECEIPT_1'
    AND r->'metadata'->'scope'=(convert_from(m.intent_raw,'UTF8')::jsonb)->'Scope'
    AND r->'metadata'->>'kind'=NEW.kind AND r->'metadata'->>'evidence_hash'=NEW.evidence_hash
    AND (r->'metadata'->>'amount_fen')::bigint=((convert_from(m.intent_raw,'UTF8')::jsonb)->>'Amount')::bigint
    AND COALESCE(r->'metadata'->>'trade','')=COALESCE(NEW.channel_reference,'')
    AND COALESCE(r->'metadata'->>'prepay','')=COALESCE(NEW.prepay_reference,'')
    AND (r->'metadata'->>'observed_at')::timestamptz=NEW.observed_at) IS TRUE THEN
    RAISE EXCEPTION 'original encrypted signed receipt required';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_receipt_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.payment_machine_receipts
  FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_receipt();

CREATE FUNCTION p11.guard_machine_payment_update() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m p11.payment_machine_contexts%ROWTYPE;
BEGIN
  SELECT * INTO m FROM p11.payment_machine_contexts WHERE payment_id=OLD.id;
  IF NOT FOUND THEN RETURN NEW; END IF;
  IF (to_jsonb(NEW)-ARRAY['state','version','prepay_reference','channel_reference']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['state','version','prepay_reference','channel_reference']) THEN
    RAISE EXCEPTION 'original Machine Pay binding is immutable';
  END IF;
  IF OLD.state='SETTLED' AND NEW.state<>'SETTLED' THEN RAISE EXCEPTION 'paid fact cannot be downgraded'; END IF;
  IF NEW.state='SETTLED' AND NOT EXISTS(SELECT 1 FROM p11.payment_machine_receipts r
    WHERE r.id=m.last_receipt_id AND r.payment_id=NEW.id AND r.kind='VERIFIED_PAID'
      AND r.evidence_hash=m.last_evidence_hash AND r.channel_reference=NEW.channel_reference
      AND (OLD.state='SETTLED' OR r.writer_xact=pg_current_xact_id())) THEN
    RAISE EXCEPTION 'same-transaction verified original signed receipt required';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_payment_update BEFORE UPDATE ON p11.payments
  FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_payment_update();

CREATE FUNCTION p11.guard_machine_recovery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m p11.payment_machine_contexts%ROWTYPE; c p11.channel_requests%ROWTYPE; j p11.durable_jobs%ROWTYPE;
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original proof recovery must retain its tombstone'; END IF;
  IF TG_OP='UPDATE' THEN
    IF (to_jsonb(NEW)-ARRAY['state','version','proof_ciphertext']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['state','version','proof_ciphertext']) OR
       (NEW.proof_ciphertext IS DISTINCT FROM OLD.proof_ciphertext AND NOT (NEW.state='RETIRED' AND NEW.proof_ciphertext IS NULL)) OR
       (OLD.state='RETIRED' AND NEW.state<>'RETIRED') THEN
      RAISE EXCEPTION 'original recovery identity and proof cannot change';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO m FROM p11.payment_machine_contexts WHERE payment_id=NEW.payment_id;
  SELECT * INTO c FROM p11.channel_requests WHERE id=NEW.channel_request_id;
  SELECT * INTO j FROM p11.durable_jobs WHERE id=NEW.durable_job_id;
  IF NOT (m.scope_hash=NEW.scope_hash AND m.buyer_id=NEW.buyer_id AND NEW.state='PENDING'
    AND c.operation='QUERY' AND c.target_ref=NEW.payment_id AND c.original_request_key=NEW.id
    AND c.request_digest=NEW.proof_cipher_digest AND c.state='INTENT'
    AND j.kind='VERIFY_MACHINE_PAYMENT' AND j.target_ref=NEW.id AND j.state='PENDING') IS TRUE THEN
    RAISE EXCEPTION 'original proof and dedicated verification job required';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_recovery_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.payment_machine_recoveries
  FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_recovery();
