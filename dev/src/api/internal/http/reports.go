package controlhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/invocation"
)

func attemptFromReportPath(r *http.Request) string {
	parts := strings.Split(r.URL.Path, "/")
	return parts[len(parts)-2]
}
func (s *Server) writeReport(w http.ResponseWriter, r *http.Request, v invocation.ReportResult, e error) {
	if e != nil {
		s.appError(w, r, e)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 200, json.RawMessage(v.Body))
}
func (s *Server) recordAttemptProgress(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in devicecontrol.ProgressWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	var v invocation.ReportResult
	var e error
	if s.c.Continuations != nil {
		v, e = s.c.Continuations.RecordProgress(r.Context(), a.p, attemptFromReportPath(r), in, key)
	} else {
		v, e = s.c.Claims.RecordProgress(r.Context(), a.p, attemptFromReportPath(r), in, key)
	}
	s.writeReport(w, r, v, e)
}
func (s *Server) recordAttemptCleanup(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in devicecontrol.CleanupWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Claims.RecordCleanup(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
