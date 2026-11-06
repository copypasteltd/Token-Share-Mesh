package api_test

// SOURCE_ONLY. These future native HTTP definitions do not run a channel.
import (
	"net/http"
	"p11.local/compute/shared/ids"
	"testing"
)

func TestHTTPRefundRoutesRequireExplicitOriginalRefundHost(t *testing.T) {
	h := newHarness(t, 1000, 100)
	s, _ := h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	headers.Set("Idempotency-Key", ids.New("idem"))
	for _, path := range []string{"/orders/ord_original/refunds", "/refunds/refund_original", "/finance/refund-reviews", "/finance/refund-reviews/refund_original"} {
		expectError(t, h.request(t, http.MethodGet, path, nil, headers), 503, "UNAVAILABLE")
	}
	expectError(t, h.request(t, http.MethodPost, "/orders/ord_original/refunds", map[string]any{"expected_order_version": 1, "reason_code": "NO_LONGER_NEEDED", "requested_fen": 100}, headers), 503, "UNAVAILABLE")
}
func TestHTTPRefundApprovalAndRevocationNeverAcceptAnonymousRequests(t *testing.T) {
	h := newHarness(t, 1000, 100)
	for _, suffix := range []string{"decisions", "revocations"} {
		expectError(t, h.request(t, http.MethodPost, "/finance/refund-reviews/refund_original/"+suffix, map[string]any{}, http.Header{}), 401, "UNAUTHENTICATED")
	}
}
func TestHTTPRefundReviewMutationKeepsCookieCsrfBoundary(t *testing.T) {
	h := newHarness(t, 1000, 100)
	_, cookie := h.login(t, "WEB")
	expectError(t, h.request(t, http.MethodPost, "/finance/refund-reviews/refund_original/decisions", map[string]any{}, http.Header{}, cookie), 403, "FORBIDDEN")
}
