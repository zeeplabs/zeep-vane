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
		protected.Patch("/api/status-pages/{id}/domain", handler.AttachDomain)
		protected.Patch("/api/status-pages/{id}/services", handler.SetServices)
		protected.Delete("/api/status-pages/{id}", handler.Delete)
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

func getListStatusPagesPage(t *testing.T, r http.Handler, token string, page int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/status-pages?page=%d", page), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// findStatusPageAcrossPages pages through every page of GET
// /api/status-pages looking for id - the shared dev DB this integration
// suite runs against can accumulate status pages across many tests, so
// page 1 alone is not guaranteed to include one created just now (PAG-08,
// same reasoning as domains/services handler tests).
func findStatusPageAcrossPages(t *testing.T, r http.Handler, token, id string) bool {
	t.Helper()
	for page := 1; ; page++ {
		rec := getListStatusPagesPage(t, r, token, page)
		if rec.Code != http.StatusOK {
			t.Fatalf("page=%d status = %d, want %d, body = %s", page, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got Page[statusPageResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
		}
		for _, sp := range got.Items {
			if sp.ID == id {
				return true
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return false
		}
	}
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

	var page Page[statusPageResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (default)", page.Page)
	}
	if page.PageSize != 20 {
		t.Errorf("page.PageSize = %d, want 20", page.PageSize)
	}

	if !findStatusPageAcrossPages(t, r, token, createdID) {
		t.Errorf("created status page %q not found across any page of GET /api/status-pages", createdID)
	}
}

func TestListStatusPages_InvalidPage_ClampsToPage1(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getListStatusPagesPage(t, r, token, -1)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[statusPageResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (clamped from invalid ?page=-1)", page.Page)
	}
}

func TestListStatusPages_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getListStatusPagesPage(t, r, token, 999999)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[statusPageResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("len(page.Items) = %d, want 0 for a page far beyond the last", len(page.Items))
	}
}

func TestListStatusPages_NoAuth_401(t *testing.T) {
	r, _, _ := newStatusPagesRouter(t)

	rec := getListStatusPages(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestCreateStatusPage_NoDomain_201NullDomainAndSubdomain asserts SPD-01:
// creating a status page with no domain_id/subdomain (only name) succeeds
// and both fields are null in the response.
func TestCreateStatusPage_NoDomain_201NullDomainAndSubdomain(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "No Domain Page"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	cleanupStatusPage(t, pool, created.ID)

	if created.DomainID != nil {
		t.Errorf("DomainID = %v, want nil", *created.DomainID)
	}
	if created.Subdomain != nil {
		t.Errorf("Subdomain = %v, want nil", *created.Subdomain)
	}
	if created.State != "draft" {
		t.Errorf("State = %q, want %q", created.State, "draft")
	}
}

// TestCreateStatusPage_OnlySubdomainSet_422 asserts SPD-05: giving
// subdomain without domain_id is rejected - a meaningless partial
// combination (design.md Data Models).
func TestCreateStatusPage_OnlySubdomainSet_422(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "Partial Page", Subdomain: "status"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestCreateStatusPage_OnlyDomainIDSet_422 asserts SPD-05: giving
// domain_id without subdomain is rejected the same way as the reverse
// partial combination.
func TestCreateStatusPage_OnlyDomainIDSet_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "Partial Page", DomainID: domainID})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestCreateStatusPage_EmptyName_422 asserts the pre-existing name
// requirement (design.md: name is required) still holds after relaxing
// the domain/subdomain requirement.
func TestCreateStatusPage_EmptyName_422(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// createDomainlessStatusPageViaAPI creates a domain-less status page
// through the handler under test and registers its cleanup, returning
// its ID.
func createDomainlessStatusPageViaAPI(t *testing.T, r http.Handler, pool *db.Pool, token, name string) string {
	t.Helper()
	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	id := decodeStatusPageID(t, rec)
	cleanupStatusPage(t, pool, id)
	return id
}

func patchAttachDomain(t *testing.T, r http.Handler, token, statusPageID string, req attachDomainRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPatch, "/api/status-pages/"+statusPageID+"/domain", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

// TestAttachDomain_ValidRequest_200ReflectsNewDomainAndSubdomain asserts
// SPD-06: a valid attach on a domain-less page succeeds and the response
// reflects the newly set domain_id/subdomain.
func TestAttachDomain_ValidRequest_200ReflectsNewDomainAndSubdomain(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Handler Page")

	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.DomainID == nil || *updated.DomainID != domainID {
		t.Errorf("DomainID = %v, want %q", updated.DomainID, domainID)
	}
	if updated.Subdomain == nil || *updated.Subdomain != "status" {
		t.Errorf("Subdomain = %v, want %q", updated.Subdomain, "status")
	}
}

// TestAttachDomain_AlreadyAttached_409 asserts SPD-07.
func TestAttachDomain_AlreadyAttached_409(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	firstDomainID := createTestDomain(t, pool)
	secondDomainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Already Page")

	firstRec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: firstDomainID, Subdomain: "status"})
	if firstRec.Code != http.StatusOK {
		t.Fatalf("setup attach status = %d, want %d, body = %s", firstRec.Code, http.StatusOK, firstRec.Body.String())
	}

	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: secondDomainID, Subdomain: "other"})
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

// TestAttachDomain_EmptySubdomain_422 asserts SPD-08.
func TestAttachDomain_EmptySubdomain_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Empty Subdomain Page")

	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestAttachDomain_NonexistentDomainID_422 asserts SPD-07.
func TestAttachDomain_NonexistentDomainID_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Invalid Domain Page")

	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{
		DomainID: "00000000-0000-0000-0000-000000000000", Subdomain: "status",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestAttachDomain_DuplicatePair_409 asserts SPD-09.
func TestAttachDomain_DuplicatePair_409(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)

	takenPageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Duplicate Taken Page")
	takenRec := patchAttachDomain(t, r, token, takenPageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if takenRec.Code != http.StatusOK {
		t.Fatalf("setup attach status = %d, want %d, body = %s", takenRec.Code, http.StatusOK, takenRec.Body.String())
	}

	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Attach Duplicate Colliding Page")
	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

// TestAttachDomain_NonexistentStatusPageID_404 asserts the not-found
// outcome for the attach endpoint itself (design.md Error Handling
// Strategy).
func TestAttachDomain_NonexistentStatusPageID_404(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)

	rec := patchAttachDomain(t, r, token, "00000000-0000-0000-0000-000000000000", attachDomainRequest{
		DomainID: domainID, Subdomain: "status",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func patchSetServices(t *testing.T, r http.Handler, token, statusPageID string, req setServicesRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPatch, "/api/status-pages/"+statusPageID+"/services", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

// TestSetServices_ReplacesLinkedSet asserts SPD-15: PATCHing services
// replaces the full set (drops what isn't in the new list, adds what is),
// and the response reflects the new set immediately - no separate reload
// needed by the caller.
func TestSetServices_ReplacesLinkedSet(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceA := createTestService(t, pool)
	serviceB := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Set Services Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{ServiceIDs: []string{serviceA}})
	if rec.Code != http.StatusOK {
		t.Fatalf("first PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var first statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(first.ServiceIDs) != 1 || first.ServiceIDs[0] != serviceA {
		t.Fatalf("ServiceIDs after first PATCH = %v, want [%q]", first.ServiceIDs, serviceA)
	}

	rec = patchSetServices(t, r, token, pageID, setServicesRequest{ServiceIDs: []string{serviceB}})
	if rec.Code != http.StatusOK {
		t.Fatalf("second PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var second statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(second.ServiceIDs) != 1 || second.ServiceIDs[0] != serviceB {
		t.Fatalf("ServiceIDs after second PATCH = %v, want [%q] (serviceA should be dropped)", second.ServiceIDs, serviceB)
	}
}

// TestSetServices_EmptyServiceIDs_200UnlinksAll asserts an empty array is
// a valid request body (unlink everything), not a validation error - only
// a missing/null field is rejected.
func TestSetServices_EmptyServiceIDs_200UnlinksAll(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceA := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Set Services Unlink Page")
	cleanupStatusPage(t, pool, pageID)

	if rec := patchSetServices(t, r, token, pageID, setServicesRequest{ServiceIDs: []string{serviceA}}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{ServiceIDs: []string{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(resp.ServiceIDs) != 0 {
		t.Errorf("ServiceIDs = %v, want empty", resp.ServiceIDs)
	}
}

// TestSetServices_MissingServiceIDs_422 asserts a request body with no
// service_ids field at all is rejected, distinguishing it from the valid
// "empty array" unlink-everything case above.
func TestSetServices_MissingServiceIDs_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Set Services Missing Field Page")
	cleanupStatusPage(t, pool, pageID)

	httpReq := httptest.NewRequest(http.MethodPatch, "/api/status-pages/"+pageID+"/services", bytes.NewReader([]byte(`{}`)))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestSetServices_NonexistentStatusPageID_404 mirrors
// TestAttachDomain_NonexistentStatusPageID_404 for the services endpoint.
func TestSetServices_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchSetServices(t, r, token, "00000000-0000-0000-0000-000000000000", setServicesRequest{ServiceIDs: []string{}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func deleteStatusPage(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/status-pages/"+id, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDeleteStatusPage_Existing_204RemovesItAndItsServiceLinks(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)

	createRec := postCreateStatusPage(t, r, token, createStatusPageRequest{
		Name: "To Be Deleted", ServiceIDs: []string{serviceID},
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	id := decodeStatusPageID(t, createRec)

	rec := deleteStatusPage(t, r, token, id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var count int
	row := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM status_pages WHERE id = $1", id)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("status_pages row still exists after delete")
	}

	row = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM status_page_services WHERE status_page_id = $1", id)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("status_page_services rows still exist after delete")
	}
}

func TestDeleteStatusPage_NotFound_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := deleteStatusPage(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteStatusPage_NoAuth_401(t *testing.T) {
	r, _, _ := newStatusPagesRouter(t)

	rec := deleteStatusPage(t, r, "", "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
