package comfyworker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/gpuid"
)

const UpstreamCommit = "2c75dc1b379a59e0880619d029607af5169a62ef"

var modelAlias = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.+-]{0,191}$`)

// RuntimeSelection is independently approved host configuration, NEVER a
// buyer JSON body or a downloaded runtime discovery response. Paths/aliases
// cannot be chosen by an invocation. File bytes are checked before imports.
type RuntimeSelection struct {
	GPU            gpuid.Identity
	Driver, Recipe string
	Models         []ModelFile
	Extras         []Module
}

func (s RuntimeSelection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[local ComfyUI runtime selection redacted]")
}
func (s RuntimeSelection) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

type ModelFile struct {
	Category string `json:"category"`
	Alias    string `json:"alias"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}
type Module struct {
	Path    string   `json:"path"`
	Parent  string   `json:"module_parent"`
	Classes []string `json:"classes"`
}
type runtimeFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type environment struct {
	Profile     string        `json:"profile_id"`
	Commit      string        `json:"upstream_commit"`
	Digest      string        `json:"environment_digest"`
	Root        string        `json:"bundle_root"`
	Files       []runtimeFile `json:"files"`
	Core        []string      `json:"core_classes"`
	Modules     []Module      `json:"modules"`
	Models      []ModelFile   `json:"model_files"`
	Recipe      string        `json:"recipe_path"`
	RecipeHash  string        `json:"recipe_sha256"`
	RecipeBytes int64         `json:"recipe_bytes"`
	Driver      string        `json:"cuda_driver_path"`
	UUID        string        `json:"gpu_uuid"`
	PCI         string        `json:"gpu_pci"`
}
type session struct {
	ID          string `json:"session_id"`
	Resource    string `json:"resource_id"`
	Generation  string `json:"local_generation"`
	Environment string `json:"environment_digest"`
	Recipe      string `json:"recipe_digest"`
	HardMS      int64  `json:"hard_deadline_unix_ms"`
}
type initialization struct {
	Session     session     `json:"session"`
	Environment environment `json:"environment"`
}
type launchSpec struct {
	root, python, bridge, uuid string
	initial                    []byte
	session                    session
}

// ModelSetDigest binds all immutable category/alias/size/hash choices, excluding
// machine-specific paths, using a versioned canonical sorted JSON array. It is
// a model-set digest, not a ciphertext/content digest or a hardware attestation.
func ModelSetDigest(models []ModelFile) (string, error) {
	if len(models) < 1 || len(models) > 64 {
		return "", ErrOutput
	}
	type entry struct {
		Category string `json:"category"`
		Alias    string `json:"alias"`
		Bytes    int64  `json:"bytes"`
		SHA256   string `json:"sha256"`
	}
	categories := map[string]bool{"checkpoints": true, "text_encoders": true, "diffusion_models": true, "vae": true, "clip_vision": true, "controlnet": true, "loras": true, "embeddings": true, "upscale_models": true}
	rows := make([]entry, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		key := m.Category + "/" + m.Alias
		if !categories[m.Category] || !modelAlias.MatchString(m.Alias) || m.Alias == "." || m.Alias == ".." || seen[key] || m.Bytes < 1 || m.Bytes > 200<<30 || !executionlease.Hash(m.SHA256) {
			return "", ErrOutput
		}
		seen[key] = true
		rows = append(rows, entry{m.Category, m.Alias, m.Bytes, m.SHA256})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Category == rows[j].Category {
			return rows[i].Alias < rows[j].Alias
		}
		return rows[i].Category < rows[j].Category
	})
	raw, e := json.Marshal(rows)
	if e != nil {
		return "", ErrOutput
	}
	defer clear(raw)
	h := sha256.New()
	_, _ = h.Write([]byte("P11_COMFY_MODEL_SET_1\x00"))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func buildLaunch(use comfyenv.Use, s RuntimeSelection) (launchSpec, error) {
	l, e := use.Layout()
	if e != nil {
		return launchSpec{}, ErrOutput
	}
	h, p := use.Original()
	m := p.Details()
	if m.Engine != "COMFYUI" || m.Release != Release || m.EnvironmentHash != l.Environment || m.PublicSpec.WorkflowID == "" || m.PublicSpec.ModelID != "" || l.Commit != UpstreamCommit || s.GPU.Validate() != nil {
		return launchSpec{}, ErrOutput
	}
	physical, e := gpuid.Digest(s.GPU.UUID)
	if e != nil || physical != p.Approval().PhysicalHash {
		return launchSpec{}, ErrOutput
	}
	modelsHash, e := ModelSetDigest(s.Models)
	if e != nil || modelsHash != m.ModelHash {
		return launchSpec{}, ErrOutput
	}
	files := map[string]comfyenv.File{}
	env := environment{Profile: Profile, Commit: l.Commit, Digest: l.Environment, Root: l.Root, Core: []string{}, Modules: []Module{}, Models: append([]ModelFile{}, s.Models...), Files: []runtimeFile{}, Recipe: s.Recipe, RecipeHash: m.RecipeHash, Driver: s.Driver, UUID: s.GPU.UUID, PCI: "0000" + s.GPU.PCI}
	for _, f := range l.Files {
		files[f.Path] = f
		env.Files = append(env.Files, runtimeFile{f.Path, f.SHA256, f.Bytes})
	}
	recipe, ok := files[s.Recipe]
	if !ok || !strings.HasPrefix(s.Recipe, "bridge/") || recipe.SHA256 != m.RecipeHash || recipe.Bytes < 1 || recipe.Bytes > 2<<20 || l.Bridge != "bridge/worker.py" || !files[l.Python].Executable || files[l.Bridge].Bytes < 1 {
		return launchSpec{}, ErrOutput
	}
	env.RecipeBytes = recipe.Bytes
	for _, model := range s.Models {
		if !filepath.IsAbs(model.Path) || filepath.Clean(model.Path) != model.Path || filepath.Ext(model.Path) != ".safetensors" {
			return launchSpec{}, ErrOutput
		}
	}
	if runtime.GOOS == "windows" {
		if !validWindowsDriver(s.Driver) {
			return launchSpec{}, ErrOutput
		}
	} else if runtime.GOOS != "linux" || (s.Driver != "/usr/lib/x86_64-linux-gnu/libcuda.so.1" && s.Driver != "/usr/lib64/libcuda.so.1") {
		return launchSpec{}, ErrOutput
	}
	classes := map[string]bool{}
	modules := map[string]bool{}
	all := map[string]bool{}
	for _, c := range l.CoreClasses {
		all[c] = true
	}
	for _, extra := range s.Extras {
		if !strings.HasPrefix(extra.Path, "comfy/comfy_extras/") || extra.Parent != "comfy_extras" || files[extra.Path].Bytes < 1 || modules[extra.Path] || len(extra.Classes) < 1 {
			return launchSpec{}, ErrOutput
		}
		modules[extra.Path] = true
		for _, c := range extra.Classes {
			if !all[c] || classes[c] {
				return launchSpec{}, ErrOutput
			}
			classes[c] = true
		}
		env.Modules = append(env.Modules, Module{extra.Path, extra.Parent, append([]string{}, extra.Classes...)})
	}
	for _, c := range l.CoreClasses {
		if !classes[c] {
			env.Core = append(env.Core, c)
		}
	}
	for _, pack := range l.NodePacks {
		env.Modules = append(env.Modules, Module{pack.Root, "custom_nodes", append([]string{}, pack.Classes...)})
	}
	if len(env.Core) < 1 || len(env.Modules) > 64 {
		return launchSpec{}, ErrOutput
	}
	remaining := time.Until(use.PreparationDeadline())
	if remaining <= 0 || remaining > 2*time.Minute {
		return launchSpec{}, ErrOutput
	}
	sess := session{h.Owner(), h.Resource(), h.LocalGeneration(), l.Environment, m.RecipeHash, use.PreparationDeadline().UnixMilli()}
	raw, e := json.Marshal(initialization{sess, env})
	if e != nil || len(raw) > inputLimit(Initialize) {
		clear(raw)
		return launchSpec{}, ErrOutput
	}
	return launchSpec{root: l.Root, python: filepath.Join(l.Root, filepath.FromSlash(l.Python)), bridge: filepath.Join(l.Root, filepath.FromSlash(l.Bridge)), uuid: s.GPU.UUID, initial: raw, session: sess}, nil
}
