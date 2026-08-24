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
	// UPT-08: the preview endpoint must return the identical hourly-history
	// shape as production - same field, same bucket count.
	if len(found.HourlyHistory) != 24 {
		t.Errorf("len(HourlyHistory) = %d, want %d", len(found.HourlyHistory), 24)
	}
}

// TestPublicStatusPreview_ZeroSnapshotService_AllHourlyBucketsNoData covers
// UPT-06/08: a never-polled service previews the same all-no_data history
// an admin's visitors would see on the real public page.
func TestPublicStatusPreview_ZeroSnapshotService_AllHourlyBucketsNoData(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: uniqueServiceName(t), SLOID: "slo-preview-no-snapshot-test"}
	if err := services.Create(ctx, service); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(ctx, service.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })
	statusPageID := createPublicStatusPageFixture(t, pool, service.ID)

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, service.ID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in preview response", service.ID)
	}
	if len(found.HourlyHistory) != 24 {
		t.Fatalf("len(HourlyHistory) = %d, want %d", len(found.HourlyHistory), 24)
	}
	for i, bucket := range found.HourlyHistory {
		if bucket.Status != "no_data" {
			t.Errorf("HourlyHistory[%d].Status = %q, want %q", i, bucket.Status, "no_data")
		}
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

// TestPublicStatusPreview_DraftPageWithDomain_200 asserts AD-008 (SPD-03):
// a status page in "draft" state with a domain already attached now
// composes successfully - previously (I12) this 404'd to mirror
// router.HostRouter's own production gate, but that gate is deliberately
// removed here so an admin can preview before DNS/TLS resolve. REWRITTEN
// from the prior TestPublicStatusPreview_DraftPage_404 (asserted 404),
// not deleted: the behavior it tested was the exact bug AD-008 fixes.
func TestPublicStatusPreview_DraftPageWithDomain_200(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)
	statusPageID := createPublicStatusPageFixture(t, pool)

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestPublicStatusPreview_DraftPageNoDomain_200 asserts SPD-02: a status
// page with domain_id: null (never had a domain attached at all) composes
// successfully - the core bug this feature fixes.
func TestPublicStatusPreview_DraftPageNoDomain_200(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "preview-no-domain-page"}
	if err := statusPages.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	rec := getPublicStatusPreview(t, r, token, statusPage.ID)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestPublicStatusPreview_PublishedPage_200Unaffected asserts SPD-04: a
// published page's preview is unaffected by removing the gate (it already
// composed successfully before this change, via
// TestPublicStatusPreview_AuthenticatedByID_200SameShapeAsProduction -
// this test targets the state explicitly, without asserting on the
// response body shape covered there).
func TestPublicStatusPreview_PublishedPage_200Unaffected(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)
	statusPageID := createPublicStatusPageFixture(t, pool)
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET state = 'published' WHERE id = $1", statusPageID); err != nil {
		t.Fatalf("setup publish update returned unexpected error: %v", err)
	}

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestPublicStatusPreview_TLSFailedPage_200Unaffected asserts SPD-04: a
// tls_failed page's preview is unaffected by removing the gate.
func TestPublicStatusPreview_TLSFailedPage_200Unaffected(t *testing.T) {
	r, pool, admins := newPublicStatusPreviewRouter(t)
	token := issueTestSessionToken(t, admins)
	statusPageID := createPublicStatusPageFixture(t, pool)
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET state = 'tls_failed' WHERE id = $1", statusPageID); err != nil {
		t.Fatalf("setup tls_failed update returned unexpected error: %v", err)
	}

	rec := getPublicStatusPreview(t, r, token, statusPageID)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
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
