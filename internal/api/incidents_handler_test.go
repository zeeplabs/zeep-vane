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

func newIncidentsRouter(t *testing.T) (http.Handler, *db.Pool) {
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

	repo := db.NewIncidentRepository(pool)
	handler := NewIncidentsHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret))
		protected.Post("/api/incidents", handler.Create)
		protected.Post("/api/incidents/{id}/updates", handler.AddUpdate)
	})

	return r, pool
}

// createIncidentTestService inserts a service to link an incident to,
// registering cleanup.
func createIncidentTestService(t *testing.T, pool *db.Pool) string {
	t.Helper()
	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: uniqueServiceName(t), SLOID: "slo-incidents-test"}
	if err := services.Create(context.Background(), service); err != nil {
		t.Fatalf("setup Create() service returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })
	return service.ID
}

func postCreateIncident(t *testing.T, r http.Handler, token, title string, serviceIDs []string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(createIncidentRequest{Title: title, ServiceIDs: serviceIDs})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/incidents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateIncident_ValidRequest_201LinksServices(t *testing.T) {
	r, pool := newIncidentsRouter(t)
	token := issueTestSessionToken(t, "admin-1")
	serviceID := createIncidentTestService(t, pool)

	rec := postCreateIncident(t, r, token, "database latency spike", []string{serviceID})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE title = $1", "database latency spike")
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created incidentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.Status != "investigating" {
		t.Errorf("Status = %q, want %q", created.Status, "investigating")
	}
	if created.ResolvedAt != nil {
		t.Errorf("ResolvedAt = %v, want nil", created.ResolvedAt)
	}

	var linkedServiceID string
	row := pool.QueryRow(context.Background(), "SELECT service_id FROM incident_services WHERE incident_id = $1", created.ID)
	if err := row.Scan(&linkedServiceID); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if linkedServiceID != serviceID {
		t.Errorf("linked service_id = %q, want %q", linkedServiceID, serviceID)
	}
}

func TestCreateIncident_NoAuth_401(t *testing.T) {
	r, _ := newIncidentsRouter(t)

	rec := postCreateIncident(t, r, "", "unauthorized incident", []string{"any-id"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func postIncidentUpdate(t *testing.T, r http.Handler, token, incidentID, body string) *httptest.ResponseRecorder {
	t.Helper()
	reqBody, err := json.Marshal(addIncidentUpdateRequest{Body: body})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/incidents/"+incidentID+"/updates", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAddIncidentUpdate_TwoUpdates_TimelineOrderedMostRecentFirst(t *testing.T) {
	r, pool := newIncidentsRouter(t)
	token := issueTestSessionToken(t, "admin-1")
	serviceID := createIncidentTestService(t, pool)

	createRec := postCreateIncident(t, r, token, "timeline ordering test incident", []string{serviceID})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var incident incidentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &incident); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	firstRec := postIncidentUpdate(t, r, token, incident.ID, "investigating the root cause")
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first update status = %d, want %d, body = %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}

	secondRec := postIncidentUpdate(t, r, token, incident.ID, "root cause identified, rolling out fix")
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("second update status = %d, want %d, body = %s", secondRec.Code, http.StatusCreated, secondRec.Body.String())
	}

	var timeline []incidentUpdateResponse
	if err := json.Unmarshal(secondRec.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if len(timeline) != 2 {
		t.Fatalf("len(timeline) = %d, want %d", len(timeline), 2)
	}
	if timeline[0].Body != "root cause identified, rolling out fix" {
		t.Errorf("timeline[0].Body = %q, want the most recent update first", timeline[0].Body)
	}
	if timeline[1].Body != "investigating the root cause" {
		t.Errorf("timeline[1].Body = %q, want the oldest update last", timeline[1].Body)
	}
}

func TestAddIncidentUpdate_IncidentNotFound_404(t *testing.T) {
	r, _ := newIncidentsRouter(t)
	token := issueTestSessionToken(t, "admin-1")

	rec := postIncidentUpdate(t, r, token, "00000000-0000-0000-0000-000000000000", "update on a nonexistent incident")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
