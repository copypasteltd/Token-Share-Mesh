package node

import (
	"context"
	"errors"

	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
)

// DiscardPublished retires only the ORIGINAL encrypted local text cache after
// the durable remote READY and original actual cleanup acknowledgement exist.
// Remote buyer read/acceptance scope survives. A next GPU owner is not queried,
// canceled or cleared. Unknown removal retains quota and its pending journal.
func (s *NativeSession) DiscardPublished(ctx context.Context, id string) error {
	s.mu.Lock()
	ready := !s.closing && !s.closed && s.backend != nil
	s.mu.Unlock()
	if !ready {
		return ErrHostPending
	}
	owned, finish, err := s.host.beginWork(ctx, id)
	if err != nil {
		return err
	}
	defer finish()
	c, err := s.host.command(owned, id)
	if err != nil {
		return err
	}
	if c.ResultRetired {
		return nil
	}
	if c.Phase != "CLEANED" || c.CleanupAck == nil || c.ResultContent == nil || !c.ResultContent.Ready || c.ResultDelivery == nil {
		return ErrHostPending
	}
	a, err := s.host.originalProgress(c)
	if err != nil {
		return err
	}
	l := a.Verified().Details()
	if l.Resource != s.resource || l.Capability != s.capability || l.Environment != s.info.Environment {
		return ErrHostPolicy
	}
	status, err := s.host.ledger.CommandStatus(owned, id)
	if err != nil {
		return err
	}
	if !status.Found || status.Phase != "CLEANED" || status.MetadataHash != hostDigest(c.Lease) {
		return ErrHostPending
	}
	r, err := s.host.ledger.Recovery(owned, l.ID)
	if err != nil {
		return err
	}
	w := supervisor.Work{RequestID: l.ID, Resource: l.Resource, LocalGeneration: r.Hold.LocalGeneration(), Environment: l.Environment}
	if err = s.backend.Discard(owned, w); err != nil {
		if errors.Is(err, results.ErrPending) {
			return ErrHostPending
		}
		return err
	}
	return s.host.update(owned, id, func(c *hostCommand) error {
		if c.Phase != "CLEANED" || c.CleanupAck == nil || c.ResultContent == nil || !c.ResultContent.Ready {
			return ErrHostPending
		}
		c.ResultRetired = true
		return nil
	})
}
