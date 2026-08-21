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

	var domains []domainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &domains); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	found := false
	for _, d := range domains {
		if d.Hostname == hostname {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() response %+v does not include created hostname %q", domains, hostname)
	}
}

func TestListDomains_NoAuth_401(t *testing.T) {
	r, _, _ := newDomainsRouter(t)

	rec := getListDomains(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
