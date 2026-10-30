package outbox

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"time"
)

type Queue struct {
	DB       *persistence.DB
	MaxTries int
}
type Job struct {
	ID           string
	Kind         string
	Target       string
	Generation   int64
	Tries        int
	ClaimedUntil time.Time
}

// Only metadata/query/cleanup jobs are accepted. A payment in UNKNOWN is queried;
// financial creation and inference cannot be retried as generic background jobs.
var kinds = map[string]bool{"QUERY_PAYMENT": true, "QUERY_REFUND": true, "QUERY_SETTLEMENT": true, "DELETE_ASSET": true, "STOP_CLEANUP": true, "EXPIRE_CAPABILITY": true, "NOTIFY_METADATA": true}

// A character filter cannot make arbitrary upstream content safe to persist.
// Consumers must map failures to one of these fixed metadata-only codes.
var safeCodes = map[string]bool{"QUERY_UNAVAILABLE": true, "CHANNEL_PENDING": true, "CLEANUP_BUSY": true, "RESOURCE_UNAVAILABLE": true, "NOTIFY_UNAVAILABLE": true, "TEMPORARY_UNAVAILABLE": true}

func (q Queue) Enqueue(ctx context.Context, tx pgx.Tx, kind, target, dedupe string, runAt time.Time) (string, error) {
	if !kinds[kind] || !ids.Valid(target) || len(dedupe) < 8 || len(dedupe) > 256 || runAt.IsZero() {
		return "", fault.New(fault.Invalid, "Invalid durable job")
	}
	id := ids.New("bg")
	_, err := tx.Exec(ctx, `INSERT INTO p11.durable_jobs(id,kind,dedupe_key,target_ref,state,run_at) VALUES($1,$2,$3,$4,'PENDING',$5) ON CONFLICT(dedupe_key) DO NOTHING`, id, kind, dedupe, target, runAt)
	if err != nil {
		return "", err
	}
	var oldKind, oldTarget string
	err = tx.QueryRow(ctx, `SELECT id,kind,target_ref FROM p11.durable_jobs WHERE dedupe_key=$1`, dedupe).Scan(&id, &oldKind, &oldTarget)
	if err == nil && (oldKind != kind || oldTarget != target) {
		return "", fault.New(fault.Conflict, "Job dedupe key has different business scope")
	}
	return id, err
}

// Claim handles only this queue's allowlisted metadata kinds. Initial lease
// delivery intents use their own authenticated consumer and must not be claimed,
// completed or marked DEAD by a generic metadata worker.
func (q Queue) Claim(ctx context.Context, limit int, ttl time.Duration) ([]Job, error) {
	claimKinds := make([]string, 0, len(kinds))
	for kind := range kinds {
		claimKinds = append(claimKinds, kind)
	}
	jobs := []Job{}
	if q.DB == nil || q.MaxTries < 1 || q.MaxTries > 20 || limit < 1 || limit > 100 || ttl < time.Second || ttl > 5*time.Minute {
		return nil, fault.New(fault.Invalid, "Invalid job claim policy")
	}
	err := q.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE p11.durable_jobs SET state='DEAD',claimed_until=NULL,last_safe_error='CLAIM_EXHAUSTED' WHERE kind=ANY($2::text[]) AND tries>=$1 AND (state='PENDING' OR (state='CLAIMED' AND claimed_until<=clock_timestamp()))`, q.MaxTries, claimKinds)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `WITH candidates AS (
SELECT id FROM p11.durable_jobs WHERE kind=ANY($4::text[]) AND tries<$1 AND ((state='PENDING' AND run_at<=clock_timestamp()) OR (state='CLAIMED' AND claimed_until<=clock_timestamp())) ORDER BY run_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)
UPDATE p11.durable_jobs j SET state='CLAIMED',claim_generation=claim_generation+1,claimed_until=clock_timestamp()+$3::interval,tries=tries+1 FROM candidates c WHERE j.id=c.id RETURNING j.id,j.kind,j.target_ref,j.claim_generation,j.tries,j.claimed_until`, q.MaxTries, limit, fmt.Sprintf("%f seconds", ttl.Seconds()), claimKinds)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j Job
			if err = rows.Scan(&j.ID, &j.Kind, &j.Target, &j.Generation, &j.Tries, &j.ClaimedUntil); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, err
}
func (q Queue) Complete(ctx context.Context, j Job) error {
	if q.DB == nil || !ids.Valid(j.ID) || j.Generation < 1 {
		return fault.New(fault.Invalid, "Invalid job acknowledgement")
	}
	tag, err := q.DB.Pool.Exec(ctx, `UPDATE p11.durable_jobs SET state='DONE',claimed_until=NULL,last_safe_error=NULL WHERE id=$1 AND state='CLAIMED' AND claim_generation=$2 AND claimed_until>clock_timestamp()`, j.ID, j.Generation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fault.New(fault.Conflict, "Job claim no longer owned")
	}
	return nil
}
func (q Queue) Fail(ctx context.Context, j Job, code string) error {
	if q.DB == nil || q.MaxTries < 1 || q.MaxTries > 20 || !ids.Valid(j.ID) || j.Generation < 1 || !safeCodes[code] {
		return fault.New(fault.Invalid, "Expected a public-safe error code")
	}
	tag, err := q.DB.Pool.Exec(ctx, `UPDATE p11.durable_jobs SET state=CASE WHEN tries>=$3 THEN 'DEAD' ELSE 'PENDING' END,run_at=clock_timestamp()+make_interval(secs => power(2,least(greatest(tries,1),9)-1)::double precision),claimed_until=NULL,last_safe_error=$4 WHERE id=$1 AND state='CLAIMED' AND claim_generation=$2 AND claimed_until>clock_timestamp()`, j.ID, j.Generation, q.MaxTries, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fault.New(fault.Conflict, "Job claim no longer owned")
	}
	return nil
}

// ConsumeOnce places the inbox receipt and local SQL side effect in one
// transaction. A crashed transaction cannot leave a false DONE receipt.
func ConsumeOnce(ctx context.Context, db *persistence.DB, consumer, eventID string, payload []byte, fn func(pgx.Tx) error) (bool, error) {
	if !ids.Valid(consumer) || !ids.Valid(eventID) || len(payload) > 65536 {
		return false, fault.New(fault.Invalid, "Invalid event consumer scope")
	}
	duplicate := false
	err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO p11.inbox_events(consumer,event_id,payload_digest,state) VALUES($1,$2,$3,'CLAIMED') ON CONFLICT DO NOTHING`, consumer, eventID, Digest(payload))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var digest, state string
			if err = tx.QueryRow(ctx, `SELECT payload_digest,state FROM p11.inbox_events WHERE consumer=$1 AND event_id=$2 FOR UPDATE`, consumer, eventID).Scan(&digest, &state); err != nil {
				return err
			}
			if digest != Digest(payload) || state != "DONE" {
				return fault.New(fault.Conflict, "Event identity or receipt mismatch")
			}
			duplicate = true
			return nil
		}
		if err = fn(tx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE p11.inbox_events SET state='DONE',processed_at=clock_timestamp() WHERE consumer=$1 AND event_id=$2`, consumer, eventID)
		return err
	})
	return duplicate, err
}
