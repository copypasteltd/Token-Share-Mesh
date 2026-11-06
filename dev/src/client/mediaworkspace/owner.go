// Package mediaworkspace owns original rental-media directories. Its methods
// are trusted native-host calls, never public HTTP or filesystem capabilities.
package mediaworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

const Directory = "media-workspaces"

var ErrWorkspace = errors.New("original media workspace unavailable or unconfirmed")
var basename = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)

// Protect and Sync are the ORIGINAL node's native permission/durability
// operations. They receive fixed relative names beneath the original root.
// Root remains owned by node; Owner never closes or replaces it.
type Config struct {
	Root    *os.Root
	Ledger  *execution.Store
	Protect func(string, bool) error
	Sync    func(string) error
}
type Owner struct {
	mu                  sync.Mutex
	config              Config
	root                *os.Root
	namespace           execution.MediaNamespace
	closing             bool
	workspaces          map[string]*Workspace
	operations, handles sync.WaitGroup
	once                sync.Once
	done                chan struct{}
}
type Workspace struct{ state *workspaceState }
type workspaceState struct {
	mu                                         sync.Mutex
	owner                                      *Owner
	hold                                       execution.Hold
	record                                     execution.MediaWorkspace
	expected                                   rentalaccess.Expected
	root                                       *os.Root
	journalKey                                 []byte
	claimed, launchTaken, closing, cleanupOnly bool
	joined                                     <-chan struct{}
	readers                                    map[*ReadLease]struct{}
	once                                       sync.Once
	done                                       chan struct{}
}

// Launch is ephemeral private pipe material. It is never a network DTO.
type Launch struct {
	Path, AnchorHash string
	JournalKey       []byte
}

func (l Launch) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private media workspace launch redacted]")
}
func (l Launch) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (w Workspace) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[original media workspace redacted]")
}
func (w Workspace) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		clear(b)
		return nil, ErrWorkspace
	}
	return b, nil
}
func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func plain(root *os.Root, name string, directory bool) error {
	info, e := root.Lstat(name)
	if e != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || info.IsDir() != directory || !directory && !info.Mode().IsRegular() {
		return ErrWorkspace
	}
	return nil
}
func verifyAnchor(root *os.Root, name, want string) error {
	if plain(root, name, false) != nil {
		return ErrWorkspace
	}
	f, e := root.Open(name)
	if e != nil {
		return ErrWorkspace
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != 32 {
		return ErrWorkspace
	}
	b := make([]byte, 33)
	defer clear(b)
	n, e := io.ReadFull(f, b)
	if n != 32 || e != io.ErrUnexpectedEOF || sum(b[:32]) != want {
		return ErrWorkspace
	}
	return nil
}
func createAnchor(root *os.Root, name string, raw []byte) error {
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrWorkspace
	}
	n, e := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if e != nil || n != len(raw) || syncErr != nil || closeErr != nil {
		return ErrWorkspace
	}
	return nil
}

// OpenOwned(create=true) is an explicit initialization before node loops.
// Any interrupted creation remains anchored/QUARANTINED, never adopted/reset.
func OpenOwned(ctx context.Context, c Config, create bool) (*Owner, error) {
	if ctx == nil || ctx.Err() != nil || c.Root == nil || c.Ledger == nil || c.Protect == nil || c.Sync == nil {
		return nil, ErrWorkspace
	}
	n, e := c.Ledger.MediaNamespace(ctx)
	if create {
		if !errors.Is(e, execution.ErrQuery) {
			return nil, ErrWorkspace
		}
		if _, e = c.Root.Lstat(Directory); !errors.Is(e, os.ErrNotExist) {
			return nil, ErrWorkspace
		}
		id, e := randomBytes(32)
		if e != nil {
			return nil, e
		}
		defer clear(id)
		anchor, e := randomBytes(32)
		if e != nil {
			return nil, e
		}
		defer clear(anchor)
		ticket, e := c.Ledger.BeginMediaNamespace(ctx, hex.EncodeToString(id), sum(anchor))
		if e != nil {
			return nil, ErrWorkspace
		}
		n, e = ticket.Take()
		if e != nil {
			return nil, ErrWorkspace
		}
		if c.Root.Mkdir(Directory, 0700) != nil || c.Protect(Directory, true) != nil {
			return nil, ErrWorkspace
		}
		if createAnchor(c.Root, Directory+"/namespace.anchor", anchor) != nil || c.Protect(Directory+"/namespace.anchor", false) != nil || c.Sync(Directory) != nil || c.Sync(".") != nil {
			return nil, ErrWorkspace
		}
		if c.Ledger.ConfirmMediaNamespace(ctx, n) != nil {
			return nil, ErrWorkspace
		}
		n.Phase = "READY"
	} else if e != nil || n.Phase != "READY" {
		return nil, ErrWorkspace
	}
	if plain(c.Root, Directory, true) != nil || verifyAnchor(c.Root, Directory+"/namespace.anchor", n.AnchorHash) != nil {
		return nil, ErrWorkspace
	}
	r, e := c.Root.OpenRoot(Directory)
	if e != nil {
		return nil, ErrWorkspace
	}
	return &Owner{config: c, root: r, namespace: n, workspaces: map[string]*Workspace{}, done: make(chan struct{})}, nil
}
func (o *Owner) enter() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing {
		return false
	}
	o.operations.Add(1)
	return true
}

// Create is invoked after original BeginRentalInvocation, before native entry.
// Replay never recreates/adopts a path, even when the expected directory is gone.
func (o *Owner) Create(ctx context.Context, h execution.Hold, id string, current rentalaccess.Expected) (Workspace, error) {
	if ctx == nil || ctx.Err() != nil || !o.enter() {
		return Workspace{}, ErrWorkspace
	}
	defer o.operations.Done()
	name, e := randomBytes(16)
	if e != nil {
		return Workspace{}, e
	}
	defer clear(name)
	anchor, e := randomBytes(32)
	if e != nil {
		return Workspace{}, e
	}
	defer clear(anchor)
	key, e := randomBytes(32)
	if e != nil {
		return Workspace{}, e
	}
	defer clear(key)
	ticket, w, disposition, e := o.config.Ledger.ReserveRentalMediaWorkspace(ctx, h, o.namespace.ID, id, current, "ws_"+hex.EncodeToString(name), sum(anchor), execution.MaximumMediaWorkspaceBytes)
	if e != nil || disposition != "WORKSPACE_RESERVED" || ticket == nil {
		return Workspace{}, ErrWorkspace
	}
	w, e = ticket.Take()
	if e != nil {
		return Workspace{}, ErrWorkspace
	}
	failed := true
	defer func() {
		if failed {
			stop, cancel := context.WithTimeout(context.Background(), metadataTimeout)
			defer cancel()
			_ = o.config.Ledger.QuarantineRentalMediaWorkspace(stop, h, id)
		}
	}()
	if o.root.Mkdir(w.Basename, 0700) != nil || o.config.Protect(Directory+"/"+w.Basename, true) != nil {
		return Workspace{}, ErrWorkspace
	}
	if createAnchor(o.root, w.Basename+"/ownership.anchor", anchor) != nil || o.config.Protect(Directory+"/"+w.Basename+"/ownership.anchor", false) != nil || o.config.Sync(Directory+"/"+w.Basename) != nil || o.config.Sync(Directory) != nil {
		return Workspace{}, ErrWorkspace
	}
	if o.config.Ledger.ConfirmRentalMediaWorkspace(ctx, h, w) != nil {
		return Workspace{}, ErrWorkspace
	}
	w.Phase = "READY"
	if plain(o.root, w.Basename, true) != nil || verifyAnchor(o.root, w.Basename+"/ownership.anchor", w.AnchorHash) != nil {
		return Workspace{}, ErrWorkspace
	}
	r, e := o.root.OpenRoot(w.Basename)
	if e != nil {
		return Workspace{}, ErrWorkspace
	}
	s := &workspaceState{owner: o, hold: h, record: w, expected: current, root: r, journalKey: append([]byte(nil), key...), readers: map[*ReadLease]struct{}{}, done: make(chan struct{})}
	result := Workspace{state: s}
	o.mu.Lock()
	if o.closing || o.workspaces[id] != nil {
		o.mu.Unlock()
		r.Close()
		clear(s.journalKey)
		return Workspace{}, ErrWorkspace
	}
	o.handles.Add(1)
	o.workspaces[id] = &result
	o.mu.Unlock()
	failed = false
	return result, nil
}
func (w Workspace) Record() execution.MediaWorkspace {
	if w.state == nil {
		return execution.MediaWorkspace{}
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.state.record
}

// joined must represent ACTUAL original invocation work/I/O/key-transfer join.
// Closing HTTP, Cancel acknowledgement or a caller timeout is not that event.
func (w Workspace) Claim(ctx context.Context, current rentalaccess.Expected, joined <-chan struct{}) error {
	if w.state == nil || ctx == nil || joined == nil {
		return ErrWorkspace
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.cleanupOnly || s.claimed || s.expected != current {
		return ErrWorkspace
	}
	record, e := s.owner.config.Ledger.ClaimRentalMediaWorkspace(ctx, s.hold, s.record.Invocation, current)
	if e != nil {
		return ErrWorkspace
	}
	s.record = record
	s.claimed = true
	s.joined = joined
	return nil
}
func (w Workspace) WithLaunch(ctx context.Context, action func(Launch) error) error {
	if w.state == nil || ctx == nil || action == nil {
		return ErrWorkspace
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.cleanupOnly || !s.claimed || s.launchTaken || len(s.journalKey) != 32 {
		return ErrWorkspace
	}
	if _, e := s.owner.config.Ledger.CheckRentalExpected(ctx, s.hold, s.expected, "INVOKE"); e != nil {
		return ErrWorkspace
	}
	if verifyAnchor(s.root, "ownership.anchor", s.record.AnchorHash) != nil {
		return ErrWorkspace
	}
	s.launchTaken = true
	key := append([]byte(nil), s.journalKey...)
	defer clear(key)
	return action(Launch{Path: s.root.Name(), AnchorHash: s.record.AnchorHash, JournalKey: key})
}

// ReadLease pins the ORIGINAL workspace through the actual cipher reader.
// Open must finish before any returned reader is exposed to a caller.
type ReadLease struct {
	mu            sync.Mutex
	workspace     *workspaceState
	closer        io.Closer
	bound, closed bool
	done          chan struct{}
	closeError    error
}

func (w Workspace) AcquireRead(ctx context.Context) (*ReadLease, error) {
	if w.state == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrWorkspace
	}
	s := w.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.cleanupOnly || !s.claimed {
		return nil, ErrWorkspace
	}
	if _, e := s.owner.config.Ledger.CheckRentalExpected(ctx, s.hold, s.expected, "EXPORT_SELF"); e != nil {
		return nil, ErrWorkspace
	}
	l := &ReadLease{workspace: s, done: make(chan struct{})}
	s.readers[l] = struct{}{}
	return l, nil
}

// Open pins initialization as well as the returned reader. Close cannot close
// the root during an unbound OpenCipher; it waits for this callback to return.
func (l *ReadLease) Open(action func(*os.Root) (io.Closer, error)) error {
	if l == nil || action == nil {
		return ErrWorkspace
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.bound {
		return ErrWorkspace
	}
	l.bound = true
	reader, e := action(l.workspace.root)
	if e != nil || reader == nil {
		if reader != nil {
			_ = reader.Close()
		}
		return ErrWorkspace
	}
	l.closer = reader
	return nil
}
func (l *ReadLease) Close() error {
	if l == nil {
		return nil
	}
	if l.workspace == nil || l.done == nil {
		return ErrWorkspace
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		<-l.done
		l.mu.Lock()
		e := l.closeError
		l.mu.Unlock()
		return e
	}
	l.closed = true
	c := l.closer
	l.closer = nil
	l.mu.Unlock()
	var e error
	if c != nil {
		e = c.Close()
	}
	s := l.workspace
	s.mu.Lock()
	delete(s.readers, l)
	s.mu.Unlock()
	l.mu.Lock()
	l.closeError = e
	close(l.done)
	l.mu.Unlock()
	return e
}

// Close revokes future API entry first. Claimed work retains its root/key and
// original node OS lock until actual joined, even if an external wait expires.
func (w Workspace) Close() {
	if w.state == nil {
		return
	}
	s := w.state
	s.once.Do(func() {
		s.mu.Lock()
		s.closing = true
		joined := s.joined
		readers := make([]*ReadLease, 0, len(s.readers))
		for r := range s.readers {
			readers = append(readers, r)
		}
		s.mu.Unlock()
		for _, r := range readers {
			_ = r.Close()
		}
		if joined != nil {
			<-joined
		}
		s.mu.Lock()
		clear(s.journalKey)
		s.journalKey = nil
		_ = s.root.Close()
		s.mu.Unlock()
		s.owner.handles.Done()
		close(s.done)
	})
	<-s.done
}

// RetireAndRemove does not mark the invocation CLEANED or release its parent.
// It requires existing CLEANING and independently supplied original stop proof.
func (w Workspace) RetireAndRemove(ctx context.Context, c execution.RentalChildCleanup) error {
	if w.state == nil || ctx == nil || ctx.Err() != nil {
		return ErrWorkspace
	}
	s := w.state
	o := s.owner
	if !o.enter() {
		return ErrWorkspace
	}
	defer o.operations.Done()
	s.mu.Lock()
	joined := s.joined
	claimed := s.claimed
	s.mu.Unlock()
	if claimed {
		select {
		case <-joined:
		default:
			return ErrWorkspace
		}
	}
	w.Close()
	if o.config.Ledger.RetireRentalMediaWorkspace(ctx, s.hold, s.record.Invocation, c) != nil {
		return ErrWorkspace
	}
	ticket, e := o.config.Ledger.BeginRentalMediaRemoval(ctx, s.hold, s.record.Invocation)
	if e != nil {
		return ErrWorkspace
	}
	record, e := ticket.Take()
	if e != nil {
		return ErrWorkspace
	}
	if removeOriginal(o.root, record) != nil || o.config.Sync(Directory) != nil {
		_ = o.config.Ledger.QuarantineRentalMediaWorkspace(ctx, s.hold, record.Invocation)
		return ErrWorkspace
	}
	if o.config.Ledger.FinishRentalMediaRemoval(ctx, s.hold, record) != nil {
		return ErrWorkspace
	}
	s.mu.Lock()
	s.record = record
	s.record.Phase = "CLEANED"
	s.mu.Unlock()
	return nil
}
func removeOriginal(root *os.Root, w execution.MediaWorkspace) error {
	if root == nil || w.Phase != "REMOVING" || w.CleanupHash == "" || !basename.MatchString(w.Basename) {
		return ErrWorkspace
	}
	// Verify the final resolved absolute target BEFORE any recursive deletion.
	base, e := filepath.Abs(root.Name())
	if e != nil {
		return ErrWorkspace
	}
	target := filepath.Clean(filepath.Join(base, w.Basename))
	rel, e := filepath.Rel(base, target)
	if e != nil || rel != w.Basename || rel == "." || rel == ".." || strings.ContainsAny(rel, "/\\") {
		return ErrWorkspace
	}
	if plain(root, w.Basename, true) != nil || verifyAnchor(root, w.Basename+"/ownership.anchor", w.AnchorHash) != nil {
		return ErrWorkspace
	}
	r, e := root.OpenRoot(w.Basename)
	if e != nil {
		return ErrWorkspace
	}
	files, e := r.Open(".")
	if e != nil {
		r.Close()
		return ErrWorkspace
	}
	entries, e := files.ReadDir(17)
	files.Close()
	// Root contains one anchor and at most five cipher/reservation pairs.
	if e != nil && e != io.EOF || len(entries) > 11 {
		r.Close()
		return ErrWorkspace
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "ownership.anchor" && !ownedCipher.MatchString(name) {
			r.Close()
			return ErrWorkspace
		}
		if plain(r, name, false) != nil {
			r.Close()
			return ErrWorkspace
		}
	}
	r.Close()
	// os.Root.RemoveAll remains beneath this same owned namespace. No adoption,
	// global cleanup, symlink traversal or second root is allowed.
	if root.RemoveAll(w.Basename) != nil {
		return ErrWorkspace
	}
	return nil
}

var ownedCipher = regexp.MustCompile(`^cipher-[a-f0-9]{32}\.bin(?:\.reservation)?$`)

// OpenOriginalCleanup is explicit recovery AFTER independent trusted physical
// stop reconciliation has moved the original invocation to CLEANING. It can
// neither restore an input/result key nor run/read content. A missing anchor
// or an interrupted/unidentified directory cannot be adopted for deletion.
func (o *Owner) OpenOriginalCleanup(ctx context.Context, h execution.Hold, id string) (Workspace, error) {
	if ctx == nil || ctx.Err() != nil || !o.enter() {
		return Workspace{}, ErrWorkspace
	}
	defer o.operations.Done()
	v, e := o.config.Ledger.RentalInvocation(ctx, h, id)
	if e != nil || v.Phase != "CLEANING" {
		return Workspace{}, ErrWorkspace
	}
	w, e := o.config.Ledger.RentalMediaWorkspace(ctx, h, id)
	if e != nil || w.Namespace != o.namespace.ID || (w.Phase != "QUARANTINED" && w.Phase != "RETIRED") {
		return Workspace{}, ErrWorkspace
	}
	if !basename.MatchString(w.Basename) || plain(o.root, w.Basename, true) != nil || verifyAnchor(o.root, w.Basename+"/ownership.anchor", w.AnchorHash) != nil {
		return Workspace{}, ErrWorkspace
	}
	root, e := o.root.OpenRoot(w.Basename)
	if e != nil {
		return Workspace{}, ErrWorkspace
	}
	joined := make(chan struct{})
	close(joined)
	state := &workspaceState{owner: o, hold: h, record: w, root: root, cleanupOnly: true, claimed: true, joined: joined, readers: map[*ReadLease]struct{}{}, done: make(chan struct{})}
	result := Workspace{state: state}
	o.mu.Lock()
	if o.closing || o.workspaces[id] != nil {
		o.mu.Unlock()
		root.Close()
		return Workspace{}, ErrWorkspace
	}
	o.handles.Add(1)
	o.workspaces[id] = &result
	o.mu.Unlock()
	return result, nil
}
func (o *Owner) Close() {
	if o == nil || o.done == nil {
		return
	}
	o.once.Do(func() {
		o.mu.Lock()
		o.closing = true
		list := make([]Workspace, 0, len(o.workspaces))
		for _, w := range o.workspaces {
			list = append(list, *w)
		}
		o.mu.Unlock()
		for _, w := range list {
			w.Close()
		}
		o.operations.Wait()
		o.handles.Wait()
		o.root.Close()
		close(o.done)
	})
	<-o.done
}
