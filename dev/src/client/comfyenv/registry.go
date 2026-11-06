package comfyenv

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sync"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
)

const RegistryProfile = "P11_NATIVE_COMFY_REGISTRY_1"
const MaximumRegistryBytes = 256 << 10

// Journal is implemented only by the already locked enclosing node. Commit
// must atomically replace and flush the exact original encrypted journal.
// Root must remain owned until Registry.Close has joined every operation.
type Journal interface {
	Root() *os.Root
	Read() ([]byte, error)
	Commit([]byte) error
}
type Maintenance interface {
	CheckMaintenance(context.Context, execution.Hold) error
	WithMaintenance(context.Context, execution.Hold, func() error) error
}
type Candidate struct {
	Digest            string `json:"environment_digest"`
	CatalogDigest     string `json:"catalog_digest"`
	AllowlistRevision string `json:"allowlist_revision"`
	Stage             string `json:"original_stage_id"`
	Phase             string `json:"phase"`
	Reason            string `json:"reason_code"`
}
type Selection struct {
	Resource        string `json:"resource_id"`
	LocalGeneration string `json:"local_generation"`
	Owner           string `json:"maintenance_owner"`
	Previous        string `json:"previous_digest"`
	Current         string `json:"environment_digest"`
}
type registryState struct {
	Profile    string      `json:"profile_id"`
	Binding    string      `json:"binding_digest"`
	Candidates []Candidate `json:"candidates"`
	Active     []Selection `json:"local_selections"`
}
type Registry struct {
	mu                sync.Mutex
	journal           Journal
	key               []byte
	s                 registryState
	closing, poisoned bool
	active            bool
	cancel            context.CancelFunc
	operations        sync.WaitGroup
	uses              map[string]*useState
}

func nowUTC() time.Time { return time.Now().UTC() }

// OpenOwned does not acquire the parent's OS lock. The node must anchor the
// new registry ID before create=true. Missing/corrupt anchored state fails;
// reopening an interrupted installation quarantines it, never adopts a tree.
func OpenOwned(j Journal, key []byte, binding string, create bool) (*Registry, error) {
	if j == nil || j.Root() == nil || len(key) != 32 || !hash(binding) {
		return nil, ErrState
	}
	r := &Registry{journal: j, key: append([]byte(nil), key...), s: registryState{Profile: RegistryProfile, Binding: binding, Candidates: []Candidate{}, Active: []Selection{}}}
	failed := true
	defer func() {
		if failed {
			clear(r.key)
		}
	}()
	if create {
		if _, e := j.Read(); e == nil || !isMissing(e) {
			return nil, ErrState
		}
		if r.persistLocked() != nil {
			return nil, ErrState
		}
		for _, directory := range []string{bundleRoot, bundleRoot + "/stages", bundleRoot + "/environments"} {
			if j.Root().Mkdir(directory, 0700) != nil {
				return nil, ErrState
			}
		}
	} else {
		sealed, e := j.Read()
		if e != nil || len(sealed) > MaximumRegistryBytes {
			return nil, ErrState
		}
		aead, e := aead(r.key)
		if e != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
			return nil, ErrState
		}
		plain, e := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(RegistryProfile+"\n"+binding))
		defer clear(plain)
		if e != nil || strictjson.Decode(plain, &r.s, MaximumRegistryBytes) != nil || r.validate(binding) != nil {
			return nil, ErrState
		}
		for _, directory := range []string{bundleRoot, bundleRoot + "/stages", bundleRoot + "/environments"} {
			info, e := plainFile(j.Root(), directory, false)
			if e != nil || info == nil || !info.IsDir() {
				return nil, ErrState
			}
		}
		changed := false
		for i := range r.s.Candidates {
			c := &r.s.Candidates[i]
			if c.Phase == "INSTALLING" || c.Phase == "COMMITTING" {
				c.Phase = "QUARANTINED"
				c.Reason = "INTERRUPTED_OR_UNCONFIRMED_INSTALL"
				changed = true
			}
		}
		if changed && r.persistLocked() != nil {
			return nil, ErrState
		}
	}
	failed = false
	return r, nil
}
func aead(key []byte) (cipher.AEAD, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (r *Registry) validate(binding string) error {
	if r.s.Profile != RegistryProfile || r.s.Binding != binding || r.s.Candidates == nil || r.s.Active == nil || len(r.s.Candidates) > MaximumCandidates || len(r.s.Active) > execution.MaximumResources {
		return ErrState
	}
	digests := map[string]bool{}
	stages := map[string]bool{}
	for _, c := range r.s.Candidates {
		if !hash(c.Digest) || !hash(c.CatalogDigest) || !hash(c.Stage) || !id(c.AllowlistRevision) || digests[c.Digest] || stages[c.Stage] {
			return ErrState
		}
		switch c.Phase {
		case "INSTALLING", "COMMITTING", "VERIFIED", "QUARANTINED", "TOMBSTONED", "DELETED":
		default:
			return ErrState
		}
		if len(c.Reason) > 128 {
			return ErrState
		}
		digests[c.Digest] = true
		stages[c.Stage] = true
	}
	resources := map[string]bool{}
	for _, s := range r.s.Active {
		if _, e := executionlease.Counter(s.LocalGeneration); e != nil || s.LocalGeneration == "0" {
			return ErrState
		}
		if !id(s.Resource) || !id(s.Owner) || s.LocalGeneration == "" || !digests[s.Current] || s.Previous != "" && !hash(s.Previous) || resources[s.Resource] {
			return ErrState
		}
		resources[s.Resource] = true
	}
	return nil
}
func (r *Registry) persistLocked() error {
	if r.poisoned || r.closing {
		return ErrState
	}
	plain, e := json.Marshal(r.s)
	defer clear(plain)
	if e != nil || len(plain) > MaximumRegistryBytes-64 {
		return ErrState
	}
	a, e := aead(r.key)
	if e != nil {
		return ErrState
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return ErrState
	}
	sealed := a.Seal(nonce, nonce, plain, []byte(RegistryProfile+"\n"+r.s.Binding))
	if r.journal.Commit(sealed) != nil {
		r.poisoned = true
		for _, use := range r.uses {
			use.cancel()
		}
		return ErrState
	}
	return nil
}
func (r *Registry) begin(ctx context.Context) (context.Context, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || r.closing || r.poisoned || r.active {
		return nil, nil, ErrBusy
	}
	life, cancel := context.WithCancel(ctx)
	r.active = true
	r.cancel = cancel
	r.operations.Add(1)
	return life, func() {
		cancel()
		r.mu.Lock()
		r.active = false
		r.cancel = nil
		r.mu.Unlock()
		r.operations.Done()
	}, nil
}
func (r *Registry) index(digest string) int {
	for i, c := range r.s.Candidates {
		if c.Digest == digest {
			return i
		}
	}
	return -1
}
func (r *Registry) mark(index int, phase, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.s.Candidates[index].Phase = phase
	r.s.Candidates[index].Reason = reason
	if phase == "QUARANTINED" {
		for _, use := range r.uses {
			if use.environment == r.s.Candidates[index].Digest {
				use.cancel()
			}
		}
	}
	return r.persistLocked()
}
func (r *Registry) Snapshot() ([]Candidate, []Selection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing || r.poisoned {
		return nil, nil, ErrState
	}
	return append([]Candidate{}, r.s.Candidates...), append([]Selection{}, r.s.Active...), nil
}

// Install accepts only local archive references keyed by approved artifact ID.
// It records ownership BEFORE creating a stage. Failure retains the exact tree
// as QUARANTINED; no automatic cleanup, activation, Python/pip/git or download.
func (r *Registry) Install(ctx context.Context, p Plan, archives map[string]string) (Candidate, error) {
	if r == nil || !p.ValidAt(nowUTC()) || p.state.catalog.OS != runtime.GOOS || p.state.catalog.Architecture != runtime.GOARCH || len(archives) != len(p.state.artifacts) {
		return Candidate{}, ErrCatalog
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return Candidate{}, e
	}
	defer done()
	for _, a := range p.state.artifacts {
		if !portable(archives[a.ID]) {
			return Candidate{}, ErrFiles
		}
	}
	r.mu.Lock()
	if at := r.index(p.Digest()); at >= 0 {
		c := r.s.Candidates[at]
		r.mu.Unlock()
		return c, ErrBusy
	}
	if len(r.s.Candidates) >= MaximumCandidates {
		r.mu.Unlock()
		return Candidate{}, ErrBusy
	}
	var nonce [32]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		r.mu.Unlock()
		return Candidate{}, ErrState
	}
	c := Candidate{Digest: p.Digest(), CatalogDigest: p.CatalogDigest(), AllowlistRevision: p.state.catalog.Revision, Stage: hex.EncodeToString(nonce[:]), Phase: "INSTALLING"}
	at := len(r.s.Candidates)
	r.s.Candidates = append(r.s.Candidates, c)
	e = r.persistLocked()
	r.mu.Unlock()
	if e != nil {
		return c, ErrState
	}
	stage := bundleRoot + "/stages/" + c.Stage
	final := bundleRoot + "/environments/" + c.Digest
	fail := func(reason string) (Candidate, error) {
		r.mark(at, "QUARANTINED", reason)
		c.Phase = "QUARANTINED"
		c.Reason = reason
		return c, ErrFiles
	}
	if r.journal.Root().Mkdir(stage, 0700) != nil {
		return fail("STAGE_CREATE_FAILED")
	}
	for _, a := range p.state.artifacts {
		if extractArtifact(life, r.journal.Root(), stage, archives[a.ID], a) != nil {
			return fail("EXACT_ARTIFACT_INSTALL_FAILED")
		}
	}
	if writeMetadata(r.journal.Root(), stage, p) != nil || verifyTree(life, r.journal.Root(), stage, p) != nil {
		return fail("PRECOMMIT_TREE_VERIFICATION_FAILED")
	}
	if r.mark(at, "COMMITTING", "") != nil {
		return c, ErrState
	}
	if existing, e := plainFile(r.journal.Root(), final, true); e != nil || existing != nil {
		return fail("FINAL_PATH_ALREADY_EXISTS")
	}
	if life.Err() != nil || r.journal.Root().Rename(stage, final) != nil {
		return fail("DIRECTORY_MOVE_UNCONFIRMED")
	}
	if verifyTree(life, r.journal.Root(), final, p) != nil {
		return fail("POSTMOVE_TREE_VERIFICATION_FAILED")
	}
	if r.mark(at, "VERIFIED", "") != nil {
		return c, ErrState
	}
	c.Phase = "VERIFIED"
	return c, nil
}

// VerifyInstalled repeats the complete enabled-directory check. File drift or
// revoked/expired catalog never falls back to another version or node pack.
func (r *Registry) VerifyInstalled(ctx context.Context, p Plan) error {
	if r == nil || !p.ValidAt(nowUTC()) {
		return ErrCatalog
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	r.mu.Lock()
	at := r.index(p.Digest())
	if at < 0 || r.s.Candidates[at].Phase != "VERIFIED" || r.s.Candidates[at].CatalogDigest != p.CatalogDigest() {
		r.mu.Unlock()
		return ErrState
	}
	r.mu.Unlock()
	if verifyTree(life, r.journal.Root(), bundleRoot+"/environments/"+p.Digest(), p) != nil {
		r.mark(at, "QUARANTINED", "ENABLED_DIRECTORY_DRIFT")
		return ErrFiles
	}
	return nil
}

// ActivateLocal selects a VERIFIED local environment under the SAME physical
// maintenance owner. It runs nothing and grants no platform qualification or
// sales permission. Rollback is this same operation with a still-valid Plan;
// identity, execution state, fences, prices and money are never rolled back.
func (r *Registry) ActivateLocal(ctx context.Context, gate Maintenance, h execution.Hold, p Plan) (Selection, error) {
	if r == nil || gate == nil || !p.ValidAt(nowUTC()) || p.state.catalog.OS != runtime.GOOS || p.state.catalog.Architecture != runtime.GOARCH {
		return Selection{}, ErrCatalog
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return Selection{}, e
	}
	defer done()
	if gate.CheckMaintenance(life, h) != nil {
		return Selection{}, ErrBusy
	}
	r.mu.Lock()
	at := r.index(p.Digest())
	valid := at >= 0 && r.s.Candidates[at].Phase == "VERIFIED"
	r.mu.Unlock()
	if !valid {
		return Selection{}, ErrState
	}
	if verifyTree(life, r.journal.Root(), bundleRoot+"/environments/"+p.Digest(), p) != nil {
		r.mark(at, "QUARANTINED", "ACTIVATION_DIRECTORY_DRIFT")
		return Selection{}, ErrFiles
	}
	selection := Selection{}
	err := gate.WithMaintenance(life, h, func() error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if life.Err() != nil || !p.ValidAt(nowUTC()) || r.uses[h.Resource()] != nil {
			return ErrState
		}
		selection = Selection{Resource: h.Resource(), LocalGeneration: h.LocalGeneration(), Owner: h.Owner(), Current: p.Digest()}
		position := -1
		for i, old := range r.s.Active {
			if old.Resource == h.Resource() {
				selection.Previous = old.Current
				position = i
				break
			}
		}
		if position < 0 {
			if len(r.s.Active) >= execution.MaximumResources {
				return ErrBusy
			}
			r.s.Active = append(r.s.Active, selection)
		} else {
			r.s.Active[position] = selection
		}
		return r.persistLocked()
	})
	if err != nil {
		return Selection{}, ErrState
	}
	return selection, nil
}

// Delete records an immutable tombstone before removing only this candidate's
// exact original stage/final trees. Active candidates cannot be deleted; old
// records remain counted rather than resetting history or authority.
func (r *Registry) Delete(ctx context.Context, digest string) error {
	if r == nil || !hash(digest) {
		return ErrState
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	r.mu.Lock()
	at := r.index(digest)
	if at < 0 {
		r.mu.Unlock()
		return ErrState
	}
	c := r.s.Candidates[at]
	for _, use := range r.uses {
		if use.environment == digest {
			r.mu.Unlock()
			return ErrBusy
		}
	}
	for _, active := range r.s.Active {
		if active.Current == digest {
			r.mu.Unlock()
			return ErrBusy
		}
	}
	if c.Phase == "INSTALLING" || c.Phase == "COMMITTING" {
		r.mu.Unlock()
		return ErrBusy
	}
	if c.Phase == "DELETED" {
		r.mu.Unlock()
		return nil
	}
	r.s.Candidates[at].Phase = "TOMBSTONED"
	e = r.persistLocked()
	r.mu.Unlock()
	if e != nil {
		return ErrState
	}
	if life.Err() != nil {
		return ErrState
	}
	for _, name := range []string{bundleRoot + "/stages/" + c.Stage, bundleRoot + "/environments/" + c.Digest} {
		if removeOwnedTree(r.journal.Root(), name) != nil {
			return ErrFiles
		}
	}
	return r.mark(at, "DELETED", "")
}
func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closing = true
	if r.cancel != nil {
		r.cancel()
	}
	uses := make([]*useState, 0, len(r.uses))
	for _, use := range r.uses {
		uses = append(uses, use)
	}
	r.mu.Unlock()
	// Cancel prepared references immediately. Claimed references retain their
	// root and parent key until the actual native process/pipes have joined.
	for _, use := range uses {
		(Use{use}).Close()
	}
	r.operations.Wait()
	r.mu.Lock()
	clear(r.key)
	r.key = nil
	r.mu.Unlock()
}

// ReadPlan reads only the selected candidate's own signed public metadata and
// caller-supplied independent consent. A caller must supply current original pins,
// revocations and exact current local approval; it cannot restore live grants.
func (r *Registry) ReadPlan(ctx context.Context, digest string, v *Verifier, consent Consent) (Plan, error) {
	if r == nil || !hash(digest) || v == nil {
		return Plan{}, ErrState
	}
	life, done, e := r.begin(ctx)
	if e != nil {
		return Plan{}, e
	}
	defer done()
	r.mu.Lock()
	at := r.index(digest)
	valid := at >= 0 && r.s.Candidates[at].Phase == "VERIFIED"
	r.mu.Unlock()
	if !valid {
		return Plan{}, ErrState
	}
	name := bundleRoot + "/environments/" + digest + "/" + metadataDirectory + "/catalog.json"
	info, e := plainFile(r.journal.Root(), name, false)
	if e != nil || info == nil || info.Size() > MaximumCatalogBytes {
		return Plan{}, ErrState
	}
	f, e := r.journal.Root().Open(name)
	if e != nil {
		return Plan{}, ErrState
	}
	raw, e := readBounded(life, f, MaximumCatalogBytes)
	end := f.Close()
	if e != nil || end != nil {
		return Plan{}, ErrState
	}
	p, e := v.Verify(raw, consent, nowUTC())
	if e != nil || p.Digest() != digest {
		return Plan{}, ErrCatalog
	}
	return p, nil
}

// Keep errors opaque; filesystem paths and signed package URLs are not sent to
// external buyers as kernel execution diagnostics.
func isMissing(e error) bool { return errors.Is(e, os.ErrNotExist) }
