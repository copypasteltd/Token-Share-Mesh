package llamaworker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"p11.local/compute/shared/gpuid"
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
)

type child struct {
	input, control io.WriteCloser
	output         io.ReadCloser
	kill           func()
	wait           <-chan error
	pid            int
}
type submission struct {
	entry    *entry
	body     []byte
	deadline time.Time
}
type entry struct {
	work                       supervisor.Work
	id, manifest, state        string
	inputLimit, outputLimit    uint32
	next                       uint32
	output                     [][]byte
	outputBytes                int
	outputNotify               chan struct{}
	done                       chan struct{}
	terminal, revoked, cleaned bool
	outputErr                  error
	timer                      *time.Timer
	result                     Result
	started                    bool
	start                      supervisor.TextStart
	startNotify                chan struct{}
	stopCount                  uint32
}

// Result is local kernel measurement, NOT trusted platform usage, a billable
// event, payment evidence or a successful final response acknowledgement.
type Result = supervisor.TextResult

type Adapter struct {
	physical       gpuid.Identity
	mu             sync.Mutex
	c              Config
	p              child
	jobs           map[string]*entry
	active         *entry
	sequence       uint64
	session        string
	closed, failed bool
	ready          chan error
	exited         chan struct{}
	send           chan submission
	control        chan []byte
	stop           chan struct{}
	stopOnce       sync.Once
	readyOnce      sync.Once
}

var _ supervisor.InputAdapter = (*Adapter)(nil)
var _ supervisor.TextAdapter = (*Adapter)(nil)

func (a *Adapter) CheckInput(ctx context.Context, w supervisor.Work, m executionlease.Manifest, body []byte) error {
	normal, _, err := normalize(ctx, w, m, body, a.c)
	clear(normal)
	return err
}

// Open verifies a locally approved pack, starts its owned process and waits for
// model warm-up. It is an explicit owner action outside a task submit budget.
// Successful Open is not GPU/LP/privacy/engine-environment qualification.
func Open(ctx context.Context, c Config) (*Adapter, error) {
	template, e := c.validate(ctx)
	if e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, ErrConfig
	}
	p, e := launch(c)
	if e != nil {
		return nil, ErrConfig
	}
	a := newAdapter(c, p)
	var init encoder
	for _, s := range []string{Profile, UpstreamCommit, c.Library, c.BackendRegistry, c.BackendCore, c.Model.Path, template, c.GPUDevice, c.GPUUUID, c.GPUPCI} {
		init.str(s)
	}
	for _, v := range []uint32{c.ContextTokens, c.BatchTokens, c.Threads, c.GPULayers} {
		init.u32(v)
	}
	written := make(chan error, 1)
	go func() { defer clear(init.Bytes()); written <- writeFrame(p.input, kInit, init.Bytes()) }()
	select {
	case e = <-written:
	case <-ctx.Done():
		e = ErrUnknown
	}
	if e == nil {
		select {
		case e = <-a.ready:
		case <-ctx.Done():
			e = ErrUnknown
		}
	}
	a.mu.Lock()
	if a.failed && e == nil || ctx.Err() != nil {
		e = ErrUnknown
	}
	a.mu.Unlock()
	if e != nil {
		a.terminate()
		select {
		case <-a.exited:
		case <-ctx.Done():
		}
		var startup StartupError
		if errors.As(e, &startup) {
			return nil, startup
		}
		return nil, ErrUnknown
	}
	return a, nil
}

func newAdapter(c Config, p child) *Adapter {
	var nonce [16]byte
	_, e := rand.Read(nonce[:])
	if e != nil {
		panic("native session randomness unavailable")
	}
	a := &Adapter{c: c, p: p, jobs: map[string]*entry{}, session: hex.EncodeToString(nonce[:]), ready: make(chan error, 1), exited: make(chan struct{}), send: make(chan submission, 1), control: make(chan []byte, 1), stop: make(chan struct{})}
	go a.readLoop()
	go a.writeLoop()
	go a.controlLoop()
	go func() { <-p.wait; a.failure(); close(a.exited) }()
	return a
}
func (a *Adapter) terminate() {
	a.stopOnce.Do(func() {
		close(a.stop)
		a.p.kill()
		_ = a.p.input.Close()
		_ = a.p.control.Close()
		_ = a.p.output.Close()
	})
}
func (a *Adapter) failure() {
	a.mu.Lock()
	a.failed = true
	for _, e := range a.jobs {
		if e.state == "RUNNING" {
			e.state = "UNKNOWN"
			e.outputErr = ErrUnknown
			for _, b := range e.output {
				clear(b[:cap(b)])
			}
			e.output = nil
			e.outputBytes = 0
			a.finish(e)
		}
	}
	a.mu.Unlock()
	a.readyOnce.Do(func() { a.ready <- ErrUnknown })
	a.terminate()
}
func (a *Adapter) finish(e *entry) {
	if !e.terminal {
		e.terminal = true
		if e.timer != nil {
			e.timer.Stop()
		}
		close(e.done)
	}
}

// Submit without a verified bounded body is deliberately unavailable.
func (a *Adapter) Submit(context.Context, supervisor.Work) (supervisor.Observation, error) {
	return supervisor.Observation{}, ErrInput
}

func (a *Adapter) SubmitInput(ctx context.Context, w supervisor.Work, m executionlease.Manifest, body []byte) (supervisor.Observation, error) {
	if ctx.Err() != nil {
		return supervisor.Observation{}, ErrUnknown
	}
	normal, deadline, err := normalize(ctx, w, m, body, a.c)
	if err != nil {
		return supervisor.Observation{}, err
	}
	a.mu.Lock()
	if a.closed || a.failed || a.active != nil || len(a.jobs) >= maxJobs || a.jobs[w.RequestID] != nil || ctx.Err() != nil {
		a.mu.Unlock()
		clear(normal)
		return supervisor.Observation{}, ErrCapacity
	}
	a.sequence++
	var id encoder
	id.str(a.session)
	id.u64(a.sequence)
	// The original supervisor persists a bounded opaque ID with an alphabetic
	// prefix. Encoding alone begins with the binary string-length field (zero)
	// and would leave actual entered work unrecordable and safely quarantined.
	e := &entry{work: w, id: "native_" + hex.EncodeToString(id.Bytes()), manifest: m.Digest(), state: "RUNNING", inputLimit: uint32(m.InputTokenLimit), outputLimit: uint32(m.Spec.MaxOutputTokens), outputNotify: make(chan struct{}, 1), done: make(chan struct{}), startNotify: make(chan struct{})}
	// normalize validated this bounded binary request. Remember only the stop
	// count, never an additional copy of request text or stop strings.
	e.stopCount = uint32(normal[24])<<24 | uint32(normal[25])<<16 | uint32(normal[26])<<8 | uint32(normal[27])
	a.jobs[w.RequestID] = e
	a.active = e
	e.timer = time.AfterFunc(time.Until(deadline), func() { _ = a.Cancel(context.Background(), w) })
	a.send <- submission{e, normal, deadline}
	o := a.observation(e)
	a.mu.Unlock()
	return o, nil
}
func (a *Adapter) observation(e *entry) supervisor.Observation {
	return supervisor.Observation{RequestID: e.work.RequestID, BackendID: e.id, State: e.state}
}
func (a *Adapter) Query(ctx context.Context, w supervisor.Work) (supervisor.Observation, error) {
	if ctx.Err() != nil {
		return supervisor.Observation{}, ErrUnknown
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.jobs[w.RequestID]
	if e == nil || e.work != w {
		return supervisor.Observation{RequestID: w.RequestID, State: "UNKNOWN"}, ErrWork
	}
	return a.observation(e), nil
}
func (a *Adapter) cancelLocked(e *entry) bool {
	if e.revoked || e.terminal {
		return true
	}
	e.revoked = true
	var b encoder
	b.str(e.id)
	select {
	case a.control <- b.Bytes():
		return true
	default:
		clear(b.Bytes())
		return false
	}
}
func (a *Adapter) Cancel(ctx context.Context, w supervisor.Work) error {
	if ctx.Err() != nil {
		return ErrUnknown
	}
	a.mu.Lock()
	e := a.jobs[w.RequestID]
	if e == nil || e.work != w {
		a.mu.Unlock()
		return ErrWork
	}
	ok := a.cancelLocked(e)
	a.mu.Unlock()
	if !ok {
		a.failure()
		return ErrUnknown
	}
	return nil
}
func (a *Adapter) Cleanup(ctx context.Context, w supervisor.Work, o supervisor.Observation) (supervisor.Cleaned, error) {
	if ctx.Err() != nil {
		return supervisor.Cleaned{}, ErrUnknown
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.jobs[w.RequestID]
	if e == nil || e.work != w || e.state != "STOPPED" || o != a.observation(e) {
		return supervisor.Cleaned{}, ErrWork
	}
	// Only native synchronization/clear receipt authorizes STOPPED. Erase the
	// adapter-owned output before asserting its own workspace is clear.
	discarded := false
	for _, data := range e.output {
		clear(data[:cap(data)])
		discarded = true
	}
	e.output = nil
	e.outputBytes = 0
	if discarded && e.outputErr == nil {
		e.outputErr = ErrOutput
	}
	e.cleaned = true
	e.revoked = true
	if a.active == e {
		a.active = nil
	}
	return supervisor.Cleaned{RequestID: w.RequestID, BackendID: e.id, LocalGeneration: w.LocalGeneration, ManifestHash: e.manifest, WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}, nil
}

// Read returns raw token bytes, potentially split within UTF-8. The authorized
// content sink owns and must clear each returned slice. No UI/content listener
// is provided. Slow consumers trigger bounded cancellation, never unbounded RAM.
func (a *Adapter) Read(ctx context.Context, w supervisor.Work) ([]byte, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		a.mu.Lock()
		e := a.jobs[w.RequestID]
		if e == nil || e.work != w {
			a.mu.Unlock()
			return nil, ErrWork
		}
		if len(e.output) > 0 {
			b := e.output[0]
			e.output[0] = nil
			e.output = e.output[1:]
			e.outputBytes -= len(b)
			a.mu.Unlock()
			return b, nil
		}
		if e.terminal {
			err := e.outputErr
			a.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		notify, done := e.outputNotify, e.done
		a.mu.Unlock()
		select {
		case <-notify:
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Coalesce tiny token pieces into at most two 64-KiB owned buffers. Capacity is
// counted in bytes, so fast small tokens do not spuriously exhaust two events.
func (a *Adapter) queueOutput(e *entry, data []byte) bool {
	if e.outputBytes+len(data) > 2*65536 {
		return false
	}
	for len(data) > 0 {
		if len(e.output) == 0 || len(e.output[len(e.output)-1]) == 65536 {
			if len(e.output) >= 2 {
				return false
			}
			e.output = append(e.output, make([]byte, 0, 65536))
		}
		last := len(e.output) - 1
		n := min(65536-len(e.output[last]), len(data))
		e.output[last] = append(e.output[last], data[:n]...)
		e.outputBytes += n
		data = data[n:]
	}
	select {
	case e.outputNotify <- struct{}{}:
	default:
	}
	return true
}
func (a *Adapter) Result(w supervisor.Work) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.jobs[w.RequestID]
	if e == nil || e.work != w || e.state != "STOPPED" {
		return Result{}, ErrWork
	}
	return e.result, e.outputErr
}

func (a *Adapter) Started(ctx context.Context, w supervisor.Work) (supervisor.TextStart, error) {
	for {
		if ctx.Err() != nil {
			return supervisor.TextStart{}, ctx.Err()
		}
		a.mu.Lock()
		e := a.jobs[w.RequestID]
		if e == nil || e.work != w {
			a.mu.Unlock()
			return supervisor.TextStart{}, ErrWork
		}
		if e.started {
			v := e.start
			a.mu.Unlock()
			return v, nil
		}
		if e.terminal {
			err := e.outputErr
			if err == nil {
				err = ErrUnknown
			}
			a.mu.Unlock()
			return supervisor.TextStart{}, err
		}
		notify, done := e.startNotify, e.done
		a.mu.Unlock()
		select {
		case <-notify:
		case <-done:
		case <-ctx.Done():
			return supervisor.TextStart{}, ctx.Err()
		}
	}
}

// Close kills only this adapter's owned worker after waiting for its original
// work. Killing/exit without a native receipt leaves UNKNOWN and is no proof of
// GPU stop or resource release. There is no restart/re-submit fallback.
func (a *Adapter) Close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	e := a.active
	ok := true
	if e != nil {
		ok = a.cancelLocked(e)
	}
	a.mu.Unlock()
	err := error(nil)
	if !ok {
		err = ErrUnknown
	}
	if e != nil && ok {
		select {
		case <-e.done:
			a.mu.Lock()
			if e.state != "STOPPED" {
				err = ErrUnknown
			}
			a.mu.Unlock()
		case <-ctx.Done():
			err = ErrUnknown
		}
	}
	a.terminate()
	a.mu.Lock()
	for _, e := range a.jobs {
		if len(e.output) > 0 {
			for _, b := range e.output {
				clear(b[:cap(b)])
			}
			e.output = nil
			e.outputBytes = 0
			if e.outputErr == nil {
				e.outputErr = ErrOutput
			}
		}
	}
	a.mu.Unlock()
	select {
	case <-a.exited:
	case <-ctx.Done():
		err = ErrUnknown
	}
	return err
}

func (a *Adapter) writeLoop() {
	defer func() {
		for {
			select {
			case s := <-a.send:
				clear(s.body)
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-a.stop:
			return
		case s := <-a.send:
			var b encoder
			b.str(s.entry.id)
			b.u64(uint64(s.deadline.UnixMilli()))
			b.u32(uint32(len(s.body)))
			err := writeFrame(a.p.input, kBegin, b.Bytes())
			for at := 0; err == nil && at < len(s.body); {
				end := min(at+maxFrame-1, len(s.body))
				err = writeFrame(a.p.input, kData, s.body[at:end])
				at = end
			}
			clear(s.body)
			if err == nil {
				err = writeFrame(a.p.input, kEnd, nil)
			}
			if err != nil {
				a.failure()
				return
			}
		}
	}
}
func (a *Adapter) controlLoop() {
	defer func() {
		for {
			select {
			case b := <-a.control:
				clear(b)
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-a.stop:
			return
		case b := <-a.control:
			err := writeFrame(a.p.control, kCancel, b)
			clear(b)
			if err != nil {
				a.failure()
				return
			}
		}
	}
}
func (a *Adapter) readLoop() {
	ready := false
	for {
		kind, b, err := readFrame(a.p.output)
		if err != nil {
			a.failure()
			return
		}
		d := decoder{raw: b}
		if !ready {
			if kind == kInitError {
				profile, stage, code := d.str(64), d.u32(), d.u32()
				valid := profile == Profile && stage > 0 && stage < uint32(len(startupStages)) && d.end()
				clear(b)
				if valid {
					a.readyOnce.Do(func() { a.ready <- StartupError{Stage: startupStages[stage], NativeCode: code} })
				}
				a.failure()
				return
			}
			valid := kind == kReady && d.str(64) == Profile && d.str(64) == UpstreamCommit
			identity := decodeIdentity(&d)
			if a.c.GPULayers > 0 {
				valid = valid && identity.Validate() == nil && identity.Device == a.c.GPUDevice && identity.UUID == a.c.GPUUUID && identity.PCI == a.c.GPUPCI
			} else {
				valid = valid && identity == (gpuid.Identity{})
			}
			valid = valid && d.end()
			if valid {
				a.physical = identity
			}
			clear(b)
			if !valid {
				a.failure()
				return
			}
			ready = true
			a.readyOnce.Do(func() { a.ready <- nil })
			continue
		}
		id := d.str(256)
		a.mu.Lock()
		e := a.active
		valid := e != nil && id == e.id && e.state == "RUNNING"
		if valid {
			switch kind {
			case kStarted:
				n := d.u32()
				valid = d.end() && !e.started && n > 0 && n <= e.inputLimit
				if valid {
					e.started = true
					e.start = supervisor.TextStart{InputTokens: n}
					close(e.startNotify)
				}
			case kOutput:
				sequence := d.u32()
				data := d.take(len(d.raw) - d.at)
				valid = d.end() && e.started && sequence == e.next && len(data) > 0 && len(data) <= 16384
				if valid {
					e.next++
					if e.outputErr == nil {
						if !a.queueOutput(e, data) {
							e.outputErr = ErrOutput
							valid = a.cancelLocked(e)
						}
					}
				}
			case kDone:
				in, out, reason, clean, stopIndex := d.u32(), d.u32(), d.u32(), d.u32(), d.u32()
				valid = d.end() && clean == 1 && in <= a.c.ContextTokens && out <= e.outputLimit && reason >= 1 && reason <= 6 && (reason > 3 || e.started && in > 0 && in <= e.inputLimit) && (reason != 5 || out == 0) && (!e.started || e.start.InputTokens == in) && (reason == 3 && stopIndex < e.stopCount || reason != 3 && stopIndex == supervisor.NoStopIndex)
				if valid {
					names := []string{"", "eog", "limit", "stop", "cancelled", "rejected", "kernel_error"}
					e.result = Result{InputTokens: in, OutputTokens: out, Reason: names[reason], StopIndex: stopIndex}
					// A completed native decode cannot undo an earlier parent-side
					// revocation that arrived while synchronization was pending.
					if e.revoked && e.outputErr == nil {
						e.outputErr = context.Canceled
					}
					if reason == 4 && e.outputErr == nil {
						e.outputErr = context.Canceled
					}
					if reason == 5 && e.outputErr == nil {
						e.outputErr = ErrInput
					}
					if reason == 6 && e.outputErr == nil {
						e.outputErr = ErrUnknown
					}
					e.state = "STOPPED"
					e.revoked = true
					a.finish(e)
				}
			default:
				valid = false
			}
		}
		a.mu.Unlock()
		clear(b)
		if !valid {
			a.failure()
			return
		}
	}
}
