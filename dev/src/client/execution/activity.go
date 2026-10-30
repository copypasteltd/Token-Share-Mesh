package execution

import (
	"context"
	"database/sql"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
	"time"
)

// Activity is internal supervisor metadata, not an execution grant or a public
// status response. Only a live in-memory guard can make Authorized true.
type Activity struct {
	Hold                                  Hold
	Phase, InboxPhase, BackendID          string
	Environment, Capability, ManifestHash string
	Authorized                            bool
	Remaining                             time.Duration
	Deadline                              time.Time
}

func (s *Store) activities(ctx context.Context, only *Hold) ([]Activity, error) {
	result := []Activity{}
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		rows, e := tx.QueryContext(ctx, `SELECT r.resource_id,r.owner_id,r.local_high,r.mode,r.phase,i.phase,i.backend_id,i.metadata_json FROM resources r LEFT JOIN inbox i ON i.lease_id=r.owner_id WHERE r.mode='JOB' AND r.owner_id<>'' ORDER BY r.resource_id`)
		if e != nil {
			return ErrStorage
		}
		defer rows.Close()
		for rows.Next() {
			var a Activity
			var raw string
			if rows.Scan(&a.Hold.resource, &a.Hold.owner, &a.Hold.local, &a.Hold.mode, &a.Phase, &a.InboxPhase, &a.BackendID, &raw) != nil {
				return ErrStorage
			}
			if only != nil && a.Hold != *only {
				continue
			}
			var l executionlease.Lease
			if strictjson.Decode([]byte(raw), &l, executionlease.MaximumBody) != nil || l.ID != a.Hold.owner || l.Resource != a.Hold.resource || !executionlease.Hash(l.Environment) || !executionlease.Hash(l.ManifestHash) {
				return ErrStorage
			}
			a.Environment, a.Capability, a.ManifestHash = l.Environment, l.Capability, l.ManifestHash
			if g, live := s.guards[l.ID]; live && !s.clockUnsafe && (a.Phase == "PREPARING" || a.Phase == "EXECUTING") {
				a.Remaining = min(g.expires.Sub(stamp.Wall), g.until-stamp.Elapsed)
				a.Deadline = s.sampledAt.Add(a.Remaining)
				if g.deadline.Before(a.Deadline) {
					a.Deadline = g.deadline
				}
				a.Authorized = a.Remaining > 0
			}
			result = append(result, a)
		}
		if rows.Err() != nil {
			return ErrStorage
		}
		if only != nil && len(result) != 1 {
			return ErrFence
		}
		return nil
	})
	// Commit may consume the whole remaining interval. Return no stale live
	// authority, while retaining the durable owner until actual cleanup.
	for i := range result {
		if result[i].Authorized {
			result[i].Remaining = max(0, time.Until(result[i].Deadline))
			result[i].Authorized = result[i].Remaining > 0
		}
	}
	return result, err
}

func (s *Store) Activities(ctx context.Context) ([]Activity, error) { return s.activities(ctx, nil) }
func (s *Store) Current(ctx context.Context, h Hold) (Activity, error) {
	a, e := s.activities(ctx, &h)
	if e != nil {
		return Activity{}, e
	}
	return a[0], nil
}

// Quarantine withdraws authority without releasing ownership. A repeated error
// must not undo stop/cleanup progress. It grants no new submission permission.
func (s *Store) Quarantine(ctx context.Context, h Hold) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) || h.mode != "JOB" {
			return ErrFence
		}
		if r.phase != "STOPPING" && r.phase != "CLEANING" {
			if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='QUARANTINED',stopped=0 WHERE resource_id=?; UPDATE inbox SET phase='QUARANTINED' WHERE lease_id=?`, h.resource, h.owner); e != nil {
				return ErrStorage
			}
		}
		delete(s.guards, h.owner)
		return nil
	})
}
