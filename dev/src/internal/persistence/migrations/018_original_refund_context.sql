-- R026 CODE_FIRST / SOURCE_ONLY. Not executed. Published 001-017 unchanged.
CREATE TABLE p11.refund_machine_contexts (
  refund_id p11.entity_id PRIMARY KEY REFERENCES p11.refunds(id),
  payment_id p11.entity_id NOT NULL,
  order_id p11.entity_id NOT NULL,
  route_ref p11.entity_id NOT NULL,
  scope_raw bytea NOT NULL CHECK(octet_length(scope_raw) BETWEEN 1 AND 8192),
  scope_hash p11.sha256_hex NOT NULL,
  intent_raw bytea NOT NULL CHECK(octet_length(intent_raw) BETWEEN 1 AND 8192),
  intent_digest p11.sha256_hex NOT NULL,
  route_raw bytea NOT NULL CHECK(octet_length(route_raw) BETWEEN 1 AND 8192),
  route_digest p11.sha256_hex NOT NULL,
  decision_evidence_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.evidence(id),
  decision_raw bytea NOT NULL CHECK(octet_length(decision_raw) BETWEEN 1 AND 8192),
  decision_digest p11.sha256_hex NOT NULL,
  paid_receipt_id p11.entity_id NOT NULL REFERENCES p11.payment_machine_receipts(id),
  submit_request_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.channel_requests(id),
  query_request_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.channel_requests(id),
  durable_job_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.durable_jobs(id),
  dispatched_at timestamptz,
  next_query_at timestamptz,
  query_generation p11.counter NOT NULL DEFAULT 0,
  last_receipt_id p11.entity_id,
  last_evidence_hash p11.sha256_hex,
  observed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
  UNIQUE(refund_id,scope_hash),
  CHECK(scope_hash=encode(sha256(scope_raw),'hex')),
  CHECK(intent_digest=encode(sha256(intent_raw),'hex')),
  CHECK(route_digest=encode(sha256(route_raw),'hex')),
  CHECK(decision_digest=encode(sha256(decision_raw),'hex')),
  CHECK((last_receipt_id IS NULL)=(last_evidence_hash IS NULL)),
  CHECK(last_evidence_hash IS NULL OR observed_at IS NOT NULL)
);
CREATE INDEX refund_machine_payment_dispatch ON p11.refund_machine_contexts(payment_id,dispatched_at);
CREATE TABLE p11.refund_machine_receipts (
  id p11.entity_id PRIMARY KEY,
  refund_id p11.entity_id NOT NULL,
  scope_hash p11.sha256_hex NOT NULL,
  intent_digest p11.sha256_hex NOT NULL,
  kind text NOT NULL CHECK(kind IN ('REFUND_SUBMITTED','REFUND_QUERY_UNRESOLVED','REFUND_CONFIRMED')),
  method text NOT NULL,
  evidence_hash p11.sha256_hex NOT NULL,
  amount_fen p11.money_fen NOT NULL CHECK(amount_fen BETWEEN 1 AND 5000),
  cumulative_fen p11.money_fen NOT NULL CHECK(cumulative_fen<=5000),
  observed_at timestamptz NOT NULL,
  encrypted_raw bytea NOT NULL CHECK(octet_length(encrypted_raw) BETWEEN 1 AND 100000),
  encrypted_digest p11.sha256_hex NOT NULL,
  event_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.outbox_events(id) DEFERRABLE INITIALLY DEFERRED,
  writer_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(refund_id,scope_hash) REFERENCES p11.refund_machine_contexts(refund_id,scope_hash),
  UNIQUE(id,refund_id),
  CHECK(encrypted_digest=encode(sha256(encrypted_raw),'hex')),
  CHECK((kind='REFUND_SUBMITTED' AND method='alipay.trade.refund') OR
        (kind IN ('REFUND_QUERY_UNRESOLVED','REFUND_CONFIRMED') AND method='alipay.trade.fastpay.refund.query' AND cumulative_fen=0))
);
ALTER TABLE p11.refund_machine_contexts ADD CONSTRAINT refund_last_signed_receipt_fk
 FOREIGN KEY(last_receipt_id,refund_id) REFERENCES p11.refund_machine_receipts(id,refund_id) DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION p11.guard_original_refund_context() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE i jsonb; s jsonb; r jsonb; d jsonb; f p11.refunds%ROWTYPE;
 p p11.payments%ROWTYPE; c p11.payment_machine_contexts%ROWTYPE; o p11.orders%ROWTYPE;
 paid p11.payment_machine_receipts%ROWTYPE; submit p11.channel_requests%ROWTYPE;
 query_request p11.channel_requests%ROWTYPE; j p11.durable_jobs%ROWTYPE; previous_dispatch timestamptz;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original refund context cannot be deleted'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (to_jsonb(NEW)-ARRAY['dispatched_at','next_query_at','query_generation','last_receipt_id','last_evidence_hash','observed_at','updated_at']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['dispatched_at','next_query_at','query_generation','last_receipt_id','last_evidence_hash','observed_at','updated_at']) THEN
   RAISE EXCEPTION 'original refund intent and decision are immutable';
  END IF;
  IF (OLD.dispatched_at IS NOT NULL AND NEW.dispatched_at IS DISTINCT FROM OLD.dispatched_at) OR
     NEW.query_generation<OLD.query_generation OR NEW.query_generation::numeric>OLD.query_generation::numeric+1 OR
     (OLD.observed_at IS NOT NULL AND (NEW.observed_at IS NULL OR NEW.observed_at<OLD.observed_at)) OR
     (OLD.last_receipt_id IS NOT NULL AND NEW.last_receipt_id IS NULL) THEN
   RAISE EXCEPTION 'original refund dispatch and evidence cannot rewind';
  END IF;
  IF OLD.dispatched_at IS NULL AND NEW.dispatched_at IS NOT NULL THEN
   PERFORM 1 FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
   PERFORM 1 FROM p11.payments WHERE id=NEW.payment_id FOR UPDATE;
   SELECT max(dispatched_at) INTO previous_dispatch FROM p11.refund_machine_contexts
    WHERE payment_id=NEW.payment_id AND refund_id<>NEW.refund_id;
   IF NEW.query_generation<>1 OR (previous_dispatch IS NOT NULL AND NEW.dispatched_at<previous_dispatch+interval '3 seconds') THEN
    RAISE EXCEPTION 'same original trade refund submissions require a three second boundary';
   END IF;
  END IF;
  IF EXISTS(SELECT 1 FROM p11.refunds WHERE id=NEW.refund_id AND state='SUCCEEDED') AND
    (to_jsonb(NEW)-'updated_at') IS DISTINCT FROM (to_jsonb(OLD)-'updated_at') THEN
   RAISE EXCEPTION 'confirmed original refund context cannot change';
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO f FROM p11.refunds WHERE id=NEW.refund_id;
 SELECT * INTO o FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO p FROM p11.payments WHERE id=NEW.payment_id FOR UPDATE;
 SELECT * INTO c FROM p11.payment_machine_contexts WHERE payment_id=NEW.payment_id;
 SELECT * INTO paid FROM p11.payment_machine_receipts WHERE id=NEW.paid_receipt_id;
 SELECT * INTO submit FROM p11.channel_requests WHERE id=NEW.submit_request_id;
 SELECT * INTO query_request FROM p11.channel_requests WHERE id=NEW.query_request_id;
 SELECT * INTO j FROM p11.durable_jobs WHERE id=NEW.durable_job_id;
 i:=convert_from(NEW.intent_raw,'UTF8')::jsonb; s:=convert_from(NEW.scope_raw,'UTF8')::jsonb;
 r:=convert_from(NEW.route_raw,'UTF8')::jsonb; d:=convert_from(NEW.decision_raw,'UTF8')::jsonb;
 IF NOT (p.state='SETTLED' AND p.purpose='ORDER' AND p.purpose_ref=o.id AND p.amount_fen=o.max_amount_fen
  AND f.payment_id=p.id AND f.order_id=o.id AND f.state='REQUESTED'
  AND NEW.order_id=p.order_id AND c.order_id=o.id AND i->>'profile'='P11_ALIPAY_ORIGINAL_REFUND_1'
  AND i->'scope'=s AND s->'original'=(convert_from(c.intent_raw,'UTF8')::jsonb)->'Scope'
  AND s->>'refund_id'=f.id AND s->>'original_request_key'=f.original_request_key
  AND s->>'route_hash'=NEW.route_digest AND s->>'decision_ref'=NEW.decision_evidence_id
  AND s->>'decision_hash'=i->'authority'->>'EvidenceHash'
  AND i->'authority'->>'Reference'=NEW.decision_evidence_id
  AND i->'authority'->>'Payment'=p.id AND i->'authority'->>'Order'=o.id
  AND (i->>'amount_fen')::bigint=f.amount_fen AND (i->>'collected_fen')::bigint=p.amount_fen
  AND f.amount_fen BETWEEN 1 AND (i->'authority'->>'Maximum')::bigint
  AND (i->'authority'->>'Maximum')::bigint<=p.amount_fen
  AND i->>'original_trade_no'=p.channel_reference AND i->>'paid_evidence_hash'=paid.evidence_hash
  AND paid.payment_id=p.id AND paid.scope_hash=c.scope_hash AND paid.kind='VERIFIED_PAID'
  AND paid.channel_reference=p.channel_reference
  AND (i->>'created_at')::timestamptz=f.created_at AND paid.observed_at<=f.created_at
  AND (i->>'submit_before')::timestamptz>f.created_at
  AND (i->>'submit_before')::timestamptz<=(r->>'valid_until')::timestamptz
  AND (i->>'resolve_until')::timestamptz=(r->>'resolve_until')::timestamptz
  AND r->>'profile'='P11_ALIPAY_ORIGINAL_REFUND_1'
  AND r->>'original_binding_hash'=s->'original'->>'BindingHash'
  AND r->>'evidence_ref'=(convert_from(c.policy_raw,'UTF8')::jsonb)->>'refund_evidence_ref'
  AND r->>'evidence_hash'=(convert_from(c.policy_raw,'UTF8')::jsonb)->>'refund_evidence_hash'
  AND (r->>'valid_from')::timestamptz<=f.created_at AND (r->>'valid_until')::timestamptz>f.created_at
  AND (r->>'resolve_until')::timestamptz>=(r->>'valid_until')::timestamptz
  AND (r->>'resolve_until')::timestamptz<=(p.channel_binding_snapshot->>'ResolveUntil')::timestamptz
  AND d->>'refund_policy'='UNUSED_BALANCE_1' AND d->>'refund_route_hash'=NEW.route_digest
  AND d->>'payment_id'=p.id AND d->>'order_id'=o.id AND d->>'buyer_id'=o.buyer_id AND d->>'contract_hash'=o.contract_hash
  AND d->>'paid_evidence_hash'=paid.evidence_hash AND (d->>'order_version')::bigint=o.version
  AND (d->>'payment_version')::bigint=p.version AND (d->>'refund_amount_fen')::bigint=f.amount_fen
  AND (d->>'maximum_refundable_fen')::bigint=(i->'authority'->>'Maximum')::bigint
  AND submit.target_ref=f.id AND submit.operation='REFUND' AND submit.provider=p.provider AND submit.environment=p.environment
  AND submit.original_request_key=f.original_request_key AND submit.request_digest=NEW.intent_digest AND submit.state='INTENT'
  AND query_request.target_ref=f.id AND query_request.operation='QUERY' AND query_request.provider=p.provider AND query_request.environment=p.environment
  AND query_request.original_request_key=f.original_request_key AND query_request.request_digest=NEW.intent_digest AND query_request.state='INTENT'
  AND j.kind='RESOLVE_MACHINE_REFUND' AND j.target_ref=f.id AND j.dedupe_key='machine-refund:'||f.id
  AND j.state='PENDING' AND NEW.dispatched_at IS NULL AND NEW.query_generation=0 AND NEW.next_query_at IS NULL
  AND NEW.last_receipt_id IS NULL AND NEW.observed_at IS NULL) IS TRUE THEN
  RAISE EXCEPTION 'original refund intent does not match original paid transaction';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_refund_context_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.refund_machine_contexts
 FOR EACH ROW EXECUTE FUNCTION p11.guard_original_refund_context();

CREATE FUNCTION p11.guard_original_refund_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c p11.refund_machine_contexts%ROWTYPE; i jsonb; a jsonb; m jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original signed refund receipts are append only'; END IF;
 SELECT * INTO c FROM p11.refund_machine_contexts WHERE refund_id=NEW.refund_id;
 i:=convert_from(c.intent_raw,'UTF8')::jsonb; a:=convert_from(NEW.encrypted_raw,'UTF8')::jsonb; m:=a->'metadata';
 IF NOT (NEW.writer_xact=pg_current_xact_id() AND c.scope_hash=NEW.scope_hash AND c.intent_digest=NEW.intent_digest
  AND a->>'profile'='P11_ORIGINAL_REFUND_SIGNED_RECEIPT_1'
  AND m->'scope'=convert_from(c.scope_raw,'UTF8')::jsonb AND m->>'intent_hash'=c.intent_digest
  AND m->>'kind'=NEW.kind AND m->>'method'=NEW.method AND m->>'evidence_hash'=NEW.evidence_hash
  AND (m->>'amount_fen')::bigint=NEW.amount_fen AND NEW.amount_fen=(i->>'amount_fen')::bigint
  AND (m->>'cumulative_fen')::bigint=NEW.cumulative_fen AND NEW.cumulative_fen<=(i->>'collected_fen')::bigint
  AND (m->>'observed_at')::timestamptz=NEW.observed_at AND NEW.observed_at>=(i->>'created_at')::timestamptz
  AND octet_length(decode(a->>'nonce','base64'))=12
  AND octet_length(decode(a->>'ciphertext','base64')) BETWEEN 18 AND 65552) IS TRUE THEN
  RAISE EXCEPTION 'complete original encrypted signed refund receipt required';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER original_refund_receipt_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.refund_machine_receipts
 FOR EACH ROW EXECUTE FUNCTION p11.guard_original_refund_receipt();

CREATE FUNCTION p11.check_original_refund_state() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE f p11.refunds%ROWTYPE; c p11.refund_machine_contexts%ROWTYPE; r p11.refund_machine_receipts%ROWTYPE;
 i jsonb; n jsonb; prior jsonb; held numeric; consumed numeric; refund_ref p11.entity_id;
BEGIN
 n:=to_jsonb(NEW); prior:=NULL;
 IF TG_OP='UPDATE' THEN prior:=to_jsonb(OLD); END IF;
 IF TG_TABLE_NAME='refunds' THEN refund_ref:=n->>'id'; ELSE refund_ref:=n->>'refund_id'; END IF;
 SELECT * INTO c FROM p11.refund_machine_contexts WHERE refund_id=refund_ref;
 IF NOT FOUND THEN
  -- Historical/manual unknown holds remain holds. They cannot fabricate success.
  IF TG_TABLE_NAME='refunds' THEN
   IF n->>'state'='SUCCEEDED' AND EXISTS(SELECT 1 FROM p11.payment_machine_contexts WHERE payment_id=(n->>'payment_id')::p11.entity_id) THEN
    RAISE EXCEPTION 'Machine refund success needs its original context and signed query';
   END IF;
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO f FROM p11.refunds WHERE id=refund_ref;
 i:=convert_from(c.intent_raw,'UTF8')::jsonb;
 IF f.state='FAILED' OR NOT (
  (f.state='REQUESTED' AND c.query_generation=0 AND c.dispatched_at IS NULL AND c.next_query_at IS NULL AND c.last_receipt_id IS NULL) OR
  (f.state IN ('UNKNOWN','PROCESSING','SUCCEEDED') AND c.query_generation>=1
   AND c.dispatched_at>=(i->>'created_at')::timestamptz AND c.dispatched_at<(i->>'submit_before')::timestamptz
   AND ((f.state='SUCCEEDED' AND c.next_query_at IS NULL) OR
        (f.state IN ('UNKNOWN','PROCESSING') AND c.next_query_at>=c.dispatched_at+interval '10 seconds')))
 ) IS TRUE THEN RAISE EXCEPTION 'original refund dispatch/query state is inconsistent'; END IF;
 IF f.state='PROCESSING' AND c.last_receipt_id IS NULL THEN RAISE EXCEPTION 'processing requires a signed original receipt'; END IF;
 IF c.last_receipt_id IS NOT NULL THEN
  SELECT * INTO r FROM p11.refund_machine_receipts WHERE id=c.last_receipt_id;
  IF NOT (r.refund_id=f.id AND r.scope_hash=c.scope_hash AND r.intent_digest=c.intent_digest
   AND r.evidence_hash=c.last_evidence_hash AND r.observed_at<=c.observed_at) IS TRUE THEN
   RAISE EXCEPTION 'original refund state and archived receipt differ';
  END IF;
 END IF;
 IF f.state='SUCCEEDED' THEN
  IF NOT (r.kind='REFUND_CONFIRMED' AND r.method='alipay.trade.fastpay.refund.query') IS TRUE THEN
   RAISE EXCEPTION 'only the exact original signed refund query confirms success';
  END IF;
  IF TG_TABLE_NAME='refunds' THEN
   IF TG_OP='INSERT' THEN
    IF r.writer_xact<>pg_current_xact_id() THEN RAISE EXCEPTION 'new original refund success requires its own signed query transaction'; END IF;
   ELSIF prior->>'state'<>'SUCCEEDED' AND r.writer_xact<>pg_current_xact_id() THEN
    RAISE EXCEPTION 'original refund success and signed query must commit together';
   END IF;
  END IF;
 END IF;
 SELECT coalesce(sum(amount_fen::numeric),0) INTO held FROM p11.refunds WHERE payment_id=c.payment_id AND state<>'FAILED';
 SELECT budget_consumed_fen::numeric+budget_reserved_fen::numeric+budget_unverified_fen::numeric INTO consumed FROM p11.orders WHERE id=c.order_id;
 IF held+consumed>(i->>'collected_fen')::numeric THEN RAISE EXCEPTION 'original collected consideration is already held or earned'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER original_refund_context_state AFTER INSERT OR UPDATE ON p11.refund_machine_contexts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_original_refund_state();
CREATE CONSTRAINT TRIGGER original_refund_financial_state AFTER INSERT OR UPDATE ON p11.refunds
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_original_refund_state();

CREATE FUNCTION p11.guard_refund_order_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE held numeric;
BEGIN
 SELECT coalesce(sum(f.amount_fen::numeric),0) INTO held FROM p11.refunds f
 JOIN p11.payments p ON p.id=f.payment_id JOIN p11.payment_machine_contexts c ON c.payment_id=p.id
 WHERE f.order_id=NEW.id AND p.purpose='ORDER' AND f.state<>'FAILED';
 IF held>0 AND held+NEW.budget_consumed_fen::numeric+NEW.budget_reserved_fen::numeric+NEW.budget_unverified_fen::numeric>NEW.max_amount_fen::numeric THEN
  RAISE EXCEPTION 'original refund obligations cannot become execution capacity';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER refund_order_capacity_guard BEFORE UPDATE OF budget_consumed_fen,budget_reserved_fen,budget_unverified_fen ON p11.orders
 FOR EACH ROW EXECUTE FUNCTION p11.guard_refund_order_capacity();
