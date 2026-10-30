package execution

import (
	"context"
	"database/sql"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
)

// ResultBinding verifies durable ORIGINAL inbox identity even after its
// resource is FREE or owned by another generation. It grants no execution,
// renewal, billing or trusted usage authority and never reads current owners.
func (s *Store) ResultBinding(ctx context.Context, h Hold, m executionlease.Manifest) error {
	if h.mode != "JOB" || h.owner == "" || h.local == "" || h.resource != m.Resource {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		var resource, local, raw, phase string
		if tx.QueryRowContext(ctx, `SELECT resource_id,local_generation,metadata_json,phase FROM inbox WHERE lease_id=?`, h.owner).Scan(&resource, &local, &raw, &phase) != nil {
			return ErrFence
		}
		var l executionlease.Lease
		if resource != h.resource || local != h.local || phase == "RECEIVED" || strictjson.Decode([]byte(raw), &l, executionlease.MaximumBody) != nil || l.ID != h.owner || l.Resource != h.resource || l.ManifestHash != m.Digest() || l.Environment != m.EnvironmentDigest || l.Capability != m.Capability {
			return ErrFence
		}
		return nil
	})
}
