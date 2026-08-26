//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

const routesTestSessionSecret = "cli-routes-test-session-secret-32b!!"

func newAdminRouterForTest(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
	t.Helper()
	pool := newServeTestPool(t)
	cfg := config.Config{SessionSecret: routesTestSessionSecret, MasterKey: "cli-routes-test-master-key", UploadsDir: t.TempDir()}
	pollerManager := NewPollerManager(context.Background(), pool, cfg, zap.NewNop())
	handler := buildAdminRouter(pool, cfg, zap.NewNop(), pollerManager)

	// The company_settings row is a singleton shared across every test in
	// this package - reset it to a known state before and after each
	// test. That reset races internal/db's and internal/api's own
	// company_settings tests across the separate concurrent processes
	// `go test ./...` runs them as, so take the shared advisory lock for
	// the duration of this test - see LockCompanySettings' doc comment.
	dbtest.LockCompanySettings(t, context.Background(), testDatabaseURL(t))
	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)

	return handler, pool, db.NewAdminRepository(pool)
}

// issueRoutesTestToken inserts a real admin row with role and issues a
// session token for it, so RequireAuth's GetByID lookup and RequireRole's
// role check both see real state.
//
// admins.Create always inserts with the `admins.role` column's database
// default, which is `owner` (see migration 0009) - regardless of the
// `role` requested here, every call transiently creates an owner-role row
// until/unless the UpdateRole call below moves it away. That makes this
// helper the single common point every owner-sensitive test in this
// package goes through, so it takes LockAdminsTable itself rather than
// relying on each call site to remember to. See LockAdminsTable's doc
// comment for why this must be held across concurrently-run packages,
// not just within this one - it is not enough to serialize the calls
// within this test binary since `go test ./...` runs each package as a
// separate concurrent process against the same TEST_DATABASE_URL.
func issueRoutesTestToken(t *testing.T, admins *db.AdminRepository, role string) string {
	t.Helper()
	ctx := context.Background()
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))
	admin := &db.Admin{
		Email:        fmt.Sprintf("cli-routes-test-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	}
	if err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), admin.ID) })

	if role != db.RoleOwner {
		if err := admins.UpdateRole(ctx, admin.ID, role); err != nil {
			t.Fatalf("admins.UpdateRole() returned unexpected error: %v", err)
		}
	}

	token, err := auth.IssueSession(admin.ID, routesTestSessionSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}
	return token
}

// postCreateDomainRoute drives the mvp-core write route this suite uses to
// represent "any mvp-core write route" (design.md's route list).
func postCreateDomainRoute(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"hostname": fmt.Sprintf("cli-routes-test-%d.example.com", time.Now().UnixNano()),
	})
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

// getServicesRoute drives the mvp-core read route this suite uses to
// represent "any mvp-core read route".
func getServicesRoute(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAdminRouter_Owner_WriteRoute_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOwner)

	rec := postCreateDomainRoute(t, r, token)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestAdminRouter_Owner_ReadRoute_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOwner)

	rec := getServicesRoute(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminRouter_Operator_WriteRoute_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOperator)

	rec := postCreateDomainRoute(t, r, token)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestAdminRouter_Operator_ReadRoute_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOperator)

	rec := getServicesRoute(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminRouter_Viewer_WriteRoute_403(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleViewer)

	rec := postCreateDomainRoute(t, r, token)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminRouter_Viewer_ReadRoute_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleViewer)

	rec := getServicesRoute(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// routeCase describes one route this suite drives through the real router
// built by buildAdminRouter, so a wiring regression (e.g. a missing
// RequireRole) is caught regardless of which route it hits.
type routeCase struct {
	name   string
	method string
	path   string
	body   func() []byte
}

const routesTestNonexistentID = "00000000-0000-0000-0000-000000000000"

// writeRouteCases lists every mvp-core write route mounted in routes.go
// (ADM-10, ADM-11). Before this, the suite drove only POST /api/domains as
// a stand-in for "any write route" - removing RequireRole from
// /api/status-pages or /api/incidents broke nothing (validation.md M5, M6).
// Each body func is called fresh per request so the /api/domains case gets
// a unique hostname (its only unique-constrained field) every call.
func writeRouteCases() []routeCase {
	return []routeCase{
		{
			name:   "POST /api/domains",
			method: http.MethodPost,
			path:   "/api/domains",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"hostname": fmt.Sprintf("cli-routes-test-%d.example.com", time.Now().UnixNano()),
				})
				return b
			},
		},
		{
			name:   "POST /api/services",
			method: http.MethodPost,
			path:   "/api/services",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"name":   fmt.Sprintf("cli-routes-test-service-%d", time.Now().UnixNano()),
					"slo_id": fmt.Sprintf("cli-routes-test-slo-%d", time.Now().UnixNano()),
				})
				return b
			},
		},
		{
			name:   "POST /api/integrations/datadog",
			method: http.MethodPost,
			path:   "/api/integrations/datadog",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"api_key": "cli-routes-test-api-key",
					"app_key": "cli-routes-test-app-key",
				})
				return b
			},
		},
		{
			name:   "GET /api/integrations/datadog/slos",
			method: http.MethodGet,
			path:   "/api/integrations/datadog/slos?query=checkout",
			body:   func() []byte { return nil },
		},
		{
			name:   "POST /api/incidents",
			method: http.MethodPost,
			path:   "/api/incidents",
			body: func() []byte {
				b, _ := json.Marshal(map[string]interface{}{
					"title":       "cli-routes-test-incident",
					"service_ids": []string{routesTestNonexistentID},
				})
				return b
			},
		},
		{
			name:   "POST /api/incidents/{id}/updates",
			method: http.MethodPost,
			path:   "/api/incidents/" + routesTestNonexistentID + "/updates",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{"body": "cli-routes-test-update"})
				return b
			},
		},
		{
			name:   "PATCH /api/incidents/{id}",
			method: http.MethodPatch,
			path:   "/api/incidents/" + routesTestNonexistentID,
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{"status": "investigating"})
				return b
			},
		},
		{
			name:   "POST /api/status-pages",
			method: http.MethodPost,
			path:   "/api/status-pages",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"name":      "cli-routes-test-page",
					"subdomain": fmt.Sprintf("cli-routes-test-sub-%d", time.Now().UnixNano()),
					"domain_id": routesTestNonexistentID,
				})
				return b
			},
		},
		{
			name:   "PATCH /api/status-pages/{id}/domain",
			method: http.MethodPatch,
			path:   "/api/status-pages/" + routesTestNonexistentID + "/domain",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"domain_id": routesTestNonexistentID,
					"subdomain": "status",
				})
				return b
			},
		},
		{
			name:   "GET /api/instance/dns-target",
			method: http.MethodGet,
			path:   "/api/instance/dns-target",
			body:   func() []byte { return nil },
		},
	}
}

// adminManagementRouteCases lists every admin-management route mounted in
// routes.go, restricted to owner (ADM-09). Before this, only
// internal/api/admins_test.go exercised these routes, through a router it
// assembles itself rather than buildAdminRouter, so removing RequireRole
// from routes.go's real wiring (validation.md M8) broke nothing.
func adminManagementRouteCases() []routeCase {
	return []routeCase{
		{
			name:   "POST /api/admins",
			method: http.MethodPost,
			path:   "/api/admins",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{
					"email": fmt.Sprintf("cli-routes-test-admin-%d@example.com", time.Now().UnixNano()),
					"role":  db.RoleViewer,
				})
				return b
			},
		},
		{
			name:   "GET /api/admins",
			method: http.MethodGet,
			path:   "/api/admins",
			body:   func() []byte { return nil },
		},
		{
			name:   "PATCH /api/admins/{id}/role",
			method: http.MethodPatch,
			path:   "/api/admins/" + routesTestNonexistentID + "/role",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{"role": db.RoleViewer})
				return b
			},
		},
		{
			name:   "DELETE /api/admins/{id}",
			method: http.MethodDelete,
			path:   "/api/admins/" + routesTestNonexistentID,
			body:   func() []byte { return nil },
		},
	}
}

func doRouteRequest(t *testing.T, r http.Handler, token string, rt routeCase) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(rt.method, rt.path, bytes.NewReader(rt.body()))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestAdminRouter_Viewer_AllWriteRoutes_403 is Fix 1 (ADM-10, ADM-11): every
// mvp-core write route mounted by buildAdminRouter must reject viewer with
// 403, not just /api/domains.
func TestAdminRouter_Viewer_AllWriteRoutes_403(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleViewer)

	for _, rt := range writeRouteCases() {
		t.Run(rt.name, func(t *testing.T) {
			rec := doRouteRequest(t, r, token, rt)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}
}

// TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization is Fix 1
// (ADM-10): owner and operator must clear RequireRole on every mvp-core
// write route - i.e. the response must never be 401/403. It does not assert
// full handler success: several cases reference IDs (incident, domain) that
// don't exist in this test, so a 404/422/500 from business logic downstream
// of authorization is an expected, acceptable outcome here.
func TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization(t *testing.T) {
	// This test's write-route assertions depend on the admin tokens it
	// issues still resolving to real admins mid-test - a concurrent
	// bulk-clear of the shared `admins` table by another package's
	// bootstrap tests would break that. issueRoutesTestToken (called in
	// each role's subtest below) takes LockAdminsTable itself, so there is
	// no separate lock call here - taking it at this level too would
	// deadlock, since each subtest runs on its own *testing.T (a
	// different session's dedicated lock connection) while this level's
	// connection sits held until this function returns.
	r, _, admins := newAdminRouterForTest(t)

	for _, role := range []string{db.RoleOwner, db.RoleOperator} {
		role := role
		t.Run(role, func(t *testing.T) {
			token := issueRoutesTestToken(t, admins, role)
			for _, rt := range writeRouteCases() {
				t.Run(rt.name, func(t *testing.T) {
					rec := doRouteRequest(t, r, token, rt)
					if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
						t.Errorf("status = %d, want not 401/403 for role %q, body = %s", rec.Code, role, rec.Body.String())
					}
				})
			}
		})
	}
}

// TestAdminRouter_OperatorAndViewer_AdminManagementRoutes_403 is Fix 2
// (ADM-09): admin-management routes must reject operator and viewer with
// 403 through the real router, not just through admins_test.go's
// self-assembled router.
func TestAdminRouter_OperatorAndViewer_AdminManagementRoutes_403(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)

	for _, role := range []string{db.RoleOperator, db.RoleViewer} {
		role := role
		t.Run(role, func(t *testing.T) {
			token := issueRoutesTestToken(t, admins, role)
			for _, rt := range adminManagementRouteCases() {
				t.Run(rt.name, func(t *testing.T) {
					rec := doRouteRequest(t, r, token, rt)
					if rec.Code != http.StatusForbidden {
						t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
					}
				})
			}
		})
	}
}

// TestAdminRouter_Owner_AdminManagementRoutes_PassAuthorization is Fix 2
// (ADM-09): owner must clear RequireRole on every admin-management route
// through the real router.
func TestAdminRouter_Owner_AdminManagementRoutes_PassAuthorization(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOwner)

	for _, rt := range adminManagementRouteCases() {
		t.Run(rt.name, func(t *testing.T) {
			rec := doRouteRequest(t, r, token, rt)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("status = %d, want not 401/403 for owner, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAdminRouter_Viewer_PollerStatus_200 is Fix 5 (ADM-13): viewer must be
// able to read poller status through the real router (anyRole, not
// writeRoles - validation.md M12).
func TestAdminRouter_Viewer_PollerStatus_200(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleViewer)

	req := httptest.NewRequest(http.MethodGet, "/api/poller/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// buildMultipartLogoBody builds a minimal multipart/form-data body for
// POST /api/company-settings/logo, returning it alongside the request's
// Content-Type header value.
func buildMultipartLogoBody(t *testing.T) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("logo", "logo.png")
	if err != nil {
		t.Fatalf("CreateFormFile() returned unexpected error: %v", err)
	}
	if _, err := part.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}); err != nil {
		t.Fatalf("part.Write() returned unexpected error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() returned unexpected error: %v", err)
	}
	return buf.Bytes(), writer.FormDataContentType()
}

// companySettingsRouteCases lists the 3 company settings routes mounted in
// routes.go (SET-02), each restricted to owner only.
func companySettingsRouteCases(t *testing.T) []routeCase {
	logoBody, _ := buildMultipartLogoBody(t)
	return []routeCase{
		{
			name:   "GET /api/company-settings",
			method: http.MethodGet,
			path:   "/api/company-settings",
			body:   func() []byte { return nil },
		},
		{
			name:   "PATCH /api/company-settings",
			method: http.MethodPatch,
			path:   "/api/company-settings",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{"name": "Acme Inc.", "contact_email": "owner@acme.example.com"})
				return b
			},
		},
		{
			name:   "POST /api/company-settings/logo",
			method: http.MethodPost,
			path:   "/api/company-settings/logo",
			body:   func() []byte { return append([]byte{}, logoBody...) },
		},
	}
}

func doCompanySettingsRequest(t *testing.T, r http.Handler, token string, rt routeCase, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(rt.method, rt.path, bytes.NewReader(rt.body()))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestAdminRouter_CompanySettings_OperatorAndViewer_403 is T8's RBAC
// requirement (SET-02): every company settings route rejects operator and
// viewer with 403, the same way admin-management routes do.
func TestAdminRouter_CompanySettings_OperatorAndViewer_403(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	_, logoContentType := buildMultipartLogoBody(t)

	for _, role := range []string{db.RoleOperator, db.RoleViewer} {
		role := role
		t.Run(role, func(t *testing.T) {
			token := issueRoutesTestToken(t, admins, role)
			for _, rt := range companySettingsRouteCases(t) {
				t.Run(rt.name, func(t *testing.T) {
					contentType := "application/json"
					if rt.method == http.MethodPost {
						contentType = logoContentType
					}
					rec := doCompanySettingsRequest(t, r, token, rt, contentType)
					if rec.Code != http.StatusForbidden {
						t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
					}
				})
			}
		})
	}
}

// TestAdminRouter_CompanySettings_Owner_PassAuthorization is T8's RBAC
// requirement (SET-02): owner clears RequireRole on every company settings
// route - the response must never be 401/403 (it may still be a normal
// application-level status like 200 or 422).
func TestAdminRouter_CompanySettings_Owner_PassAuthorization(t *testing.T) {
	r, _, admins := newAdminRouterForTest(t)
	token := issueRoutesTestToken(t, admins, db.RoleOwner)
	_, logoContentType := buildMultipartLogoBody(t)

	for _, rt := range companySettingsRouteCases(t) {
		t.Run(rt.name, func(t *testing.T) {
			contentType := "application/json"
			if rt.method == http.MethodPost {
				contentType = logoContentType
			}
			rec := doCompanySettingsRequest(t, r, token, rt, contentType)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("status = %d, want not 401/403 for owner, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAdminRouter_CompanySettings_NoSession_401 confirms the company
// settings routes require authentication at all - no Authorization header
// or session cookie gets 401 before RequireRole is ever reached.
func TestAdminRouter_CompanySettings_NoSession_401(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)
	_, logoContentType := buildMultipartLogoBody(t)

	for _, rt := range companySettingsRouteCases(t) {
		t.Run(rt.name, func(t *testing.T) {
			contentType := "application/json"
			if rt.method == http.MethodPost {
				contentType = logoContentType
			}
			rec := doCompanySettingsRequest(t, r, "", rt, contentType)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

// TestAdminRouter_UploadsLogoFile_NoSession_ReachesHandler asserts SET-12:
// GET /uploads/{filename} on the real admin router requires no session at
// all - it must never respond 401/403 the way every other route in this
// router does without one. No logo has been uploaded in this test, so the
// expected outcome is 404 (missing file), proving the request reached
// logoFileHandler rather than being rejected by auth middleware.
func TestAdminRouter_UploadsLogoFile_NoSession_ReachesHandler(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want not 401/403 (no auth required for /uploads/)", rec.Code)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (no logo uploaded in this test)", rec.Code, http.StatusNotFound)
	}
}

// TestAdminRouter_StatusPageDomainAttachAndDNSTarget_NoSession_401 asserts
// SPD-11: PATCH /api/status-pages/{id}/domain and GET
// /api/instance/dns-target both require authentication - no Authorization
// header gets 401 before RequireRole is ever reached.
func TestAdminRouter_StatusPageDomainAttachAndDNSTarget_NoSession_401(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	cases := []routeCase{
		{
			name:   "PATCH /api/status-pages/{id}/domain",
			method: http.MethodPatch,
			path:   "/api/status-pages/" + routesTestNonexistentID + "/domain",
			body: func() []byte {
				b, _ := json.Marshal(map[string]string{"domain_id": routesTestNonexistentID, "subdomain": "status"})
				return b
			},
		},
		{
			name:   "GET /api/instance/dns-target",
			method: http.MethodGet,
			path:   "/api/instance/dns-target",
			body:   func() []byte { return nil },
		},
	}

	for _, rt := range cases {
		t.Run(rt.name, func(t *testing.T) {
			rec := doRouteRequest(t, r, "", rt)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

// bootstrapRoutesRawRow and clearAdminsForBootstrapRoutesTest snapshot and
// restore the admins table (and its two FK-dependent tables) the same way
// internal/db's and internal/api's own bootstrap tests do: proving the
// bootstrap routes actually create the first admin through the real
// production router needs a table with a known admin count, and the
// shared TEST_DATABASE_URL database otherwise carries whatever other
// suites' tests left behind.
type bootstrapRoutesRawRow struct{ values []any }

func snapshotTableForBootstrapRoutesTest(t *testing.T, pool *db.Pool, ctx context.Context, query string) []bootstrapRoutesRawRow {
	t.Helper()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		t.Fatalf("failed to snapshot table (%s): %v", query, err)
	}
	defer rows.Close()

	var saved []bootstrapRoutesRawRow
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatalf("failed to scan snapshotted row (%s): %v", query, err)
		}
		saved = append(saved, bootstrapRoutesRawRow{values: values})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed while iterating snapshotted rows (%s): %v", query, err)
	}
	return saved
}

func clearAdminsForBootstrapRoutesTest(t *testing.T, pool *db.Pool) func() {
	t.Helper()
	ctx := context.Background()

	// Serialize against every other package's tests that bulk-clear or
	// exact-count the shared `admins` table - see LockAdminsTable's doc
	// comment for why this is needed across concurrently-run packages.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

	invites := snapshotTableForBootstrapRoutesTest(t, pool, ctx,
		"SELECT id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at FROM admin_invites")
	tokens := snapshotTableForBootstrapRoutesTest(t, pool, ctx,
		"SELECT id, admin_id, token_hash, expires_at, used_at FROM password_reset_tokens")
	admins := snapshotTableForBootstrapRoutesTest(t, pool, ctx,
		"SELECT id, email, password_hash, role, sessions_revoked_at, created_at FROM admins")

	clearAll := func() {
		if _, err := pool.Exec(ctx, "DELETE FROM admin_invites"); err != nil {
			t.Fatalf("failed to clear admin_invites: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM password_reset_tokens"); err != nil {
			t.Fatalf("failed to clear password_reset_tokens: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM admins"); err != nil {
			t.Fatalf("failed to clear admins table for bootstrap routes test: %v", err)
		}
	}
	clearAll()

	return func() {
		clearAll()
		for _, a := range admins {
			if _, err := pool.Exec(ctx,
				"INSERT INTO admins (id, email, password_hash, role, sessions_revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
				a.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted admin: %v", err)
			}
		}
		for _, inv := range invites {
			if _, err := pool.Exec(ctx,
				"INSERT INTO admin_invites (id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
				inv.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted admin_invite: %v", err)
			}
		}
		for _, tok := range tokens {
			if _, err := pool.Exec(ctx,
				"INSERT INTO password_reset_tokens (id, admin_id, token_hash, expires_at, used_at) VALUES ($1, $2, $3, $4, $5)",
				tok.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted password_reset_token: %v", err)
			}
		}
	}
}

// TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter asserts
// SHD-14/SHD-15: GET /api/bootstrap/status and POST /api/bootstrap are
// mounted on the exact router buildAdminRouter returns for production,
// not a hand-rolled test router - and a full status-then-create round
// trip against an admin-less table behaves as designed (SHD-16).
func TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter(t *testing.T) {
	r, pool, _ := newAdminRouterForTest(t)
	restore := clearAdminsForBootstrapRoutesTest(t, pool)
	t.Cleanup(restore)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/bootstrap/status", nil)
	statusRec := httptest.NewRecorder()
	r.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("GET /api/bootstrap/status status = %d, want 200", statusRec.Code)
	}
	var statusBody map[string]bool
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusBody); err != nil {
		t.Fatalf("status response is not valid JSON: %v", err)
	}
	if statusBody["bootstrapped"] {
		t.Error(`GET /api/bootstrap/status "bootstrapped" = true on an admin-less table, want false`)
	}

	createBody, err := json.Marshal(map[string]string{
		"email":    fmt.Sprintf("cli-routes-bootstrap-test-%d@example.com", time.Now().UnixNano()),
		"password": "correct-horse-battery-staple",
	})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("POST /api/bootstrap status = %d, want 200, body = %s", createRec.Code, createRec.Body.String())
	}

	statusAfterRec := httptest.NewRecorder()
	r.ServeHTTP(statusAfterRec, httptest.NewRequest(http.MethodGet, "/api/bootstrap/status", nil))
	var statusAfterBody map[string]bool
	if err := json.Unmarshal(statusAfterRec.Body.Bytes(), &statusAfterBody); err != nil {
		t.Fatalf("status response is not valid JSON: %v", err)
	}
	if !statusAfterBody["bootstrapped"] {
		t.Error(`GET /api/bootstrap/status "bootstrapped" = false after a successful bootstrap, want true`)
	}
}

// TestAdminRouter_ExistingAPIRoute_StillReturnsJSON_AfterNotFoundWired is
// the design.md-flagged risk test: r.NotFound(web.StaticHandler()) must
// never shadow an already-registered /api/* route. GET /healthz is
// registered by router.New before buildAdminRouter adds anything else,
// so if NotFound registration order ever broke chi's dispatch, this is
// exactly the route that would silently start returning the SPA's
// index.html instead of its own JSON.
func TestAdminRouter_ExistingAPIRoute_StillReturnsJSON_AfterNotFoundWired(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Fatalf("GET /healthz Content-Type = %q, want application/json (means NotFound intercepted a real route)", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /healthz body is not valid JSON (means it fell back to the SPA's index.html): %v (body=%q)", err, rec.Body.String())
	}
}

// TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML asserts the
// SPA fallback: a path that matches no registered route and does not
// start with /api/ gets the embedded index.html through the real
// production router.
func TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/some-spa-route", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
		t.Errorf("Content-Type = %q, want it to mention html", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<html")) {
		t.Errorf("body does not look like index.html: %q", rec.Body.String())
	}
}

// TestAdminRouter_UnmatchedAPIPath_ReturnsJSON404NotHTML asserts an
// unmatched /api/... path still reads as an API error, not the SPA
// fallback, through the real production router.
func TestAdminRouter_UnmatchedAPIPath_ReturnsJSON404NotHTML(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON (means it fell back to index.html): %v (body=%q)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Error(`response body missing a non-empty "error" field`)
	}
}

// TestAdminRouter_SecurityHeaders_SetOnAdminListener is the M14 regression
// guard for the admin HTTP listener specifically: previously nothing but
// CORS touched response headers on this listener.
func TestAdminRouter_SecurityHeaders_SetOnAdminListener(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("Content-Security-Policy header is empty, want a policy set")
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want empty on the plain HTTP admin listener", got)
	}
}

// TestAdminRouter_LoginRateLimit_ExceedsBurst_429 is the H10 regression
// guard, exercised through the real production router (not a standalone
// test-only chi mux): login had no rate limit at all before - an attacker
// could brute-force a password with unbounded, unthrottled requests.
func TestAdminRouter_LoginRateLimit_ExceedsBurst_429(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	body, err := json.Marshal(map[string]string{"email": "nobody@example.com", "password": "wrong-password"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	postLogin := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.9:54321"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// credentialRouteBurst (10) requests should all pass the rate limiter
	// (each still 401s on the wrong credentials - only the limiter's
	// behavior is under test here).
	for i := 0; i < credentialRouteBurst; i++ {
		rec := postLogin()
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: status = %d, want %d (rate limiter must not block within burst)", i, rec.Code, http.StatusUnauthorized)
		}
	}

	rec := postLogin()
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request past burst: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// TestAdminRouter_LoginRateLimit_SharedAcrossCredentialRoutes_429 asserts
// the limiter's budget is shared across every credential-sensitive route,
// not reset per-route - otherwise an attacker could multiply their
// effective rate simply by spreading guesses across login/password-reset
// (H10).
func TestAdminRouter_LoginRateLimit_SharedAcrossCredentialRoutes_429(t *testing.T) {
	r, _, _ := newAdminRouterForTest(t)

	loginBody, err := json.Marshal(map[string]string{"email": "nobody@example.com", "password": "wrong-password"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	resetBody, err := json.Marshal(map[string]string{"email": "nobody@example.com"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	post := func(path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < credentialRouteBurst; i++ {
		var rec *httptest.ResponseRecorder
		if i%2 == 0 {
			rec = post("/api/auth/login", loginBody)
		} else {
			rec = post("/api/auth/password-reset/request", resetBody)
		}
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 within the shared burst, want it exhausted only after %d total requests across both routes", i, credentialRouteBurst)
		}
	}

	rec := post("/api/auth/login", loginBody)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d (budget shared with password-reset/request must already be exhausted)", rec.Code, http.StatusTooManyRequests)
	}
}
