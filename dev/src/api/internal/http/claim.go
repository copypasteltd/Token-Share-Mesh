package controlhttp

import (
	"net/http"

	"p11.local/compute/client/protocol/devicecontrol"
)

func (s *Server) claimLease(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in devicecontrol.LeaseClaim
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Claims.Claim(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 200, v.Lease)
}
