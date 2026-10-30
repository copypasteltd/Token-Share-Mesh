package controlhttp

import (
	"net/http"
	"p11.local/compute/internal/routecontract"
	"strings"
)

func (s *Server) routeContracts(w http.ResponseWriter, r *http.Request, a actor) {
	if r.Method == http.MethodPost {
		if !s.noQuery(w, r) {
			return
		}
		var in routecontract.Write
		if !s.body(w, r, &in) {
			return
		}
		key, ok := s.idempotency(w, r)
		if !ok {
			return
		}
		c, err := s.c.Routes.Create(r.Context(), a.p, in, key)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		if c.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, 201, c)
		return
	}
	q, err := query(r, "cursor", "limit")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	limit, err := pageLimit(q, 100)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	scope := "routes:" + a.p.UserID
	cursor, err := s.cursor(q.Get("cursor"), scope, false)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	page, err := s.c.Routes.List(r.Context(), a.p, cursor, limit)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if page.NextCursor != nil {
		value, err := s.cursor(*page.NextCursor, scope, true)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		page.NextCursor = &value
	}
	writeJSON(w, 200, page)
}

func (s *Server) getRouteContract(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix+"/route-contracts/")
	c, err := s.c.Routes.Get(r.Context(), a.p, id)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}
