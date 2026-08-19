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

func newIncidentsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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
	admins := db.NewAdminRepository(pool)
	handler := NewIncidentsHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Post("/api/incidents", handler.Create)
		protected.Post("/api/incidents/{id}/updates", handler.AddUpdate)
		protected.Patch("/api/incidents/{id}", handler.Transition)
	})

	return r, pool, admins
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
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
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
	r, _, _ := newIncidentsRouter(t)

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
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
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
	r, _, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postIncidentUpdate(t, r, token, "00000000-0000-0000-0000-000000000000", "update on a nonexistent incident")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func patchIncidentStatus(t *testing.T, r http.Handler, token, incidentID, status string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(transitionIncidentRequest{Status: status})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/incidents/"+incidentID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func createTestIncident(t *testing.T, r http.Handler, pool *db.Pool, token, title string) incidentResponse {
	t.Helper()
	serviceID := createIncidentTestService(t, pool)

	createRec := postCreateIncident(t, r, token, title, []string{serviceID})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var incident incidentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &incident); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	return incident
}

func TestTransitionIncident_ToResolved_SetsResolvedAt(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	incident := createTestIncident(t, r, pool, token, "transition to resolved test incident")

	rec := patchIncidentStatus(t, r, token, incident.ID, "resolved")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated incidentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.Status != "resolved" {
		t.Errorf("Status = %q, want %q", updated.Status, "resolved")
	}
	if updated.ResolvedAt == nil {
		t.Error("ResolvedAt = nil, want a timestamp set on resolution")
	}
}

// TestTransitionIncident_ReopenAfterResolved_AllowedAndRecordedOnTimeline
// covers SP-20: moving a resolved incident back to an earlier state (e.g.
// "investigating") is a legitimate reopening, not rejected, and must be
// recorded on the timeline with a timestamp.
func TestTransitionIncident_ReopenAfterResolved_AllowedAndRecordedOnTimeline(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	incident := createTestIncident(t, r, pool, token, "reopen after resolved test incident")

	resolveRec := patchIncidentStatus(t, r, token, incident.ID, "resolved")
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("setup resolve status = %d, want %d, body = %s", resolveRec.Code, http.StatusOK, resolveRec.Body.String())
	}

	reopenRec := patchIncidentStatus(t, r, token, incident.ID, "investigating")
	if reopenRec.Code != http.StatusOK {
		t.Fatalf("reopen status = %d, want %d, body = %s", reopenRec.Code, http.StatusOK, reopenRec.Body.String())
	}

	var reopened incidentResponse
	if err := json.Unmarshal(reopenRec.Body.Bytes(), &reopened); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if reopened.Status != "investigating" {
		t.Errorf("Status = %q, want %q", reopened.Status, "investigating")
	}
	if reopened.ResolvedAt != nil {
		t.Errorf("ResolvedAt = %v, want nil after reopening", reopened.ResolvedAt)
	}

	var timelineCount int
	row := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM incident_updates WHERE incident_id = $1 AND body = $2",
		incident.ID, "Status changed to investigating")
	if err := row.Scan(&timelineCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if timelineCount != 1 {
		t.Errorf("timeline entries for reopening = %d, want %d", timelineCount, 1)
	}

	var reopenedAt time.Time
	row = pool.QueryRow(context.Background(),
		"SELECT created_at FROM incident_updates WHERE incident_id = $1 AND body = $2",
		incident.ID, "Status changed to investigating")
	if err := row.Scan(&reopenedAt); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if reopenedAt.IsZero() {
		t.Error("reopening timeline entry has zero timestamp, want a real one")
	}
}
