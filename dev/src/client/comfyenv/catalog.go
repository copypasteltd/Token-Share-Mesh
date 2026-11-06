// Package comfyenv verifies and installs complete immutable ComfyUI bundles.
// It never executes Python, pip, git, nodes, models or a package installer.
// Its caller must hold the enclosing node's OS lock and private root for the
// complete operation. Catalog metadata cannot grant GPU or sales authority.
package comfyenv

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const CatalogProfile = "P11_COMFY_ENVIRONMENT_CATALOG_1"
const ConsentProfile = "P11_LOCAL_COMFY_ENVIRONMENT_CONSENT_1"
const MaximumCatalogBytes = 32 << 20
const MaximumFiles = 200000
const MaximumCandidates = 32
const MaximumUnpackedBytes int64 = 200 << 30

var ErrCatalog = errors.New("ComfyUI package or independent owner consent rejected")
var ErrFiles = errors.New("ComfyUI installation differs from its exact approved files")
var ErrState = errors.New("owned ComfyUI installation state is unknown; retain candidate and resource")
var ErrBusy = errors.New("ComfyUI installation operation is occupied or quarantined")

type File struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Executable bool   `json:"executable"`
}
type Artifact struct {
	ID        string `json:"artifact_id"`
	SourceURL string `json:"source_url"`
	SHA256    string `json:"archive_sha256"`
	Bytes     int64  `json:"archive_bytes"`
	Files     []File `json:"files"`
}
type Component struct {
	ID          string   `json:"component_id"`
	Role        string   `json:"role"`
	Version     string   `json:"exact_version"`
	License     string   `json:"license_expression"`
	LicenseFile string   `json:"license_file"`
	Artifacts   []string `json:"artifact_ids"`
	Optional    bool     `json:"optional"`
}
type NodePack struct {
	ID           string   `json:"node_pack_id"`
	Revision     string   `json:"exact_revision"`
	Root         string   `json:"installed_root"`
	Classes      []string `json:"class_types"`
	Artifacts    []string `json:"artifact_ids"`
	Dependencies []string `json:"dependency_component_ids"`
}
type Catalog struct {
	Profile       string      `json:"profile_id"`
	Issuer        string      `json:"issuer_id"`
	Revision      string      `json:"allowlist_revision"`
	Issued        string      `json:"issued_at"`
	Expires       string      `json:"expires_at"`
	OS            string      `json:"os"`
	Architecture  string      `json:"architecture"`
	ComfyRevision string      `json:"comfy_commit"`
	Python        string      `json:"python_executable"`
	Bridge        string      `json:"private_bridge"`
	CoreClasses   []string    `json:"core_class_types"`
	Components    []Component `json:"components"`
	Artifacts     []Artifact  `json:"artifacts"`
	NodePacks     []NodePack  `json:"node_packs"`
}
type signedCatalog struct {
	Payload   json.RawMessage `json:"payload"`
	KeyID     string          `json:"key_id"`
	Signature string          `json:"signature"`
}

// Consent is independently supplied by a trusted local owner UI/configuration,
// never filled by copying a network catalog. Every selected component license
// and the exact uncompressed disk ceiling must be explicitly acknowledged.
type Consent struct {
	Profile            string   `json:"profile_id"`
	CatalogDigest      string   `json:"catalog_digest"`
	OS                 string   `json:"os"`
	Architecture       string   `json:"architecture"`
	SelectedNodePacks  []string `json:"selected_node_packs"`
	AcceptedComponents []string `json:"accepted_component_ids"`
	MaximumBytes       int64    `json:"maximum_unpacked_bytes"`
}

type planState struct {
	catalog               Catalog
	consent               Consent
	raw                   []byte
	digest, catalogDigest string
	since, until          time.Time
	files                 map[string]File
	artifacts             []Artifact
	unpacked              int64
}

// Plan is opaque and immutable. Accessors return copies, not internal slices.
type Plan struct{ state *planState }

func (p Plan) Digest() string {
	if p.state == nil {
		return ""
	}
	return p.state.digest
}
func (p Plan) CatalogDigest() string {
	if p.state == nil {
		return ""
	}
	return p.state.catalogDigest
}
func (p Plan) ValidAt(now time.Time) bool {
	return p.state != nil && !now.Before(p.state.since) && now.Before(p.state.until)
}
func (p Plan) Bytes() int64 {
	if p.state == nil {
		return 0
	}
	return p.state.unpacked
}
func (p Plan) Raw() []byte {
	if p.state == nil {
		return nil
	}
	return bytes.Clone(p.state.raw)
}
func (p Plan) Consent() Consent {
	if p.state == nil {
		return Consent{}
	}
	c := p.state.consent
	c.SelectedNodePacks = append([]string{}, c.SelectedNodePacks...)
	c.AcceptedComponents = append([]string{}, c.AcceptedComponents...)
	return c
}
func (p Plan) Catalog() Catalog {
	if p.state == nil {
		return Catalog{}
	}
	b, _ := json.Marshal(p.state.catalog)
	var c Catalog
	_ = json.Unmarshal(b, &c)
	return c
}

// Verifier owns separately provisioned pins and an explicit revocation snapshot.
// Neither public keys nor revocation state are accepted from the document.
type Verifier struct {
	mu      sync.RWMutex
	issuer  string
	pins    map[string]ed25519.PublicKey
	revoked map[string]bool
	closed  bool
}

func NewVerifier(issuer string, pins map[string]ed25519.PublicKey, revoked []string) (*Verifier, error) {
	if !id(issuer) || len(pins) < 1 || len(pins) > 16 || len(revoked) > 4096 {
		return nil, ErrCatalog
	}
	v := &Verifier{issuer: issuer, pins: map[string]ed25519.PublicKey{}, revoked: map[string]bool{}}
	for kid, key := range pins {
		if !id(kid) || len(key) != ed25519.PublicKeySize {
			return nil, ErrCatalog
		}
		v.pins[kid] = bytes.Clone(key)
	}
	for _, h := range revoked {
		if !hash(h) {
			return nil, ErrCatalog
		}
		v.revoked[h] = true
	}
	return v, nil
}
func (v *Verifier) Close() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	for _, key := range v.pins {
		clear(key)
	}
	clear(v.pins)
}
func (v *Verifier) Verify(raw []byte, consent Consent, now time.Time) (Plan, error) {
	if v == nil {
		return Plan{}, ErrCatalog
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	var signed signedCatalog
	if v.closed || strictjson.Decode(raw, &signed, MaximumCatalogBytes) != nil || !id(signed.KeyID) {
		return Plan{}, ErrCatalog
	}
	key, ok := v.pins[signed.KeyID]
	signature, e := base64.RawURLEncoding.Strict().DecodeString(signed.Signature)
	if !ok || e != nil || len(signature) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(signature) != signed.Signature {
		return Plan{}, ErrCatalog
	}
	digest := sum(signed.Payload)
	if v.revoked[digest] || !ed25519.Verify(key, append([]byte(CatalogProfile+"\n"), signed.Payload...), signature) {
		return Plan{}, ErrCatalog
	}
	var c Catalog
	if strictjson.Decode(signed.Payload, &c, MaximumCatalogBytes) != nil || c.Issuer != v.issuer {
		return Plan{}, ErrCatalog
	}
	return prepare(c, raw, digest, consent, now)
}

var partPattern = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.+-]{0,191}$`)
var classPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_ .:/+-]{0,127}$`)

func id(s string) bool { return len(s) <= 128 && ids.Valid(s) }
func hash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func revision(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func portable(s string) bool {
	if len(s) == 0 || len(s) > 1024 || path.Clean(s) != s || strings.HasPrefix(s, "/") || strings.Contains(s, "\\") {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		stem := strings.ToUpper(strings.SplitN(p, ".", 2)[0])
		reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9'
		if !partPattern.MatchString(p) || p == "." || p == ".." || strings.HasSuffix(p, ".") || reserved {
			return false
		}
	}
	return true
}
func filePath(s string) bool {
	if !portable(s) {
		return false
	}
	top, _, _ := strings.Cut(s, "/")
	return top == "python" || top == "comfy" || top == "bridge" || top == "tools"
}
func canonicalSet(values []string, valid func(string) bool) (map[string]bool, bool) {
	m := map[string]bool{}
	for _, v := range values {
		if !valid(v) || m[v] {
			return nil, false
		}
		m[v] = true
	}
	return m, true
}

func prepare(c Catalog, raw []byte, digest string, consent Consent, now time.Time) (Plan, error) {
	issued, e := time.Parse(time.RFC3339Nano, c.Issued)
	if e != nil {
		return Plan{}, ErrCatalog
	}
	until, e := time.Parse(time.RFC3339Nano, c.Expires)
	if e != nil || issued.UTC().Format(time.RFC3339Nano) != c.Issued || until.UTC().Format(time.RFC3339Nano) != c.Expires || c.Profile != CatalogProfile || !id(c.Revision) || !revision(c.ComfyRevision) || !now.Before(until) || now.Before(issued) || !issued.Before(until) || until.Sub(issued) > 366*24*time.Hour ||
		(c.OS != "windows" && c.OS != "linux") || c.Architecture != "amd64" || !filePath(c.Python) || !filePath(c.Bridge) || !strings.HasPrefix(c.Python, "python/") || !strings.HasPrefix(c.Bridge, "bridge/") ||
		len(c.Components) < 3 || len(c.Components) > 1024 || len(c.Artifacts) < 1 || len(c.Artifacts) > 512 || len(c.NodePacks) > 64 || len(c.CoreClasses) < 1 || len(c.CoreClasses) > 512 ||
		consent.Profile != ConsentProfile || consent.CatalogDigest != digest || consent.OS != c.OS || consent.Architecture != c.Architecture || consent.MaximumBytes < 1 || consent.MaximumBytes > MaximumUnpackedBytes {
		return Plan{}, ErrCatalog
	}
	classes, ok := canonicalSet(c.CoreClasses, classPattern.MatchString)
	if !ok {
		return Plan{}, ErrCatalog
	}
	selected, ok := canonicalSet(consent.SelectedNodePacks, id)
	if !ok {
		return Plan{}, ErrCatalog
	}
	accepted, ok := canonicalSet(consent.AcceptedComponents, id)
	if !ok {
		return Plan{}, ErrCatalog
	}
	artifacts := map[string]Artifact{}
	files := map[string]File{}
	folded := map[string]bool{}
	spelling := map[string]string{}
	total := int64(0)
	for _, a := range c.Artifacts {
		if !id(a.ID) || artifacts[a.ID].ID != "" || !hash(a.SHA256) || a.Bytes < 1 || a.Bytes > MaximumUnpackedBytes || len(a.Files) == 0 {
			return Plan{}, ErrCatalog
		}
		if !approvedSource(a.SourceURL) {
			return Plan{}, ErrCatalog
		}
		for _, f := range a.Files {
			lower := strings.ToLower(f.Path)
			if !filePath(f.Path) || !hash(f.SHA256) || f.Bytes < 0 || f.Bytes > MaximumUnpackedBytes || folded[lower] || len(files) >= MaximumFiles || f.Bytes > MaximumUnpackedBytes-total {
				return Plan{}, ErrCatalog
			}
			for parent := path.Dir(lower); parent != "."; parent = path.Dir(parent) {
				if folded[parent] {
					return Plan{}, ErrCatalog
				}
			}
			for name := f.Path; name != "."; name = path.Dir(name) {
				key := strings.ToLower(name)
				if previous, ok := spelling[key]; ok && previous != name {
					return Plan{}, ErrCatalog
				}
				spelling[key] = name
			}
			files[f.Path] = f
			folded[lower] = true
			total += f.Bytes
		}
		artifacts[a.ID] = a
	}
	// Detect a file declared after one of its descendants, including Windows
	// case folding; no directory/file collision may reach extraction.
	for name := range folded {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if folded[parent] {
				return Plan{}, ErrCatalog
			}
		}
	}
	components := map[string]Component{}
	roles := map[string]bool{}
	artifactOwners := map[string]string{}
	for _, component := range c.Components {
		if !id(component.ID) || components[component.ID].ID != "" || !exactVersion(component.Version) || len(component.License) < 1 || len(component.License) > 192 || !filePath(component.LicenseFile) || files[component.LicenseFile].Path == "" || len(component.Artifacts) == 0 {
			return Plan{}, ErrCatalog
		}
		switch component.Role {
		case "PYTHON", "COMFY_CORE", "PRIVATE_BRIDGE", "PYTHON_DEPENDENCY", "NODE_PACK", "MEDIA_TOOL":
		default:
			return Plan{}, ErrCatalog
		}
		if component.Role == "PYTHON" || component.Role == "COMFY_CORE" || component.Role == "PRIVATE_BRIDGE" {
			if roles[component.Role] {
				return Plan{}, ErrCatalog
			}
			roles[component.Role] = true
		}
		if component.Role == "COMFY_CORE" && component.Version != c.ComfyRevision {
			return Plan{}, ErrCatalog
		}
		if component.Optional && component.Role != "PYTHON_DEPENDENCY" {
			return Plan{}, ErrCatalog
		}
		for _, a := range component.Artifacts {
			if artifacts[a].ID == "" || artifactOwners[a] != "" {
				return Plan{}, ErrCatalog
			}
			artifactOwners[a] = component.ID
		}
		components[component.ID] = component
	}
	if len(artifactOwners) != len(artifacts) || !roles["PYTHON"] || !roles["COMFY_CORE"] || !roles["PRIVATE_BRIDGE"] || files[c.Python].Path == "" || files[c.Bridge].Path == "" || !files[c.Python].Executable {
		return Plan{}, ErrCatalog
	}
	for _, component := range c.Components {
		licenseOwned := false
		for _, artifact := range component.Artifacts {
			for _, file := range artifacts[artifact].Files {
				if file.Path == component.LicenseFile {
					licenseOwned = true
				}
				prefix := ""
				switch component.Role {
				case "PYTHON", "PYTHON_DEPENDENCY":
					prefix = "python/"
				case "COMFY_CORE":
					prefix = "comfy/"
				case "PRIVATE_BRIDGE":
					prefix = "bridge/"
				case "MEDIA_TOOL":
					prefix = "tools/"
				case "NODE_PACK":
					prefix = "comfy/custom_nodes/"
				}
				if !strings.HasPrefix(file.Path, prefix) || component.Role != "NODE_PACK" && strings.HasPrefix(file.Path, "comfy/custom_nodes/") || file.Path == c.Python && component.Role != "PYTHON" || file.Path == c.Bridge && component.Role != "PRIVATE_BRIDGE" {
					return Plan{}, ErrCatalog
				}
			}
		}
		if !licenseOwned {
			return Plan{}, ErrCatalog
		}
	}
	if files["comfy/execution.py"].Path == "" || files["comfy/nodes.py"].Path == "" {
		return Plan{}, ErrCatalog
	}
	nodeArtifacts := map[string]string{}
	packs := map[string]NodePack{}
	for _, p := range c.NodePacks {
		if !id(p.ID) || packs[p.ID].ID != "" || !exactVersion(p.Revision) || p.Root != "comfy/custom_nodes/"+p.ID || len(p.Classes) < 1 || len(p.Classes) > 256 || len(p.Artifacts) == 0 {
			return Plan{}, ErrCatalog
		}
		local, ok := canonicalSet(p.Classes, classPattern.MatchString)
		if !ok {
			return Plan{}, ErrCatalog
		}
		for class := range local {
			if classes[class] {
				return Plan{}, ErrCatalog
			}
			classes[class] = true
		}
		deps, ok := canonicalSet(p.Dependencies, id)
		if !ok {
			return Plan{}, ErrCatalog
		}
		for dep := range deps {
			if components[dep].ID == "" || components[dep].Role != "PYTHON_DEPENDENCY" {
				return Plan{}, ErrCatalog
			}
		}
		for _, a := range p.Artifacts {
			if artifacts[a].ID == "" || nodeArtifacts[a] != "" || components[artifactOwners[a]].Role != "NODE_PACK" {
				return Plan{}, ErrCatalog
			}
			nodeArtifacts[a] = p.ID
			for _, f := range artifacts[a].Files {
				if !strings.HasPrefix(f.Path, p.Root+"/") || strings.EqualFold(path.Base(f.Path), "prestartup_script.py") || strings.EqualFold(path.Base(f.Path), "install.py") {
					return Plan{}, ErrCatalog
				}
			}
		}
		packs[p.ID] = p
	}
	for p := range selected {
		if packs[p].ID == "" {
			return Plan{}, ErrCatalog
		}
	}
	requiredDependencies := map[string]bool{}
	for pack := range selected {
		for _, dep := range packs[pack].Dependencies {
			requiredDependencies[dep] = true
		}
	}
	chosen := []Artifact{}
	chosenFiles := map[string]File{}
	usedComponents := map[string]bool{}
	unpacked := int64(0)
	for _, a := range c.Artifacts {
		owner := components[artifactOwners[a.ID]]
		if owner.Optional && !requiredDependencies[owner.ID] {
			continue
		}
		if owner.Role == "NODE_PACK" {
			pack := nodeArtifacts[a.ID]
			if pack == "" {
				return Plan{}, ErrCatalog
			}
			if !selected[pack] {
				continue
			}
		}
		if !accepted[owner.ID] {
			return Plan{}, ErrCatalog
		}
		usedComponents[owner.ID] = true
		chosen = append(chosen, a)
		for _, f := range a.Files {
			chosenFiles[f.Path] = f
			unpacked += f.Bytes
		}
	}
	for component := range accepted {
		if !usedComponents[component] || chosenFiles[components[component].LicenseFile].Path == "" {
			return Plan{}, ErrCatalog
		}
	}
	for pack := range selected {
		for _, dep := range packs[pack].Dependencies {
			if !usedComponents[dep] {
				return Plan{}, ErrCatalog
			}
		}
	}
	if unpacked > consent.MaximumBytes {
		return Plan{}, ErrCatalog
	}
	consent.SelectedNodePacks = append([]string{}, consent.SelectedNodePacks...)
	consent.AcceptedComponents = append([]string{}, consent.AcceptedComponents...)
	sort.Strings(consent.SelectedNodePacks)
	sort.Strings(consent.AcceptedComponents)
	approval, _ := json.Marshal(consent)
	planDigest := sum(append([]byte(CatalogProfile+"\n"+digest+"\n"), approval...))
	return Plan{&planState{catalog: c, consent: consent, raw: bytes.Clone(raw), digest: planDigest, catalogDigest: digest, since: issued, until: until, files: chosenFiles, artifacts: chosen, unpacked: unpacked}}, nil
}
func exactVersion(v string) bool {
	if len(v) < 1 || len(v) > 128 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\r\n\x00*<>=~^") {
		return false
	}
	switch strings.ToLower(v) {
	case "latest", "main", "master", "head", "unfrozen":
		return false
	}
	return true
}
