-- R016: atomic, metadata-only original invocation preparation. Not a lease.
-- Existing 001-007 and native client SQLite SQL bytes remain unchanged.
ALTER TABLE p11.jobs
  ADD COLUMN invocation_profile text NOT NULL DEFAULT 'UNBOUND_JOB'
    CHECK(invocation_profile IN ('UNBOUND_JOB','ROUTE_PREPARE_1')),
  ADD COLUMN route_contract_id p11.entity_id REFERENCES p11.route_contracts(id),
  ADD COLUMN invoker_key_id p11.entity_id REFERENCES p11.api_keys(id),
  ADD COLUMN fingerprint_version text,
  ADD COLUMN invocation_spec jsonb,
  ADD COLUMN reservation_id p11.entity_id REFERENCES p11.budget_reservations(id)
    DEFERRABLE INITIALLY DEFERRED,
  ADD CONSTRAINT invocation_binding_shape CHECK
    ((invocation_profile='UNBOUND_JOB' AND route_contract_id IS NULL AND invoker_key_id IS NULL
      AND fingerprint_version IS NULL AND invocation_spec IS NULL AND reservation_id IS NULL) OR
     (invocation_profile='ROUTE_PREPARE_1' AND route_contract_id IS NOT NULL AND invoker_key_id IS NOT NULL
      AND fingerprint_version IS NOT NULL AND fingerprint_version ~ '^[A-Za-z][A-Za-z0-9_-]{2,127}$'
      AND invocation_spec IS NOT NULL AND jsonb_typeof(invocation_spec)='object' AND reservation_id IS NOT NULL));
CREATE INDEX route_invocation_count ON p11.jobs(route_contract_id,id)
  WHERE invocation_profile='ROUTE_PREPARE_1';
CREATE UNIQUE INDEX invocation_reservation_once ON p11.jobs(reservation_id)
  WHERE reservation_id IS NOT NULL;

CREATE FUNCTION p11.guard_route_invocation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o p11.orders%ROWTYPE; r p11.route_contracts%ROWTYPE; k p11.api_keys%ROWTYPE;
  p p11.payments%ROWTYPE; n bigint; spec jsonb;
BEGIN
  IF TG_OP='DELETE' THEN
    IF OLD.invocation_profile='ROUTE_PREPARE_1' THEN RAISE EXCEPTION 'original invocation is retained'; END IF;
    RETURN OLD;
  END IF;
  IF TG_OP='UPDATE' THEN
    IF OLD.invocation_profile IS DISTINCT FROM NEW.invocation_profile OR
      ROW(NEW.route_contract_id,NEW.invoker_key_id,NEW.fingerprint_version,NEW.invocation_spec,NEW.reservation_id)
        IS DISTINCT FROM ROW(OLD.route_contract_id,OLD.invoker_key_id,OLD.fingerprint_version,OLD.invocation_spec,OLD.reservation_id) THEN
      RAISE EXCEPTION 'invocation admission binding is immutable';
    END IF;
    IF OLD.invocation_profile='ROUTE_PREPARE_1' AND
      ROW(NEW.id,NEW.order_id,NEW.seller_id,NEW.caller_key,NEW.request_digest,NEW.deadline,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.seller_id,OLD.caller_key,OLD.request_digest,OLD.deadline,OLD.created_at) THEN
      RAISE EXCEPTION 'invocation original intent is immutable';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.invocation_profile='UNBOUND_JOB' THEN RETURN NEW; END IF;
  SELECT * INTO STRICT o FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
  SELECT * INTO STRICT r FROM p11.route_contracts WHERE id=NEW.route_contract_id FOR SHARE;
  SELECT * INTO STRICT k FROM p11.api_keys WHERE id=NEW.invoker_key_id FOR SHARE;
  SELECT * INTO STRICT p FROM p11.payments WHERE id=r.payment_id AND order_id=o.id FOR SHARE;
  spec:=NEW.invocation_spec;
  SELECT count(*) INTO n FROM p11.jobs WHERE route_contract_id=r.id AND invocation_profile='ROUTE_PREPARE_1';
  IF r.binding_profile<>'CONFIRMED_ROUTE_1' OR r.state<>'ACTIVE' OR r.order_id<>o.id OR
    r.buyer_id<>o.buyer_id OR r.seller_id<>o.seller_id OR NEW.seller_id<>o.seller_id OR
    o.mode<>'PLAINTEXT' OR o.privacy<>'STANDARD' OR o.state NOT IN ('READY','FULFILLING') OR
    NEW.state<>'INPUT_PENDING' OR NEW.version<>1 OR NEW.fencing_token<>0 OR
    NEW.current_attempt_id IS NOT NULL OR NEW.accepted_delivery_id IS NOT NULL OR
    NEW.deadline<=clock_timestamp() OR NEW.deadline>r.expires_at OR NEW.deadline>o.deadline OR
    n>=(r.definition->>'max_call_count')::bigint OR
    k.user_id<>o.buyer_id OR k.revoked_at IS NOT NULL OR k.expires_at<=clock_timestamp() OR NEW.deadline>k.expires_at OR
    NOT ('ROUTE_INVOKE'=ANY(k.scopes)) OR NOT (r.id::text=ANY(k.route_ids)) OR
    p.state<>'SETTLED' OR p.provider<>'ALIPAY' OR p.purpose<>'ORDER' OR p.purpose_ref<>o.id OR
    p.amount_fen<o.max_amount_fen OR (p.funds_expires_at IS NOT NULL AND NEW.deadline>p.funds_expires_at) OR
    EXISTS(SELECT 1 FROM p11.refunds WHERE payment_id=p.id AND state<>'FAILED') OR
    (SELECT count(*) FROM jsonb_object_keys(spec))<>4 OR
    spec->>'protocol_profile_id' IS DISTINCT FROM r.protocol_profile OR
    spec->>'public_model' IS DISTINCT FROM r.order_snapshot->'public_spec'->>'model_id' OR
    jsonb_typeof(spec->'max_output_tokens') IS DISTINCT FROM 'number' OR
    coalesce((spec->>'max_output_tokens')::numeric,0)<1 OR
    (spec->>'max_output_tokens')::numeric<>trunc((spec->>'max_output_tokens')::numeric) OR
    (spec->>'max_output_tokens')::numeric>32768 OR
    (spec->>'max_output_tokens')::numeric>(r.order_snapshot->'public_spec'->>'output_token_limit')::numeric OR
    jsonb_typeof(spec->'stream') IS DISTINCT FROM 'boolean' THEN
    RAISE EXCEPTION 'invocation must bind current original route, key, funding and limits';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER route_invocation_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.jobs
  FOR EACH ROW EXECUTE FUNCTION p11.guard_route_invocation();

-- Both references must exist at COMMIT. A caller cannot commit the job/count
-- without its original budget reservation and a safe preparation outbox event.
CREATE FUNCTION p11.verify_invocation_preparation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r p11.route_contracts%ROWTYPE;
BEGIN
  IF NEW.invocation_profile<>'ROUTE_PREPARE_1' THEN RETURN NULL; END IF;
  SELECT * INTO STRICT r FROM p11.route_contracts WHERE id=NEW.route_contract_id;
  IF NOT EXISTS(SELECT 1 FROM p11.budget_reservations b WHERE b.id=NEW.reservation_id
    AND b.order_id=NEW.order_id AND b.purpose_ref=NEW.id AND b.purpose_kind='JOB'
    AND b.accounting_profile='ORDER_CAPACITY_1' AND b.state='RESERVED'
    AND b.amount_fen=(r.definition->>'budget_per_call_fen')::bigint) OR
    NOT EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_type='ROUTE_INVOCATION'
      AND e.aggregate_id=NEW.id AND e.aggregate_version=1 AND e.event_type='RouteInvocationPrepared'
      AND e.schema_version='3.0' AND e.safe_payload=jsonb_build_object('job_id',NEW.id::text,
        'route_id',r.id::text,'order_id',NEW.order_id::text,'contract_hash',r.contract_hash::text)) THEN
    RAISE EXCEPTION 'invocation, original budget and preparation event commit together';
  END IF;
  RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER invocation_preparation_atomic AFTER INSERT ON p11.jobs
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.verify_invocation_preparation();
