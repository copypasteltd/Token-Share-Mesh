package node

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"os"

	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

const kernelRegistryFile = "kernel-registry.enc"

// kernelJournal is reachable only through the original, OS-locked node. It
// does not acquire a second GPU gate or accept a caller-chosen storage path.
type kernelJournal struct{ node *Store }

func (j kernelJournal) Root() *os.Root { return j.node.root }
func (j kernelJournal) Read() ([]byte, error) {
	s := j.node
	if s.root == nil || s.poisoned || s.checkFile(kernelRegistryFile, true) != nil {
		return nil, comfyenv.ErrState
	}
	if _, err := s.root.Lstat(kernelRegistryFile); errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	} else if err != nil {
		return nil, comfyenv.ErrState
	}
	return s.read(kernelRegistryFile, comfyenv.MaximumRegistryBytes)
}
func (j kernelJournal) Commit(sealed []byte) error {
	s := j.node
	if s.root == nil || s.poisoned || len(sealed) < 28 || len(sealed) > comfyenv.MaximumRegistryBytes {
		return comfyenv.ErrState
	}
	id, err := randomHex(16)
	if err != nil {
		return comfyenv.ErrState
	}
	name := "kernel-next-" + id
	if s.writeNew(name, sealed) != nil {
		return comfyenv.ErrState
	}
	defer s.root.Remove(name)
	if s.checkFile(kernelRegistryFile, true) != nil || replaceFile(s.path, s.root, name, kernelRegistryFile) != nil || syncDirectory(s.path) != nil {
		return comfyenv.ErrState
	}
	return nil
}
func (s *Store) kernelBinding() string {
	return hostDigest([]byte(comfyenv.RegistryProfile + "\n" + s.s.Installation + "\n" + s.s.KernelID + "\n" + s.s.DeviceID + "\n" + s.s.SellerID + "\n" + s.s.Config.Base + "\n"))
}

// InitKernelRegistry is explicit, metadata-only and before starting node loops.
// The random anchor is flushed to the original AEAD state BEFORE creation.
// An interrupted or missing anchored journal is never silently reinitialized.
func (s *Store) InitKernelRegistry() (*comfyenv.Registry, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenKernels() || s.s.KernelID != "" || s.kernels != nil || s.s.Pending != nil || s.host != nil || len(s.nativeRentals) != 0 {
		return nil, comfyenv.ErrState
	}
	if _, err := (kernelJournal{s}).Read(); !errors.Is(err, os.ErrNotExist) {
		return nil, comfyenv.ErrState
	}
	for _, name := range []string{"kernel-bundles", "kernel-bundles/stages", "kernel-bundles/environments"} {
		if _, err := s.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return nil, comfyenv.ErrState
		}
	}
	id, err := randomHex(32)
	if err != nil {
		return nil, comfyenv.ErrState
	}
	s.s.KernelID = id
	if s.save() != nil {
		return nil, comfyenv.ErrState
	}
	return s.openKernelsLocked(true)
}

// OpenKernelRegistry restores only metadata. It cannot restore GPU execution,
// an ALLOW deadline, qualification, owner consent or a platform capability.
func (s *Store) OpenKernelRegistry() (*comfyenv.Registry, error) {
	s.nativeMu.Lock()
	defer s.nativeMu.Unlock()
	if !s.canOpenKernels() || s.s.KernelID == "" {
		return nil, comfyenv.ErrState
	}
	if s.kernels != nil {
		return s.kernels, nil
	}
	return s.openKernelsLocked(false)
}
func (s *Store) canOpenKernels() bool {
	return s.root != nil && !s.poisoned && !s.closingNative && s.s.DeviceID != "" && s.s.Config.Kind == "GPU_DEVICE" && s.s.RemoteState != "REVOKED"
}
func (s *Store) openKernelsLocked(create bool) (*comfyenv.Registry, error) {
	binding := s.kernelBinding()
	mac := hmac.New(sha256.New, s.master)
	mac.Write([]byte("P11_NATIVE_COMFY_METADATA_KEY_1\n" + binding))
	key := mac.Sum(nil)
	defer clear(key)
	r, err := comfyenv.OpenOwned(kernelJournal{s}, key, binding, create)
	if err != nil {
		return nil, comfyenv.ErrState
	}
	s.kernels = r
	return r, nil
}

// ActivateComfy uses only this node's original open execution ledger. It starts
// no interpreter and leaves the exact maintenance owner occupied; the caller
// must complete the original supervision/cleanup protocol before sharing.
func (s *Store) ActivateComfy(ctx context.Context, h execution.Hold, p comfyenv.Plan) (comfyenv.Selection, error) {
	s.nativeMu.Lock()
	if !s.canOpenKernels() || s.kernels == nil || s.execution == nil {
		s.nativeMu.Unlock()
		return comfyenv.Selection{}, comfyenv.ErrState
	}
	registry, gate := s.kernels, s.execution
	s.nativeMu.Unlock()
	return registry.ActivateLocal(ctx, gate, h, p)
}

// OpenComfyUse binds the installed immutable environment to this node's
// ORIGINAL rental preparation owner. It starts no engine and restores no grant.
// Store.Close joins claimed process references before its root/key/OS lock.
func (s *Store) OpenComfyUse(ctx context.Context, h execution.Hold, prepared rentalaccess.Prepared, p comfyenv.Plan) (comfyenv.Use, error) {
	s.nativeMu.Lock()
	if !s.canOpenKernels() || s.kernels == nil || s.execution == nil {
		s.nativeMu.Unlock()
		return comfyenv.Use{}, comfyenv.ErrState
	}
	registry, gate := s.kernels, s.execution
	s.nativeMu.Unlock()
	return registry.OpenRentalUse(ctx, gate, h, prepared, p)
}
