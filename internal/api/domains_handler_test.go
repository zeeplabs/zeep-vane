//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	statusPages := db.NewStatusPageRepository(pool)
	admins := db.NewUserRepository(pool)
	handler := NewDomainsHandler(repo, statusPages, audit.NewLog(pool), "", zap.NewNop())
	for _, opt := range opts {
		opt(handler)
	}

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
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

// TestListDomains_ResponseHasVerificationTxtValueNoSSLStatus covers DATV-02/
// DATV-08: each listed domain carries the TXT value the operator must publish
// at their DNS provider, and the response never contains an ssl_status key.
func TestListDomains_ResponseHasVerificationTxtValueNoSSLStatus(t *testing.T) {
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
	if strings.Contains(rec.Body.String(), "ssl_status") {
		t.Errorf("List response contains an ssl_status key, want none")
	}

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.VerificationTxtValue == "" {
		t.Error("listed domain VerificationTxtValue = \"\", want a non-empty verification token")
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

// TestDeleteDomain_Existing_RecordsDomainDeletedAuditLabelSurvivingDelete
// covers ACTIVITY-03/ACTIVITY-04: the domain_deleted audit entry's
// target_label must be the hostname captured before Delete runs - by the
// time this assertion runs, the domains row is already gone, so a label
// fetched after the delete would always be empty.
func TestDeleteDomain_Existing_RecordsDomainDeletedAuditLabelSurvivingDelete(t *testing.T) {
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

	var gotTargetLabel *string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'domain_deleted'", created.ID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel == nil || *gotTargetLabel != hostname {
		t.Errorf("admin_audit_log target_label = %v, want %q (must survive the delete above)", gotTargetLabel, hostname)
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

// TestCreateDomain_DefaultsToPendingCustom covers DOMVER-02 and DATV-01: a
// newly created domain's response shows domain_type=custom, status=pending,
// verified_at=null, a non-empty verification_txt_value, and no ssl_status key.
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
	if created.VerifiedAt != nil {
		t.Errorf("VerifiedAt = %v, want nil", created.VerifiedAt)
	}
	if created.VerificationTxtValue == "" {
		t.Error("VerificationTxtValue = \"\", want a non-empty verification token")
	}
	if strings.Contains(rec.Body.String(), "ssl_status") {
		t.Errorf("Create response contains an ssl_status key, want none: %s", rec.Body.String())
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

// insertStatusPageAttachedTo inserts a raw status_pages row attached to
// domainID (bypassing StatusPageRepository.AttachDomain's "exactly once"
// constraint, same fixture technique as TestDeleteDomain_InUseByStatusPage_409),
// registers its cleanup, and returns the generated id/name.
func insertStatusPageAttachedTo(t *testing.T, pool *db.Pool, domainID, name, subdomain string) string {
	t.Helper()
	var id string
	row := pool.QueryRow(context.Background(),
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3) RETURNING id",
		name, subdomain, domainID,
	)
	if err := row.Scan(&id); err != nil {
		t.Fatalf("failed to insert status page fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", id) })
	return id
}

// findDomainResponseAcrossPages is findDomainAcrossPages's sibling,
// returning the full domainResponse instead of a bool, so tests can assert
// on attached_page_name/attached_page_count.
func findDomainResponseAcrossPages(t *testing.T, r http.Handler, token, hostname string) (domainResponse, bool) {
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
				return d, true
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return domainResponse{}, false
		}
	}
}

// TestListDomains_ZeroAttachedPages_NilNameZeroCount covers DSP-02: a
// domain with no status page attached shows attached_page_name=nil,
// attached_page_count=0.
func TestListDomains_ZeroAttachedPages_NilNameZeroCount(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.AttachedPageName != nil {
		t.Errorf("AttachedPageName = %v, want nil", *got.AttachedPageName)
	}
	if got.AttachedPageCount != 0 {
		t.Errorf("AttachedPageCount = %d, want 0", got.AttachedPageCount)
	}
}

// TestListDomains_OneAttachedPage_NameAndCountOne covers DSP-03: a domain
// with exactly one attached status page shows that page's name and count 1.
func TestListDomains_OneAttachedPage_NameAndCountOne(t *testing.T) {
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
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE domain_id = $1", created.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", created.ID)
	})

	insertStatusPageAttachedTo(t, pool, created.ID, "One Page Status", "one")

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.AttachedPageName == nil || *got.AttachedPageName != "One Page Status" {
		t.Errorf("AttachedPageName = %v, want %q", got.AttachedPageName, "One Page Status")
	}
	if got.AttachedPageCount != 1 {
		t.Errorf("AttachedPageCount = %d, want 1", got.AttachedPageCount)
	}
}

// TestListDomains_TwoOrMoreAttachedPages_FirstNameAndFullCount covers
// DSP-04: a domain with 2+ attached status pages shows the earliest
// created page's name and the full count.
func TestListDomains_TwoOrMoreAttachedPages_FirstNameAndFullCount(t *testing.T) {
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
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE domain_id = $1", created.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", created.ID)
	})

	firstID := insertStatusPageAttachedTo(t, pool, created.ID, "First Attached Page", "first")
	insertStatusPageAttachedTo(t, pool, created.ID, "Second Attached Page", "second")
	// Force a deterministic created_at ordering so "first" is unambiguous.
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET created_at = $1 WHERE id = $2",
		time.Now().Add(-1*time.Hour), firstID); err != nil {
		t.Fatalf("failed to backdate first page created_at: %v", err)
	}

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.AttachedPageName == nil || *got.AttachedPageName != "First Attached Page" {
		t.Errorf("AttachedPageName = %v, want %q (earliest created)", got.AttachedPageName, "First Attached Page")
	}
	if got.AttachedPageCount != 2 {
		t.Errorf("AttachedPageCount = %d, want 2", got.AttachedPageCount)
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

// TestVerifyDomain_Success_Verified covers DATV-03: a successful TXT check
// (record found, value matches the token) persists status=verified, stamps
// verified_at, clears last_error, and returns no ssl_status key.
func TestVerifyDomain_Success_Verified(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeApexTXTVerifier{result: apexTXTResult{TXTFound: true, TXTMatches: true}}
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
	if updated.VerifiedAt == nil {
		t.Error("VerifiedAt = nil, want a timestamp")
	}
	if updated.LastError != nil {
		t.Errorf("LastError = %v, want nil", updated.LastError)
	}
	if updated.VerificationTxtValue == "" {
		t.Error("VerificationTxtValue = \"\", want a non-empty verification token")
	}
	if strings.Contains(rec.Body.String(), "ssl_status") {
		t.Errorf("Verify response contains an ssl_status key, want none: %s", rec.Body.String())
	}
}

// TestVerifyDomain_Success_RecordsDomainVerifiedAuditLabel covers
// ACTIVITY-03: the domain_verified audit entry's target_label must carry
// the domain's own hostname, not a bare target_id.
func TestVerifyDomain_Success_RecordsDomainVerifiedAuditLabel(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeApexTXTVerifier{result: apexTXTResult{TXTFound: true, TXTMatches: true}}
	})
	token := issueTestSessionToken(t, admins)
	domain := createVerifiableTestDomain(t, r, pool, token)

	rec := postDomainVerify(t, r, token, domain.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel *string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'domain_verified'", domain.ID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel == nil || *gotTargetLabel != domain.Hostname {
		t.Errorf("admin_audit_log target_label = %v, want %q", gotTargetLabel, domain.Hostname)
	}
}

// TestVerifyDomain_TXTNotFound_ErrorWithTXTMessage covers DATV-04: an absent
// TXT record persists status=error with a TXT-specific message, never one
// suggesting a CNAME or TLS action.
func TestVerifyDomain_TXTNotFound_ErrorWithTXTMessage(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeApexTXTVerifier{result: apexTXTResult{TXTFound: false}}
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
		t.Fatal("LastError is nil/empty, want a populated description")
	}
	if !strings.Contains(*updated.LastError, "TXT") {
		t.Errorf("LastError = %q, want TXT-specific wording", *updated.LastError)
	}
	if strings.Contains(*updated.LastError, "CNAME") || strings.Contains(*updated.LastError, "DNS not resolved") {
		t.Errorf("LastError = %q, must not mention a CNAME or DNS resolution", *updated.LastError)
	}
}

// TestVerifyDomain_TXTMismatch_ErrorWithTXTMessage covers DATV-04's other
// failure branch: a TXT record exists but none of its values equals the
// token (TXTFound=true, TXTMatches=false), distinct from "no record at all".
func TestVerifyDomain_TXTMismatch_ErrorWithTXTMessage(t *testing.T) {
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeApexTXTVerifier{result: apexTXTResult{TXTFound: true, TXTMatches: false}}
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
		t.Errorf("Status = %q, want %q (TXT found but value does not match)", updated.Status, "error")
	}
	if updated.LastError == nil || *updated.LastError == "" {
		t.Fatal("LastError is nil/empty, want a populated mismatch description")
	}
	if !strings.Contains(*updated.LastError, "does not match") {
		t.Errorf("LastError = %q, want a value-mismatch description", *updated.LastError)
	}
	if strings.Contains(*updated.LastError, "CNAME") || strings.Contains(*updated.LastError, "not resolved") {
		t.Errorf("LastError = %q, must not mention a CNAME or DNS resolution", *updated.LastError)
	}
}

// fakeApexTXTVerifier lets DomainsHandler Verify tests control the TXT check
// outcome without real DNS. Distinct from fakeDomainVerifier, which serves
// the unrelated StatusPagesHandler.VerifyDomain subdomain flow.
type fakeApexTXTVerifier struct {
	result apexTXTResult
}

func (f *fakeApexTXTVerifier) Verify(ctx context.Context, hostname, expectedToken string) apexTXTResult {
	return f.result
}

// TestVerifyDomain_UnknownDomain_404 covers DOMVER-05.
func TestVerifyDomain_UnknownDomain_404(t *testing.T) {
	r, _, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = &fakeApexTXTVerifier{}
	})
	token := issueTestSessionToken(t, admins)

	rec := postDomainVerify(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// countingApexTXTVerifier wraps fakeApexTXTVerifier to count Verify() calls,
// so the cooldown and re-verify tests can assert on the number of real checks.
type countingApexTXTVerifier struct {
	result apexTXTResult
	calls  int
}

func (f *countingApexTXTVerifier) Verify(ctx context.Context, hostname, expectedToken string) apexTXTResult {
	f.calls++
	return f.result
}

// TestVerifyDomain_WithinCooldown_ReturnsExistingStateNoNewCheck covers
// DOMVER-06: a second verify call within verifyDomainCooldown of the first
// returns the existing persisted state (200) without a new network call,
// unlike StatusPagesHandler.VerifyDomain's 429.
func TestVerifyDomain_WithinCooldown_ReturnsExistingStateNoNewCheck(t *testing.T) {
	counter := &countingApexTXTVerifier{result: apexTXTResult{TXTFound: true, TXTMatches: true}}
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

// TestVerifyDomain_AlreadyVerified_ReVerifiesAndUpdates covers DATV-05: a
// domain already "verified" still triggers a fresh TXT lookup on the next
// verify (not short-circuited by the current state), and the fresh result
// updates status/verified_at. The cooldown is simulated as elapsed by
// backdating the handler's per-domain timestamp.
func TestVerifyDomain_AlreadyVerified_ReVerifiesAndUpdates(t *testing.T) {
	counter := &countingApexTXTVerifier{result: apexTXTResult{TXTFound: true, TXTMatches: true}}
	var handler *DomainsHandler
	r, pool, admins := newDomainsRouter(t, func(h *DomainsHandler) {
		h.verifier = counter
		handler = h
	})
	token := issueTestSessionToken(t, admins)
	domain := createVerifiableTestDomain(t, r, pool, token)

	firstRec := postDomainVerify(t, r, token, domain.ID)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first verify status = %d, want %d, body = %s", firstRec.Code, http.StatusOK, firstRec.Body.String())
	}
	var first domainResponse
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if first.Status != "verified" {
		t.Fatalf("first response Status = %q, want %q", first.Status, "verified")
	}

	// Simulate the cooldown window elapsing so the next call performs a real
	// lookup instead of returning the persisted state.
	handler.lastVerifyMu.Lock()
	handler.lastVerifyAt[domain.ID] = time.Now().Add(-time.Minute)
	handler.lastVerifyMu.Unlock()

	// The record is now gone/mismatched: the fresh check must run and flip
	// the already-verified domain back to error.
	counter.result = apexTXTResult{TXTFound: true, TXTMatches: false}
	secondRec := postDomainVerify(t, r, token, domain.ID)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second verify status = %d, want %d, body = %s", secondRec.Code, http.StatusOK, secondRec.Body.String())
	}

	if counter.calls != 2 {
		t.Errorf("verifier.Verify() calls = %d, want 2 (an already-verified domain must still be re-checked)", counter.calls)
	}
	var second domainResponse
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if second.Status != "error" {
		t.Errorf("second response Status = %q, want %q (fresh TXT mismatch must update the already-verified state)", second.Status, "error")
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

// TestListDomains_HealthDataPopulated_ReturnsAllSevenFields covers
// DHM-04/DHM-08: a domain whose health check has run returns expiration,
// registrar, expected/current NS, drift, last-check timestamp and RDAP error
// on GET /api/domains.
func TestListDomains_HealthDataPopulated_ReturnsAllSevenFields(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}
	var created domainResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	repo := db.NewDomainRepository(pool)
	expires := time.Now().UTC().Add(6 * 24 * time.Hour).Truncate(time.Second)
	registrar := "GoDaddy"
	ns := []string{"ns1.example.net", "ns2.example.net"}
	if err := repo.SetHealthCheckResult(context.Background(), created.ID, &expires, &registrar, ns, false, nil); err != nil {
		t.Fatalf("SetHealthCheckResult() returned unexpected error: %v", err)
	}

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.ExpiresAt == nil || got.ExpiresAt.Unix() != expires.Unix() {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
	if got.Registrar == nil || *got.Registrar != registrar {
		t.Errorf("Registrar = %v, want %q", got.Registrar, registrar)
	}
	if len(got.ExpectedNS) != len(ns) || got.ExpectedNS[0] != ns[0] || got.ExpectedNS[1] != ns[1] {
		t.Errorf("ExpectedNS = %v, want %v", got.ExpectedNS, ns)
	}
	if len(got.CurrentNS) != len(ns) || got.CurrentNS[0] != ns[0] || got.CurrentNS[1] != ns[1] {
		t.Errorf("CurrentNS = %v, want %v", got.CurrentNS, ns)
	}
	if got.NSDriftDetected {
		t.Error("NSDriftDetected = true, want false")
	}
	if got.LastRDAPCheckAt == nil {
		t.Error("LastRDAPCheckAt = nil, want the check's timestamp")
	}
	if got.RDAPLastError != nil {
		t.Errorf("RDAPLastError = %v, want nil", got.RDAPLastError)
	}
}

// TestListDomains_NoHealthData_NullHealthFields covers T4's edge case: a
// domain whose health check has not run yet serializes the new fields as
// null/zero without erroring.
func TestListDomains_NoHealthData_NullHealthFields(t *testing.T) {
	r, pool, admins := newDomainsRouter(t)
	token := issueTestSessionToken(t, admins)
	hostname := uniqueHostname(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE hostname = $1", hostname) })

	createRec := postCreateDomain(t, r, token, hostname)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", createRec.Code, http.StatusCreated, createRec.Body.String())
	}

	got, found := findDomainResponseAcrossPages(t, r, token, hostname)
	if !found {
		t.Fatalf("created hostname %q not found across any page of GET /api/domains", hostname)
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt = %v, want nil", got.ExpiresAt)
	}
	if got.Registrar != nil {
		t.Errorf("Registrar = %v, want nil", got.Registrar)
	}
	if got.ExpectedNS != nil {
		t.Errorf("ExpectedNS = %v, want nil", got.ExpectedNS)
	}
	if got.CurrentNS != nil {
		t.Errorf("CurrentNS = %v, want nil", got.CurrentNS)
	}
	if got.NSDriftDetected {
		t.Error("NSDriftDetected = true, want false")
	}
	if got.LastRDAPCheckAt != nil {
		t.Errorf("LastRDAPCheckAt = %v, want nil", got.LastRDAPCheckAt)
	}
	if got.RDAPLastError != nil {
		t.Errorf("RDAPLastError = %v, want nil", got.RDAPLastError)
	}
}

// TestCreateDomain_NewHostname_RecordsDomainCreatedAudit covers
// AUDITEXP-07.
func TestCreateDomain_NewHostname_RecordsDomainCreatedAudit(t *testing.T) {
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

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'domain_created'", created.ID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != hostname {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, hostname)
	}
}
