//go:build integration

package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// createPublishedStatusPageFixture inserts a domain and a status page
// published under a fresh hostname, registering cleanup for both, and
// returns the hostname.
func createPublishedStatusPageFixture(t *testing.T, pool *db.Pool) string {
	t.Helper()
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootHostname := fmt.Sprintf("host-router-test-%s.example.com", suffix)
	subdomain := "status"
	hostname := subdomain + "." + rootHostname

	domains := db.NewDomainRepository(pool)
	domain := &db.Domain{Hostname: rootHostname}
	if err := domains.Create(ctx, domain); err != nil {
		t.Fatalf("failed to create domain fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "host-router-test-page", Subdomain: subdomain, DomainID: domain.ID}
	if err := statusPages.Create(ctx, statusPage, nil); err != nil {
		t.Fatalf("failed to create status page fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	if err := statusPages.MarkPublished(ctx, hostname); err != nil {
		t.Fatalf("failed to publish status page fixture: %v", err)
	}

	return hostname
}

func newHostRouterTestPool(t *testing.T) *db.Pool {
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

	return pool
}

func TestHostRouter_PublishedStatusPageHost_RoutesToPublicHandler(t *testing.T) {
	pool := newHostRouterTestPool(t)
	hostname := createPublishedStatusPageFixture(t, pool)

	statusPages := db.NewStatusPageRepository(pool)
	publicHandlerCalled := false
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHandlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("public-ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = hostname
	rec := httptest.NewRecorder()

	HostRouter(statusPages, publicHandler).ServeHTTP(rec, req)

	if !publicHandlerCalled {
		t.Fatal("publicHandler was not invoked for a published status page host")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "public-ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "public-ok")
	}
}

func TestHostRouter_UnrecognizedHost_404(t *testing.T) {
	pool := newHostRouterTestPool(t)
	statusPages := db.NewStatusPageRepository(pool)

	publicHandlerCalled := false
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "no-such-status-page.example.com"
	rec := httptest.NewRecorder()

	HostRouter(statusPages, publicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if publicHandlerCalled {
		t.Error("publicHandler was invoked for an unrecognized host")
	}
}
