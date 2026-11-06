package supervisor

import (
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

const MaximumManifestBindings = 4096

type kernelKey struct{ environment, capability string }

// KernelRegistration is trusted installation configuration supplied before Run.
// Jobs cannot supply an adapter. Environment binds the complete installed model,
// recipe and runtime selection; Host independently checks its original rules.
type KernelRegistration struct {
	Environment, Capability string
	Backend                 Adapter
}

// NewResident freezes the installed adapter set, while allowing the enclosing
// trusted host to bind subsequent verified original manifests to those adapters.
// There is no wildcard lookup, engine replacement or implicit Submit authority.
func NewResident(store *execution.Store, kernels []KernelRegistration, o Options) (*Supervisor, error) {
	if len(kernels) < 1 || len(kernels) > 64 {
		return nil, ErrAdapter
	}
	s, err := newSupervisor(store, o)
	if err != nil {
		return nil, err
	}
	s.kernels = make(map[kernelKey]Adapter, len(kernels))
	for _, k := range kernels {
		key := kernelKey{k.Environment, k.Capability}
		if !executionlease.Hash(k.Environment) || !ids.Valid(k.Capability) || k.Backend == nil || s.kernels[key] != nil {
			return nil, ErrAdapter
		}
		s.kernels[key] = k.Backend
	}
	return s, nil
}

// BindManifest accepts metadata only after independent Host approval. It copies
// an existing immutable kernel binding and cannot install or replace a backend.
// Successful binding alone neither creates a guard nor bypasses the inbox gate.
func (s *Supervisor) BindManifest(environment, capability, manifest string) error {
	if !executionlease.Hash(environment) || !ids.Valid(capability) || !executionlease.Hash(manifest) {
		return ErrAdapter
	}
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	if s.closed.Load() {
		return ErrClosed
	}
	k := kernelKey{environment, capability}
	backend := s.kernels[k]
	if backend == nil {
		return ErrAdapter
	}
	key := registryKey{environment, capability, manifest}
	if s.registry[key] != nil {
		return nil // same immutable kernel, never an adapter supplied by a job
	}
	if len(s.registry) >= MaximumManifestBindings {
		return ErrAdapter
	}
	s.registry[key] = backend
	return nil
}

func (s *Supervisor) lookup(environment, capability, manifest string) Adapter {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()
	return s.registry[registryKey{environment, capability, manifest}]
}
