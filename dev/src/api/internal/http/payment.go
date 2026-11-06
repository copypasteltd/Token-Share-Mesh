package controlhttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

func (s *Server) paymentAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.c.Payments == nil {
		s.appError(w, r, fault.New(fault.Unavailable, "Original seller payment configuration required"))
		return false
	}
	return true
}
func (s *Server) paymentError(w http.ResponseWriter, r *http.Request, e error) {
	switch {
	case errors.Is(e, payments.ErrIntent), errors.Is(e, payments.ErrProof):
		e = fault.New(fault.Invalid, "Original bounded payment input required")
	case errors.Is(e, payments.ErrConflict):
		e = fault.New(fault.Conflict, "Original payment facts conflict; query the original payment")
	case errors.Is(e, payments.ErrBinding), errors.Is(e, payments.ErrUnsupported):
		e = fault.New(fault.Unavailable, "Original seller payment capability unavailable")
	case errors.Is(e, payments.ErrUnknown):
		e = fault.New(fault.Unavailable, "Payment outcome unresolved; recover the original request")
	}
	s.appError(w, r, e)
}
func paymentResponse(w http.ResponseWriter, status int, v payments.Payment) {
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(v.Version, 10)))
	writeJSON(w, status, v)
}
func (s *Server) createPaymentIntent(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	var in payments.IntentInput
	if !s.body(w, r, &in) {
		return
	}
	if in.ExpectedOrderVersion < 1 || in.ExpectedOrderVersion > 2147483647 || !ids.Valid(in.ChannelCapabilityRef) || (in.ReturnTargetID != "" && !ids.Valid(in.ReturnTargetID)) {
		s.paymentError(w, r, payments.ErrIntent)
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
	if len(parts) != 3 {
		s.paymentError(w, r, payments.ErrIntent)
		return
	}
	v, e := s.c.Payments.CreateIntent(r.Context(), a.p, parts[1], in, key)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	// Resume only this still-undispatched original intent. The DB claim commits
	// first; simultaneous/replayed requests cannot issue a second precreate.
	if v.State == "CREATED" {
		v, e = s.c.Payments.Precreate(r.Context(), a.p, v.ID)
		if e != nil {
			s.paymentError(w, r, e)
			return
		}
	}
	paymentResponse(w, http.StatusCreated, v)
}
func (s *Server) getPayment(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix+"/payments/")
	v, e := s.c.Payments.Get(r.Context(), a.p, id)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	paymentResponse(w, http.StatusOK, v)
}

func erasePaymentProof(h http.Header) {
	for name := range h {
		if strings.EqualFold(name, "Payment-Proof") {
			delete(h, name)
		}
	}
}
func paymentProofPath(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	return paymentProofResource(r.URL.Path)
}
func paymentProofResource(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, prefix+"/"), "/")
	return len(parts) == 3 && parts[0] == "payments" && ids.Valid(parts[1]) && parts[2] == "proofs"
}
func paymentResourceID(r *http.Request) string {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix+"/"), "/")
	if len(parts) < 2 || !ids.Valid(parts[1]) {
		return ""
	}
	return parts[1]
}
func (s *Server) listOrderPaymentOptions(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	v, e := s.c.Payments.Options(r.Context(), a.p, paymentResourceID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (s *Server) getOrderPayment(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	v, e := s.c.Payments.GetForOrder(r.Context(), a.p, paymentResourceID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	paymentResponse(w, http.StatusOK, v)
}
func (s *Server) getPaymentChallenge(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	v, e := s.c.Payments.Challenge(r.Context(), a.p, paymentResourceID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	// The existing native server owns request/write deadlines. This is exactly
	// the same committed original prepay reference, never a fresh financial call.
	now := time.Now()
	if !v.ValidAt(now) {
		s.paymentError(w, r, payments.ErrUnknown)
		return
	}
	_ = v.WriteChallenge(w, now)
}
func (s *Server) submitMachinePaymentProof(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	var empty struct{}
	if !s.body(w, r, &empty) {
		return
	}
	key, ok := s.idempotency(w, r)
	if !ok {
		return
	}
	proof, e := payments.ProofFromRequest(r)
	erasePaymentProof(r.Header)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	defer proof.Close()
	v, e := s.c.Payments.SubmitProof(r.Context(), a.p, paymentResourceID(r), proof, key)
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	if v.Replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	// Only safe metadata is returned. Dedicated original recovery owns verify;
	// neither receipt upload nor this 202 marks payment, READY or execution done.
	w.Header().Set("Retry-After", "2")
	writeJSON(w, http.StatusAccepted, v)
}
func (s *Server) getMachinePaymentRecovery(w http.ResponseWriter, r *http.Request, a actor) {
	if !s.noQuery(w, r) || !s.paymentAvailable(w, r) {
		return
	}
	v, e := s.c.Payments.GetRecovery(r.Context(), a.p, paymentResourceID(r))
	if e != nil {
		s.paymentError(w, r, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
