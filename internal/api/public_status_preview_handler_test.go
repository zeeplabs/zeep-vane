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
	inner := NewPublicStatusHandler(services, snapshots, incidents, zap.NewNop())
	handler := NewPublicStatusPreviewHandler(inner, zap.NewNop())

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

func TestPublicStatusPreview_NoAuth_401(t *testing.T) {
	r, pool, _ := newPublicStatusPreviewRouter(t)
	statusPageID := createPublicStatusPageFixture(t, pool)

	rec := getPublicStatusPreview(t, r, "", statusPageID)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
