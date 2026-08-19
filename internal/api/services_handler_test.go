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

	var services []serviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &services); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	var found *serviceResponse
	for i := range services {
		if services[i].Name == name {
			found = &services[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("created service %q not present in list response", name)
	}
	if found.CurrentStatus != "not_configured" {
		t.Errorf("CurrentStatus = %q, want %q", found.CurrentStatus, "not_configured")
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
