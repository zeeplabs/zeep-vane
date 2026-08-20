//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	cfg := config.Config{SessionSecret: routesTestSessionSecret, MasterKey: "cli-routes-test-master-key"}
	handler := buildAdminRouter(pool, cfg, zap.NewNop())
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
