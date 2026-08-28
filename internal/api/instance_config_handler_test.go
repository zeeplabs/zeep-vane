//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func newInstanceConfigRouter(t *testing.T, dnsTarget string) (http.Handler, *db.AdminRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	admins := db.NewAdminRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	handler := NewInstanceConfigHandler(dnsTarget, companySettings, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/api/instance/branding", handler.Branding)
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/instance/dns-target", handler.DNSTarget)
	})

	return r, admins, pool
}

func getDNSTarget(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/instance/dns-target", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestDNSTarget_Configured_200ReturnsValue asserts SPD-10: a configured
// PUBLIC_DNS_TARGET is returned verbatim.
func TestDNSTarget_Configured_200ReturnsValue(t *testing.T) {
	r, admins, _ := newInstanceConfigRouter(t, "vane.example.com")
	token := issueTestSessionToken(t, admins)

	rec := getDNSTarget(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body dnsTargetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Target == nil || *body.Target != "vane.example.com" {
		t.Errorf("Target = %v, want %q", body.Target, "vane.example.com")
	}
}

// TestDNSTarget_Unconfigured_200ReturnsNull asserts SPD-10's fallback:
// an unconfigured PUBLIC_DNS_TARGET (empty string) surfaces as target: null,
// not an error - the missing hint never blocks anything.
func TestDNSTarget_Unconfigured_200ReturnsNull(t *testing.T) {
	r, admins, _ := newInstanceConfigRouter(t, "")
	token := issueTestSessionToken(t, admins)

	rec := getDNSTarget(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body dnsTargetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Target != nil {
		t.Errorf("Target = %v, want nil", *body.Target)
	}
}

// TestDNSTarget_NoAuth_401 asserts SPD-11's RBAC floor on this endpoint:
// unauthenticated requests are rejected.
func TestDNSTarget_NoAuth_401(t *testing.T) {
	r, _, _ := newInstanceConfigRouter(t, "vane.example.com")

	rec := getDNSTarget(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func getBranding(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/instance/branding", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestBranding_NoAuth_200 asserts Branding is reachable without any
// Authorization header - the login screen has no session yet.
func TestBranding_NoAuth_200(t *testing.T) {
	r, _, _ := newInstanceConfigRouter(t, "vane.example.com")

	rec := getBranding(t, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestBranding_LogoUploaded_ReturnsLogoURL asserts the real, persisted logo
// URL is surfaced, not a placeholder.
func TestBranding_LogoUploaded_ReturnsLogoURL(t *testing.T) {
	// This test mutates the shared company_settings singleton row, which
	// races internal/db's and internal/cli's own company_settings tests
	// across the separate concurrent processes `go test ./...` runs them
	// as - see LockCompanySettings' doc comment.
	dbtest.LockCompanySettings(t, context.Background(), testDatabaseURL(t))

	r, _, pool := newInstanceConfigRouter(t, "vane.example.com")
	companySettings := db.NewCompanySettingsRepository(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET logo_data = NULL, logo_content_type = NULL WHERE id = 1")
	})
	if _, err := companySettings.UpdateLogo(context.Background(), "image/png", []byte("fake-png-bytes")); err != nil {
		t.Fatalf("setup UpdateLogo() returned unexpected error: %v", err)
	}

	rec := getBranding(t, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body brandingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.LogoURL == nil || *body.LogoURL != "/uploads/logo" {
		t.Errorf("LogoURL = %v, want %q", body.LogoURL, "/uploads/logo")
	}
}

// TestBranding_NoLogoUploaded_ReturnsNull asserts the no-logo case never
// fabricates a placeholder path.
func TestBranding_NoLogoUploaded_ReturnsNull(t *testing.T) {
	// This test mutates the shared company_settings singleton row, which
	// races internal/db's and internal/cli's own company_settings tests
	// across the separate concurrent processes `go test ./...` runs them
	// as - see LockCompanySettings' doc comment.
	dbtest.LockCompanySettings(t, context.Background(), testDatabaseURL(t))

	r, _, pool := newInstanceConfigRouter(t, "vane.example.com")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET logo_data = NULL, logo_content_type = NULL WHERE id = 1")
	})
	if _, err := pool.Exec(context.Background(), "UPDATE company_settings SET logo_data = NULL, logo_content_type = NULL WHERE id = 1"); err != nil {
		t.Fatalf("setup returned unexpected error: %v", err)
	}

	rec := getBranding(t, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body brandingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.LogoURL != nil {
		t.Errorf("LogoURL = %v, want nil", *body.LogoURL)
	}
}
