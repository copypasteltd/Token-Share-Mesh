package sdkhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"p11.local/compute/client/protocol/textoutput"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/resultwire"
)

// originalOutput streams authenticated bounded chunks into the existing
// renderer in RAM. It exposes no execution method and creates no disk files.
type originalOutput struct {
	server    *Server
	principal identity.Principal
	state     invocation.SDKState
	next      int
	bytes     int64
	closed    bool
	sum       interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}
}

func (o *originalOutput) Descriptor() supervisor.TextDescriptor {
	h := o.state.Head
	at, _ := time.Parse(time.RFC3339Nano, h.Declaration.Completed)
	return supervisor.TextDescriptor{Manifest: h.Manifest, ResultID: h.Declaration.Result, CreatedUnix: at.Unix(), IncludeUsage: h.IncludeUsage}
}
func (o *originalOutput) Started(ctx context.Context) (supervisor.TextStart, error) {
	if ctx.Err() != nil || o.closed {
		return supervisor.TextStart{}, textoutput.ErrOutput
	}
	return supervisor.TextStart{InputTokens: uint32(o.state.Head.Declaration.InputTokens)}, nil
}
func (o *originalOutput) Read(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil || o.closed {
		return nil, textoutput.ErrOutput
	}
	h := o.state.Head
	if o.next == h.Chunks {
		if o.bytes != h.Declaration.OutputBytes || hex.EncodeToString(o.sum.Sum(nil)) != h.Declaration.OutputHash {
			return nil, textoutput.ErrOutput
		}
		return nil, io.EOF
	}
	c, err := o.server.c.Calls.Chunk(ctx, o.principal, o.state.Job, h.Delivery, h.Digest(), o.next)
	left := h.Declaration.OutputBytes - int64(o.next)*resultwire.ChunkBytes
	expected := int64(resultwire.ChunkBytes)
	if left < expected {
		expected = left
	}
	if err != nil || c.Validate() != nil || c.Scope != h.Declaration.Scope || c.Delivery != h.Delivery || c.Head != h.Digest() || c.Index != o.next || int64(len(c.Content)) != expected {
		clear(c.Content)
		return nil, textoutput.ErrOutput
	}
	o.next++
	o.bytes += int64(len(c.Content))
	_, _ = o.sum.Write(c.Content)
	return c.Content, nil
}
func (o *originalOutput) Result(ctx context.Context) (supervisor.TextResult, error) {
	d := o.state.Head.Declaration
	if ctx.Err() != nil || o.closed || o.next != o.state.Head.Chunks || o.bytes != d.OutputBytes || hex.EncodeToString(o.sum.Sum(nil)) != d.OutputHash {
		return supervisor.TextResult{}, textoutput.ErrOutput
	}
	v := supervisor.TextResult{InputTokens: uint32(d.InputTokens), OutputTokens: uint32(d.OutputTokens), Reason: d.Finish, StopIndex: supervisor.NoStopIndex}
	if d.Finish == "stop" {
		v.StopIndex = 0
	}
	return v, nil
}
func (o *originalOutput) StopSequence(ctx context.Context, index uint32) ([]byte, error) {
	if ctx.Err() != nil || o.closed || index != 0 || o.state.Head.Declaration.Finish != "stop" || o.state.Head.End == nil {
		return nil, textoutput.ErrOutput
	}
	return []byte(o.state.Head.End.Stop), nil
}
func (o *originalOutput) Close(context.Context) error {
	o.closed = true
	o.principal = identity.Principal{}
	return nil
}

type outputBuffer struct{ data []byte }

func (b *outputBuffer) Write(ctx context.Context, p []byte) error {
	if ctx.Err() != nil || len(b.data)+len(p) > ResponseBytes {
		return textoutput.ErrLimit
	}
	b.data = append(b.data, p...)
	return nil
}

func (s *Server) render(ctx context.Context, p identity.Principal, state invocation.SDKState) ([]byte, error) {
	h := state.Head
	if h == nil || h.Validate() != nil || h.State != "READY" || h.End == nil || h.Manifest.Spec.Stream ||
		h.Declaration.OutputBytes > TextBytes || h.Manifest.Job != state.Job || h.Manifest.Order != state.Order ||
		h.Manifest.Route != state.Route || h.Manifest.Seller != state.Seller || h.Manifest.Spec != state.Spec {
		return nil, fault.New(fault.Unavailable, "Original bounded SDK content unavailable")
	}
	expires, err := time.Parse(time.RFC3339Nano, h.Expires)
	if err != nil || !expires.After(time.Now()) {
		return nil, fault.New(fault.NotFound, "Original result expired")
	}
	readCtx, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	out := &originalOutput{server: s, principal: p, state: state, sum: sha256.New()}
	buffer := &outputBuffer{}
	_, err = textoutput.Render(readCtx, buffer, out, textoutput.Limits{TextBytes: TextBytes, ResponseBytes: ResponseBytes, EventBytes: 64 << 10})
	if err != nil {
		clear(buffer.data)
		return nil, fault.New(fault.Unavailable, "Original SDK rendering unavailable")
	}
	return buffer.data, nil
}
