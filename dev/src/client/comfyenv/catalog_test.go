package comfyenv

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// SOURCE ONLY. These tiny synthetic bytes are not Python/ComfyUI/model packs,
// and never qualify real hardware, licenses, runtime safety or media execution.
type catalogFixture struct {
	c        Catalog
	consent  Consent
	key      ed25519.PrivateKey
	v        *Verifier
	archives map[string][]byte
}

func zipFixture(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0600)
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(files[name]); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return bytes.Clone(buffer.Bytes())
}
func newCatalogFixture(t *testing.T, withNode bool) *catalogFixture {
	t.Helper()
	now := time.Now().UTC()
	f := &catalogFixture{key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{37}, 32)), archives: map[string][]byte{}}
	f.c = Catalog{Profile: CatalogProfile, Issuer: "issuer_synthetic", Revision: "allowlist_synthetic_1", Issued: now.Add(-time.Minute).Format(time.RFC3339Nano), Expires: now.Add(time.Hour).Format(time.RFC3339Nano), OS: runtime.GOOS, Architecture: "amd64", ComfyRevision: strings.Repeat("a", 40), Python: "python/python.exe", Bridge: "bridge/main.py", CoreClasses: []string{"SyntheticCore"}, Components: []Component{}, Artifacts: []Artifact{}, NodePacks: []NodePack{}}
	add := func(id, role, version, license string, files map[string][]byte, optional bool) {
		archive := zipFixture(t, files)
		a := Artifact{ID: "artifact_" + id, SourceURL: "https://packages.example.invalid/" + id + ".zip", SHA256: sum(archive), Bytes: int64(len(archive)), Files: []File{}}
		for name, b := range files {
			a.Files = append(a.Files, File{Path: name, SHA256: sum(b), Bytes: int64(len(b)), Executable: name == f.c.Python})
		}
		sort.Slice(a.Files, func(i, j int) bool { return a.Files[i].Path < a.Files[j].Path })
		f.c.Components = append(f.c.Components, Component{ID: id, Role: role, Version: version, License: "SYNTHETIC_TEST_LICENSE", LicenseFile: license, Artifacts: []string{a.ID}, Optional: optional})
		f.c.Artifacts = append(f.c.Artifacts, a)
		f.archives[a.ID] = archive
	}
	add("python_synthetic", "PYTHON", "3.12.0", "python/LICENSE.txt", map[string][]byte{"python/python.exe": []byte("NOT_AN_EXECUTABLE"), "python/LICENSE.txt": []byte("synthetic license")}, false)
	add("core_synthetic", "COMFY_CORE", f.c.ComfyRevision, "comfy/LICENSE", map[string][]byte{"comfy/execution.py": []byte("SYNTHETIC_NO_CODE"), "comfy/nodes.py": []byte("SYNTHETIC_NO_CODE"), "comfy/LICENSE": []byte("synthetic license")}, false)
	add("bridge_synthetic", "PRIVATE_BRIDGE", "bridge_1", "bridge/LICENSE", map[string][]byte{"bridge/main.py": []byte("SYNTHETIC_NO_CODE"), "bridge/LICENSE": []byte("synthetic license")}, false)
	if withNode {
		add("dependency_synthetic", "PYTHON_DEPENDENCY", "1.0.0", "python/dependency/LICENSE", map[string][]byte{"python/dependency/LICENSE": []byte("synthetic license"), "python/dependency/data.bin": []byte("fixture bytes")}, true)
		add("node_component", "NODE_PACK", "commit_synthetic_1", "comfy/custom_nodes/node_synthetic/LICENSE", map[string][]byte{"comfy/custom_nodes/node_synthetic/LICENSE": []byte("synthetic license"), "comfy/custom_nodes/node_synthetic/__init__.py": []byte("SYNTHETIC_NO_CODE")}, false)
		f.c.NodePacks = []NodePack{{ID: "node_synthetic", Revision: "commit_synthetic_1", Root: "comfy/custom_nodes/node_synthetic", Classes: []string{"SyntheticDecoder"}, Artifacts: []string{"artifact_node_component"}, Dependencies: []string{"dependency_synthetic"}}}
	}
	f.consent = Consent{Profile: ConsentProfile, OS: runtime.GOOS, Architecture: "amd64", SelectedNodePacks: []string{}, AcceptedComponents: []string{"python_synthetic", "core_synthetic", "bridge_synthetic"}, MaximumBytes: 8 << 20}
	if withNode {
		f.consent.SelectedNodePacks = []string{"node_synthetic"}
		f.consent.AcceptedComponents = append(f.consent.AcceptedComponents, "dependency_synthetic", "node_component")
	}
	var e error
	f.v, e = NewVerifier(f.c.Issuer, map[string]ed25519.PublicKey{"key_synthetic": f.key.Public().(ed25519.PublicKey)}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(f.v.Close)
	return f
}
func (f *catalogFixture) signed(t *testing.T) ([]byte, Consent) {
	t.Helper()
	payload, e := json.Marshal(f.c)
	if e != nil {
		t.Fatal(e)
	}
	c := f.consent
	c.CatalogDigest = sum(payload)
	signature := ed25519.Sign(f.key, append([]byte(CatalogProfile+"\n"), payload...))
	raw, e := json.Marshal(signedCatalog{payload, "key_synthetic", base64.RawURLEncoding.EncodeToString(signature)})
	if e != nil {
		t.Fatal(e)
	}
	return raw, c
}
func (f *catalogFixture) plan(t *testing.T) Plan {
	t.Helper()
	raw, c := f.signed(t)
	p, e := f.v.Verify(raw, c, time.Now().UTC())
	if e != nil {
		t.Fatal("synthetic signed package fixture refused", e)
	}
	return p
}

func TestComfyCatalogRequiresIndependentPinsConsentAndCurrentRevocations(t *testing.T) {
	f := newCatalogFixture(t, true)
	raw, c := f.signed(t)
	if _, e := f.v.Verify(raw, c, time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	bad := c
	bad.CatalogDigest = strings.Repeat("b", 64)
	if _, e := f.v.Verify(raw, bad, time.Now().UTC()); e == nil {
		t.Fatal("network catalog self-approved")
	}
	bad = c
	bad.AcceptedComponents = []string{"python_synthetic"}
	if _, e := f.v.Verify(raw, bad, time.Now().UTC()); e == nil {
		t.Fatal("missing license consent accepted")
	}
	v, e := NewVerifier(f.c.Issuer, map[string]ed25519.PublicKey{"key_synthetic": f.key.Public().(ed25519.PublicKey)}, []string{c.CatalogDigest})
	if e != nil {
		t.Fatal(e)
	}
	defer v.Close()
	if _, e = v.Verify(raw, c, time.Now().UTC()); e == nil {
		t.Fatal("revoked catalog accepted")
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{38}, 32))
	v2, e := NewVerifier(f.c.Issuer, map[string]ed25519.PublicKey{"key_synthetic": other.Public().(ed25519.PublicKey)}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer v2.Close()
	if _, e = v2.Verify(raw, c, time.Now().UTC()); e == nil {
		t.Fatal("foreign signer accepted")
	}
}
func TestComfyCatalogSelectsFiniteNodesAndOnlyTheirOptionalDependencies(t *testing.T) {
	f := newCatalogFixture(t, true)
	selected := f.plan(t)
	f.consent.SelectedNodePacks = []string{}
	f.consent.AcceptedComponents = []string{"python_synthetic", "core_synthetic", "bridge_synthetic"}
	base := f.plan(t)
	if len(selected.state.artifacts) != 5 || len(base.state.artifacts) != 3 || selected.Digest() == base.Digest() {
		t.Fatal("node selection did not define separate complete immutable environments")
	}
	for name := range base.state.files {
		if strings.Contains(name, "node_synthetic") || strings.Contains(name, "dependency/") {
			t.Fatal("unselected pack/dependency installed")
		}
	}
	f.consent.SelectedNodePacks = []string{"buyer_arbitrary_repository"}
	raw, c := f.signed(t)
	if _, e := f.v.Verify(raw, c, time.Now()); e == nil {
		t.Fatal("unknown node accepted")
	}
}
func TestComfyCatalogRejectsPathsCaseAliasesHooksAndCrossRoleOverrides(t *testing.T) {
	cases := map[string]func(*catalogFixture){
		"traversal":        func(f *catalogFixture) { f.c.Artifacts[0].Files[0].Path = "python/../outside" },
		"device_name":      func(f *catalogFixture) { f.c.Artifacts[0].Files[0].Path = "python/CON.txt" },
		"alternate_stream": func(f *catalogFixture) { f.c.Artifacts[0].Files[0].Path = "python/a:payload" },
		"case_directory": func(f *catalogFixture) {
			f.c.Artifacts[0].Files = append(f.c.Artifacts[0].Files, File{Path: "Python/other.txt", SHA256: sum(nil), Bytes: 0})
		},
		"lower_case_directory_alias": func(f *catalogFixture) {
			f.c.Artifacts[0].Files = append(f.c.Artifacts[0].Files, File{Path: "python/Lib/a", SHA256: sum(nil)}, File{Path: "python/lib/b", SHA256: sum(nil)})
		},
		"core_custom_node": func(f *catalogFixture) {
			f.c.Artifacts[1].Files = append(f.c.Artifacts[1].Files, File{Path: "comfy/custom_nodes/injected.py", SHA256: sum(nil)})
		},
		"install_hook": func(f *catalogFixture) {
			f.c.Artifacts[4].Files[0].Path = "comfy/custom_nodes/node_synthetic/install.py"
			f.c.Components[4].LicenseFile = f.c.Artifacts[4].Files[0].Path
		},
		"prestartup_hook": func(f *catalogFixture) {
			f.c.Artifacts[4].Files[0].Path = "comfy/custom_nodes/node_synthetic/prestartup_script.py"
			f.c.Components[4].LicenseFile = f.c.Artifacts[4].Files[0].Path
		},
		"class_override":       func(f *catalogFixture) { f.c.NodePacks[0].Classes = []string{"SyntheticCore"} },
		"floating_version":     func(f *catalogFixture) { f.c.Components[0].Version = "latest" },
		"core_commit_mismatch": func(f *catalogFixture) { f.c.Components[1].Version = strings.Repeat("f", 40) },
		"foreign_license":      func(f *catalogFixture) { f.c.Components[0].LicenseFile = "bridge/LICENSE" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCatalogFixture(t, true)
			mutate(f)
			raw, c := f.signed(t)
			if _, e := f.v.Verify(raw, c, time.Now()); e == nil {
				t.Fatal("unsafe complete bundle accepted")
			}
		})
	}
}
func TestComfyCatalogImmutablePlanCopiesAndExactExpiry(t *testing.T) {
	f := newCatalogFixture(t, true)
	p := f.plan(t)
	original := p.Digest()
	c := p.Catalog()
	c.Components[0].Artifacts[0] = "changed"
	consent := p.Consent()
	consent.SelectedNodePacks[0] = "changed"
	raw := p.Raw()
	clear(raw)
	if p.Digest() != original || p.Catalog().Components[0].Artifacts[0] == "changed" || p.Consent().SelectedNodePacks[0] == "changed" {
		t.Fatal("caller mutated original plan")
	}
	if p.ValidAt(p.state.until) {
		t.Fatal("exact expiry accepted")
	}
	f.v.Close()
	signed, approval := f.signed(t)
	if _, e := f.v.Verify(signed, approval, time.Now()); e == nil {
		t.Fatal("closed pins reused")
	}
}
func TestComfyCatalogStrictJSONAndDiskBudget(t *testing.T) {
	f := newCatalogFixture(t, false)
	raw, c := f.signed(t)
	unknown := append([]byte(`{"unknown":true,`), raw[1:]...)
	if _, e := f.v.Verify(unknown, c, time.Now()); e == nil {
		t.Fatal("unknown envelope field accepted")
	}
	c.MaximumBytes = 1
	if _, e := f.v.Verify(raw, c, time.Now()); e == nil {
		t.Fatal("owner disk ceiling exceeded")
	}
	if _, e := f.v.Verify(raw, c, time.Now().Add(24*time.Hour)); e == nil {
		t.Fatal("expired catalog accepted")
	}
}

type memoryJournal struct {
	root    *os.Root
	sealed  []byte
	fail    bool
	commits int
}

func (j *memoryJournal) Root() *os.Root { return j.root }
func (j *memoryJournal) Read() ([]byte, error) {
	if j.sealed == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(j.sealed), nil
}
func (j *memoryJournal) Commit(b []byte) error {
	j.commits++
	if j.fail {
		return os.ErrPermission
	}
	j.sealed = bytes.Clone(b)
	return nil
}
func fixtureRegistry(t *testing.T, f *catalogFixture) (*Registry, *memoryJournal, map[string]string) {
	t.Helper()
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	j := &memoryJournal{root: root}
	r, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(r.Close)
	archives := map[string]string{}
	for id, b := range f.archives {
		name := "downloads/" + id + ".zip"
		if e := writeNew(root, name, b); e != nil {
			t.Fatal(e)
		}
		archives[id] = name
	}
	return r, j, archives
}
func fixtureInstall(t *testing.T) (*catalogFixture, Plan, *Registry, *memoryJournal, map[string]string, Candidate) {
	t.Helper()
	f := newCatalogFixture(t, false)
	p := f.plan(t)
	r, j, archives := fixtureRegistry(t, f)
	c, e := r.Install(context.Background(), p, archives)
	if e != nil {
		t.Fatal(e)
	}
	return f, p, r, j, archives, c
}
