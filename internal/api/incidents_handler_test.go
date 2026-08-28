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
		protected.Get("/api/incidents", handler.List)
		protected.Post("/api/incidents/{id}/updates", handler.AddUpdate)
		protected.Get("/api/incidents/{id}/updates", handler.ListUpdates)
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

func getIncidents(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	return getIncidentsPage(t, r, token, "")
}

// getIncidentsPage issues GET /api/incidents, appending ?page=rawPage to the
// URL verbatim when rawPage is non-empty (so tests can exercise invalid
// values like "abc", "0", "-1").
func getIncidentsPage(t *testing.T, r http.Handler, token, rawPage string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/incidents"
	if rawPage != "" {
		url += "?page=" + rawPage
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getIncidentUpdates(t *testing.T, r http.Handler, token, incidentID string) *httptest.ResponseRecorder {
	t.Helper()
	return getIncidentUpdatesPage(t, r, token, incidentID, "")
}

// getIncidentUpdatesPage issues GET /api/incidents/{id}/updates, appending
// ?page=rawPage to the URL verbatim when rawPage is non-empty.
func getIncidentUpdatesPage(t *testing.T, r http.Handler, token, incidentID, rawPage string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/incidents/" + incidentID + "/updates"
	if rawPage != "" {
		url += "?page=" + rawPage
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestListIncidents_ReturnsMostRecentFirstWithServiceIDs(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createIncidentTestService(t, pool)

	firstRec := postCreateIncident(t, r, token, "list order test incident A", []string{serviceID})
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup create A status = %d, want %d, body = %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}
	var first incidentResponse
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", first.ID) })

	secondRec := postCreateIncident(t, r, token, "list order test incident B", []string{serviceID})
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("setup create B status = %d, want %d, body = %s", secondRec.Code, http.StatusCreated, secondRec.Body.String())
	}
	var second incidentResponse
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", second.ID) })

	rec := getIncidents(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[incidentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	listed := page.Items
	if len(listed) < 2 {
		t.Fatalf("len(listed) = %d, want at least 2", len(listed))
	}
	if listed[0].ID != second.ID {
		t.Errorf("listed[0].ID = %q, want %q (most recently created first)", listed[0].ID, second.ID)
	}
	if listed[1].ID != first.ID {
		t.Errorf("listed[1].ID = %q, want %q", listed[1].ID, first.ID)
	}
	if len(listed[0].ServiceIDs) != 1 || listed[0].ServiceIDs[0] != serviceID {
		t.Errorf("listed[0].ServiceIDs = %v, want [%q]", listed[0].ServiceIDs, serviceID)
	}
	if page.PageSize != 25 {
		t.Errorf("page_size = %d, want 25", page.PageSize)
	}
	if page.Page != 1 {
		t.Errorf("page = %d, want 1 (default, no ?page= given)", page.Page)
	}
}

func TestListIncidents_NoAuth_401(t *testing.T) {
	r, _, _ := newIncidentsRouter(t)

	rec := getIncidents(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestListIncidentUpdates_ReturnsTimelineMostRecentFirst(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	incident := createTestIncident(t, r, pool, token, "list updates test incident")

	firstRec := postIncidentUpdate(t, r, token, incident.ID, "first update")
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup first update status = %d, want %d, body = %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}
	secondRec := postIncidentUpdate(t, r, token, incident.ID, "second update")
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("setup second update status = %d, want %d, body = %s", secondRec.Code, http.StatusCreated, secondRec.Body.String())
	}

	rec := getIncidentUpdates(t, r, token, incident.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[incidentUpdateResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	timeline := page.Items
	if len(timeline) != 2 {
		t.Fatalf("len(timeline) = %d, want %d", len(timeline), 2)
	}
	if timeline[0].Body != "second update" {
		t.Errorf("timeline[0].Body = %q, want the most recent update first", timeline[0].Body)
	}
	if timeline[1].Body != "first update" {
		t.Errorf("timeline[1].Body = %q, want the oldest update last", timeline[1].Body)
	}
	if page.Total != 2 {
		t.Errorf("total = %d, want 2", page.Total)
	}
	if page.PageSize != 25 {
		t.Errorf("page_size = %d, want 25", page.PageSize)
	}
}

func TestListIncidentUpdates_UnknownIncident_404(t *testing.T) {
	r, _, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getIncidentUpdates(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestListIncidentUpdates_NoAuth_401(t *testing.T) {
	r, _, _ := newIncidentsRouter(t)

	rec := getIncidentUpdates(t, r, "", "any-id")
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

func TestTransitionIncident_UnknownIncident_404(t *testing.T) {
	r, _, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchIncidentStatus(t, r, token, "00000000-0000-0000-0000-000000000000", "resolved")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
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

// seedHandlerTestIncidents inserts n incidents directly via the repository
// (bypassing HTTP for speed), each named prefix-0..prefix-(n-1), registering
// cleanup for every one.
func seedHandlerTestIncidents(t *testing.T, pool *db.Pool, prefix string, n int) []string {
	t.Helper()
	repo := db.NewIncidentRepository(pool)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		incident := &db.Incident{Title: prefix + "-" + string(rune('a'+i%26))}
		if err := repo.Create(context.Background(), incident, nil); err != nil {
			t.Fatalf("seed incident Create() returned unexpected error: %v", err)
		}
		ids[i] = incident.ID
		t.Cleanup(func(id string) func() {
			return func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", id) }
		}(incident.ID))
		time.Sleep(time.Millisecond)
	}
	return ids
}

// TestListIncidents_Page2_ReturnsRemainderNoOverlapWithPage1 covers PAG-01:
// GET /api/incidents?page=2 returns the incidents past the first page's 25,
// with no overlap between the two pages.
func TestListIncidents_Page2_ReturnsRemainderNoOverlapWithPage1(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	seedHandlerTestIncidents(t, pool, "handler-page2-test", 27)

	rec1 := getIncidentsPage(t, r, token, "1")
	if rec1.Code != http.StatusOK {
		t.Fatalf("page=1 status = %d, want %d, body = %s", rec1.Code, http.StatusOK, rec1.Body.String())
	}
	var page1 Page[incidentResponse]
	if err := json.Unmarshal(rec1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("json.Unmarshal() (page 1) returned unexpected error: %v", err)
	}
	if len(page1.Items) != 25 {
		t.Fatalf("len(page1.Items) = %d, want 25", len(page1.Items))
	}

	rec2 := getIncidentsPage(t, r, token, "2")
	if rec2.Code != http.StatusOK {
		t.Fatalf("page=2 status = %d, want %d, body = %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
	var page2 Page[incidentResponse]
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("json.Unmarshal() (page 2) returned unexpected error: %v", err)
	}
	if page2.Total != page1.Total {
		t.Errorf("total differs between page 1 (%d) and page 2 (%d), want equal", page1.Total, page2.Total)
	}
	if page2.Page != 2 {
		t.Errorf("page = %d, want 2", page2.Page)
	}

	seen := map[string]bool{}
	for _, inc := range page1.Items {
		seen[inc.ID] = true
	}
	for _, inc := range page2.Items {
		if seen[inc.ID] {
			t.Errorf("incident %s appeared on both page 1 and page 2", inc.ID)
		}
	}
	if len(page2.Items) == 0 {
		t.Error("page 2 returned no items, want at least the 2 remaining seeded incidents")
	}
}

// TestListIncidents_InvalidPage_ClampsToPageOne_200 covers PAG-03: an
// invalid ?page= (non-numeric, zero, or negative) clamps to page 1 and
// responds 200, never 400.
func TestListIncidents_InvalidPage_ClampsToPageOne_200(t *testing.T) {
	r, _, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)

	for _, rawPage := range []string{"abc", "0", "-1"} {
		rec := getIncidentsPage(t, r, token, rawPage)
		if rec.Code != http.StatusOK {
			t.Fatalf("?page=%s status = %d, want %d, body = %s", rawPage, rec.Code, http.StatusOK, rec.Body.String())
		}
		var page Page[incidentResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("?page=%s json.Unmarshal() returned unexpected error: %v", rawPage, err)
		}
		if page.Page != 1 {
			t.Errorf("?page=%s -> page = %d, want 1 (clamped)", rawPage, page.Page)
		}
	}
}

// TestListIncidents_PageBeyondLast_EmptyItems200 covers PAG-04: a page
// number past the last page responds 200 with an empty items array and the
// correct total/page_size.
func TestListIncidents_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	seedHandlerTestIncidents(t, pool, "handler-beyond-test", 2)

	rec := getIncidentsPage(t, r, token, "9999")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[incidentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(page.Items))
	}
	if page.Total < 2 {
		t.Errorf("total = %d, want >= 2", page.Total)
	}
	if page.PageSize != 25 {
		t.Errorf("page_size = %d, want 25", page.PageSize)
	}
}

// TestListIncidentUpdates_Page2_ReturnsRemainderNoOverlapWithPage1 covers
// PAG-05: GET /api/incidents/{id}/updates?page=2 returns the correct slice
// for an incident with more than one page of updates.
func TestListIncidentUpdates_Page2_ReturnsRemainderNoOverlapWithPage1(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	incident := createTestIncident(t, r, pool, token, "handler-updates-page2-test")

	const seedCount = 27
	for i := 0; i < seedCount; i++ {
		rec := postIncidentUpdate(t, r, token, incident.ID, "update-"+string(rune('a'+i%26)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed update %d status = %d, want %d", i, rec.Code, http.StatusCreated)
		}
	}

	rec1 := getIncidentUpdatesPage(t, r, token, incident.ID, "1")
	if rec1.Code != http.StatusOK {
		t.Fatalf("page=1 status = %d, want %d, body = %s", rec1.Code, http.StatusOK, rec1.Body.String())
	}
	var page1 Page[incidentUpdateResponse]
	if err := json.Unmarshal(rec1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("json.Unmarshal() (page 1) returned unexpected error: %v", err)
	}
	if len(page1.Items) != 25 {
		t.Fatalf("len(page1.Items) = %d, want 25", len(page1.Items))
	}
	if page1.Total != seedCount {
		t.Errorf("total = %d, want %d", page1.Total, seedCount)
	}

	rec2 := getIncidentUpdatesPage(t, r, token, incident.ID, "2")
	if rec2.Code != http.StatusOK {
		t.Fatalf("page=2 status = %d, want %d, body = %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
	var page2 Page[incidentUpdateResponse]
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("json.Unmarshal() (page 2) returned unexpected error: %v", err)
	}
	if len(page2.Items) != seedCount-25 {
		t.Errorf("len(page2.Items) = %d, want %d", len(page2.Items), seedCount-25)
	}
	if page2.Total != seedCount {
		t.Errorf("total (page 2) = %d, want %d", page2.Total, seedCount)
	}
}

// TestListIncidentUpdates_InvalidPage_ClampsToPageOne_200 covers PAG-03
// applied to the incident-updates endpoint: an invalid ?page= clamps to
// page 1 and responds 200.
func TestListIncidentUpdates_InvalidPage_ClampsToPageOne_200(t *testing.T) {
	r, pool, admins := newIncidentsRouter(t)
	token := issueTestSessionToken(t, admins)
	incident := createTestIncident(t, r, pool, token, "handler-updates-invalid-page-test")

	for _, rawPage := range []string{"abc", "0", "-1"} {
		rec := getIncidentUpdatesPage(t, r, token, incident.ID, rawPage)
		if rec.Code != http.StatusOK {
			t.Fatalf("?page=%s status = %d, want %d, body = %s", rawPage, rec.Code, http.StatusOK, rec.Body.String())
		}
		var page Page[incidentUpdateResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("?page=%s json.Unmarshal() returned unexpected error: %v", rawPage, err)
		}
		if page.Page != 1 {
			t.Errorf("?page=%s -> page = %d, want 1 (clamped)", rawPage, page.Page)
		}
	}
}
