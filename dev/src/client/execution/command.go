package execution

import (
	"context"
	"database/sql"
	"errors"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

type CommandStatus struct {
	Found                        bool
	LeaseID, Phase, MetadataHash string
	CleanupHash                  string
	CurrentMetadataHash          string
	Revision                     int64
}

// CommandStatus is an exact original-command projection for host recovery. It
// supplies no execution guard and never releases or creates a resource owner.
func (s *Store) CommandStatus(ctx context.Context, id string) (CommandStatus, error) {
	var out CommandStatus
	if !ids.Valid(id) {
		return out, ErrFence
	}
	e := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		var raw, cleanup string
		e := tx.QueryRowContext(ctx, `SELECT i.lease_id,i.phase,i.metadata_hash,r.metadata_hash,i.metadata_json,i.cleanup_hash FROM receipts r JOIN inbox i ON i.lease_id=r.lease_id WHERE r.command_id=? AND r.kind='RECEIVE'`, id).Scan(&out.LeaseID, &out.Phase, &out.CurrentMetadataHash, &out.MetadataHash, &raw, &cleanup)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil || !executionlease.Hash(out.MetadataHash) || !executionlease.Hash(out.CurrentMetadataHash) || digest([]byte(raw)) != out.CurrentMetadataHash || out.Phase == "CLEANED" && !executionlease.Hash(cleanup) {
			return ErrStorage
		}
		var current executionlease.Lease
		if strictjson.Decode([]byte(raw), &current, executionlease.MaximumBody) != nil || current.ID != out.LeaseID || current.Revision < 1 {
			return ErrStorage
		}
		// RECEIVE is immutable original-command identity. A continuation changes
		// the inbox metadata, with its own RENEW receipt, never that identity.
		if out.CurrentMetadataHash != out.MetadataHash {
			var renewed bool
			if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM receipts WHERE lease_id=? AND kind='RENEW' AND metadata_hash=?)`, out.LeaseID, out.CurrentMetadataHash).Scan(&renewed) != nil || !renewed || current.Revision < 2 {
				return ErrStorage
			}
		}
		out.Found = true
		out.Revision = current.Revision
		out.CleanupHash = cleanup
		return nil
	})
	return out, e
}
