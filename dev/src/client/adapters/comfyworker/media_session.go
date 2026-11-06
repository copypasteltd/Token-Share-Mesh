package comfyworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

// RequestStop closes content admission synchronously, before any metadata or
// blocking reader cleanup. A bounded independent control write asks the worker
// to revoke future starts and synchronize. Forced exit cannot fabricate CLOSED.
func (p *Process) RequestStop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	p.closeOnce.Do(func() {
		go func() {
			stop, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_ = p.controlMessage(stop, StopSession, "")
			cancel()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-p.done:
			case <-timer.C:
				p.abort()
			}
		}()
	})
}
func (a *RentalMediaAdapter) Original() (execution.Hold, rentalaccess.Prepared, rentalaccess.MediaPolicy) {
	if a == nil {
		return execution.Hold{}, rentalaccess.Prepared{}, rentalaccess.MediaPolicy{}
	}
	return a.hold, a.prepared, a.policy
}
func (a *RentalMediaAdapter) RequestStop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.withdrawn = true
	a.mu.Unlock()
	a.process.RequestStop()
}
func (c *MediaCall) Original() (execution.RentalInvocation, rentalaccess.MediaDescriptor, rentalaccess.Expected) {
	if c == nil || c.adapter == nil {
		return execution.RentalInvocation{}, rentalaccess.MediaDescriptor{}, rentalaccess.Expected{}
	}
	c.adapter.mu.Lock()
	defer c.adapter.mu.Unlock()
	return c.invocation, c.descriptor, c.expected
}
func (c *MediaCall) Done() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.done
}
func (c *MediaCall) Succeeded() bool {
	if c == nil || c.adapter == nil {
		return false
	}
	c.adapter.mu.Lock()
	defer c.adapter.mu.Unlock()
	return c.stopped && c.senderDone && c.result && !c.unknown
}

// Revoke is trusted original-host control, valid even after the buyer's grant
// is withdrawn. It is not a public CANCEL_SELF authorization bypass.
func (c *MediaCall) Revoke(ctx context.Context) error {
	if c == nil || c.adapter == nil || ctx == nil {
		return ErrOutput
	}
	return c.adapter.process.Cancel(ctx, c.invocation.ID)
}

type MediaSessionProof struct {
	Session, Resource, LocalGeneration, ManifestHash                      string
	WorkStopped, FutureStartsRevoked, WorkspaceCleared, OutputKeysHandled bool
}

// End closes actual original process/pipes, revokes all result custody, and
// checks ALL original durable children/workspaces. Neither a missing process
// nor a caller-provided JSON flag can produce this projection.
func (a *RentalMediaAdapter) End(ctx context.Context) (MediaSessionProof, error) {
	if a == nil || ctx == nil {
		return MediaSessionProof{}, ErrOutput
	}
	a.mu.Lock()
	child := a.active
	cleaned := child == nil || child.cleaned
	a.mu.Unlock()
	if !cleaned {
		return MediaSessionProof{}, ErrOutput
	}
	a.RequestStop()
	if a.Close(ctx) != nil || a.process.Join(ctx) != nil {
		return MediaSessionProof{}, ErrOutput
	}
	a.process.mu.Lock()
	closed := a.process.closedFrame && !a.process.failed && a.process.nativeError == nil
	a.process.mu.Unlock()
	if !closed {
		return MediaSessionProof{}, ErrOutput
	}
	remaining, e := a.ledger.RentalOutstanding(ctx, a.hold)
	if e != nil || remaining.Invocations != 0 || remaining.Workspaces != 0 {
		return MediaSessionProof{}, ErrOutput
	}
	raw, _ := json.Marshal(struct{ Session, Resource, Generation, Manifest string }{a.hold.Owner(), a.hold.Resource(), a.hold.LocalGeneration(), a.prepared.Digest()})
	hash := sha256.Sum256(raw)
	return MediaSessionProof{Session: a.hold.Owner(), Resource: a.hold.Resource(), LocalGeneration: a.hold.LocalGeneration(), ManifestHash: hex.EncodeToString(hash[:]), WorkStopped: true, FutureStartsRevoked: true, WorkspaceCleared: true, OutputKeysHandled: true}, nil
}

// Join always waits actual adapter/process/content operations before returning.
// A timeout retains the enclosing node lifetime. A failed native close remains
// an error after the join and cannot release a quarantined resource.
func (a *RentalMediaAdapter) Join(ctx context.Context) error {
	if a == nil || ctx == nil {
		return ErrOutput
	}
	if a.Close(ctx) != nil {
		return ErrOutput
	}
	return a.process.Join(ctx)
}

// Only a trusted original runtime calls this after binding id to its own inbox.
// It can tombstone a child before native entry and requires no expired Bearer.
func (a *RentalMediaAdapter) CancelOriginal(ctx context.Context, id string) error {
	if a == nil || a.process == nil || ctx == nil {
		return ErrOutput
	}
	return a.process.Cancel(ctx, id)
}
