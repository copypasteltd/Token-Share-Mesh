// Package rentalmedia supervises original RENTAL media sessions. It does not
// manufacture text jobs, attempt leases, buyer acceptance or billing evidence.
package rentalmedia

import (
	"context"
	"errors"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

var ErrClosed = errors.New("original rental media admission is closed")
var ErrUnknown = errors.New("original media execution or cleanup is unconfirmed; retain owner")
var ErrQuery = errors.New("original media invocation is query-only; do not resubmit")

type Control interface {
	Hold() execution.Hold
	CheckScope(context.Context, string) error
	RefreshControl(context.Context) error
	Stop(context.Context) error
}
type Reader interface {
	Read(context.Context, []byte) (int, error)
	Seek(context.Context, int64, int) (int64, error)
	Close() error
}
type Result interface {
	Open(context.Context) (Reader, error)
	Close()
}
type Child interface {
	Original() (execution.RentalInvocation, rentalaccess.MediaDescriptor, rentalaccess.Expected)
	Observation() execution.Observation
	Done() <-chan struct{}
	Succeeded() bool
	Result() (Result, error)
	Cleanup(context.Context) error
}
type SessionProof struct {
	Session, Resource, LocalGeneration, ManifestHash                      string
	WorkStopped, FutureStartsRevoked, WorkspaceCleared, OutputKeysHandled bool
}

// RequestStop is a synchronous admission latch plus an owned asynchronous
// independent native control request. It may not wait for SQL, readers or GPU.
// Join includes actual native/process/pipes and all entered backend operations.
type Backend interface {
	Original() (execution.Hold, rentalaccess.Prepared, rentalaccess.MediaPolicy)
	Submit(context.Context, *rentalaccess.MediaInput, rentalaccess.Expected) (Child, error)
	CancelOriginal(context.Context, string) error
	RequestStop()
	End(context.Context) (SessionProof, error)
	Join(context.Context) error
}
type Options struct{ Poll, ControlPoll, CallTimeout, MetadataTimeout, ShutdownTimeout, ResultRetention time.Duration }

func options(o Options) (Options, error) {
	if o.Poll == 0 {
		o.Poll = 200 * time.Millisecond
	}
	if o.ControlPoll == 0 {
		o.ControlPoll = 10 * time.Second
	}
	if o.CallTimeout == 0 {
		o.CallTimeout = 2 * time.Second
	}
	if o.MetadataTimeout == 0 {
		o.MetadataTimeout = 2 * time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 12 * time.Second
	}
	if o.ResultRetention == 0 {
		o.ResultRetention = 5 * time.Minute
	}
	if o.Poll < 10*time.Millisecond || o.Poll > time.Second || o.ControlPoll < 100*time.Millisecond || o.ControlPoll > 10*time.Second || o.CallTimeout < 20*time.Millisecond || o.CallTimeout > 5*time.Second || o.MetadataTimeout < 20*time.Millisecond || o.MetadataTimeout > 5*time.Second || o.ShutdownTimeout < time.Second || o.ShutdownTimeout > 40*time.Second || o.ResultRetention < time.Second || o.ResultRetention > 30*time.Minute {
		return o, ErrClosed
	}
	return o, nil
}

// InputOwner is a trusted enclosing node owner, not a network acknowledgement.
// It revokes unsubmitted envelope keys and joins all entered local setup calls
// before the runtime can project session cleanup or report actual Done.
type InputOwner interface {
	StopInputs()
	JoinInputs(context.Context) error
}
