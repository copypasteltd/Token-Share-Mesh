package node

import (
	"context"
	"sync"

	"p11.local/compute/client/supervisor"
)

// nativeOutput keeps the original result capability in its owning session's
// lifetime. It does not carry execution authority, keys or another content copy.
type nativeOutput struct {
	supervisor.TextOutput
	session *NativeSession
	once    sync.Once
	done    chan struct{}
	err     error
}

func (s *NativeSession) ownOutput(ctx context.Context, raw supervisor.TextOutput) (supervisor.TextOutput, error) {
	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		_ = raw.Close(ctx)
		return nil, ErrHostPending
	}
	if s.outputs == nil {
		s.outputs = map[*nativeOutput]bool{}
	}
	o := &nativeOutput{TextOutput: raw, session: s, done: make(chan struct{})}
	s.outputs[o] = true
	s.mu.Unlock()
	return o, nil
}

// The underlying local view serializes Read/Close. Revocation is performed by
// the result store before session drain, waking waiting reads. Waiting here is
// bounded by the original caller's deadline; timeout never claims completion.
func (o *nativeOutput) Close(ctx context.Context) error {
	o.once.Do(func() {
		go func() {
			o.err = o.TextOutput.Close(ctx)
			o.session.mu.Lock()
			delete(o.session.outputs, o)
			o.session.mu.Unlock()
			close(o.done)
		}()
	})
	select {
	case <-o.done:
		return o.err
	case <-ctx.Done():
		return ErrHostPending
	}
}

func (s *NativeSession) closeOutputs(ctx context.Context) error {
	s.mu.Lock()
	all := make([]*nativeOutput, 0, len(s.outputs))
	for o := range s.outputs {
		all = append(all, o)
	}
	s.mu.Unlock()
	var err error
	for _, o := range all {
		if o.Close(ctx) != nil {
			err = ErrHostPending
		}
	}
	return err
}
