package comfyworker

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

type Kind byte

const (
	Initialize  Kind = 1
	Invoke      Kind = 2
	AssetBegin  Kind = 3
	AssetData   Kind = 4
	AssetEnd    Kind = 5
	Shutdown    Kind = 6
	Ready       Kind = 101
	Accepted    Kind = 102
	State       Kind = 103
	Result      Kind = 104
	Stopped     Kind = 105
	Closed      Kind = 106
	Failure     Kind = 107
	Cancel      Kind = 201
	StopSession Kind = 202
)

// Frames are private engine encoding, not public OpenAI/Anthropic endpoints,
// native rental grants, BINARY_STREAM_1 or any buyer acceptance evidence.
// Body is single-use shared state so fmt/json cannot expose an input or key.
type frameState struct {
	mu    sync.Mutex
	kind  Kind
	body  []byte
	taken bool
}
type Frame struct{ state *frameState }

func (f Frame) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private ComfyUI frame redacted]")
}
func (f Frame) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (f Frame) Kind() Kind {
	if f.state == nil {
		return 0
	}
	return f.state.kind
}
func (f Frame) Take() ([]byte, error) {
	if f.state == nil {
		return nil, ErrOutput
	}
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	if f.state.taken {
		return nil, ErrOutput
	}
	f.state.taken = true
	b := f.state.body
	f.state.body = nil
	return b, nil
}
func (f Frame) Close() {
	if f.state == nil {
		return
	}
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	f.state.taken = true
	clear(f.state.body)
	f.state.body = nil
}

func inputLimit(k Kind) int {
	switch k {
	case Initialize:
		return 32 << 20
	case Invoke:
		return 2 << 20
	case AssetBegin, AssetEnd, Shutdown:
		return 4096
	case AssetData:
		return chunkBytes
	case Cancel, StopSession:
		return 4096
	}
	return 0
}
func readFull(ctx context.Context, r io.Reader, b []byte) error {
	for len(b) > 0 {
		if ctx.Err() != nil {
			return ErrOutput
		}
		n, e := r.Read(b)
		if n < 0 || n > len(b) {
			return ErrOutput
		}
		b = b[n:]
		if e != nil && len(b) > 0 {
			return ErrOutput
		}
		if n == 0 && e == nil {
			return ErrOutput
		}
	}
	return nil
}

// ReadWorkerFrame bounds allocation BEFORE reading an owned worker's payload.
// The process owner must close/cancel the underlying pipe to interrupt a
// blocked OS read; context alone is not a process/pipe join guarantee.
func ReadWorkerFrame(ctx context.Context, r io.Reader) (Frame, error) {
	if ctx == nil || r == nil {
		return Frame{}, ErrOutput
	}
	var h [5]byte
	if readFull(ctx, r, h[:]) != nil {
		return Frame{}, ErrOutput
	}
	n := binary.LittleEndian.Uint32(h[:4])
	kind := Kind(h[4])
	if n < 1 || n > 65537 || kind < Ready || kind > Failure {
		return Frame{}, ErrOutput
	}
	b := make([]byte, int(n)-1)
	if readFull(ctx, r, b) != nil {
		clear(b)
		return Frame{}, ErrOutput
	}
	return Frame{&frameState{kind: kind, body: b}}, nil
}

// WritePrivateFrame is used by the owned content writer OR a separate owned
// control writer, never a common mutex that lets content block cancellation.
// The caller supplies independently authorized metadata/body and keeps its
// process/pipe context until the actual writer has returned.
func WritePrivateFrame(ctx context.Context, w io.Writer, kind Kind, body []byte) error {
	limit := inputLimit(kind)
	if ctx == nil || w == nil || limit == 0 || len(body) > limit || len(body) == 0 {
		return ErrOutput
	}
	var h [5]byte
	binary.LittleEndian.PutUint32(h[:4], uint32(len(body)+1))
	h[4] = byte(kind)
	for _, part := range [][]byte{h[:], body} {
		for len(part) > 0 {
			if ctx.Err() != nil {
				return ErrOutput
			}
			n, e := w.Write(part)
			if e != nil || n <= 0 || n > len(part) {
				return ErrOutput
			}
			part = part[n:]
		}
	}
	return nil
}
