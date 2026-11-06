package comfyenv

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestComfyInstallExactFullTreeAndFreshPinsReadback(t *testing.T) {
	f, p, r, j, _, candidate := fixtureInstall(t)
	if candidate.Phase != "VERIFIED" || r.VerifyInstalled(context.Background(), p) != nil {
		t.Fatal("exact complete bundle missing")
	}
	if bytes.Contains(j.sealed, []byte("allowlist_synthetic")) || bytes.Contains(j.sealed, []byte(p.Digest())) {
		t.Fatal("metadata journal stored plaintext")
	}
	current, e := r.ReadPlan(context.Background(), p.Digest(), f.v, p.Consent())
	if e != nil || current.Digest() != p.Digest() {
		t.Fatal(e)
	}
	bad := p.Consent()
	bad.MaximumBytes++
	if _, e = r.ReadPlan(context.Background(), p.Digest(), f.v, bad); e == nil {
		t.Fatal("different local approval adopted installed tree")
	}
	r.Close()
	reopened, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if reopened.VerifyInstalled(context.Background(), p) != nil {
		t.Fatal("original exact metadata failed reopen")
	}
}
func TestComfyInstallDirectoryDriftQuarantinesWithoutFallback(t *testing.T) {
	_, p, r, j, _, _ := fixtureInstall(t)
	if e := writeNew(j.root, bundleRoot+"/environments/"+p.Digest()+"/comfy/custom_nodes/injected/__init__.py", []byte("SYNTHETIC_NO_CODE")); e != nil {
		t.Fatal(e)
	}
	if r.VerifyInstalled(context.Background(), p) == nil {
		t.Fatal("unapproved directory ignored")
	}
	list, _, e := r.Snapshot()
	if e != nil || len(list) != 1 || list[0].Phase != "QUARANTINED" {
		t.Fatal("drift did not preserve original candidate", e)
	}
}
func TestComfyInstallDamagedOriginalArchiveKeepsStageAndCandidate(t *testing.T) {
	f := newCatalogFixture(t, false)
	p := f.plan(t)
	r, j, archives := fixtureRegistry(t, f)
	name := archives[p.state.artifacts[0].ID]
	file, e := j.root.OpenFile(name, os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.WriteAt([]byte{0}, 0); e != nil {
		t.Fatal(e)
	}
	file.Close()
	c, e := r.Install(context.Background(), p, archives)
	if e == nil || c.Phase != "QUARANTINED" {
		t.Fatal("damaged archive accepted")
	}
	if _, e = j.root.Stat(bundleRoot + "/stages/" + c.Stage); e != nil {
		t.Fatal("failed stage was silently removed", e)
	}
	if _, e = r.Install(context.Background(), p, archives); e == nil {
		t.Fatal("same failed environment retried with new identity")
	}
}
func TestComfyArchiveRejectsExtraTraversalAndCaseEntries(t *testing.T) {
	cases := map[string]string{"extra": "python/extra.py", "traversal": "../escape", "case_alias": "Python/python.exe"}
	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			f := newCatalogFixture(t, false)
			a := f.c.Artifacts[0]
			files := map[string][]byte{}
			for _, file := range a.Files {
				if file.Path == f.c.Python {
					files[file.Path] = []byte("NOT_AN_EXECUTABLE")
				} else {
					files[file.Path] = []byte("synthetic license")
				}
			}
			files[name] = []byte("UNLISTED_BYTES")
			archive := zipFixture(t, files)
			f.archives[a.ID] = archive
			f.c.Artifacts[0].Bytes = int64(len(archive))
			f.c.Artifacts[0].SHA256 = sum(archive)
			p := f.plan(t)
			r, _, archives := fixtureRegistry(t, f)
			if _, e := r.Install(context.Background(), p, archives); e == nil {
				t.Fatal("unlisted ZIP entry accepted")
			}
		})
	}
}
func TestComfyArchiveBoundsRejectBeforeEntryAllocation(t *testing.T) {
	f := newCatalogFixture(t, false)
	archive := bytes.Clone(f.archives[f.c.Artifacts[0].ID])
	end := len(archive) - 22
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint16(b[end+4:], 1) },
		func(b []byte) {
			binary.LittleEndian.PutUint16(b[end+10:], 60000)
			binary.LittleEndian.PutUint16(b[end+8:], 60000)
		},
		func(b []byte) { binary.LittleEndian.PutUint32(b[end+12:], maximumDirectoryBytes+1) },
	} {
		b := bytes.Clone(archive)
		mutate(b)
		file, e := os.CreateTemp(t.TempDir(), "archive-")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.Write(b); e != nil {
			t.Fatal(e)
		}
		if archiveBounds(context.Background(), file, int64(len(b))) == nil {
			t.Fatal("malformed archive metadata accepted")
		}
		file.Close()
	}
}
func TestComfyRegistryInterruptedInstallationNeverAdoptsTree(t *testing.T) {
	f := newCatalogFixture(t, false)
	p := f.plan(t)
	r, j, _ := fixtureRegistry(t, f)
	r.mu.Lock()
	r.s.Candidates = append(r.s.Candidates, Candidate{Digest: p.Digest(), CatalogDigest: p.CatalogDigest(), AllowlistRevision: f.c.Revision, Stage: strings.Repeat("c", 64), Phase: "COMMITTING"})
	e := r.persistLocked()
	r.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	next, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), false)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	list, _, e := next.Snapshot()
	if e != nil || len(list) != 1 || list[0].Phase != "QUARANTINED" {
		t.Fatal("interrupted candidate adopted", e)
	}
	if next.VerifyInstalled(context.Background(), p) == nil {
		t.Fatal("unconfirmed final path activated")
	}
}
func TestComfyRegistryMissingCorruptOrDifferentBindingNeverRecreated(t *testing.T) {
	f := newCatalogFixture(t, false)
	r, j, _ := fixtureRegistry(t, f)
	r.Close()
	sealed := bytes.Clone(j.sealed)
	for _, b := range [][]byte{nil, {}, []byte("PLAINTEXT_NOT_A_JOURNAL"), append(bytes.Clone(sealed), 0)} {
		j.sealed = b
		if opened, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), false); e == nil {
			opened.Close()
			t.Fatal("missing/corrupt anchored metadata accepted")
		}
	}
	j.sealed = sealed
	if opened, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("d", 64), false); e == nil {
		opened.Close()
		t.Fatal("foreign installation binding accepted")
	}
	before := j.commits
	if opened, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), true); e == nil {
		opened.Close()
		t.Fatal("existing journal overwritten")
	}
	if j.commits != before {
		t.Fatal("failed create changed original journal")
	}
}
func TestComfyRegistryCommitFailurePoisonsOriginalMetadata(t *testing.T) {
	f := newCatalogFixture(t, false)
	p := f.plan(t)
	r, j, archives := fixtureRegistry(t, f)
	j.fail = true
	if _, e := r.Install(context.Background(), p, archives); !errors.Is(e, ErrState) {
		t.Fatal("uncertain commit not closed", e)
	}
	j.fail = false
	if _, _, e := r.Snapshot(); e == nil {
		t.Fatal("poisoned original registry silently reopened")
	}
	if _, e := r.Install(context.Background(), p, archives); e == nil {
		t.Fatal("poisoned state allowed replacement attempt")
	}
}
func TestComfyDeleteTombstoneRetainsHistoryAndOtherOwnedFiles(t *testing.T) {
	_, p, r, j, _, c := fixtureInstall(t)
	unrelated := "owner-data/keep.txt"
	if writeNew(j.root, unrelated, []byte("KEEP_ORIGINAL")) != nil {
		t.Fatal("fixture write")
	}
	if r.Delete(context.Background(), p.Digest()) != nil {
		t.Fatal("original candidate delete failed")
	}
	if _, e := j.root.Stat(bundleRoot + "/environments/" + c.Digest); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("deleted environment still present", e)
	}
	file, e := j.root.Open(unrelated)
	if e != nil {
		t.Fatal("unrelated owner file removed", e)
	}
	file.Close()
	list, _, e := r.Snapshot()
	if e != nil || len(list) != 1 || list[0].Phase != "DELETED" || list[0].Stage != c.Stage {
		t.Fatal("original history discarded", e)
	}
	if r.Delete(context.Background(), p.Digest()) != nil {
		t.Fatal("exact delete replay failed")
	}
}
func TestComfyDeleteUncertainTombstoneNeverRemovesOriginalTree(t *testing.T) {
	_, p, r, j, _, c := fixtureInstall(t)
	j.fail = true
	if r.Delete(context.Background(), p.Digest()) == nil {
		t.Fatal("unconfirmed tombstone accepted")
	}
	if _, e := j.root.Stat(bundleRoot + "/environments/" + c.Digest); e != nil {
		t.Fatal("tree removed before durable tombstone", e)
	}
}
func TestComfyRegistryCloseCancelsAndJoinsEnteredOperation(t *testing.T) {
	f := newCatalogFixture(t, false)
	r, _, _ := fixtureRegistry(t, f)
	life, done, e := r.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	closed := make(chan struct{})
	go func() { r.Close(); close(closed) }()
	<-life.Done()
	select {
	case <-closed:
		t.Fatal("close released original key before owned operation joined")
	default:
	}
	done()
	<-closed
	if r.key != nil {
		t.Fatal("registry key survived actual join")
	}
	if _, _, e = r.Snapshot(); e == nil {
		t.Fatal("closed registry returned authority")
	}
}
func TestComfyRegistryRejectsAlteredAuthenticatedMetadata(t *testing.T) {
	_, _, r, j, _, _ := fixtureInstall(t)
	r.Close()
	j.sealed[len(j.sealed)-1] ^= 1
	if next, e := OpenOwned(j, bytes.Repeat([]byte{91}, 32), strings.Repeat("b", 64), false); e == nil {
		next.Close()
		t.Fatal("modified GCM accepted")
	}
}
func TestComfyMetadataIncludesExactConsentWithoutContentOrExecutionGrant(t *testing.T) {
	_, p, r, _, _, _ := fixtureInstall(t)
	candidates, selections, e := r.Snapshot()
	if e != nil || len(candidates) != 1 || len(selections) != 0 {
		t.Fatal(e)
	}
	b, e := json.Marshal(struct {
		Candidates []Candidate
		Selections []Selection
	}{candidates, selections})
	if e != nil {
		t.Fatal(e)
	}
	for _, forbidden := range []string{"SYNTHETIC_NO_CODE", "NOT_AN_EXECUTABLE", "Bearer", "QUALIFIED", "prompt"} {
		if bytes.Contains(b, []byte(forbidden)) {
			t.Fatal("content/authority escaped safe metadata")
		}
	}
	if candidates[0].CatalogDigest != p.CatalogDigest() {
		t.Fatal("original catalog identity changed")
	}
}
