package textoutput

import (
	"context"
	"net/http"
	"time"
)

type httpSink struct {
	writer     http.ResponseWriter
	controller *http.ResponseController
	timeout    time.Duration
}

// NewHTTPSink wraps a caller's ALREADY AUTHORIZED response writer. It opens no
// server. The caller sets status/content-type after its own admission. Native
// HTTP transports must support deadlines; unsupported middleware fails closed.
func NewHTTPSink(w http.ResponseWriter, timeout time.Duration) (Sink, error) {
	if w == nil || timeout < 10*time.Millisecond || timeout > 30*time.Second {
		return nil, ErrOutput
	}
	return &httpSink{w, http.NewResponseController(w), timeout}, nil
}
func (s *httpSink) Write(ctx context.Context, b []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	deadline := time.Now().Add(s.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if s.controller.SetWriteDeadline(deadline) != nil {
		return ErrOutput
	}
	stop := context.AfterFunc(ctx, func() { _ = s.controller.SetWriteDeadline(time.Now()) })
	defer stop()
	n, e := s.writer.Write(b)
	if e != nil || n != len(b) {
		return ErrOutput
	}
	if s.controller.Flush() != nil {
		return ErrOutput
	}
	return ctx.Err()
}
