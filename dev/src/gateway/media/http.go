package media

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Outcome records local protocol write facts, never buyer ACK, usage or money.
type Outcome struct {
	InvocationID, Code        string
	ResponseStarted, Complete bool
}
type wire struct {
	writer      http.ResponseWriter
	controller  *http.ResponseController
	ctx         context.Context
	timeout     time.Duration
	started     bool
	status      int
	contentType string
}

func (w *wire) write(b []byte) error {
	if w.ctx.Err() != nil {
		return ErrAccess
	}
	deadline := time.Now().Add(w.timeout)
	if end, ok := w.ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if w.controller.SetWriteDeadline(deadline) != nil {
		return ErrAccess
	}
	if !w.started {
		w.writer.Header().Set("Content-Type", w.contentType)
		w.writer.Header().Set("Cache-Control", "no-store")
		w.writer.Header().Set("X-Content-Type-Options", "nosniff")
		w.writer.WriteHeader(w.status)
		w.started = true
	}
	n, e := w.writer.Write(b)
	if e != nil || n != len(b) {
		return ErrAccess
	}
	return nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out := h.Forward(w, r)
	if !out.Complete {
		panic(http.ErrAbortHandler)
	}
}
func (h *Handler) Forward(writer http.ResponseWriter, r *http.Request) (out Outcome) {
	if h == nil || writer == nil || r == nil {
		return Outcome{Code: "gateway_unavailable"}
	}
	wire := &wire{writer: writer, controller: http.NewResponseController(writer), timeout: h.config.Write, status: 200, contentType: "application/json"}
	ctx, cancel := context.WithTimeout(r.Context(), h.config.Total)
	wire.ctx = ctx
	defer cancel()
	fail := func(status int, code string) Outcome {
		out.Code = code
		if !wire.started {
			wire.status = status
			wire.contentType = "application/json"
			writer.Header().Del("Content-Length")
			writer.Header().Del("Content-Range")
			writer.Header().Del("Accept-Ranges")
			out.Complete = h.json(wire, map[string]any{"error": map[string]any{"message": "Original media request unavailable", "type": "invalid_request_error", "param": nil, "code": code}}) == nil
		}
		out.ResponseStarted = wire.started
		return out
	}
	if !h.enter() {
		return fail(503, "original_gateway_closed")
	}
	defer h.operations.Done()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		return fail(429, "original_gateway_capacity")
	}
	stopLife := context.AfterFunc(h.life, cancel)
	defer stopLife()
	if !h.validRequest(r) {
		return fail(400, "unsupported_request")
	}
	action, public := h.action(r)
	if action == "" {
		return fail(404, "unsupported_endpoint")
	}
	credential, e := bearer(r)
	if e != nil {
		return fail(401, "invalid_credential")
	}
	scope := "QUEUE_SELF"
	if action == "create" {
		scope = "INVOKE"
	}
	if action == "content" {
		scope = "EXPORT_SELF"
	}
	if action == "cancel" {
		scope = "CANCEL_SELF"
	}
	if h.config.Session.Check(ctx, credential, scope) != nil {
		return fail(403, "original_scope_unavailable")
	}
	if action == "create" && (h.config.Mapping.Profile == Images || h.config.Mapping.Profile == Speech) && h.config.Session.Check(ctx, credential, "EXPORT_SELF") != nil {
		return fail(403, "original_export_scope_unavailable")
	}
	if (action == "cancel" || action == "create" && (h.config.Mapping.Profile == Videos || h.config.Mapping.Profile == Assets)) && h.config.Session.Check(ctx, credential, "QUEUE_SELF") != nil {
		return fail(403, "original_query_scope_unavailable")
	}
	if action == "models" {
		out.Complete = h.json(wire, map[string]any{"object": "list", "data": []any{map[string]any{"id": h.config.Mapping.Model, "object": "model", "created": 0, "owned_by": h.config.Prepared.Details().Seller}}}) == nil
		out.ResponseStarted = wire.started
		return out
	}
	end, _ := ctx.Deadline()
	if wire.controller.SetReadDeadline(end) != nil {
		return fail(503, "bounded_transport_required")
	}
	// Join the cancellation callback before the response writer/body can be
	// reused. Close must interrupt the original native HTTP body read.
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() {
			if r.Body != nil {
				_ = r.Body.Close()
			}
		})
	}
	joined := make(chan struct{})
	stopIO := context.AfterFunc(ctx, func() {
		defer close(joined)
		_ = wire.controller.SetReadDeadline(time.Now())
		_ = wire.controller.SetWriteDeadline(time.Now())
		closeBody()
	})
	defer func() {
		if !stopIO() {
			<-joined
		}
		closeBody()
		_ = wire.controller.SetReadDeadline(time.Time{})
		_ = wire.controller.SetWriteDeadline(time.Time{})
	}()
	var item *entry
	if action == "create" {
		select {
		case h.ingress <- struct{}{}:
			defer func() { <-h.ingress }()
		default:
			return fail(429, "original_input_capacity")
		}
		if r.Body == nil || r.ContentLength > MaximumRequestBytes {
			return fail(413, "request_too_large")
		}
		req, err := parseRequest(ctx, r, h.config.Mapping)
		if err != nil {
			return fail(400, "unsupported_media_fields")
		}
		defer req.clear()
		if h.config.Mapping.Profile == Images && (req.format == "url" && h.config.PublicBaseURL == "" || req.format == "b64_json" && (h.config.Policy.MaximumOutputBytes+2)/3*4+256 > MaximumJSONBytes) {
			return fail(400, "response_format_exceeds_approved_profile")
		}
		var replay bool
		item, replay, err = h.reserve(ctx, req, r.Header.Get("Idempotency-Key"))
		if err != nil {
			if errors.Is(err, ErrConflict) {
				return fail(409, "original_request_conflict")
			}
			return fail(429, "original_request_capacity")
		}
		out.InvocationID = item.id
		if !replay {
			if h.submit(ctx, credential, req, item) != nil {
				return fail(503, "original_submission_unknown")
			}
		}
		if h.config.Mapping.Profile == Images || h.config.Mapping.Profile == Speech {
			if h.await(ctx, credential, item) != nil {
				item.mu.Lock()
				item.failed = true
				h.dropCache(item)
				item.mu.Unlock()
				stop, end := context.WithTimeout(context.Background(), 2*time.Second)
				_ = h.config.Session.Cancel(stop, credential, item.id)
				end()
				return fail(503, "original_result_unavailable")
			}
			item.mu.Lock()
			defer item.mu.Unlock()
			if h.config.Mapping.Profile == Images {
				e = h.image(wire, credential, item, req.format)
			} else {
				e = h.content(wire, credential, item, "")
			}
		} else {
			e = h.resource(wire, credential, item)
		}
	} else {
		prefix := "file_"
		if action != "content" || h.config.Mapping.Profile == Videos {
			prefix = "resp_"
			if h.config.Mapping.Profile == Videos {
				prefix = "video_"
			}
		}
		item, e = h.lookup(public, prefix)
		if e != nil {
			return fail(404, "original_resource_unavailable")
		}
		out.InvocationID = item.id
		if action == "cancel" {
			if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
				return fail(400, "unsupported_cancel_body")
			}
			// This local withdrawal is independent of a potentially held cache
			// reader mutex. The original cancel remains uncertain until observed.
			item.cancelled.Store(true)
			if h.config.Session.Cancel(ctx, credential, item.id) != nil {
				return fail(503, "original_cancel_unknown")
			}
		}
		if action == "content" {
			if h.await(ctx, credential, item) != nil {
				return fail(409, "original_result_not_available")
			}
			item.mu.Lock()
			defer item.mu.Unlock()
			e = h.content(wire, credential, item, r.Header.Get("Range"))
		} else {
			e = h.resource(wire, credential, item)
		}
	}
	if e != nil {
		return fail(503, "original_response_interrupted")
	}
	out.ResponseStarted = wire.started
	out.Complete = true
	return out
}
func bearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", ErrAccess
	}
	value := strings.TrimPrefix(values[0], "Bearer ")
	if len(value) < 16 || len(value) > 8192 || strings.ContainsAny(value, " \r\n\t\x00") {
		return "", ErrAccess
	}
	return value, nil
}
func (h *Handler) validRequest(r *http.Request) bool {
	if r.URL == nil || r.URL.IsAbs() || r.URL.RawPath != "" || r.URL.Fragment != "" || r.URL.ForceQuery || !slices.Contains(h.config.AllowedHosts, strings.ToLower(r.Host)) || strings.Contains(r.RequestURI, "\\") {
		return false
	}
	if r.Method != "POST" && r.Method != "GET" {
		return false
	}
	size := 0
	seen := map[string]bool{}
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if seen[lower] {
			return false
		}
		seen[lower] = true
		for _, v := range values {
			size += len(name) + len(v)
			if strings.ContainsAny(v, "\r\n\x00") {
				return false
			}
		}
		if size > 16384 {
			return false
		}
		if slices.Contains([]string{"authorization", "content-type", "content-length", "origin", "range", "idempotency-key", "accept"}, lower) && len(values) != 1 {
			return false
		}
		if slices.Contains([]string{"cookie", "content-encoding", "transfer-encoding", "x-api-key", "x-forwarded-host", "x-forwarded-for", "forwarded", "if-range", "if-none-match"}, lower) {
			return false
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" && !slices.Contains(h.config.AllowedOrigins, origin) {
		return false
	}
	if key := r.Header.Get("Idempotency-Key"); key != "" && !keyPattern.MatchString(key) {
		return false
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return false
	}
	if r.Method == "GET" && (r.ContentLength != 0 || len(r.TransferEncoding) != 0) {
		return false
	}
	if r.Header.Get("Range") != "" && (r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/content")) {
		return false
	}
	return true
}
func (h *Handler) action(r *http.Request) (string, string) {
	path := r.URL.Path
	if r.URL.RawQuery != "" {
		if h.config.Mapping.Profile != Videos || r.Method != "GET" || !strings.HasSuffix(path, "/content") || r.URL.RawQuery != "variant=video" {
			return "", ""
		}
	}
	if r.Method == "GET" && path == "/v1/models" {
		return "models", ""
	}
	if r.Method == "POST" {
		switch h.config.Mapping.Profile {
		case Images:
			if path == "/v1/images/generations" || path == "/v1/images/edits" {
				return "create", ""
			}
		case Videos:
			if path == "/v1/videos" {
				return "create", ""
			}
		case Speech:
			if path == "/v1/audio/speech" {
				return "create", ""
			}
		case Assets:
			if path == "/v1/responses" {
				return "create", ""
			}
		}
	}
	if r.Method == "GET" && strings.HasPrefix(path, "/v1/files/") && strings.HasSuffix(path, "/content") && h.config.Mapping.Profile != Videos {
		return "content", strings.TrimSuffix(strings.TrimPrefix(path, "/v1/files/"), "/content")
	}
	root := "/v1/responses/"
	if h.config.Mapping.Profile == Videos {
		root = "/v1/videos/"
	}
	if h.config.Mapping.Profile != Assets && h.config.Mapping.Profile != Videos || !strings.HasPrefix(path, root) {
		return "", ""
	}
	id := strings.TrimPrefix(path, root)
	if r.Method == "GET" && strings.HasSuffix(id, "/content") && h.config.Mapping.Profile == Videos {
		return "content", strings.TrimSuffix(id, "/content")
	}
	if r.Method == "POST" && strings.HasSuffix(id, "/cancel") && h.config.Mapping.Profile == Assets {
		return "cancel", strings.TrimSuffix(id, "/cancel")
	}
	if r.Method == "GET" && !strings.Contains(id, "/") {
		return "query", id
	}
	return "", ""
}
