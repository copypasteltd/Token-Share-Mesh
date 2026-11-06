package comfyworker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/mediaworkspace"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/strictjson"
)

// RentalMediaAdapter is a typed original RENTAL child adapter. It owns the
// one process frame reader, serial media calls and their result keys. It is
// not a public SDK server, independent readiness, session supervisor/meter,
// paid-renewal service or platform owner/payment release authority.
type RentalMediaAdapter struct {
	mu                               sync.Mutex
	process                          *Process
	ledger                           *execution.Store
	owner                            *mediaworkspace.Owner
	hold                             execution.Hold
	prepared                         rentalaccess.Prepared
	policy                           rentalaccess.MediaPolicy
	active                           *MediaCall
	creating, closing, processJoined bool
	operations                       sync.WaitGroup
	done                             chan struct{}
	joined                           chan struct{}
	closeOnce                        sync.Once
	withdrawn, finalizing            bool
}
type MediaCall struct {
	cleaning                                                        bool
	adapter                                                         *RentalMediaAdapter
	invocation                                                      execution.RentalInvocation
	descriptor                                                      rentalaccess.MediaDescriptor
	expected                                                        rentalaccess.Expected
	workspace                                                       mediaworkspace.Workspace
	custody                                                         MediaCustody
	backend                                                         string
	accepted, result, stopped, unknown, senderDone, joined, cleaned bool
	done                                                            chan struct{}
}

func (c *MediaCall) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[original rental media call redacted]")
}
func (c *MediaCall) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// This consumes the Process frame-reader role once. It does not authorize it.
func NewRentalMediaAdapter(p *Process, ledger *execution.Store, owner *mediaworkspace.Owner, policy rentalaccess.MediaPolicy) (*RentalMediaAdapter, error) {
	if p == nil || ledger == nil || owner == nil {
		return nil, ErrOutput
	}
	h, prepared := p.use.Original()
	if !policy.Binds(prepared) {
		return nil, ErrOutput
	}
	a := &RentalMediaAdapter{process: p, ledger: ledger, owner: owner, hold: h, prepared: prepared, policy: policy, done: make(chan struct{}), joined: make(chan struct{})}
	p.mu.Lock()
	if p.mediaOwned || p.closing || p.failed {
		p.mu.Unlock()
		return nil, ErrOutput
	}
	p.mediaOwned = true
	p.mediaAdapter = a
	p.mu.Unlock()
	go a.receive()
	return a, nil
}
func (a *RentalMediaAdapter) bound(d rentalaccess.MediaDescriptor, current rentalaccess.Expected) bool {
	m := a.prepared.Details()
	return d.Profile == rentalaccess.MediaInputProfile && d.ID != "" && d.Session == m.Session && d.Resource == m.Resource && d.Generation == m.Generation && d.LocalGeneration == a.hold.LocalGeneration() && d.Grant == current.Grant && d.GrantDigest == current.TokenDigest && d.ManifestHash == a.prepared.Digest() && d.ManifestHash == current.ManifestHash && d.EnvironmentHash == m.EnvironmentHash && d.EnvironmentHash == current.EnvironmentHash && d.RecipeHash == m.RecipeHash && d.PolicyHash == a.policy.Digest() && d.Kind == a.policy.Kind && a.prepared.BindsGrant(current)
}

// Submit never creates a T1/job/attempt or a per-output charge. All refusal
// after original inbox reservation remains QUERY_ONLY; no fallback/retry.
func (a *RentalMediaAdapter) Submit(ctx context.Context, input *rentalaccess.MediaInput, current rentalaccess.Expected) (*MediaCall, error) {
	if a == nil || ctx == nil || ctx.Err() != nil || input == nil {
		return nil, ErrOutput
	}
	d := input.Descriptor()
	if !a.bound(d, current) {
		return nil, ErrOutput
	}
	a.mu.Lock()
	if a.closing || a.withdrawn || a.creating || a.active != nil && !a.active.cleaned {
		a.mu.Unlock()
		return nil, ErrOutput
	}
	a.creating = true
	a.operations.Add(1)
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.creating = false; a.mu.Unlock(); a.operations.Done() }()
	if _, e := a.ledger.CheckRentalExpected(ctx, a.hold, current, "INVOKE"); e != nil {
		return nil, ErrOutput
	}
	ticket, v, disposition, e := a.ledger.BeginRentalInvocation(ctx, a.hold, d.ID, d.InputCiphertextDigest, d.RecipeHash)
	if e != nil || disposition != "SUBMISSION_RESERVED" || ticket == nil {
		return nil, ErrOutput
	}
	reserved, e := ticket.Take()
	if e != nil || reserved != v {
		return nil, ErrOutput
	}
	workspace, e := a.owner.Create(ctx, a.hold, v.ID, current)
	if e != nil {
		return nil, ErrOutput
	}
	digest := sha256.Sum256([]byte(v.Session + "\x00" + v.ID + "\x00" + v.LocalGeneration))
	call := &MediaCall{adapter: a, invocation: v, descriptor: d, expected: current, workspace: workspace, backend: "comfy_" + hex.EncodeToString(digest[:16]), done: make(chan struct{})}
	a.mu.Lock()
	if a.closing || a.withdrawn || a.processJoined {
		a.mu.Unlock()
		workspace.Close()
		return nil, ErrOutput
	}
	a.active = call
	a.mu.Unlock()
	defer func() { a.mu.Lock(); call.senderDone = true; a.tryJoined(call); a.mu.Unlock() }()
	if workspace.Claim(ctx, current, call.done) != nil {
		a.fail(call)
		return call, ErrOutput
	}
	plain, e := input.Take(ctx, a.prepared, current, time.Now().UTC())
	input.Close()
	if e != nil {
		a.fail(call)
		return call, ErrOutput
	}
	defer plain.Close()
	assets, e := plain.Assets(ctx)
	if e != nil {
		a.fail(call)
		return call, ErrOutput
	}
	record := workspace.Record()
	e = workspace.WithLaunch(ctx, func(launch mediaworkspace.Launch) error {
		return plain.WithParameters(func(parameters []byte) error {
			hard, e := time.Parse(time.RFC3339Nano, current.HardDeadline)
			if e != nil {
				return ErrOutput
			}
			declarations := make([]map[string]any, 0, len(assets))
			for _, asset := range assets {
				declarations = append(declarations, map[string]any{"asset_id": asset.ID, "plaintext_bytes": asset.Bytes, "plaintext_sha256": asset.SHA256})
			}
			scope := Scope{Session: record.Session, Invocation: record.Invocation, LocalGeneration: record.LocalGeneration, Environment: record.EnvironmentHash, Recipe: record.RecipeHash}
			body, e := json.Marshal(map[string]any{"scope": scope, "hard_deadline_unix_ms": hard.UnixMilli(), "workspace_root": launch.Path, "workspace_anchor_sha256": launch.AnchorHash, "workspace_journal_key": base64.RawURLEncoding.EncodeToString(launch.JournalKey), "parameters": json.RawMessage(parameters), "assets": declarations})
			if e != nil {
				return ErrOutput
			}
			defer clear(body)
			if a.process.writeContent(ctx, Invoke, body) != nil {
				return ErrOutput
			}
			return nil
		})
	})
	if e != nil {
		a.fail(call)
		return call, ErrOutput
	}
	for _, asset := range assets {
		marker, e := json.Marshal(map[string]string{"asset_id": asset.ID})
		if e != nil {
			a.fail(call)
			return call, ErrOutput
		}
		if a.process.writeContent(ctx, AssetBegin, marker) != nil {
			clear(marker)
			a.fail(call)
			return call, ErrOutput
		}
		e = plain.StreamAsset(ctx, asset.ID, func(chunk []byte) error { return a.process.writeContent(ctx, AssetData, chunk) })
		if e != nil || a.process.writeContent(ctx, AssetEnd, marker) != nil {
			clear(marker)
			a.fail(call)
			return call, ErrOutput
		}
		clear(marker)
	}
	return call, nil
}
func (a *RentalMediaAdapter) tryJoined(c *MediaCall) {
	if c != nil && !c.joined && c.senderDone && (c.stopped || a.processJoined) {
		c.joined = true
		close(c.done)
	}
}
func (a *RentalMediaAdapter) fail(c *MediaCall) {
	a.mu.Lock()
	if c != nil {
		c.unknown = true
	}
	a.mu.Unlock()
	a.process.abort()
}
func (a *RentalMediaAdapter) receive() {
	defer close(a.done)
	for {
		frame, e := a.process.Next(context.Background())
		if e != nil {
			break
		}
		if frame.Kind() == Closed {
			frame.Close()
			break
		}
		raw, e := frame.Take()
		kind := frame.Kind()
		frame.Close()
		if e == nil {
			e = a.acceptFrame(kind, raw)
		}
		clear(raw)
		if e != nil {
			a.process.abort()
			break
		}
	}
	// Actual Wait, pipes and API operations join; absence of STOPPED remains
	// UNKNOWN even though local memory/root references can now be revoked.
	<-a.process.done
	a.mu.Lock()
	a.processJoined = true
	c := a.active
	if c != nil {
		if !c.stopped {
			c.unknown = true
			c.custody.Close()
		}
		a.tryJoined(c)
	}
	a.mu.Unlock()
}
func (a *RentalMediaAdapter) acceptFrame(kind Kind, raw []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.active
	if c == nil || c.cleaned || c.stopped || c.unknown {
		return ErrOutput
	}
	switch kind {
	case Accepted:
		var v struct {
			ID    string `json:"invocation_id"`
			Phase string `json:"phase"`
		}
		if c.accepted || strictjson.Decode(raw, &v, 4096) != nil || v.ID != c.invocation.ID || v.Phase != "EXECUTING" {
			return ErrOutput
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if a.ledger.MarkRentalSubmitted(ctx, a.hold, v.ID, c.backend) != nil {
			return ErrOutput
		}
		c.accepted = true
	case Result:
		if !c.accepted || c.result {
			return ErrOutput
		}
		custody, e := newMediaCustody(raw, a.policy.Kind, a.ledger, a.hold, c.expected, c.workspace)
		if e != nil {
			return ErrOutput
		}
		if custody.state.receipt.state.wire.Bytes > a.policy.MaximumOutputBytes {
			custody.Close()
			return ErrOutput
		}
		c.custody = custody
		c.result = true
	case Stopped:
		var v struct {
			ID        string `json:"invocation_id"`
			Stopped   bool   `json:"work_stopped"`
			Revoked   bool   `json:"future_start_revoked"`
			Cleared   bool   `json:"input_keys_cleared"`
			Keys      string `json:"output_keys"`
			Succeeded bool   `json:"result_succeeded"`
		}
		if strictjson.Decode(raw, &v, 4096) != nil || v.ID != c.invocation.ID || !v.Stopped || !v.Revoked || !v.Cleared || v.Succeeded != c.result || c.result && v.Keys != "PRIVATE_HOST_TRANSFERRED" || !c.result && v.Keys != "REVOKED" {
			return ErrOutput
		}
		c.stopped = true
		a.tryJoined(c)
	default:
		return ErrOutput
	}
	return nil
}
func (c *MediaCall) Observation() execution.Observation {
	if c == nil || c.adapter == nil {
		return execution.Observation{State: "UNKNOWN"}
	}
	a := c.adapter
	a.mu.Lock()
	defer a.mu.Unlock()
	state := "SUBMITTING"
	if c.accepted {
		state = "EXECUTING"
	}
	if c.stopped && c.senderDone {
		state = "STOPPED"
	}
	if c.unknown {
		state = "UNKNOWN"
	}
	return execution.Observation{RequestID: c.invocation.ID, BackendID: c.backend, State: state}
}
func (c *MediaCall) Result() (MediaCustody, error) {
	if c == nil || c.adapter == nil {
		return MediaCustody{}, ErrOutput
	}
	a := c.adapter
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing || c.unknown || c.cleaned || !c.stopped || !c.senderDone || !c.result {
		return MediaCustody{}, ErrOutput
	}
	return c.custody, nil
}
func (c *MediaCall) Cancel(ctx context.Context) error {
	if c == nil || c.adapter == nil || ctx == nil {
		return ErrOutput
	}
	a := c.adapter
	if _, e := a.ledger.CheckRentalExpected(ctx, a.hold, c.expected, "CANCEL_SELF"); e != nil {
		return ErrOutput
	}
	if a.ledger.StopRentalInvocation(ctx, a.hold, c.invocation.ID) != nil {
		return ErrOutput
	}
	return a.process.Cancel(ctx, c.invocation.ID)
}

// Cleanup must follow actual STOPPED and closed result custody. It consumes
// only this original directory and cleans this child; parent remains rented.
func (c *MediaCall) Cleanup(ctx context.Context) error {
	if c == nil || c.adapter == nil || ctx == nil {
		return ErrOutput
	}
	a := c.adapter
	a.mu.Lock()
	if a.finalizing || c.cleaning || c.unknown || !c.stopped || !c.senderDone || c.cleaned {
		a.mu.Unlock()
		return ErrOutput
	}
	c.cleaning = true
	a.operations.Add(1)
	a.mu.Unlock()
	defer func() { a.mu.Lock(); c.cleaning = false; a.mu.Unlock(); a.operations.Done() }()
	if a.ledger.StopRentalInvocation(ctx, a.hold, c.invocation.ID) != nil {
		return ErrOutput
	}
	observation := c.Observation()
	if observation.State != "STOPPED" || a.ledger.ObserveRentalInvocationStopped(ctx, a.hold, c.invocation.ID, observation) != nil {
		return ErrOutput
	}
	c.custody.Close()
	raw, _ := json.Marshal(struct{ Invocation, Backend, Generation string }{c.invocation.ID, c.backend, c.invocation.LocalGeneration})
	hash := sha256.Sum256(raw)
	proof := execution.RentalChildCleanup{Session: c.invocation.Session, Invocation: c.invocation.ID, LocalGeneration: c.invocation.LocalGeneration, BackendID: c.backend, ManifestHash: hex.EncodeToString(hash[:]), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}
	if c.workspace.RetireAndRemove(ctx, proof) != nil {
		return ErrOutput
	}
	if a.ledger.FinishRentalInvocationCleanup(ctx, a.hold, c.invocation.ID, proof) != nil {
		return ErrOutput
	}
	a.mu.Lock()
	c.cleaned = true
	a.mu.Unlock()
	return nil
}
func (a *RentalMediaAdapter) Close(ctx context.Context) error {
	if a == nil || a.process == nil || ctx == nil {
		return ErrOutput
	}
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closing = true
		a.finalizing = true
		a.mu.Unlock()
		go func() {
			wait, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = a.process.Close(wait)
			cancel()
			// A timeout never closes joined. Retain original owners until actual exit.
			<-a.process.done
			<-a.done
			a.operations.Wait()
			a.mu.Lock()
			c := a.active
			var custody MediaCustody
			if c != nil {
				custody = c.custody
			}
			a.mu.Unlock()
			custody.Close()
			close(a.joined)
		}()
	})
	select {
	case <-ctx.Done():
		return ErrOutput
	case <-a.joined:
		return nil
	}
}

// Called by the original Process finalizer BEFORE its done/Use release. It
// revokes result keys/readers even when no external caller invokes Close.
func (a *RentalMediaAdapter) revokeResults() {
	a.mu.Lock()
	a.closing = true
	c := a.active
	var custody MediaCustody
	if c != nil {
		custody = c.custody
	}
	a.mu.Unlock()
	custody.Close()
}
func (p *Process) SamePreparation(h execution.Hold, prepared rentalaccess.Prepared) bool {
	if p == nil {
		return false
	}
	original, manifest := p.use.Original()
	return original == h && manifest.Digest() != "" && manifest.Digest() == prepared.Digest()
}
