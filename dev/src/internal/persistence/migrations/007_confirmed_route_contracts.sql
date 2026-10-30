-- R015: immutable original-order route configuration. No invocation/funds grant.
ALTER TABLE p11.route_contracts
  ADD COLUMN binding_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED'
    CHECK(binding_profile IN ('LEGACY_UNVERIFIED','CONFIRMED_ROUTE_1')),
  ADD COLUMN definition jsonb,
  ADD COLUMN order_snapshot jsonb,
  ADD COLUMN payment_id p11.entity_id,
  ADD COLUMN created_at timestamptz,
  ADD CONSTRAINT route_payment_order FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
  ADD CONSTRAINT route_definition_shape CHECK
    ((binding_profile='LEGACY_UNVERIFIED' AND definition IS NULL AND order_snapshot IS NULL AND payment_id IS NULL AND created_at IS NULL) OR
     (binding_profile='CONFIRMED_ROUTE_1' AND definition IS NOT NULL AND jsonb_typeof(definition)='object'
      AND order_snapshot IS NOT NULL AND jsonb_typeof(order_snapshot)='object' AND payment_id IS NOT NULL AND created_at IS NOT NULL));

-- Historical routing metadata cannot establish the missing confirmed terms.
-- Preserve references and history, but never manufacture payment/model consent.
UPDATE p11.route_contracts SET state='SUSPENDED',version=version+1 WHERE state='ACTIVE';
CREATE UNIQUE INDEX one_confirmed_route_per_order ON p11.route_contracts(order_id)
  WHERE binding_profile='CONFIRMED_ROUTE_1';
CREATE INDEX route_buyer_cursor ON p11.route_contracts(buyer_id,id);

CREATE FUNCTION p11.guard_confirmed_route() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o p11.orders%ROWTYPE; p p11.payments%ROWTYPE; d jsonb;
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'route history is retained'; END IF;
  IF TG_OP='UPDATE' THEN
    IF (to_jsonb(NEW)-ARRAY['state','version']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['state','version'])
      OR NEW.version<>OLD.version+1 OR NEW.state NOT IN ('SUSPENDED','EXPIRED','REVOKED')
      OR OLD.state IN ('EXPIRED','REVOKED') OR OLD.binding_profile<>'CONFIRMED_ROUTE_1' THEN
      RAISE EXCEPTION 'route binding is immutable and terminal routes cannot reactivate';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO STRICT o FROM p11.orders WHERE id=NEW.order_id FOR SHARE;
  SELECT * INTO STRICT p FROM p11.payments WHERE id=NEW.payment_id AND order_id=o.id FOR SHARE;
  d:=NEW.definition;
  IF NEW.binding_profile<>'CONFIRMED_ROUTE_1' OR NEW.state<>'ACTIVE' OR NEW.version<>1 OR
    NEW.buyer_id<>o.buyer_id OR NEW.seller_id<>o.seller_id OR NEW.contract_hash<>o.contract_hash OR
    NEW.order_snapshot IS DISTINCT FROM o.contract_snapshot OR o.mode<>'PLAINTEXT' OR o.privacy<>'STANDARD' OR
    o.state NOT IN ('READY','FULFILLING') OR NEW.expires_at>o.deadline OR NEW.expires_at<=clock_timestamp() OR
    NEW.protocol_profile NOT IN ('P11_CHAT_TEXT_NATIVE_1','P11_RESPONSES_TEXT_NATIVE_1','P11_MESSAGES_TEXT_NATIVE_1') OR
    d->>'confirmed_order_id' IS DISTINCT FROM o.id::text OR d->>'seller_id' IS DISTINCT FROM o.seller_id::text OR
    d->>'listing_id' IS DISTINCT FROM o.contract_snapshot->>'listing_id' OR
    d->'listing_revision' IS DISTINCT FROM o.contract_snapshot->'listing_revision' OR
    d->'policy' IS DISTINCT FROM o.contract_snapshot->'policy' OR
    d->'policy'->>'protocol_profile_id' IS DISTINCT FROM NEW.protocol_profile OR
    d->>'purchase_authorization_ref' IS DISTINCT FROM NEW.payment_id::text OR
    d->>'expires_at' IS NULL OR (d->>'expires_at')::timestamptz IS DISTINCT FROM NEW.expires_at OR
    coalesce((d->>'total_budget_fen')::numeric,-1)<>o.max_amount_fen OR
    coalesce((d->>'budget_per_call_fen')::numeric,0)<1 OR
    (d->>'budget_per_call_fen')::numeric>o.max_amount_fen-o.budget_reserved_fen-o.budget_consumed_fen-o.budget_unverified_fen OR
    coalesce((d->>'max_call_count')::numeric,0)<1 OR (d->>'max_call_count')::numeric>2147483647 OR
    d ? 'recipient_id' OR d ? 'recipient_key_hash' OR
    p.purpose<>'ORDER' OR p.purpose_ref<>o.id OR p.provider<>'ALIPAY' OR p.state<>'SETTLED' OR
    p.amount_fen<o.max_amount_fen OR coalesce(length(p.channel_reference),0)=0 OR
    coalesce(length(p.product_profile_revision),0)=0 OR jsonb_typeof(p.channel_binding_snapshot)<>'object' OR
    p.channel_binding_snapshot='{}'::jsonb OR (p.funds_expires_at IS NOT NULL AND NEW.expires_at>p.funds_expires_at) OR
    EXISTS(SELECT 1 FROM p11.refunds WHERE payment_id=p.id AND state<>'FAILED') THEN
    RAISE EXCEPTION 'route must bind eligible original order and persisted settled payment';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER confirmed_route_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.route_contracts
  FOR EACH ROW EXECUTE FUNCTION p11.guard_confirmed_route();

-- Budget counter/version and legitimate order/payment lifecycle changes remain
-- possible. They cannot rewrite the identity, price or consent of a bound route.
CREATE FUNCTION p11.guard_routed_order_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.id,NEW.quote_id,NEW.buyer_id,NEW.seller_id,NEW.mode,NEW.privacy,NEW.currency,
         NEW.max_amount_fen,NEW.fee_bps,NEW.contract_snapshot,NEW.contract_hash,NEW.deadline)
    IS DISTINCT FROM
     ROW(OLD.id,OLD.quote_id,OLD.buyer_id,OLD.seller_id,OLD.mode,OLD.privacy,OLD.currency,
         OLD.max_amount_fen,OLD.fee_bps,OLD.contract_snapshot,OLD.contract_hash,OLD.deadline)
    AND EXISTS(SELECT 1 FROM p11.route_contracts WHERE order_id=OLD.id AND binding_profile='CONFIRMED_ROUTE_1') THEN
    RAISE EXCEPTION 'routed original order binding is immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER routed_order_binding_guard BEFORE UPDATE ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.guard_routed_order_binding();

CREATE FUNCTION p11.guard_routed_payment_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (to_jsonb(NEW)-ARRAY['state','version','funds_expires_at']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['state','version','funds_expires_at']) AND
    EXISTS(SELECT 1 FROM p11.route_contracts WHERE payment_id=OLD.id AND binding_profile='CONFIRMED_ROUTE_1') THEN
    RAISE EXCEPTION 'routed original payment binding is immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER routed_payment_binding_guard BEFORE UPDATE ON p11.payments
  FOR EACH ROW EXECUTE FUNCTION p11.guard_routed_payment_binding();
