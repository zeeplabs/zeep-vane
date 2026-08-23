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
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const routesTestSessionSecret = "cli-routes-test-session-secret-32b!!"

func newAdminRouterForTest(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
	t.Helper()
	pool := newServeTestPool(t)
	cfg := config.Config{SessionSecret: routesTestSessionSecret, MasterKey: "cli-routes-test-master-key", UploadsDir: t.TempDir()}
	handler := buildAdminRouter(pool, cfg, zap.NewNop())

	// The company_settings row is a singleton shared across every test in
	// this package - reset it to a known state before and after each test.
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
func issueRoutesTestToken(t *testing.T, admins *db.AdminRepository, role string) string {
	t.Helper()
	ctx := context.Background()
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
