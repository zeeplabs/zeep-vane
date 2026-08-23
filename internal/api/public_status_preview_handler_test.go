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
)

func newPublicStatusPreviewRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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
	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	statusPages := db.NewStatusPageRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	inner := NewPublicStatusHandler(services, snapshots, incidents, companySettings, zap.NewNop())
	handler := NewPublicStatusPreviewHandler(statusPages, inner, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/status-pages/{id}/public-preview", handler.Get)
	})

	return r, pool, admins
}

func getPublicStatusPreview(t *testing.T, r http.Handler, token, statusPageID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/status-pages/"+statusPageID+"/public-preview", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPublicStatusPreview_AuthenticatedByID_200SameShapeAsProduction(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)

	fetchedAt := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", fetchedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET state = 'published' WHERE id = $1", statusPageID); err != nil {
		t.Fatalf("setup publish update returned unexpected error: %v", err)
	}

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in preview response", serviceID)
	}
	if found.Status != "operational" {
		t.Errorf("Status = %q, want %q", found.Status, "operational")
	}
}

// TestPublicStatusPreview_CompanySettingsSet_IncludesNameAndLogo covers
// SET-15: the I12 dev/preview endpoint shares composeResponse with
// production, so it must surface the same real company identity, sourced
// the same way.
func TestPublicStatusPreview_CompanySettingsSet_IncludesNameAndLogo(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	resetCompanySettingsForPublicStatusTest(t, pool)
	token := issueTestSessionToken(t, admins)

	companySettings := db.NewCompanySettingsRepository(pool)
	if _, err := companySettings.Update(context.Background(), "Acme Status", "contato@acme.example"); err != nil {
		t.Fatalf("setup Update() returned unexpected error: %v", err)
	}

	statusPageID := createPublicStatusPageFixture(t, pool)
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET state = 'published' WHERE id = $1", statusPageID); err != nil {
		t.Fatalf("setup publish update returned unexpected error: %v", err)
	}

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if body.Company.Name != "Acme Status" {
		t.Errorf("Company.Name = %q, want %q", body.Company.Name, "Acme Status")
	}
	if body.Company.LogoURL != nil {
		t.Errorf("Company.LogoURL = %v, want nil (no logo uploaded in this test)", *body.Company.LogoURL)
	}
}

func TestPublicStatusPreview_NoAuth_401(t *testing.T) {
	r, pool, _ := newPublicStatusPreviewRouter(t)
	statusPageID := createPublicStatusPageFixture(t, pool)

	rec := getPublicStatusPreview(t, r, "", statusPageID)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestPublicStatusPreview_DraftPage_404 mirrors router.HostRouter's own
// gate: a status page still in "draft" (the DB default - SP-15) is never
// composed, even for an authenticated admin previewing it, so the preview
// never disagrees with what the page's real hostname would show once
// published.
func TestPublicStatusPreview_DraftPage_404(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)
	statusPageID := createPublicStatusPageFixture(t, pool)

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPublicStatusPreview_UnknownID_404(t *testing.T) {
	r, _, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getPublicStatusPreview(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
