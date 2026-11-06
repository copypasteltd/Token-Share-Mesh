package controlhttp

import (
	"net/http"
	"strings"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/resultwire"
)

func (s *Server) completionAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.Completion == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Original completion service is not configured"))
		return false
	}
	return true
}

func (s *Server) registerAttemptDelivery(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	var in resultwire.Delivery
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Completion.Register(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) recordAttemptUsage(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	var in resultwire.Statement
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Completion.RecordUsage(r.Context(), a.p, attemptFromReportPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func jobFromCompletionPath(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/jobs/"), "/")
	return parts[0]
}
func (s *Server) getJob(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	v, e := s.c.Completion.Job(r.Context(), a.p, jobFromCompletionPath(r))
	if e != nil {
		s.appError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) recordBuyerReceipt(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	var in invocation.DecisionInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Completion.Decide(r.Context(), a.p, jobFromCompletionPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	var in invocation.CancelInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Completion.Cancel(r.Context(), a.p, jobFromCompletionPath(r), in, key)
	s.writeReport(w, r, v, e)
}
func (s *Server) getOrderBill(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.completionAvailable(w, r) {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/orders/"), "/bill")
	v, e := s.c.Completion.Bill(r.Context(), a.p, id)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
