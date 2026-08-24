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

func newBootstrapRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool) {
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

	repo := db.NewAdminRepository(pool)
	handler := NewBootstrapHandler(pool, repo, zap.NewNop(), testBootstrapSessionSecret)

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
	// exact-count the shared `admins` table - see LockAdminsTable's doc
	// comment for why this is needed across concurrently-run packages.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

	invites := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at FROM admin_invites")
	tokens := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, admin_id, token_hash, expires_at, used_at FROM password_reset_tokens")
	admins := snapshotTableForBootstrapTest(t, pool, ctx,
		"SELECT id, email, password_hash, role, sessions_revoked_at, created_at FROM admins")

	clearAll := func() {
		if _, err := pool.Exec(ctx, "DELETE FROM admin_invites"); err != nil {
			t.Fatalf("failed to clear admin_invites: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM password_reset_tokens"); err != nil {
			t.Fatalf("failed to clear password_reset_tokens: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM admins"); err != nil {
			t.Fatalf("failed to clear admins table for bootstrap handler test: %v", err)
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
	rec := postBootstrap(t, r, bootstrapCreateRequest{Email: email, Password: "correct-horse-battery-staple"})

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

func TestBootstrapHandler_Create_AlreadyBootstrapped_Returns409NoSecondAdmin(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	first := postBootstrap(t, r, bootstrapCreateRequest{Email: bootstrapUniqueTestEmail(t), Password: "correct-horse-battery-staple"})
	if first.Code != http.StatusOK {
		t.Fatalf("first POST /api/bootstrap status = %d, want 200 (body=%q)", first.Code, first.Body.String())
	}

	second := postBootstrap(t, r, bootstrapCreateRequest{Email: bootstrapUniqueTestEmail(t), Password: "another-horse-battery-staple"})
	if second.Code != http.StatusConflict {
		t.Fatalf("second POST /api/bootstrap status = %d, want 409", second.Code)
	}
	if second.Body.String() != alreadyBootstrappedBody {
		t.Errorf("second POST /api/bootstrap body = %q, want %q", second.Body.String(), alreadyBootstrappedBody)
	}

	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after refused second bootstrap = %d, want 1", count)
	}
}

func TestBootstrapHandler_Create_EmptyPassword_Returns422NoAdminCreated(t *testing.T) {
	r, _, pool := newBootstrapRouter(t)
	restore := clearAdminsForBootstrapTest(t, pool)
	t.Cleanup(restore)

	rec := postBootstrap(t, r, bootstrapCreateRequest{Email: bootstrapUniqueTestEmail(t), Password: ""})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	ctx := context.Background()
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("admins row count after a 422-rejected bootstrap = %d, want 0", count)
	}
}
