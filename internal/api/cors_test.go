//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newCORSTestRouter(allowedOrigin string) http.Handler {
	r := chi.NewRouter()
	r.Use(NewCORSMiddleware(allowedOrigin))
	r.Get("/api/domains", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func TestCORS_PreflightFromAllowedOrigin_GrantsCredentials(t *testing.T) {
	r := newCORSTestRouter("http://localhost:5173")

	req := httptest.NewRequest(http.MethodOptions, "/api/domains", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:5173")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
}

func TestCORS_PreflightFromDisallowedOrigin_NoOriginHeaderGranted(t *testing.T) {
	r := newCORSTestRouter("http://localhost:5173")

	req := httptest.NewRequest(http.MethodOptions, "/api/domains", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty (origin not in allowlist)", got)
	}
}

// TestCORS_NeverCombinesWildcardOriginWithCredentials guards the
// configuration invariant directly, at the Options level our constructor
// builds - not relying on the library's runtime behavior alone. It covers
// every concrete origin the config layer can actually produce (the Vite
// dev default and a representative production-style origin); none may
// ever be "*" while AllowCredentials is true.
func TestCORS_NeverCombinesWildcardOriginWithCredentials(t *testing.T) {
	for _, origin := range []string{"http://localhost:5173", "https://vane.example.com"} {
		opts := corsOptions(origin)
		if !opts.AllowCredentials {
			t.Fatalf("origin %q: AllowCredentials = false, want true", origin)
		}
		for _, o := range opts.AllowedOrigins {
			if o == "*" {
				t.Fatalf("origin %q: AllowedOrigins contains wildcard %q alongside AllowCredentials=true, want never combined", origin, o)
			}
		}
	}
}
