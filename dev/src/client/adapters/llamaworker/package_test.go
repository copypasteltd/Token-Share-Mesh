package llamaworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func packageFixture(t *testing.T) (string, Package, OwnerSelection) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "lib"), 0700); err != nil {
		t.Fatal(err)
	}
	file := func(path, content string) PackageFile {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(content))
		return PackageFile{path, hex.EncodeToString(digest[:])}
	}
	p := Package{Schema: PackageProfile, Profile: Profile, EngineRelease: EngineRelease,
		Executable: file("worker.exe", "not an executable: configuration test only"), Model: file("model.gguf", "not real weights"), Template: file("template.txt", "chatml"),
		Library: "lib/llama.dll", BackendRegistry: "lib/base.dll", BackendCore: "lib/base.dll", Libraries: []PackageFile{file("lib/llama.dll", "library"), file("lib/base.dll", "registry")}}
	return root, p, OwnerSelection{Capability: "cap_local", Resource: "physical_gpu_original", GPUDevice: "CUDA0", GPUUUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", GPUPCI: "0000:01:00.0", ContextTokens: 2048, BatchTokens: 32, Threads: 2, GPULayers: 99}
}
func packageDocument(t *testing.T, root string, raw []byte, s OwnerSelection) (FilePin, string) {
	t.Helper()
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	pin := FilePin{path, hex.EncodeToString(h[:])}
	environment, err := PackageEnvironment(pin.SHA256, s)
	if err != nil {
		t.Fatal(err)
	}
	return pin, environment
}
func packageJSON(t *testing.T, p Package) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestNativePackageLoadsOnlyCompletePinnedLocalSelection(t *testing.T) {
	root, p, s := packageFixture(t)
	pin, env := packageDocument(t, root, packageJSON(t, p), s)
	c, err := LoadPackage(context.Background(), root, pin, s, env)
	if err != nil || c.Environment != env || c.GPUDevice != "CUDA0" || c.GPULayers != 99 || c.Resource != s.Resource || c.Capability != s.Capability || c.Model.SHA256 != p.Model.SHA256 || !filepath.IsAbs(c.Executable.Path) {
		t.Fatal("local package binding lost", err)
	}
	// CPU operation is possible only with a newly approved explicit selection.
	s.GPULayers, s.GPUDevice, s.GPUUUID, s.GPUPCI = 0, "", "", ""
	if _, err = LoadPackage(context.Background(), root, pin, s, env); err == nil {
		t.Fatal("GPU approval silently downgraded to CPU")
	}
	cpuEnv, err := PackageEnvironment(pin.SHA256, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPackage(context.Background(), root, pin, s, cpuEnv); err != nil {
		t.Fatal("explicit separate CPU selection rejected", err)
	}
}
func TestNativePackageRejectsAmbiguousAndForeignDocuments(t *testing.T) {
	root, p, s := packageFixture(t)
	valid := string(packageJSON(t, p))
	for name, raw := range map[string]string{
		"duplicate":      strings.Replace(valid, `"schema":`, `"schema":"other","schema":`, 1),
		"case_alias":     strings.Replace(valid, `"schema":`, `"Schema":`, 1),
		"unknown":        strings.TrimSuffix(valid, "}") + `,"argv":["remote-script"]}`,
		"trailing":       valid + `{}`,
		"foreign_pipe":   strings.Replace(valid, Profile, "P11_LLAMA_PIPE_1", 1),
		"foreign_engine": strings.Replace(valid, EngineRelease, "newest-unapproved", 1),
		"null_libraries": strings.Replace(valid, `"libraries":[`, `"libraries":null,"other":[`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			pin, env := packageDocument(t, root, []byte(raw), s)
			if _, err := LoadPackage(context.Background(), root, pin, s, env); err == nil {
				t.Fatal("ambiguous or foreign document accepted")
			}
		})
	}
}
func TestNativePackageRejectsPathsAndUnlistedOrChangedLibraries(t *testing.T) {
	for _, path := range []string{"../outside.exe", "/absolute.exe", `C:/outside.exe`, `lib\\alias.dll`, "lib/../worker.exe", "lib/NUL.dll", "lib/COM1.dll", "lib/trailing.", "lib/a.dll:stream", "lib//worker.exe"} {
		t.Run(path, func(t *testing.T) {
			root, p, s := packageFixture(t)
			p.Executable.Path = path
			pin, env := packageDocument(t, root, packageJSON(t, p), s)
			if _, err := LoadPackage(context.Background(), root, pin, s, env); err == nil {
				t.Fatal("nonportable escaping path accepted")
			}
		})
	}
	for _, mode := range []string{"extra_dll", "changed_library", "changed_model", "changed_template", "changed_document"} {
		t.Run(mode, func(t *testing.T) {
			root, p, s := packageFixture(t)
			pin, env := packageDocument(t, root, packageJSON(t, p), s)
			target := map[string]string{"extra_dll": "lib/extra.dll", "changed_library": "lib/llama.dll", "changed_model": "model.gguf", "changed_template": "template.txt", "changed_document": "package.json"}[mode]
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(target)), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPackage(context.Background(), root, pin, s, env); err == nil {
				t.Fatal("changed package or extra DLL accepted")
			}
		})
	}
}
func TestNativePackageEnvironmentBindsEveryOwnerExecutionParameter(t *testing.T) {
	_, _, s := packageFixture(t)
	sha := strings.Repeat("a", 64)
	want, err := PackageEnvironment(sha, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*OwnerSelection){func(v *OwnerSelection) { v.GPUDevice = "CUDA1" }, func(v *OwnerSelection) { v.GPUUUID = "GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21" }, func(v *OwnerSelection) { v.GPUPCI = "0000:02:00.0" }, func(v *OwnerSelection) { v.ContextTokens++ }, func(v *OwnerSelection) { v.BatchTokens++ }, func(v *OwnerSelection) { v.Threads++ }, func(v *OwnerSelection) { v.GPULayers++ }} {
		next := s
		change(&next)
		got, err := PackageEnvironment(sha, next)
		if err != nil || got == want {
			t.Fatal("changed owner parameter inherited approval")
		}
	}
	for _, next := range []OwnerSelection{{}, {GPUDevice: "CUDA0", GPUUUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", GPUPCI: "0000:01:00.0", ContextTokens: 32, BatchTokens: 1, Threads: 1}, {GPULayers: 1, ContextTokens: 32, BatchTokens: 1, Threads: 1}, {ContextTokens: 32, BatchTokens: 33, Threads: 1}} {
		if _, err := PackageEnvironment(sha, next); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	if _, err := PackageEnvironment(strings.ToUpper(sha), s); err == nil {
		t.Fatal("noncanonical package digest accepted")
	}
}
func TestNativePackageBoundsCancellationAndIndependentEnvironmentPin(t *testing.T) {
	root, p, s := packageFixture(t)
	pin, env := packageDocument(t, root, packageJSON(t, p), s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadPackage(ctx, root, pin, s, env); err == nil {
		t.Fatal("cancelled package verification accepted")
	}
	if _, err := LoadPackage(context.Background(), root, pin, s, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unapproved environment accepted")
	}
	pin, env = packageDocument(t, root, []byte(strings.Repeat(" ", maximumPackageBytes+1)), s)
	if _, err := LoadPackage(context.Background(), root, pin, s, env); err == nil {
		t.Fatal("oversized package document accepted")
	}
}
