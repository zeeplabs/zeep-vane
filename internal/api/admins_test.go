//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newAdminsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository, *db.AdminInviteRepository) {
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

	admins := db.NewAdminRepository(pool)
	invites := db.NewAdminInviteRepository(pool)
	auditLog := audit.NewLog(pool)
	handler := NewAdminsHandler(pool, admins, invites, auditLog, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.With(RequireRole(db.RoleOwner)).Post("/api/admins", handler.Invite)
	})

	return r, pool, admins, invites
}

// issueTestSessionTokenWithRole is issueTestSessionToken, additionally
// setting the created admin's role - needed to exercise RequireRole's 403
// path for operator/viewer.
func issueTestSessionTokenWithRole(t *testing.T, admins *db.AdminRepository, role string) string {
	t.Helper()
	ctx := context.Background()
	admin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), admin.ID) })

	if role != db.RoleOwner {
		if err := admins.UpdateRole(ctx, admin.ID, role); err != nil {
			t.Fatalf("admins.UpdateRole() returned unexpected error: %v", err)
		}
	}

	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}
	return token
}

func postInviteAdmin(t *testing.T, r http.Handler, token, email, role string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(inviteAdminRequest{Email: email, Role: role})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// pendingInviteRow is the shape this test suite reads back directly from
// admin_invites by email - the handler never returns or logs the raw token
// where a test could recover it (one-way hash), so assertions read invite
// state straight from the table instead.
type pendingInviteRow struct {
	id     string
	role   string
	usedAt *time.Time
}

func latestInviteForEmail(t *testing.T, pool *db.Pool, email string) pendingInviteRow {
	t.Helper()
	row := pool.QueryRow(context.Background(),
		"SELECT id, role, used_at FROM admin_invites WHERE email = $1 ORDER BY created_at DESC LIMIT 1", email)

	var got pendingInviteRow
	if err := row.Scan(&got.id, &got.role, &got.usedAt); err != nil {
		t.Fatalf("querying latest admin_invites row for %q returned unexpected error: %v", email, err)
	}
	return got
}

func TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	ctx := context.Background()
	inviterEmail := uniqueTestEmail(t)
	inviter := &db.Admin{Email: inviterEmail, PasswordHash: "hash"}
	if err := admins.Create(ctx, inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token, err := auth.IssueSession(inviter.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })

	rec := postInviteAdmin(t, r, token, email, db.RoleOperator)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	invite := latestInviteForEmail(t, pool, email)
	if invite.role != db.RoleOperator {
		t.Errorf("invite.role = %q, want %q", invite.role, db.RoleOperator)
	}
	if invite.usedAt != nil {
		t.Error("invite.usedAt = non-nil, want nil (fresh invite)")
	}

	var gotActorID, gotAction string
	row := pool.QueryRow(context.Background(),
		"SELECT actor_id, action FROM admin_audit_log WHERE target_id = $1", invite.id)
	if err := row.Scan(&gotActorID, &gotAction); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if gotActorID != inviter.ID {
		t.Errorf("admin_audit_log actor_id = %q, want %q", gotActorID, inviter.ID)
	}
	if gotAction != "invited" {
		t.Errorf("admin_audit_log action = %q, want %q", gotAction, "invited")
	}
}

func TestInviteAdmin_Operator_403(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleOperator)

	rec := postInviteAdmin(t, r, token, uniqueTestEmail(t), db.RoleViewer)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestInviteAdmin_Viewer_403(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := postInviteAdmin(t, r, token, uniqueTestEmail(t), db.RoleViewer)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestInviteAdmin_DuplicatePendingInvite_InvalidatesPreviousWithoutDuplicateRow(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })

	first := postInviteAdmin(t, r, token, email, db.RoleOperator)
	if first.Code != http.StatusCreated {
		t.Fatalf("first invite status = %d, want %d", first.Code, http.StatusCreated)
	}
	firstInviteID := latestInviteForEmail(t, pool, email).id

	second := postInviteAdmin(t, r, token, email, db.RoleViewer)
	if second.Code != http.StatusCreated {
		t.Fatalf("second invite status = %d, want %d", second.Code, http.StatusCreated)
	}

	var firstUsedAt *time.Time
	row := pool.QueryRow(context.Background(), "SELECT used_at FROM admin_invites WHERE id = $1", firstInviteID)
	if err := row.Scan(&firstUsedAt); err != nil {
		t.Fatalf("querying first invite returned unexpected error: %v", err)
	}
	if firstUsedAt == nil {
		t.Error("first invite used_at = nil, want invalidated (non-nil) after a second invite for the same email")
	}

	var pendingCount int
	countRow := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM admin_invites WHERE email = $1 AND used_at IS NULL", email)
	if err := countRow.Scan(&pendingCount); err != nil {
		t.Fatalf("querying admin_invites returned unexpected error: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("pending admin_invites rows for %q = %d, want 1 (no duplicate)", email, pendingCount)
	}
}

func TestInviteAdmin_EmailAlreadyActiveAdmin_409(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)
	activeEmail := uniqueTestEmail(t)
	activeAdmin := &db.Admin{Email: activeEmail, PasswordHash: "hash"}
	if err := admins.Create(context.Background(), activeAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), activeAdmin.ID) })

	rec := postInviteAdmin(t, r, token, activeEmail, db.RoleOperator)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestInviteAdmin_InvalidRole_422(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postInviteAdmin(t, r, token, uniqueTestEmail(t), "superadmin")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
