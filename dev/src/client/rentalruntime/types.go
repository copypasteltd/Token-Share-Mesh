// Package rentalruntime supervises an original RENTAL session and serial child
// invocations. It never constructs ordinary JOB/attempt authority or charges
// per invocation. Backend observations are trusted local adapter facts only.
package rentalruntime

import (
	"context"
	"errors"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/supervisor"
)

var ErrClosed = errors.New("original rental execution admission is closed")
var ErrBackend = errors.New("original rental backend state is unconfirmed; retain owner")
var ErrQuery = errors.New("original rental invocation requires query; do not resubmit")

// Work is the original metadata used for every submit/query/cancel/cleanup.
// Expected is independently loaded current authority, not derived from input.
type Work struct {
	Invocation execution.RentalInvocation
	Descriptor rentalaccess.TextDescriptor
	Expected   rentalaccess.Expected
}

type Identity struct{ ManifestHash, PhysicalHash, EnvironmentHash, ModelHash, RecipeHash, PolicyHash string }
type Observation struct{ RequestID, BackendID, State string }
type TextStart = supervisor.TextStart
type TextResult = supervisor.TextResult
type ChildProof struct {
	Session, Invocation, LocalGeneration, BackendID, ManifestHash    string
	WorkStopped, FutureStartRevoked, InputCleared, OutputKeysHandled bool
}
type SessionProof struct {
	Session, Resource, LocalGeneration, ManifestHash                      string
	WorkStopped, FutureStartsRevoked, WorkspaceCleared, OutputKeysHandled bool
}

// Backend implementations own their exact native process and private pipes.
// Cancel must create a future-start tombstone even before SubmitText enters.
// UNKNOWN or a missing process is never ABSENT/STOPPED. All calls honor ctx.
type Backend interface {
	Identity() Identity
	SubmitText(context.Context, Work, *rentalaccess.TextInput) (Observation, error)
	Query(context.Context, Work) (Observation, error)
	Cancel(context.Context, Work) error
	Cleanup(context.Context, Work, Observation) (ChildProof, error)
	Started(context.Context, Work) (supervisor.TextStart, error)
	Read(context.Context, Work) ([]byte, error)
	Result(Work) (supervisor.TextResult, error)
	CloseSession(context.Context, execution.Hold) (SessionProof, error)
	Join(context.Context) error
}

// Control is implemented by the original node.ProviderRental. Its checks join
// the encrypted control journal and the SAME physical resource ledger.
type Control interface {
	Hold() execution.Hold
	CheckScope(context.Context, string) error
	RefreshControl(context.Context) error
	Stop(context.Context) error
}

type Options struct{ Poll, ControlPoll, CallTimeout, MetadataTimeout, DrainGrace, ShutdownTimeout time.Duration }

func defaults(o Options) (Options, error) {
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
	if o.DrainGrace == 0 {
		o.DrainGrace = 3 * time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 8 * time.Second
	}
	if o.Poll < 20*time.Millisecond || o.Poll > time.Second || o.ControlPoll < time.Second || o.ControlPoll > 15*time.Second || o.CallTimeout < 20*time.Millisecond || o.CallTimeout > 5*time.Second || o.MetadataTimeout < 20*time.Millisecond || o.MetadataTimeout > 5*time.Second || o.DrainGrace < 100*time.Millisecond || o.DrainGrace > 10*time.Second || o.ShutdownTimeout < o.CallTimeout || o.ShutdownTimeout > 30*time.Second {
		return o, ErrBackend
	}
	return o, nil
}

func BoundWork(w Work, p rentalaccess.Prepared, policy rentalaccess.TextPolicy) bool {
	m, d, v := p.Details(), w.Descriptor, w.Invocation
	return policy.Binds(p) && p.BindsGrant(w.Expected) && d.Profile == rentalaccess.TextInputProfile &&
		d.ID == v.ID && d.Session == m.Session && d.Session == v.Session && d.Resource == m.Resource && d.Resource == v.Resource && d.Generation == m.Generation && d.LocalGeneration == v.LocalGeneration &&
		d.Grant == w.Expected.Grant && d.Grant == v.Grant && d.GrantDigest == w.Expected.TokenDigest && d.GrantDigest == v.GrantDigest &&
		d.ManifestHash == p.Digest() && d.EnvironmentHash == m.EnvironmentHash && d.RecipeHash == m.RecipeHash && d.RecipeHash == v.RecipeDigest &&
		d.InputCiphertextDigest == v.InputCiphertextDigest && d.PolicyHash == policy.Digest() && d.InputBytes > 0 && d.InputBytes <= rentalaccess.MaximumTextInputBytes &&
		d.InputTokenLimit == policy.InputTokenLimit && d.Spec.Protocol == policy.Protocol && d.Spec.PublicModel == policy.PublicModel && d.Spec.MaxOutputTokens > 0 && uint64(d.Spec.MaxOutputTokens) <= uint64(policy.OutputTokenLimit)
}
