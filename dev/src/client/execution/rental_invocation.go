package execution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

// RentalInvocation has no prompt, plaintext hash, paths, keys or asset URLs.
// InputCiphertextDigest identifies already authenticated encrypted input, not
// a fingerprint manufactured from plaintext at this gate.
type RentalInvocation struct {
	ID, Session, Resource, LocalGeneration                  string
	Grant, GrantDigest, InputCiphertextDigest, RecipeDigest string
	Phase, BackendID                                        string
}
type RentalSubmission struct {
	mu    sync.Mutex
	value RentalInvocation
	taken bool
}

func (t *RentalSubmission) Take() (RentalInvocation, error) {
	if t == nil {
		return RentalInvocation{}, ErrQuery
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.taken {
		return RentalInvocation{}, ErrQuery
	}
	t.taken = true
	return t.value, nil
}

func readRentalInvocation(ctx context.Context, tx *sql.Tx, h Hold, id string) (RentalInvocation, error) {
	v := RentalInvocation{ID: id, Resource: h.resource, LocalGeneration: h.local}
	err := tx.QueryRowContext(ctx, `SELECT session_id,original_grant,grant_digest,input_ciphertext_digest,recipe_digest,phase,backend_id FROM rental_invocations WHERE invocation_id=?`, id).
		Scan(&v.Session, &v.Grant, &v.GrantDigest, &v.InputCiphertextDigest, &v.RecipeDigest, &v.Phase, &v.BackendID)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrFence
	}
	if err != nil {
		return v, ErrStorage
	}
	if v.Session != h.owner {
		return v, ErrFence
	}
	return v, nil
}

// BeginRentalInvocation durably reserves ONE submission before native entry.
// An original-ID replay only returns query metadata, never another ticket.
// The outer session keeps the original physical resource through all children.
func (s *Store) BeginRentalInvocation(ctx context.Context, h Hold, id, inputCiphertextDigest, recipeDigest string) (*RentalSubmission, RentalInvocation, string, error) {
	var out RentalInvocation
	var deadline time.Time
	disposition := ""
	if !ids.Valid(id) || !executionlease.Hash(inputCiphertextDigest) || !executionlease.Hash(recipeDigest) {
		return nil, out, disposition, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		prior, e := readRentalInvocation(ctx, tx, h, id)
		if e == nil {
			if prior.InputCiphertextDigest != inputCiphertextDigest || prior.RecipeDigest != recipeDigest {
				return ErrFence
			}
			out = prior
			disposition = "QUERY_ONLY"
			return nil
		}
		if !errors.Is(e, ErrFence) {
			return e
		}
		// A globally existing foreign invocation ID is not a new local request.
		var exists, busy bool
		var count int
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_invocations WHERE invocation_id=?)`, id).Scan(&exists) != nil || tx.QueryRowContext(ctx, `SELECT count(*) FROM rental_invocations`).Scan(&count) != nil {
			return ErrStorage
		}
		if exists {
			return ErrFence
		}
		if count >= MaximumRentalInvocations {
			return ErrCapacity
		}
		g, live := s.rentalGuards[h.owner]
		if s.clockUnsafe || !live || !g.allowed || o.phase != "ACTIVE" || o.grant != g.expected.Grant || o.digest != g.grant.Digest() || o.manifestHash != g.expected.ManifestHash || o.physical != g.physical || !g.grant.Allows("INVOKE", stamp.Wall) || !time.Now().Before(g.clock.deadline) {
			return ErrRecovery
		}
		if recipeDigest != o.manifest.RecipeHash {
			return ErrFence
		}
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_invocations WHERE session_id=? AND phase<>'CLEANED')`, h.owner).Scan(&busy) != nil {
			return ErrStorage
		}
		if busy {
			return ErrBusy
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_invocations(invocation_id,session_id,original_grant,grant_digest,input_ciphertext_digest,recipe_digest,phase) VALUES(?,?,?,?,?,?,'SUBMITTING')`, id, h.owner, o.grant, o.digest, inputCiphertextDigest, recipeDigest); e != nil {
			return ErrStorage
		}
		out = RentalInvocation{ID: id, Session: h.owner, Resource: h.resource, LocalGeneration: h.local, Grant: o.grant, GrantDigest: o.digest, InputCiphertextDigest: inputCiphertextDigest, RecipeDigest: recipeDigest, Phase: "SUBMITTING"}
		deadline = g.clock.deadline
		disposition = "SUBMISSION_RESERVED"
		return nil
	})
	if err != nil {
		return nil, RentalInvocation{}, "", err
	}
	if disposition != "SUBMISSION_RESERVED" {
		return nil, out, disposition, nil
	}
	// Commit/flush time is charged to the original clock. An elapsed deadline
	// retains SUBMITTING for original-key recovery, never a late submit ticket.
	if !time.Now().Before(deadline) {
		return nil, out, "QUERY_ONLY", ErrRecovery
	}
	return &RentalSubmission{value: out}, out, disposition, nil
}

func (s *Store) MarkRentalSubmitted(ctx context.Context, h Hold, id, backend string) error {
	if !ids.Valid(id) || !ids.Valid(backend) {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.BackendID != "" && v.BackendID != backend {
			return ErrFence
		}
		if v.Phase == "CLEANED" || v.Phase == "CLEANING" {
			return ErrQuery
		}
		phase := v.Phase
		if phase == "SUBMITTING" {
			g, ok := s.rentalGuards[h.owner]
			if !s.clockUnsafe && ok && g.allowed && o.phase == "ACTIVE" && o.grant == g.expected.Grant && o.digest == g.grant.Digest() && o.manifestHash == g.expected.ManifestHash && o.physical == g.physical && g.grant.Allows("INVOKE", stamp.Wall) && time.Now().Before(g.clock.deadline) {
				phase = "EXECUTING"
			} else {
				phase = "STOPPING"
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_invocations SET backend_id=?,phase=? WHERE invocation_id=?`, backend, phase, id); e != nil {
			return ErrStorage
		}
		return nil
	})
}

func (s *Store) RentalInvocation(ctx context.Context, h Hold, id string) (RentalInvocation, error) {
	var v RentalInvocation
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		var e error
		v, e = readRentalInvocation(ctx, tx, h, id)
		return e
	})
	return v, err
}
func (s *Store) StopRentalInvocation(ctx context.Context, h Hold, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.Phase == "CLEANING" || v.Phase == "CLEANED" {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_invocations SET phase='STOPPING' WHERE invocation_id=?`, id); e != nil {
			return ErrStorage
		}
		return nil
	})
}

// Observation comes only from the trusted owned adapter's original-key query.
// A missing process, HTTP close or cancel acknowledgement does not prove it.
func (s *Store) ObserveRentalInvocationStopped(ctx context.Context, h Hold, id string, observation Observation) error {
	if observation.RequestID != id || (observation.State != "STOPPED" && observation.State != "ABSENT") || observation.State == "ABSENT" && observation.BackendID != "" || observation.State == "STOPPED" && !ids.Valid(observation.BackendID) {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.Phase != "STOPPING" && v.Phase != "QUARANTINED" {
			return ErrEvidence
		}
		if v.BackendID != "" && v.BackendID != observation.BackendID {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_invocations SET phase='CLEANING',backend_id=? WHERE invocation_id=?`, observation.BackendID, id); e != nil {
			return ErrStorage
		}
		return nil
	})
}

type RentalChildCleanup struct {
	Session, Invocation, LocalGeneration, BackendID, ManifestHash    string
	WorkStopped, FutureStartRevoked, InputCleared, OutputKeysHandled bool
}

// Child cleanup never releases its parent's resource or money. Facts are
// supplied by trusted original native adapters, never unmarshaled from a buyer
// or provider HTTP JSON acknowledgement.
func (s *Store) FinishRentalInvocationCleanup(ctx context.Context, h Hold, id string, c RentalChildCleanup) error {
	if c.Session != h.owner || c.Invocation != id || c.LocalGeneration != h.local || !executionlease.Hash(c.ManifestHash) || !c.WorkStopped || !c.FutureStartRevoked || !c.InputCleared || !c.OutputKeysHandled {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.Phase != "CLEANING" || v.BackendID != c.BackendID {
			return ErrEvidence
		}
		var mediaOutstanding bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_media_workspaces WHERE invocation_id=? AND phase<>'CLEANED')`, id).Scan(&mediaOutstanding) != nil {
			return ErrStorage
		}
		if mediaOutstanding {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_invocations SET phase='CLEANED',cleanup_hash=? WHERE invocation_id=?`, c.ManifestHash, id); e != nil {
			return ErrStorage
		}
		return nil
	})
}

type RentalSessionStop struct {
	Session, LocalGeneration, ScopeDigest   string
	AllRequestsStopped, FutureStartsRevoked bool
}

func (s *Store) ObserveRentalSessionStopped(ctx context.Context, h Hold, o RentalSessionStop) error {
	if o.Session != h.owner || o.LocalGeneration != h.local || !executionlease.Hash(o.ScopeDigest) || !o.AllRequestsStopped || !o.FutureStartsRevoked {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		r, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		if r.phase != "STOPPING" && r.phase != "QUARANTINED" {
			return ErrEvidence
		}
		var outstanding bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_invocations WHERE session_id=? AND phase<>'CLEANED')`, h.owner).Scan(&outstanding) != nil {
			return ErrStorage
		}
		if outstanding {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_owners SET phase='CLEANING' WHERE session_id=?`, h.owner); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='CLEANING',stopped=1 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		return nil
	})
}

type RentalSessionCleanup struct {
	Session, LocalGeneration, ManifestHash                             string
	WorkStopped, AllGrantsRevoked, WorkspaceCleared, OutputKeysHandled bool
}

func (s *Store) FinishRentalSessionCleanup(ctx context.Context, h Hold, c RentalSessionCleanup) error {
	if c.Session != h.owner || c.LocalGeneration != h.local || !executionlease.Hash(c.ManifestHash) || !c.WorkStopped || !c.AllGrantsRevoked || !c.WorkspaceCleared || !c.OutputKeysHandled {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		r, e := readResource(ctx, tx, h.resource)
		if e != nil {
			return e
		}
		if o.phase != "CLEANING" || r.phase != "CLEANING" || !r.stopped {
			return ErrEvidence
		}
		var remaining bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM rental_invocations WHERE session_id=? AND phase<>'CLEANED') OR EXISTS(SELECT 1 FROM rental_grants WHERE session_id=? AND state='CURRENT')`, h.owner, h.owner).Scan(&remaining) != nil {
			return ErrStorage
		}
		if remaining {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, `UPDATE rental_owners SET phase='CLEANED',cleanup_hash=? WHERE session_id=?`, c.ManifestHash, h.owner); e != nil {
			return ErrStorage
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resources SET owner_id='',mode='',phase='FREE',stopped=0 WHERE resource_id=?`, h.resource); e != nil {
			return ErrStorage
		}
		delete(s.rentalGuards, h.owner)
		return nil
	})
}
