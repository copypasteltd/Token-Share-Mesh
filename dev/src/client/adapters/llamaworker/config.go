package llamaworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"p11.local/compute/shared/gpuid"
	"path/filepath"
	"regexp"
	"strings"
)

type FilePin struct {
	Path   string
	SHA256 string
}

// Config is provisioned and approved locally; it MUST NOT be deserialized from
// an execution manifest. Libraries is the complete native library-directory
// allowlist. Model is single-shard GGUF; Template contains an approved built-in
// llama chat-template name (not arbitrary Jinja code). Paths stay under runtime.
type Config struct {
	RuntimeRoot                                    string
	Executable                                     FilePin
	Libraries                                      []FilePin
	Library, BackendRegistry, BackendCore          string
	Model, Template                                FilePin
	Environment, Capability, Resource              string
	GPUDevice                                      string // owner-approved native name
	GPUUUID, GPUPCI                                string // independently approved physical selection
	ContextTokens, BatchTokens, Threads, GPULayers uint32
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var splitPattern = regexp.MustCompile(`(?i)-[0-9]{5}-of-[0-9]{5}\.gguf$`)
var templatePattern = regexp.MustCompile(`^[a-z0-9_./-]{1,64}$`)

func pinned(ctx context.Context, root string, p FilePin) error {
	if !digestPattern.MatchString(p.SHA256) || !filepath.IsAbs(p.Path) {
		return ErrConfig
	}
	rel, e := filepath.Rel(root, p.Path)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ErrConfig
	}
	for at := filepath.Clean(p.Path); ; at = filepath.Dir(at) {
		s, e := os.Lstat(at)
		if e != nil || s.Mode()&os.ModeSymlink != 0 {
			return ErrConfig
		}
		if at == root {
			break
		}
		if filepath.Dir(at) == at {
			return ErrConfig
		}
	}
	f, e := os.Open(p.Path)
	if e != nil {
		return ErrConfig
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() {
		return ErrConfig
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	defer clear(buf)
	for {
		if ctx.Err() != nil {
			return ErrConfig
		}
		n, e := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return ErrConfig
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return ErrConfig
	}
	return nil
}
func (c Config) validate(ctx context.Context) (string, error) {
	root := filepath.Clean(c.RuntimeRoot)
	if !filepath.IsAbs(root) || !digestPattern.MatchString(c.Environment) || c.Capability == "" || len(c.Capability) > 192 || c.Resource == "" || len(c.Resource) > 192 ||
		c.ContextTokens < 32 || c.ContextTokens > 131072 || c.BatchTokens < 1 || c.BatchTokens > 2048 || c.BatchTokens > c.ContextTokens || c.Threads < 1 || c.Threads > 64 || c.GPULayers > 999 ||
		len(c.Libraries) < 1 || len(c.Libraries) > 64 || strings.ToLower(filepath.Ext(c.Model.Path)) != ".gguf" || splitPattern.MatchString(c.Model.Path) || len(c.GPUDevice) > 128 || strings.IndexByte(c.GPUDevice, 0) >= 0 || c.GPULayers > 0 && c.GPUDevice == "" || c.GPULayers == 0 && c.GPUDevice != "" {
		return "", ErrConfig
	}
	if err := validateGPU(c.GPUDevice, c.GPUUUID, c.GPUPCI, c.GPULayers); err != nil {
		return "", err
	}
	all := append([]FilePin{c.Executable, c.Model, c.Template}, c.Libraries...)
	for _, p := range all {
		if e := pinned(ctx, root, p); e != nil {
			return "", e
		}
	}
	dir := filepath.Dir(c.Library)
	seen := map[string]bool{}
	for _, p := range c.Libraries {
		if filepath.Dir(p.Path) != dir || seen[strings.ToLower(filepath.Base(p.Path))] {
			return "", ErrConfig
		}
		seen[strings.ToLower(filepath.Base(p.Path))] = true
	}
	if !seen[strings.ToLower(filepath.Base(c.Library))] || filepath.Dir(c.BackendRegistry) != dir || !seen[strings.ToLower(filepath.Base(c.BackendRegistry))] || filepath.Dir(c.BackendCore) != dir || !seen[strings.ToLower(filepath.Base(c.BackendCore))] {
		return "", ErrConfig
	}
	items, e := os.ReadDir(dir)
	if e != nil {
		return "", ErrConfig
	}
	for _, v := range items {
		name := strings.ToLower(v.Name())
		if strings.HasSuffix(name, ".dll") || strings.Contains(name, ".so") {
			if v.IsDir() || !seen[name] {
				return "", ErrConfig
			}
		}
	}
	b, e := os.ReadFile(c.Template.Path)
	if e != nil || len(b) > 64 || !templatePattern.Match(b) {
		return "", ErrConfig
	}
	defer clear(b)
	return string(b), nil
}

func validateGPU(device, uuid, pci string, layers uint32) error {
	if layers == 0 {
		if device != "" || uuid != "" || pci != "" {
			return ErrConfig
		}
		return nil
	}
	u, p, err := gpuid.Normalize(uuid, pci)
	if err != nil || u != uuid || p != pci || device == "" {
		return ErrConfig
	}
	return nil
}
