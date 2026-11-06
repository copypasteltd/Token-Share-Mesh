package controlhttp

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/resultwire"
)

type clearingControlBody struct {
	*bytes.Reader
	raw []byte
}

func (b *clearingControlBody) Close() error { clear(b.raw); b.raw = nil; return nil }
func originalContentTransfer(r *http.Request) bool {
	if r.Method == "GET" {
		return strings.HasPrefix(r.URL.Path, prefix+"/result-content/")
	}
	if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, prefix+"/runner/attempts/") {
		return false
	}
	return strings.HasSuffix(r.URL.Path, "/content/begin") || strings.HasSuffix(r.URL.Path, "/content/chunks") || strings.HasSuffix(r.URL.Path, "/content/seal")
}

func (s *Server) resultContentAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.ResultContent == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Original result content is not configured"))
		return false
	}
	return true
}
func (s *Server) beginOriginalResultContent(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	var in resultwire.BeginContent
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.ResultContent.Begin(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) putOriginalResultChunk(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	var in resultwire.ContentChunk
	if !s.body(w, r, &in) {
		return
	}
	defer clear(in.Content)
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.ResultContent.Put(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) sealOriginalResultContent(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	var in resultwire.SealContent
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.ResultContent.Seal(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) createOriginalResultReadGrant(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/jobs/"), "/")
	if len(parts) != 4 {
		s.appError(w, r, fault.New(fault.Invalid, "Original result path required"))
		return
	}
	var in resultwire.ReadGrantRequest
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.ResultContent.CreateReadGrant(r.Context(), a.p, parts[0], parts[2], in, key)
	defer clear(v.Body)
	s.writeReport(w, r, v, e)
}

// These two registered routes bypass account authentication solely to accept
// their own read-only p11r credential. No other operation accepts this token.
func (s *Server) originalReadToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(r.Header.Values("Cookie")) != 0 || len(r.Header.Values("Origin")) != 0 || !strings.HasPrefix(values[0], "Bearer p11r.") || r.ContentLength > 0 {
		s.appError(w, r, fault.New(fault.Unauthenticated, "Native original read-only credential required"))
		return "", false
	}
	return strings.TrimPrefix(values[0], "Bearer "), true
}
func (s *Server) getOriginalResultContent(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	token, ok := s.originalReadToken(w, r)
	if !ok {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/result-content/"), "/manifest")
	v, e := s.c.ResultContent.ReadHead(r.Context(), token, id)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) getOriginalResultChunk(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	token, ok := s.originalReadToken(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/result-content/"), "/")
	if len(parts) != 3 {
		s.appError(w, r, fault.New(fault.Invalid, "Original chunk path required"))
		return
	}
	index, e := strconv.Atoi(parts[2])
	if e != nil || strconv.Itoa(index) != parts[2] {
		s.appError(w, r, fault.New(fault.Invalid, "Canonical chunk index required"))
		return
	}
	v, e := s.c.ResultContent.ReadChunk(r.Context(), token, parts[0], index)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	defer clear(v.Content)
	writeJSON(w, 200, v)
}

func (s *Server) getOriginalResultUploadStatus(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.resultContentAvailable(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/runner/attempts/"), "/")
	if len(parts) != 4 {
		s.appError(w, r, fault.New(fault.Invalid, "Original upload path required"))
		return
	}
	v, e := s.c.ResultContent.UploadStatus(r.Context(), a.p, parts[0], parts[2])
	if e != nil {
		s.appError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
