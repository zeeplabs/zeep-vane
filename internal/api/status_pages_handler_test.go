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

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeDomainVerifier lets VerifyDomain tests control the DNS/TLS check
// outcome without depending on real network access - a real domainVerifier
// dials the real internet (domain_verifier.go), which would make this
// suite flaky/slow and require a real publicly-resolvable hostname.
type fakeDomainVerifier struct {
	result domainVerificationResult
}

func (f *fakeDomainVerifier) Verify(ctx context.Context, hostname, expectedTarget string) domainVerificationResult {
	return f.result
}

func newStatusPagesRouter(t *testing.T, opts ...func(*StatusPagesHandler)) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()

	pool, _ := newAPITenantScopedPool(t)

	repo := db.NewStatusPageRepository(pool)
	admins := db.NewUserRepository(pool)
	handler := NewStatusPagesHandler(repo, audit.NewLog(pool), "", zap.NewNop())
	for _, opt := range opts {
		opt(handler)
	}

	// writeRoles/anyRole mirror internal/cli/routes.go's gating convention
	// for the group endpoints - the rest of this router's routes are left
	// ungated (pre-existing, not this task's concern) to avoid changing
	// the role requirements asserted by tests that predate T5.
	writeRoles := RequireRole(db.RoleOwner, db.RoleOperator)
	anyRole := RequireRole(db.RoleOwner, db.RoleOperator, db.RoleViewer)

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		// Mirrors buildAdminRouter: TenantContext runs right after
		// RequireAuth and is what resolves the caller's role in the active
		// tenant for RequireRole (multi-tenancy-core, AD-022).
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Post("/api/status-pages", handler.Create)
		protected.Get("/api/status-pages", handler.List)
		protected.Patch("/api/status-pages/{id}/domain", handler.AttachDomain)
		protected.Patch("/api/status-pages/{id}/environment", handler.UpdateEnvironment)
		protected.Patch("/api/status-pages/{id}/services", handler.SetServices)
		protected.Post("/api/status-pages/{id}/verify-domain", handler.VerifyDomain)
		protected.Delete("/api/status-pages/{id}", handler.Delete)
		protected.With(writeRoles).Post("/api/status-pages/{id}/groups", handler.CreateGroup)
		protected.With(writeRoles).Patch("/api/status-pages/{id}/groups/{groupId}", handler.UpdateGroup)
		protected.With(writeRoles).Delete("/api/status-pages/{id}/groups/{groupId}", handler.DeleteGroup)
		protected.With(anyRole).Get("/api/status-pages/{id}/groups", handler.ListGroups)
		protected.With(anyRole).Get("/api/status-pages/{id}/services", handler.ListServicePlacements)
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
	row := pool.QueryRow(context.Background(),
		"INSERT INTO domains (hostname, verification_token) VALUES ($1, $2) RETURNING id",
		hostname, "status-pages-handler-fixture-token")
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
	tenantID := seedTestTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		row := pool.QueryRow(ctx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", name, "slo-fixture")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("failed to insert service fixture: %v", err)
		}
	})
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

// TestCreateStatusPage_WithEnvironment_201PersistsValue asserts SPEB-01/02:
// a valid non-empty environment given on create is accepted and reflected
// in the response.
func TestCreateStatusPage_WithEnvironment_201PersistsValue(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "Staging Page", Environment: "staging"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	cleanupStatusPage(t, pool, created.ID)
	if created.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", created.Environment, "staging")
	}
}

// TestCreateStatusPage_OmittedEnvironment_201DefaultsToEmpty asserts the
// ""-means-absent convention (SPEB-01): omitting environment on create
// still succeeds and defaults to "" (no banner).
func TestCreateStatusPage_OmittedEnvironment_201DefaultsToEmpty(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "No Env Page"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	cleanupStatusPage(t, pool, created.ID)
	if created.Environment != "" {
		t.Errorf("Environment = %q, want empty", created.Environment)
	}
}

// TestCreateStatusPage_InvalidEnvironment_422 asserts SPEB-02: a value
// outside the fixed enum ("", "staging", "development") is rejected, same
// 422 convention as the service-environment-tag feature's validation.
func TestCreateStatusPage_InvalidEnvironment_422(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "Bad Env Page", Environment: "production"})
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
	// Regression guard: AttachDomain used to leave state at "draft", which
	// tls.HostPolicy permanently refuses to issue a certificate for -
	// every status page with a domain was stuck forever. See migration
	// 0020 and internal/tls/manager.go's draftState doc comment.
	if updated.State != "pending_tls" {
		t.Errorf("State = %q, want %q (a page must leave \"draft\" the moment a domain is attached, or tls.HostPolicy can never issue it a certificate)", updated.State, "pending_tls")
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

func patchUpdateEnvironment(t *testing.T, r http.Handler, token, statusPageID string, req updateStatusPageEnvironmentRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPatch, "/api/status-pages/"+statusPageID+"/environment", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

// TestUpdateEnvironment_ValidRequest_200ReflectsNewValue asserts SPEB-02:
// a valid PATCH sets the environment and the response reflects it.
func TestUpdateEnvironment_ValidRequest_200ReflectsNewValue(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Update Env Page")

	rec := patchUpdateEnvironment(t, r, token, pageID, updateStatusPageEnvironmentRequest{Environment: "development"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.Environment != "development" {
		t.Errorf("Environment = %q, want %q", updated.Environment, "development")
	}
}

// TestUpdateEnvironment_InvalidValue_422 asserts SPEB-02's enum
// validation on the update path, same as create.
func TestUpdateEnvironment_InvalidValue_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Bad Update Env Page")

	rec := patchUpdateEnvironment(t, r, token, pageID, updateStatusPageEnvironmentRequest{Environment: "production"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestUpdateEnvironment_NonexistentStatusPageID_404 mirrors AttachDomain's
// not-found convention.
func TestUpdateEnvironment_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchUpdateEnvironment(t, r, token, "00000000-0000-0000-0000-000000000000", updateStatusPageEnvironmentRequest{Environment: "staging"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func postVerifyDomain(t *testing.T, r http.Handler, token, statusPageID string) *httptest.ResponseRecorder {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodPost, "/api/status-pages/"+statusPageID+"/verify-domain", nil)
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

func TestVerifyDomain_NoDomainAttached_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Verify No Domain Page")

	rec := postVerifyDomain(t, r, token, pageID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestVerifyDomain_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postVerifyDomain(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestVerifyDomain_ValidRequest_200ReturnsCheckResult injects a
// fakeDomainVerifier (real DNS/TLS would make this test flaky/slow and
// require a publicly resolvable hostname) and asserts the response
// combines the fake check result with the status page's current DB state.
func TestVerifyDomain_ValidRequest_200ReturnsCheckResult(t *testing.T) {
	matchesTarget := true
	r, pool, admins := newStatusPagesRouter(t, func(h *StatusPagesHandler) {
		h.verifier = &fakeDomainVerifier{result: domainVerificationResult{
			ResolvedIPs:      []string{"203.0.113.10"},
			DNSResolved:      true,
			DNSMatchesTarget: &matchesTarget,
			TLSReachable:     true,
			TLSCertValid:     false,
			TLSError:         strPtr("x509: certificate is valid for other.example.com, not status.empresa.com"),
		}}
	})
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Verify Valid Page")
	attachRec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if attachRec.Code != http.StatusOK {
		t.Fatalf("setup attach status = %d, want %d, body = %s", attachRec.Code, http.StatusOK, attachRec.Body.String())
	}

	rec := postVerifyDomain(t, r, token, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got verifyDomainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(got.ResolvedIPs) != 1 || got.ResolvedIPs[0] != "203.0.113.10" {
		t.Errorf("ResolvedIPs = %v, want [203.0.113.10]", got.ResolvedIPs)
	}
	if !got.DNSResolved {
		t.Error("DNSResolved = false, want true")
	}
	if got.DNSMatchesTarget == nil || !*got.DNSMatchesTarget {
		t.Errorf("DNSMatchesTarget = %v, want true", got.DNSMatchesTarget)
	}
	if !got.TLSReachable {
		t.Error("TLSReachable = false, want true")
	}
	if got.TLSCertValid {
		t.Error("TLSCertValid = true, want false")
	}
	if got.TLSError == nil {
		t.Error("TLSError = nil, want a certificate mismatch message")
	}
	if got.State != "pending_tls" {
		t.Errorf("State = %q, want %q", got.State, "pending_tls")
	}
}

// TestVerifyDomain_ValidRequest_RecordsStatusPageDomainVerifiedAuditLabel
// covers ACTIVITY-03: the status_page_domain_verified audit entry's
// target_label must carry the status page's own name, not a bare
// target_id.
func TestVerifyDomain_ValidRequest_RecordsStatusPageDomainVerifiedAuditLabel(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t, func(h *StatusPagesHandler) {
		h.verifier = &fakeDomainVerifier{result: domainVerificationResult{DNSResolved: true, TLSReachable: true, TLSCertValid: true}}
	})
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Verify Label Page")
	attachRec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if attachRec.Code != http.StatusOK {
		t.Fatalf("setup attach status = %d, want %d, body = %s", attachRec.Code, http.StatusOK, attachRec.Body.String())
	}

	rec := postVerifyDomain(t, r, token, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel *string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_domain_verified'", pageID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel == nil || *gotTargetLabel != "Verify Label Page" {
		t.Errorf("admin_audit_log target_label = %v, want %q", gotTargetLabel, "Verify Label Page")
	}
}

// TestVerifyDomain_SecondCallWithinCooldown_429 asserts the per-hostname
// cooldown (verifyDomainCooldown) rejects a second attempt made
// immediately after the first, protecting against a client hammering the
// button and tripping Let's Encrypt's failed-validation rate limit.
func TestVerifyDomain_SecondCallWithinCooldown_429(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t, func(h *StatusPagesHandler) {
		h.verifier = &fakeDomainVerifier{}
	})
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Verify Cooldown Page")
	attachRec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "status"})
	if attachRec.Code != http.StatusOK {
		t.Fatalf("setup attach status = %d, want %d, body = %s", attachRec.Code, http.StatusOK, attachRec.Body.String())
	}

	first := postVerifyDomain(t, r, token, pageID)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body = %s", first.Code, http.StatusOK, first.Body.String())
	}

	second := postVerifyDomain(t, r, token, pageID)
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second status = %d, want %d, body = %s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
}

// TestCheckVerifyCooldown_AfterWindowElapsed_Allows asserts the cooldown
// is a sliding window, not a one-shot lockout: a page verified once must
// become verifiable again after verifyDomainCooldown passes. Manipulates
// lastVerifyAt directly instead of a real 15s sleep (or the HTTP layer)
// so this test stays fast and exercises checkVerifyCooldown in isolation.
func TestCheckVerifyCooldown_AfterWindowElapsed_Allows(t *testing.T) {
	h := &StatusPagesHandler{lastVerifyAt: make(map[string]time.Time)}
	h.lastVerifyAt["page-1"] = time.Now().Add(-verifyDomainCooldown - time.Second)

	if !h.checkVerifyCooldown("page-1") {
		t.Error("checkVerifyCooldown() = false after the cooldown window elapsed, want true")
	}
}

func strPtr(s string) *string { return &s }

// placementsOf builds a setServicesRequest.Services slice for the given
// serviceIDs with no group/parent (0-based position, in order) - a
// convenience for tests that only care about which services are linked,
// not their group/parent placement.
func placementsOf(serviceIDs ...string) []servicePlacementRequest {
	placements := make([]servicePlacementRequest, len(serviceIDs))
	for i, id := range serviceIDs {
		placements[i] = servicePlacementRequest{ServiceID: id, Position: i}
	}
	return placements
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

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: placementsOf(serviceA)})
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

	rec = patchSetServices(t, r, token, pageID, setServicesRequest{Services: placementsOf(serviceB)})
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

	if rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: placementsOf(serviceA)}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{}})
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

// TestSetServices_MissingServices_422 asserts a request body with no
// services field at all is rejected, distinguishing it from the valid
// "empty array" unlink-everything case above.
func TestSetServices_MissingServices_422(t *testing.T) {
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

// TestSetServices_GroupAndParentPlacement_200PersistsBoth asserts T6's new
// request shape wires group_id and parent_service_id through to the
// repository together on a single placement (SGD-02, SGD-03, SGD-06,
// SGD-07): a child service can belong to a group AND have a parent in the
// very same PATCH.
func TestSetServices_GroupAndParentPlacement_200PersistsBoth(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	parent := createTestService(t, pool)
	child := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Set Services Group Parent Page")
	cleanupStatusPage(t, pool, pageID)
	groupID := createTestGroup(t, r, token, pageID, "Core")

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: parent, Position: 0},
		{ServiceID: child, GroupID: &groupID, ParentServiceID: &parent, Position: 1},
	}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotGroupID, gotParentID *string
	var gotPosition int
	row := pool.QueryRow(context.Background(),
		"SELECT group_id, parent_service_id, position FROM status_page_services WHERE status_page_id = $1 AND service_id = $2",
		pageID, child)
	if err := row.Scan(&gotGroupID, &gotParentID, &gotPosition); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotGroupID == nil || *gotGroupID != groupID {
		t.Errorf("group_id = %v, want %q", gotGroupID, groupID)
	}
	if gotParentID == nil || *gotParentID != parent {
		t.Errorf("parent_service_id = %v, want %q", gotParentID, parent)
	}
	if gotPosition != 1 {
		t.Errorf("position = %d, want 1", gotPosition)
	}
}

// TestSetServices_GroupCrossPage_422 asserts a group_id that doesn't belong
// to this status page surfaces as its own distinct 422 message (SGD-11),
// not a generic error.
func TestSetServices_GroupCrossPage_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Group Cross Page A")
	cleanupStatusPage(t, pool, pageID)
	otherPageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Group Cross Page B")
	cleanupStatusPage(t, pool, otherPageID)
	foreignGroupID := createTestGroup(t, r, token, otherPageID, "Foreign Group")

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: serviceID, GroupID: &foreignGroupID, Position: 0},
	}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != groupCrossPageRequestBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), groupCrossPageRequestBody)
	}
}

// TestSetServices_SelfReference_422 asserts a placement whose
// parent_service_id equals its own service_id surfaces as its own distinct
// 422 message (SGD-12), not the generic "parent not in set" one.
func TestSetServices_SelfReference_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Self Reference Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: serviceID, ParentServiceID: &serviceID, Position: 0},
	}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != selfReferenceRequestBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), selfReferenceRequestBody)
	}
}

// TestSetServices_ParentNotInSet_422 asserts a parent_service_id that
// doesn't match any service_id in the same request surfaces as its own
// distinct 422 message.
func TestSetServices_ParentNotInSet_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)
	missingParent := "00000000-0000-0000-0000-000000000000"
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Parent Not In Set Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: serviceID, ParentServiceID: &missingParent, Position: 0},
	}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != parentNotInSetRequestBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), parentNotInSetRequestBody)
	}
}

// TestSetServices_MaxDepthExceeded_422 asserts a placement that is itself
// used as another placement's parent but also carries its own
// parent_service_id (a 3-level chain) surfaces as its own distinct 422
// message - nesting is capped at depth 2.
func TestSetServices_MaxDepthExceeded_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	grandparent := createTestService(t, pool)
	parent := createTestService(t, pool)
	child := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Max Depth Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: grandparent, Position: 0},
		{ServiceID: parent, ParentServiceID: &grandparent, Position: 1},
		{ServiceID: child, ParentServiceID: &parent, Position: 2},
	}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != maxDepthExceededRequestBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), maxDepthExceededRequestBody)
	}
}

// TestSetServices_NonexistentStatusPageID_404 mirrors
// TestAttachDomain_NonexistentStatusPageID_404 for the services endpoint.
func TestSetServices_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchSetServices(t, r, token, "00000000-0000-0000-0000-000000000000", setServicesRequest{Services: []servicePlacementRequest{}})
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

// TestDeleteStatusPage_Existing_RecordsStatusPageDeletedAuditLabelSurvivingDelete
// covers ACTIVITY-03/ACTIVITY-04: the status_page_deleted audit entry's
// target_label must be the page's name captured before Delete runs - by
// the time this assertion runs, the status_pages row is already gone, so
// a label fetched after the delete would always be empty.
func TestDeleteStatusPage_Existing_RecordsStatusPageDeletedAuditLabelSurvivingDelete(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	createRec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "To Be Deleted With Label"})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	id := decodeStatusPageID(t, createRec)

	rec := deleteStatusPage(t, r, token, id)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var gotTargetLabel *string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_deleted'", id)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel == nil || *gotTargetLabel != "To Be Deleted With Label" {
		t.Errorf("admin_audit_log target_label = %v, want %q (must survive the delete above)", gotTargetLabel, "To Be Deleted With Label")
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

// TestCreateStatusPage_ValidRequest_RecordsStatusPageCreatedAudit covers
// AUDITEXP-04.
func TestCreateStatusPage_ValidRequest_RecordsStatusPageCreatedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateStatusPage(t, r, token, createStatusPageRequest{Name: "Audit Create Page"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created statusPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	cleanupStatusPage(t, pool, created.ID)

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_created'", created.ID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audit Create Page" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audit Create Page")
	}
}

// TestAttachDomain_ValidRequest_RecordsStatusPageDomainAttachedAudit covers
// AUDITEXP-05.
func TestAttachDomain_ValidRequest_RecordsStatusPageDomainAttachedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	domainID := createTestDomain(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Audit Attach Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchAttachDomain(t, r, token, pageID, attachDomainRequest{DomainID: domainID, Subdomain: "audit-status"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_domain_attached'", pageID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audit Attach Page" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audit Attach Page")
	}
}

// TestSetServices_ReplacesLinkedSet_RecordsStatusPageServicesUpdatedAudit
// covers AUDITEXP-06.
func TestSetServices_ReplacesLinkedSet_RecordsStatusPageServicesUpdatedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceA := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Audit SetServices Page")
	cleanupStatusPage(t, pool, pageID)

	rec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: placementsOf(serviceA)})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_services_updated'", pageID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audit SetServices Page" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audit SetServices Page")
	}
}

// --- T5: service group endpoints ---

func postCreateGroup(t *testing.T, r http.Handler, token, statusPageID string, req createGroupRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/status-pages/"+statusPageID+"/groups", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

func patchUpdateGroup(t *testing.T, r http.Handler, token, statusPageID, groupID string, req updateGroupRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPatch, "/api/status-pages/"+statusPageID+"/groups/"+groupID, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

func deleteGroup(t *testing.T, r http.Handler, token, statusPageID, groupID string) *httptest.ResponseRecorder {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodDelete, "/api/status-pages/"+statusPageID+"/groups/"+groupID, nil)
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

func getGroups(t *testing.T, r http.Handler, token, statusPageID string) *httptest.ResponseRecorder {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodGet, "/api/status-pages/"+statusPageID+"/groups", nil)
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

// createTestGroup creates a group via the API and returns its id.
func createTestGroup(t *testing.T, r http.Handler, token, statusPageID, name string) string {
	t.Helper()
	rec := postCreateGroup(t, r, token, statusPageID, createGroupRequest{Name: name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create group status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	return created.ID
}

func TestCreateGroup_ValidRequest_201CreatedAtEndPosition(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Create Group Page")

	first := createTestGroup(t, r, token, pageID, "First Group")

	rec := postCreateGroup(t, r, token, pageID, createGroupRequest{Name: "Second Group"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.StatusPageID != pageID {
		t.Errorf("StatusPageID = %q, want %q", created.StatusPageID, pageID)
	}
	if created.Name != "Second Group" {
		t.Errorf("Name = %q, want %q", created.Name, "Second Group")
	}
	if created.Position != 1 {
		t.Errorf("Position = %d, want 1 (should append after %q)", created.Position, first)
	}
}

func TestCreateGroup_EmptyName_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Create Group Empty Name Page")

	rec := postCreateGroup(t, r, token, pageID, createGroupRequest{Name: ""})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCreateGroup_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateGroup(t, r, token, "00000000-0000-0000-0000-000000000000", createGroupRequest{Name: "Orphan Group"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCreateGroup_ViewerRole_403(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	ownerToken := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, ownerToken, "Create Group Viewer Page")
	viewerToken := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := postCreateGroup(t, r, viewerToken, pageID, createGroupRequest{Name: "Viewer Group"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCreateGroup_ValidRequest_RecordsStatusPageGroupCreatedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Audit Create Group Page")

	rec := postCreateGroup(t, r, token, pageID, createGroupRequest{Name: "Audited Group"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_group_created'", created.ID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audited Group" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audited Group")
	}
}

func TestUpdateGroup_ValidRequest_200RenamesAndRepositions(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Update Group Page")
	groupID := createTestGroup(t, r, token, pageID, "Original Name")

	rec := patchUpdateGroup(t, r, token, pageID, groupID, updateGroupRequest{Name: "Renamed", Position: 3})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var updated statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("Name = %q, want %q", updated.Name, "Renamed")
	}
	if updated.Position != 3 {
		t.Errorf("Position = %d, want 3", updated.Position)
	}
}

func TestUpdateGroup_EmptyName_422(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Update Group Empty Name Page")
	groupID := createTestGroup(t, r, token, pageID, "Has A Name")

	rec := patchUpdateGroup(t, r, token, pageID, groupID, updateGroupRequest{Name: "", Position: 0})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestUpdateGroup_NonexistentGroupID_404(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Update Group Missing Page")

	rec := patchUpdateGroup(t, r, token, pageID, "00000000-0000-0000-0000-000000000000", updateGroupRequest{Name: "Ghost", Position: 0})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestUpdateGroup_ViewerRole_403(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	ownerToken := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, ownerToken, "Update Group Viewer Page")
	groupID := createTestGroup(t, r, ownerToken, pageID, "Pre-existing Group")
	viewerToken := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := patchUpdateGroup(t, r, viewerToken, pageID, groupID, updateGroupRequest{Name: "Should Not Apply", Position: 0})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestUpdateGroup_ValidRequest_RecordsStatusPageGroupUpdatedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Audit Update Group Page")
	groupID := createTestGroup(t, r, token, pageID, "Pre-update Name")

	rec := patchUpdateGroup(t, r, token, pageID, groupID, updateGroupRequest{Name: "Audited Update", Position: 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_group_updated'", groupID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audited Update" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audited Update")
	}
}

func TestDeleteGroup_Existing_204UngroupsMembersWithoutDeletingThem(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Delete Group Page")
	cleanupStatusPage(t, pool, pageID)
	groupID := createTestGroup(t, r, token, pageID, "To Be Deleted")

	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_page_services (status_page_id, service_id, group_id, position) VALUES ($1, $2, $3, 0)",
		pageID, serviceID, groupID); err != nil {
		t.Fatalf("setup insert placement returned unexpected error: %v", err)
	}

	rec := deleteGroup(t, r, token, pageID, groupID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var stillExists bool
	row := pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM status_page_services WHERE status_page_id = $1 AND service_id = $2)", pageID, serviceID)
	if err := row.Scan(&stillExists); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if !stillExists {
		t.Fatal("status_page_services row was deleted along with its group, want it left in place")
	}

	var groupIDAfter *string
	row = pool.QueryRow(context.Background(),
		"SELECT group_id FROM status_page_services WHERE status_page_id = $1 AND service_id = $2", pageID, serviceID)
	if err := row.Scan(&groupIDAfter); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if groupIDAfter != nil {
		t.Errorf("group_id = %v, want nil (ON DELETE SET NULL)", *groupIDAfter)
	}
}

func TestDeleteGroup_NotFound_404(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Delete Group Missing Page")

	rec := deleteGroup(t, r, token, pageID, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteGroup_ViewerRole_403(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	ownerToken := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, ownerToken, "Delete Group Viewer Page")
	groupID := createTestGroup(t, r, ownerToken, pageID, "Protected Group")
	viewerToken := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := deleteGroup(t, r, viewerToken, pageID, groupID)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestDeleteGroup_Existing_RecordsStatusPageGroupDeletedAudit(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "Audit Delete Group Page")
	groupID := createTestGroup(t, r, token, pageID, "Audited Delete Group")

	rec := deleteGroup(t, r, token, pageID, groupID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'status_page_group_deleted'", groupID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Audited Delete Group" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Audited Delete Group")
	}
}

func TestListGroups_AnyRole_200ReturnsPositionOrdered(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	ownerToken := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, ownerToken, "List Groups Page")
	first := createTestGroup(t, r, ownerToken, pageID, "First")
	second := createTestGroup(t, r, ownerToken, pageID, "Second")

	viewerToken := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)
	rec := getGroups(t, r, viewerToken, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var groups []statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(groups) != 2 || groups[0].ID != first || groups[1].ID != second {
		t.Fatalf("groups = %+v, want [%q, %q] in that order", groups, first, second)
	}
}

func TestListGroups_NoGroups_200EmptyArrayNotNull(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "List Groups Empty Page")

	rec := getGroups(t, r, token, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() == "null\n" || rec.Body.String() == "null" {
		t.Fatalf("body = %q, want an empty JSON array, not null", rec.Body.String())
	}
	var groups []statusPageServiceGroupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("groups = %+v, want empty", groups)
	}
}

func getServicePlacements(t *testing.T, r http.Handler, token, statusPageID string) *httptest.ResponseRecorder {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodGet, "/api/status-pages/"+statusPageID+"/services", nil)
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httpReq)
	return rec
}

// TestListServicePlacements_MixedPlacements_200ReturnsFullDetail asserts
// T7's endpoint returns every placement's group_id/parent_service_id/
// position detail - set up via a real SetServices call (T6) first, then
// read back through the new read endpoint - covering a grouped+parented
// child, an ungrouped parentless service, and a grouped parentless service
// in the same page (SGD-02, SGD-03, SGD-06, SGD-07).
func TestListServicePlacements_MixedPlacements_200ReturnsFullDetail(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	parent := createTestService(t, pool)
	child := createTestService(t, pool)
	ungrouped := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "List Service Placements Page")
	groupID := createTestGroup(t, r, token, pageID, "Core")

	setRec := patchSetServices(t, r, token, pageID, setServicesRequest{Services: []servicePlacementRequest{
		{ServiceID: parent, GroupID: &groupID, Position: 0},
		{ServiceID: child, GroupID: &groupID, ParentServiceID: &parent, Position: 1},
		{ServiceID: ungrouped, Position: 2},
	}})
	if setRec.Code != http.StatusOK {
		t.Fatalf("setup SetServices status = %d, want %d, body = %s", setRec.Code, http.StatusOK, setRec.Body.String())
	}

	rec := getServicePlacements(t, r, token, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var placements []servicePlacementResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &placements); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(placements) != 3 {
		t.Fatalf("placements = %+v, want 3 entries", placements)
	}

	byID := make(map[string]servicePlacementResponse, len(placements))
	for _, p := range placements {
		byID[p.ServiceID] = p
	}

	gotParent, ok := byID[parent]
	if !ok {
		t.Fatalf("placements missing parent service %q: %+v", parent, placements)
	}
	if gotParent.GroupID == nil || *gotParent.GroupID != groupID {
		t.Errorf("parent.GroupID = %v, want %q", gotParent.GroupID, groupID)
	}
	if gotParent.ParentServiceID != nil {
		t.Errorf("parent.ParentServiceID = %v, want nil", gotParent.ParentServiceID)
	}
	if gotParent.Position != 0 {
		t.Errorf("parent.Position = %d, want 0", gotParent.Position)
	}

	gotChild, ok := byID[child]
	if !ok {
		t.Fatalf("placements missing child service %q: %+v", child, placements)
	}
	if gotChild.GroupID == nil || *gotChild.GroupID != groupID {
		t.Errorf("child.GroupID = %v, want %q", gotChild.GroupID, groupID)
	}
	if gotChild.ParentServiceID == nil || *gotChild.ParentServiceID != parent {
		t.Errorf("child.ParentServiceID = %v, want %q", gotChild.ParentServiceID, parent)
	}
	if gotChild.Position != 1 {
		t.Errorf("child.Position = %d, want 1", gotChild.Position)
	}

	gotUngrouped, ok := byID[ungrouped]
	if !ok {
		t.Fatalf("placements missing ungrouped service %q: %+v", ungrouped, placements)
	}
	if gotUngrouped.GroupID != nil {
		t.Errorf("ungrouped.GroupID = %v, want nil", gotUngrouped.GroupID)
	}
	if gotUngrouped.ParentServiceID != nil {
		t.Errorf("ungrouped.ParentServiceID = %v, want nil", gotUngrouped.ParentServiceID)
	}
	if gotUngrouped.Position != 2 {
		t.Errorf("ungrouped.Position = %d, want 2", gotUngrouped.Position)
	}
}

// TestListServicePlacements_NoServices_200EmptyArrayNotNull mirrors
// TestListGroups_NoGroups_200EmptyArrayNotNull: a status page with no
// linked services returns an empty JSON array, never null.
func TestListServicePlacements_NoServices_200EmptyArrayNotNull(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, token, "List Service Placements Empty Page")

	rec := getServicePlacements(t, r, token, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() == "null\n" || rec.Body.String() == "null" {
		t.Fatalf("body = %q, want an empty JSON array, not null", rec.Body.String())
	}
	var placements []servicePlacementResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &placements); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(placements) != 0 {
		t.Errorf("placements = %+v, want empty", placements)
	}
}

// TestListServicePlacements_NonexistentStatusPageID_404 mirrors
// TestSetServices_NonexistentStatusPageID_404 for the new read endpoint.
func TestListServicePlacements_NonexistentStatusPageID_404(t *testing.T) {
	r, _, admins := newStatusPagesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getServicePlacements(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestListServicePlacements_ViewerRole_200 asserts this is a read endpoint
// gated anyRole (unlike SetServices/CreateGroup's writeRoles) - a viewer can
// read placement detail, mirroring TestListGroups_AnyRole_200ReturnsPositionOrdered's
// use of a viewer token.
func TestListServicePlacements_ViewerRole_200(t *testing.T) {
	r, pool, admins := newStatusPagesRouter(t)
	ownerToken := issueTestSessionToken(t, admins)
	serviceID := createTestService(t, pool)
	pageID := createDomainlessStatusPageViaAPI(t, r, pool, ownerToken, "List Service Placements Viewer Page")

	setRec := patchSetServices(t, r, ownerToken, pageID, setServicesRequest{Services: placementsOf(serviceID)})
	if setRec.Code != http.StatusOK {
		t.Fatalf("setup SetServices status = %d, want %d, body = %s", setRec.Code, http.StatusOK, setRec.Body.String())
	}

	viewerToken := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)
	rec := getServicePlacements(t, r, viewerToken, pageID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var placements []servicePlacementResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &placements); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(placements) != 1 || placements[0].ServiceID != serviceID {
		t.Fatalf("placements = %+v, want [%q]", placements, serviceID)
	}
}
