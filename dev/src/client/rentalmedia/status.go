package rentalmedia

import (
	"context"

	"p11.local/compute/client/execution"
)

// MediaState separates durable query metadata from actual native stop/result.
// It is not progress, usage, delivery ACK or a remote stopped-work proof.
type MediaState struct {
	Invocation                          execution.RentalInvocation
	NativeState                         string
	Stopped, Succeeded, ResultAvailable bool
}

func (r *Runtime) Status(ctx context.Context, id string) (MediaState, error) {
	return r.status(ctx, id, "QUEUE_SELF")
}

// ExportStatus observes only this original invocation under the export scope.
// A result reader does not acquire queue-management authority as a side effect.
func (r *Runtime) ExportStatus(ctx context.Context, id string) (MediaState, error) {
	return r.status(ctx, id, "EXPORT_SELF")
}

func (r *Runtime) status(ctx context.Context, id, scope string) (MediaState, error) {
	j, e := r.originalChild(ctx, id, scope)
	if e != nil {
		return MediaState{}, e
	}
	meta, cancel := r.metadata()
	v, e := r.ledger.RentalInvocation(meta, r.hold, id)
	cancel()
	if e != nil || v.Grant != j.expected.Grant || v.GrantDigest != j.expected.TokenDigest {
		return MediaState{}, ErrQuery
	}
	out := MediaState{Invocation: v, NativeState: "SUBMITTING"}
	select {
	case <-j.submitted:
	default:
		return out, nil
	}
	r.mu.Lock()
	child, cleaned, released := j.child, j.cleaned, j.released
	r.mu.Unlock()
	if child == nil {
		return MediaState{}, ErrUnknown
	}
	seen := child.Observation()
	if seen.RequestID != id || seen.BackendID == "" || seen.State == "UNKNOWN" || seen.State == "ABSENT" {
		return MediaState{}, ErrUnknown
	}
	out.NativeState = seen.State
	select {
	case <-child.Done():
		out.Stopped = seen.State == "STOPPED"
	default:
	}
	out.Succeeded = out.Stopped && child.Succeeded()
	out.ResultAvailable = out.Succeeded && !cleaned && !released
	return out, nil
}
