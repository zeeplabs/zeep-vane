//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newServicesRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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

	repo := db.NewServiceRepository(pool)
	admins := db.NewAdminRepository(pool)
	handler := NewServicesHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Post("/api/services", handler.Create)
		protected.Get("/api/services", handler.List)
	})

	return r, pool, admins
}

func uniqueServiceName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("services-handler-test-%d", time.Now().UnixNano())
}

func postCreateService(t *testing.T, r http.Handler, token, name, sloID string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(createServiceRequest{Name: name, SLOID: sloID})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getServices(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getServicesPage(t *testing.T, r http.Handler, token string, page int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/services?page=%d", page), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// findServiceAcrossPages pages through every page of GET /api/services
// looking for name - the shared dev DB this integration suite runs against
// can accumulate services across many tests, so page 1 alone is not
// guaranteed to include one created just now (same reasoning as domains
// handler's findDomainAcrossPages, PAG-08).
func findServiceAcrossPages(t *testing.T, r http.Handler, token, name string) *serviceResponse {
	t.Helper()
	for page := 1; ; page++ {
		rec := getServicesPage(t, r, token, page)
		if rec.Code != http.StatusOK {
			t.Fatalf("page=%d status = %d, want %d, body = %s", page, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got Page[serviceResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
		}
		for i := range got.Items {
			if got.Items[i].Name == name {
				return &got.Items[i]
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return nil
		}
	}
}

func TestCreateService_ValidRequest_201SavesSLOLink(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateService(t, r, token, name, "slo-abc-123")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created serviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.SLOID != "slo-abc-123" {
		t.Errorf("response SLOID = %q, want %q", created.SLOID, "slo-abc-123")
	}

	var storedSLOID string
	row := pool.QueryRow(context.Background(), "SELECT slo_id FROM services WHERE name = $1", name)
	if err := row.Scan(&storedSLOID); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if storedSLOID != "slo-abc-123" {
		t.Errorf("stored slo_id = %q, want %q", storedSLOID, "slo-abc-123")
	}
}

func TestListServices_ReturnsAllWithCurrentStatus(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	createRec := postCreateService(t, r, token, name, "slo-xyz-789")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	rec := getServices(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (default)", page.Page)
	}
	if page.PageSize != 20 {
		t.Errorf("page.PageSize = %d, want 20", page.PageSize)
	}

	found := findServiceAcrossPages(t, r, token, name)
	if found == nil {
		t.Fatalf("created service %q not present across any page of GET /api/services", name)
	}
	if found.CurrentStatus != "not_configured" {
		t.Errorf("CurrentStatus = %q, want %q", found.CurrentStatus, "not_configured")
	}
}

func TestListServices_InvalidPage_ClampsToPage1(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getServicesPage(t, r, token, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (clamped from invalid ?page=0)", page.Page)
	}
}

func TestListServices_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getServicesPage(t, r, token, 999999)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("len(page.Items) = %d, want 0 for a page far beyond the last", len(page.Items))
	}
}

func TestServicesRoutes_NoAuth_401(t *testing.T) {
	r, _, _ := newServicesRouter(t)

	createRec := postCreateService(t, r, "", "any-name", "any-slo")
	if createRec.Code != http.StatusUnauthorized {
		t.Errorf("POST status = %d, want %d", createRec.Code, http.StatusUnauthorized)
	}

	listRec := getServices(t, r, "")
	if listRec.Code != http.StatusUnauthorized {
		t.Errorf("GET status = %d, want %d", listRec.Code, http.StatusUnauthorized)
	}
}
