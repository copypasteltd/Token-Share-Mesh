package node

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
)

func (h *Host) validateCleanupReport(c hostCommand) bool {
	if c.CleanupReport == nil {
		return c.CleanupAck == nil
	}
	var l executionlease.Lease
	if c.Phase != "CLEANED" || json.Unmarshal(c.Lease, &l) != nil || c.CleanupReport.Validate() != nil || c.CleanupReport.State != "ACKED" || c.CleanupReport.Lease != l.ID || c.CleanupReport.Fence != l.Fence {
		return false
	}
	m, e := devicecontrol.CleanupMessage(h.device, l.Attempt, l.Generation, *c.CleanupReport)
	sig, _ := base64.RawURLEncoding.Strict().DecodeString(c.CleanupReport.Signature)
	key := h.node.key()
	defer clear(key)
	if e != nil || !ed25519.Verify(key.Public().(ed25519.PublicKey), m, sig) {
		return false
	}
	if c.CleanupAck != nil {
		b, _ := json.Marshal(c.CleanupAck)
		a, e := devicecontrol.CleanupAck(devicecontrol.Reply{Status: 200, Body: b})
		if e != nil || a.Status != "CLEANED" {
			return false
		}
	}
	return true
}

// ReportCleanup is an explicit, bounded original-command outbox drain. The
// existing local receipt must prove CLEANED before signing; unknown work is
// never converted into cleanup. Exact report/key bytes survive network loss
// and restart, while each HTTP retry gets a fresh device proof nonce. This
// method never submits, renews, runs a kernel or creates a financial fact.
func (h *Host) ReportCleanup(ctx context.Context, id string) (devicecontrol.Ack, error) {
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return devicecontrol.Ack{}, e
	}
	defer finish()
	if e = h.ReconcileCleanup(owned, id); e != nil {
		return devicecontrol.Ack{}, e
	}
	c, e := h.command(owned, id)
	if e != nil {
		return devicecontrol.Ack{}, e
	}
	o, e := h.ledger.CommandStatus(owned, id)
	if e != nil || !o.Found || o.Phase != "CLEANED" || o.MetadataHash != hostDigest(c.Lease) || !executionlease.Hash(o.CleanupHash) {
		return devicecontrol.Ack{}, ErrHostPending
	}
	if c.CleanupReport != nil && c.CleanupReport.ManifestHash != o.CleanupHash {
		h.fail()
		return devicecontrol.Ack{}, ErrHost
	}
	if c.CleanupAck != nil {
		return *c.CleanupAck, nil
	}
	var l executionlease.Lease
	if json.Unmarshal(c.Lease, &l) != nil || o.LeaseID != l.ID {
		return devicecontrol.Ack{}, ErrHost
	}
	if c.CleanupReport == nil {
		key := h.node.key()
		in, se := devicecontrol.SignCleanup(key, l, devicecontrol.CleanupWrite{Lease: l.ID, Fence: l.Fence, ManifestHash: o.CleanupHash, State: "ACKED", WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, Recorded: time.Now().UTC().Format(time.RFC3339Nano), Profile: devicecontrol.CleanupProfile})
		clear(key)
		if se != nil {
			return devicecontrol.Ack{}, ErrHost
		}
		if e = h.update(owned, id, func(current *hostCommand) error {
			if current.Phase != "CLEANED" || string(current.Lease) != string(c.Lease) || current.CleanupReport != nil {
				return ErrHostPending
			}
			current.CleanupReport = &in
			return nil
		}); e != nil {
			return devicecontrol.Ack{}, e
		}
		c, e = h.command(owned, id)
		if e != nil {
			return devicecontrol.Ack{}, e
		}
	}
	p, e := h.client.PrepareCleanup(l.Attempt, id, *c.CleanupReport)
	if e != nil {
		return devicecontrol.Ack{}, ErrHost
	}
	r, e := h.client.SendBounded(owned, p, 4096)
	if e != nil {
		return devicecontrol.Ack{}, e
	}
	defer clear(r.Body)
	if r.Status != 200 {
		return devicecontrol.Ack{}, classify(r)
	}
	a, e := devicecontrol.CleanupAck(r)
	if e != nil || a.Status != "CLEANED" {
		return devicecontrol.Ack{}, ErrHostPending
	}
	e = h.update(owned, id, func(current *hostCommand) error {
		if current.CleanupReport == nil || *current.CleanupReport != *c.CleanupReport || current.Phase != "CLEANED" {
			return ErrHost
		}
		current.CleanupAck = &a
		return nil
	})
	return a, e
}
