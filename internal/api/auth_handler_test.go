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
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
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

	// Every test using this router creates an admin via createTestAdmin,
	// and AdminRepository.Create always inserts with the `admins.role`
	// column's database default (owner, migration 0009) - see
	// LockAdminsTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockAdminsTable(t, context.Background(), dsn)

	repo := db.NewAdminRepository(pool)
	// secureCookies=true: this file's cookie assertions expect the default,
	// Secure-only behavior. The off case is covered separately by
	// TestLogin_SecureCookiesDisabled_CookieNotSecure.
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret, true)

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

func TestLogin_CorrectCredentials_SetsSessionCookie(t *testing.T) {
	r, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	rec := postLogin(t, r, email, "correct-horse-battery-staple")

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "vane_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("no vane_session cookie in response, want one set (cookies: %+v)", cookies)
	}
	if sessionCookie.Value == "" {
		t.Error("vane_session cookie has empty value, want the session token")
	}
	if !sessionCookie.HttpOnly {
		t.Error("vane_session cookie HttpOnly = false, want true")
	}
	if !sessionCookie.Secure {
		t.Error("vane_session cookie Secure = false, want true")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("vane_session cookie SameSite = %v, want %v", sessionCookie.SameSite, http.SameSiteStrictMode)
	}
	if sessionCookie.Path != "/" {
		t.Errorf("vane_session cookie Path = %q, want %q", sessionCookie.Path, "/")
	}
	if sessionCookie.MaxAge != int(auth.SessionTTL.Seconds()) {
		t.Errorf("vane_session cookie MaxAge = %d, want %d", sessionCookie.MaxAge, int(auth.SessionTTL.Seconds()))
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Token == "" {
		t.Error("response body has no token, want the body contract unchanged")
	}
}

// TestLogin_SecureCookiesDisabled_CookieNotSecure asserts H9's opt-out: with
// secureCookies=false (VANE_SECURE_COOKIES=false), the vane_session cookie's
// Secure attribute is false, so a browser will send it back over plain HTTP
// - required for a self-hosted instance reached by internal IP/hostname
// without a TLS-terminating reverse proxy. Every other attribute is
// unchanged.
func TestLogin_SecureCookiesDisabled_CookieNotSecure(t *testing.T) {
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
	dbtest.LockAdminsTable(t, context.Background(), dsn)

	repo := db.NewAdminRepository(pool)
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret, false)
	r := chi.NewRouter()
	r.Post("/api/auth/login", handler.Login)

	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	rec := postLogin(t, r, email, "correct-horse-battery-staple")

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "vane_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no vane_session cookie in response, want one set")
	}
	if sessionCookie.Secure {
		t.Error("vane_session cookie Secure = true, want false with secureCookies=false")
	}
	if !sessionCookie.HttpOnly {
		t.Error("vane_session cookie HttpOnly = false, want true (unaffected by secureCookies)")
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

	// Every test using this router creates an admin via createTestAdmin,
	// and AdminRepository.Create always inserts with the `admins.role`
	// column's database default (owner, migration 0009) - see
	// LockAdminsTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockAdminsTable(t, context.Background(), dsn)

	repo := db.NewAdminRepository(pool)
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret, true)

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

func newLogoutRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool) {
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

	// Every test using this router creates an admin via createTestAdmin,
	// and AdminRepository.Create always inserts with the `admins.role`
	// column's database default (owner, migration 0009) - see
	// LockAdminsTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockAdminsTable(t, context.Background(), dsn)

	repo := db.NewAdminRepository(pool)
	handler := NewAuthHandler(repo, zap.NewNop(), testSessionSecret, true)

	r := chi.NewRouter()
	protected := chi.NewRouter()
	protected.Use(RequireAuth(testSessionSecret, repo))
	protected.Get("/api/auth/me", handler.Me)
	protected.Post("/api/auth/logout", handler.Logout)
	r.Mount("/", protected)

	return r, repo, pool
}

func TestLogout_ExpiresCookie_SubsequentRequestRejected(t *testing.T) {
	r, repo, pool := newLogoutRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token, err := auth.IssueSession(admin.ID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	logoutRec := httptest.NewRecorder()
	r.ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", logoutRec.Code, http.StatusOK)
	}

	var expiredCookie *http.Cookie
	for _, c := range logoutRec.Result().Cookies() {
		if c.Name == "vane_session" {
			expiredCookie = c
			break
		}
	}
	if expiredCookie == nil {
		t.Fatal("logout response has no vane_session cookie, want an expiring one")
	}
	if expiredCookie.MaxAge >= 0 {
		t.Errorf("expired cookie MaxAge = %d, want negative (immediate expiry)", expiredCookie.MaxAge)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: expiredCookie.Value})
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusUnauthorized {
		t.Errorf("subsequent request status = %d, want %d (expired cookie must not authenticate - simulated via empty cookie value browsers send after expiry)", meRec.Code, http.StatusUnauthorized)
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
