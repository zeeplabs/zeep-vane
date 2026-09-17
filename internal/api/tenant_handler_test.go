//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// buildTenantRouter mirrors buildCompanySettingsRouter: RequireAuth +
// TenantContext only, no RequireRole - RBAC for /api/tenants/current is
// asserted at the routes.go wiring level (internal/cli), not here.
func buildTenantRouter(pool *db.Pool, admins *db.UserRepository) http.Handler {
	handler := NewTenantHandler(db.NewTenantMembershipRepository(pool), db.NewTenantRepository(pool), audit.NewLog(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Delete("/api/tenants/current", handler.Delete)
	})
	return r
}

func deleteCurrentTenant(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/tenants/current", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// seedOwnerSession creates a user with an owner membership on tenantID and
// returns both a session token bound to it and the user's id (the caller
// often needs the id to add a second membership afterward).
func seedOwnerSession(t *testing.T, users *db.UserRepository, tenantID string) (token, userID string) {
	t.Helper()
	ctx := context.Background()
	dbtest.LockUsersTable(t, ctx, testDatabaseURL(t))

	user := &db.User{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("users.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = users.Delete(context.Background(), user.ID) })

	seedMembership(t, user.ID, tenantID, db.RoleOwner)

	tok, err := auth.IssueSessionWithTenant(user.ID, tenantID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSessionWithTenant() returned unexpected error: %v", err)
	}
	return tok, user.ID
}

// seedSecondTenant creates a throwaway extra tenant so a test can give one
// user memberships in two tenants at once.
func seedSecondTenant(t *testing.T, pool *db.Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO tenants (name) VALUES ($1) RETURNING id", "tenant-handler-second-tenant",
	).Scan(&tenantID); err != nil {
		t.Fatalf("seeding second tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })
	return tenantID
}

// TestDeleteTenant_NoSession_401 asserts the handler rejects a request with
// no session before touching any membership/tenant data.
func TestDeleteTenant_NoSession_401(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)

	rec := deleteCurrentTenant(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// TestDeleteTenant_OnlyActiveTenant_409NoChange asserts CFGPG-11: a user
// with exactly one active tenant is blocked from deleting it, and the
// tenant's status is left untouched.
func TestDeleteTenant_OnlyActiveTenant_409NoChange(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)
	tenantID := apiTestTenantID(t)
	token, _ := seedOwnerSession(t, admins, tenantID)

	rec := deleteCurrentTenant(t, r, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	var status string
	if err := pool.QueryRow(context.Background(), "SELECT status FROM tenants WHERE id = $1", tenantID).Scan(&status); err != nil {
		t.Fatalf("querying tenant status returned unexpected error: %v", err)
	}
	if status != "active" {
		t.Errorf("tenant status = %q, want %q (unchanged)", status, "active")
	}
}

// TestDeleteTenant_SecondActiveTenantExists_200SoftDeletes asserts
// CFGPG-09: when the caller has another active tenant, deleting the
// active one soft-deletes it (status='deleted', deleted_at set) without
// touching the other tenant.
func TestDeleteTenant_SecondActiveTenantExists_200SoftDeletes(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)
	tenantID := apiTestTenantID(t)
	token, userID := seedOwnerSession(t, admins, tenantID)
	otherTenantID := seedSecondTenant(t, pool)
	seedMembership(t, userID, otherTenantID, db.RoleOwner)

	rec := deleteCurrentTenant(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if !resp["deleted"] {
		t.Errorf("response = %v, want deleted=true", resp)
	}

	var status string
	var deletedAt *string
	if err := pool.QueryRow(context.Background(),
		"SELECT status, deleted_at::text FROM tenants WHERE id = $1", tenantID,
	).Scan(&status, &deletedAt); err != nil {
		t.Fatalf("querying tenant status returned unexpected error: %v", err)
	}
	if status != "deleted" {
		t.Errorf("tenant status = %q, want %q", status, "deleted")
	}
	if deletedAt == nil {
		t.Error("deleted_at = nil, want a timestamp")
	}

	var otherStatus string
	if err := pool.QueryRow(context.Background(), "SELECT status FROM tenants WHERE id = $1", otherTenantID).Scan(&otherStatus); err != nil {
		t.Fatalf("querying other tenant status returned unexpected error: %v", err)
	}
	if otherStatus != "active" {
		t.Errorf("other tenant status = %q, want unchanged %q", otherStatus, "active")
	}
}

// TestDeleteTenant_SecondActiveTenantExists_RecordsTenantDeletedAuditLabelSurvivingDelete
// covers AUDITEXP-15: the audit entry's target_label survives the delete
// (captured before it runs), same pattern already verified for
// service_deleted/status_page_deleted.
func TestDeleteTenant_SecondActiveTenantExists_RecordsTenantDeletedAuditLabelSurvivingDelete(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)
	tenantID := apiTestTenantID(t)
	token, userID := seedOwnerSession(t, admins, tenantID)
	otherTenantID := seedSecondTenant(t, pool)
	seedMembership(t, userID, otherTenantID, db.RoleOwner)

	// Give the fixture tenant a real name first: its default/reset state
	// (shared with company_settings_handler_test.go's fixture) is an empty
	// name, which the audit log persists as SQL NULL (audit.Log.Record's
	// documented nilIfEmpty convention) rather than exercising the
	// surviving-label assertion this test is actually for.
	wantName := "Audit Delete Tenant " + tenantID
	if _, err := pool.Exec(context.Background(), "UPDATE tenants SET name = $1 WHERE id = $2", wantName, tenantID); err != nil {
		t.Fatalf("seeding tenant name returned unexpected error: %v", err)
	}

	rec := deleteCurrentTenant(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE target_id = $1 AND action = 'tenant_deleted'", tenantID)
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != wantName {
		t.Errorf("admin_audit_log target_label = %q, want %q (must survive the delete above)", gotTargetLabel, wantName)
	}
}

// TestDeleteTenant_OnlyActiveTenant_NoAuditRecorded covers AUDITEXP-16: the
// 409 "last active tenant" path records nothing.
func TestDeleteTenant_OnlyActiveTenant_NoAuditRecorded(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)
	tenantID := apiTestTenantID(t)
	token, _ := seedOwnerSession(t, admins, tenantID)

	var before int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit_log WHERE action = 'tenant_deleted'").Scan(&before); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}

	rec := deleteCurrentTenant(t, r, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	var after int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM admin_audit_log WHERE action = 'tenant_deleted'").Scan(&after); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if after != before {
		t.Errorf("admin_audit_log tenant_deleted count went from %d to %d, want unchanged after a 409", before, after)
	}
}

// TestDeleteTenant_DeletedTenant_ExcludedFromListForUser asserts CFGPG-13:
// once soft-deleted, the tenant no longer shows up in the user's
// ListForUser (login/tenant-selector) results.
func TestDeleteTenant_DeletedTenant_ExcludedFromListForUser(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	r := buildTenantRouter(pool, admins)
	tenantID := apiTestTenantID(t)
	token, userID := seedOwnerSession(t, admins, tenantID)
	otherTenantID := seedSecondTenant(t, pool)
	seedMembership(t, userID, otherTenantID, db.RoleOwner)

	if rec := deleteCurrentTenant(t, r, token); rec.Code != http.StatusOK {
		t.Fatalf("setup DELETE status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	memberships := db.NewTenantMembershipRepository(pool)
	list, err := memberships.ListForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListForUser() returned unexpected error: %v", err)
	}
	for _, m := range list {
		if m.TenantID == tenantID {
			t.Errorf("ListForUser() still includes the deleted tenant %q", tenantID)
		}
	}
	if len(list) != 1 || list[0].TenantID != otherTenantID {
		t.Errorf("ListForUser() = %+v, want exactly the other tenant %q", list, otherTenantID)
	}
}
