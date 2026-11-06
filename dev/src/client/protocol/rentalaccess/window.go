package rentalaccess

import (
	"encoding/json"
	"math"
	"strconv"
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
)

const AnchorProfile = "P11_RENTAL_CONTROL_ANCHOR_1"

// Anchor is an original-session durable control watermark. The local encrypted
// owner journal must retain it across crashes. A restored anchor NEVER restores
// a previous ALLOW window; only a new signed sequence can permit future work.
// Missing/corrupt/rolled-back journals must not be replaced with NewWindow.
type Anchor struct {
	Profile       string `json:"profile_id"`
	GrantDigest   string `json:"grant_digest"`
	Session       string `json:"session_id"`
	Generation    string `json:"resource_lock_generation"`
	Terms         string `json:"terms_revision"`
	Requested     string `json:"requested_sequence"`
	Accepted      string `json:"accepted_sequence"`
	ReplyIdentity string `json:"reply_identity,omitempty"`
	Stopped       bool   `json:"stopped"`
}

// SaveAnchor must atomically write and durably flush the original journal.
// A failed/uncertain commit poisons the in-memory window and cannot be retried
// as a new session. It must not log credentials or cause native execution.
type SaveAnchor func(Anchor) error

// EncodeAnchor/DecodeAnchor are only a bounded strict metadata codec. They do
// not encrypt, fsync, prove file ownership or detect rollback by themselves.
// Those properties belong to the original owner journal/independent watermark.
func EncodeAnchor(a Anchor, grant Verified, current Expected) ([]byte, error) {
	if !current.binds(grant) || !a.matches(grant) {
		return nil, ErrAccess
	}
	b, e := json.Marshal(a)
	if e != nil || len(b) > 2048 {
		return nil, ErrAccess
	}
	return b, nil
}
func DecodeAnchor(raw []byte, grant Verified, current Expected) (Anchor, error) {
	var a Anchor
	var fields map[string]json.RawMessage
	if !current.binds(grant) || strictjson.Decode(raw, &a, 2048) != nil || json.Unmarshal(raw, &fields) != nil || fields["stopped"] == nil || !a.matches(grant) {
		return Anchor{}, ErrAccess
	}
	return a, nil
}

type Window struct {
	mu                                   sync.Mutex
	grant                                Verified
	anchor                               Anchor
	save                                 SaveAnchor
	deadline, lastWall                   time.Time
	allowed, terminal, needsFreshRequest bool
}

func counter(s string) (uint64, error) {
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil || strconv.FormatUint(n, 10) != s {
		return 0, ErrAccess
	}
	return n, nil
}
func (a Anchor) matches(v Verified) bool {
	r, e := counter(a.Requested)
	if e != nil {
		return false
	}
	n, e := counter(a.Accepted)
	if e != nil {
		return false
	}
	return v.valid && a.Profile == AnchorProfile && a.GrantDigest == v.digest && a.Session == v.claims.Session && a.Generation == v.claims.Generation && a.Terms == v.claims.Terms && n <= r && ((n == 0 && a.ReplyIdentity == "") || (n > 0 && executionlease.Hash(a.ReplyIdentity)))
}

// NewWindow is only for a newly committed grant and newly created owner journal.
// It persists the zero watermark before returning. It does not permit work.
func NewWindow(grant Verified, current Expected, now time.Time, save SaveAnchor) (*Window, error) {
	if save == nil || !grant.ValidAt(now) || !current.binds(grant) {
		return nil, ErrAccess
	}
	a := Anchor{Profile: AnchorProfile, GrantDigest: grant.digest, Session: grant.claims.Session, Generation: grant.claims.Generation, Terms: grant.claims.Terms, Requested: "0", Accepted: "0"}
	if save(a) != nil {
		return nil, ErrAccess
	}
	return &Window{grant: grant, anchor: a, save: save, lastWall: now.Round(0)}, nil
}

func RestoreWindow(grant Verified, current Expected, now time.Time, a Anchor, save SaveAnchor) (*Window, error) {
	if save == nil || !grant.ValidAt(now) || !current.binds(grant) || !a.matches(grant) {
		return nil, ErrAccess
	}
	return &Window{grant: grant, anchor: a, save: save, lastWall: now.Round(0), terminal: a.Stopped, needsFreshRequest: true}, nil
}

func (w *Window) stopLocked() error {
	if w.terminal {
		return nil
	}
	w.allowed = false
	w.terminal = true
	w.anchor.Stopped = true
	// Safety is immediate even if a disk failure makes this write uncertain.
	if w.save(w.anchor) != nil {
		return ErrAccess
	}
	return nil
}
func (w *Window) validLocked(current Expected, now time.Time) bool {
	if w.terminal {
		return false
	}
	wall := now.Round(0)
	if !w.grant.ValidAt(now) || !current.binds(w.grant) || wall.Before(w.lastWall) || w.allowed && !now.Before(w.deadline) {
		w.stopLocked()
		return false
	}
	w.lastWall = wall
	return true
}
func (w *Window) commitLocked(next Anchor) error {
	if w.save(next) != nil {
		w.allowed = false
		w.terminal = true
		w.anchor.Stopped = true
		return ErrAccess
	}
	w.anchor = next
	return nil
}

// Request freezes one sequence BEFORE network I/O. A transport retry returns
// that exact outstanding sequence; it cannot consume a later control receipt.
func (w *Window) Request(current Expected, now time.Time) (string, error) {
	if w == nil {
		return "", ErrAccess
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.validLocked(current, now) {
		return "", ErrAccess
	}
	r, _ := counter(w.anchor.Requested)
	a, _ := counter(w.anchor.Accepted)
	if r > a && !w.needsFreshRequest {
		return w.anchor.Requested, nil
	}
	if r == math.MaxUint64 {
		w.stopLocked()
		return "", ErrAccess
	}
	next := w.anchor
	next.Requested = strconv.FormatUint(r+1, 10)
	if w.commitLocked(next) != nil {
		return "", ErrAccess
	}
	w.needsFreshRequest = false
	return next.Requested, nil
}

// Accept never extends a duplicate window and never revives expired/stopped
// work. The verifier, grant and independently refreshed current expectation all
// have to be supplied; receiving arbitrary JSON cannot manufacture authority.
func (w *Window) Accept(verifier *Verifier, raw []byte, current Expected, now time.Time) error {
	if w == nil {
		return ErrAccess
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.validLocked(current, now) || w.needsFreshRequest {
		return ErrAccess
	}
	reply, e := verifier.VerifyControl(raw, w.grant, current, now)
	if e != nil {
		return ErrAccess
	}
	c := reply.value
	if c.Sequence == w.anchor.Accepted {
		if reply.identity != w.anchor.ReplyIdentity {
			return ErrAccess
		}
		// A restored process has no live deadline; cached replies do not start it.
		return nil
	}
	if c.Sequence != w.anchor.Requested {
		return ErrAccess
	}
	next := w.anchor
	next.Accepted = c.Sequence
	next.ReplyIdentity = reply.identity
	next.Stopped = c.Decision == "STOP"
	if w.commitLocked(next) != nil {
		return ErrAccess
	}
	if c.Decision == "STOP" {
		w.allowed = false
		w.terminal = true
		return ErrAccess
	}
	expires, _ := utc(c.Expires)
	// Duration is bounded by the signed absolute expiry. time.Time.Add retains
	// Go's monotonic clock when now originates from time.Now; wall rollback is
	// independently refused. Receipt/replay time never creates another 45 sec.
	w.deadline = now.Add(expires.Sub(now.Round(0)))
	w.allowed = true
	return nil
}

// Check establishes control freshness only. Callers must also check the scope,
// current physical owner_session_id/generation, approved kernel/environment,
// serial invocation inbox and current permission before every native entry.
func (w *Window) Check(current Expected, now time.Time) error {
	if w == nil {
		return ErrAccess
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.validLocked(current, now) || !w.allowed {
		return ErrAccess
	}
	return nil
}
func (w *Window) Stop() error {
	if w == nil {
		return ErrAccess
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopLocked()
}
func (w *Window) Snapshot() Anchor {
	if w == nil {
		return Anchor{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.anchor
}
