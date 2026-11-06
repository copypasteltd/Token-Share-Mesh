-- R026 CODE_FIRST / SOURCE_ONLY. Not executed. Published 001-018 unchanged.
CREATE TABLE p11.refund_applications (
 id p11.entity_id PRIMARY KEY,
 order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
 payment_id p11.entity_id NOT NULL,
 buyer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
 seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
 contract_hash p11.sha256_hex NOT NULL,
 request_raw bytea NOT NULL CHECK(octet_length(request_raw) BETWEEN 1 AND 16384),
 request_digest p11.sha256_hex NOT NULL CHECK(request_digest=encode(sha256(request_raw),'hex')),
 requested_fen p11.money_fen NOT NULL CHECK(requested_fen BETWEEN 1 AND 5000),
 reason_code text NOT NULL CHECK(reason_code IN ('NO_LONGER_NEEDED','SERVICE_UNAVAILABLE','BILLING_REVIEW')),
 evidence_refs jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(evidence_refs)='array' AND jsonb_array_length(evidence_refs)<=50),
 review_state text NOT NULL DEFAULT 'PENDING' CHECK(review_state IN ('PENDING','APPROVED','REJECTED','REVOKED')),
 review_version p11.counter NOT NULL DEFAULT 1 CHECK(review_version BETWEEN 1 AND 2147483647),
 review_reason_code text,
 reviewed_by p11.entity_id REFERENCES p11.users(id),
 revoked_by p11.entity_id REFERENCES p11.users(id),
 decision_evidence_id p11.entity_id UNIQUE REFERENCES p11.evidence(id),
 decision_digest p11.sha256_hex,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
 CHECK((decision_evidence_id IS NULL)=(decision_digest IS NULL)),
 CHECK((review_state='PENDING' AND review_version=1 AND reviewed_by IS NULL AND review_reason_code IS NULL AND decision_evidence_id IS NULL AND revoked_by IS NULL) OR
       (review_state='REJECTED' AND reviewed_by IS NOT NULL AND review_reason_code IS NOT NULL AND decision_evidence_id IS NULL AND revoked_by IS NULL) OR
       (review_state='APPROVED' AND reviewed_by IS NOT NULL AND review_reason_code='UNUSED_BALANCE_CONFIRMED' AND decision_evidence_id IS NOT NULL AND revoked_by IS NULL) OR
       (review_state='REVOKED' AND reviewed_by IS NOT NULL AND review_reason_code='AUTHORITY_REVOKED' AND decision_evidence_id IS NOT NULL AND revoked_by IS NOT NULL)),
 CHECK(reviewed_by IS NULL OR (reviewed_by<>buyer_id AND reviewed_by<>seller_id)),
 CHECK(revoked_by IS NULL OR (revoked_by<>buyer_id AND revoked_by<>seller_id)),
 CHECK(updated_at>=created_at)
);
CREATE UNIQUE INDEX refund_application_one_pending ON p11.refund_applications(order_id) WHERE review_state='PENDING';
CREATE INDEX refund_application_order_page ON p11.refund_applications(order_id,id);
CREATE INDEX refund_application_review_page ON p11.refund_applications(id,buyer_id,seller_id);

CREATE FUNCTION p11.guard_refund_application() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o p11.orders%ROWTYPE; p p11.payments%ROWTYPE; i jsonb; actor p11.entity_id; ref jsonb;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original refund application cannot be deleted'; END IF;
 SELECT * INTO o FROM p11.orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO p FROM p11.payments WHERE id=NEW.payment_id FOR UPDATE;
 IF TG_OP='INSERT' THEN
  i:=convert_from(NEW.request_raw,'UTF8')::jsonb;
  IF NOT (o.id=p.order_id AND p.purpose='ORDER' AND p.purpose_ref=o.id AND p.state='SETTLED'
   AND EXISTS(SELECT 1 FROM p11.payment_machine_contexts WHERE payment_id=p.id)
   AND NEW.buyer_id=o.buyer_id AND NEW.seller_id=o.seller_id AND NEW.contract_hash=o.contract_hash
   AND NEW.requested_fen<=p.amount_fen AND (i->>'requested_fen')::bigint=NEW.requested_fen
   AND i->>'reason_code'=NEW.reason_code AND (i->>'expected_order_version')::bigint=o.version
   AND coalesce(i->'evidence_refs','[]'::jsonb)=NEW.evidence_refs AND NEW.review_state='PENDING'
   AND NEW.created_at=NEW.updated_at) IS TRUE THEN
   RAISE EXCEPTION 'original refund request and original paid contract differ';
  END IF;
  FOR ref IN SELECT value FROM jsonb_array_elements(NEW.evidence_refs) LOOP
   IF NOT EXISTS(SELECT 1 FROM p11.evidence e WHERE e.id=ref#>>'{}' AND e.scope_snapshot @> jsonb_build_object('order_id',o.id,'buyer_id',o.buyer_id)) THEN
    RAISE EXCEPTION 'refund evidence must belong to the original buyer and order';
   END IF;
  END LOOP;
  RETURN NEW;
 END IF;
 IF (to_jsonb(NEW)-ARRAY['review_state','review_version','review_reason_code','reviewed_by','revoked_by','decision_evidence_id','decision_digest','updated_at']) IS DISTINCT FROM
    (to_jsonb(OLD)-ARRAY['review_state','review_version','review_reason_code','reviewed_by','revoked_by','decision_evidence_id','decision_digest','updated_at']) OR
    NEW.review_version<>OLD.review_version+1 OR NEW.updated_at<OLD.updated_at THEN
  RAISE EXCEPTION 'original refund request is immutable and review version cannot rewind';
 END IF;
 IF OLD.review_state='PENDING' AND NEW.review_state IN ('APPROVED','REJECTED') THEN
  actor:=NEW.reviewed_by;
 ELSIF OLD.review_state='APPROVED' AND NEW.review_state='REVOKED' AND NEW.reviewed_by=OLD.reviewed_by
   AND NEW.decision_evidence_id=OLD.decision_evidence_id AND NEW.decision_digest=OLD.decision_digest THEN
  actor:=NEW.revoked_by;
 ELSE RAISE EXCEPTION 'original refund review transition not allowed'; END IF;
 IF NOT EXISTS(SELECT 1 FROM p11.users u JOIN p11.role_grants g ON g.user_id=u.id
  WHERE u.id=actor AND u.state='ACTIVE' AND u.id<>NEW.buyer_id AND u.id<>NEW.seller_id
  AND g.role_code='PAYMENT_REVIEWER' AND g.scope_ref='self' AND g.revoked_at IS NULL
  AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'current independent financial reviewer required';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER refund_application_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.refund_applications
 FOR EACH ROW EXECUTE FUNCTION p11.guard_refund_application();

CREATE FUNCTION p11.check_refund_application_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a p11.refund_applications%ROWTYPE; e p11.evidence%ROWTYPE;
 c p11.refund_machine_contexts%ROWTYPE; f p11.refunds%ROWTYPE; application_ref p11.entity_id;
BEGIN
 IF TG_TABLE_NAME='evidence' THEN
  SELECT id INTO application_ref FROM p11.refund_applications WHERE decision_evidence_id=NEW.id;
  IF NOT FOUND THEN RETURN NEW; END IF;
 ELSIF TG_TABLE_NAME='refund_machine_contexts' THEN application_ref:=NEW.refund_id;
 ELSE application_ref:=NEW.id; END IF;
 SELECT * INTO a FROM p11.refund_applications WHERE id=application_ref;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF a.review_state IN ('PENDING','REJECTED') THEN
  IF EXISTS(SELECT 1 FROM p11.refunds WHERE id=a.id) THEN RAISE EXCEPTION 'unapproved application cannot create a financial refund'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO e FROM p11.evidence WHERE id=a.decision_evidence_id;
 SELECT * INTO c FROM p11.refund_machine_contexts WHERE refund_id=a.id;
 SELECT * INTO f FROM p11.refunds WHERE id=a.id;
 IF NOT (e.evidence_type='MACHINE_PAY_REFUND_DECISION_1' AND e.seller_id=a.seller_id AND e.device_id IS NULL
  AND e.digest=a.decision_digest AND e.reviewed_by=a.reviewed_by
  AND e.scope_snapshot->>'refund_application_id'=a.id AND e.scope_snapshot->>'application_request_hash'=a.request_digest
  AND e.scope_snapshot->>'order_id'=a.order_id AND e.scope_snapshot->>'payment_id'=a.payment_id
  AND e.scope_snapshot->>'buyer_id'=a.buyer_id AND e.scope_snapshot->>'contract_hash'=a.contract_hash
  AND e.scope_snapshot->>'reviewer_id'=a.reviewed_by AND e.scope_snapshot->>'refund_policy'='UNUSED_BALANCE_1'
  AND (e.scope_snapshot->>'refund_amount_fen')::bigint=a.requested_fen
  AND c.refund_id=a.id AND c.payment_id=a.payment_id AND c.order_id=a.order_id
  AND c.decision_evidence_id=e.id AND (convert_from(c.scope_raw,'UTF8')::jsonb)->>'decision_hash'=e.digest
  AND f.payment_id=a.payment_id AND f.order_id=a.order_id AND f.amount_fen=a.requested_fen
  AND ((a.review_state='APPROVED' AND e.decision='APPROVED') OR (a.review_state='REVOKED' AND e.decision='REVOKED'))) IS TRUE THEN
  RAISE EXCEPTION 'review approval and exact original financial intent must commit together';
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER refund_application_decision AFTER INSERT OR UPDATE ON p11.refund_applications
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_refund_application_decision();
CREATE CONSTRAINT TRIGGER refund_application_evidence_decision AFTER UPDATE ON p11.evidence
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_refund_application_decision();
CREATE CONSTRAINT TRIGGER refund_application_financial_decision AFTER INSERT OR UPDATE ON p11.refunds
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_refund_application_decision();
CREATE CONSTRAINT TRIGGER refund_application_context_decision AFTER INSERT OR UPDATE ON p11.refund_machine_contexts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_refund_application_decision();

CREATE FUNCTION p11.guard_linked_refund_review_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM p11.refund_applications WHERE decision_evidence_id=OLD.id) THEN
  IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
 END IF;
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'original refund approval evidence cannot be deleted'; END IF;
 IF (to_jsonb(NEW)-'decision') IS DISTINCT FROM (to_jsonb(OLD)-'decision') OR OLD.decision<>'APPROVED' OR NEW.decision<>'REVOKED' THEN
  RAISE EXCEPTION 'original refund approval is immutable except coordinated revocation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER linked_refund_review_evidence_guard BEFORE UPDATE OR DELETE ON p11.evidence
 FOR EACH ROW EXECUTE FUNCTION p11.guard_linked_refund_review_evidence();
