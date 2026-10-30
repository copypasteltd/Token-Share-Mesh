-- R014: order-local capacity accounting, never a wallet or payment authorization.
-- Existing 001-005 bytes and the native client DDL remain unchanged.
ALTER TABLE p11.orders
  ADD COLUMN budget_reserved_fen p11.money_fen NOT NULL DEFAULT 0,
  ADD COLUMN budget_consumed_fen p11.money_fen NOT NULL DEFAULT 0,
  ADD COLUMN budget_unverified_fen p11.money_fen NOT NULL DEFAULT 0,
  ADD CONSTRAINT order_budget_capacity CHECK
    (budget_reserved_fen::numeric+budget_consumed_fen::numeric+budget_unverified_fen::numeric<=max_amount_fen);

ALTER TABLE p11.budget_reservations
  ADD COLUMN accounting_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED'
    CHECK (accounting_profile IN ('LEGACY_UNVERIFIED','ORDER_CAPACITY_1')),
  ADD COLUMN purpose_kind text CHECK (purpose_kind IN ('JOB','RENTAL_SESSION')),
  ADD COLUMN consumed_fen p11.money_fen,
  ADD COLUMN qualification_id p11.entity_id REFERENCES p11.trade_qualifications(id) UNIQUE,
  ADD COLUMN resolved_at timestamptz,
  ADD CONSTRAINT budget_resolution_shape CHECK
    ((accounting_profile='LEGACY_UNVERIFIED' AND purpose_kind IS NULL AND consumed_fen IS NULL
      AND qualification_id IS NULL AND resolved_at IS NULL) OR
     (accounting_profile='ORDER_CAPACITY_1' AND purpose_kind IS NOT NULL AND
       ((state='RESERVED' AND consumed_fen IS NULL AND qualification_id IS NULL AND resolved_at IS NULL) OR
        (state='CONSUMED' AND consumed_fen IS NOT NULL AND consumed_fen>0 AND consumed_fen<=amount_fen AND qualification_id IS NOT NULL AND resolved_at IS NOT NULL) OR
        (state='RELEASED' AND consumed_fen IS NOT NULL AND consumed_fen=0 AND qualification_id IS NOT NULL AND resolved_at IS NOT NULL))));

-- Every legacy row, including old RELEASED/CONSUMED rows, remains fully held.
-- No actual usage or valid release evidence can be inferred from the old shape.
-- Inconsistent historical totals abort this transaction for explicit recovery.
UPDATE p11.orders o SET budget_unverified_fen=(
  SELECT coalesce(sum(b.amount_fen::numeric),0)
  FROM p11.budget_reservations b WHERE b.order_id=o.id);

CREATE FUNCTION p11.guard_order_budget_counters() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='INSERT' THEN
    IF NEW.budget_reserved_fen<>0 OR NEW.budget_consumed_fen<>0 OR NEW.budget_unverified_fen<>0 THEN
      RAISE EXCEPTION 'new order budget counters must be zero';
    END IF;
  ELSIF ROW(NEW.budget_reserved_fen,NEW.budget_consumed_fen,NEW.budget_unverified_fen)
    IS DISTINCT FROM ROW(OLD.budget_reserved_fen,OLD.budget_consumed_fen,OLD.budget_unverified_fen)
    AND pg_trigger_depth()<2 THEN
    RAISE EXCEPTION 'budget counters require a reservation transition';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER order_budget_counter_guard BEFORE INSERT OR UPDATE ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.guard_order_budget_counters();

-- Qualification is an append-only, independently produced billing fact.
-- This migration/service does not create one from relay usage or an HTTP EOF.
CREATE TRIGGER trade_qualification_immutable BEFORE UPDATE OR DELETE ON p11.trade_qualifications
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE FUNCTION p11.guard_budget_reservation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o p11.orders%ROWTYPE; q p11.trade_qualifications%ROWTYPE; linked boolean;
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'budget reservation is retained'; END IF;
  IF TG_OP='UPDATE' AND
    (OLD.accounting_profile<>'ORDER_CAPACITY_1' OR OLD.state<>'RESERVED' OR
     ROW(NEW.id,NEW.order_id,NEW.purpose_ref,NEW.amount_fen,NEW.accounting_profile,NEW.purpose_kind,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.purpose_ref,OLD.amount_fen,OLD.accounting_profile,OLD.purpose_kind,OLD.created_at)
     OR NEW.version<>OLD.version+1) THEN
    RAISE EXCEPTION 'budget binding or resolved outcome is immutable';
  END IF;
  SELECT * INTO STRICT o FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
  IF NEW.accounting_profile<>'ORDER_CAPACITY_1' OR NEW.purpose_kind IS NULL THEN
    RAISE EXCEPTION 'legacy budget requires independent recovery';
  END IF;
  IF NEW.purpose_kind='JOB' THEN
    SELECT EXISTS(SELECT 1 FROM p11.jobs j WHERE j.id=NEW.purpose_ref AND j.order_id=o.id
      AND j.seller_id=o.seller_id AND o.mode IN ('PLAINTEXT','E2E')) INTO linked;
  ELSIF NEW.purpose_kind='RENTAL_SESSION' THEN
    SELECT EXISTS(SELECT 1 FROM p11.rental_sessions s WHERE s.id=NEW.purpose_ref
      AND s.order_id=o.id AND s.seller_id=o.seller_id AND o.mode='GPU_RENTAL') INTO linked;
  ELSE linked:=false;
  END IF;
  IF NOT linked THEN RAISE EXCEPTION 'budget purpose outside original order'; END IF;
  IF TG_OP='INSERT' THEN
    IF NEW.state<>'RESERVED' OR NEW.version<>1 OR NEW.consumed_fen IS NOT NULL OR
      NEW.qualification_id IS NOT NULL OR NEW.resolved_at IS NOT NULL OR
      o.state NOT IN ('READY','FULFILLING') OR o.deadline<=clock_timestamp() THEN
      RAISE EXCEPTION 'new budget reservation is not eligible';
    END IF;
    IF NEW.purpose_kind='JOB' AND NOT EXISTS(SELECT 1 FROM p11.jobs
      WHERE id=NEW.purpose_ref AND state IN ('INPUT_PENDING','QUEUED')) THEN
      RAISE EXCEPTION 'started or terminal purpose requires original budget recovery';
    END IF;
    IF NEW.purpose_kind='RENTAL_SESSION' AND NOT EXISTS(SELECT 1 FROM p11.rental_sessions
      WHERE id=NEW.purpose_ref AND state IN ('REQUESTED','RESERVED','PREPARING','READY_WAIT_BUYER','ACTIVE')) THEN
      RAISE EXCEPTION 'closed rental purpose requires original budget recovery';
    END IF;
    UPDATE p11.orders SET budget_reserved_fen=budget_reserved_fen+NEW.amount_fen,version=version+1
      WHERE id=o.id AND budget_reserved_fen::numeric+budget_consumed_fen::numeric+
        budget_unverified_fen::numeric+NEW.amount_fen<=max_amount_fen;
    IF NOT FOUND THEN RAISE EXCEPTION 'order capacity exhausted'; END IF;
  ELSE
    IF NEW.state NOT IN ('CONSUMED','RELEASED') OR NEW.consumed_fen IS NULL OR
      NEW.consumed_fen>OLD.amount_fen OR NEW.qualification_id IS NULL OR NEW.resolved_at IS NULL THEN
      RAISE EXCEPTION 'a final budget outcome requires known qualified usage';
    END IF;
    SELECT * INTO STRICT q FROM p11.trade_qualifications
      WHERE id=NEW.qualification_id AND order_id=o.id FOR SHARE;
    IF q.gross_fen<>NEW.consumed_fen OR
      (q.gross_fen=0 AND NEW.state<>'RELEASED') OR (q.gross_fen>0 AND NEW.state<>'CONSUMED') THEN
      RAISE EXCEPTION 'budget amount does not match its qualification';
    END IF;
    linked:=false;
    IF NEW.purpose_kind='JOB' AND q.source_type='VERIFIED_USAGE' THEN
      SELECT EXISTS(SELECT 1 FROM p11.usage_evidence u JOIN p11.jobs j ON j.id=u.job_id
        WHERE u.id=q.source_id AND j.id=NEW.purpose_ref AND j.order_id=o.id
          AND u.status='VERIFIED' AND u.input_tokens IS NOT NULL AND u.output_tokens IS NOT NULL) INTO linked;
    ELSIF NEW.purpose_kind='JOB' AND q.source_type='ACCEPTANCE' THEN
      SELECT EXISTS(SELECT 1 FROM p11.acceptances a JOIN p11.jobs j ON j.id=a.job_id
        JOIN p11.deliveries d ON d.id=a.delivery_id AND d.job_id=j.id AND d.attempt_id=a.attempt_id
        WHERE a.id=q.source_id AND j.id=NEW.purpose_ref AND j.order_id=o.id AND a.decision='ACCEPT'
          AND a.signer_id=o.buyer_id AND d.state='VALID' AND j.state='ACCEPTED'
          AND j.accepted_delivery_id=a.delivery_id AND j.current_attempt_id=a.attempt_id) INTO linked;
    ELSIF NEW.purpose_kind='RENTAL_SESSION' AND q.source_type='FINAL_RENTAL_METER' THEN
      SELECT EXISTS(SELECT 1 FROM p11.meter_statements m JOIN p11.rental_sessions s ON s.id=m.session_id
        WHERE m.id=q.source_id AND s.id=NEW.purpose_ref AND s.order_id=o.id
          AND m.is_final AND m.amount_fen=q.gross_fen) INTO linked;
    END IF;
    IF NOT linked THEN RAISE EXCEPTION 'unverified or unrelated budget source'; END IF;
    UPDATE p11.orders SET budget_reserved_fen=budget_reserved_fen-OLD.amount_fen,
      budget_consumed_fen=budget_consumed_fen+NEW.consumed_fen,version=version+1 WHERE id=o.id;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER budget_reservation_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.budget_reservations
  FOR EACH ROW EXECUTE FUNCTION p11.guard_budget_reservation();
