//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// TestTenantContext_AuthenticatedRequest_AppIsSystemNeverSet is the HTTP
// half of AD-024's containment guarantee: app.is_system - the flag the
// tenants system_iteration_read policy (0027) trusts to mean "this
// session is the poller, not a request" - must never be set on a
// transaction opened for an HTTP request. Here the request is fully
// authenticated with an active tenant, i.e. the most privileged shape the
// admin API ever runs in, and the probe reads the flag from the request's
// own transaction, where TenantContext already set app.user_id and
// app.tenant_id.
func TestTenantContext_AuthenticatedRequest_AppIsSystemNeverSet(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	var tenantID string
	if err := pool.QueryRow(context.Background(), "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "system-flag-http-test-tenant").Scan(&tenantID); err != nil {
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	var gotIsSystem string
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// COALESCE - current_setting(_, true) returns NULL, not "", for a
		// GUC never set in this session, which is exactly the expected
		// state here.
		if err := pool.QueryRow(r.Context(), "SELECT COALESCE(current_setting('app.is_system', true), '')").Scan(&gotIsSystem); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r := RequireAuth(middlewareTestSecret, db.NewUserRepository(pool), db.NewSessionRepository(pool), zap.NewNop())(
		TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop())(probe),
	)

	token, err := auth.IssueSessionWithTenant(admin.ID, tenantID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSessionWithTenant() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotIsSystem == "true" {
		t.Errorf("app.is_system seen by an authenticated request's transaction = %q, want it unset (AD-024: no HTTP path may claim system scope)", gotIsSystem)
	}
}
