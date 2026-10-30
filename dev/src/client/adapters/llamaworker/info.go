package llamaworker

// LocalRuntimeInfo is safe owner-facing metadata. GPUDevice/GPULayers describe
// the approved selection, not measured offload or physical mapping. ProcessID
// identifies this owned process only; it never authorizes Query/Cleanup/release.
// No prompt, generated content, arguments, IPC handles or keys are exposed.
type LocalRuntimeInfo struct {
	ProcessID                                                   int
	Profile, UpstreamCommit, EngineRelease                      string
	Environment, ModelSHA256, TemplateSHA256, SelectedGPUDevice string
	SelectedGPULayers                                           uint32
}

func (a *Adapter) LocalInfo() LocalRuntimeInfo {
	return LocalRuntimeInfo{ProcessID: a.p.pid, Profile: Profile, UpstreamCommit: UpstreamCommit, EngineRelease: EngineRelease,
		Environment: a.c.Environment, ModelSHA256: a.c.Model.SHA256, TemplateSHA256: a.c.Template.SHA256,
		SelectedGPUDevice: a.c.GPUDevice, SelectedGPULayers: a.c.GPULayers}
}
