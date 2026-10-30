package execution

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"strconv"
	"time"
)

type resource struct {
	hold        Hold
	phase, high string
	stopped     bool
}

func readResource(ctx context.Context, tx *sql.Tx, id string) (resource, error) {
	var r resource
	r.hold.resource = id
	e := tx.QueryRowContext(ctx, `SELECT owner_id,local_high,mode,phase,remote_high,stopped FROM resources WHERE resource_id=?`, id).Scan(&r.hold.owner, &r.hold.local, &r.hold.mode, &r.phase, &r.high, &r.stopped)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrFence
	}
	if e != nil {
		return r, ErrStorage
	}
	if _, e = executionlease.Counter(r.high); e != nil {
		return r, ErrStorage
	}
	if _, e = executionlease.Counter(r.hold.local); e != nil {
		return r, ErrStorage
	}
	return r, nil
}
func match(r resource, h Hold) bool { return h.owner != "" && r.hold == h && r.phase != "FREE" }
func successor(value string) (string, error) {
	n, e := executionlease.Counter(value)
	if e != nil {
		return "", ErrStorage
	}
	if n == math.MaxUint64 {
		return "", ErrCapacity
	}
	return strconv.FormatUint(n+1, 10), nil
}
func receiptCapacity(ctx context.Context, tx *sql.Tx) error {
	var n int
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts`).Scan(&n) != nil {
		return ErrStorage
	}
	if n >= MaximumReceipts {
		return ErrCapacity
	}
	return nil
}
func (s *Store) installGuard(l executionlease.Lease, stamp Stamp) {
	expires, _ := time.Parse(time.RFC3339Nano, l.Expires)
	remaining := expires.Sub(stamp.Wall)
	// Anchor the native monotonic deadline BEFORE metadata processing and
	// fsync. Neither transaction latency nor later reads can renew a lease.
	s.guards[l.ID] = guard{expires: expires, until: stamp.Elapsed + remaining, deadline: s.sampledAt.Add(remaining)}
}

// Receive durably records the original command and both remote fences before
// a backend submission can be reserved. A duplicate never grants submit again.
func (s *Store) Receive(ctx context.Context, command string, verified executionlease.Verified) (Hold, string, error) {
	var hold Hold
	disposition := ""
	l := verified.Details()
	if !ids.Valid(command) || len(verified.Metadata()) == 0 {
		return hold, disposition, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		var id, hash string
		e := tx.QueryRowContext(ctx, `SELECT lease_id,metadata_hash FROM receipts WHERE command_id=?`, command).Scan(&id, &hash)
		if e == nil {
			if id != l.ID || hash != verified.Digest() {
				return ErrFence
			}
			var generation string
			if tx.QueryRowContext(ctx, `SELECT local_generation FROM inbox WHERE lease_id=?`, id).Scan(&generation) != nil {
				return ErrStorage
			}
			hold = Hold{l.Resource, id, generation, "JOB"}
			disposition = "QUERY_ONLY"
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		var metadataHash, generation string
		e = tx.QueryRowContext(ctx, `SELECT metadata_hash,local_generation FROM inbox WHERE lease_id=?`, l.ID).Scan(&metadataHash, &generation)
		if e == nil {
			if metadataHash != verified.Digest() {
				return ErrFence
			}
			hold = Hold{l.Resource, l.ID, generation, "JOB"}
			disposition = "QUERY_ONLY"
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		if !verified.ValidAt(stamp.Wall) || l.Device != s.binding.Device || l.Seller != s.binding.Seller {
			return executionlease.ErrLease
		}
		if e = receiptCapacity(ctx, tx); e != nil {
			return e
		}
		r, e := readResource(ctx, tx, l.Resource)
		if e != nil {
			return e
		}
		if r.phase != "FREE" {
			return ErrBusy
		}
		hi, _ := executionlease.Counter(r.high)
		next, _ := executionlease.Counter(l.Generation)
		if next <= hi {
			return ErrFence
		}
		var token string
		e = tx.QueryRowContext(ctx, `SELECT token FROM job_fences WHERE job_id=?`, l.Job).Scan(&token)
		if e == nil {
			before, e1 := executionlease.Counter(token)
			after, e2 := executionlease.Counter(l.Fence)
			if e1 != nil || e2 != nil {
				return ErrStorage
			}
			if after <= before {
				return ErrFence
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		generation, e = successor(r.hold.local)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO inbox(lease_id,resource_id,local_generation,metadata_json,metadata_hash,phase) VALUES(?,?,?,?,?,'RECEIVED')`, l.ID, l.Resource, generation, string(verified.Metadata()), verified.Digest()); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO receipts(command_id,lease_id,metadata_hash,kind) VALUES(?,?,?,'RECEIVE')`, command, l.ID, verified.Digest()); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO job_fences(job_id,token,lease_id) VALUES(?,?,?) ON CONFLICT(job_id) DO UPDATE SET token=excluded.token,lease_id=excluded.lease_id`, l.Job, l.Fence, l.ID); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET remote_high=?,local_high=?,owner_id=?,mode='JOB',phase='PREPARING',stopped=0 WHERE resource_id=?`, l.Generation, generation, l.ID, l.Resource); e != nil {
			return ErrStorage
		}
		hold = Hold{l.Resource, l.ID, generation, "JOB"}
		disposition = "RECEIVED"
		s.installGuard(l, stamp)
		return nil
	})
	return hold, disposition, err
}

// AcquireLocal covers managed personal use and maintenance only. It cannot
// manufacture a JOB or RENTAL grant. The physical digest is unique across all
// engine aliases; personal processes outside this supervisor are not controlled.
func (s *Store) AcquireLocal(ctx context.Context, resourceID, owner, mode string) (Hold, error) {
	var hold Hold
	if !ids.Valid(owner) || (mode != "LOCAL" && mode != "MAINTENANCE") {
		return hold, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		r, e := readResource(ctx, tx, resourceID)
		if e != nil {
			return e
		}
		if r.phase != "FREE" {
			return ErrBusy
		}
		next, e := successor(r.hold.local)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET local_high=?,owner_id=?,mode=?,phase='PREPARING',stopped=0 WHERE resource_id=?`, next, owner, mode, resourceID); e != nil {
			return ErrStorage
		}
		hold = Hold{resourceID, owner, next, mode}
		return nil
	})
	return hold, err
}

// BeginSubmit is the only durable transition granting one backend submission
// attempt. Persist SUBMITTING before calling the adapter with key=lease_id.
// If the reply is lost, query that same key; never call BeginSubmit again.
func (s *Store) BeginSubmit(ctx context.Context, h Hold) (string, error) {
	key := ""
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) || h.mode != "JOB" {
			return ErrFence
		}
		if r.phase == "EXECUTING" {
			return ErrQuery
		}
		if _, live := s.guards[h.owner]; !live || r.phase != "PREPARING" {
			return ErrRecovery
		}
		var phase string
		if tx.QueryRowContext(ctx, `SELECT phase FROM inbox WHERE lease_id=?`, h.owner).Scan(&phase) != nil {
			return ErrStorage
		}
		if phase != "RECEIVED" {
			return ErrQuery
		}
		if _, e = tx.ExecContext(ctx, `UPDATE inbox SET phase='SUBMITTING' WHERE lease_id=?`, h.owner); e != nil {
			return ErrStorage
		}
		key = h.owner
		return nil
	})
	return key, err
}

// RecordBackend also accepts a late positive query while stopping/quarantined.
// Learning an ID never restores authority or sends a second submit.
func (s *Store) RecordBackend(ctx context.Context, h Hold, backend string) error {
	if !ids.Valid(backend) {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) || h.mode != "JOB" {
			return ErrFence
		}
		var old, phase string
		if tx.QueryRowContext(ctx, `SELECT backend_id,phase FROM inbox WHERE lease_id=?`, h.owner).Scan(&old, &phase) != nil {
			return ErrStorage
		}
		if old != "" && old != backend {
			return ErrFence
		}
		if phase != "SUBMITTING" && phase != "EXECUTING" && phase != "QUARANTINED" && phase != "STOPPING" {
			return ErrFence
		}
		if phase == "SUBMITTING" && r.phase == "PREPARING" {
			if _, ok := s.guards[h.owner]; !ok {
				return ErrRecovery
			}
			phase = "EXECUTING"
			if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='EXECUTING' WHERE resource_id=?`, h.resource); e != nil {
				return ErrStorage
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE inbox SET backend_id=?,phase=? WHERE lease_id=?`, backend, phase, h.owner); e != nil {
			return ErrStorage
		}
		return nil
	})
}

func (s *Store) Renew(ctx context.Context, command string, h Hold, verified executionlease.Verified) error {
	if !ids.Valid(command) || len(verified.Metadata()) == 0 {
		return ErrFence
	}
	l := verified.Details()
	return s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) || h.mode != "JOB" || l.ID != h.owner {
			return ErrFence
		}
		if r.phase != "PREPARING" && r.phase != "EXECUTING" {
			return ErrRecovery
		}
		if _, live := s.guards[h.owner]; !live {
			return ErrRecovery
		}
		var raw, hash string
		if tx.QueryRowContext(ctx, `SELECT metadata_json,metadata_hash FROM inbox WHERE lease_id=?`, h.owner).Scan(&raw, &hash) != nil {
			return ErrStorage
		}
		var before executionlease.Lease
		if strictjson.Decode([]byte(raw), &before, executionlease.MaximumBody) != nil {
			return ErrStorage
		}
		var receiptHash string
		e = tx.QueryRowContext(ctx, `SELECT metadata_hash FROM receipts WHERE command_id=?`, command).Scan(&receiptHash)
		if e == nil {
			if receiptHash != verified.Digest() {
				return ErrFence
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		if verified.Digest() == hash {
			return nil
		}
		if !verified.ValidAt(stamp.Wall) || !executionlease.SameOwner(before, l) || l.Revision <= before.Revision {
			return executionlease.ErrLease
		}
		oldIssued, _ := time.Parse(time.RFC3339Nano, before.Issued)
		issued, _ := time.Parse(time.RFC3339Nano, l.Issued)
		oldExpires, _ := time.Parse(time.RFC3339Nano, before.Expires)
		expires, _ := time.Parse(time.RFC3339Nano, l.Expires)
		if issued.Before(oldIssued) || expires.Before(oldExpires) {
			return executionlease.ErrLease
		}
		if e = receiptCapacity(ctx, tx); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE inbox SET metadata_json=?,metadata_hash=? WHERE lease_id=?`, string(verified.Metadata()), verified.Digest(), l.ID); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO receipts(command_id,lease_id,metadata_hash,kind) VALUES(?,?,?,'RENEW')`, command, l.ID, verified.Digest()); e != nil {
			return ErrStorage
		}
		s.installGuard(l, stamp)
		return nil
	})
}

// Recovery exposes original IDs only, never a new submission permit. Reopen,
// duplicate delivery and ambiguous submit all follow this query-first path.
type Recovery struct {
	Hold                        Hold
	RequestID, BackendID, Phase string
}

func (s *Store) Recovery(ctx context.Context, leaseID string) (Recovery, error) {
	var result Recovery
	if !ids.Valid(leaseID) {
		return result, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		result.RequestID = leaseID
		result.Hold.owner = leaseID
		result.Hold.mode = "JOB"
		e := tx.QueryRowContext(ctx, `SELECT resource_id,local_generation,backend_id,phase FROM inbox WHERE lease_id=?`, leaseID).Scan(&result.Hold.resource, &result.Hold.local, &result.BackendID, &result.Phase)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrFence
		}
		if e != nil {
			return ErrStorage
		}
		return nil
	})
	return result, err
}

func (s *Store) RequestStop(ctx context.Context, h Hold) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) {
			return ErrFence
		}
		if r.phase == "CLEANING" {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='STOPPING',stopped=0 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		if h.mode == "JOB" {
			if _, e = tx.ExecContext(ctx, `UPDATE inbox SET phase='STOPPING' WHERE lease_id=?`, h.owner); e != nil {
				return ErrStorage
			}
			delete(s.guards, h.owner)
		}
		return nil
	})
}

// Observation must come from the owned backend adapter's current query. ABSENT
// requires an authoritative request-key lookup and no previously known backend
// ID. Network failure, HTTP closure, TTL or a missing PID is never STOPPED.
type Observation struct{ RequestID, BackendID, State string }

func (s *Store) ObserveStopped(ctx context.Context, h Hold, o Observation) error {
	if o.RequestID != h.owner || (o.State != "STOPPED" && o.State != "ABSENT") || o.State == "ABSENT" && o.BackendID != "" || o.State == "STOPPED" && !ids.Valid(o.BackendID) {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) {
			return ErrFence
		}
		if r.phase != "STOPPING" {
			return ErrEvidence
		}
		if h.mode == "JOB" {
			var known string
			if tx.QueryRowContext(ctx, `SELECT backend_id FROM inbox WHERE lease_id=?`, h.owner).Scan(&known) != nil {
				return ErrStorage
			}
			if known != "" && known != o.BackendID {
				return ErrEvidence
			}
			if _, e = tx.ExecContext(ctx, `UPDATE inbox SET backend_id=?,phase='CLEANING' WHERE lease_id=?`, o.BackendID, h.owner); e != nil {
				return ErrStorage
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='CLEANING',stopped=1 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		return nil
	})
}

type Cleanup struct {
	WorkStopped, TokensRevoked, WorkspaceCleared bool
	ManifestHash                                 string
}

func (s *Store) FinishCleanup(ctx context.Context, h Hold, c Cleanup) error {
	if !c.WorkStopped || !c.TokensRevoked || !c.WorkspaceCleared || !executionlease.Hash(c.ManifestHash) {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if !match(r, h) {
			return ErrFence
		}
		if r.phase != "CLEANING" || !r.stopped {
			return ErrEvidence
		}
		if h.mode == "JOB" {
			if _, e = tx.ExecContext(ctx, `UPDATE inbox SET phase='CLEANED',cleanup_hash=? WHERE lease_id=?`, c.ManifestHash, h.owner); e != nil {
				return ErrStorage
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET owner_id='',mode='',phase='FREE',stopped=0 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		if h.mode == "JOB" {
			delete(s.guards, h.owner)
		}
		return nil
	})
}
