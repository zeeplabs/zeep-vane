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

// newTenantContextTestRouter builds RequireAuth -> TenantContext -> a probe
// handler that reads back the RLS session settings (app.user_id,
// app.tenant_id) as seen by the request's own transaction - the exact
// values every tenant-scoped policy in 0024 checks - proving what
// TenantContext actually set, independent of RLS enforcement itself
// (already proven at the database level by internal/db/rls_test.go; the
// disposable test container's bootstrap role is a superuser there too, and
// this package's tests all run as that same role, so asserting on a
// filtered row count here would just retest the superuser-bypass problem
// rls_test.go already solved with a dedicated non-superuser role).
func newTenantContextTestRouter(pool *db.Pool, gotUserID, gotTenantID *string) http.Handler {
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// COALESCE(...,'') - current_setting(_, true) returns NULL (not
		// "") when the GUC was never set at all in this session, which
		// TenantContext deliberately skips (BeginTenantTx) when there is
		// no active tenant; a plain Scan into *string would fail on NULL.
		if err := pool.QueryRow(r.Context(), "SELECT COALESCE(current_setting('app.user_id', true), '')").Scan(gotUserID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := pool.QueryRow(r.Context(), "SELECT COALESCE(current_setting('app.tenant_id', true), '')").Scan(gotTenantID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return RequireAuth(middlewareTestSecret, db.NewUserRepository(pool), db.NewSessionRepository(pool), zap.NewNop())(
		TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop())(probe),
	)
}

// TestTenantContext_ActiveTenantClaim_SetsAppTenantID proves TENANT-02: a
// request whose session carries an active tenant runs its transaction with
// app.tenant_id set to exactly that tenant (and app.user_id set to the
// authenticated admin) before the handler executes - the two RLS session
// settings every tenant-scoped policy (0024) checks.
func TestTenantContext_ActiveTenantClaim_SetsAppTenantID(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	var tenantID string
	if err := pool.QueryRow(context.Background(), "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "tenant-context-test-tenant").Scan(&tenantID); err != nil {
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	var gotUserID, gotTenantID string
	r := newTenantContextTestRouter(pool, &gotUserID, &gotTenantID)

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
	if gotTenantID != tenantID {
		t.Errorf("app.tenant_id seen by the handler's transaction = %q, want %q", gotTenantID, tenantID)
	}
	if gotUserID != admin.ID {
		t.Errorf("app.user_id seen by the handler's transaction = %q, want %q", gotUserID, admin.ID)
	}
}

// TestTenantContext_NoActiveTenant_AppTenantIDUnset proves TENANT-03: a
// session with no active tenant (IssueSession, tenant claim "") leaves
// app.tenant_id unset for the request's transaction - never defaulted to
// some other tenant, and never erroring - which every tenant-scoped RLS
// policy treats as fail-closed (zero rows), not a leak.
func TestTenantContext_NoActiveTenant_AppTenantIDUnset(t *testing.T) {
	repo, _ := newMiddlewareTestAdmins(t)
	// A plain, non-tenant-scoped pool: this test's whole point is that the
	// setting is genuinely absent, which the fixture pools deliberately
	// preset at connection level (see newAPITenantScopedPool).
	pool := newPlainTestPool(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	var gotUserID, gotTenantID string
	r := newTenantContextTestRouter(pool, &gotUserID, &gotTenantID)

	token, err := auth.IssueSession(admin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotTenantID != "" {
		t.Errorf("app.tenant_id seen by the handler's transaction = %q, want unset (\"\")", gotTenantID)
	}
	if gotUserID != admin.ID {
		t.Errorf("app.user_id seen by the handler's transaction = %q, want %q (set independent of an active tenant)", gotUserID, admin.ID)
	}
}

// TestTenantContext_NoAdminInContext_401 proves the middleware refuses to
// run ahead of RequireAuth silently - the same "no admin in context"
// posture RequireRole already has.
func TestTenantContext_NoAdminInContext_401(t *testing.T) {
	_, pool := newMiddlewareTestAdmins(t)

	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler ran without an admin in context - TenantContext should have rejected the request first")
	})
	r := TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop())(probe)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
