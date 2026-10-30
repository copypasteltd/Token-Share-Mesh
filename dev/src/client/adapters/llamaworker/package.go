package llamaworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"p11.local/compute/shared/strictjson"
)

const PackageProfile = "P11_LLAMA_PACK_1"
const maximumPackageBytes = 64 << 10

// PackageFile uses portable relative paths. The entire document's exact bytes
// must also have an independently approved digest; parsing grants no authority.
type PackageFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Package struct {
	Schema          string        `json:"schema"`
	Profile         string        `json:"profile"`
	EngineRelease   string        `json:"engine_release"`
	Executable      PackageFile   `json:"executable"`
	Libraries       []PackageFile `json:"libraries"`
	Library         string        `json:"library"`
	BackendRegistry string        `json:"backend_registry"`
	BackendCore     string        `json:"backend_core"`
	Model           PackageFile   `json:"model"`
	Template        PackageFile   `json:"template"`
}

// OwnerSelection is local configuration, never a request or execution manifest.
// A zero GPU layer count is an explicit CPU selection, never automatic fallback.
type OwnerSelection struct {
	Capability    string `json:"capability"`
	Resource      string `json:"resource"`
	GPUDevice     string `json:"gpu_device"`
	ContextTokens uint32 `json:"context_tokens"`
	BatchTokens   uint32 `json:"batch_tokens"`
	Threads       uint32 `json:"threads"`
	GPULayers     uint32 `json:"gpu_layers"`
}

// PackageEnvironment binds the exact package plus execution parameters. It is
// a deterministic local identity, NOT a signed approval or device qualification.
// Resource/capability remain separately checked by Host's approved rule.
func PackageEnvironment(packageSHA256 string, s OwnerSelection) (string, error) {
	if !digestPattern.MatchString(packageSHA256) || s.ContextTokens < 32 || s.ContextTokens > 131072 ||
		s.BatchTokens < 1 || s.BatchTokens > 2048 || s.BatchTokens > s.ContextTokens || s.Threads < 1 || s.Threads > 64 ||
		s.GPULayers > 999 || len(s.GPUDevice) > 128 || strings.IndexByte(s.GPUDevice, 0) >= 0 || (s.GPULayers > 0) != (s.GPUDevice != "") {
		return "", ErrConfig
	}
	parameters := struct {
		GPUDevice                                      string
		ContextTokens, BatchTokens, Threads, GPULayers uint32
	}{s.GPUDevice, s.ContextTokens, s.BatchTokens, s.Threads, s.GPULayers}
	b, err := json.Marshal(parameters)
	if err != nil {
		return "", ErrConfig
	}
	h := sha256.New()
	_, _ = h.Write([]byte("P11_LLAMA_ENV_1\x00" + packageSHA256 + "\x00"))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

var packagePart = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,127}$`)

func packagePath(base, relative string) (string, error) {
	if len(relative) == 0 || len(relative) > 512 || strings.Contains(relative, "\\") {
		return "", ErrConfig
	}
	for _, part := range strings.Split(relative, "/") {
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
			len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9'
		if !packagePart.MatchString(part) || strings.HasSuffix(part, ".") || reserved {
			return "", ErrConfig
		}
	}
	return filepath.Join(base, filepath.FromSlash(relative)), nil
}

// LoadPackage reads and verifies a complete, independently pinned installation.
// It starts no process/listener, performs no inference and mints no approval.
// Open rechecks all files immediately before starting its owned native worker.
func LoadPackage(ctx context.Context, runtimeRoot string, document FilePin, s OwnerSelection, approvedEnvironment string) (Config, error) {
	var empty Config
	environment, err := PackageEnvironment(document.SHA256, s)
	if err != nil || approvedEnvironment != environment || !filepath.IsAbs(runtimeRoot) || !filepath.IsAbs(document.Path) {
		return empty, ErrConfig
	}
	info, err := os.Stat(document.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximumPackageBytes || pinned(ctx, filepath.Clean(runtimeRoot), document) != nil {
		return empty, ErrConfig
	}
	f, err := os.Open(document.Path)
	if err != nil {
		return empty, ErrConfig
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maximumPackageBytes+1))
	if err != nil || len(raw) > maximumPackageBytes {
		return empty, ErrConfig
	}
	defer clear(raw)
	// Detect replacement between the initial streaming pin and the bounded read.
	actual := sha256.Sum256(raw)
	if hex.EncodeToString(actual[:]) != document.SHA256 {
		return empty, ErrConfig
	}
	var p Package
	if strictjson.Decode(raw, &p, maximumPackageBytes) != nil || p.Schema != PackageProfile || p.Profile != Profile || p.EngineRelease != EngineRelease || len(p.Libraries) < 1 || len(p.Libraries) > 64 {
		return empty, ErrConfig
	}
	base := filepath.Dir(document.Path)
	pin := func(v PackageFile) (FilePin, error) {
		path, e := packagePath(base, v.Path)
		return FilePin{Path: path, SHA256: v.SHA256}, e
	}
	c := Config{RuntimeRoot: filepath.Clean(runtimeRoot), Environment: environment, Capability: s.Capability, Resource: s.Resource,
		GPUDevice: s.GPUDevice, ContextTokens: s.ContextTokens, BatchTokens: s.BatchTokens, Threads: s.Threads, GPULayers: s.GPULayers}
	if c.Executable, err = pin(p.Executable); err != nil {
		return empty, err
	}
	if c.Model, err = pin(p.Model); err != nil {
		return empty, err
	}
	if c.Template, err = pin(p.Template); err != nil {
		return empty, err
	}
	if c.Library, err = packagePath(base, p.Library); err != nil {
		return empty, err
	}
	if c.BackendRegistry, err = packagePath(base, p.BackendRegistry); err != nil {
		return empty, err
	}
	if c.BackendCore, err = packagePath(base, p.BackendCore); err != nil {
		return empty, err
	}
	for _, v := range p.Libraries {
		file, e := pin(v)
		if e != nil {
			return empty, e
		}
		c.Libraries = append(c.Libraries, file)
	}
	if _, err = c.validate(ctx); err != nil {
		return empty, err
	}
	return c, nil
}
