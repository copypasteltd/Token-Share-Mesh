-- PostgreSQL 17 query PATTERNS. Not a complete application transaction script.
-- sqlc-style named queries, explicit typed parameters; each query parses alone.
-- Application must check affected rows, deadlines, policy and lock order.

-- name: LockOrder :one
SELECT * FROM p11.orders WHERE id=$1::p11.entity_id FOR UPDATE;

-- name: LockJob :one
SELECT * FROM p11.jobs WHERE id=$1::p11.entity_id AND order_id=$2::p11.entity_id FOR UPDATE;

-- name: LockCandidateResource :one
SELECT rl.* FROM p11.resource_locks rl
JOIN p11.resources r ON r.id=rl.resource_id
WHERE rl.resource_id=$1::p11.entity_id AND r.seller_id=$2::p11.entity_id
  AND rl.phase='FREE'
FOR UPDATE OF rl NOWAIT;

-- name: OccupyResourceForAttempt :one
UPDATE p11.resource_locks SET lock_generation=lock_generation+1,phase='RESERVED',
  owner_attempt_id=$2::p11.entity_id,expires_at=$3::timestamptz,
  version=version+1,updated_at=now()
WHERE resource_id=$1::p11.entity_id AND phase='FREE' AND version=$4::p11.counter
RETURNING *;

-- name: IssuePreparedAttempt :one
UPDATE p11.attempts SET state='ISSUED',resource_lock_generation=$2::p11.counter,version=version+1
WHERE id=$1::p11.entity_id AND state='PREPARED' AND fencing_token=$3::p11.counter
  AND EXISTS(SELECT 1 FROM p11.resource_locks rl WHERE rl.resource_id=p11.attempts.resource_id
    AND rl.owner_attempt_id=p11.attempts.id AND rl.lock_generation=$2::p11.counter)
RETURNING *;

-- name: AcceptCurrentDelivery :one
UPDATE p11.jobs j SET state='ACCEPTED',accepted_delivery_id=$4::p11.entity_id,version=version+1
WHERE j.id=$1::p11.entity_id AND j.current_attempt_id=$2::p11.entity_id
  AND j.fencing_token=$3::p11.counter AND j.version=$5::p11.counter
  AND j.state IN ('VALIDATING','WAITING_BUYER') AND j.deadline>now()
  AND EXISTS(SELECT 1 FROM p11.deliveries d WHERE d.id=$4::p11.entity_id
    AND d.job_id=j.id AND d.attempt_id=j.current_attempt_id AND d.fencing_token=j.fencing_token
    AND d.state='VALID')
RETURNING j.*;
-- In the same TX-06 insert unique acceptance and TX-07 qualification/outbox.
-- E2E VALID must follow verified buyer signature; content is not scanned here.

-- name: CancelUnacceptedJob :one
UPDATE p11.jobs SET state='CANCELED',version=version+1
WHERE id=$1::p11.entity_id AND version=$2::p11.counter
  AND state IN ('INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER')
  AND accepted_delivery_id IS NULL
RETURNING *;
-- Preserve current owner/token until stop/cleanup; create revoke outbox in same TX.

-- name: LockCurrentLeaseForRenewal :one
SELECT l.* FROM p11.execution_leases l
JOIN p11.jobs j ON j.id=l.job_id AND j.current_attempt_id=l.attempt_id AND j.fencing_token=l.fencing_token
JOIN p11.resource_locks rl ON rl.resource_id=l.resource_id
  AND rl.owner_attempt_id=l.attempt_id AND rl.lock_generation=l.resource_lock_generation
WHERE l.id=$1::p11.entity_id AND l.lease_revision=$2::integer
  AND l.state IN ('ISSUED','ACKED') AND j.state='RUNNING'
  AND rl.phase='ACTIVE' AND l.expires_at>now() AND l.hard_deadline>now()
FOR UPDATE OF l;
-- Lock order/job/resource before this query. Derive full new Lease from these
-- locked facts, sign locally, update revision/expiry/signature atomically, and
-- persist safe ProgressAck under principal+operation+sequence idempotency key.
-- A replay returns its original signed revision; client rejects stale revisions.

-- name: LockPaymentForRefund :one
SELECT * FROM p11.payments WHERE id=$1::p11.entity_id AND order_id=$2::p11.entity_id FOR UPDATE;

-- name: ReservedRefundTotal :one
SELECT coalesce(sum(amount_fen::numeric),0)::numeric AS reserved_fen
FROM p11.refunds WHERE payment_id=$1::p11.entity_id
  AND state IN ('REQUESTED','PROCESSING','SUCCEEDED','UNKNOWN');
-- TX-11: while the original payment is locked, verify available channel-confirmed
-- refundable amount; then insert refund, channel intent and outbox. No provider IO.

-- name: ClaimOutboxBatch :many
WITH candidates AS (
  SELECT id FROM p11.outbox_events
  WHERE (state='PENDING' AND available_at<=now()) OR
        (state='CLAIMED' AND claimed_until<now())
  ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT $1::integer
)
UPDATE p11.outbox_events e SET state='CLAIMED',claim_generation=e.claim_generation+1,
  claimed_until=now()+$2::interval,tries=e.tries+1
FROM candidates c WHERE e.id=c.id RETURNING e.*;

-- name: CompleteClaimedOutbox :execrows
UPDATE p11.outbox_events SET state='DELIVERED',claimed_until=NULL
WHERE id=$1::p11.entity_id AND state='CLAIMED' AND claim_generation=$2::p11.counter;

-- name: ClaimDurableBatch :many
WITH candidates AS (
  SELECT id FROM p11.durable_jobs
  WHERE (state='PENDING' AND run_at<=now()) OR (state='CLAIMED' AND claimed_until<now())
  ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT $1::integer
)
UPDATE p11.durable_jobs j SET state='CLAIMED',claim_generation=j.claim_generation+1,
  claimed_until=now()+$2::interval,tries=j.tries+1
FROM candidates c WHERE j.id=c.id RETURNING j.*;

-- name: RejectOverlappingRentalInterval :one
SELECT EXISTS(SELECT 1 FROM p11.availability_intervals
  WHERE session_id=$1::p11.entity_id AND state='CONFIRMED'
    AND begins_at<$3::timestamptz AND ends_at>$2::timestamptz) AS overlaps;
-- TX-09 locks rental session first; validate monotonic evidence, rate, signed
-- deadline, generation and no overlap before inserting CONFIRMED interval.

-- name: ComputeRentalCharge :one
SELECT ceil(coalesce(sum(i.duration_ms::numeric),0)*s.rate_fen_per_minute::numeric/60000)::bigint AS amount_fen
FROM p11.rental_sessions s LEFT JOIN p11.availability_intervals i
  ON i.session_id=s.id AND i.state='CONFIRMED'
WHERE s.id=$1::p11.entity_id GROUP BY s.id;
-- Compare against signed term and funded budget; no per-interval rounding.

-- name: ReleaseCleanAttemptResource :execrows
UPDATE p11.resource_locks rl SET phase='FREE',owner_attempt_id=NULL,expires_at=NULL,
  version=version+1,updated_at=now()
WHERE rl.resource_id=$1::p11.entity_id AND rl.lock_generation=$2::p11.counter
  AND rl.owner_attempt_id=$3::p11.entity_id AND rl.phase='CLEANING'
  AND EXISTS(SELECT 1 FROM p11.attempts a WHERE a.id=rl.owner_attempt_id AND a.cleanup_state='ACKED');
-- ACKED must include actual backend stop + owned-file cleanup evidence.

-- name: FindOriginalUnknownRequests :many
SELECT * FROM p11.channel_requests WHERE state='UNKNOWN' AND next_query_at<=now()
ORDER BY next_query_at,id LIMIT $1::integer;
-- Query original request/reference only; never mint a new pay/refund/settlement key.

-- name: CalculateCommission :one
SELECT floor($1::numeric*$2::integer/10000)::bigint AS commission_fen;
-- Inputs are remaining valid gross and frozen bps; partial refunds recompute
-- cumulative entitlement and post only the delta, never independently floor parts.
