package api_test

import (
	"net/http"
	"p11.local/compute/shared/ids"
	"testing"
)

// SOURCE_ONLY. No API server, database or native engine has been started.
func TestHTTPCompletionReadsNeedOriginalServiceAndCurrentSession(t *testing.T) {
	h := newHarness(t, 1000, 100)
	s, _ := h.login(t, "MINIAPP")
	for _, path := range []string{"/jobs/job_original", "/orders/order_original/bill"} {
		expectError(t, h.request(t, http.MethodGet, path, nil, bearer(s.AccessToken)), 503, "UNAVAILABLE")
		expectError(t, h.request(t, http.MethodGet, path, nil, http.Header{}), 401, "UNAUTHENTICATED")
	}
}
func TestHTTPCompletionDecisionKeepsCookieCsrfBoundary(t *testing.T) {
	h := newHarness(t, 1000, 100)
	_, cookie := h.login(t, "WEB")
	headers := http.Header{}
	headers.Set("Idempotency-Key", ids.New("original"))
	expectError(t, h.request(t, http.MethodPost, "/jobs/job_original/receipts", map[string]any{}, headers, cookie), 403, "FORBIDDEN")
}
func TestHTTPCompletionCancelWithoutConfiguredAuthorityCannotAct(t *testing.T) {
	h := newHarness(t, 1000, 100)
	s, _ := h.login(t, "MINIAPP")
	headers := bearer(s.AccessToken)
	headers.Set("Idempotency-Key", ids.New("original"))
	expectError(t, h.request(t, http.MethodPost, "/jobs/job_original/cancel", map[string]any{"expected_job_version": 1, "reason_code": "BUYER_REQUEST"}, headers), 503, "UNAVAILABLE")
}
