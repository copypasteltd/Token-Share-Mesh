package execution

import (
	"context"
	"database/sql"
	"errors"

	"p11.local/compute/client/protocol/executionlease"
)

// RentalHistory is safe original-owner recovery metadata. After actual cleanup
// the current physical owner may be another person/task; this projection never
// borrows that new owner's authority and cannot query/cancel/submit its backend.
type RentalHistory struct{ Session, Resource, LocalGeneration, ManifestHash, ApprovalHash, Phase, CleanupHash string }

func (s *Store) RentalHistory(ctx context.Context, h Hold) (RentalHistory, error) {
	var out RentalHistory
	if h.mode != "RENTAL" {
		return out, ErrFence
	}
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		err := tx.QueryRowContext(ctx, `SELECT session_id,resource_id,local_generation,manifest_hash,approval_hash,phase,cleanup_hash FROM rental_owners WHERE session_id=? AND resource_id=? AND local_generation=?`, h.owner, h.resource, h.local).
			Scan(&out.Session, &out.Resource, &out.LocalGeneration, &out.ManifestHash, &out.ApprovalHash, &out.Phase, &out.CleanupHash)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFence
		}
		if err != nil {
			return ErrStorage
		}
		if !executionlease.Hash(out.ManifestHash) || !executionlease.Hash(out.ApprovalHash) || out.Phase == "CLEANED" && !executionlease.Hash(out.CleanupHash) {
			return ErrStorage
		}
		return nil
	})
	return out, err
}
