package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/protocol/rentalaccess"
)

const MaximumMediaWorkspaceBytes int64 = 130 << 20
const MaximumMediaReservedBytes int64 = 2 << 30

var mediaBasename = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)

// No plaintext fingerprint, content key, file receipt or caller-selected path.
type MediaNamespace struct{ ID, AnchorHash, Phase string }
type MediaWorkspace struct {
	Invocation, Session, Namespace, Resource, LocalGeneration                            string
	Grant, GrantDigest, ManifestHash, EnvironmentHash, RecipeHash, InputCiphertextDigest string
	Basename, AnchorHash, Phase, CleanupHash                                             string
	ReservedBytes                                                                        int64
}

type MediaNamespaceTicket struct {
	mu    sync.Mutex
	value MediaNamespace
	taken bool
}

func (t *MediaNamespaceTicket) Take() (MediaNamespace, error) {
	if t == nil {
		return MediaNamespace{}, ErrQuery
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.taken {
		return MediaNamespace{}, ErrQuery
	}
	t.taken = true
	return t.value, nil
}

type MediaWorkspaceTicket struct {
	mu    sync.Mutex
	value MediaWorkspace
	taken bool
}

func (t *MediaWorkspaceTicket) Take() (MediaWorkspace, error) {
	if t == nil {
		return MediaWorkspace{}, ErrQuery
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.taken {
		return MediaWorkspace{}, ErrQuery
	}
	t.taken = true
	return t.value, nil
}

type MediaRemovalTicket struct {
	mu    sync.Mutex
	value MediaWorkspace
	taken bool
}

func (t *MediaRemovalTicket) Take() (MediaWorkspace, error) {
	if t == nil {
		return MediaWorkspace{}, ErrQuery
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.taken {
		return MediaWorkspace{}, ErrQuery
	}
	t.taken = true
	return t.value, nil
}

func mediaNamespaceTx(ctx context.Context, tx *sql.Tx) (MediaNamespace, error) {
	var n MediaNamespace
	e := tx.QueryRowContext(ctx, "SELECT namespace_id,anchor_hash,phase FROM media_namespace WHERE singleton=1").Scan(&n.ID, &n.AnchorHash, &n.Phase)
	if errors.Is(e, sql.ErrNoRows) {
		return n, ErrQuery
	}
	if e != nil {
		return n, ErrStorage
	}
	return n, nil
}

// Initialization reserves identity BEFORE any namespace directory exists.
func (s *Store) BeginMediaNamespace(ctx context.Context, id, anchor string) (*MediaNamespaceTicket, error) {
	if !executionlease.Hash(id) || !executionlease.Hash(anchor) {
		return nil, ErrFence
	}
	n := MediaNamespace{ID: id, AnchorHash: anchor, Phase: "CREATING"}
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		var exists bool
		if tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_namespace)").Scan(&exists) != nil {
			return ErrStorage
		}
		if exists {
			return ErrRecovery
		}
		if _, e := tx.ExecContext(ctx, "INSERT INTO media_namespace(singleton,namespace_id,anchor_hash,phase) VALUES(1,?,?,'CREATING')", id, anchor); e != nil {
			return ErrStorage
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return &MediaNamespaceTicket{value: n}, nil
}
func (s *Store) MediaNamespace(ctx context.Context) (MediaNamespace, error) {
	var n MediaNamespace
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error { var e error; n, e = mediaNamespaceTx(ctx, tx); return e })
	return n, e
}
func (s *Store) ConfirmMediaNamespace(ctx context.Context, n MediaNamespace) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		old, e := mediaNamespaceTx(ctx, tx)
		if e != nil {
			return e
		}
		if old.ID != n.ID || old.AnchorHash != n.AnchorHash || old.Phase != "CREATING" || n.Phase != "CREATING" {
			return ErrFence
		}
		if _, e = tx.ExecContext(ctx, "UPDATE media_namespace SET phase='READY' WHERE singleton=1"); e != nil {
			return ErrStorage
		}
		return nil
	})
}
func readMediaWorkspace(ctx context.Context, tx *sql.Tx, h Hold, id string) (MediaWorkspace, error) {
	var w MediaWorkspace
	e := tx.QueryRowContext(ctx, `SELECT invocation_id,session_id,namespace_id,resource_id,local_generation,grant_id,grant_digest,manifest_hash,environment_hash,recipe_hash,input_ciphertext_digest,basename,anchor_hash,reserved_bytes,phase,cleanup_hash FROM rental_media_workspaces WHERE invocation_id=?`, id).Scan(&w.Invocation, &w.Session, &w.Namespace, &w.Resource, &w.LocalGeneration, &w.Grant, &w.GrantDigest, &w.ManifestHash, &w.EnvironmentHash, &w.RecipeHash, &w.InputCiphertextDigest, &w.Basename, &w.AnchorHash, &w.ReservedBytes, &w.Phase, &w.CleanupHash)
	if errors.Is(e, sql.ErrNoRows) {
		return w, ErrQuery
	}
	if e != nil {
		return w, ErrStorage
	}
	if h.mode != "RENTAL" || w.Session != h.owner || w.Resource != h.resource || w.LocalGeneration != h.local {
		return MediaWorkspace{}, ErrFence
	}
	return w, nil
}
func (s *Store) RentalMediaWorkspace(ctx context.Context, h Hold, id string) (MediaWorkspace, error) {
	var w MediaWorkspace
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		var e error
		w, e = readMediaWorkspace(ctx, tx, h, id)
		return e
	})
	return w, e
}
func mediaAuthorized(s *Store, ctx context.Context, tx *sql.Tx, h Hold, expected rentalaccess.Expected, stamp Stamp) error {
	o, e := rentalOwnerTx(ctx, tx, h)
	if e != nil {
		return e
	}
	if o.manifest.Engine != "COMFYUI" {
		return ErrFence
	}
	r, e := readResource(ctx, tx, h.resource)
	if e != nil {
		return e
	}
	g, live := s.rentalGuards[h.owner]
	if s.clockUnsafe || !live || !g.allowed || g.expected != expected || o.phase != "ACTIVE" || r.phase != "EXECUTING" || r.stopped || o.grant != expected.Grant || o.digest != expected.TokenDigest || o.manifestHash != expected.ManifestHash || o.physical != g.physical || !g.grant.Matches(expected, stamp.Wall) || !g.grant.Allows("INVOKE", stamp.Wall) || !stamp.Wall.Before(g.clock.expires) || stamp.Elapsed >= g.clock.until || !time.Now().Before(g.clock.deadline) {
		return ErrRecovery
	}
	return nil
}

// A replay is QUERY_ONLY. It never mints another directory-creation ticket.
func (s *Store) ReserveRentalMediaWorkspace(ctx context.Context, h Hold, namespace, id string, current rentalaccess.Expected, basename, anchor string, reserved int64) (*MediaWorkspaceTicket, MediaWorkspace, string, error) {
	var w MediaWorkspace
	disposition := ""
	if !executionlease.Hash(namespace) || !executionlease.Hash(anchor) || !mediaBasename.MatchString(basename) || reserved <= 0 || reserved > MaximumMediaWorkspaceBytes {
		return nil, w, "", ErrFence
	}
	e := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		old, e := readMediaWorkspace(ctx, tx, h, id)
		if e == nil {
			if old.Namespace != namespace || old.Grant != current.Grant || old.GrantDigest != current.TokenDigest || old.ManifestHash != current.ManifestHash || old.EnvironmentHash != current.EnvironmentHash || old.ReservedBytes != reserved {
				return ErrFence
			}
			w = old
			disposition = "QUERY_ONLY"
			return nil
		}
		if !errors.Is(e, ErrQuery) {
			return e
		}
		if e = mediaAuthorized(s, ctx, tx, h, current, stamp); e != nil {
			return e
		}
		n, e := mediaNamespaceTx(ctx, tx)
		if e != nil {
			return e
		}
		if n.ID != namespace || n.Phase != "READY" {
			return ErrRecovery
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.Phase != "SUBMITTING" || v.BackendID != "" || v.Grant != current.Grant || v.GrantDigest != current.TokenDigest {
			return ErrFence
		}
		var total int64
		var count int
		if tx.QueryRowContext(ctx, "SELECT coalesce(sum(reserved_bytes),0) FROM rental_media_workspaces WHERE phase<>'CLEANED'").Scan(&total) != nil || tx.QueryRowContext(ctx, "SELECT count(*) FROM rental_media_workspaces").Scan(&count) != nil {
			return ErrStorage
		}
		if count >= 4096 || total > MaximumMediaReservedBytes-reserved {
			return ErrBusy
		}
		w = MediaWorkspace{Invocation: id, Session: h.owner, Namespace: namespace, Resource: h.resource, LocalGeneration: h.local, Grant: v.Grant, GrantDigest: v.GrantDigest, ManifestHash: current.ManifestHash, EnvironmentHash: current.EnvironmentHash, RecipeHash: v.RecipeDigest, InputCiphertextDigest: v.InputCiphertextDigest, Basename: basename, AnchorHash: anchor, ReservedBytes: reserved, Phase: "CREATING"}
		if _, e = tx.ExecContext(ctx, `INSERT INTO rental_media_workspaces(invocation_id,session_id,namespace_id,resource_id,local_generation,grant_id,grant_digest,manifest_hash,environment_hash,recipe_hash,input_ciphertext_digest,basename,anchor_hash,reserved_bytes,phase) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'CREATING')`, w.Invocation, w.Session, w.Namespace, w.Resource, w.LocalGeneration, w.Grant, w.GrantDigest, w.ManifestHash, w.EnvironmentHash, w.RecipeHash, w.InputCiphertextDigest, w.Basename, w.AnchorHash, w.ReservedBytes); e != nil {
			return ErrStorage
		}
		disposition = "WORKSPACE_RESERVED"
		return nil
	})
	if e != nil {
		return nil, MediaWorkspace{}, "", e
	}
	if disposition != "WORKSPACE_RESERVED" {
		return nil, w, disposition, nil
	}
	return &MediaWorkspaceTicket{value: w}, w, disposition, nil
}
func (s *Store) ConfirmRentalMediaWorkspace(ctx context.Context, h Hold, w MediaWorkspace) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		old, e := readMediaWorkspace(ctx, tx, h, w.Invocation)
		if e != nil {
			return e
		}
		if old != w || old.Phase != "CREATING" {
			return ErrFence
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='READY' WHERE invocation_id=?", w.Invocation); e != nil {
			return ErrStorage
		}
		return nil
	})
}
func (s *Store) ClaimRentalMediaWorkspace(ctx context.Context, h Hold, id string, current rentalaccess.Expected) (MediaWorkspace, error) {
	var w MediaWorkspace
	e := s.transaction(ctx, func(tx *sql.Tx, stamp Stamp) error {
		if e := mediaAuthorized(s, ctx, tx, h, current, stamp); e != nil {
			return e
		}
		var e error
		w, e = readMediaWorkspace(ctx, tx, h, id)
		if e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if w.Phase != "READY" || v.Phase != "SUBMITTING" || v.BackendID != "" || w.Grant != current.Grant || w.GrantDigest != current.TokenDigest || w.ManifestHash != current.ManifestHash || w.EnvironmentHash != current.EnvironmentHash {
			return ErrFence
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='ACTIVE' WHERE invocation_id=?", id); e != nil {
			return ErrStorage
		}
		w.Phase = "ACTIVE"
		return nil
	})
	return w, e
}

// This is a trusted-host evidence projection, never an HTTP acknowledgement.
// The filesystem owner calls it only AFTER child stop, all readers joined and
// all private keys closed. The original invocation must already be CLEANING.
func (s *Store) RetireRentalMediaWorkspace(ctx context.Context, h Hold, id string, c RentalChildCleanup) error {
	if c.Session != h.owner || c.Invocation != id || c.LocalGeneration != h.local || !c.WorkStopped || !c.FutureStartRevoked || !c.InputCleared || !c.OutputKeysHandled || !executionlease.Hash(c.ManifestHash) {
		return ErrEvidence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		o, e := rentalOwnerTx(ctx, tx, h)
		if e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		w, e := readMediaWorkspace(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if v.Phase != "CLEANING" || v.BackendID != c.BackendID || o.manifestHash != w.ManifestHash || w.Phase == "CLEANED" || w.Phase == "REMOVING" {
			return ErrEvidence
		}
		raw, _ := json.Marshal(struct {
			Workspace MediaWorkspace
			Proof     RentalChildCleanup
		}{w, c})
		proof := w.CleanupHash
		if proof == "" {
			proof = digest(raw)
		}
		if w.Phase == "RETIRED" {
			return nil
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='RETIRED',cleanup_hash=? WHERE invocation_id=?", proof, id); e != nil {
			return ErrStorage
		}
		return nil
	})
}
func (s *Store) BeginRentalMediaRemoval(ctx context.Context, h Hold, id string) (*MediaRemovalTicket, error) {
	var w MediaWorkspace
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		var e error
		w, e = readMediaWorkspace(ctx, tx, h, id)
		if e != nil {
			return e
		}
		v, e := readRentalInvocation(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if w.Phase != "RETIRED" || w.CleanupHash == "" || v.Phase != "CLEANING" {
			return ErrRecovery
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='REMOVING' WHERE invocation_id=?", id); e != nil {
			return ErrStorage
		}
		w.Phase = "REMOVING"
		return nil
	})
	if e != nil {
		return nil, e
	}
	return &MediaRemovalTicket{value: w}, nil
}

// Only the original filesystem owner passes the consumed ticket after actual
// removal + parent-directory sync. Missing files or timed-out handles do not.
func (s *Store) FinishRentalMediaRemoval(ctx context.Context, h Hold, w MediaWorkspace) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		old, e := readMediaWorkspace(ctx, tx, h, w.Invocation)
		if e != nil {
			return e
		}
		if old != w || w.Phase != "REMOVING" || w.CleanupHash == "" {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='CLEANED' WHERE invocation_id=?", w.Invocation); e != nil {
			return ErrStorage
		}
		return nil
	})
}
func (s *Store) QuarantineRentalMediaWorkspace(ctx context.Context, h Hold, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if _, e := rentalOwnerTx(ctx, tx, h); e != nil {
			return e
		}
		w, e := readMediaWorkspace(ctx, tx, h, id)
		if e != nil {
			return e
		}
		if w.Phase == "CLEANED" {
			return ErrEvidence
		}
		if _, e = tx.ExecContext(ctx, "UPDATE rental_media_workspaces SET phase='QUARANTINED' WHERE invocation_id=?", id); e != nil {
			return ErrStorage
		}
		return nil
	})
}
