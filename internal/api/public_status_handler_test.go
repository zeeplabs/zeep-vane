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

	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	handler := NewPublicStatusHandler(services, snapshots, incidents, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/", handler.Get)

	return r, pool
}

// createPublicStatusServiceFixture creates a service, sets its current
// status, and records a StatusSnapshot with a fixed, past fetchedAt (rather
// than "now") so tests can assert the exact timestamp the public endpoint
// exposes, not just that some recent-looking value came back.
func createPublicStatusServiceFixture(t *testing.T, pool *db.Pool, status string, fetchedAt time.Time) (serviceID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	name := uniqueServiceName(t)

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: name, SLOID: "slo-public-test"}
	if err := services.Create(ctx, service); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(ctx, service.ID, status); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO status_snapshots (service_id, status, error_budget_remaining, fetched_at) VALUES ($1, $2, $3, $4)",
		service.ID, status, 0.5, fetchedAt,
	); err != nil {
		t.Fatalf("setup snapshot insert returned unexpected error: %v", err)
	}

	return service.ID, func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) }
}

func findPublicService(services []publicServiceResponse, serviceID string, allServices []db.Service) *publicServiceResponse {
	var name string
	for _, s := range allServices {
		if s.ID == serviceID {
			name = s.Name
			break
		}
	}
	for i := range services {
		if services[i].Name == name {
			return &services[i]
		}
	}
	return nil
}

func TestPublicStatusGet_NoAuthHeader_200WithServiceStatus(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	fetchedAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", fetchedAt)
	t.Cleanup(cleanup)

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

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if found.Status != "operational" {
		t.Errorf("Status = %q, want %q", found.Status, "operational")
	}
	if !found.LastUpdatedAt.Equal(fetchedAt) {
		t.Errorf("LastUpdatedAt = %v, want %v (the recorded snapshot's fetched_at)", found.LastUpdatedAt, fetchedAt)
	}
}

// TestPublicStatusGet_IntegrationInvalid_StillServesLastSnapshot simulates
// the Datadog connection failing (Integration marked "invalid" after the
// poller exhausts its retries, T24) and confirms the public endpoint keeps
// serving the last valid cached status instead of erroring out (SP-08,
// SP-09).
func TestPublicStatusGet_IntegrationInvalid_StillServesLastSnapshot(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	fetchedAt := time.Date(2026, 2, 1, 8, 30, 0, 0, time.UTC)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "degraded", fetchedAt)
	t.Cleanup(cleanup)

	dsn := testDatabaseURL(t)
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	integrations := db.NewIntegrationRepository(pool)
	if err := integrations.UpsertDatadog(context.Background(), []byte("encrypted-key"), []byte("encrypted-app-key")); err != nil {
		t.Fatalf("setup UpsertDatadog() returned unexpected error: %v", err)
	}
	if err := integrations.MarkDatadogInvalid(context.Background(), "connection failure: exhausted retries"); err != nil {
		t.Fatalf("setup MarkDatadogInvalid() returned unexpected error: %v", err)
	}

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

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if found.Status != "degraded" {
		t.Errorf("Status = %q, want %q (last cached status, unaffected by integration failure)", found.Status, "degraded")
	}
	if !found.LastUpdatedAt.Equal(fetchedAt) {
		t.Errorf("LastUpdatedAt = %v, want %v (real last-success timestamp, never a fabricated now)", found.LastUpdatedAt, fetchedAt)
	}
}

// TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown covers
// the spec.md edge case: a service with no SLO linked yet stays
// "not_configured" on the admin side and must never appear in the public
// response, while a service with a real status appears normally alongside
// it.
func TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()

	configuredID, cleanupConfigured := createPublicStatusServiceFixture(t, pool, "operational", time.Now().UTC())
	t.Cleanup(cleanupConfigured)

	services := db.NewServiceRepository(pool)
	notConfiguredName := uniqueServiceName(t)
	notConfigured := &db.Service{Name: notConfiguredName, SLOID: "slo-not-configured-test"}
	if err := services.Create(ctx, notConfigured); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", notConfigured.ID) })
	if notConfigured.CurrentStatus != "not_configured" {
		t.Fatalf("setup: fresh service CurrentStatus = %q, want %q", notConfigured.CurrentStatus, "not_configured")
	}

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

	for _, s := range body.Services {
		if s.Name == notConfiguredName {
			t.Fatalf("not_configured service %q present in public response, want hidden", notConfiguredName)
		}
	}

	allServices, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if found := findPublicService(body.Services, configuredID, allServices); found == nil {
		t.Fatalf("configured service %s not present in public response, want shown", configuredID)
	}
}

// createPublicIncidentFixture creates an incident linked to a fresh service
// and returns its ID, registering cleanup.
func createPublicIncidentFixture(t *testing.T, pool *db.Pool, title string) string {
	t.Helper()
	ctx := context.Background()

	serviceID := createIncidentTestService(t, pool)
	incidents := db.NewIncidentRepository(pool)
	incident := &db.Incident{Title: title}
	if err := incidents.Create(ctx, incident, []string{serviceID}); err != nil {
		t.Fatalf("setup incident Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	return incident.ID
}

func findPublicIncident(incidents []publicIncidentResponse, incidentID string) *publicIncidentResponse {
	for i := range incidents {
		if incidents[i].ID == incidentID {
			return &incidents[i]
		}
	}
	return nil
}

// TestPublicStatusGet_UnresolvedIncident_AppearsInActive covers SP-18: an
// incident not yet resolved must appear in destaque (the "active" group) on
// the public status page.
func TestPublicStatusGet_UnresolvedIncident_AppearsInActive(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID := createPublicIncidentFixture(t, pool, "unresolved incident public test")

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

	if found := findPublicIncident(body.Incidents.Active, incidentID); found == nil {
		t.Fatalf("unresolved incident %s not present in active incidents, want it featured", incidentID)
	}
	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found != nil {
		t.Errorf("unresolved incident %s present in resolved incidents, want it absent", incidentID)
	}
}

// TestPublicStatusGet_ResolvedIncidentWithinRetention_AppearsInHistory
// covers SP-18/retention: an incident resolved less than 90 days ago must
// still appear in the public history.
func TestPublicStatusGet_ResolvedIncidentWithinRetention_AppearsInHistory(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID := createPublicIncidentFixture(t, pool, "recently resolved incident public test")

	if _, err := pool.Exec(context.Background(),
		"UPDATE incidents SET status = 'resolved', resolved_at = now() - interval '10 days' WHERE id = $1",
		incidentID,
	); err != nil {
		t.Fatalf("setup resolved_at update returned unexpected error: %v", err)
	}

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

	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found == nil {
		t.Fatalf("incident %s resolved 10 days ago not present in resolved history, want it within the 90-day window", incidentID)
	}
}

// TestPublicStatusGet_ResolvedIncidentBeyondRetention_Hidden covers the
// spec.md 90-day retention assumption: an incident resolved more than 90
// days ago must never appear on the public status page.
func TestPublicStatusGet_ResolvedIncidentBeyondRetention_Hidden(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID := createPublicIncidentFixture(t, pool, "long-resolved incident public test")

	if _, err := pool.Exec(context.Background(),
		"UPDATE incidents SET status = 'resolved', resolved_at = now() - interval '91 days' WHERE id = $1",
		incidentID,
	); err != nil {
		t.Fatalf("setup resolved_at update returned unexpected error: %v", err)
	}

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

	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found != nil {
		t.Errorf("incident %s resolved 91 days ago present in resolved history, want it hidden past the 90-day window", incidentID)
	}
	if found := findPublicIncident(body.Incidents.Active, incidentID); found != nil {
		t.Errorf("incident %s present in active incidents, want it hidden entirely", incidentID)
	}
}
