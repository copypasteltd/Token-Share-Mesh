package node

import (
	"bytes"
	"context"
	"encoding/json"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/ids"
	"sync"
	"time"
)

type PreparedExecution struct {
	host         *Host
	id           string
	accepted     devicecontrol.AcceptedDelivery
	body         []byte
	mu           sync.Mutex
	used, closed bool
	life         context.Context
	cancel       context.CancelFunc
}

func (p *PreparedExecution) Manifest() executionlease.Manifest { return p.accepted.Manifest() }
func (p *PreparedExecution) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	clear(p.body)
	p.body = nil
	p.host.mu.Lock()
	if p.host.tickets[p.id] == p {
		delete(p.host.tickets, p.id)
	}
	p.host.mu.Unlock()
}
func (h *Host) beginWork(ctx context.Context, id string) (context.Context, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !ids.Valid(id) || len(id) < 8 || len(id) > 128 {
		return nil, nil, ErrHostPolicy
	}
	if h.closed || h.poisoned.Load() || ctx.Err() != nil {
		return nil, nil, ErrHost
	}
	if _, ok := h.busy[id]; ok {
		return nil, nil, ErrHostPending
	}
	if len(h.busy) >= execution.MaximumResources {
		return nil, nil, ErrHostPending
	}
	owned, cancel := context.WithCancel(ctx)
	h.busy[id] = cancel
	h.workers.Add(1)
	return owned, func() { cancel(); h.mu.Lock(); delete(h.busy, id); h.mu.Unlock(); h.workers.Done() }, nil
}
func (h *Host) accept(c hostCommand, claim, manifest devicecontrol.Reply, now time.Time) (devicecontrol.AcceptedDelivery, error) {
	a, e := h.config.approve(manifest)
	if e != nil {
		return devicecontrol.AcceptedDelivery{}, e
	}
	accepted, e := devicecontrol.AcceptDelivery(claim, manifest, h.config.pin(), a, now)
	if e != nil || accepted.Manifest().Capability != c.Claim.Capability || accepted.Manifest().Device != h.device || accepted.Manifest().Seller != h.seller {
		return devicecontrol.AcceptedDelivery{}, ErrHostPolicy
	}
	return accepted, nil
}
func (h *Host) resolve(ctx context.Context, id string) (devicecontrol.AcceptedDelivery, error) {
	var zero devicecontrol.AcceptedDelivery
	c, e := h.command(ctx, id)
	if e != nil {
		return zero, e
	}
	if c.Phase == "CLEANED" {
		return zero, ErrHostPending
	}
	request, e := h.client.PrepareLeaseClaim(c.Claim)
	if e != nil {
		return zero, ErrHost
	}
	claim, e := h.client.SendBounded(ctx, request, 16384)
	if e != nil {
		return zero, e
	}
	defer clear(claim.Body)
	if claim.Status != 200 {
		return zero, classify(claim)
	}
	raw, e := devicecontrol.UnverifiedClaim(claim)
	if e != nil {
		return zero, ErrHostPolicy
	}
	defer clear(raw)
	var lease executionlease.Lease
	if json.Unmarshal(raw, &lease) != nil || lease.Device != h.device || lease.Seller != h.seller || lease.Capability != c.Claim.Capability {
		return zero, ErrHostPolicy
	}
	canonical := canonicalHostJSON(lease)
	if e = h.update(ctx, id, func(current *hostCommand) error {
		if len(current.Lease) != 0 && !bytes.Equal(current.Lease, canonical) {
			return ErrHostPolicy
		}
		if current.Phase == "CLEANED" {
			return ErrHostPending
		}
		current.Lease = canonical
		if current.Phase == "CLAIM_PENDING" {
			current.Phase = "CLAIMED"
		}
		return nil
	}); e != nil {
		return zero, e
	}
	request, e = h.client.PrepareManifest(lease.ID)
	if e != nil {
		return zero, ErrHost
	}
	manifest, e := h.client.SendBounded(ctx, request, 16384)
	if e != nil {
		return zero, e
	}
	defer clear(manifest.Body)
	accepted, e := h.accept(c, claim, manifest, time.Now())
	if e != nil {
		return zero, e
	}
	m := canonicalHostJSON(accepted.Manifest())
	if e = h.update(ctx, id, func(current *hostCommand) error {
		if !bytes.Equal(current.Lease, accepted.Verified().Metadata()) || len(current.Manifest) != 0 && !bytes.Equal(current.Manifest, m) {
			return ErrHostPolicy
		}
		if current.Phase == "CLEANED" {
			return ErrHostPending
		}
		current.Manifest = m
		if current.Phase == "CLAIMED" {
			current.Phase = "VERIFIED"
		}
		return nil
	}); e != nil {
		return zero, e
	}
	return accepted, nil
}

// Prepare replays the already persisted original claim with fresh device proof
// nonces, rechecks the manifest and fetches the original input once. It does not
// reserve an execution receipt or submit. Close the returned in-memory ticket.
func (h *Host) Prepare(ctx context.Context, id string) (*PreparedExecution, error) {
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return nil, e
	}
	defer finish()
	h.mu.Lock()
	existing := h.tickets[id] != nil
	h.mu.Unlock()
	if existing {
		return nil, ErrHostPending
	}
	accepted, e := h.resolve(owned, id)
	if e != nil {
		return nil, e
	}
	request, e := h.client.PrepareInput(accepted.Verified().Details().ID)
	if e != nil {
		return nil, ErrHost
	}
	r, e := h.client.SendBounded(owned, request, 256<<10)
	if e != nil {
		return nil, e
	}
	defer clear(r.Body)
	if r.Status != 200 {
		return nil, classify(r)
	}
	body, e := accepted.AcceptInput(r, time.Now())
	if e != nil {
		return nil, ErrHostPolicy
	}
	life, cancel := context.WithCancel(context.Background())
	p := &PreparedExecution{host: h, id: id, accepted: accepted, body: body, life: life, cancel: cancel}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.poisoned.Load() || owned.Err() != nil || len(h.tickets) >= execution.MaximumResources || h.tickets[id] != nil {
		clear(body)
		cancel()
		return nil, ErrHostPending
	}
	h.tickets[id] = p
	return p, nil
}

// Start persists a submission intent before entering the native execution
// inbox. The inbox remains authoritative for once-only Submit and both fences.
// A replay after any unknown result cannot call InputAdapter.SubmitInput twice.
func (p *PreparedExecution) Start(ctx context.Context, watch *supervisor.Supervisor) (execution.Hold, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.used || watch == nil || !watch.BoundTo(p.host.ledger) {
		return execution.Hold{}, ErrHostPolicy
	}
	p.host.mu.Lock()
	bound := p.host.watchdogs[watch]
	p.host.mu.Unlock()
	if !bound {
		return execution.Hold{}, ErrHostPolicy
	}
	owned, finish, e := p.host.beginWork(ctx, p.id)
	if e != nil {
		return execution.Hold{}, e
	}
	defer finish()
	active, interrupt := context.WithCancel(owned)
	defer interrupt()
	owned = active
	cancelOnClose := context.AfterFunc(p.life, interrupt)
	defer cancelOnClose()
	p.used = true
	defer func() { clear(p.body); p.body = nil }()
	if e = p.host.update(owned, p.id, func(c *hostCommand) error {
		if c.Phase == "CLEANED" || !bytes.Equal(c.Lease, p.accepted.Verified().Metadata()) || !bytes.Equal(c.Manifest, canonicalHostJSON(p.accepted.Manifest())) {
			return ErrHostPolicy
		}
		c.Phase = "SUBMITTING"
		return nil
	}); e != nil {
		return execution.Hold{}, e
	}
	hold, e := watch.SubmitInput(owned, p.id, p.accepted, p.body)
	if e != nil {
		return hold, e
	}
	if e = p.host.update(owned, p.id, func(c *hostCommand) error {
		if c.Phase != "CLEANED" {
			c.Phase = "SUBMITTED"
		}
		return nil
	}); e != nil {
		return hold, e
	}
	return hold, nil
}
