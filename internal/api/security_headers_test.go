package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestOKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeaders_SetsNosniffAndCSP(t *testing.T) {
	handler := SecurityHeaders(false)(newTestOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("Content-Security-Policy header is empty, want a policy set")
	}
}

func TestSecurityHeaders_HSTSFalse_NoStrictTransportSecurity(t *testing.T) {
	handler := SecurityHeaders(false)(newTestOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want empty (this listener isn't terminating TLS)", got)
	}
}

func TestSecurityHeaders_HSTSTrue_SetsStrictTransportSecurity(t *testing.T) {
	handler := SecurityHeaders(true)(newTestOKHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("Strict-Transport-Security header is empty, want a max-age directive")
	}
}
