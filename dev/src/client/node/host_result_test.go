package node

import (
	"context"
	"io"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	"sync/atomic"
	"testing"
	"time"
)

type custodyBackend struct {
	contentBackend
	read     atomic.Bool
	manifest string
}

func (*custodyBackend) CheckInput(context.Context, supervisor.Work, executionlease.Manifest, []byte) error {
	return nil
}
func (b *custodyBackend) SubmitInput(ctx context.Context, w supervisor.Work, m executionlease.Manifest, body []byte) (supervisor.Observation, error) {
	b.manifest = m.Digest()
	return b.contentBackend.SubmitInput(ctx, w, m, body)
}
func (*custodyBackend) Started(context.Context, supervisor.Work) (supervisor.TextStart, error) {
	return supervisor.TextStart{InputTokens: 3}, nil
}
func (b *custodyBackend) Read(context.Context, supervisor.Work) ([]byte, error) {
	if !b.read.Swap(true) {
		return []byte("original text custody"), nil
	}
	b.running.Store(false)
	return nil, io.EOF
}
func (*custodyBackend) Result(supervisor.Work) (supervisor.TextResult, error) {
	return supervisor.TextResult{InputTokens: 3, OutputTokens: 2, Reason: "eog", StopIndex: supervisor.NoStopIndex}, nil
}
func (b *custodyBackend) Cleanup(ctx context.Context, w supervisor.Work, o supervisor.Observation) (supervisor.Cleaned, error) {
	c, e := b.nodeBackend.Cleanup(ctx, w, o)
	c.ManifestHash = b.manifest
	return c, e
}

func TestHostResultCapabilityReadsOriginalAfterComputeCleanup(t *testing.T) {
	// Signed HTTP delivery, both SQLite journals and supervisor are real;
	// inference/authority are explicit synthetic fixtures, not LP/GPU proof.
	f := newHostFixture(t)
	ctx := context.Background()
	f.begin(t, "command_result")
	p, e := f.host.Prepare(ctx, "command_result")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	spool, e := results.Open(results.DefaultConfig(t.TempDir()))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if e := spool.Close(closeCtx); e != nil {
			t.Error(e)
		}
	}()
	n := &custodyBackend{}
	adapter, e := results.New(n, spool)
	if e != nil {
		t.Fatal(e)
	}
	a := f.config.Rules[0].Approval
	watch, e := f.host.NewSupervisor(ctx, []HostKernel{{Environment: a.EnvironmentDigest, Capability: a.Capability, EngineKind: a.EngineKind, EngineRelease: a.EngineRelease, ModelDigest: a.ModelDigest, RecipeDigest: a.RecipeDigest, Backend: adapter}}, supervisor.Options{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Output(ctx); e == nil {
		t.Fatal("unstarted ticket read a result")
	}
	cancel, done := runHostWatch(t, watch)
	if _, e = p.Start(ctx, watch); e != nil {
		t.Fatal(e)
	}
	// Let shutdown drive actual query/stop/cleanup; ciphertext custody survives.
	stopHostWatch(t, cancel, done, nil)
	if e = f.host.ReconcileCleanup(ctx, "command_result"); e != nil {
		t.Fatal(e)
	}
	out, e := p.Output(ctx)
	if e != nil {
		t.Fatal("cleaned compute lost result", e)
	}
	var text []byte
	for {
		b, err := out.Read(ctx)
		text = append(text, b...)
		clear(b)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(text) != "original text custody" || n.inputCalls.Load() != 1 {
		t.Fatal("wrong result or re-execution")
	}
	clear(text)
	if _, e = out.Result(ctx); e != nil {
		t.Fatal(e)
	}
	_ = out.Close(ctx)
	if _, e = p.Start(ctx, watch); e == nil {
		t.Fatal("result access enabled replay")
	}
	p.Close()
	if _, e = p.Output(ctx); e == nil {
		t.Fatal("closed ticket retained public access")
	}
}
