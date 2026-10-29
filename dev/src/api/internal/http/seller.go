package controlhttp

import (
	"net/http"
	"p11.local/compute/internal/seller"
	"p11.local/compute/shared/fault"
	"strconv"
	"strings"
)

func (s *Server) sellerProfile(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	profile, err := s.c.Seller.Profile(r.Context(), a.p)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(profile.Version, 10)))
	writeJSON(w, 200, profile)
}
func (s *Server) sellerApplication(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in seller.Application
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	profile, err := s.c.Seller.Apply(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if profile.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(profile.Version, 10)))
	writeJSON(w, 201, profile)
}
func (s *Server) sellerExit(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in seller.Reason
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	ack, replay, err := s.c.Seller.Exit(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	if replay {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, 200, ack)
}
func (s *Server) recordConsent(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	var in seller.Consent
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	ack, err := s.c.Seller.RecordConsent(r.Context(), a.p, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	writeJSON(w, 200, ack)
}
func listingResponse(w http.ResponseWriter, status int, l seller.Listing) {
	if l.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(l.Version, 10)))
	writeJSON(w, status, l)
}
func (s *Server) ownListings(w http.ResponseWriter, r *http.Request, a actor) {
	if r.Method == http.MethodPost {
		if !s.noQuery(w, r) {
			return
		}
		var in seller.Definition
		if !s.body(w, r, &in) {
			return
		}
		key, ok := s.idempotency(w, r)
		if !ok {
			return
		}
		l, err := s.c.Seller.CreateListing(r.Context(), a.p, in, key)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		listingResponse(w, 201, l)
		return
	}
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
	scope := "seller-listings:" + a.p.UserID
	cursor, err := s.cursor(q.Get("cursor"), scope, false)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	page, err := s.c.Seller.ListListings(r.Context(), a.p, cursor, limit)
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
func (s *Server) ownListing(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix+"/seller/listings/")
	if r.Method == http.MethodGet {
		l, err := s.c.Seller.GetListing(r.Context(), a.p, id)
		if err != nil {
			s.appError(w, r, err)
			return
		}
		listingResponse(w, 200, l)
		return
	}
	var in seller.Patch
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	values := r.Header.Values("If-Match")
	var version int64
	if len(values) == 1 && strings.HasPrefix(values[0], "\"") && strings.HasSuffix(values[0], "\"") {
		raw := strings.TrimSuffix(strings.TrimPrefix(values[0], "\""), "\"")
		v, err := strconv.ParseInt(raw, 10, 32)
		if err == nil && v > 0 && strconv.FormatInt(v, 10) == raw {
			version = v
		}
	}
	if version == 0 {
		s.appError(w, r, fault.New(fault.Invalid, "One strong numeric If-Match ETag required"))
		return
	}
	l, err := s.c.Seller.UpdateListing(r.Context(), a.p, id, version, in, key)
	if err != nil {
		s.appError(w, r, err)
		return
	}
	listingResponse(w, 200, l)
}
func (s *Server) listingState(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix+"/seller/listings/")
	id, action, _ := strings.Cut(path, "/")
	var in seller.Reason
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	l, err := s.c.Seller.ChangeListingState(r.Context(), a.p, id, in, key, action == "submit")
	if err != nil {
		s.appError(w, r, err)
		return
	}
	listingResponse(w, 200, l)
}
