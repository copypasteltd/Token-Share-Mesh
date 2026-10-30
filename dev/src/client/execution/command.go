package execution

import (
	"context"
	"database/sql"
	"errors"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

type CommandStatus struct {
	Found                        bool
	LeaseID, Phase, MetadataHash string
}

// CommandStatus is an exact original-command projection for host recovery. It
// supplies no execution guard and never releases or creates a resource owner.
func (s *Store) CommandStatus(ctx context.Context, id string) (CommandStatus, error) {
	var out CommandStatus
	if !ids.Valid(id) {
		return out, ErrFence
	}
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		var receipt, cleanup string
		e := tx.QueryRowContext(ctx, `SELECT i.lease_id,i.phase,i.metadata_hash,r.metadata_hash,i.cleanup_hash FROM receipts r JOIN inbox i ON i.lease_id=r.lease_id WHERE r.command_id=? AND r.kind='RECEIVE'`, id).Scan(&out.LeaseID, &out.Phase, &out.MetadataHash, &receipt, &cleanup)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil || out.MetadataHash != receipt || !executionlease.Hash(out.MetadataHash) || out.Phase == "CLEANED" && !executionlease.Hash(cleanup) {
			return ErrStorage
		}
		out.Found = true
		return nil
	})
	return out, e
}
