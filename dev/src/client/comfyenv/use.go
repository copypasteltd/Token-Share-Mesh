package comfyenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
)

// PreparationGate is the original physical ledger, never a network approval.
type PreparationGate interface {
	WithRentalPreparation(context.Context, execution.Hold, rentalaccess.Prepared, func() error) (time.Time, error)
	RentalCurrent(context.Context, execution.Hold) (execution.RentalStatus, error)
	CheckRental(context.Context, execution.Hold, string) error
	CheckRentalExpected(context.Context, execution.Hold, rentalaccess.Expected, string) (time.Time, error)
}

// RuntimeFiles includes ONLY enabled files and the registry's exact original
// metadata. It returns copies and creates no execution or filesystem rights.
func (p Plan) RuntimeFiles() []File {
	if p.state == nil {
		return nil
	}
	files := make([]File, 0, len(p.state.files)+2)
	for _, file := range p.state.files {
		files = append(files, file)
	}
	consent, e := json.Marshal(p.Consent())
	if e != nil {
		return nil
	}
	for name, raw := range map[string][]byte{metadataDirectory + "/catalog.json": p.state.raw, metadataDirectory + "/consent.json": consent} {
		files = append(files, File{Path: name, SHA256: sum(raw), Bytes: int64(len(raw))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

// Layout is private host preparation metadata, not a public status response.
// Node packs are only those independently selected in the original Consent.
type Layout struct {
	Root, Python, Bridge, Commit, Environment string
	Files                                     []File
	CoreClasses                               []string
	NodePacks                                 []NodePack
}

func (l Layout) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private ComfyUI runtime layout redacted]")
}
func (l Layout) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

type useState struct {
	mu                sync.Mutex
	registry          *Registry
	root              *os.Root
	plan              Plan
	environment       string
	hold              execution.Hold
	prepared          rentalaccess.Prepared
	gate              PreparationGate
	life              context.Context
	cancel            context.CancelFunc
	prepareUntil      time.Time
	timer             *time.Timer
	claimed, released bool
	finish            sync.Once
}

// Use is an opaque shared reference. Copies share cancellation/claim/release.
// A claimed reference cannot be released by Close before actual native join.
type Use struct{ state *useState }

func (u Use) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[owned ComfyUI environment reference redacted]")
}
func (u Use) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// OpenRentalUse performs full validation before imports, then registers under
// the SAME original physical preparation gate. It never creates a new hold,
// accepts an unselected environment, restores an ALLOW or starts a process.
func (r *Registry) OpenRentalUse(ctx context.Context, gate PreparationGate, h execution.Hold, prepared rentalaccess.Prepared, p Plan) (Use, error) {
	m := prepared.Details()
	if r == nil || gate == nil || !p.ValidAt(nowUTC()) || !prepared.ValidAt(nowUTC()) || m.Engine != "COMFYUI" || m.EnvironmentHash != p.Digest() || m.Resource != h.Resource() || m.Session != h.Owner() || p.state.catalog.OS != runtime.GOOS || p.state.catalog.Architecture != runtime.GOARCH {
		return Use{}, ErrCatalog
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return Use{}, e
	}
	defer done()
	r.mu.Lock()
	at := r.index(p.Digest())
	selected := false
	for _, selection := range r.s.Active {
		if selection.Resource == h.Resource() && selection.Current == p.Digest() {
			selected = true
		}
	}
	valid := at >= 0 && r.s.Candidates[at].Phase == "VERIFIED" && r.s.Candidates[at].CatalogDigest == p.CatalogDigest() && selected && r.uses[h.Resource()] == nil
	r.mu.Unlock()
	if !valid {
		return Use{}, ErrState
	}
	directory := bundleRoot + "/environments/" + p.Digest()
	if verifyTree(life, r.journal.Root(), directory, p) != nil {
		r.mark(at, "QUARANTINED", "RUNTIME_DIRECTORY_DRIFT")
		return Use{}, ErrFiles
	}
	root, e := r.journal.Root().OpenRoot(directory)
	if e != nil {
		return Use{}, ErrFiles
	}
	if !filepath.IsAbs(root.Name()) || filepath.Clean(root.Name()) != root.Name() {
		root.Close()
		return Use{}, ErrFiles
	}
	owned, cancel := context.WithDeadline(context.Background(), p.state.until)
	state := &useState{registry: r, root: root, plan: p, environment: p.Digest(), hold: h, prepared: prepared, gate: gate, life: owned, cancel: cancel}
	registered := false
	deadline, e := gate.WithRentalPreparation(life, h, prepared, func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closing || r.poisoned || life.Err() != nil || r.uses[h.Resource()] != nil || !p.ValidAt(nowUTC()) {
			return ErrState
		}
		if r.uses == nil {
			r.uses = map[string]*useState{}
		}
		r.uses[h.Resource()] = state
		r.operations.Add(1)
		registered = true
		return nil
	})
	if e != nil {
		if registered {
			state.release()
		} else {
			cancel()
			root.Close()
		}
		return Use{}, ErrState
	}
	state.mu.Lock()
	if state.released || state.life.Err() != nil || life.Err() != nil || deadline.IsZero() || !time.Now().Before(deadline) {
		state.mu.Unlock()
		state.release()
		return Use{}, ErrState
	}
	state.prepareUntil = deadline
	state.timer = time.AfterFunc(max(0, time.Until(deadline)), func() {
		state.mu.Lock()
		unused := !state.claimed
		state.mu.Unlock()
		if unused {
			(Use{state}).Close()
		}
	})
	state.mu.Unlock()
	return Use{state}, nil
}
func (s *useState) release() {
	s.finish.Do(func() {
		s.mu.Lock()
		s.released = true
		s.plan = Plan{}
		s.cancel()
		if s.timer != nil {
			s.timer.Stop()
		}
		root := s.root
		s.root = nil
		s.mu.Unlock()
		if root != nil {
			root.Close()
		}
		s.registry.mu.Lock()
		if s.registry.uses[s.hold.Resource()] == s {
			delete(s.registry.uses, s.hold.Resource())
		}
		s.registry.mu.Unlock()
		s.registry.operations.Done()
	})
}
func (u Use) Close() {
	if u.state == nil {
		return
	}
	s := u.state
	s.mu.Lock()
	s.cancel()
	claimed := s.claimed
	s.mu.Unlock()
	if !claimed {
		s.release()
	}
}
func (u Use) Context() context.Context {
	if u.state == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return u.state.life
}
func (u Use) Original() (execution.Hold, rentalaccess.Prepared) {
	if u.state == nil {
		return execution.Hold{}, rentalaccess.Prepared{}
	}
	return u.state.hold, u.state.prepared
}
func (u Use) PreparationDeadline() time.Time {
	if u.state == nil {
		return time.Time{}
	}
	u.state.mu.Lock()
	defer u.state.mu.Unlock()
	return u.state.prepareUntil
}

// Current and CheckScope always use the same original ledger which admitted
// this reference, never a caller-supplied alternative DB/control interface.
func (u Use) Current(ctx context.Context) (execution.RentalStatus, error) {
	if ctx == nil || ctx.Err() != nil || u.state == nil || u.Context().Err() != nil {
		return execution.RentalStatus{}, ErrState
	}
	s := u.state
	status, e := s.gate.RentalCurrent(ctx, s.hold)
	if e != nil || status.Hold != s.hold || status.ManifestHash != s.prepared.Digest() || status.ApprovalHash != s.prepared.ApprovalDigest() {
		return execution.RentalStatus{}, ErrState
	}
	return status, nil
}
func (u Use) CheckScope(ctx context.Context, scope string) error {
	if ctx == nil || ctx.Err() != nil || u.state == nil || u.Context().Err() != nil {
		return ErrState
	}
	return u.state.gate.CheckRental(ctx, u.state.hold, scope)
}
func (u Use) CheckExpected(ctx context.Context, expected rentalaccess.Expected, scope string) (time.Time, error) {
	if ctx == nil || ctx.Err() != nil || u.state == nil || u.Context().Err() != nil {
		return time.Time{}, ErrState
	}
	return u.state.gate.CheckRentalExpected(ctx, u.state.hold, expected, scope)
}

// Claim transfers lifetime to an actual process owner's completion channel.
// The channel must close only AFTER process wait, pipe readers/writers and all
// entered host operations join, including start failures. Cancellation asks
// that owner to stop, but neither caller Close nor a timeout releases it.
// This trusted-host API is not an adversarial proof of OS process completion.
func (u Use) Claim(joined <-chan struct{}) error {
	if u.state == nil || joined == nil {
		return ErrState
	}
	s := u.state
	s.mu.Lock()
	if s.released || s.claimed || s.life.Err() != nil || !time.Now().Before(s.prepareUntil) {
		s.mu.Unlock()
		return ErrState
	}
	select {
	case <-joined:
		s.mu.Unlock()
		return ErrState
	default:
	}
	s.claimed = true
	// The host already built its exact INIT from Layout. Do not retain the
	// large preparation file/catalog inventory throughout an hourly session.
	s.plan = Plan{}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	go func() { <-joined; s.release() }()
	return nil
}
func (u Use) Layout() (Layout, error) {
	if u.state == nil {
		return Layout{}, ErrState
	}
	s := u.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released || s.claimed || s.life.Err() != nil || s.root == nil {
		return Layout{}, ErrState
	}
	c := s.plan.Catalog()
	selected := map[string]bool{}
	for _, id := range s.plan.Consent().SelectedNodePacks {
		selected[id] = true
	}
	l := Layout{Root: s.root.Name(), Python: c.Python, Bridge: c.Bridge, Commit: c.ComfyRevision, Environment: s.plan.Digest(), Files: s.plan.RuntimeFiles(), CoreClasses: append([]string{}, c.CoreClasses...), NodePacks: []NodePack{}}
	for _, pack := range c.NodePacks {
		if selected[pack.ID] {
			l.NodePacks = append(l.NodePacks, pack)
		}
	}
	return l, nil
}
