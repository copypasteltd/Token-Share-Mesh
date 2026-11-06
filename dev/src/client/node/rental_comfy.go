package node

import (
	"context"

	"p11.local/compute/client/adapters/comfyworker"
	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/protocol/rentalaccess"
)

// OpenPreparedComfy uses only this provider's actual preparation and this
// original node's already-open approved registry/physical ledger. Registry
// shutdown retains the node lock/root/key until actual native wait and pipes
// join. This is a local prepared engine, NOT platform sale/readiness approval,
// an invocation adapter, hourly metering, E2E or low-privilege qualification.
func (r *ProviderRental) OpenPreparedComfy(ctx context.Context, p comfyenv.Plan, selection comfyworker.RuntimeSelection) (*comfyworker.Process, error) {
	if r == nil || r.node == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrRentalJournal
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed || !r.journal.matchesPreparation(r.prepared) || r.prepared.Details().Engine != "COMFYUI" {
		return nil, ErrRentalJournal
	}
	use, e := r.node.OpenComfyUse(ctx, r.hold, r.prepared, p)
	if e != nil {
		return nil, e
	}
	process, e := comfyworker.OpenPrepared(ctx, use, selection)
	if e != nil {
		use.Close()
		return nil, e
	}
	return process, nil
}

// BindPreparedComfyMedia adds typed child media and owned workspace/result
// custody to this SAME original prepared process. Authorization remains a
// separate verified-grant step. No SDK listener/session ready/meter is opened.
func (r *ProviderRental) BindPreparedComfyMedia(process *comfyworker.Process, policy rentalaccess.MediaPolicy) (*comfyworker.RentalMediaAdapter, error) {
	if r == nil || r.node == nil || !process.SamePreparation(r.hold, r.prepared) {
		return nil, ErrRentalJournal
	}
	r.mu.RLock()
	closed := r.closed
	r.mu.RUnlock()
	if closed || !r.journal.matchesPreparation(r.prepared) {
		return nil, ErrRentalJournal
	}
	node := r.node
	node.nativeMu.Lock()
	defer node.nativeMu.Unlock()
	if node.closingNative || node.root == nil || node.poisoned || node.media == nil || node.execution != r.ledger {
		return nil, ErrRentalJournal
	}
	return comfyworker.NewRentalMediaAdapter(process, r.ledger, node.media, policy)
}
