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

func newDomainsRouter(t *testing.T, opts ...func(*DomainsHandler)) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()

	pool, _ := newAPITenantScopedPool(t)

	repo := db.NewDomainRepository(pool)
	admins := db.NewUserRepository(pool)
	handler := NewDomainsHandler(repo, audit.NewLog(pool), "", zap.NewNop())
	for _, opt := range opts {
		opt(handler)
	}

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		// Mirrors buildAdminRouter: TenantContext runs right after
		// RequireAuth and is what resolves the caller's role in the active
		// tenant for RequireRole (multi-tenancy-core, AD-022).
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Post("/api/domains", handler.Create)
		protected.Get("/api/domains", handler.List)
		protected.Delete("/api/domains/{id}", handler.Delete)
		protected.Post("/api/domains/{id}/verify", handler.Verify)
	})

	return r, pool, admins
}

func uniqueHostname(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("domains-handler-test-%d.example.com", time.Now().UnixNano())
}

func postCreateDomain(t *testing.T, r http.Handler, token, hostname string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(createDomainRequest{Hostname: hostname})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateDomain_NewHostname_201(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	rec := postCreateDomain(t, r, token, hostname)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.Hostname != hostname {
		t.Errorf("response Hostname = %q, want %q", created.Hostname, hostname)
	}

	var storedHostname string
	row := pool.QueryRow(context.Background(), "SELECT hostname FROM domains WHERE hostname = $1", hostname)
	if err := row.Scan(&storedHostname); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if storedHostname != hostname {
		t.Errorf("stored hostname = %q, want %q", storedHostname, hostname)
	}
}

func TestCreateDomain_DuplicateHostname_409(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	firstRec := postCreateDomain(t, r, token, hostname)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", firstRec.Code, http.StatusCreated)
	}

	rec := postCreateDomain(t, r, token, hostname)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestCreateDomain_NoAuth_401(t *testing.T) {
	r, _, _ := newDomainsRouter(t)

	rec := postCreateDomain(t, r, "", "any-hostname.example.com")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func getListDomains(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getListDomainsPage(t *testing.T, r http.Handler, token string, page int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/domains?page=%d", page), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// findDomainAcrossPages pages through every page of GET /api/domains
// looking for hostname - the shared dev DB this integration suite runs
// against accumulates domains across many tests (PAG-08 exposes this: page
// 1 alone is no longer guaranteed to include a hostname created just now,
// once total exceeds one page).
func findDomainAcrossPages(t *testing.T, r http.Handler, token, hostname string) bool {
	t.Helper()
	for page := 1; ; page++ {
		rec := getListDomainsPage(t, r, token, page)
		if rec.Code != http.StatusOK {
			t.Fatalf("page=%d status = %d, want %d, body = %s", page, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got domainsPageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
		}
		for _, d := range got.Items {
			if d.Hostname == hostname {
				return true
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return false
		}
	}
}

func TestListDomains_AnyRole_200IncludesCreated(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	rec := getListDomains(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page domainsPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (default)", page.Page)
	}
	if page.PageSize != 20 {
		t.Errorf("page.PageSize = %d, want 20", page.PageSize)
	}

	if !findDomainAcrossPages(t, r, token, hostname) {
		t.Errorf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
}

func TestListDomains_InvalidPage_ClampsToPage1(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/domains?page=abc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page domainsPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (clamped from invalid ?page=abc)", page.Page)
	}
}

func TestListDomains_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, _, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)

	req := httptest.NewRequest(http.MethodGet, "/api/domains?page=999999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page domainsPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("len(page.Items) = %d, want 0 for a page far beyond the last", len(page.Items))
	}
}

func TestListDomains_NoAuth_401(t *testing.T) {
	r, _, _ := newDomainsRouter(t)

	rec := getListDomains(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func deleteDomain(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/domains/"+id, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDeleteDomain_Existing_204(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var created domainResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	rec := deleteDomain(t, r, token, created.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var count int
	row := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM domains WHERE id = $1", created.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("domains row still exists after delete")
	}
}

func TestDeleteDomain_NotFound_404(t *testing.T) {
	r, _, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := deleteDomain(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestDeleteDomain_InUseByStatusPage_409 asserts the FK-violation path: a
// domain still attached to a status page (domain_id) can't be deleted out
// from under it (ErrDomainInUse), since that would silently break the
// page's public URL.
func TestDeleteDomain_InUseByStatusPage_409(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create domain status = %d, want %d", createRec.Code, http.StatusCreated)
	}
	var created domainResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE domain_id = $1", created.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", created.ID)
	})

	var statusPageID string
	row := pool.QueryRow(context.Background(),
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3) RETURNING id",
		"In Use Status Page", "inuse", created.ID,
	)
	if err := row.Scan(&statusPageID); err != nil {
		t.Fatalf("failed to insert status page fixture: %v", err)
	}

	rec := deleteDomain(t, r, token, created.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestDeleteDomain_NoAuth_401(t *testing.T) {
	r, _, _ := newDomainsRouter(t)

	rec := deleteDomain(t, r, "", "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestCreateDomain_DefaultsToPendingCustom covers DOMVER-02: a newly
// created domain's response shows domain_type=custom, status=pending,
// ssl_status=pending, verified_at=null.
func TestCreateDomain_DefaultsToPendingCustom(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	rec := postCreateDomain(t, r, token, hostname)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.DomainType != "custom" {
		t.Errorf("DomainType = %q, want %q", created.DomainType, "custom")
	}
	if created.Status != "pending" {
		t.Errorf("Status = %q, want %q", created.Status, "pending")
	}
	if created.SSLStatus != "pending" {
		t.Errorf("SSLStatus = %q, want %q", created.SSLStatus, "pending")
	}
	if created.VerifiedAt != nil {
		t.Errorf("VerifiedAt = %v, want nil", created.VerifiedAt)
	}
}

// TestListDomains_DNSTargetConfigured_IncludedInResponse covers DOMVER-03:
// the response carries the operator's real configured CNAME target.
func TestListDomains_DNSTargetConfigured_IncludedInResponse(t *testing.T) {
	r, _, admins := newDomainsRouter(t, func(h *DomainsHandler) { h.dnsTarget = "lb.example-cluster.internal" })
	token := issueTestSessionToken(t, admins)

	rec := getListDomains(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page domainsPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.DNSTarget == nil || *page.DNSTarget != "lb.example-cluster.internal" {
		t.Errorf("DNSTarget = %v, want %q", page.DNSTarget, "lb.example-cluster.internal")
	}
}

// TestListDomains_DNSTargetUnconfigured_NullInResponse covers the edge
// case: an unset PUBLIC_DNS_TARGET is reported as null, not an empty
// string, so the frontend can show "not configured" explicitly.
func TestListDomains_DNSTargetUnconfigured_NullInResponse(t *testing.T) {
	r, _, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getListDomains(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var page domainsPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.DNSTarget != nil {
		t.Errorf("DNSTarget = %v, want nil", *page.DNSTarget)
	}
}

func postDomainVerify(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/domains/"+id+"/verify", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// createVerifiableTestDomain creates a domain via the handler and returns
// its response, registering cleanup. Distinct from status_pages_handler_test.go's
// own createTestDomain (a raw-SQL fixture with a different signature).
func createVerifiableTestDomain(t *testing.T, r http.Handler, pool *db.Pool, token string) domainResponse {
	t.Helper()
	hostname := uniqueHostname(t)
	rec := postCreateDomain(t, r, token, hostname)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", created.ID) })
	return created
}

// TestVerifyDomain_Success_VerifiedActive covers DOMVER-04/07: a
// successful DNS+TLS check persists status=verified, ssl_status=active.
func TestVerifyDomain_Success_VerifiedActive(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeDomainVerifier{result: domainVerificationResult{
			DNSResolved: true, TLSReachable: true, TLSCertValid: true,
		}}
	})
	token := issueTestSessionToken(t, admins)
	domain := createVerifiableTestDomain(t, r, pool, token)

	rec := postDomainVerify(t, r, token, domain.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.Status != "verified" {
		t.Errorf("Status = %q, want %q", updated.Status, "verified")
	}
	if updated.SSLStatus != "active" {
		t.Errorf("SSLStatus = %q, want %q", updated.SSLStatus, "active")
	}
	if updated.VerifiedAt == nil {
		t.Error("VerifiedAt = nil, want a timestamp")
	}
	if updated.LastError != nil {
		t.Errorf("LastError = %v, want nil", updated.LastError)
	}
}

// TestVerifyDomain_DNSFailure_ErrorWithLastError covers DOMVER-08: a DNS
// resolution failure persists status=error with a populated last_error.
func TestVerifyDomain_DNSFailure_ErrorWithLastError(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeDomainVerifier{result: domainVerificationResult{DNSResolved: false}}
	})
	token := issueTestSessionToken(t, admins)
	domain := createVerifiableTestDomain(t, r, pool, token)

	rec := postDomainVerify(t, r, token, domain.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if updated.Status != "error" {
		t.Errorf("Status = %q, want %q", updated.Status, "error")
	}
	if updated.LastError == nil || *updated.LastError == "" {
		t.Error("LastError is nil/empty, want a populated description")
	}
}

// TestVerifyDomain_UnknownDomain_404 covers DOMVER-05.
func TestVerifyDomain_UnknownDomain_404(t *testing.T) {
	r, _, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeDomainVerifier{}
	})
	token := issueTestSessionToken(t, admins)

	rec := postDomainVerify(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// countingDomainVerifier wraps fakeDomainVerifier to count Verify() calls,
// so the cooldown test can assert no second network call was made.
type countingDomainVerifier struct {
	result domainVerificationResult
	calls  int
}

func (f *countingDomainVerifier) Verify(ctx context.Context, hostname, expectedTarget string) domainVerificationResult {
	f.calls++
	return f.result
}

// TestVerifyDomain_WithinCooldown_ReturnsExistingStateNoNewCheck covers
// DOMVER-06: a second verify call within verifyDomainCooldown of the first
// returns the existing persisted state (200) without a new network call,
// unlike StatusPagesHandler.VerifyDomain's 429.
func TestVerifyDomain_WithinCooldown_ReturnsExistingStateNoNewCheck(t *testing.T) {
	counter := &countingDomainVerifier{result: domainVerificationResult{DNSResolved: true, TLSReachable: true, TLSCertValid: true}}
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) { h.verifier = counter })
	token := issueTestSessionToken(t, admins)
	domain := createVerifiableTestDomain(t, r, pool, token)

	firstRec := postDomainVerify(t, r, token, domain.ID)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first verify status = %d, want %d, body = %s", firstRec.Code, http.StatusOK, firstRec.Body.String())
	}

	secondRec := postDomainVerify(t, r, token, domain.ID)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second verify status = %d, want %d, body = %s", secondRec.Code, http.StatusOK, secondRec.Body.String())
	}

	if counter.calls != 1 {
		t.Errorf("verifier.Verify() calls = %d, want 1 (second call within cooldown must not trigger a fresh network check)", counter.calls)
	}

	var second domainResponse
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if second.Status != "verified" {
		t.Errorf("second response Status = %q, want %q (existing persisted state)", second.Status, "verified")
	}
}

// TestVerifyDomain_NoAuth_401 proves the endpoint requires authentication.
func TestVerifyDomain_NoAuth_401(t *testing.T) {
	r, _, _ := newDomainsRouter(t)

	rec := postDomainVerify(t, r, "", "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
