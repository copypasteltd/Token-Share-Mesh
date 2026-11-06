package node

import (
	"context"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalmedia"
	mediagateway "p11.local/compute/gateway/media"
)

// OpenMediaGateway binds the real original node leaf and its independent
// verifier. Caller configuration cannot supply a permissive Session/context
// or replace stop ownership. No listener, model, payment or fallback is opened.
func (s *ComfyRental) OpenMediaGateway(ctx context.Context, verifier *rentalaccess.Verifier, c mediagateway.Config) (*mediagateway.Handler, error) {
	if s == nil || ctx == nil || ctx.Err() != nil || verifier == nil {
		return nil, ErrRentalJournal
	}
	s.mu.Lock()
	if s.closing || !s.authorized || s.runtime == nil || s.gateway != nil || s.openingGateway {
		s.mu.Unlock()
		return nil, ErrRentalJournal
	}
	s.openingGateway = true
	s.operations.Add(1)
	watch := s.runtime
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.openingGateway = false; s.mu.Unlock(); s.operations.Done() }()
	c.Prepared = s.provider.prepared
	c.Policy = s.policy
	c.Session = comfyGatewayPort{s, verifier}
	c.OriginalContext = watch.Context()
	c.StopOriginal = s.requestClose
	handler, e := mediagateway.New(c)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	if s.closing || ctx.Err() != nil {
		s.mu.Unlock()
		_ = handler.Close(context.Background())
		return nil, ErrRentalJournal
	}
	s.gateway = handler
	s.mu.Unlock()
	return handler, nil
}
func (s *ComfyRental) MediaStatus(ctx context.Context, v *rentalaccess.Verifier, credential, id string) (rentalmedia.MediaState, error) {
	if !s.enterAccess() {
		return rentalmedia.MediaState{}, ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "QUEUE_SELF")
	if e != nil {
		return rentalmedia.MediaState{}, e
	}
	return watch.Status(ctx, id)
}

func (s *ComfyRental) MediaExportStatus(ctx context.Context, v *rentalaccess.Verifier, credential, id string) (rentalmedia.MediaState, error) {
	if !s.enterAccess() {
		return rentalmedia.MediaState{}, ErrRentalJournal
	}
	defer s.operations.Done()
	watch, e := s.access(ctx, v, credential, "EXPORT_SELF")
	if e != nil {
		return rentalmedia.MediaState{}, e
	}
	return watch.ExportStatus(ctx, id)
}

type comfyGatewayPort struct {
	session  *ComfyRental
	verifier *rentalaccess.Verifier
}

func (p comfyGatewayPort) Check(ctx context.Context, credential, scope string) error {
	if !p.session.enterAccess() {
		return ErrRentalJournal
	}
	defer p.session.operations.Done()
	_, e := p.session.access(ctx, p.verifier, credential, scope)
	return e
}
func (p comfyGatewayPort) Seal(ctx context.Context, credential, id string, params []byte, assets []rentalaccess.MediaAsset) (*rentalaccess.MediaInput, error) {
	return p.session.SealMediaInput(ctx, p.verifier, credential, id, params, assets)
}
func (p comfyGatewayPort) Invoke(ctx context.Context, input *rentalaccess.MediaInput) (execution.RentalInvocation, error) {
	return p.session.Invoke(ctx, input)
}
func (p comfyGatewayPort) Status(ctx context.Context, credential, id string) (rentalmedia.MediaState, error) {
	return p.session.MediaStatus(ctx, p.verifier, credential, id)
}
func (p comfyGatewayPort) ExportStatus(ctx context.Context, credential, id string) (rentalmedia.MediaState, error) {
	return p.session.MediaExportStatus(ctx, p.verifier, credential, id)
}
func (p comfyGatewayPort) Output(ctx context.Context, credential, id string) (*rentalmedia.Output, error) {
	return p.session.Output(ctx, p.verifier, credential, id)
}
func (p comfyGatewayPort) Cancel(ctx context.Context, credential, id string) error {
	return p.session.Cancel(ctx, p.verifier, credential, id)
}
func (p comfyGatewayPort) Discard(ctx context.Context, credential, id string) error {
	return p.session.DiscardResult(ctx, p.verifier, credential, id)
}
