package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

func (c *Core) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.Forward(w, r) }

// Forward writes the frozen native dialect and returns local execution facts.
// The caller must persist/resolve an ambiguous original submission and drive
// real backend stop/query/cleanup. This HTTP transport never releases an owner.
func (c *Core) Forward(w http.ResponseWriter, r *http.Request) (out Outcome) {
	ctx, cancel := context.WithTimeout(r.Context(), c.limits.Total)
	defer cancel()
	controller := http.NewResponseController(w)
	write := func(p []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d := time.Now().Add(c.limits.Write)
		if end, ok := ctx.Deadline(); ok && end.Before(d) {
			d = end
		}
		if e := controller.SetWriteDeadline(d); e != nil {
			return e
		}
		if ctx.Err() != nil {
			_ = controller.SetWriteDeadline(time.Now())
			return ctx.Err()
		}
		_, e := w.Write(p)
		if e != nil {
			return e
		}
		return controller.Flush()
	}
	fail := func(status int, code string) Outcome {
		out.Code = code
		if !out.HeadersSent {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(status)
			out.HeadersSent = true
			_ = write(errorJSON(c.binding.Profile, code))
		} else {
			_ = write(errorSSE(c.binding.Profile, code))
		}
		return out
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return fail(429, "relay_capacity")
	}
	readEnd, _ := ctx.Deadline()
	if controller.SetReadDeadline(readEnd) != nil {
		return fail(503, "bounded_io_required")
	}
	defer controller.SetReadDeadline(time.Time{})
	if r.Method != "POST" || r.URL == nil || r.URL.Path != c.binding.Profile.path() || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.IsAbs() || r.URL.Fragment != "" {
		return fail(400, "unsupported_request")
	}
	if r.ContentLength > int64(c.limits.RequestBytes) {
		return fail(413, "request_too_large")
	}
	if !validHeaders(r, c.binding.Profile) {
		return fail(400, "unsupported_headers")
	}
	cred, e := credential(r, c.binding.Profile)
	if e != nil {
		return fail(401, "invalid_credential")
	}
	if r.Body == nil {
		return fail(400, "unsupported_request")
	}
	stopRead := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer stopRead()
	defer r.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(r.Body, int64(c.limits.RequestBytes)+1))
	if len(raw) > c.limits.RequestBytes {
		return fail(413, "request_too_large")
	}
	if e != nil || ctx.Err() != nil {
		return fail(400, "request_interrupted")
	}
	req, e := validateRequest(raw, c.binding, c.limits)
	if e != nil {
		return fail(400, "unsupported_fields")
	}
	intent := Intent{c.binding, req.hash, r.Header.Get("Idempotency-Key"), req.maxOutput, req.stream}
	g, e := c.authorizer.Admit(ctx, cred, intent)
	if e != nil {
		switch {
		case errors.Is(e, ErrUnauthorized):
			return fail(401, "invalid_credential")
		case errors.Is(e, ErrForbidden):
			return fail(403, "authority_rejected")
		default:
			return fail(503, "admission_unavailable")
		}
	}
	if !grantValid(g, c.binding) {
		return fail(403, "authority_rejected")
	}
	out.InvocationID = g.InvocationID
	grantCtx, grantCancel := context.WithDeadline(ctx, g.Deadline)
	defer grantCancel()
	ctx = grantCtx
	writeStopped := make(chan struct{})
	stopWrite := context.AfterFunc(grantCtx, func() { defer close(writeStopped); _ = controller.SetWriteDeadline(time.Now()) })
	// Never let a cancellation callback touch a ResponseWriter after the HTTP
	// handler has returned and its connection may serve another request.
	defer func() {
		if !stopWrite() {
			<-writeStopped
		}
	}()
	// One bounded watcher per admitted call; it always exits with the call.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-g.Revoked:
			grantCancel()
		case <-ctx.Done():
		case <-watchDone:
		}
	}()
	select {
	case <-g.Revoked:
		return fail(403, "authority_rejected")
	default:
	}
	secret, e := c.secrets.Lookup(ctx, c.binding.SellerID, c.binding.SecretRef)
	if e != nil || !secretValid(secret) {
		return fail(503, "credential_unavailable")
	}
	select {
	case <-g.Revoked:
		return fail(403, "authority_rejected")
	default:
	}
	if ctx.Err() != nil {
		return fail(403, "authority_rejected")
	}
	u := c.endpoint
	up, e := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(req.raw))
	if e != nil {
		return fail(503, "relay_unavailable")
	}
	up.GetBody = nil // Never replay a possibly submitted model POST.
	up.Header.Set("Content-Type", "application/json")
	up.Header.Set("Accept", "application/json")
	if req.stream {
		up.Header.Set("Accept", "text/event-stream")
	}
	if c.binding.Profile == MessagesText {
		up.Header.Set("x-api-key", secret)
		up.Header.Set("anthropic-version", AnthropicVersion)
	} else {
		up.Header.Set("Authorization", "Bearer "+secret)
	}
	out.SubmissionAttempted = true
	resp, e := c.transport.RoundTrip(up)
	if e != nil {
		return fail(502, "upstream_interrupted")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Do not drain an unbounded error body, expose a vendor error, follow a
		// redirect, change seller or retry on 401/429/5xx.
		status := 502
		if resp.StatusCode == 429 {
			status = 429
		}
		return fail(status, "upstream_rejected")
	}
	if resp.Header.Get("Content-Encoding") != "" || headerSize(resp.Header) > 32<<10 {
		return fail(502, "backend_protocol")
	}
	typeName, params, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil {
		return fail(502, "backend_protocol")
	}
	if ch, ok := params["charset"]; ok && !strings.EqualFold(ch, "utf-8") {
		return fail(502, "backend_protocol")
	}
	if (!req.stream && typeName != "application/json") || (req.stream && typeName != "text/event-stream") {
		return fail(502, "backend_protocol")
	}
	// Only valid semantic events refresh idle, never raw bytes or comments.
	idle := time.AfterFunc(c.limits.Idle, grantCancel)
	defer idle.Stop()
	if !req.stream {
		body, e := io.ReadAll(io.LimitReader(resp.Body, int64(c.limits.ResponseBytes)+1))
		if e != nil || len(body) > c.limits.ResponseBytes || ctx.Err() != nil {
			return fail(502, "upstream_interrupted")
		}
		body, kind, usage, e := responseJSON(body, c.binding, c.limits.ResponseBytes)
		if e != nil {
			return fail(502, "backend_protocol")
		}
		out.TerminalValidated = true
		out.Terminal = kind
		out.Usage = usage
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		out.HeadersSent = true
		if e = write(body); e != nil {
			out.Code = "downstream_interrupted"
			return out
		}
		out.Delivered = true
		return out
	}
	reader := newEventReader(resp.Body, c.limits.EventBytes)
	state := streamState{b: c.binding, includeUsage: req.includeUsage}
	for {
		name, data, e := reader.next()
		if e != nil || ctx.Err() != nil {
			return fail(502, "stream_interrupted")
		}
		frame, e := state.event(name, data)
		if e != nil {
			return fail(502, "backend_protocol")
		}
		idle.Reset(c.limits.Idle)
		if !out.HeadersSent {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(200)
			out.HeadersSent = true
		}
		if e = write(frame); e != nil {
			out.Code = "downstream_interrupted"
			return out
		}
		if state.terminal {
			out.TerminalValidated = true
			out.Delivered = true
			out.Terminal = state.terminalKind
			out.Usage = state.usage
			return out
		}
	}
}

func headerSize(h http.Header) int {
	n := 0
	for k, v := range h {
		n += len(k)
		for _, s := range v {
			n += len(s) + 4
		}
	}
	return n
}
func validHeaders(r *http.Request, p Profile) bool {
	if headerSize(r.Header) > 32<<10 || r.ContentLength > 256<<10 || len(r.TransferEncoding) > 1 {
		return false
	}
	for k, values := range r.Header {
		for _, v := range values {
			if strings.ContainsAny(v, "\r\n\x00") {
				return false
			}
		}
		switch strings.ToLower(k) {
		case "authorization", "x-api-key", "content-type", "anthropic-version", "idempotency-key":
			if len(values) != 1 || strings.TrimSpace(values[0]) != values[0] {
				return false
			}
		case "anthropic-beta", "content-encoding", "proxy-authorization", "payment-proof", "amtpaymentlink":
			return false
		}
	}
	t, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || t != "application/json" {
		return false
	}
	if ch, ok := params["charset"]; ok && !strings.EqualFold(ch, "utf-8") {
		return false
	}
	if p == MessagesText {
		if r.Header.Get("anthropic-version") != AnthropicVersion {
			return false
		}
	} else if r.Header.Get("anthropic-version") != "" {
		return false
	}
	key := r.Header.Get("Idempotency-Key")
	if key != "" && !identifier.MatchString(key) {
		return false
	}
	if len(r.TransferEncoding) > 0 && (r.TransferEncoding[0] != "chunked" || r.ContentLength >= 0) {
		return false
	}
	return true
}
func credential(r *http.Request, p Profile) (Credential, error) {
	a, x := r.Header.Get("Authorization"), r.Header.Get("x-api-key")
	if p == MessagesText {
		if a != "" || !secretValid(x) {
			return Credential{}, ErrUnauthorized
		}
		return Credential{"x-api-key", x}, nil
	}
	if x != "" || !strings.HasPrefix(a, "Bearer ") || !secretValid(strings.TrimPrefix(a, "Bearer ")) {
		return Credential{}, ErrUnauthorized
	}
	return Credential{"bearer", strings.TrimPrefix(a, "Bearer ")}, nil
}
func errorJSON(p Profile, code string) []byte {
	message := "Request could not be completed (" + code + ")."
	if p == MessagesText {
		t := "api_error"
		if strings.HasPrefix(code, "unsupported") || code == "request_too_large" {
			t = "invalid_request_error"
		}
		if code == "invalid_credential" {
			t = "authentication_error"
		}
		if code == "authority_rejected" {
			t = "permission_error"
		}
		if code == "relay_capacity" {
			t = "rate_limit_error"
		}
		return encode(map[string]any{"type": "error", "error": map[string]string{"type": t, "message": message}})
	}
	typeName := "server_error"
	if strings.HasPrefix(code, "unsupported") || code == "request_too_large" {
		typeName = "invalid_request_error"
	}
	return encode(map[string]any{"error": map[string]any{"message": message, "type": typeName, "param": nil, "code": code}})
}
func errorSSE(p Profile, code string) []byte {
	if p == MessagesText {
		return []byte("event: error\ndata: " + string(errorJSON(p, code)) + "\n\n")
	}
	if p == ResponsesText {
		return []byte("event: error\ndata: " + string(encode(map[string]any{"type": "error", "code": code, "message": "Stream interrupted.", "param": nil})) + "\n\n")
	}
	return []byte("data: " + string(errorJSON(p, code)) + "\n\n")
}
