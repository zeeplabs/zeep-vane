//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// testDatabaseURL returns TEST_DATABASE_URL, skipping the test if unset -
// matching the pattern used by every other package's integration tests
// (see internal/db/pool_test.go).
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newServeTestPool(t *testing.T) *db.Pool {
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

// createServeTestService creates a service with an "operational" status,
// registering cleanup.
func createServeTestService(t *testing.T, pool *db.Pool, namePrefix string) string {
	t.Helper()
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixNano()), SLOID: "slo-serve-test"}
	if err := services.Create(ctx, service); err != nil {
		t.Fatalf("setup service Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(ctx, service.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	return service.ID
}

// createServePublishedStatusPageFixture creates a domain and a status page
// published under a fresh hostname, linked only to serviceID, registering
// cleanup for both. It returns the hostname a real visitor would use.
func createServePublishedStatusPageFixture(t *testing.T, pool *db.Pool, serviceID string) string {
	t.Helper()
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootHostname := fmt.Sprintf("serve-test-%s.example.com", suffix)
	subdomain := "status"
	hostname := subdomain + "." + rootHostname

	domains := db.NewDomainRepository(pool)
	domain := &db.Domain{Hostname: rootHostname}
	if err := domains.Create(ctx, domain); err != nil {
		t.Fatalf("setup domain Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "serve-test-page", Subdomain: subdomain, DomainID: domain.ID}
	if err := statusPages.Create(ctx, statusPage, []string{serviceID}); err != nil {
		t.Fatalf("setup status page Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPage.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	if err := statusPages.MarkPublished(ctx, hostname); err != nil {
		t.Fatalf("setup MarkPublished() returned unexpected error: %v", err)
	}

	return hostname
}

// servePublicStatusResponse is a minimal decode shape for the public
// status endpoint's response, just enough to assert which services came
// back.
type servePublicStatusResponse struct {
	Services []struct {
		Name string `json:"name"`
	} `json:"services"`
}

func containsServiceName(services []struct {
	Name string `json:"name"`
}, name string) bool {
	for _, s := range services {
		if s.Name == name {
			return true
		}
	}
	return false
}

// fetchPublicStatus issues a GET against baseURL with its Host header set
// to hostname - exactly how CertMagic's on-demand TLS listener dispatches
// a real visitor's request by SNI/Host in production - and decodes the
// public status response.
func fetchPublicStatus(t *testing.T, baseURL, hostname string) servePublicStatusResponse {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() returned unexpected error: %v", err)
	}
	req.Host = hostname

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do() returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, http.StatusOK, body)
	}

	var out servePublicStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("json.Decode() returned unexpected error: %v", err)
	}
	return out
}

// TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointServices proves
// Gap 1 (the public status page handler and its Host-based router were
// dead code, never reached from the binary serve starts) and Gap 2 (SP-15
// scoping) are both fixed together: it drives an actual net/http listener
// wrapping the exact Handler newHTTPSServer builds for cmd/vane serve's
// HTTPS listener - not a hand-assembled router/handler pair like the
// router and api package's own isolated tests use - and confirms two
// different published status pages, each linked to a disjoint service,
// return different service lists when hit with their own Host header.
func TestNewHTTPSServer_TwoPublishedStatusPages_ReturnDisjointServices(t *testing.T) {
	pool := newServeTestPool(t)

	serviceA := createServeTestService(t, pool, "svc-a")
	serviceB := createServeTestService(t, pool, "svc-b")
	hostnameA := createServePublishedStatusPageFixture(t, pool, serviceA)
	hostnameB := createServePublishedStatusPageFixture(t, pool, serviceB)

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	var nameA, nameB string
	for _, s := range allServices {
		switch s.ID {
		case serviceA:
			nameA = s.Name
		case serviceB:
			nameB = s.Name
		}
	}

	httpsSrv := newHTTPSServer(pool, zap.NewNop())
	testServer := httptest.NewServer(httpsSrv.Handler)
	defer testServer.Close()

	bodyA := fetchPublicStatus(t, testServer.URL, hostnameA)
	if !containsServiceName(bodyA.Services, nameA) {
		t.Errorf("status page A's response missing its own linked service %q", nameA)
	}
	if containsServiceName(bodyA.Services, nameB) {
		t.Errorf("status page A's response contains status page B's service %q, want scoped to its own services only (SP-15)", nameB)
	}

	bodyB := fetchPublicStatus(t, testServer.URL, hostnameB)
	if !containsServiceName(bodyB.Services, nameB) {
		t.Errorf("status page B's response missing its own linked service %q", nameB)
	}
	if containsServiceName(bodyB.Services, nameA) {
		t.Errorf("status page B's response contains status page A's service %q, want scoped to its own services only (SP-15)", nameA)
	}
}

// TestNewHTTPSServer_UnregisteredHost_404 confirms the HTTPS listener's
// real Handler - the same one wired into cmd/vane serve - still 404s an
// unrecognized Host, exactly as router.HostRouter's own isolated tests
// expect, now proven through the actual serve.go wiring.
func TestNewHTTPSServer_UnregisteredHost_404(t *testing.T) {
	pool := newServeTestPool(t)

	httpsSrv := newHTTPSServer(pool, zap.NewNop())
	testServer := httptest.NewServer(httpsSrv.Handler)
	defer testServer.Close()

	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() returned unexpected error: %v", err)
	}
	req.Host = "no-such-status-page.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do() returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
