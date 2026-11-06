package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const MaximumRentalCommands = 4096
const MaximumRentalInvocations = 4096

type rentalGuard struct {
	clock    guard
	grant    rentalaccess.Verified
	expected rentalaccess.Expected
	physical string
	allowed  bool
}
type rentalOwner struct {
	hold                                                                      Hold
	manifest                                                                  rentalaccess.Manifest
	manifestHash, approvalHash, physical, phase, grant, digest, context, hard string
}

func rentalOwnerTx(ctx context.Context, tx *sql.Tx, h Hold) (rentalOwner, error) {
	var o rentalOwner
	r, err := readResource(ctx, tx, h.resource)
	if err != nil {
		return o, err
	}
	if !match(r, h) || h.mode != "RENTAL" {
		return o, ErrFence
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT resource_id,local_generation,metadata_raw,manifest_hash,approval_hash,phase,grant_id,grant_digest,grant_context,hard_deadline_ns FROM rental_owners WHERE session_id=?`, h.owner).
		Scan(&o.hold.resource, &o.hold.local, &raw, &o.manifestHash, &o.approvalHash, &o.phase, &o.grant, &o.digest, &o.context, &o.hard)
	o.hold.owner = h.owner
	o.hold.mode = "RENTAL"
	if err != nil || o.hold != h || strictjson.Decode([]byte(raw), &o.manifest, rentalaccess.MaximumManifestBytes) != nil || digest([]byte(raw)) != o.manifestHash || o.manifest.Session != h.owner || o.manifest.Resource != h.resource || o.manifest.Generation != r.high {
		return o, ErrStorage
	}
	if tx.QueryRowContext(ctx, `SELECT physical_digest FROM resources WHERE resource_id=?`, h.resource).Scan(&o.physical) != nil {
		return o, ErrStorage
	}
	want := map[string]string{"PREPARING": "PREPARING", "ACTIVE": "EXECUTING", "STOPPING": "STOPPING", "CLEANING": "CLEANING", "QUARANTINED": "QUARANTINED"}[o.phase]
	if want == "" || r.phase != want {
		return o, ErrRecovery
	}
	return o, nil
}

func rentalCommandCapacity(ctx context.Context, tx *sql.Tx) error {
	var n int
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM rental_commands`).Scan(&n) != nil {
		return ErrStorage
	}
	if n >= MaximumRentalCommands {
		return ErrCapacity
	}
	return nil
}

// ReceiveRentalPreparation owns the SAME resources row used by local use,
// maintenance and JOB. The actual owner is session_id with mode RENTAL, never
// owner_attempt_id or an invented job. Reserving is not permission to invoke.
func (s *Store) ReceiveRentalPreparation(ctx context.Context, command string, p rentalaccess.Prepared) (Hold, string, error) {
	var h Hold
	disposition := ""
	m, a := p.Details(), p.Approval()
	if !ids.Valid(command) || p.Digest() == "" {
		return h, disposition, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		var previous, hash, kind string
		e := tx.QueryRowContext(ctx, `SELECT session_id,metadata_hash,kind FROM rental_commands WHERE command_id=?`, command).Scan(&previous, &hash, &kind)
		if e == nil {
			if previous != m.Session || hash != p.ApprovalDigest() || kind != "PREPARE" {
				return ErrFence
			}
			var resource, local string
			if tx.QueryRowContext(ctx, `SELECT resource_id,local_generation FROM rental_owners WHERE session_id=?`, previous).Scan(&resource, &local) != nil {
				return ErrStorage
			}
			h = Hold{resource, previous, local, "RENTAL"}
			disposition = "QUERY_ONLY"
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		var exists bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_owners WHERE session_id=?)`, m.Session).Scan(&exists) != nil {
			return ErrStorage
		}
		if exists {
			return ErrQuery
		}
		if !p.ValidAt(stamp.Wall) || m.Device != s.binding.Device || m.Seller != s.binding.Seller {
			return ErrFence
		}
		if e = rentalCommandCapacity(ctx, tx); e != nil {
			return e
		}
		r, e := readResource(ctx, tx, m.Resource)
		if e != nil {
			return e
		}
		if r.phase != "FREE" {
			return ErrBusy
		}
		var physical string
		if tx.QueryRowContext(ctx, `SELECT physical_digest FROM resources WHERE resource_id=?`, m.Resource).Scan(&physical) != nil {
			return ErrStorage
		}
		if physical != a.PhysicalHash {
			return ErrFence
		}
		high, _ := executionlease.Counter(r.high)
		next, _ := executionlease.Counter(m.Generation)
		if next <= high {
			return ErrFence
		}
		local, e := successor(r.hold.local)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_owners(session_id,resource_id,local_generation,metadata_raw,manifest_hash,approval_hash,phase,prepare_until_ns) VALUES(?,?,?,?,?,?,'PREPARING',?)`, m.Session, m.Resource, local, string(p.Raw()), p.Digest(), p.ApprovalDigest(), strconv.FormatInt(a.PrepareUntil.UnixNano(), 10)); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_commands(command_id,session_id,metadata_hash,kind) VALUES(?,?,?,'PREPARE')`, command, m.Session, p.ApprovalDigest()); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET owner_id=?,local_high=?,remote_high=?,mode='RENTAL',phase='PREPARING',stopped=0 WHERE resource_id=?`, m.Session, local, m.Generation, m.Resource); e != nil {
			return ErrStorage
		}
		h = Hold{m.Resource, m.Session, local, "RENTAL"}
		remaining := a.PrepareUntil.Sub(stamp.Wall)
		s.rentalGuards[m.Session] = rentalGuard{clock: guard{expires: a.PrepareUntil, until: stamp.Elapsed + remaining, deadline: s.sampledAt.Add(remaining)}}
		disposition = "PREPARATION_RESERVED"
		return nil
	})
	return h, disposition, err
}

// BindRentalGrant persists a separately verified original grant. Even after a
// successful bind, no invocation is allowed until ApplyRentalControl commits a
// fresh signed receipt. Replacement requires no unfinished child and cannot
// revive a stopped/quarantined session, change paid terms or its hard deadline.
func (s *Store) BindRentalGrant(ctx context.Context, command string, h Hold, p rentalaccess.Prepared, g rentalaccess.Verified, current rentalaccess.Expected) error {
	if !ids.Valid(command) || !p.BindsGrant(current) {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe || !g.Matches(current, stamp.Wall) {
			return ErrRecovery
		}
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		if o.manifestHash != p.Digest() || o.approvalHash != p.ApprovalDigest() || (o.phase != "PREPARING" && o.phase != "ACTIVE") {
			return ErrFence
		}
		var prior, hash, kind string
		e = tx.QueryRowContext(ctx, `SELECT session_id,metadata_hash,kind FROM rental_commands WHERE command_id=?`, command).Scan(&prior, &hash, &kind)
		if e == nil {
			if prior != h.owner || hash != g.Digest() || kind != "GRANT" {
				return ErrFence
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		if e = rentalCommandCapacity(ctx, tx); e != nil {
			return e
		}
		var known, busy bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_grants WHERE grant_id=?)`, current.Grant).Scan(&known) != nil || tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_invocations WHERE session_id=? AND phase<>'CLEANED')`, h.owner).Scan(&busy) != nil {
			return ErrStorage
		}
		if known {
			return ErrQuery
		}
		if busy {
			return ErrBusy
		}
		hard, e := time.Parse(time.RFC3339Nano, current.HardDeadline)
		if e != nil {
			return ErrFence
		}
		if o.hard != "0" && o.hard != strconv.FormatInt(hard.UnixNano(), 10) {
			return ErrFence
		}
		if o.context != "" {
			var old rentalaccess.Expected
			if strictjson.Decode([]byte(o.context), &old, 8192) != nil {
				return ErrStorage
			}
			if old.Terms != current.Terms || old.TermsHash != current.TermsHash || old.HardDeadline != current.HardDeadline {
				return ErrFence
			}
		}
		raw, e := json.Marshal(current)
		if e != nil || len(raw) > 8192 {
			return ErrFence
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_grants SET state='REVOKED' WHERE session_id=? AND state='CURRENT'`, h.owner); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_grants(grant_id,session_id,digest,issuer_key_id,expected_raw,state) VALUES(?,?,?,?,?,'CURRENT')`, current.Grant, h.owner, g.Digest(), g.KeyID(), string(raw)); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_commands(command_id,session_id,metadata_hash,kind) VALUES(?,?,?,'GRANT')`, command, h.owner, g.Digest()); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_owners SET phase='ACTIVE',grant_id=?,grant_digest=?,grant_context=?,hard_deadline_ns=? WHERE session_id=?`, current.Grant, g.Digest(), string(raw), strconv.FormatInt(hard.UnixNano(), 10), h.owner); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='EXECUTING',stopped=0 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		expires := stamp.Wall.Add(rentalaccess.MaximumControlTerm)
		for _, limit := range []time.Time{hard, time.Unix(g.Details().Expires, 0)} {
			if limit.Before(expires) {
				expires = limit
			}
		}
		remaining := expires.Sub(stamp.Wall)
		s.rentalGuards[h.owner] = rentalGuard{clock: guard{expires: expires, until: stamp.Elapsed + remaining, deadline: s.sampledAt.Add(remaining)}, grant: g, expected: current, physical: p.Approval().PhysicalHash}
		return nil
	})
}

func stopRentalTx(ctx context.Context, tx *sql.Tx, h Hold, quarantine bool) error {
	phase := "STOPPING"
	if quarantine {
		phase = "QUARANTINED"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources SET phase=CASE WHEN phase='CLEANING' THEN phase ELSE ? END,stopped=CASE WHEN phase='CLEANING' THEN stopped ELSE 0 END WHERE resource_id=? AND owner_id=? AND local_high=? AND mode='RENTAL'`, phase, h.resource, h.owner, h.local); err != nil {
		return ErrStorage
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rental_owners SET phase=CASE WHEN phase IN ('CLEANING','CLEANED') THEN phase ELSE ? END WHERE session_id=?`, phase, h.owner); err != nil {
		return ErrStorage
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rental_grants SET state='STOPPED' WHERE session_id=? AND state='CURRENT'`, h.owner); err != nil {
		return ErrStorage
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rental_invocations SET phase=CASE WHEN phase IN ('CLEANING','CLEANED') THEN phase ELSE ? END WHERE session_id=?`, phase, h.owner); err != nil {
		return ErrStorage
	}
	return nil
}

func (s *Store) advanceRentalTx(ctx context.Context, tx *sql.Tx, stamp Stamp) error {
	for session, g := range s.rentalGuards {
		if !stamp.Wall.Before(g.clock.expires) || stamp.Elapsed >= g.clock.until || !time.Now().Before(g.clock.deadline) {
			var h Hold
			h.owner = session
			h.mode = "RENTAL"
			if tx.QueryRowContext(ctx, `SELECT resource_id,local_generation FROM rental_owners WHERE session_id=?`, session).Scan(&h.resource, &h.local) != nil {
				return ErrStorage
			}
			if err := stopRentalTx(ctx, tx, h, true); err != nil {
				return err
			}
			delete(s.rentalGuards, session)
		}
	}
	return nil
}

func (s *Store) ApplyRentalControl(ctx context.Context, h Hold, proof rentalaccess.VerifiedControl) error {
	var deadline time.Time
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		g, live := s.rentalGuards[h.owner]
		if !live || o.phase != "ACTIVE" || !proof.Matches(g.grant, g.expected, stamp.Wall) || o.grant != g.expected.Grant || o.digest != g.grant.Digest() || o.manifestHash != g.expected.ManifestHash || o.physical != g.physical {
			return ErrRecovery
		}
		c := proof.Details()
		var high, identity, state string
		if tx.QueryRowContext(ctx, `SELECT control_sequence,control_identity,state FROM rental_grants WHERE grant_id=? AND session_id=?`, c.Grant, h.owner).Scan(&high, &identity, &state) != nil {
			return ErrStorage
		}
		if state != "CURRENT" {
			return ErrRecovery
		}
		old, e := executionlease.Counter(high)
		if e != nil {
			return ErrStorage
		}
		next, e := executionlease.Counter(c.Sequence)
		if e != nil || next < old {
			return ErrFence
		}
		if next == old {
			if identity != proof.Identity() {
				return ErrFence
			}
			deadline = g.clock.deadline
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_grants SET control_sequence=?,control_identity=? WHERE grant_id=?`, c.Sequence, proof.Identity(), c.Grant); e != nil {
			return ErrStorage
		}
		if c.Decision == "STOP" {
			delete(s.rentalGuards, h.owner)
			return stopRentalTx(ctx, tx, h, false)
		}
		expires, e := time.Parse(time.RFC3339Nano, c.Expires)
		if e != nil {
			return ErrFence
		}
		remaining := expires.Sub(stamp.Wall)
		g.clock = guard{expires: expires, until: stamp.Elapsed + remaining, deadline: s.sampledAt.Add(remaining)}
		g.allowed = true
		deadline = g.clock.deadline
		s.rentalGuards[h.owner] = g
		return nil
	})
	if err == nil && !deadline.IsZero() && !time.Now().Before(deadline) {
		return ErrRecovery
	}
	return err
}

type RentalStatus struct {
	Hold                                     Hold
	Phase, Grant, ManifestHash, ApprovalHash string
	Expected                                 rentalaccess.Expected
	Authorized                               bool
	Deadline                                 time.Time
}

// RentalCurrent reads the original local tuple even before its first ALLOW.
// It cannot create permission; CheckRental additionally requires the in-memory
// signed control clock and exact scope. Reopening never reconstructs that clock.
func (s *Store) RentalCurrent(ctx context.Context, h Hold) (RentalStatus, error) {
	var out RentalStatus
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		out = RentalStatus{Hold: h, Phase: o.phase, Grant: o.grant, ManifestHash: o.manifestHash, ApprovalHash: o.approvalHash}
		if o.context != "" && strictjson.Decode([]byte(o.context), &out.Expected, 8192) != nil {
			return ErrStorage
		}
		// A preparation or not-yet-ALLOW grant still has an immutable stop
		// deadline. Reporting it does not grant invocation authority.
		if g, ok := s.rentalGuards[h.owner]; ok {
			out.Deadline = g.clock.deadline
		}
		if g, ok := s.rentalGuards[h.owner]; ok && g.allowed && o.phase == "ACTIVE" && !s.clockUnsafe && g.expected == out.Expected {
			out.Deadline = g.clock.deadline
			out.Authorized = stamp.Wall.Before(g.clock.expires) && stamp.Elapsed < g.clock.until && time.Now().Before(g.clock.deadline)
		}
		return nil
	})
	if out.Authorized && !time.Now().Before(out.Deadline) {
		out.Authorized = false
	}
	return out, err
}

func (s *Store) CheckRental(ctx context.Context, h Hold, scope string) error {
	var deadline time.Time
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		g, live := s.rentalGuards[h.owner]
		if !live || !g.allowed || o.phase != "ACTIVE" || o.grant != g.expected.Grant || o.digest != g.grant.Digest() || o.manifestHash != g.expected.ManifestHash || o.physical != g.physical || !g.grant.Allows(scope, stamp.Wall) || !time.Now().Before(g.clock.deadline) {
			return ErrRecovery
		}
		deadline = g.clock.deadline
		return nil
	})
	if err == nil && !time.Now().Before(deadline) {
		return ErrRecovery
	}
	return err
}

func (s *Store) StopRental(ctx context.Context, h Hold) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, err := rentalOwnerTx(ctx, tx, h); err != nil {
			return err
		}
		delete(s.rentalGuards, h.owner)
		return stopRentalTx(ctx, tx, h, false)
	})
}
