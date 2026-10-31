package supervisor

import (
	"context"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
)

const NoStopIndex = ^uint32(0)

// TextStart/TextResult are local protocol measurements. Neither creates trusted
// usage, buyer acceptance, settlement eligibility or a resource-release proof.
type TextStart struct{ InputTokens uint32 }
type TextResult struct {
	InputTokens, OutputTokens uint32
	Reason                    string
	StopIndex                 uint32
}

// TextAdapter never exposes a localhost content server. A trusted content
// custodian drains it independently from transport and execution cleanup.
type TextAdapter interface {
	InputAdapter
	CheckInput(context.Context, Work, executionlease.Manifest, []byte) error
	Started(context.Context, Work) (TextStart, error)
	Read(context.Context, Work) ([]byte, error)
	Result(Work) (TextResult, error)
}

type TextDescriptor struct {
	Work         Work
	Manifest     executionlease.Manifest
	ResultID     string
	CreatedUnix  int64
	IncludeUsage bool
}

// TextOutput is a separately authorized, bounded result capability. Its exact
// original descriptor survives execution cleanup; it cannot submit/renew work.
// Read transfers a plaintext slice which the consumer must clear after use.
type TextOutput interface {
	Descriptor() TextDescriptor
	Started(context.Context) (TextStart, error)
	Read(context.Context) ([]byte, error)
	Result(context.Context) (TextResult, error)
	StopSequence(context.Context, uint32) ([]byte, error)
	Close(context.Context) error
}

type TextOutputProvider interface {
	Output(context.Context, Work) (TextOutput, error)
}

// Output resolves only an already accepted original inbox. Current resource
// ownership may have advanced; the result capability cannot address it.
func (s *Supervisor) Output(ctx context.Context, h execution.Hold, a devicecontrol.AcceptedDelivery) (TextOutput, error) {
	l, m := a.Verified().Details(), a.Manifest()
	if l.ID != h.Owner() || l.Resource != h.Resource() || m.Digest() != l.ManifestHash || s.store.ResultBinding(ctx, h, m) != nil {
		return nil, ErrAdapter
	}
	backend := s.registry[registryKey{l.Environment, l.Capability, l.ManifestHash}]
	provider, ok := backend.(TextOutputProvider)
	if !ok {
		return nil, ErrAdapter
	}
	w := Work{RequestID: h.Owner(), Resource: h.Resource(), LocalGeneration: h.LocalGeneration(), Environment: l.Environment}
	out, e := provider.Output(ctx, w)
	if e != nil {
		return nil, e
	}
	descriptor := out.Descriptor()
	if descriptor.Work != w || descriptor.Manifest.Digest() != m.Digest() {
		_ = out.Close(ctx)
		return nil, ErrAdapter
	}
	return out, nil
}
