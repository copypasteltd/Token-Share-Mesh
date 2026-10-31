package results

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

func scope(id string) (supervisor.Work, executionlease.Manifest) {
	w := supervisor.Work{RequestID: id, Resource: "physical_gpu", LocalGeneration: "local_" + id, Environment: strings.Repeat("a", 64)}
	return w, executionlease.Manifest{Profile: executionlease.ManifestProfile, Resource: w.Resource, EnvironmentDigest: w.Environment, Mode: "PLAINTEXT", Privacy: "STANDARD", InputTokenLimit: 64, HardDeadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Spec: executionlease.Spec{Protocol: relay.ChatText, Model: "fixture_model", MaxOutputTokens: 32}}
}
func store(t *testing.T, cfg func(*Config)) *Store {
	t.Helper()
	c := DefaultConfig(t.TempDir())
	if cfg != nil {
		cfg(&c)
	}
	s, e := Open(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func sealed(t *testing.T, s *Store, id, text string) *record {
	t.Helper()
	w, m := scope(id)
	r, e := s.begin(w, m, options{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], 3)
	if e = r.append(kStarted, b[:]); e != nil {
		t.Fatal(e)
	}
	if text != "" {
		if e = r.append(kText, []byte(text)); e != nil {
			t.Fatal(e)
		}
	}
	h := sha(text)
	end := encodeResult(supervisor.TextResult{InputTokens: 3, OutputTokens: 2, Reason: "eog", StopIndex: supervisor.NoStopIndex}, uint64(len(text)), h)
	if e = r.append(kFinal, end); e != nil {
		t.Fatal(e)
	}
	clear(end)
	if e = r.file.Sync(); e != nil {
		t.Fatal(e)
	}
	r.complete(nil)
	return r
}
func sha(text string) []byte { sum := sha256.Sum256([]byte(text)); return sum[:] }
func drain(t *testing.T, v supervisor.TextOutput) (string, error) {
	t.Helper()
	var b []byte
	for {
		piece, e := v.Read(context.Background())
		b = append(b, piece...)
		clear(piece)
		if e != nil {
			if e == io.EOF {
				_, e = v.Result(context.Background())
			}
			return string(b), e
		}
	}
}

// Native semantics here are a deliberately explicit test fixture, not a
// permissive production implementation or real model/GPU qualification.
type native struct {
	mu                              sync.Mutex
	pieces                          [][]byte
	index, submits, cancels, cleans int
	check, submitErr, resultErr     error
	result                          supervisor.TextResult
	gate                            chan struct{}
	checkEntered                    chan struct{}
}

func (n *native) CheckInput(context.Context, supervisor.Work, executionlease.Manifest, []byte) error {
	if n.checkEntered != nil {
		close(n.checkEntered)
	}
	if n.gate != nil {
		<-n.gate
	}
	return n.check
}

func TestEncryptedResultStoreCloseWithdrawsPendingEntry(t *testing.T) {
	s := store(t, nil)
	n := fixture()
	n.gate = make(chan struct{})
	n.checkEntered = make(chan struct{})
	a, _ := New(n, s)
	w, m := scope("closing")
	done := make(chan error, 1)
	go func() { _, e := a.SubmitInput(context.Background(), w, m, []byte(`{}`)); done <- e }()
	<-n.checkEntered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := s.Close(ctx); e != nil {
		t.Fatal(e)
	}
	close(n.gate)
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("closed result store started native execution")
		}
	case <-ctx.Done():
		t.Fatal("pending admission did not return")
	}
	if n.submits != 0 {
		t.Fatal("closed admission reached native")
	}
	o, e := a.Query(ctx, w)
	if e != nil || o.State != "ABSENT" {
		t.Fatal("completed pre-entry refusal lost absence proof")
	}
}
func (*native) Submit(context.Context, supervisor.Work) (supervisor.Observation, error) {
	return supervisor.Observation{}, ErrBinding
}
func (n *native) SubmitInput(_ context.Context, w supervisor.Work, _ executionlease.Manifest, _ []byte) (supervisor.Observation, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.submits++
	n.index = 0
	return supervisor.Observation{RequestID: w.RequestID, BackendID: w.RequestID, State: "RUNNING"}, n.submitErr
}
func (n *native) Query(_ context.Context, w supervisor.Work) (supervisor.Observation, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state := "RUNNING"
	if n.index == len(n.pieces) {
		state = "STOPPED"
	}
	if n.resultErr != nil {
		state = "UNKNOWN"
	}
	return supervisor.Observation{RequestID: w.RequestID, BackendID: w.RequestID, State: state}, nil
}
func (n *native) Cancel(context.Context, supervisor.Work) error {
	n.mu.Lock()
	n.cancels++
	n.mu.Unlock()
	return nil
}
func (n *native) Cleanup(_ context.Context, w supervisor.Work, _ supervisor.Observation) (supervisor.Cleaned, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.index != len(n.pieces) || n.resultErr != nil {
		return supervisor.Cleaned{}, ErrUnavailable
	}
	n.cleans++
	return supervisor.Cleaned{RequestID: w.RequestID, BackendID: w.RequestID, LocalGeneration: w.LocalGeneration, WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}, nil
}
func (n *native) Started(context.Context, supervisor.Work) (supervisor.TextStart, error) {
	return supervisor.TextStart{InputTokens: 3}, nil
}
func (n *native) Read(_ context.Context, _ supervisor.Work) ([]byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.index == len(n.pieces) {
		return nil, io.EOF
	}
	b := append([]byte(nil), n.pieces[n.index]...)
	n.index++
	return b, nil
}
func (n *native) Result(supervisor.Work) (supervisor.TextResult, error) { return n.result, n.resultErr }
func fixture() *native {
	return &native{pieces: [][]byte{[]byte("原始结果 private-output")}, result: supervisor.TextResult{InputTokens: 3, OutputTokens: 2, Reason: "eog", StopIndex: supervisor.NoStopIndex}}
}

func TestEncryptedResultRetainsOriginalAcrossCleanupAndNextOwner(t *testing.T) {
	s := store(t, nil)
	n := fixture()
	a, _ := New(n, s)
	w, m := scope("original")
	if _, e := a.SubmitInput(context.Background(), w, m, []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := a.Cleanup(ctx, w, supervisor.Observation{}); e != nil {
		t.Fatal(e)
	}
	next, m2 := scope("next")
	if _, e := a.SubmitInput(ctx, next, m2, []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	v, e := a.Output(ctx, w)
	if e != nil {
		t.Fatal(e)
	}
	if text, e := drain(t, v); e != nil || text != "原始结果 private-output" {
		t.Fatal("original custody lost", e)
	}
	_ = v.Close(ctx)
	wrong := w
	wrong.LocalGeneration = next.LocalGeneration
	if _, e = a.Output(ctx, wrong); e == nil {
		t.Fatal("next generation read original")
	}
	if _, e = a.SubmitInput(ctx, w, m, []byte(`{}`)); e == nil {
		t.Fatal("result replay re-executed")
	}
	n.mu.Lock()
	count := n.submits
	n.mu.Unlock()
	if count != 2 {
		t.Fatal("submit count", count)
	}
}

func TestEncryptedResultRejectsTamperTruncateSwapAndTrailing(t *testing.T) {
	for _, attack := range []string{"ciphertext", "truncate", "swap_header", "sequence", "trailing"} {
		t.Run(attack, func(t *testing.T) {
			s := store(t, nil)
			r := sealed(t, s, "first", "unlogged private text")
			b, e := os.ReadFile(r.path)
			if e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(b, []byte("unlogged private text")) || bytes.Contains(b, r.key) {
				t.Fatal("plaintext/key on disk")
			}
			switch attack {
			case "ciphertext":
				b[len(b)-1] ^= 1
			case "truncate":
				b = b[:len(b)-1]
			case "swap_header":
				other := sealed(t, s, "other", "x")
				copy(b[:headerBytes], other.header[:])
			case "sequence":
				b[headerBytes+4] ^= 1
			case "trailing":
				b = append(b, 0x41)
			}
			if e = os.WriteFile(r.path, b, 0600); e != nil {
				t.Fatal(e)
			}
			clear(b)
			v, e := r.open(context.Background())
			if e != nil {
				return
			}
			defer v.Close(context.Background())
			if _, e = drain(t, v); e == nil {
				t.Fatal("tampered result accepted")
			}
		})
	}
}

func TestEncryptedResultLimitsRetentionAndOneReader(t *testing.T) {
	s := store(t, func(c *Config) {
		c.MaxResults = 1
		c.ResultBytes = 65536
		c.TotalBytes = 65536
		c.Retention = time.Second
	})
	r := sealed(t, s, "first", "hello")
	ctx := context.Background()
	v, e := r.open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.open(ctx); e == nil {
		t.Fatal("parallel reader accepted")
	}
	w, m := scope("other")
	if _, e = s.begin(w, m, options{}, nil); e != ErrCapacity {
		t.Fatal("unreserved quota")
	}
	select {
	case <-r.revoked:
	case <-time.After(3 * time.Second):
		t.Fatal("retention did not revoke")
	}
	if _, e = v.Read(ctx); e == nil {
		t.Fatal("retired reader served data")
	}
	if _, e = s.begin(w, m, options{}, nil); e != ErrCapacity {
		t.Fatal("active reader silently freed quota")
	}
	if e = v.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(r.path); !os.IsNotExist(e) {
		t.Fatal("retired ciphertext survived reader close")
	}
}

func TestEncryptedResultQuotaFailureCancelsAndDrainsBeforeRelease(t *testing.T) {
	s := store(t, func(c *Config) { c.MaxResults = 1; c.ResultBytes = 65536; c.TotalBytes = 65536 })
	n := fixture()
	n.pieces = [][]byte{bytes.Repeat([]byte("x"), 32768), bytes.Repeat([]byte("y"), 32768), []byte("last piece must be drained")}
	a, _ := New(n, s)
	w, m := scope("full")
	_, e := a.SubmitInput(context.Background(), w, m, []byte(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e = a.Cleanup(ctx, w, supervisor.Observation{}); e != nil {
		t.Fatal(e)
	}
	n.mu.Lock()
	read, cancelled := n.index, n.cancels
	n.mu.Unlock()
	if read != 3 || cancelled < 1 {
		t.Fatal("overflow failed to drain/cancel")
	}
	if _, e = a.Output(ctx, w); e == nil {
		t.Fatal("overflow manufactured complete result")
	}
	if e = a.Discard(ctx, w); e != nil {
		t.Fatal(e)
	}
}

func TestEncryptedResultMissingKeyCannotReplayOrStart(t *testing.T) {
	s := store(t, nil)
	r := sealed(t, s, "original", "lost key text")
	if _, e := Open(DefaultConfig(filepath.Dir(r.path))); e == nil {
		t.Fatal("abandoned/active root silently reused")
	}
	fresh, e := Open(DefaultConfig(t.TempDir()))
	if e != nil {
		t.Fatal(e)
	}
	n := fixture()
	a, _ := New(n, fresh)
	defer fresh.Close(context.Background())
	if _, e = a.Output(context.Background(), r.descriptor.Work); e == nil {
		t.Fatal("new process reconstructed capability")
	}
	o, e := a.Query(context.Background(), r.descriptor.Work)
	if e == nil || o.State != "UNKNOWN" || n.submits != 0 {
		t.Fatal("missing state became ABSENT/re-execution")
	}
}

func TestEncryptedResultPreEntryAbsenceAndAmbiguousEntry(t *testing.T) {
	s := store(t, nil)
	n := fixture()
	n.check = errors.New("synthetic input rejection")
	a, _ := New(n, s)
	w, m := scope("rejected")
	if _, e := a.SubmitInput(context.Background(), w, m, []byte(`{}`)); e == nil {
		t.Fatal("rejected check accepted")
	}
	o, e := a.Query(context.Background(), w)
	if e != nil || o.State != "ABSENT" {
		t.Fatal("known pre-entry absence lost")
	}
	if c, e := a.Cleanup(context.Background(), w, o); e != nil || !c.WorkStopped || !c.WorkspaceCleared || n.submits != 0 {
		t.Fatal("pre-entry proof", e)
	}
	n.check = nil
	n.submitErr = ErrUnavailable
	n.resultErr = ErrUnavailable
	w, m = scope("ambiguous")
	if _, e = a.SubmitInput(context.Background(), w, m, []byte(`{}`)); e == nil {
		t.Fatal("ambiguous entry succeeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e = a.Cleanup(ctx, w, supervisor.Observation{}); e == nil {
		t.Fatal("entered ambiguity manufactured cleanup")
	}
	o, _ = a.Query(ctx, w)
	if o.State != "UNKNOWN" || n.submits != 1 {
		t.Fatal("entered error fabricated ABSENT/retry")
	}
}

func TestEncryptedResultCancelledReadCannotForgeDurableEnd(t *testing.T) {
	s := store(t, nil)
	w, m := scope("pending")
	r, e := s.begin(w, m, options{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = r.open(ctx); e == nil {
		t.Fatal("cancelled open admitted")
	}
	v, e := r.open(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e = v.Started(ctx); e == nil {
		t.Fatal("pending STARTED manufactured")
	}
	_ = v.Close(context.Background())
	r.complete(ErrUnavailable)
	if _, e = r.open(context.Background()); e == nil {
		t.Fatal("failed custody readable")
	}
}
