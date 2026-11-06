package node

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
)

// One bounded pending write and its receipt replace an unbounded progress log.
// Only signed metadata is persisted; original Lease/Manifest remain immutable.
type hostProgress struct {
	Write        devicecontrol.ProgressWrite `json:"write"`
	Ack          *devicecontrol.ProgressAck  `json:"ack,omitempty"`
	Applied      bool                        `json:"applied"`
	Disposition  string                      `json:"disposition,omitempty"`
	CurrentLease json.RawMessage             `json:"current_lease,omitempty"`
	Stopped      bool                        `json:"stopped"`
}

type ProgressStatus struct {
	Sequence    string `json:"sequence"`
	Action      string `json:"action"`
	Disposition string `json:"disposition"`
	Revision    int64  `json:"lease_revision"`
	Stopped     bool   `json:"stopped"`
}

func progressKey(id, sequence string) string {
	return "hp_" + hostDigest([]byte("P11_HOST_PROGRESS_1\n"+id+"\n"+sequence+"\n"))
}

func progressPhase(phase string) int {
	switch phase {
	case "RUNNING":
		return 1
	case "FAILED", "CANCELED":
		return 2
	}
	return 0
}

func (h *Host) originalProgress(c hostCommand) (devicecontrol.AcceptedDelivery, error) {
	var l executionlease.Lease
	if json.Unmarshal(c.Lease, &l) != nil {
		return devicecontrol.AcceptedDelivery{}, ErrHost
	}
	i, err := time.Parse(time.RFC3339Nano, l.Issued)
	if err != nil {
		return devicecontrol.AcceptedDelivery{}, ErrHost
	}
	return h.accept(c, devicecontrol.Reply{Status: 200, Body: c.Lease}, devicecontrol.Reply{Status: 200, Body: c.Manifest}, i)
}

func (h *Host) continuation(a devicecontrol.AcceptedDelivery, l executionlease.Lease, now time.Time) (executionlease.Verified, error) {
	v, err := executionlease.Verify(canonicalHostJSON(l), h.config.pin(), a.Manifest().ApprovedPlan(), now)
	if err != nil || !executionlease.SameOwner(a.Verified().Details(), l) {
		return executionlease.Verified{}, ErrHostPolicy
	}
	return v, nil
}

func (h *Host) validateProgress(c hostCommand) bool {
	p := c.Progress
	if p == nil {
		return true
	}
	if c.Phase != "SUBMITTING" && c.Phase != "SUBMITTED" && c.Phase != "CLEANED" {
		return false
	}
	a, err := h.originalProgress(c)
	if err != nil {
		return false
	}
	original := a.Verified().Details()
	if p.Write.Validate() != nil || progressPhase(p.Write.Phase) == 0 || p.Write.Fraction != nil || p.Write.Lease != original.ID || p.Write.Fence != original.Fence || p.Write.Reason != "" && p.Write.Reason != "NATIVE_EXECUTION_FAILED" && p.Write.Reason != "NATIVE_EXECUTION_CANCELED" {
		return false
	}
	current := original
	if len(p.CurrentLease) != 0 {
		if len(p.CurrentLease) > executionlease.MaximumBody || json.Unmarshal(p.CurrentLease, &current) != nil {
			return false
		}
		i, e := time.Parse(time.RFC3339Nano, current.Issued)
		if e != nil {
			return false
		}
		if _, e = h.continuation(a, current, i); e != nil || current.Revision <= original.Revision {
			return false
		}
	}
	if p.Ack == nil {
		return !p.Applied && p.Disposition == "" && !p.Stopped
	}
	ack, err := devicecontrol.ProgressReceipt(devicecontrol.Reply{Status: 200, Body: canonicalHostJSON(p.Ack)})
	if err != nil {
		return false
	}
	if progressPhase(p.Write.Phase) == 2 && ack.Lease != nil {
		return false
	}
	if ack.Lease != nil {
		i, e := time.Parse(time.RFC3339Nano, ack.Lease.Issued)
		if e != nil {
			return false
		}
		if _, e = h.continuation(a, *ack.Lease, i); e != nil {
			return false
		}
		if p.Applied && p.Disposition == "RENEWED" {
			if string(canonicalHostJSON(current)) != string(canonicalHostJSON(*ack.Lease)) {
				return false
			}
		} else if ack.Lease.Revision <= current.Revision {
			return false
		}
	}
	if !p.Applied {
		return p.Disposition == "" && !p.Stopped
	}
	switch p.Disposition {
	case "RENEWED":
		return ack.Action == "CONTINUE" && !p.Stopped
	case "NOT_APPLIED":
		return ack.Action == "CONTINUE" && p.Stopped
	case "STOPPED":
		return ack.Action == "STOP" && p.Stopped
	case "RECORDED":
		return ack.Action == "NO_CHANGE" && !p.Stopped
	case "LOCAL_STOPPED":
		return ack.Action == "NO_CHANGE" && p.Stopped && progressPhase(p.Write.Phase) == 2
	}
	return false
}

func progressStatus(c hostCommand) ProgressStatus {
	p := c.Progress
	if p == nil {
		return ProgressStatus{}
	}
	v := ProgressStatus{Sequence: p.Write.Sequence, Disposition: p.Disposition, Revision: 1, Stopped: p.Stopped}
	if p.Ack != nil {
		v.Action = p.Ack.Action
	}
	if len(p.CurrentLease) != 0 {
		var l executionlease.Lease
		_ = json.Unmarshal(p.CurrentLease, &l)
		v.Revision = l.Revision
	}
	return v
}

// ReportProgress first drains a persisted original write. A changed phase cannot
// replace an ambiguous request/sequence. Only a later call can issue its next
// sequence, and a signed STOP or lost live guard permanently closes renewal.
func (h *Host) ReportProgress(ctx context.Context, id, phase string) (ProgressStatus, error) {
	if progressPhase(phase) == 0 {
		return ProgressStatus{}, ErrHostPolicy
	}
	owned, finish, err := h.beginWork(ctx, id)
	if err != nil {
		return ProgressStatus{}, err
	}
	defer finish()
	c, err := h.command(owned, id)
	if err != nil {
		return ProgressStatus{}, err
	}
	a, err := h.originalProgress(c)
	if err != nil {
		return ProgressStatus{}, err
	}
	l := a.Verified().Details()
	status, err := h.ledger.CommandStatus(owned, id)
	if err != nil {
		h.fail()
		return ProgressStatus{}, ErrHost
	}
	if !status.Found || status.LeaseID != l.ID || status.MetadataHash != hostDigest(c.Lease) {
		return ProgressStatus{}, ErrHostPending
	}
	if c.Progress != nil && c.Progress.Stopped {
		return progressStatus(c), nil
	}
	if c.Progress == nil || c.Progress.Applied {
		if status.Phase == "CLEANED" {
			return progressStatus(c), ErrHostPending
		}
		seq := uint64(1)
		var current json.RawMessage
		if c.Progress != nil {
			previous, _ := strconv.ParseUint(c.Progress.Write.Sequence, 10, 63)
			if previous == math.MaxInt64 {
				return ProgressStatus{}, ErrCounter
			}
			if progressPhase(phase) < progressPhase(c.Progress.Write.Phase) || progressPhase(c.Progress.Write.Phase) == 2 && phase != c.Progress.Write.Phase {
				return ProgressStatus{}, ErrHostPolicy
			}
			seq = previous + 1
			current = append(json.RawMessage(nil), c.Progress.CurrentLease...)
		}
		p := &hostProgress{Write: devicecontrol.ProgressWrite{Lease: l.ID, Fence: l.Fence, Sequence: strconv.FormatUint(seq, 10), Phase: phase}, CurrentLease: current}
		if phase == "FAILED" {
			p.Write.Reason = "NATIVE_EXECUTION_FAILED"
		}
		if phase == "CANCELED" {
			p.Write.Reason = "NATIVE_EXECUTION_CANCELED"
		}
		if err = h.update(owned, id, func(c *hostCommand) error {
			if c.Progress != nil && !c.Progress.Applied {
				return ErrHostPending
			}
			c.Progress = p
			return nil
		}); err != nil {
			return ProgressStatus{}, err
		}
		c, err = h.command(owned, id)
		if err != nil {
			return ProgressStatus{}, err
		}
	}
	p := c.Progress
	key := progressKey(id, p.Write.Sequence)
	if progressPhase(p.Write.Phase) == 2 && status.Phase != "CLEANED" {
		r, e := h.ledger.Recovery(owned, l.ID)
		if e != nil {
			return ProgressStatus{}, e
		}
		if e = h.ledger.RequestStop(owned, r.Hold); e != nil {
			return ProgressStatus{}, e
		}
	}
	if p.Ack == nil {
		request, e := h.client.PrepareProgress(l.Attempt, key, p.Write)
		if e != nil {
			return ProgressStatus{}, ErrHost
		}
		reply, e := h.client.SendBounded(owned, request, 32768)
		defer clear(reply.Body)
		if e != nil {
			return ProgressStatus{}, e
		}
		if reply.Status != 200 {
			return ProgressStatus{}, classify(reply)
		}
		ack, e := devicecontrol.ProgressReceipt(reply)
		if e != nil {
			return ProgressStatus{}, &devicecontrol.TransportError{Unknown: true}
		}
		if progressPhase(p.Write.Phase) == 2 && ack.Lease != nil {
			return ProgressStatus{}, ErrHostPolicy
		}
		candidate := c
		copy := *p
		copy.Ack = &ack
		candidate.Progress = &copy
		if !h.validateProgress(candidate) {
			return ProgressStatus{}, ErrHostPolicy
		}
		// The receipt is durable before any new guard is installed. A crash can
		// replay only the same renewal key; a reopened ledger has no live guard.
		if err = h.update(owned, id, func(c *hostCommand) error {
			if c.Progress == nil || c.Progress.Write.Sequence != p.Write.Sequence || c.Progress.Ack != nil {
				return ErrHostPending
			}
			c.Progress.Ack = &ack
			return nil
		}); err != nil {
			return ProgressStatus{}, err
		}
		c, err = h.command(owned, id)
		if err != nil {
			return ProgressStatus{}, err
		}
		p = c.Progress
	}
	status, err = h.ledger.CommandStatus(owned, id)
	if err != nil {
		h.fail()
		return ProgressStatus{}, ErrHost
	}
	if !status.Found || status.MetadataHash != hostDigest(c.Lease) {
		return ProgressStatus{}, ErrHost
	}
	disposition := "RECORDED"
	stop := p.Ack.Action == "STOP"
	if progressPhase(p.Write.Phase) == 2 && p.Ack.Action == "NO_CHANGE" {
		disposition, stop = "LOCAL_STOPPED", true
	}
	var renewed json.RawMessage
	if p.Ack.Action == "CONTINUE" {
		disposition, stop = "NOT_APPLIED", true
		if status.Phase != "CLEANED" {
			recovery, e := h.ledger.Recovery(owned, l.ID)
			if e != nil {
				return ProgressStatus{}, e
			}
			live, e := h.ledger.Current(owned, recovery.Hold)
			if e != nil {
				return ProgressStatus{}, e
			}
			v, e := h.continuation(a, *p.Ack.Lease, time.Now())
			if e == nil && live.Authorized {
				e = h.ledger.Renew(owned, key, recovery.Hold, v)
				if e == nil {
					disposition, stop, renewed = "RENEWED", false, v.Metadata()
				}
				if e != nil && !errors.Is(e, execution.ErrRecovery) && !errors.Is(e, executionlease.ErrLease) {
					return ProgressStatus{}, e
				}
			}
		}
	}
	if p.Ack.Action == "STOP" {
		disposition = "STOPPED"
	}
	if stop && status.Phase != "CLEANED" {
		r, e := h.ledger.Recovery(owned, l.ID)
		if e != nil {
			return ProgressStatus{}, e
		}
		if e = h.ledger.RequestStop(owned, r.Hold); e != nil {
			return ProgressStatus{}, e
		}
	}
	err = h.update(owned, id, func(c *hostCommand) error {
		if c.Progress == nil || c.Progress.Write.Sequence != p.Write.Sequence || c.Progress.Ack == nil || c.Progress.Applied {
			return ErrHostPending
		}
		c.Progress.Applied, c.Progress.Disposition, c.Progress.Stopped = true, disposition, stop
		if len(renewed) != 0 {
			c.Progress.CurrentLease = renewed
		}
		return nil
	})
	if err != nil {
		return ProgressStatus{}, err
	}
	c, err = h.command(owned, id)
	return progressStatus(c), err
}
