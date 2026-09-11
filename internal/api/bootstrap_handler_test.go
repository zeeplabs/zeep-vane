//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

const testBootstrapSessionSecret = "test-bootstrap-session-secret-32bytes!!"

func newBootstrapRouter(t *testing.T) (http.Handler, *db.UserRepository, *db.Pool) {
	t.Helper()

	pool, _ := newAPITenantScopedPool(t)

	repo := db.NewUserRepository(pool)
	tenants := db.NewTenantRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	// secureCookies=true: default behavior, no test in this file exercises
	// the VANE_SECURE_COOKIES=false path (covered in auth_handler_test.go).
	handler := NewBootstrapHandler(pool, repo, tenants, memberships, db.NewSessionRepository(pool), zap.NewNop(), testBootstrapSessionSecret, true)

	r := chi.NewRouter()
	r.Get("/api/bootstrap/status", handler.Status)
	r.Post("/api/bootstrap", handler.Create)

	return r, repo, pool
}

func bootstrapUniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("bootstrap-handler-test-%d@example.com", time.Now().UnixNano())
}

// bootstrapRawRow and clearAdminsForBootstrapTest snapshot and restore the
// admins table (plus its two FK-dependent tables) exactly the same way
// internal/db's own BootstrapFirst tests do: this handler's whole
// contract - both "no admin yet" and "refuse once one exists" - is only
// observable against a table with a known admin count, and the shared
// TEST_DATABASE_URL database otherwise carries whatever admins other
// suites' tests left behind.
type bootstrapRawRow struct{ values []any }

func snapshotTableForBootstrapTest(t *testing.T, pool *db.Pool, ctx context.Context, query string) []bootstrapRawRow {
	t.Helper()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		t.Fatalf("failed to snapshot table (%s): %v", query, err)
	}
	defer rows.Close()

	var saved []bootstrapRawRow
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatalf("failed to scan snapshotted row (%s): %v", query, err)
		}
		saved = append(saved, bootstrapRawRow{values: values})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed while iterating snapshotted rows (%s): %v", query, err)
	}
	return saved
}

func clearAdminsForBootstrapTest(t *testing.T, pool *db.Pool) func() {
	t.Helper()
	ctx := context.Background()

	// Serialize against every other package's tests that bulk-clear or
	// exact-count the shared `admins` table - see LockUsersTable's doc
	// comment for why this is needed across concurrently-run packages.
	dbtest.LockUsersTable(t, ctx, testDatabaseURL(t))

	invites := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, tenant_id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at FROM tenant_invites")
	tokens := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, user_id, token_hash, expires_at, used_at FROM password_reset_tokens")
	admins := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, email, password_hash, sessions_revoked_at, email_verified_at, created_at FROM users")

	clearAll := func() {
		if _, err := pool.Exec(ctx, "DELETE FROM tenant_invites"); err != nil {
			t.Fatalf("failed to clear tenant_invites: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM password_reset_tokens"); err != nil {
			t.Fatalf("failed to clear password_reset_tokens: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM users"); err != nil {
			t.Fatalf("failed to clear admins table for bootstrap handler test: %v", err)
		}
	}
	clearAll()

	return func() {
		clearAll()
		for _, a := range admins {
			if _, err := pool.Exec(ctx,
				"INSERT INTO users (id, email, password_hash, sessions_revoked_at, email_verified_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
				a.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted admin: %v", err)
			}
		}
		for _, inv := range invites {
			if _, err := pool.Exec(ctx,
				"INSERT INTO tenant_invites (id, tenant_id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)",
				inv.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted admin_invite: %v", err)
			}
		}
		for _, tok := range tokens {
			if _, err := pool.Exec(ctx,
				"INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at, used_at) VALUES ($1, $2, $3, $4, $5)",
				tok.values...,
			); err != nil {
				t.Fatalf("failed to restore snapshotted password_reset_token: %v", err)
			}
		}
	}
}

func getBootstrapStatus(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func postBootstrap(t *testing.T, r http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bootstrap", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestBootstrapHandler_Status_NoAdmins_ReturnsFalse(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	rec := getBootstrapStatus(t, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body bootstrapStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body.Bootstrapped {
		t.Error("Bootstrapped = true on an admin-less table, want false")
	}
}

func TestBootstrapHandler_Status_AfterSuccessfulCreate_ReturnsTrue(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	createRec := postBootstrap(t, r, bootstrapCreateRequest{
		Name:     "Test Owner",
		Email:    bootstrapUniqueTestEmail(t),
		Password: "correct-horse-battery-staple",
	})
	if createRec.Code != http.StatusOK {
		t.Fatalf("POST /api/bootstrap status = %d, want 200 (body=%q)", createRec.Code, createRec.Body.String())
	}

	statusRec := getBootstrapStatus(t, r)
	var body bootstrapStatusResponse
	if err := json.Unmarshal(statusRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if !body.Bootstrapped {
		t.Error("Bootstrapped = false after a successful bootstrap, want true")
	}
}

func TestBootstrapHandler_Create_Success_SetsSessionCookieAndReturnsIdentity(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	email := bootstrapUniqueTestEmail(t)
	rec := postBootstrap(t, r, bootstrapCreateRequest{Name: "Test Owner", Email: email, Password: "correct-horse-battery-staple"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}

	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body.Email != email {
		t.Errorf("response Email = %q, want %q", body.Email, email)
	}
	if body.Role != db.RoleOwner {
		t.Errorf("response Role = %q, want %q", body.Role, db.RoleOwner)
	}
	if body.ID == "" {
		t.Error("response ID is empty, want the new admin's id")
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "vane_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no vane_session cookie set on successful bootstrap")
	}
	if sessionCookie.Value == "" {
		t.Error("vane_session cookie value is empty")
	}
	if !sessionCookie.HttpOnly {
		t.Error("vane_session cookie is not HttpOnly")
	}
	if !sessionCookie.Secure {
		t.Error("vane_session cookie is not Secure")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("vane_session cookie SameSite = %v, want Strict", sessionCookie.SameSite)
	}
}

// TestBootstrapHandler_Create_Success_CreatesTenantAndOwnerMembership
// proves TENANT-05/06/07: bootstrapping a fresh instance creates exactly 1
// tenant and links the new admin to it as owner, atomically with each
// other.
func TestBootstrapHandler_Create_Success_CreatesTenantAndOwnerMembership(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	email := bootstrapUniqueTestEmail(t)
	tenantName := fmt.Sprintf("Test Owner Tenant %d", time.Now().UnixNano())
	rec := postBootstrap(t, r, bootstrapCreateRequest{Name: tenantName, Email: email, Password: "correct-horse-battery-staple"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}

	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	adminID := body.ID
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenant_memberships WHERE user_id = $1", adminID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE name = $1", tenantName)
	})

	ctx := context.Background()
	var tenantCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenants WHERE name = $1", tenantName).Scan(&tenantCount); err != nil {
		t.Fatalf("counting tenants returned unexpected error: %v", err)
	}
	if tenantCount != 1 {
		t.Fatalf("tenants row count for the new admin = %d, want 1", tenantCount)
	}

	var membershipRole string
	row := pool.QueryRow(ctx,
		"SELECT tm.role FROM tenant_memberships tm JOIN tenants t ON t.id = tm.tenant_id WHERE tm.user_id = $1 AND t.name = $2",
		adminID, tenantName,
	)
	if err := row.Scan(&membershipRole); err != nil {
		t.Fatalf("expected exactly one owner membership linking the new admin to their tenant: %v", err)
	}
	if membershipRole != db.RoleOwner {
		t.Errorf("membership role = %q, want %q", membershipRole, db.RoleOwner)
	}
}

func TestBootstrapHandler_Create_AlreadyBootstrapped_Returns409NoSecondAdmin(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	firstName := fmt.Sprintf("First Owner Tenant %d", time.Now().UnixNano())
	first := postBootstrap(t, r, bootstrapCreateRequest{Name: firstName, Email: bootstrapUniqueTestEmail(t), Password: "correct-horse-battery-staple"})
	if first.Code != http.StatusOK {
		t.Fatalf("first POST /api/bootstrap status = %d, want 200 (body=%q)", first.Code, first.Body.String())
	}
	var firstBody meResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("first response body is not valid JSON: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenant_memberships WHERE user_id = $1", firstBody.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE name = $1", firstName)
	})

	second := postBootstrap(t, r, bootstrapCreateRequest{Name: "Second Owner Tenant", Email: bootstrapUniqueTestEmail(t), Password: "another-horse-battery-staple"})
	if second.Code != http.StatusConflict {
		t.Fatalf("second POST /api/bootstrap status = %d, want 409", second.Code)
	}
	if second.Body.String() != alreadyBootstrappedBody {
		t.Errorf("second POST /api/bootstrap body = %q, want %q", second.Body.String(), alreadyBootstrappedBody)
	}

	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after refused second bootstrap = %d, want 1", count)
	}

	// TENANT-05/07: a refused second bootstrap must never create a second
	// tenant either - not even the first tenant's row duplicated, and
	// definitely not one for the rejected request's name.
	var tenantCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenants WHERE name IN ($1, $2)", firstName, "Second Owner Tenant").Scan(&tenantCount); err != nil {
		t.Fatalf("counting tenants returned unexpected error: %v", err)
	}
	if tenantCount != 1 {
		t.Errorf("tenants row count after refused second bootstrap = %d, want 1 (only the first)", tenantCount)
	}
}

func TestBootstrapHandler_Create_EmptyPassword_Returns422NoAdminCreated(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	rec := postBootstrap(t, r, bootstrapCreateRequest{Name: "Test Owner", Email: bootstrapUniqueTestEmail(t), Password: ""})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("admins row count after a 422-rejected bootstrap = %d, want 0", count)
	}
}

// TestBootstrapHandler_Create_WeakPassword_Returns422NoAdminCreated is the
// H11 regression guard: a password below auth.MinPasswordLength must be
// rejected before hashing, not silently accepted as the first owner's
// credential.
func TestBootstrapHandler_Create_WeakPassword_Returns422NoAdminCreated(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	rec := postBootstrap(t, r, bootstrapCreateRequest{Name: "Test Owner", Email: bootstrapUniqueTestEmail(t), Password: "1234567"})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("admins row count after a weak-password-rejected bootstrap = %d, want 0", count)
	}
}
