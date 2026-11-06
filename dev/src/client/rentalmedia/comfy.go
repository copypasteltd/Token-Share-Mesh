package rentalmedia

import (
	"context"

	"p11.local/compute/client/adapters/comfyworker"
	"p11.local/compute/client/protocol/rentalaccess"
)

type comfyBackend struct {
	*comfyworker.RentalMediaAdapter
}
type comfyChild struct{ *comfyworker.MediaCall }
type comfyResult struct{ comfyworker.MediaCustody }

// Comfy adapts the actual typed native leaf, not a network-supplied observation.
func Comfy(adapter *comfyworker.RentalMediaAdapter) Backend {
	if adapter == nil {
		return nil
	}
	return comfyBackend{adapter}
}
func (b comfyBackend) Submit(ctx context.Context, input *rentalaccess.MediaInput, current rentalaccess.Expected) (Child, error) {
	c, e := b.RentalMediaAdapter.Submit(ctx, input, current)
	if c == nil {
		return nil, e
	}
	return comfyChild{c}, e
}
func (b comfyBackend) End(ctx context.Context) (SessionProof, error) {
	p, e := b.RentalMediaAdapter.End(ctx)
	return SessionProof{Session: p.Session, Resource: p.Resource, LocalGeneration: p.LocalGeneration, ManifestHash: p.ManifestHash, WorkStopped: p.WorkStopped, FutureStartsRevoked: p.FutureStartsRevoked, WorkspaceCleared: p.WorkspaceCleared, OutputKeysHandled: p.OutputKeysHandled}, e
}
func (c comfyChild) Result() (Result, error) {
	r, e := c.MediaCall.Result()
	if e != nil {
		return nil, e
	}
	return comfyResult{r}, nil
}
func (r comfyResult) Open(ctx context.Context) (Reader, error) { return r.MediaCustody.Open(ctx) }
