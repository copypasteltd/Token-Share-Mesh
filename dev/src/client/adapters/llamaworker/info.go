package llamaworker

import "p11.local/compute/shared/gpuid"

// LocalRuntimeInfo is safe owner-facing metadata. GPUDevice/GPULayers describe
// the approved selection. ObservedGPU is a local identity check, not measured
// offload or remote attestation. ProcessID
// identifies this owned process only; it never authorizes Query/Cleanup/release.
// No prompt, generated content, arguments, IPC handles or keys are exposed.
type LocalRuntimeInfo struct {
	ProcessID                                                   int
	Profile, UpstreamCommit, EngineRelease                      string
	Environment, ModelSHA256, TemplateSHA256, SelectedGPUDevice string
	SelectedGPULayers                                           uint32
	ObservedGPU                                                 gpuid.Identity
	PhysicalDigest                                              string
}

func (a *Adapter) LocalInfo() LocalRuntimeInfo {
	digest, _ := gpuid.Digest(a.physical.UUID)
	return LocalRuntimeInfo{ObservedGPU: a.physical, PhysicalDigest: digest, ProcessID: a.p.pid, Profile: Profile, UpstreamCommit: UpstreamCommit, EngineRelease: EngineRelease,
		Environment: a.c.Environment, ModelSHA256: a.c.Model.SHA256, TemplateSHA256: a.c.Template.SHA256,
		SelectedGPUDevice: a.c.GPUDevice, SelectedGPULayers: a.c.GPULayers}
}
