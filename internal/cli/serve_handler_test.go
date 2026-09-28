//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// newServeAdminHandlerForTest builds the real admin-listener handler through
// serveAdminHandler against a tenant-scoped fixture pool, with the AD-038
// flag set as the caller asks. It reuses the same fixtures and pool helper
// serve_test.go uses, so these tests exercise the exact handler cmd/vane
// serve assigns to its admin HTTP listener.
func newServeAdminHandlerForTest(t *testing.T, tenantDomainsOnAdminListener bool) (http.Handler, *db.Pool) {
	t.Helper()

	pool := newServeTestPool(t)
	cfg := config.Config{
		SessionSecret:                routesTestSessionSecret,
		MasterKey:                    "cli-serve-handler-test-master-key",
		TenantDomainsOnAdminListener: tenantDomainsOnAdminListener,
	}
	pollerManager := NewPollerManager(context.Background(), pool, cfg, zap.NewNop(), testDatabaseURL(t))

	return serveAdminHandler(pool, cfg, zap.NewNop(), pollerManager), pool
}

// TestServeAdminHandler_FlagDisabled_TenantHostReachesAdminRouterOnly proves
// TDS-03's load-bearing claim: with the flag off (the default), the admin
// listener never routes by Host. A request carrying a published status
// page's hostname is answered by the admin router exactly as any other
// hostname - there is no HostRouter wrap in front of it - so
// /api/public-status 404s as JSON instead of resolving to the tenant's
// public status page (which would be a 200 with a services list). A
// backward-wired flag would fail this test. The proof is behavioral rather
// than a call-counting fake lookup: the disabled path constructs no
// status-page repository at all, so there is nothing to count - the
// observable difference (admin 404 vs public 200) is what pins the wiring.
func TestServeAdminHandler_FlagDisabled_TenantHostReachesAdminRouterOnly(t *testing.T) {
	handler, pool := newServeAdminHandlerForTest(t, false)
	serviceID := createServeTestService(t, pool, "svc-flag-off")
	hostname := createServePublishedStatusPageFixture(t, pool, serviceID)

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/public-status", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() returned unexpected error: %v", err)
	}
	req.Host = hostname

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do() returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d - a tenant hostname must not be routed by Host when the flag is off; body = %s", resp.StatusCode, http.StatusNotFound, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q (the admin router's own 404, not the public status response)", ct, "application/json")
	}
}

// TestServeAdminHandler_FlagEnabled_TenantHostServesPublicStatus proves
// TDS-01: with the flag on, a request whose Host resolves to a published
// status page is served by the same public mux newHTTPSServer uses, scoped
// to that page's own services.
func TestServeAdminHandler_FlagEnabled_TenantHostServesPublicStatus(t *testing.T) {
	handler, pool := newServeAdminHandlerForTest(t, true)
	serviceID := createServeTestService(t, pool, "svc-flag-on")
	hostname := createServePublishedStatusPageFixture(t, pool, serviceID)

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	var serviceName string
	for _, s := range allServices {
		if s.ID == serviceID {
			serviceName = s.Name
		}
	}
	if serviceName == "" {
		t.Fatal("setup service not found via List()")
	}

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	body := fetchPublicStatus(t, testServer.URL, hostname)
	if !containsServiceName(body.Services, serviceName) {
		t.Errorf("status page response missing its own linked service %q", serviceName)
	}
}

// TestServeAdminHandler_FlagEnabled_AdminHostFallsThroughToAdminRouter proves
// TDS-02: with the flag on, a Host that matches no published status page
// (the admin's own domain) still reaches the admin router unchanged - here
// /healthz answers with the health JSON rather than the public mux's SPA.
func TestServeAdminHandler_FlagEnabled_AdminHostFallsThroughToAdminRouter(t *testing.T) {
	handler, _ := newServeAdminHandlerForTest(t, true)

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() returned unexpected error: %v", err)
	}
	req.Host = "admin.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do() returned unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body = %s", resp.StatusCode, http.StatusOK, body)
	}

	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("json.Decode() returned unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("health status = %q, want %q", health.Status, "ok")
	}
}
