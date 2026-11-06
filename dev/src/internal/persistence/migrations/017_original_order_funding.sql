-- R026 CODE_FIRST / SOURCE_ONLY; never executed in this phase.
-- Published platform001-016 and native DDL/source.json retain original bytes.
-- This is original-order allocation metadata, NOT a wallet or money transfer.
CREATE TABLE p11.order_fundings (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL UNIQUE,
  payment_id p11.entity_id NOT NULL UNIQUE,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  contract_hash p11.sha256_hex NOT NULL,
  scope_hash p11.sha256_hex NOT NULL,
  binding_hash p11.sha256_hex NOT NULL,
  profile text NOT NULL CHECK(profile='P11_ORIGINAL_ORDER_FUNDS_1'),
  environment text NOT NULL CHECK(environment IN ('SANDBOX','PRODUCTION')),
  amount_fen p11.money_fen NOT NULL CHECK(amount_fen BETWEEN 1 AND 5000),
  receipt_id p11.entity_id NOT NULL,
  approval_raw bytea NOT NULL CHECK(octet_length(approval_raw) BETWEEN 1 AND 8192),
  approval_digest p11.sha256_hex NOT NULL,
  valid_until timestamptz NOT NULL,
  admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  event_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.outbox_events(id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(order_id,buyer_id,seller_id) REFERENCES p11.orders(id,buyer_id,seller_id),
  FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
  FOREIGN KEY(payment_id,scope_hash) REFERENCES p11.payment_machine_contexts(payment_id,scope_hash),
  FOREIGN KEY(receipt_id,payment_id) REFERENCES p11.payment_machine_receipts(id,payment_id),
  CHECK(approval_digest=encode(sha256(approval_raw),'hex')),
  CHECK(valid_until>admitted_at),
  CHECK(buyer_id<>seller_id)
);

CREATE FUNCTION p11.guard_original_order_funding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o p11.orders%ROWTYPE; p p11.payments%ROWTYPE; c p11.payment_machine_contexts%ROWTYPE;
  r p11.payment_machine_receipts%ROWTYPE; a jsonb; b jsonb; s jsonb; policy jsonb;
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original funding allocation is immutable'; END IF;
  SELECT * INTO o FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
  SELECT * INTO p FROM p11.payments WHERE id=NEW.payment_id FOR SHARE;
  SELECT * INTO c FROM p11.payment_machine_contexts WHERE payment_id=NEW.payment_id FOR SHARE;
  SELECT * INTO r FROM p11.payment_machine_receipts WHERE id=NEW.receipt_id;
  a:=convert_from(NEW.approval_raw,'UTF8')::jsonb; b:=p.channel_binding_snapshot;
  s:=(convert_from(c.intent_raw,'UTF8')::jsonb)->'Scope';
  policy:=convert_from(c.policy_raw,'UTF8')::jsonb;
  IF NOT (o.id=c.order_id AND o.buyer_id=NEW.buyer_id AND o.seller_id=NEW.seller_id
    AND o.contract_hash=NEW.contract_hash AND o.state='WAITING_PAYMENT'
    AND o.budget_reserved_fen=0 AND o.budget_consumed_fen=0 AND o.budget_unverified_fen=0
    AND o.deadline>NEW.admitted_at AND NEW.valid_until<=o.deadline
    AND NEW.valid_until<=(b->>'ValidUntil')::timestamptz
    AND p.order_id=o.id AND p.purpose='ORDER' AND p.purpose_ref=o.id AND p.state='SETTLED'
    AND p.provider='ALIPAY' AND p.environment=NEW.environment
    AND p.product_profile_revision='P11_MACHINE_PAY_PREPAID_1'
    AND p.amount_fen=NEW.amount_fen AND NEW.amount_fen=o.max_amount_fen
    AND (p.funds_expires_at IS NULL OR NEW.valid_until<=p.funds_expires_at)
    AND c.scope_hash=NEW.scope_hash AND s->>'BindingHash'=NEW.binding_hash
    AND r.payment_id=p.id AND r.scope_hash=NEW.scope_hash AND r.kind='VERIFIED_PAID'
    AND r.channel_reference=p.channel_reference AND c.last_receipt_id=r.id
    AND NOT EXISTS(SELECT 1 FROM p11.refunds f WHERE f.payment_id=p.id AND f.state<>'FAILED')
    AND o.fee_bps=0 AND (o.contract_snapshot->>'buyer_fee_fen')::bigint=0
    AND (o.contract_snapshot->'price'->>'commission_bps')::integer=0
    AND policy->>'collection'='SELLER_DIRECT_NO_FEE'
    AND a->'scope'=jsonb_build_object('payment_profile',p.product_profile_revision,'seller_id',o.seller_id,
      'app_id',b->>'AppID','service_id',b->>'ServiceID','environment',p.environment,
      'revision',b->>'Revision','authority_subject',b->>'AuthoritySubject',
      'signing_key_hash',b->>'SigningKeyHash','alipay_key_hash',b->>'AlipayKeyHash',
      'sku_id',policy->>'sku_id','mode',o.mode,'privacy',o.privacy,'collection',policy->>'collection')
    AND a->'evidence'=jsonb_build_array(
      jsonb_build_object('id',b->>'AuthorizationRef','digest',b->>'AuthorizationHash','kind','MACHINE_PAY_AUTHORITY_1'),
      jsonb_build_object('id',b->>'QualificationRef','digest',b->>'QualificationHash','kind','MACHINE_PAY_PERSONAL_SELLER_1'),
      jsonb_build_object('id',policy->>'refund_evidence_ref','digest',policy->>'refund_evidence_hash','kind','MACHINE_PAY_REFUND_ROUTE_1'),
      jsonb_build_object('id',policy->>'commission_evidence_ref','digest',policy->>'commission_evidence_hash','kind','MACHINE_PAY_COMMISSION_ROUTE_1'))
    ) IS TRUE THEN RAISE EXCEPTION 'verified original paid scope and allocation required'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER original_order_funding_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.order_fundings
 FOR EACH ROW EXECUTE FUNCTION p11.guard_original_order_funding();

-- Original Machine refund writers serialize through the same parent as all
-- funding readers. Production services must also lock order -> payment BEFORE
-- touching a refund row. The trigger guards direct SQL, not channel authority.
CREATE FUNCTION p11.guard_machine_refund_parent() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent p11.entity_id; original p11.entity_id;
BEGIN
  IF TG_OP='DELETE' THEN
    parent:=OLD.order_id; original:=OLD.payment_id;
  ELSE
    parent:=NEW.order_id; original:=NEW.payment_id;
  END IF;
  IF TG_OP='UPDATE' AND ROW(NEW.id,NEW.payment_id,NEW.order_id,NEW.original_request_key,NEW.amount_fen)
      IS DISTINCT FROM ROW(OLD.id,OLD.payment_id,OLD.order_id,OLD.original_request_key,OLD.amount_fen)
      AND EXISTS(SELECT 1 FROM p11.payment_machine_contexts WHERE payment_id IN (NEW.payment_id,OLD.payment_id)) THEN
    RAISE EXCEPTION 'original machine refund scope is immutable';
  END IF;
  IF EXISTS(SELECT 1 FROM p11.payment_machine_contexts WHERE payment_id=original) THEN
    PERFORM 1 FROM p11.orders WHERE id=parent FOR UPDATE;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original machine refund obligation cannot be deleted'; END IF;
    IF TG_OP='UPDATE' AND OLD.state='SUCCEEDED' AND NEW.state<>'SUCCEEDED' THEN
      RAISE EXCEPTION 'confirmed refund cannot be downgraded into spendable funds';
    END IF;
  END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_refund_parent_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.refunds
 FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_refund_parent();

-- Paid is not READY. Only the original unique allocation may advance a real
-- Machine order. Historic non-Machine orders are not backfilled or authorized.
CREATE FUNCTION p11.guard_machine_order_funding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.state IN ('READY','FULFILLING') AND
      EXISTS(SELECT 1 FROM p11.payment_machine_contexts WHERE order_id=NEW.id) THEN
    IF OLD.state NOT IN ('WAITING_PAYMENT','READY','FULFILLING') OR NOT EXISTS(
      SELECT 1 FROM p11.order_fundings f JOIN p11.payments p ON p.id=f.payment_id
      WHERE f.order_id=NEW.id AND f.buyer_id=NEW.buyer_id AND f.seller_id=NEW.seller_id
        AND f.contract_hash=NEW.contract_hash AND p.state='SETTLED'
        AND f.valid_until>clock_timestamp() AND NEW.deadline>clock_timestamp()
        AND NOT EXISTS(SELECT 1 FROM p11.refunds r WHERE r.payment_id=p.id AND r.state<>'FAILED')) THEN
      RAISE EXCEPTION 'original machine order requires its current unique funding allocation';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER machine_order_funding_guard BEFORE UPDATE OF state ON p11.orders
 FOR EACH ROW EXECUTE FUNCTION p11.guard_machine_order_funding();

-- Existing real Machine paid facts get a dedicated metadata-only admission job.
-- Legacy synthetic/unknown flags never become funding or qualified money.
INSERT INTO p11.durable_jobs(id,kind,dedupe_key,target_ref,state,run_at)
SELECT 'mfj_'||encode(sha256(convert_to(p.id,'UTF8')),'hex'),'ADMIT_MACHINE_FUNDS',
  'machine-funds:'||p.id,p.id,'PENDING',clock_timestamp()
FROM p11.payments p JOIN p11.payment_machine_contexts c ON c.payment_id=p.id
JOIN p11.payment_machine_receipts r ON r.id=c.last_receipt_id AND r.payment_id=p.id
WHERE p.state='SETTLED' AND p.product_profile_revision='P11_MACHINE_PAY_PREPAID_1'
 AND r.kind='VERIFIED_PAID' AND r.channel_reference=p.channel_reference
ON CONFLICT(dedupe_key) DO NOTHING;
