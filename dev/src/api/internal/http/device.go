package controlhttp

import (
	"net/http"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/shared/fault"
	"strconv"
	"strings"
)

func deviceResponse(w http.ResponseWriter, status int, v device.View) {
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Version, 10)))
	writeJSON(w, status, v)
}
func (s *Server) createPairing(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in device.PairInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Devices.CreatePairing(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 201, v)
}
func (s *Server) enrollDevice(w http.ResponseWriter, r *http.Request, _ actor) {
	if !s.noQuery(w, r) {
		return
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(r.Header.Values("Cookie")) != 0 || len(r.Header.Values("Origin")) != 0 || !strings.HasPrefix(values[0], "Bearer p11p.") || strings.Contains(values[0][7:], " ") {
		s.appError(w, r, fault.New(fault.Unauthenticated, "One native pairing credential required"))
		return
	}
	var in device.Enrollment
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Devices.Enroll(r.Context(), values[0][7:], in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	deviceResponse(w, 201, v)
}
func (s *Server) getDevice(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix+"/devices/")
	v, err := s.c.Devices.Get(r.Context(), a.p, id)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	deviceResponse(w, 200, v)
}
func (s *Server) ownDevices(w http.ResponseWriter, r *http.Request, a actor) {
	q, err := query(r, "cursor", "limit")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	limit, err := pageLimit(q, 50)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	scope := "seller-devices:" + a.p.UserID
	cursor, err := s.cursor(q.Get("cursor"), scope, false)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	page, err := s.c.Devices.List(r.Context(), a.p, cursor, limit)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if page.NextCursor != nil {
		v, err := s.cursor(*page.NextCursor, scope, true)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		page.NextCursor = &v
	}
	writeJSON(w, 200, page)
}
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/devices/"), "/heartbeats")
	var in device.Heartbeat
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Devices.Heartbeat(r.Context(), a.p, id, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 200, v)
}
func (s *Server) deviceState(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix+"/devices/"), "/")
	var in device.Reason
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Devices.ChangeState(r.Context(), a.p, id, in, key, action == "revoke")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	deviceResponse(w, 200, v)
}
func (s *Server) devicePolicy(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix+"/devices/"), "/resource-policy")
	var in device.Policy
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	var version int64
	values := r.Header.Values("If-Match")
	if len(values) == 1 {
		raw, e := strconv.Unquote(values[0])
		if e == nil {
			v, e := strconv.ParseInt(raw, 10, 32)
			if e == nil && v > 0 && strconv.Quote(strconv.FormatInt(v, 10)) == values[0] {
				version = v
			}
		}
	}
	if version == 0 {
		s.appError(w, r, fault.New(fault.Invalid, "One strong numeric If-Match ETag required"))
		return
	}
	v, err := s.c.Devices.UpdatePolicy(r.Context(), a.p, id, version, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Version, 10)))
	writeJSON(w, 200, v)
}
