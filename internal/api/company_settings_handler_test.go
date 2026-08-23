//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// newCompanySettingsRouter builds a router mounting only RequireAuth (no
// RequireRole) in front of CompanySettingsHandler, mirroring
// newDomainsRouter: RBAC for these routes is asserted at the routes.go
// wiring level (T8), not by the handler itself.
func newCompanySettingsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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

	// The company_settings row is a singleton shared across every test in
	// this package - reset it to a known state before and after each test.
	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)

	repo := db.NewCompanySettingsRepository(pool)
	admins := db.NewAdminRepository(pool)
	handler := NewCompanySettingsHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/company-settings", handler.Get)
		protected.Patch("/api/company-settings", handler.Update)
	})

	return r, pool, admins
}

func getCompanySettings(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/company-settings", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func patchCompanySettings(t *testing.T, r http.Handler, token string, body updateCompanySettingsRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/company-settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestCompanySettingsGet_FreshInstall_200SeededRow asserts SET-03: GET
// returns the seeded row (empty name/contact_email, null logo_url) on a
// fresh install, never a 404.
func TestCompanySettingsGet_FreshInstall_200SeededRow(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getCompanySettings(t, r, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Name != "" {
		t.Errorf("Name = %q, want \"\"", resp.Name)
	}
	if resp.ContactEmail != "" {
		t.Errorf("ContactEmail = %q, want \"\"", resp.ContactEmail)
	}
	if resp.LogoURL != nil {
		t.Errorf("LogoURL = %v, want nil", *resp.LogoURL)
	}
}

// TestCompanySettingsUpdate_ValidBody_200Persists asserts SET-01: a valid
// PATCH persists name/contact_email and responds 200 with the updated row.
func TestCompanySettingsUpdate_ValidBody_200Persists(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Name != "Acme Inc." {
		t.Errorf("Name = %q, want %q", resp.Name, "Acme Inc.")
	}
	if resp.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail = %q, want %q", resp.ContactEmail, "owner@acme.example.com")
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.Name != "Acme Inc." {
		t.Errorf("persisted Name = %q, want %q", getResp.Name, "Acme Inc.")
	}
}

// TestCompanySettingsUpdate_EmptyName_422NoPersistence asserts SET-04: an
// empty name is rejected with 422 and the persisted row is left untouched.
func TestCompanySettingsUpdate_EmptyName_422NoPersistence(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	// Seed a known-good value first so we can prove it survives the
	// rejected PATCH.
	if rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "", ContactEmail: "owner@acme.example.com"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.Name != "Acme Inc." {
		t.Errorf("Name after rejected PATCH = %q, want unchanged %q", getResp.Name, "Acme Inc.")
	}
}

// TestCompanySettingsUpdate_MalformedContactEmail_422NoPersistence asserts
// SET-05: a malformed contact_email is rejected with 422 and the persisted
// row is left untouched.
func TestCompanySettingsUpdate_MalformedContactEmail_422NoPersistence(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	if rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "not-an-email"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail after rejected PATCH = %q, want unchanged %q", getResp.ContactEmail, "owner@acme.example.com")
	}
}
