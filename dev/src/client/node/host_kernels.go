package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"time"
)

// HostKernel is supplied by trusted embedding/installed-kernel code. Incoming
// jobs cannot provide Backend, argv, URLs or substitute another environment.
// This is an adapter binding, not a signed package installer or hardware probe.
type HostKernel struct {
	Environment, Capability, EngineKind, EngineRelease, ModelDigest, RecipeDigest string
	Backend                                                                       supervisor.InputAdapter
	native                                                                        *NativeSession
}

func kernelMatches(k HostKernel, a devicecontrol.Approval) bool {
	return (k.native == nil || k.native.matches(a)) && k.Backend != nil && k.Environment == a.EnvironmentDigest && k.Capability == a.Capability && k.EngineKind == a.EngineKind && k.EngineRelease == a.EngineRelease && k.ModelDigest == a.ModelDigest && k.RecipeDigest == a.RecipeDigest
}
func (h *Host) NewSupervisor(ctx context.Context, kernels []HostKernel, options supervisor.Options) (*supervisor.Supervisor, error) {
	if len(kernels) < 1 || len(kernels) > MaximumHostRules {
		return nil, ErrHostPolicy
	}
	seen := map[string]bool{}
	for _, k := range kernels {
		key := k.Environment + "\n" + k.Capability
		if seen[key] {
			return nil, ErrHostPolicy
		}
		seen[key] = true
		approved := false
		for _, r := range h.config.Rules {
			if kernelMatches(k, r.Approval) && (k.native == nil || k.native.host == h && k.native.physical == r.PhysicalDigest && k.Backend == k.native.backend) {
				approved = true
			}
		}
		if !approved {
			return nil, ErrHostPolicy
		}
	}
	var registrations []supervisor.Registration
	bindings := map[string]bool{}
	e := h.transaction(ctx, func(tx *sql.Tx) error {
		return h.visitCommands(ctx, tx, func(c hostCommand) error {
			if c.Phase == "CLAIM_PENDING" || c.Phase == "CLAIMED" || c.Phase == "CLEANED" {
				return nil
			}
			var lease executionlease.Lease
			if json.Unmarshal(c.Lease, &lease) != nil {
				return ErrHost
			}
			issued, e := time.Parse(time.RFC3339Nano, lease.Issued)
			if e != nil {
				return ErrHost
			}
			// Past metadata is verified only to recover an ORIGINAL backend.
			// It grants no live guard, renewal or fresh Submit after reopening.
			accepted, e := h.accept(c, devicecontrol.Reply{Status: 200, Body: c.Lease}, devicecontrol.Reply{Status: 200, Body: c.Manifest}, issued)
			if e != nil {
				return ErrHost
			}
			m := accepted.Manifest()
			for _, k := range kernels {
				a, e := h.config.approve(devicecontrol.Reply{Status: 200, Body: c.Manifest})
				if e != nil {
					return ErrHost
				}
				if kernelMatches(k, a) {
					key := m.EnvironmentDigest + "\n" + m.Capability + "\n" + m.Digest()
					if !bindings[key] {
						registrations = append(registrations, supervisor.Registration{Environment: m.EnvironmentDigest, Capability: m.Capability, ManifestHash: m.Digest(), Backend: k.Backend})
						bindings[key] = true
					}
				}
			}
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	if len(registrations) == 0 {
		return nil, ErrHostPending
	}
	watch, e := supervisor.New(h.ledger, registrations, options)
	if e != nil {
		return nil, e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.nativeSession != nil {
		for _, k := range kernels {
			if k.native != h.nativeSession {
				return nil, ErrHostPolicy
			}
		}
	}
	if h.closed || h.poisoned.Load() {
		return nil, ErrHost
	}
	for old := range h.watchdogs {
		select {
		case <-old.Done():
			delete(h.watchdogs, old)
		default:
		}
	}
	// One frozen watchdog per host. Rebinding requires the previous run to
	// finish original cleanup; an unstarted watchdog must also be drained by
	// running it with a cancelled context rather than silently abandoned.
	if len(h.watchdogs) != 0 {
		return nil, ErrHostPending
	}
	h.watchdogs[watch] = true
	return watch, nil
}
