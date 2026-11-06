package controlhttp

import (
	"net/http"
	"net/url"
	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/fault"
	"strconv"
	"strings"
)

func (s *Server) refundAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.Refunds == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Original refund route configuration required"))
		return false
	}
	return true
}
func refundApplicationResponse(w http.ResponseWriter, status int, v payments.RefundApplication) {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Version, 10)))
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, status, v)
}
func (s *Server) createRefund(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.refundAvailable(w, r) {
		return
	}
	var in payments.RefundApplicationInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Refunds.Apply(r.Context(), a.p, paymentResourceID(r), in, key)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	refundApplicationResponse(w, http.StatusCreated, v)
}
func (s *Server) getRefund(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.refundAvailable(w, r) {
		return
	}
	v, e := s.c.Refunds.Application(r.Context(), a.p, paymentResourceID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	refundApplicationResponse(w, http.StatusOK, v)
}
func (s *Server) refundPageInput(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	q, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		s.paymentError(w, r, payments.ErrIntent)
		return "", 0, false
	}
	for key, values := range q {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			s.appError(w, r, fault.New(fault.Invalid, "Bounded original refund page required"))
			return "", 0, false
		}
	}
	limit := 20
	var e error
	if q.Has("limit") {
		limit, e = strconv.Atoi(q.Get("limit"))
	}
	if e != nil || limit < 1 || limit > 50 {
		s.paymentError(w, r, payments.ErrIntent)
		return "", 0, false
	}
	return q.Get("cursor"), limit, true
}
func (s *Server) listOrderRefunds(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.refundAvailable(w, r) {
		return
	}
	after, limit, ok := s.refundPageInput(w, r)
	if !ok {
		return
	}
	v, e := s.c.Refunds.ListApplications(r.Context(), a.p, paymentResourceID(r), after, limit)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) listRefundReviews(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.refundAvailable(w, r) {
		return
	}
	after, limit, ok := s.refundPageInput(w, r)
	if !ok {
		return
	}
	v, e := s.c.Refunds.ListReviews(r.Context(), a.p, after, limit)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func refundReviewID(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}
func (s *Server) getRefundReview(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.refundAvailable(w, r) {
		return
	}
	v, e := s.c.Refunds.ReviewDetails(r.Context(), a.p, refundReviewID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) decideRefundReview(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.refundAvailable(w, r) {
		return
	}
	var in payments.RefundReviewInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Refunds.Review(r.Context(), a.p, refundReviewID(r), in, key)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	refundApplicationResponse(w, http.StatusOK, v)
}
func (s *Server) revokeRefundReview(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.refundAvailable(w, r) {
		return
	}
	var in payments.RefundRevokeInput
	if !s.body(w, r, &in) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	v, e := s.c.Refunds.RevokeReview(r.Context(), a.p, refundReviewID(r), in, key)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	refundApplicationResponse(w, http.StatusOK, v)
}
