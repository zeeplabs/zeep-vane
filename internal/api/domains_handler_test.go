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

func newDomainsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
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

	repo := db.NewDomainRepository(pool)
	admins := db.NewAdminRepository(pool)
	handler := NewDomainsHandler(repo, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Post("/api/domains", handler.Create)
		protected.Get("/api/domains", handler.List)
		protected.Delete("/api/domains/{id}", handler.Delete)
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
		var got Page[domainResponse]
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

	var page Page[domainResponse]
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

	var page Page[domainResponse]
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

	var page Page[domainResponse]
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
