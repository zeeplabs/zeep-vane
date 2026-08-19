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

func newPublicStatusRouter(t *testing.T) (http.Handler, *db.Pool) {
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
	handler := NewPublicStatusHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/", handler.Get)

	return r, pool
}

func TestPublicStatusGet_NoAuthHeader_200WithServiceStatus(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: name, SLOID: "slo-public-test"}
	if err := services.Create(context.Background(), service); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(context.Background(), service.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}

	// Deliberately no Authorization header at all - the public endpoint
	// must be reachable by an anonymous visitor (SP-10).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	var found *publicServiceResponse
	for i := range body.Services {
		if body.Services[i].Name == name {
			found = &body.Services[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("service %q not present in public response", name)
	}
	if found.Status != "operational" {
		t.Errorf("Status = %q, want %q", found.Status, "operational")
	}
	if found.LastUpdatedAt.IsZero() {
		t.Error("LastUpdatedAt is zero, want a real last-update timestamp")
	}
}
