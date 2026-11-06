package comfyworker

// SOURCE_ONLY. Synthetic native pipes, not a Python/GPU runtime or stop proof.
import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func transportProcess(t *testing.T) (*Process, *os.File, *os.File, *os.File) {
	t.Helper()
	ir, iw, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	or, ow, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		t.Fatal(e)
	}
	cr, cw, e := os.Pipe()
	if e != nil {
		ir.Close()
		iw.Close()
		or.Close()
		ow.Close()
		t.Fatal(e)
	}
	life, cancel := context.WithCancel(context.Background())
	p := &Process{life: life, cancel: cancel, child: nativeChild{input: iw, output: or, control: cw, kill: func() {}}, session: session{ID: "session_original", Generation: "19"}, frames: make(chan Frame, 1), ack: make(chan authorizationAck, 1), content: make(chan writeRequest), control: make(chan writeRequest), contentSlot: make(chan struct{}, 1), controlSlot: make(chan struct{}, 1), wake: make(chan struct{}, 1), done: make(chan struct{}), cutoff: time.Now().Add(time.Minute)}
	p.contentSlot <- struct{}{}
	p.controlSlot <- struct{}{}
	p.exited = make(chan struct{})
	p.readDone = make(chan struct{})
	p.pumps.Add(2)
	go p.writeLoop(p.content, iw)
	go p.writeLoop(p.control, cw)
	t.Cleanup(func() {
		p.abort()
		ir.Close()
		ow.Close()
		cr.Close()
		p.pumps.Wait()
		p.operations.Wait()
		for {
			select {
			case frame := <-p.frames:
				frame.Close()
			default:
				return
			}
		}
	})
	return p, ir, ow, cr
}
func TestComfyNativeControlWritesWhileOriginalContentPipeIsBlocked(t *testing.T) {
	p, _, _, control := transportProcess(t)
	request := writeRequest{Invoke, bytes.Repeat([]byte("s"), 2<<20), make(chan error, 1)}
	select {
	case p.content <- request:
	case <-time.After(time.Second):
		t.Fatal("original content writer did not enter")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	read := make(chan error, 1)
	go func() {
		var header [5]byte
		if _, e := io.ReadFull(control, header[:]); e != nil {
			read <- e
			return
		}
		n := binary.LittleEndian.Uint32(header[:4])
		if Kind(header[4]) != StopSession || n < 1 || n > 4097 {
			read <- ErrOutput
			return
		}
		body := make([]byte, n-1)
		_, e := io.ReadFull(control, body)
		defer clear(body)
		var value map[string]string
		if e == nil {
			e = json.Unmarshal(body, &value)
		}
		if value["session_id"] != "session_original" || value["local_generation"] != "19" {
			e = ErrOutput
		}
		read <- e
	}()
	if e := p.controlMessage(ctx, StopSession, ""); e != nil {
		t.Fatal("control was blocked by content", e)
	}
	if e := <-read; e != nil {
		t.Fatal(e)
	}
	select {
	case <-request.done:
		t.Fatal("blocked content unexpectedly completed without a reader")
	default:
	}
	p.abort()
	select {
	case e := <-request.done:
		if e == nil {
			t.Fatal("partial content stream reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("native descriptor close did not join writer")
	}
}
func TestComfyCancelledPartialContentPoisonsSessionAndJoinsEnteredWriter(t *testing.T) {
	p, _, _, _ := transportProcess(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if p.write(ctx, false, Invoke, bytes.Repeat([]byte("s"), 2<<20)) == nil {
		t.Fatal("blocked cancelled content succeeded")
	}
	p.mu.Lock()
	bad := p.life.Err() == nil || !p.closing || !p.failed
	p.mu.Unlock()
	if bad {
		t.Fatal("partial stream was left reusable")
	}
}
func TestComfyExpiredWatchdogCannotBeRevivedOrReanchored(t *testing.T) {
	p, _, _, _ := transportProcess(t)
	p.cutoff = time.Now().Add(-time.Millisecond)
	if p.setCutoff(time.Now().Add(time.Hour)) {
		t.Fatal("expired local deadline revived")
	}
}
func TestComfyCancelledNextDoesNotConsumeQueuedPrivateResult(t *testing.T) {
	p, _, _, _ := transportProcess(t)
	frame := Frame{&frameState{kind: Result, body: []byte("SECRET_RESULT_KEY")}}
	p.frames <- frame
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Next(ctx); e == nil {
		t.Fatal("cancelled reader obtained result")
	}
	if len(p.frames) != 1 {
		t.Fatal("already-cancelled call consumed private result")
	}
}
func TestComfyOriginalHostFormattingDoesNotExposeProcessSelection(t *testing.T) {
	p, _, _, _ := transportProcess(t)
	p.expected.TokenDigest = strings.Repeat("a", 64)
	selection := RuntimeSelection{Driver: "PRIVATE_DRIVER_PATH", Recipe: "PRIVATE_RECIPE_PATH"}
	raw, _ := json.Marshal(selection)
	for _, value := range []string{fmt.Sprintf("%+v %#v", p, selection), string(raw)} {
		if strings.Contains(value, "PRIVATE_") || strings.Contains(value, p.expected.TokenDigest) {
			t.Fatal("host formatting exposed private metadata")
		}
	}
}
func TestComfyNativeEnvironmentDoesNotInheritParentSecretsOrAlternativeGPU(t *testing.T) {
	t.Setenv("P11_SYNTHETIC_PARENT_SECRET", "DO_NOT_FORWARD")
	t.Setenv("CUDA_VISIBLE_DEVICES", "GPU-foreign")
	env := nativeEnvironment("GPU-original")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "DO_NOT_FORWARD") || strings.Contains(joined, "GPU-foreign") || !strings.Contains(joined, "CUDA_VISIBLE_DEVICES=GPU-original") || !strings.Contains(joined, "CUDA_DEVICE_ORDER=PCI_BUS_ID") {
		t.Fatal("unsafe inherited native environment")
	}
}
func TestComfyModelSetDigestIsPathIndependentSortedAndContentBound(t *testing.T) {
	a := ModelFile{Category: "vae", Alias: "vae.safetensors", Path: "LOCAL_A", Bytes: 33, SHA256: strings.Repeat("a", 64)}
	b := ModelFile{Category: "checkpoints", Alias: "model.safetensors", Path: "LOCAL_B", Bytes: 66, SHA256: strings.Repeat("b", 64)}
	first, e := ModelSetDigest([]ModelFile{a, b})
	if e != nil {
		t.Fatal(e)
	}
	a.Path = "OTHER_MACHINE"
	second, e := ModelSetDigest([]ModelFile{b, a})
	if e != nil || second != first {
		t.Fatal("local file placement/order changed model set")
	}
	a.Bytes++
	changed, e := ModelSetDigest([]ModelFile{a, b})
	if e != nil || changed == first {
		t.Fatal("changed model bytes did not change immutable model set")
	}
	if _, e = ModelSetDigest([]ModelFile{b, b}); e == nil {
		t.Fatal("duplicate model alias accepted")
	}
}
func TestComfyModelSetRejectsAmbiguousAliasesHashesAndUnsupportedCategories(t *testing.T) {
	for _, m := range []ModelFile{{Category: "arbitrary", Alias: "model", Bytes: 1, SHA256: strings.Repeat("a", 64)}, {Category: "vae", Alias: "../model", Bytes: 1, SHA256: strings.Repeat("a", 64)}, {Category: "vae", Alias: "model", Bytes: 1, SHA256: "bad"}, {Category: "vae", Alias: "model", Bytes: 0, SHA256: strings.Repeat("a", 64)}} {
		if _, e := ModelSetDigest([]ModelFile{m}); e == nil {
			t.Fatal("ambiguous model mapping accepted")
		}
	}
}

func TestComfyReadyRequiresNewProtocolExactOriginalScopeAndNoQualification(t *testing.T) {
	sess := session{ID: "session_original", Environment: strings.Repeat("a", 64), Recipe: strings.Repeat("b", 64)}
	base := map[string]string{"profile_id": Profile, "engine_release": Release, "upstream_commit": UpstreamCommit, "session_id": sess.ID, "environment_digest": sess.Environment, "recipe_digest": sess.Recipe, "qualification": "NOT_GRANTED"}
	for _, change := range []map[string]string{nil, {"profile_id": "P11_COMFY_PRIVATE_PIPE_1"}, {"engine_release": "comfy-private-1"}, {"session_id": "session_foreign"}, {"environment_digest": strings.Repeat("c", 64)}, {"qualification": "APPROVED"}, {"extra": "unexpected"}} {
		value := map[string]string{}
		for k, v := range base {
			value[k] = v
		}
		for k, v := range change {
			value[k] = v
		}
		raw, _ := json.Marshal(value)
		frame := Frame{&frameState{kind: Ready, body: raw}}
		if verifyReady(frame, sess) != (change == nil) {
			t.Fatal("ready scope/version/qualification check failed")
		}
		frame.Close()
	}
}
func writeSyntheticWorkerFrame(output *os.File, kind Kind, body []byte) error {
	var header [5]byte
	binary.LittleEndian.PutUint32(header[:4], uint32(len(body)+1))
	header[4] = byte(kind)
	if _, e := output.Write(header[:]); e != nil {
		return e
	}
	_, e := output.Write(body)
	return e
}
func TestComfyForeignPrivateAuthorizationAckPoisonsOriginalSession(t *testing.T) {
	p, _, output, _ := transportProcess(t)
	p.pumps.Add(1)
	go p.readLoop()
	ack := []byte(`{"phase":"AUTHORIZED","session_id":"session_foreign","local_generation":"19","grant_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	_ = writeSyntheticWorkerFrame(output, State, ack)
	select {
	case <-p.life.Done():
	case <-time.After(time.Second):
		t.Fatal("foreign authorization ack left original stream live")
	}
}
func TestComfyDuplicateOrUnsolicitedAuthorizationAckCannotEnableContent(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			p, _, output, _ := transportProcess(t)
			p.mu.Lock()
			p.authorizing = duplicate
			p.expected.TokenDigest = strings.Repeat("a", 64)
			p.mu.Unlock()
			p.pumps.Add(1)
			go p.readLoop()
			ack := []byte(`{"phase":"AUTHORIZED","session_id":"session_original","local_generation":"19","grant_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
			if e := writeSyntheticWorkerFrame(output, State, ack); e != nil {
				t.Fatal(e)
			}
			if duplicate {
				select {
				case <-p.ack:
				case <-time.After(time.Second):
					t.Fatal("first original ack missing")
				}
				_ = writeSyntheticWorkerFrame(output, State, ack)
			}
			select {
			case <-p.life.Done():
			case <-time.After(time.Second):
				t.Fatal("duplicate/unsolicited ack left original stream live")
			}
		})
	}
}
func TestComfyExitedChildDrainsBoundedFramesUntilRealClosedBeforeJoin(t *testing.T) {
	p, _, output, _ := transportProcess(t)
	close(p.exited)
	p.pumps.Add(1)
	go p.readLoop()
	for i := 0; i < 3; i++ {
		if e := writeSyntheticWorkerFrame(output, Result, []byte("PRIVATE_KEY")); e != nil {
			t.Fatal(e)
		}
	}
	closed := []byte(`{"session_id":"session_original","work_stopped":true,"future_starts_revoked":true,"requires_original_host_process_and_pipe_join":true}`)
	if e := writeSyntheticWorkerFrame(output, Closed, closed); e != nil {
		t.Fatal(e)
	}
	select {
	case <-p.readDone:
	case <-time.After(time.Second):
		t.Fatal("exited original pipe remained blocked behind unread frames")
	}
	p.mu.Lock()
	seen := p.closedFrame
	p.mu.Unlock()
	if !seen {
		t.Fatal("actual CLOSED was discarded before native join")
	}
}
