package media

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) resource(w *wire, credential string, e *entry) error {
	state, err := h.state(w.ctx, credential, e)
	if err != nil {
		// A denied or uncertain query cannot destroy a result or authorize a
		// replacement invocation. Keep this exact entry query-only.
		return ErrAccess
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if state.Stopped && state.Succeeded && !e.cancelled.Load() && !e.failed && e.cache == nil && e.completed.IsZero() {
		if h.config.Session.Check(w.ctx, credential, "EXPORT_SELF") != nil {
			return ErrAccess
		}
		if h.cache(w.ctx, credential, e) != nil {
			e.failed = true
		}
	}
	status := "in_progress"
	if state.NativeState == "SUBMITTING" {
		status = "queued"
	}
	complete := e.cache != nil && !e.failed && !e.cancelled.Load() && time.Now().Before(e.expires)
	var failure any
	if complete {
		status = "completed"
	} else if e.failed || e.cancelled.Load() && state.Stopped || state.Stopped && !state.Succeeded || !e.completed.IsZero() {
		status = "failed"
		h.dropCache(e)
		failure = map[string]any{"code": "original_media_unavailable", "message": "Original media result unavailable"}
	}
	var completed, expires any
	if complete {
		completed = e.completed.Unix()
		expires = e.expires.Unix()
	}
	if h.config.Mapping.Profile == Videos {
		progress := 0
		if complete {
			progress = 100
		}
		return h.json(w, map[string]any{"id": e.public, "object": "video", "model": h.config.Mapping.Model, "status": status, "progress": progress, "created_at": e.created.Unix(), "completed_at": completed, "expires_at": expires, "size": e.size, "seconds": e.seconds, "prompt": nil, "error": failure, "remixed_from_video_id": nil})
	}
	if e.cancelled.Load() && state.Stopped {
		status = "cancelled"
	}
	output := []any{}
	if complete {
		asset, _ := json.Marshal(map[string]any{"file_id": e.file, "mime_type": mimeType(h.config.Policy.Kind), "bytes": e.bytes})
		output = append(output, map[string]any{"id": "msg_" + strings.TrimPrefix(e.id, "invoke_"), "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": string(asset), "annotations": []any{}}}})
		clear(asset)
	}
	return h.json(w, map[string]any{"id": e.public, "object": "response", "model": h.config.Mapping.Model, "created_at": e.created.Unix(), "completed_at": completed, "status": status, "background": true, "store": true, "error": failure, "incomplete_details": nil, "output": output, "usage": nil, "metadata": map[string]any{}})
}

type wireWriter struct{ wire *wire }

func (w wireWriter) Write(b []byte) (int, error) {
	if e := w.wire.write(b); e != nil {
		return 0, e
	}
	return len(b), nil
}
func (h *Handler) image(w *wire, credential string, e *entry, format string) error {
	if e.cache == nil || e.failed || e.cancelled.Load() || !time.Now().Before(e.expires) {
		return ErrAccess
	}
	if format == "url" {
		return h.json(w, map[string]any{"created": e.created.Unix(), "data": []any{map[string]any{"url": h.config.PublicBaseURL + "/v1/files/" + e.file + "/content"}}})
	}
	if (e.bytes+2)/3*4+256 > MaximumJSONBytes {
		return ErrRequest
	}
	prefix := []byte(`{"created":` + strconv.FormatInt(e.created.Unix(), 10) + `,"data":[{"b64_json":"`)
	if w.write(prefix) != nil {
		return ErrAccess
	}
	encoder := base64.NewEncoder(base64.StdEncoding, wireWriter{w})
	if h.copyCache(w, credential, e, 0, e.bytes, func(b []byte) error {
		n, err := encoder.Write(b)
		if err != nil || n != len(b) {
			return ErrAccess
		}
		return nil
	}) != nil {
		return ErrAccess
	}
	if encoder.Close() != nil || e.cancelled.Load() || h.config.Session.Check(w.ctx, credential, "EXPORT_SELF") != nil {
		return ErrAccess
	}
	return w.write([]byte(`"}]}`))
}
func (h *Handler) copyCache(w *wire, credential string, e *entry, start, length int64, write func([]byte) error) error {
	if e.cache == nil {
		return ErrAccess
	}
	scratch := make([]byte, 65536)
	defer clear(scratch)
	end := start + length
	for offset := start; offset < end; {
		if e.cancelled.Load() || !time.Now().Before(e.expires) || h.config.Session.Check(w.ctx, credential, "EXPORT_SELF") != nil {
			return ErrAccess
		}
		n, err := e.cache.readAt(w.ctx, scratch[:min(int64(len(scratch)), end-offset)], offset)
		if err != nil || n < 1 {
			return ErrAccess
		}
		if write(scratch[:n]) != nil {
			return ErrAccess
		}
		clear(scratch[:n])
		offset += int64(n)
	}
	return nil
}
func (h *Handler) content(w *wire, credential string, e *entry, rangeValue string) error {
	if e.cache == nil || e.failed || e.cancelled.Load() || !time.Now().Before(e.expires) {
		return ErrAccess
	}
	start, end, partial, err := byteRange(rangeValue, e.bytes)
	if err != nil {
		w.status = 416
		w.writer.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(e.bytes, 10))
		return h.json(w, map[string]any{"error": map[string]any{"type": "invalid_request_error", "code": "unsupported_range", "message": "Requested range unavailable", "param": nil}})
	}
	w.contentType = mimeType(h.config.Policy.Kind)
	w.writer.Header().Set("Accept-Ranges", "bytes")
	if partial {
		w.status = http.StatusPartialContent
		w.writer.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(e.bytes, 10))
	}
	w.writer.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	return h.copyCache(w, credential, e, start, end-start+1, w.write)
}
func byteRange(value string, size int64) (int64, int64, bool, error) {
	if value == "" {
		return 0, size - 1, false, nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false, ErrRequest
	}
	pieces := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(pieces) != 2 {
		return 0, 0, false, ErrRequest
	}
	parse := func(v string) (int64, error) {
		if v == "" || strings.ContainsAny(v, " +\t\r\n") {
			return 0, ErrRequest
		}
		return strconv.ParseInt(v, 10, 64)
	}
	if pieces[0] == "" {
		suffix, e := parse(pieces[1])
		if e != nil || suffix < 1 {
			return 0, 0, false, ErrRequest
		}
		return max(0, size-suffix), size - 1, true, nil
	}
	start, e := parse(pieces[0])
	if e != nil || start < 0 || start >= size {
		return 0, 0, false, ErrRequest
	}
	end := size - 1
	if pieces[1] != "" {
		end, e = parse(pieces[1])
		if e != nil || end < start {
			return 0, 0, false, ErrRequest
		}
		end = min(end, size-1)
	}
	return start, end, true, nil
}

var _ io.Writer = wireWriter{}
