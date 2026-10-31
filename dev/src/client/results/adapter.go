package results

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

type job struct {
	work                         supervisor.Work
	manifest                     executionlease.Manifest
	record                       *record
	preparing, entered, rejected bool
}

// Adapter decorates an approved native text adapter. Submit remains once-only;
// result custody does not approve a package, payment, LP/E2E or trusted usage.
type Adapter struct {
	mu     sync.Mutex
	native supervisor.TextAdapter
	store  *Store
	jobs   map[string]*job
}

var _ supervisor.InputAdapter = (*Adapter)(nil)
var _ supervisor.TextOutputProvider = (*Adapter)(nil)

func New(native supervisor.TextAdapter, store *Store) (*Adapter, error) {
	if native == nil || store == nil {
		return nil, ErrConfig
	}
	return &Adapter{native: native, store: store, jobs: map[string]*job{}}, nil
}

func (a *Adapter) Submit(context.Context, supervisor.Work) (supervisor.Observation, error) {
	return supervisor.Observation{}, ErrBinding
}

func inputOptions(m executionlease.Manifest, body []byte) (options, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return options{}, ErrBinding
	}
	var o options
	if m.Spec.Protocol == relay.ChatText {
		if b := raw["stream_options"]; b != nil {
			var flags struct {
				IncludeUsage bool `json:"include_usage"`
			}
			if json.Unmarshal(b, &flags) != nil {
				return o, ErrBinding
			}
			o.IncludeUsage = flags.IncludeUsage
		}
	}
	field := "stop"
	if m.Spec.Protocol == relay.MessagesText {
		field = "stop_sequences"
	}
	if b := raw[field]; b != nil {
		if b[0] == '"' {
			var s string
			if json.Unmarshal(b, &s) != nil {
				return o, ErrBinding
			}
			o.Stops = []string{s}
		} else if json.Unmarshal(b, &o.Stops) != nil {
			return o, ErrBinding
		}
	}
	return o, nil
}

func (a *Adapter) SubmitInput(ctx context.Context, w supervisor.Work, m executionlease.Manifest, body []byte) (supervisor.Observation, error) {
	a.mu.Lock()
	if a.jobs[w.RequestID] != nil || len(a.jobs) >= 1024 {
		a.mu.Unlock()
		return supervisor.Observation{}, ErrBinding
	}
	j := &job{work: w, manifest: m, preparing: true}
	a.jobs[w.RequestID] = j
	a.mu.Unlock()
	// Query during preparation is UNKNOWN. Only the completed pre-entry path
	// below can attest ABSENT; a missing map/restart is never absence evidence.
	reject := func(err error) (supervisor.Observation, error) {
		a.mu.Lock()
		j.preparing = false
		j.rejected = true
		a.mu.Unlock()
		return supervisor.Observation{}, err
	}
	if ctx.Err() != nil {
		return reject(ctx.Err())
	}
	if !original(w, m) {
		return reject(ErrBinding)
	}
	if e := a.native.CheckInput(ctx, w, m, body); e != nil {
		return reject(e)
	}
	o, e := inputOptions(m, body)
	if e != nil {
		return reject(e)
	}
	r, e := a.store.begin(w, m, o, func() {
		cancel, done := context.WithTimeout(context.Background(), a.store.c.DrainGrace)
		defer done()
		_ = a.native.Cancel(cancel, w)
	})
	if e != nil {
		return reject(e)
	}
	if ctx.Err() != nil {
		r.complete(ErrUnavailable)
		_ = r.destroy()
		return reject(ctx.Err())
	}
	a.mu.Lock()
	j.record = r
	j.preparing = false
	j.entered = true
	a.mu.Unlock()
	owned, interrupt := context.WithCancel(ctx)
	withdraw := context.AfterFunc(r.life, interrupt)
	if r.life.Err() != nil {
		interrupt()
	}
	observation, err := a.native.SubmitInput(owned, w, m, body)
	withdraw()
	interrupt()
	// Close/retention may race the native entry. Query/cancel THAT original
	// after return as well; an early "not found" must not permit a later start.
	if r.life.Err() != nil {
		r.stop()
	}
	// Entering native Submit is ambiguous even on error. Drain/query the same
	// original; never submit it again and never manufacture an ABSENT receipt.
	go a.pump(j)
	return observation, err
}

func (a *Adapter) lookup(w supervisor.Work) (*job, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	j := a.jobs[w.RequestID]
	if j == nil || j.work != w || j.preparing {
		return nil, ErrUnavailable
	}
	return j, nil
}

func (a *Adapter) Query(ctx context.Context, w supervisor.Work) (supervisor.Observation, error) {
	if ctx.Err() != nil {
		return supervisor.Observation{}, ctx.Err()
	}
	j, e := a.lookup(w)
	if e != nil {
		return supervisor.Observation{RequestID: w.RequestID, State: "UNKNOWN"}, e
	}
	if j.rejected {
		return supervisor.Observation{RequestID: w.RequestID, State: "ABSENT"}, nil
	}
	return a.native.Query(ctx, w)
}
func (a *Adapter) Cancel(ctx context.Context, w supervisor.Work) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	j, e := a.lookup(w)
	if e != nil {
		return e
	}
	if j.rejected {
		return nil
	}
	return a.native.Cancel(ctx, w)
}
func (a *Adapter) Cleanup(ctx context.Context, w supervisor.Work, o supervisor.Observation) (supervisor.Cleaned, error) {
	if ctx.Err() != nil {
		return supervisor.Cleaned{}, ctx.Err()
	}
	j, e := a.lookup(w)
	if e != nil {
		return supervisor.Cleaned{}, e
	}
	if j.rejected {
		if o != (supervisor.Observation{RequestID: w.RequestID, State: "ABSENT"}) {
			return supervisor.Cleaned{}, ErrBinding
		}
		return supervisor.Cleaned{RequestID: w.RequestID, LocalGeneration: w.LocalGeneration, ManifestHash: j.manifest.Digest(), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}, nil
	}
	select {
	case <-j.record.done:
		return a.native.Cleanup(ctx, w, o)
	case <-ctx.Done():
		return supervisor.Cleaned{}, ErrPending
	}
}

func (a *Adapter) Output(ctx context.Context, w supervisor.Work) (supervisor.TextOutput, error) {
	j, e := a.lookup(w)
	if e != nil || j.rejected || !j.entered || j.record == nil {
		return nil, ErrUnavailable
	}
	return j.record.open(ctx)
}

func (a *Adapter) Discard(ctx context.Context, w supervisor.Work) error {
	j, e := a.lookup(w)
	if e != nil || j.record == nil {
		return ErrUnavailable
	}
	r := j.record
	r.revoke()
	select {
	case <-r.done:
	default:
		if e = a.native.Cancel(ctx, w); e != nil {
			return e
		}
	}
	select {
	case <-r.done:
		return r.destroy()
	case <-ctx.Done():
		return ErrPending
	}
}

func (a *Adapter) pump(j *job) {
	r := j.record
	deadline, e := time.Parse(time.RFC3339Nano, j.manifest.HardDeadline)
	if e != nil {
		r.complete(ErrUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(a.store.c.DrainGrace))
	defer cancel()
	start, e := a.native.Started(ctx, j.work)
	failed := e != nil
	fail := func() {
		if !failed {
			failed = true
		}
		stop, done := context.WithTimeout(context.Background(), a.store.c.DrainGrace)
		_ = a.native.Cancel(stop, j.work)
		done()
	}
	if !failed {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], start.InputTokens)
		if start.InputTokens == 0 || uint64(start.InputTokens) > uint64(j.manifest.InputTokenLimit) || r.append(kStarted, b[:]) != nil {
			fail()
		}
	}
	if failed {
		fail()
	}
	h := sha256.New()
	var total uint64
	var tail []byte
	for {
		b, readErr := a.native.Read(ctx, j.work)
		if len(b) > 0 {
			if !failed {
				// Native token pieces may split a rune. Validate incrementally without
				// retaining a second complete plaintext result in memory.
				joined := append(tail, b...)
				tail = nil
				at := 0
				for at < len(joined) && utf8.FullRune(joined[at:]) {
					value, n := utf8.DecodeRune(joined[at:])
					if value == utf8.RuneError && n == 1 {
						fail()
						break
					}
					at += n
				}
				if !failed && len(joined)-at > 3 {
					fail()
				}
				if !failed {
					tail = append([]byte(nil), joined[at:]...)
				}
				clear(joined)
				if !failed {
					for at := 0; at < len(b); {
						end := at + maxPiece
						if end > len(b) {
							end = len(b)
						}
						if e = r.append(kText, b[at:end]); e != nil {
							fail()
							break
						}
						at = end
					}
					if !failed {
						_, _ = h.Write(b)
						total += uint64(len(b))
					}
				}
			}
			clear(b[:cap(b)])
		}
		if readErr != nil {
			if readErr != io.EOF {
				fail()
			}
			break
		}
		if len(b) == 0 {
			fail()
			break
		}
	}
	if len(tail) > 0 {
		fail()
	}
	clear(tail)
	if !failed {
		result, err := a.native.Result(j.work)
		if err != nil || result.InputTokens != start.InputTokens || uint64(result.OutputTokens) > uint64(j.manifest.Spec.MaxOutputTokens) || (result.Reason != "eog" && result.Reason != "limit" && result.Reason != "stop") {
			fail()
		} else {
			end := encodeResult(result, total, h.Sum(nil))
			e = r.append(kFinal, end)
			clear(end)
			if e != nil {
				fail()
			} else {
				r.mu.Lock()
				e = r.file.Sync()
				r.mu.Unlock()
				if e != nil {
					fail()
				}
			}
		}
	}
	if failed {
		r.complete(ErrUnavailable)
	} else {
		r.complete(nil)
	}
}
