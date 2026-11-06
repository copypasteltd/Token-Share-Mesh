package comfyenv

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const bundleRoot = "kernel-bundles"
const metadataDirectory = ".mesh"

func approvedSource(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && len(raw) <= 2048 && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && u.Path != ""
}
func plainFile(root *os.Root, name string, missing bool) (fs.FileInfo, error) {
	if root == nil || !portable(name) {
		return nil, ErrFiles
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		info, e := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(e, os.ErrNotExist) && missing && i == len(parts)-1 {
			return nil, nil
		}
		if e != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeIrregular != 0 {
			return nil, ErrFiles
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, ErrFiles
		}
		if i == len(parts)-1 {
			return info, nil
		}
	}
	return nil, ErrFiles
}
func createParents(root *os.Root, name string) error {
	parts := strings.Split(path.Dir(name), "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		if p == "." {
			continue
		}
		info, e := root.Lstat(p)
		if errors.Is(e, os.ErrNotExist) {
			if root.Mkdir(p, 0700) != nil {
				return ErrFiles
			}
			continue
		}
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeIrregular != 0 {
			return ErrFiles
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	return r.reader.Read(p)
}
func fileHash(ctx context.Context, f *os.File, expected File) error {
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != expected.Bytes {
		return ErrFiles
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return ErrFiles
	}
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	defer clear(buffer)
	n, e := io.CopyBuffer(h, contextReader{ctx, io.LimitReader(f, expected.Bytes+1)}, buffer)
	if e != nil || n != expected.Bytes || hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
		return ErrFiles
	}
	return nil
}
func verifyFile(ctx context.Context, root *os.Root, name string, expected File) error {
	info, e := plainFile(root, name, false)
	if e != nil || info == nil || !info.Mode().IsRegular() {
		return ErrFiles
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return ErrFiles
	}
	if runtime.GOOS == "linux" {
		mode := os.FileMode(0600)
		if expected.Executable {
			mode = 0700
		}
		if info.Mode().Perm() != mode {
			return ErrFiles
		}
	}
	f, e := root.Open(name)
	if e != nil {
		return ErrFiles
	}
	defer f.Close()
	return fileHash(ctx, f, expected)
}
func writeNew(root *os.Root, name string, raw []byte) error {
	if createParents(root, name) != nil {
		return ErrFiles
	}
	if _, e := plainFile(root, name, true); e != nil {
		return e
	}
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrFiles
	}
	n, e := f.Write(raw)
	if e == nil && n != len(raw) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	end := f.Close()
	if e != nil || end != nil {
		return ErrFiles
	}
	return nil
}
func readBounded(ctx context.Context, reader io.Reader, maximum int) ([]byte, error) {
	data, e := io.ReadAll(contextReader{ctx, io.LimitReader(reader, int64(maximum)+1)})
	if e != nil || len(data) > maximum {
		return nil, ErrFiles
	}
	return data, nil
}

// extractArtifact reads only an explicitly supplied, already downloaded ZIP.
// No shell, setup hook, wheel installer or network side effect is involved.
func extractArtifact(ctx context.Context, root *os.Root, destination, archive string, a Artifact) error {
	if !portable(archive) {
		return ErrFiles
	}
	info, e := plainFile(root, archive, false)
	if e != nil || info == nil || !info.Mode().IsRegular() {
		return ErrFiles
	}
	f, e := root.Open(archive)
	if e != nil {
		return ErrFiles
	}
	defer f.Close()
	if fileHash(ctx, f, File{Bytes: a.Bytes, SHA256: a.SHA256}) != nil {
		return ErrFiles
	}
	if archiveBounds(ctx, f, info.Size()) != nil {
		return ErrFiles
	}
	z, e := zip.NewReader(f, info.Size())
	if e != nil || len(z.File) > len(a.Files)*16+32 {
		return ErrFiles
	}
	expected := map[string]File{}
	directories := map[string]bool{}
	for _, file := range a.Files {
		expected[file.Path] = file
		for parent := path.Dir(file.Path); parent != "."; parent = path.Dir(parent) {
			directories[parent] = true
		}
	}
	seen := map[string]bool{}
	buffer := make([]byte, 64<<10)
	defer clear(buffer)
	for _, entry := range z.File {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := entry.Name
		directory := entry.FileInfo().IsDir()
		if directory {
			name = strings.TrimSuffix(name, "/")
		}
		if !filePath(name) || seen[strings.ToLower(name)] || entry.Mode()&os.ModeSymlink != 0 || entry.Mode()&os.ModeIrregular != 0 || entry.Flags&1 != 0 {
			return ErrFiles
		}
		seen[strings.ToLower(name)] = true
		if directory {
			if !directories[name] {
				return ErrFiles
			}
			continue
		}
		file, ok := expected[name]
		if !ok || !entry.Mode().IsRegular() || entry.UncompressedSize64 != uint64(file.Bytes) {
			return ErrFiles
		}
		output := destination + "/" + file.Path
		if createParents(root, output) != nil {
			return ErrFiles
		}
		mode := os.FileMode(0600)
		if file.Executable {
			mode = 0700
		}
		out, e := root.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if e != nil {
			return ErrFiles
		}
		in, e := entry.Open()
		if e != nil {
			out.Close()
			return ErrFiles
		}
		h := sha256.New()
		n, copyError := io.CopyBuffer(io.MultiWriter(out, h), contextReader{ctx, io.LimitReader(in, file.Bytes+1)}, buffer)
		readEnd := in.Close()
		if copyError == nil {
			copyError = out.Sync()
		}
		writeEnd := out.Close()
		if copyError != nil || readEnd != nil || writeEnd != nil || n != file.Bytes || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			return ErrFiles
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		return ErrFiles
	}
	return nil
}

func writeMetadata(root *os.Root, destination string, p Plan) error {
	if p.state == nil {
		return ErrCatalog
	}
	if createParents(root, destination+"/"+metadataDirectory+"/catalog.json") != nil {
		return ErrFiles
	}
	consent, e := json.Marshal(p.Consent())
	if e != nil {
		return ErrFiles
	}
	if writeNew(root, destination+"/"+metadataDirectory+"/catalog.json", p.Raw()) != nil || writeNew(root, destination+"/"+metadataDirectory+"/consent.json", consent) != nil {
		return ErrFiles
	}
	return nil
}

// verifyTree checks the complete enabled tree, including unknown directories,
// case aliases and private metadata, before any interpreter/node import.
func verifyTree(ctx context.Context, root *os.Root, directory string, p Plan) error {
	if p.state == nil || !p.ValidAt(nowUTC()) {
		return ErrCatalog
	}
	info, e := plainFile(root, directory, false)
	if e != nil || info == nil || !info.IsDir() {
		return ErrFiles
	}
	expected := map[string]File{}
	parents := map[string]bool{metadataDirectory: true}
	for name, f := range p.state.files {
		expected[name] = f
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			parents[parent] = true
		}
	}
	consent, _ := json.Marshal(p.Consent())
	metadata := map[string][]byte{metadataDirectory + "/catalog.json": p.Raw(), metadataDirectory + "/consent.json": consent}
	sub, e := fs.Sub(root.FS(), directory)
	if e != nil {
		return ErrFiles
	}
	e = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || ctx.Err() != nil {
			return ErrFiles
		}
		if name == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.Type()&os.ModeIrregular != 0 {
			return ErrFiles
		}
		if entry.IsDir() {
			if !parents[name] {
				return ErrFiles
			}
			return nil
		}
		if wanted, ok := metadata[name]; ok {
			if info, e := plainFile(root, directory+"/"+name, false); e != nil || info == nil || !info.Mode().IsRegular() {
				return ErrFiles
			}
			f, e := root.Open(directory + "/" + name)
			if e != nil {
				return ErrFiles
			}
			data, e := io.ReadAll(io.LimitReader(f, int64(len(wanted))+1))
			end := f.Close()
			if e != nil || end != nil || !bytes.Equal(data, wanted) {
				return ErrFiles
			}
			delete(metadata, name)
			return nil
		}
		wanted, ok := expected[name]
		if !ok {
			return ErrFiles
		}
		if verifyFile(ctx, root, directory+"/"+name, wanted) != nil {
			return ErrFiles
		}
		delete(expected, name)
		return nil
	})
	if e != nil || len(expected) != 0 || len(metadata) != 0 {
		return ErrFiles
	}
	return nil
}

// removeOwnedTree never follows an installation symlink or a computed path
// outside the caller-owned root. It is used only after a durable tombstone.
func removeOwnedTree(root *os.Root, name string) error {
	parts := strings.Split(name, "/")
	if len(parts) != 3 || parts[0] != bundleRoot || (parts[1] != "stages" && parts[1] != "environments") || !hash(parts[2]) {
		return ErrFiles
	}
	base, e := filepath.Abs(root.Name())
	if e != nil {
		return ErrFiles
	}
	target := filepath.Clean(filepath.Join(base, filepath.FromSlash(name)))
	relative, e := filepath.Rel(base, target)
	if e != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrFiles
	}
	info, e := plainFile(root, name, true)
	if e != nil {
		return e
	}
	if info == nil {
		return nil
	}
	if !info.IsDir() {
		return ErrFiles
	}
	sub, e := fs.Sub(root.FS(), name)
	if e != nil {
		return ErrFiles
	}
	if fs.WalkDir(sub, ".", func(_ string, d fs.DirEntry, e error) error {
		if e != nil || d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return ErrFiles
		}
		return nil
	}) != nil {
		return ErrFiles
	}
	return root.RemoveAll(name)
}
