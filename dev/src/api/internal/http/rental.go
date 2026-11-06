package controlhttp

import (
	"net/http"
	"strconv"
	"strings"

	"p11.local/compute/internal/rental"
	"p11.local/compute/shared/fault"
)

func rentalID(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
	if len(parts) > 1 && (parts[0] == "rental-sessions") {
		return parts[1]
	}
	if len(parts) > 2 && parts[0] == "runner" && parts[1] == "rental-sessions" {
		return parts[2]
	}
	return ""
}

func (s *Server) rentalAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.Rentals == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Rental authority requires explicit configuration"))
		return false
	}
	return true
}

func rentalResponse(w http.ResponseWriter, status int, v rental.View) {
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Version, 10)))
	writeJSON(w, status, v)
}

func (s *Server) createRentalSession(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.CreateWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.Create(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	rentalResponse(w, 201, v)
}

func (s *Server) getRentalSession(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	v, err := s.c.Rentals.Get(r.Context(), a.p, rentalID(r))
	if err != nil {
		s.appError(w, r, err)
		return
	}
	rentalResponse(w, 200, v)
}

func (s *Server) claimRentalPreparation(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.PreparationWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.ClaimPreparation(r.Context(), a.p, rentalID(r), in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Session.Version, 10)))
	writeJSON(w, 200, v)
}

func (s *Server) recordRentalDeviceReady(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.DeviceReadyWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.DeviceReady(r.Context(), a.p, rentalID(r), in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	rentalResponse(w, 200, v)
}

func (s *Server) confirmRentalReady(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.ReadyWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.ConfirmReady(r.Context(), a.p, rentalID(r), in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	rentalResponse(w, 200, v)
}

func (s *Server) stopRentalSession(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.StopWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.Stop(r.Context(), a.p, rentalID(r), in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	rentalResponse(w, 200, v)
}

func (s *Server) createRentalAccessGrant(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.rentalAvailable(w, r) {
		return
	}
	var in rental.AccessWrite
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, err := s.c.Rentals.CreateAccess(r.Context(), a.p, rentalID(r), in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	defer v.Close()
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Metadata().SessionVersion, 10)))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	// A partial response is not retried under a new key by this adapter. The
	// buyer explicitly replaces a lost first grant; safe replay has no Bearer.
	_ = v.WriteJSONOnce(w)
}
