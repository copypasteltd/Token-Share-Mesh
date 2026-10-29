package outbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"time"
)

type Idempotency struct{ DB *persistence.DB }
type StoredResult struct {
	Reference  string
	Body       json.RawMessage
	HTTPStatus int
	Replayed   bool
}
type Command struct {
	Principal, Operation, Resource, Key string
	PrincipalVersion                    int64
	Request                             []byte
}

func Digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// Do commits the business mutation and safe replay metadata together. The
// callback is SQL-only: channel requests and inference must use persisted intents.
func (s Idempotency) Do(ctx context.Context, c Command, fn func(pgx.Tx) (StoredResult, error)) (StoredResult, error) {
	return s.DoAuthorized(ctx, c, nil, fn)
}

// DoAuthorized rechecks operation-specific authority inside the same transaction
// even on an idempotent replay. Role revocation cannot be bypassed by old keys.
func (s Idempotency) DoAuthorized(ctx context.Context, c Command, authorize func(pgx.Tx) error, fn func(pgx.Tx) (StoredResult, error)) (StoredResult, error) {
	var result StoredResult
	if !ids.Valid(c.Principal) || c.PrincipalVersion < 1 || c.Operation == "" || c.Resource == "" || len(c.Key) < 8 || len(c.Key) > 128 {
		return result, fault.New(fault.Invalid, "Invalid idempotency scope or key")
	}
	for _, r := range c.Key {
		if r < 33 || r > 126 {
			return result, fault.New(fault.Invalid, "Invalid idempotency key characters")
		}
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var state string
		var version int64
		if err := tx.QueryRow(ctx, `SELECT state,version FROM p11.users WHERE id=$1 FOR SHARE`, c.Principal).Scan(&state, &version); err != nil {
			if err == pgx.ErrNoRows {
				return fault.New(fault.Unauthenticated, "Invalid principal")
			}
			return err
		}
		if state != "ACTIVE" || version != c.PrincipalVersion {
			return fault.New(fault.Unauthenticated, "Principal no longer valid")
		}
		if authorize != nil {
			if err := authorize(tx); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO p11.idempotency_records(principal_id,operation_id,resource_scope,key_digest,request_digest,state,expires_at)
VALUES($1,$2,$3,$4,$5,'PENDING',clock_timestamp()+interval '7 days') ON CONFLICT DO NOTHING`, c.Principal, c.Operation, c.Resource, Digest([]byte(c.Key)), Digest(c.Request))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var digest, state string
			var ref *string
			var status *int
			err = tx.QueryRow(ctx, `SELECT request_digest,state,response_reference,safe_response,response_http_status FROM p11.idempotency_records
WHERE principal_id=$1 AND operation_id=$2 AND resource_scope=$3 AND key_digest=$4 FOR UPDATE`, c.Principal, c.Operation, c.Resource, Digest([]byte(c.Key))).Scan(&digest, &state, &ref, &result.Body, &status)
			if err != nil {
				return err
			}
			if digest != Digest(c.Request) {
				return fault.New(fault.Conflict, "Idempotency key reused with different input")
			}
			if state != "SUCCEEDED" || ref == nil || status == nil {
				return fault.New(fault.Conflict, "Original operation requires recovery; do not create a new purchase")
			}
			result.Reference = *ref
			result.HTTPStatus = *status
			result.Replayed = true
			return nil
		}
		result, err = fn(tx)
		if err != nil {
			return err
		}
		var object map[string]json.RawMessage
		if !ids.Valid(result.Reference) || result.HTTPStatus < 200 || result.HTTPStatus >= 300 || len(result.Body) > 65536 || json.Unmarshal(result.Body, &object) != nil || object == nil {
			return fault.New(fault.Invalid, "Invalid safe idempotency result")
		}
		_, err = tx.Exec(ctx, `UPDATE p11.idempotency_records SET state='SUCCEEDED',response_reference=$5,safe_response=$6,response_http_status=$7
WHERE principal_id=$1 AND operation_id=$2 AND resource_scope=$3 AND key_digest=$4`, c.Principal, c.Operation, c.Resource, Digest([]byte(c.Key)), result.Reference, []byte(result.Body), result.HTTPStatus)
		return err
	})
	return result, err
}

// RetryDelay is bounded and independent of wall-clock time. Unknown financial
// creates are not retried; their dedicated query jobs retain the original key.
func RetryDelay(tries int) time.Duration {
	if tries < 1 {
		tries = 1
	}
	if tries > 9 {
		tries = 9
	}
	return time.Second * time.Duration(1<<uint(tries-1))
}
