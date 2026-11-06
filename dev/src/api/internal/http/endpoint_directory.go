package controlhttp

import (
	"net/http"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/fault"
	"strings"
)

func (s *Server) endpointDirectoryAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.EndpointDirectory == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Public endpoint directory is not configured"))
		return false
	}
	return true
}
func (s *Server) writeEndpointDirectory(w http.ResponseWriter, r *http.Request, status int, value any, e error) {
	if e != nil {
		s.appError(w, r, e)
		return
	}
	raw, e := canonicaljson.Encode(value, endpointdirectory.MaximumReply)
	if e != nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Bounded public endpoint response unavailable"))
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
func (s *Server) registerEndpointBundle(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.endpointDirectoryAvailable(w, r) {
		return
	}
	var in endpointdirectory.Registration
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.EndpointDirectory.Register(r.Context(), a.p, in, key)
	if e == nil && v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	s.writeEndpointDirectory(w, r, 201, v, e)
}
func (s *Server) listOwnEndpointBundles(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.endpointDirectoryAvailable(w, r) {
		return
	}
	q, e := query(r, "cursor")
	if e != nil {
		s.appError(w, r, e)
		return
	}
	scope := "endpoint-bundles:" + a.p.UserID + ":" + a.p.DeviceID
	cursor, e := s.cursor(q.Get("cursor"), scope, false)
	if e != nil {
		s.appError(w, r, e)
		return
	}
	v, e := s.c.EndpointDirectory.List(r.Context(), a.p, cursor)
	if e == nil && v.Next != nil {
		encoded, ce := s.cursor(*v.Next, scope, true)
		if ce != nil {
			e = ce
		} else {
			v.Next = &encoded
		}
	}
	s.writeEndpointDirectory(w, r, 200, v, e)
}
func endpointRecipientPath(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/endpoint-bundles/"), "/")
	return parts[0]
}
func (s *Server) getOwnEndpointBundle(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.endpointDirectoryAvailable(w, r) {
		return
	}
	v, e := s.c.EndpointDirectory.Get(r.Context(), a.p, endpointRecipientPath(r))
	s.writeEndpointDirectory(w, r, 200, v, e)
}
func (s *Server) rotateEndpointBundle(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.endpointDirectoryAvailable(w, r) {
		return
	}
	var in endpointdirectory.Rotation
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.EndpointDirectory.Rotate(r.Context(), a.p, endpointRecipientPath(r), in, key)
	if e == nil && v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	s.writeEndpointDirectory(w, r, 201, v, e)
}
func (s *Server) revokeEndpointBundle(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.endpointDirectoryAvailable(w, r) {
		return
	}
	var in endpointdirectory.Revocation
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.EndpointDirectory.Revoke(r.Context(), a.p, endpointRecipientPath(r), in, key)
	if e == nil && v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	s.writeEndpointDirectory(w, r, 200, v, e)
}
func (s *Server) getOrderEndpointBundle(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.endpointDirectoryAvailable(w, r) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/orders/"), "/")
	if len(parts) != 3 {
		s.appError(w, r, fault.New(fault.Invalid, "Original order endpoint path required"))
		return
	}
	v, e := s.c.EndpointDirectory.GetForOrder(r.Context(), a.p, parts[0], parts[2])
	s.writeEndpointDirectory(w, r, 200, v, e)
}
