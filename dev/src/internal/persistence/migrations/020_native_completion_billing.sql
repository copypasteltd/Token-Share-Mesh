-- R026 CODE_FIRST / SOURCE_ONLY, not executed. Published 001-019 unchanged.
-- Metadata only; no content, transferable balance, payout or trusted meter key.
ALTER TABLE p11.acceptances ADD COLUMN native_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED';
ALTER TABLE p11.acceptances ADD COLUMN decision_version p11.counter;
ALTER TABLE p11.acceptances ADD COLUMN request_digest p11.sha256_hex;
ALTER TABLE p11.acceptances ADD CONSTRAINT native_acceptance_profile CHECK(
 native_profile='LEGACY_UNVERIFIED' OR (native_profile='P11_BUYER_RESULT_DECISION_1'
 AND decision_version IS NOT NULL AND decision_version>0 AND request_digest IS NOT NULL));

CREATE TABLE p11.native_result_deliveries (
 delivery_id p11.entity_id PRIMARY KEY REFERENCES p11.deliveries(id),
 job_id p11.entity_id NOT NULL REFERENCES p11.jobs(id),
 attempt_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.attempts(id),
 lease_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.execution_leases(id),
 payload_raw bytea NOT NULL CHECK(octet_length(payload_raw) BETWEEN 1 AND 16384),
 payload_digest p11.sha256_hex NOT NULL CHECK(payload_digest=encode(sha256(payload_raw),'hex')),
 output_digest p11.sha256_hex NOT NULL,
 output_bytes bigint NOT NULL CHECK(output_bytes BETWEEN 0 AND 33554432),
 decision_deadline timestamptz NOT NULL,
 safe_response jsonb NOT NULL CHECK(jsonb_typeof(safe_response)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(delivery_id,job_id,attempt_id) REFERENCES p11.deliveries(id,job_id,attempt_id),
 FOREIGN KEY(attempt_id,job_id) REFERENCES p11.attempts(id,job_id),
 UNIQUE(delivery_id,payload_digest)
);
CREATE TABLE p11.native_usage_verifications (
 usage_id p11.entity_id PRIMARY KEY REFERENCES p11.usage_evidence(id),
 job_id p11.entity_id NOT NULL REFERENCES p11.jobs(id),
 attempt_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.attempts(id),
 delivery_digest p11.sha256_hex NOT NULL,
 statement_raw bytea NOT NULL CHECK(octet_length(statement_raw) BETWEEN 1 AND 16384),
 statement_digest p11.sha256_hex NOT NULL CHECK(statement_digest=encode(sha256(statement_raw),'hex')),
 authority_evidence_id p11.entity_id NOT NULL REFERENCES p11.evidence(id),
 authority_evidence_digest p11.sha256_hex NOT NULL,
 meter_key_id p11.entity_id NOT NULL,
 authority_id p11.entity_id NOT NULL REFERENCES p11.users(id),
 reviewer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
 input_tokens bigint NOT NULL CHECK(input_tokens BETWEEN 1 AND 2147483647),
 output_tokens bigint NOT NULL CHECK(output_tokens BETWEEN 0 AND 2147483647),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(authority_id<>reviewer_id),
 FOREIGN KEY(attempt_id,job_id) REFERENCES p11.attempts(id,job_id)
);
CREATE TABLE p11.accepted_usage_settlements (
 id p11.entity_id PRIMARY KEY,
 job_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.jobs(id),
 order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
 acceptance_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.acceptances(id),
 usage_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.native_usage_verifications(usage_id),
 qualification_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.trade_qualifications(id),
 journal_id p11.entity_id UNIQUE,
 input_tokens bigint NOT NULL CHECK(input_tokens BETWEEN 1 AND 2147483647),
 output_tokens bigint NOT NULL CHECK(output_tokens BETWEEN 0 AND 2147483647),
 gross_fen p11.money_fen NOT NULL,
 commission_fen p11.money_fen NOT NULL,
 provisional_seller_fen p11.money_fen NOT NULL,
 channel_cost_state text NOT NULL DEFAULT 'UNKNOWN' CHECK(channel_cost_state='UNKNOWN'),
 payout_state text NOT NULL DEFAULT 'NOT_QUALIFIED' CHECK(payout_state='NOT_QUALIFIED'),
 evidence_hash p11.sha256_hex NOT NULL,
 safe_outcome jsonb NOT NULL CHECK(jsonb_typeof(safe_outcome)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(gross_fen::numeric=commission_fen::numeric+provisional_seller_fen::numeric),
 CHECK((gross_fen=0 AND journal_id IS NULL) OR (gross_fen>0 AND journal_id IS NOT NULL)),
 FOREIGN KEY(journal_id,order_id) REFERENCES p11.journal_transactions(id,order_id)
);
CREATE INDEX accepted_usage_order_watermark ON p11.accepted_usage_settlements(order_id,created_at,id);
CREATE TRIGGER native_result_append_only BEFORE UPDATE OR DELETE ON p11.native_result_deliveries FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE TRIGGER native_usage_append_only BEFORE UPDATE OR DELETE ON p11.native_usage_verifications FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE TRIGGER accepted_usage_append_only BEFORE UPDATE OR DELETE ON p11.accepted_usage_settlements FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE FUNCTION p11.guard_native_result_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d p11.deliveries%ROWTYPE; a p11.attempts%ROWTYPE; l p11.execution_leases%ROWTYPE; o p11.orders%ROWTYPE; j p11.jobs%ROWTYPE; b jsonb; s jsonb;
BEGIN
 SELECT * INTO STRICT d FROM p11.deliveries WHERE id=NEW.delivery_id;
 SELECT * INTO STRICT a FROM p11.attempts WHERE id=NEW.attempt_id;
 SELECT * INTO STRICT l FROM p11.execution_leases WHERE id=NEW.lease_id;
 SELECT * INTO STRICT o FROM p11.orders WHERE id=a.order_id;
 SELECT * INTO STRICT j FROM p11.jobs WHERE id=a.job_id;
 b:=convert_from(NEW.payload_raw,'UTF8')::jsonb;s:=b->'scope';
 IF NOT (d.job_id=NEW.job_id AND d.attempt_id=a.id AND l.attempt_id=a.id AND l.job_id=NEW.job_id
 AND d.revision=1 AND d.manifest_hash=NEW.payload_digest AND cardinality(d.asset_ids)=0
 AND b->>'profile_id'='P11_NATIVE_RESULT_DELIVERY_1' AND b->>'revision'='1'
 AND b->>'output_digest'=NEW.output_digest AND (b->>'output_bytes')::bigint=NEW.output_bytes
 AND LEAST((b->>'completed_at')::timestamptz+interval '120 seconds',j.deadline)=NEW.decision_deadline
 AND l.execution_manifest->>'mode'='PLAINTEXT' AND l.execution_manifest->>'privacy'='STANDARD'
 AND s=jsonb_build_object('job_id',a.job_id,'attempt_id',a.id,'lease_id',l.id,'order_id',a.order_id,
 'seller_id',a.seller_id,'device_id',a.device_id,'resource_id',a.resource_id,'resource_lock_generation',a.resource_lock_generation::text,
 'fencing_token',a.fencing_token::text,'contract_hash',o.contract_hash,'execution_manifest_hash',l.execution_manifest_hash,
 'kernel_environment_digest',l.execution_manifest->>'kernel_environment_digest')) IS NOT TRUE THEN
  RAISE EXCEPTION 'native result must bind original signed execution';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_result_original_guard BEFORE INSERT ON p11.native_result_deliveries FOR EACH ROW EXECUTE FUNCTION p11.guard_native_result_delivery();

CREATE FUNCTION p11.guard_native_usage_verification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d p11.native_result_deliveries%ROWTYPE; u p11.usage_evidence%ROWTYPE; e p11.evidence%ROWTYPE; o p11.orders%ROWTYPE; b jsonb;
BEGIN
 SELECT * INTO STRICT d FROM p11.native_result_deliveries WHERE attempt_id=NEW.attempt_id;
 SELECT * INTO STRICT u FROM p11.usage_evidence WHERE id=NEW.usage_id;
 SELECT * INTO STRICT e FROM p11.evidence WHERE id=NEW.authority_evidence_id FOR SHARE;
 SELECT o1.* INTO STRICT o FROM p11.orders o1 JOIN p11.jobs j ON j.order_id=o1.id WHERE j.id=NEW.job_id;
 b:=convert_from(NEW.statement_raw,'UTF8')::jsonb;
 IF NOT (d.job_id=NEW.job_id AND d.payload_digest=NEW.delivery_digest AND u.job_id=NEW.job_id AND u.attempt_id=NEW.attempt_id
 AND u.status='VERIFIED' AND u.sequence=1 AND u.evidence_hash=NEW.statement_digest
 AND u.input_tokens=NEW.input_tokens AND u.output_tokens=NEW.output_tokens
 AND b->>'profile_id'='P11_INDEPENDENT_TOKEN_USAGE_1' AND b->>'delivery_digest'=d.payload_digest
 AND b->>'meter_key_id'=NEW.meter_key_id AND b->>'authority_evidence_ref'=e.id
 AND b->'scope'=convert_from(d.payload_raw,'UTF8')::jsonb->'scope'
 AND e.evidence_type='TOKEN_METER_AUTHORITY_1' AND e.decision='APPROVED' AND e.digest=NEW.authority_evidence_digest
 AND e.reviewed_by=NEW.reviewer_id AND e.valid_from<=clock_timestamp() AND e.valid_until>clock_timestamp()
 AND NEW.authority_id NOT IN(o.buyer_id,o.seller_id) AND NEW.reviewer_id NOT IN(o.buyer_id,o.seller_id)
 AND e.scope_snapshot->>'authority_id'=NEW.authority_id AND e.scope_snapshot->>'key_id'=NEW.meter_key_id) IS NOT TRUE THEN
  RAISE EXCEPTION 'native usage requires original independent approved authority';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_usage_original_guard BEFORE INSERT ON p11.native_usage_verifications FOR EACH ROW EXECUTE FUNCTION p11.guard_native_usage_verification();
CREATE FUNCTION p11.protect_native_usage_parent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM p11.native_usage_verifications WHERE usage_id=OLD.id) THEN RAISE EXCEPTION 'verified native usage is immutable'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;RETURN NEW;
END $$;
CREATE TRIGGER native_usage_parent_guard BEFORE UPDATE OR DELETE ON p11.usage_evidence FOR EACH ROW EXECUTE FUNCTION p11.protect_native_usage_parent();

CREATE FUNCTION p11.guard_native_acceptance() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j p11.jobs%ROWTYPE; d p11.deliveries%ROWTYPE; n p11.native_result_deliveries%ROWTYPE; buyer text;
BEGIN
 IF TG_OP<>'INSERT' THEN
  IF OLD.native_profile='P11_BUYER_RESULT_DECISION_1' OR EXISTS(SELECT 1 FROM p11.native_result_deliveries WHERE delivery_id=OLD.delivery_id) THEN RAISE EXCEPTION 'original native decision is immutable'; END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  IF NEW.native_profile<>'LEGACY_UNVERIFIED' THEN RAISE EXCEPTION 'legacy decision cannot be promoted to native'; END IF;RETURN NEW;
 END IF;
 SELECT * INTO n FROM p11.native_result_deliveries WHERE delivery_id=NEW.delivery_id;
 IF NOT FOUND THEN
  IF NEW.native_profile<>'LEGACY_UNVERIFIED' THEN RAISE EXCEPTION 'native decision requires original native result'; END IF;RETURN NEW;
 END IF;
 SELECT * INTO STRICT j FROM p11.jobs WHERE id=NEW.job_id;
 SELECT * INTO STRICT d FROM p11.deliveries WHERE id=NEW.delivery_id;
 SELECT buyer_id INTO STRICT buyer FROM p11.orders WHERE id=j.order_id;
 IF NOT (NEW.native_profile='P11_BUYER_RESULT_DECISION_1' AND NEW.decision_version=j.version AND NEW.request_digest IS NOT NULL
 AND NEW.signer_id=buyer AND j.state='WAITING_BUYER' AND d.state='WAITING_BUYER'
 AND j.current_attempt_id=NEW.attempt_id AND n.job_id=j.id AND n.attempt_id=NEW.attempt_id AND n.decision_deadline>clock_timestamp()) IS NOT TRUE THEN
  RAISE EXCEPTION 'native buyer decision lost original version or deadline race';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_acceptance_original_guard BEFORE INSERT OR UPDATE OR DELETE ON p11.acceptances FOR EACH ROW EXECUTE FUNCTION p11.guard_native_acceptance();

CREATE FUNCTION p11.check_accepted_usage_billing() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s p11.accepted_usage_settlements%ROWTYPE; valid boolean;
BEGIN
 SELECT * INTO STRICT s FROM p11.accepted_usage_settlements WHERE id=NEW.id;
 SELECT EXISTS(SELECT 1 FROM p11.jobs j JOIN p11.orders o ON o.id=j.order_id
 JOIN p11.acceptances a ON a.id=s.acceptance_id AND a.job_id=j.id
 JOIN p11.native_result_deliveries d ON d.delivery_id=a.delivery_id AND d.attempt_id=j.current_attempt_id
 JOIN p11.native_usage_verifications u ON u.usage_id=s.usage_id AND u.attempt_id=j.current_attempt_id AND u.delivery_digest=d.payload_digest
 JOIN p11.trade_qualifications q ON q.id=s.qualification_id AND q.order_id=o.id
 JOIN p11.budget_reservations b ON b.order_id=o.id AND b.purpose_ref=j.id
 WHERE j.id=s.job_id AND o.id=s.order_id AND j.state='ACCEPTED' AND j.accepted_delivery_id=d.delivery_id
 AND a.decision='ACCEPT' AND a.native_profile='P11_BUYER_RESULT_DECISION_1' AND a.signer_id=o.buyer_id
 AND q.source_type='ACCEPTANCE' AND q.source_id=a.id AND q.gross_fen=s.gross_fen AND q.evidence_hash=s.evidence_hash
 AND b.qualification_id=q.id AND b.consumed_fen=s.gross_fen AND b.state=CASE WHEN s.gross_fen=0 THEN 'RELEASED' ELSE 'CONSUMED' END
 AND u.input_tokens=s.input_tokens AND u.output_tokens=s.output_tokens
 AND EXISTS(SELECT 1 FROM p11.order_fundings f JOIN p11.payments p ON p.id=f.payment_id WHERE f.order_id=o.id AND f.buyer_id=o.buyer_id AND f.seller_id=o.seller_id AND f.contract_hash=o.contract_hash AND p.state='SETTLED')
 AND NOT EXISTS(SELECT 1 FROM p11.refunds rf WHERE rf.order_id=o.id AND rf.state<>'FAILED')
 AND (s.journal_id IS NULL OR EXISTS(SELECT 1 FROM p11.journal_transactions t WHERE t.id=s.journal_id AND t.order_id=o.id AND t.source_type='P11_ACCEPTED_USAGE_1' AND t.source_id=q.id))
 ) INTO valid;
 IF NOT valid THEN RAISE EXCEPTION 'accepted usage, original budget and append-only journal must commit together'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER accepted_usage_consistent AFTER INSERT ON p11.accepted_usage_settlements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_accepted_usage_billing();

CREATE FUNCTION p11.protect_native_completion_terminal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.invocation_profile='ROUTE_PREPARE_1' AND OLD.state IN('ACCEPTED','FAILED','CANCELED') AND NEW.state IS DISTINCT FROM OLD.state THEN
  RAISE EXCEPTION 'native original terminal decision cannot be replaced';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_completion_terminal_guard BEFORE UPDATE ON p11.jobs FOR EACH ROW EXECUTE FUNCTION p11.protect_native_completion_terminal();
CREATE FUNCTION p11.protect_native_delivery_state() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j p11.jobs%ROWTYPE; allowed boolean;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM p11.native_result_deliveries WHERE delivery_id=OLD.id) THEN RETURN NEW; END IF;
 IF (to_jsonb(OLD)-'state') IS DISTINCT FROM (to_jsonb(NEW)-'state') THEN RAISE EXCEPTION 'native result manifest is immutable'; END IF;
 IF OLD.state=NEW.state THEN RETURN NEW; END IF;
 SELECT * INTO STRICT j FROM p11.jobs WHERE id=OLD.job_id;
 allowed:=OLD.state='WAITING_BUYER' AND (
  (NEW.state='VALID' AND j.state='ACCEPTED' AND j.accepted_delivery_id=OLD.id AND EXISTS(SELECT 1 FROM p11.acceptances a WHERE a.job_id=j.id AND a.delivery_id=OLD.id AND a.decision='ACCEPT' AND a.native_profile='P11_BUYER_RESULT_DECISION_1'))
  OR (NEW.state='INVALID' AND j.state='FAILED' AND EXISTS(SELECT 1 FROM p11.acceptances a WHERE a.job_id=j.id AND a.delivery_id=OLD.id AND a.decision='REJECT' AND a.native_profile='P11_BUYER_RESULT_DECISION_1'))
  OR (NEW.state='STALE' AND (j.state IN('CANCELED','FAILED') OR j.current_attempt_id<>OLD.attempt_id OR EXISTS(SELECT 1 FROM p11.native_result_deliveries n WHERE n.delivery_id=OLD.id AND n.decision_deadline<=clock_timestamp())))
 );
 IF allowed IS NOT TRUE THEN RAISE EXCEPTION 'native delivery requires an original terminal decision or timeout'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_delivery_state_guard BEFORE UPDATE ON p11.deliveries FOR EACH ROW EXECUTE FUNCTION p11.protect_native_delivery_state();

CREATE FUNCTION p11.check_native_trade_qualification() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE native_accept boolean; native_usage boolean;
BEGIN
 IF NEW.source_type='ACCEPTANCE' THEN
  SELECT EXISTS(SELECT 1 FROM p11.acceptances a JOIN p11.native_result_deliveries d ON d.delivery_id=a.delivery_id WHERE a.id=NEW.source_id) INTO native_accept;
  IF native_accept AND NOT EXISTS(SELECT 1 FROM p11.accepted_usage_settlements s WHERE s.qualification_id=NEW.id AND s.acceptance_id=NEW.source_id AND s.order_id=NEW.order_id AND s.gross_fen=NEW.gross_fen) THEN
   RAISE EXCEPTION 'native acceptance without independent usage is not a trade qualification';
  END IF;
 ELSIF NEW.source_type='VERIFIED_USAGE' THEN
  SELECT EXISTS(SELECT 1 FROM p11.usage_evidence u JOIN p11.jobs j ON j.id=u.job_id WHERE u.id=NEW.source_id AND j.invocation_profile='ROUTE_PREPARE_1') INTO native_usage;
  IF native_usage THEN RAISE EXCEPTION 'native text usage requires unique original buyer acceptance before qualification'; END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER native_trade_qualification_guard AFTER INSERT ON p11.trade_qualifications DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_native_trade_qualification();
