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

func newStatusPagesRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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

	repo := db.NewStatusPageRepository(pool)
	admins := db.NewAdminRepository(pool)
	handler := NewStatusPagesHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Post("/api/status-pages", handler.Create)
		protected.Get("/api/status-pages", handler.List)
	})

	return r, pool, admins
}

// createTestDomain inserts a domain fixture and registers its cleanup,
// returning the generated ID.
func createTestDomain(t *testing.T, pool *db.Pool) string {
	t.Helper()
	hostname := fmt.Sprintf("status-pages-handler-test-%d.example.com", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	var domainID string
	row := pool.QueryRow(context.Background(), "INSERT INTO domains (hostname) VALUES ($1) RETURNING id", hostname)
	if err := row.Scan(&domainID); err != nil {
		t.Fatalf("failed to insert domain fixture: %v", err)
	}
	return domainID
}

// createTestService inserts a service fixture and registers its cleanup,
// returning the generated ID.
func createTestService(t *testing.T, pool *db.Pool) string {
	t.Helper()
	name := fmt.Sprintf("status-pages-handler-test-service-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	var serviceID string
	row := pool.QueryRow(context.Background(),
		"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", name, "slo-fixture")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("failed to insert service fixture: %v", err)
	}
	return serviceID
}

// cleanupStatusPage registers deletion of statusPageID and its
// status_page_services rows, in FK-safe order (junction rows first).
func cleanupStatusPage(t *testing.T, pool *db.Pool, statusPageID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPageID)
		_, _ = pool.Exec(ctx, "DELETE FROM status_pages WHERE id = $1", statusPageID)
	})
}

func postCreateStatusPage(t *testing.T, r http.Handler, token string, req createStatusPageRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/status-pages", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

func TestCreateStatusPage_ValidRequest_201LinksDomainAndServices(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	serviceID := createTestService(t, pool)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "Main Status", Subdomain: "status", DomainID: domainID, ServiceIDs: []string{serviceID},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	cleanupStatusPage(t, pool, created.ID)
	if created.State != "draft" {
		t.Errorf("State = %q, want %q", created.State, "draft")
	}
	if created.DomainID == nil || *created.DomainID != domainID {
		t.Errorf("DomainID = %v, want %q", created.DomainID, domainID)
	}

	var linkedServiceID string
	row := pool.QueryRow(context.Background(),
		"SELECT service_id FROM status_page_services WHERE status_page_id = $1", created.ID)
	if err := row.Scan(&linkedServiceID); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if linkedServiceID != serviceID {
		t.Errorf("linked service_id = %q, want %q", linkedServiceID, serviceID)
	}
}

func TestCreateStatusPage_SecondPageSameDomain_NotBlocked(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)

	firstRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "Page One", Subdomain: "status-1", DomainID: domainID,
	})
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d, body = %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}
	cleanupStatusPage(t, pool, decodeStatusPageID(t, firstRec))

	secondRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "Page Two", Subdomain: "status-2", DomainID: domainID,
	})
	if secondRec.Code != http.StatusCreated {
		t.Errorf("second create status = %d, want %d, body = %s", secondRec.Code, http.StatusCreated, secondRec.Body.String())
	}
	cleanupStatusPage(t, pool, decodeStatusPageID(t, secondRec))
}

func TestCreateStatusPage_SecondRootDomain_NotBlocked(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	firstDomainID := createTestDomain(t, pool)
	secondDomainID := createTestDomain(t, pool)

	firstRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "Domain One Page", Subdomain: "status", DomainID: firstDomainID,
	})
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d, body = %s", firstRec.Code, http.StatusCreated, firstRec.Body.String())
	}
	cleanupStatusPage(t, pool, decodeStatusPageID(t, firstRec))

	secondRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "Domain Two Page", Subdomain: "status", DomainID: secondDomainID,
	})
	if secondRec.Code != http.StatusCreated {
		t.Errorf("second create status = %d, want %d, body = %s", secondRec.Code, http.StatusCreated, secondRec.Body.String())
	}
	cleanupStatusPage(t, pool, decodeStatusPageID(t, secondRec))
}

// decodeStatusPageID extracts the created status page's ID from rec's JSON
// body.
func decodeStatusPageID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	return resp.ID
}

func getListStatusPages(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/status-pages", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestListStatusPages_AnyRole_200IncludesCreated(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)

	createRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "List Test Page", Subdomain: "list-test", DomainID: domainID,
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	createdID := decodeStatusPageID(t, createRec)
	cleanupStatusPage(t, pool, createdID)

	rec := getListStatusPages(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var statusPages []statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &statusPages); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	found := false
	for _, sp := range statusPages {
		if sp.ID == createdID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() response does not include created status page %q", createdID)
	}
}

func TestListStatusPages_NoAuth_401(t *testing.T) {
	r, _, _ := newStatusPagesRouter(t)

	rec := getListStatusPages(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
