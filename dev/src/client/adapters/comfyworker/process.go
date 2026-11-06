package comfyworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

type nativeChild struct {
	input, output, control *os.File
	wait                   <-chan error
	kill                   func()
}
type writeRequest struct {
	kind Kind
	body []byte
	done chan error
}
type authorizationAck struct {
	Phase      string `json:"phase"`
	Session    string `json:"session_id"`
	Generation string `json:"local_generation"`
	Digest     string `json:"grant_digest"`
}

// Process owns one original environment/physical reference and THREE private
// native pipes. It exposes no network server, sale/ready proof, GPU release,
// output acceptance, metering or workspace cleanup authority. Public media
// admission still needs the original authenticated input/workspace adapter.
type Process struct {
	mu                                                    sync.Mutex
	use                                                   comfyenv.Use
	child                                                 nativeChild
	session                                               session
	life                                                  context.Context
	cancel                                                context.CancelFunc
	done                                                  chan struct{}
	exited, readDone                                      chan struct{}
	frames                                                chan Frame
	ack                                                   chan authorizationAck
	content, control                                      chan writeRequest
	contentSlot, controlSlot                              chan struct{}
	wake                                                  chan struct{}
	cutoff                                                time.Time
	paidCutoff                                            time.Time
	expected                                              rentalaccess.Expected
	authorizing, authorized, closing, failed, closedFrame bool
	nativeError                                           error
	operations                                            sync.WaitGroup
	pumps                                                 sync.WaitGroup
	abortOnce, closeOnce                                  sync.Once
	authMu                                                sync.Mutex
	mediaOwned                                            bool
	mediaAdapter                                          *RentalMediaAdapter
}

func (p *Process) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original private ComfyUI process redacted]")
}
func (p *Process) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// OpenPrepared's ctx is the original host SESSION lifetime. Loading an exact
// approved bundle can precede a grant, but INVOKE cannot. The preparation
// deadline is monotonic and never becomes the paid session hard deadline.
func OpenPrepared(ctx context.Context, use comfyenv.Use, selection RuntimeSelection) (*Process, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrOutput
	}
	spec, e := buildLaunch(use, selection)
	if e != nil {
		return nil, e
	}
	defer func() { clear(spec.initial) }()
	life, cancel := context.WithCancel(context.Background())
	p := &Process{use: use, session: spec.session, life: life, cancel: cancel, done: make(chan struct{}), frames: make(chan Frame, 1), ack: make(chan authorizationAck, 1), content: make(chan writeRequest), control: make(chan writeRequest), contentSlot: make(chan struct{}, 1), controlSlot: make(chan struct{}, 1), wake: make(chan struct{}, 1), cutoff: use.PreparationDeadline()}
	p.contentSlot <- struct{}{}
	p.controlSlot <- struct{}{}
	p.exited = make(chan struct{})
	p.readDone = make(chan struct{})
	if e = use.Claim(p.done); e != nil {
		cancel()
		return nil, ErrOutput
	}
	child, e := launchNative(spec)
	if e != nil {
		cancel()
		close(p.done)
		return nil, ErrOutput
	}
	p.child = child
	stopCaller := context.AfterFunc(ctx, p.abort)
	stopRegistry := context.AfterFunc(use.Context(), p.abort)
	p.pumps.Add(5)
	go p.writeLoop(p.content, child.input)
	go p.writeLoop(p.control, child.control)
	go p.readLoop()
	go p.guardLoop()
	go p.deadlineLoop()
	go func() {
		nativeError := <-child.wait
		p.mu.Lock()
		p.nativeError = nativeError
		p.closing = true
		p.mu.Unlock()
		close(p.exited)
		// cmd.Wait may beat the buffered CLOSED frame. Drain only the already
		// owned stdout briefly; an exited child never grants new operations.
		drain := time.NewTimer(250 * time.Millisecond)
		select {
		case <-p.readDone:
		case <-drain.C:
			p.abort()
		}
		drain.Stop()
		p.mu.Lock()
		if !p.closedFrame {
			p.failed = true
		}
		p.mu.Unlock()
		p.stopIO()
		p.pumps.Wait()
		p.mu.Lock()
		p.closing = true
		p.mu.Unlock()
		p.operations.Wait()
		p.mu.Lock()
		mediaAdapter := p.mediaAdapter
		p.mu.Unlock()
		if mediaAdapter != nil {
			mediaAdapter.revokeResults()
		}
		for {
			select {
			case frame := <-p.frames:
				frame.Close()
			default:
				stopCaller()
				stopRegistry()
				close(p.done)
				return
			}
		}
	}()
	opening, finish := context.WithDeadline(ctx, p.cutoff)
	defer finish()
	writeErr := p.write(opening, false, Initialize, spec.initial)
	clear(spec.initial)
	spec.initial = nil
	if writeErr != nil {
		p.abort()
		return nil, ErrOutput
	}
	frame, e := p.Next(opening)
	if e != nil {
		p.abort()
		return nil, ErrOutput
	}
	defer frame.Close()
	if !verifyReady(frame, p.session) {
		p.abort()
		return nil, ErrOutput
	}
	return p, nil
}
func verifyReady(frame Frame, sess session) bool {
	raw, e := frame.Take()
	if e != nil {
		return false
	}
	defer clear(raw)
	var ready struct {
		Profile       string `json:"profile_id"`
		Release       string `json:"engine_release"`
		Commit        string `json:"upstream_commit"`
		Session       string `json:"session_id"`
		Environment   string `json:"environment_digest"`
		Recipe        string `json:"recipe_digest"`
		Qualification string `json:"qualification"`
	}
	return frame.Kind() == Ready && strictjson.Decode(raw, &ready, 65536) == nil && ready.Profile == Profile && ready.Release == Release && ready.Commit == UpstreamCommit && ready.Session == sess.ID && ready.Environment == sess.Environment && ready.Recipe == sess.Recipe && ready.Qualification == "NOT_GRANTED"
}
func (p *Process) enter() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.life.Err() != nil {
		return false
	}
	p.operations.Add(1)
	return true
}
func (p *Process) abort() { p.mu.Lock(); p.failed = true; p.mu.Unlock(); p.stopIO() }
func (p *Process) stopIO() {
	p.abortOnce.Do(func() {
		p.mu.Lock()
		p.closing = true
		p.mu.Unlock()
		p.cancel()
		// Terminate the peer before descriptor closure so a blocked native
		// write cannot postpone the request to stop the actual process.
		p.child.kill()
		p.child.input.Close()
		p.child.control.Close()
		p.child.output.Close()
	})
}
func (p *Process) writeLoop(queue <-chan writeRequest, output *os.File) {
	defer p.pumps.Done()
	for {
		select {
		case <-p.life.Done():
			return
		case request := <-queue:
			e := WritePrivateFrame(p.life, output, request.kind, request.body)
			clear(request.body)
			request.done <- e
			if e != nil {
				p.abort()
				return
			}
		}
	}
}
func (p *Process) write(ctx context.Context, control bool, kind Kind, body []byte) error {
	if ctx == nil || ctx.Err() != nil || inputLimit(kind) == 0 || len(body) < 1 || len(body) > inputLimit(kind) || !p.enter() {
		return ErrOutput
	}
	defer p.operations.Done()
	slot, queue := p.contentSlot, p.content
	if control {
		slot, queue = p.controlSlot, p.control
	}
	select {
	case <-ctx.Done():
		return ErrOutput
	case <-p.life.Done():
		return ErrOutput
	case <-slot:
	}
	defer func() { slot <- struct{}{} }()
	request := writeRequest{kind, append([]byte{}, body...), make(chan error, 1)}
	select {
	case <-ctx.Done():
		clear(request.body)
		return ErrOutput
	case <-p.life.Done():
		clear(request.body)
		return ErrOutput
	case queue <- request:
	}
	// A cancelled/partially written content frame poisons the entire original
	// stream. Close native descriptors to unblock the OS write, then join it.
	select {
	case e := <-request.done:
		return e
	case <-ctx.Done():
		p.abort()
		<-request.done
		return ErrOutput
	case <-p.life.Done():
		<-request.done
		return ErrOutput
	}
}
func (p *Process) readLoop() {
	defer p.pumps.Done()
	defer close(p.readDone)
	ackSeen := false
	for {
		frame, e := ReadWorkerFrame(p.life, p.child.output)
		if e != nil {
			p.abort()
			return
		}
		if frame.Kind() == State {
			raw, e := frame.Take()
			var ack authorizationAck
			if e != nil || strictjson.Decode(raw, &ack, 4096) != nil || ack.Phase != "AUTHORIZED" || ack.Session != p.session.ID || ack.Generation != p.session.Generation || ackSeen {
				clear(raw)
				frame.Close()
				p.abort()
				return
			}
			p.mu.Lock()
			expectedAck := p.authorizing && !p.authorized && p.expected.TokenDigest == ack.Digest
			p.mu.Unlock()
			if !expectedAck {
				clear(raw)
				frame.Close()
				p.abort()
				return
			}
			clear(raw)
			frame.Close()
			ackSeen = true
			select {
			case p.ack <- ack:
			case <-p.life.Done():
				return
			}
			continue
		}
		if frame.Kind() == Closed {
			raw, e := frame.Take()
			var closed struct {
				Session      string `json:"session_id"`
				Stopped      bool   `json:"work_stopped"`
				Revoked      bool   `json:"future_starts_revoked"`
				RequiresJoin bool   `json:"requires_original_host_process_and_pipe_join"`
			}
			if e != nil || strictjson.Decode(raw, &closed, 4096) != nil || closed.Session != p.session.ID || !closed.Stopped || !closed.Revoked || !closed.RequiresJoin {
				clear(raw)
				frame.Close()
				p.abort()
				return
			}
			frame = Frame{&frameState{kind: Closed, body: raw}}
			p.mu.Lock()
			p.closedFrame = true
			p.mu.Unlock()
			select {
			case p.frames <- frame:
			case <-p.exited:
				frame.Close()
			case <-p.life.Done():
				frame.Close()
			}
			return
		}
		select {
		case p.frames <- frame:
		case <-p.exited:
			frame.Close()
		case <-p.life.Done():
			frame.Close()
			return
		}
	}
}
func (p *Process) Next(ctx context.Context) (Frame, error) {
	if ctx == nil || ctx.Err() != nil || !p.enter() {
		return Frame{}, ErrOutput
	}
	defer p.operations.Done()
	select {
	case <-ctx.Done():
		return Frame{}, ErrOutput
	case <-p.life.Done():
		return Frame{}, ErrOutput
	case frame := <-p.frames:
		if p.life.Err() != nil || ctx.Err() != nil {
			frame.Close()
			return Frame{}, ErrOutput
		}
		return frame, nil
	}
}
func (p *Process) setCutoff(deadline time.Time) bool {
	p.mu.Lock()
	valid := p.life.Err() == nil && time.Now().Before(p.cutoff) && time.Now().Before(deadline)
	if valid {
		p.cutoff = deadline
	}
	p.mu.Unlock()
	if valid {
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
	return valid
}
func (p *Process) deadlineLoop() {
	defer p.pumps.Done()
	for {
		p.mu.Lock()
		deadline := p.cutoff
		p.mu.Unlock()
		timer := time.NewTimer(max(0, time.Until(deadline)))
		select {
		case <-p.life.Done():
			timer.Stop()
			return
		case <-p.wake:
			timer.Stop()
		case <-timer.C:
			p.mu.Lock()
			expired := !time.Now().Before(p.cutoff)
			p.mu.Unlock()
			if expired {
				p.abort()
				return
			}
		}
	}
}
func (p *Process) guardLoop() {
	defer p.pumps.Done()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.life.Done():
			return
		case <-ticker.C:
		}
		p.mu.Lock()
		cutoff, expected, paid := p.cutoff, p.expected, p.paidCutoff
		p.mu.Unlock()
		ctx, cancel := context.WithDeadline(p.life, minTime(cutoff, time.Now().Add(200*time.Millisecond)))
		status, e := p.use.Current(ctx)
		cancel()
		if p.life.Err() != nil {
			return
		}
		if e != nil || status.Phase != "PREPARING" && status.Phase != "ACTIVE" {
			p.abort()
			return
		}
		if expected.Grant == "" {
			if time.Now().After(p.use.PreparationDeadline()) {
				p.abort()
				return
			}
			if !status.Deadline.IsZero() && !p.setCutoff(minTime(status.Deadline, p.use.PreparationDeadline())) {
				p.abort()
				return
			}
			continue
		}
		if !status.Authorized || status.Expected != expected {
			p.abort()
			return
		}
		// The original ledger's signed control clock, not a new timeout from this
		// poll, is the only value that can move this local watchdog forward.
		if !p.setCutoff(minTime(status.Deadline, paid)) {
			p.abort()
			return
		}
	}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Authorize consumes neither a caller-derived token hash nor a payment claim.
// It binds a previously verified grant to the independently current original
// ledger and waits for a private ACK before content may enter another pipe.
// Paid term renewal is a separate adapter operation; this method is one-shot.
func (p *Process) Authorize(ctx context.Context, grant rentalaccess.Verified) error {
	if ctx == nil || !p.enter() {
		return ErrOutput
	}
	defer p.operations.Done()
	p.authMu.Lock()
	defer p.authMu.Unlock()
	p.mu.Lock()
	used := p.authorizing || p.authorized
	p.mu.Unlock()
	if used {
		return ErrOutput
	}
	status, e := p.use.Current(ctx)
	_, prepared := p.use.Original()
	now := time.Now()
	if e != nil || !status.Authorized || !grant.Matches(status.Expected, now) || !prepared.BindsGrant(status.Expected) || !grant.Allows("INVOKE", now) || p.use.CheckScope(ctx, "INVOKE") != nil {
		return ErrOutput
	}
	checked, e := p.use.CheckExpected(ctx, status.Expected, "INVOKE")
	if e != nil {
		return ErrOutput
	}
	status.Deadline = checked
	hard, e := time.Parse(time.RFC3339Nano, status.Expected.HardDeadline)
	if e != nil || !hard.After(now) || hard.Sub(now) > 24*time.Hour {
		return ErrOutput
	}
	// Preserve the monotonic observation made ONCE here. Reparsing/reanchoring
	// a wall-clock paid deadline on each poll could extend it after rollback.
	paid := now.Add(hard.Sub(now))
	p.mu.Lock()
	p.authorizing = true
	p.expected = status.Expected
	p.paidCutoff = paid
	p.mu.Unlock()
	if !p.setCutoff(minTime(paid, status.Deadline)) {
		p.abort()
		return ErrOutput
	}
	raw, e := json.Marshal(struct {
		Session    string `json:"session_id"`
		Generation string `json:"local_generation"`
		Digest     string `json:"grant_digest"`
		HardMS     int64  `json:"hard_deadline_unix_ms"`
	}{p.session.ID, p.session.Generation, grant.Digest(), hard.UnixMilli()})
	if e != nil {
		p.abort()
		return ErrOutput
	}
	defer clear(raw)
	if p.write(ctx, true, Authorize, raw) != nil {
		p.abort()
		return ErrOutput
	}
	select {
	case <-ctx.Done():
		p.abort()
		return ErrOutput
	case <-p.life.Done():
		return ErrOutput
	case ack := <-p.ack:
		_, e := p.use.CheckExpected(ctx, status.Expected, "INVOKE")
		if ack.Digest != grant.Digest() || e != nil {
			p.abort()
			return ErrOutput
		}
		p.mu.Lock()
		p.authorized = true
		p.authorizing = false
		p.mu.Unlock()
		return nil
	}
}

// writeContent is deliberately package-private until the actual original
// authenticated media input/workspace adapter is connected. Raw buyer JSON
// cannot bypass its durable original rental inbox, workspace and file quota.
func (p *Process) writeContent(ctx context.Context, kind Kind, body []byte) error {
	p.mu.Lock()
	allowed := p.authorized && !p.closing
	expected := p.expected
	p.mu.Unlock()
	if !allowed || (kind != Invoke && kind != AssetBegin && kind != AssetData && kind != AssetEnd) {
		return ErrOutput
	}
	if _, e := p.use.CheckExpected(ctx, expected, "INVOKE"); e != nil {
		return ErrOutput
	}
	return p.write(ctx, false, kind, body)
}
func (p *Process) Cancel(ctx context.Context, invocation string) error {
	if len(invocation) > 128 || !ids.Valid(invocation) {
		return ErrOutput
	}
	return p.controlMessage(ctx, Cancel, invocation)
}
func (p *Process) controlMessage(ctx context.Context, kind Kind, id string) error {
	if p == nil {
		return ErrOutput
	}
	value := map[string]string{"session_id": p.session.ID, "local_generation": p.session.Generation}
	if id != "" {
		value["invocation_id"] = id
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return ErrOutput
	}
	defer clear(raw)
	return p.write(ctx, true, kind, raw)
}
func (p *Process) Join(ctx context.Context) error {
	if p == nil || ctx == nil {
		return ErrOutput
	}
	select {
	case <-ctx.Done():
		return ErrOutput
	case <-p.done:
		p.mu.Lock()
		bad := p.nativeError != nil || p.failed
		p.mu.Unlock()
		if bad {
			return ErrOutput
		}
		return nil
	}
}

// Close asks via the independent control pipe, then kills on a bounded wait.
// A timeout NEVER closes done or releases the claimed environment reference.
func (p *Process) Close(ctx context.Context) error {
	if p == nil || ctx == nil {
		return ErrOutput
	}
	p.closeOnce.Do(func() {
		go func() {
			stop, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_ = p.controlMessage(stop, StopSession, "")
			cancel()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-p.done:
			case <-timer.C:
				p.abort()
			}
		}()
	})
	return p.Join(ctx)
}
