//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const testSessionSecret = "test-session-secret-at-least-32-bytes!!"

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newLoginRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool) {
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
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret)

	r := chi.NewRouter()
	r.Post("/api/auth/login", handler.Login)

	return r, repo, pool
}

func uniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("auth-handler-test-%d@example.com", time.Now().UnixNano())
}

func createTestAdmin(t *testing.T, repo *db.AdminRepository, pool *db.Pool, email, plainPassword string) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	hash, err := auth.HashPassword(plainPassword)
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}

	admin := &db.Admin{Email: email, PasswordHash: hash}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
}

func postLogin(t *testing.T, r http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(loginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	return rec
}

func TestLogin_CorrectCredentials_200(t *testing.T) {
	r, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	rec := postLogin(t, r, email, "correct-horse-battery-staple")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Token == "" {
		t.Error("response body has no token, want a non-empty session token")
	}
}

func TestLogin_WrongPassword_401Generic(t *testing.T) {
	r, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	rec := postLogin(t, r, email, "wrong-password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Body.String() != genericLoginErrorBody {
		t.Errorf("body = %q, want %q", rec.Body.String(), genericLoginErrorBody)
	}
}

func newMeRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool) {
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
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret)

	r := chi.NewRouter()
	r.With(RequireAuth(testSessionSecret, repo)).Get("/api/auth/me", handler.Me)

	return r, repo, pool
}

func TestMe_ValidSession_200WithIdentity(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	created, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	// GetByEmail's SELECT omits role - re-fetch by ID (same lookup
	// RequireAuth performs) to get the value the handler will actually see.
	admin, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	token, err := auth.IssueSession(admin.ID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.ID != admin.ID || body.Email != admin.Email || body.Role != admin.Role {
		t.Errorf("body = %+v, want {ID:%q Email:%q Role:%q}", body, admin.ID, admin.Email, admin.Role)
	}
}

func TestMe_NoSession_401(t *testing.T) {
	r, _, _ := newMeRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogin_NonexistentEmail_IdenticalToWrongPassword(t *testing.T) {
	rWrongPassword, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")
	wrongPasswordResp := postLogin(t, rWrongPassword, email, "wrong-password")

	rNonexistent, _, _ := newLoginRouter(t)
	nonexistentResp := postLogin(t, rNonexistent, "does-not-exist-"+email, "whatever")

	if nonexistentResp.Code != http.StatusUnauthorized {
		t.Errorf("nonexistent email status = %d, want %d", nonexistentResp.Code, http.StatusUnauthorized)
	}
	// SP-22: nonexistent email must be indistinguishable from a wrong
	// password for an existing email - same status and byte-identical body.
	if nonexistentResp.Code != wrongPasswordResp.Code {
		t.Errorf("nonexistent email status = %d, wrong-password status = %d, want identical", nonexistentResp.Code, wrongPasswordResp.Code)
	}
	if nonexistentResp.Body.String() != wrongPasswordResp.Body.String() {
		t.Errorf("nonexistent email body = %q, wrong-password body = %q, want identical", nonexistentResp.Body.String(), wrongPasswordResp.Body.String())
	}
}
