package api_test

// SOURCE_ONLY. These definitions do not start a model or infer qualification.
import (
	"net/http"
	"p11.local/compute/shared/ids"
	"testing"
)

func TestHTTPResultContentReadIsUnavailableWithoutConfiguredAuthority(t *testing.T) {
	h := newHarness(t, 1000, 100)
	for _, path := range []string{"/result-content/delivery_original/manifest", "/result-content/delivery_original/chunks/0"} {
		expectError(t, h.request(t, http.MethodGet, path, nil, http.Header{}), 503, "UNAVAILABLE")
	}
}
func TestHTTPResultReadCredentialIsNotGeneralAccountSession(t *testing.T) {
	h := newHarness(t, 1000, 100)
	head := bearer("p11r.grant_original.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	for _, path := range []string{"/jobs/job_original", "/orders/order_original/bill"} {
		expectError(t, h.request(t, http.MethodGet, path, nil, head), 401, "UNAUTHENTICATED")
	}
}
func TestHTTPResultGrantKeepsAccountAndCookieCsrfBoundary(t *testing.T) {
	h := newHarness(t, 1000, 100)
	path := "/jobs/job_original/results/delivery_original/read-grants"
	head := http.Header{}
	head.Set("Idempotency-Key", ids.New("grant"))
	expectError(t, h.request(t, http.MethodPost, path, map[string]any{}, head), 401, "UNAUTHENTICATED")
	_, cookie := h.login(t, "WEB")
	expectError(t, h.request(t, http.MethodPost, path, map[string]any{}, head, cookie), 403, "FORBIDDEN")
}
