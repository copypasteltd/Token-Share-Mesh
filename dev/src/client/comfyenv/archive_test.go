package comfyenv

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"testing"
)

func TestComfyArchiveRejectsSymlinkBeforeFollowingItsTarget(t *testing.T) {
	f := newCatalogFixture(t, false)
	artifact := &f.c.Artifacts[0]
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	for _, file := range artifact.Files {
		h := &zip.FileHeader{Name: file.Path, Method: zip.Store}
		data := []byte("synthetic license")
		if file.Path == f.c.Python {
			h.SetMode(os.ModeSymlink | 0700)
			data = []byte("../../UNRELATED_OWNER_FILE")
			file.SHA256 = sum(data)
			file.Bytes = int64(len(data))
			for i := range artifact.Files {
				if artifact.Files[i].Path == file.Path {
					artifact.Files[i] = file
				}
			}
		} else {
			h.SetMode(0600)
		}
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if z.Close() != nil {
		t.Fatal("synthetic archive close")
	}
	archive := bytes.Clone(buffer.Bytes())
	artifact.SHA256 = sum(archive)
	artifact.Bytes = int64(len(archive))
	f.archives[artifact.ID] = archive
	p := f.plan(t)
	r, j, archives := fixtureRegistry(t, f)
	if _, e := r.Install(context.Background(), p, archives); e == nil {
		t.Fatal("ZIP symlink installed")
	}
	if _, e := j.root.Lstat("UNRELATED_OWNER_FILE"); !os.IsNotExist(e) {
		t.Fatal("archive followed or created unrelated target", e)
	}
}
func TestComfyInstallCancelledAdmissionCreatesNoCandidateOrStage(t *testing.T) {
	f := newCatalogFixture(t, false)
	p := f.plan(t)
	r, _, archives := fixtureRegistry(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := r.Install(ctx, p, archives); e == nil {
		t.Fatal("cancelled install admitted")
	}
	list, _, e := r.Snapshot()
	if e != nil || len(list) != 0 {
		t.Fatal("cancelled admission created original candidate", e)
	}
	if _, e = r.Install(nil, p, archives); e == nil {
		t.Fatal("nil install context accepted")
	}
}
