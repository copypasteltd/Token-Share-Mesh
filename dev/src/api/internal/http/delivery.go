package controlhttp

import (
	"net/http"
	"p11.local/compute/shared/fault"
	"strings"
)

func (s *Server) deliveryID(w http.ResponseWriter, r *http.Request, suffix string) (string, bool) {
	if !s.noQuery(w, r) {
		return "", false
	}
	if len(r.Header.Values("Range")) != 0 || len(r.Header.Values("If-Range")) != 0 || len(r.Header.Values("If-None-Match")) != 0 {
		s.appError(w, r, fault.New(fault.Invalid, "Only complete authenticated original delivery is supported"))
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/runner/leases/"), "/"+suffix), true
}
func (s *Server) getExecutionManifest(w http.ResponseWriter, r *http.Request, a actor) {
	id, ok := s.deliveryID(w, r, "manifest")
	if !ok {
		return
	}
	v, e := s.c.Claims.Manifest(r.Context(), a.p, id)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) getExecutionInput(w http.ResponseWriter, r *http.Request, a actor) {
	id, ok := s.deliveryID(w, r, "input")
	if !ok {
		return
	}
	body, e := s.c.Delivery.Input(r.Context(), a.p, id)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	defer clear(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
