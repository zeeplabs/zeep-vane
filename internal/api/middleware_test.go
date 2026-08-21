//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const middlewareTestSecret = "middleware-test-secret-32-bytes-long!!"

// newMiddlewareTestAdmins builds a real *db.AdminRepository against the
// test database - RequireAuth now loads the admin row itself (Role,
// SessionsRevokedAt), so its tests need a real backing repository, not just
// a signed JWT.
func newMiddlewareTestAdmins(t *testing.T) (*db.AdminRepository, *db.Pool) {
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

	return db.NewAdminRepository(pool), pool
}

// createMiddlewareTestAdmin inserts a real admin row so RequireAuth's
// admins.GetByID lookup succeeds, and issues a session token for it.
func createMiddlewareTestAdmin(t *testing.T, repo *db.AdminRepository, pool *db.Pool) *db.Admin {
	t.Helper()
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &db.Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("repo.Create() returned unexpected error: %v", err)
	}
	return admin
}

func newProtectedHandler(admins *db.AdminRepository, gotAdmin **db.Admin) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAdmin, _ = AdminFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return RequireAuth(middlewareTestSecret, admins)(next)
}

// expiredToken signs a token whose expiry is already in the past, using the
// same claim shape and signing method as auth.IssueSession, so the test
// exercises expiry rejection specifically (not some other malformation).
func expiredToken(t *testing.T, adminID, secret string) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   adminID,
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString() returned unexpected error: %v", err)
	}
	return signed
}

func TestRequireAuth_ValidToken_PassesThrough(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotAdmin == nil {
		t.Fatal("Admin not stored in context, want the loaded admin")
	}
	if gotAdmin.ID != admin.ID {
		t.Errorf("admin ID stored in context = %q, want %q", gotAdmin.ID, admin.ID)
	}
}

func TestRequireAuth_CookieOnly_PassesThrough(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotAdmin == nil || gotAdmin.ID != admin.ID {
		t.Errorf("Admin in context = %v, want admin %q", gotAdmin, admin.ID)
	}
}

func TestRequireAuth_HeaderTakesPriorityOverCookie(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	headerAdmin := createMiddlewareTestAdmin(t, repo, pool)
	cookieAdmin := createMiddlewareTestAdmin(t, repo, pool)

	headerToken, err := auth.IssueSession(headerAdmin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}
	cookieToken, err := auth.IssueSession(cookieAdmin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+headerToken)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieToken})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotAdmin == nil || gotAdmin.ID != headerAdmin.ID {
		t.Errorf("Admin in context = %v, want header's admin %q (header must win over cookie)", gotAdmin, headerAdmin.ID)
	}
}

func TestRequireAuth_MissingToken_401(t *testing.T) {
	repo, _ := newMiddlewareTestAdmins(t)
	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_InvalidToken_401(t *testing.T) {
	repo, _ := newMiddlewareTestAdmins(t)
	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_ExpiredToken_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)
	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	token := expiredToken(t, admin.ID, middlewareTestSecret)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_TokenIssuedBeforeRevocation_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	// Revoke sessions strictly after the token above was issued, so the
	// token's iat is guaranteed to predate sessions_revoked_at.
	time.Sleep(10 * time.Millisecond)
	if err := repo.RevokeSessions(context.Background(), admin.ID); err != nil {
		t.Fatalf("RevokeSessions() returned unexpected error: %v", err)
	}

	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (token issued before sessions_revoked_at)", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_TokenIssuedAfterRevocation_PassesThrough(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)

	if err := repo.RevokeSessions(context.Background(), admin.ID); err != nil {
		t.Fatalf("RevokeSessions() returned unexpected error: %v", err)
	}

	// Issue the token strictly after the revocation above, so its iat is
	// guaranteed to be at or after sessions_revoked_at. JWT's IssuedAt
	// claim truncates to whole seconds (jwt/v5 default TimePrecision), so
	// the gap must cross a full second boundary, not just a few ms.
	time.Sleep(1100 * time.Millisecond)
	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.Admin
	handler := newProtectedHandler(repo, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (token issued after revocation is still valid)", rec.Code, http.StatusOK)
	}
	if gotAdmin == nil || gotAdmin.ID != admin.ID {
		t.Errorf("Admin in context = %v, want admin %q", gotAdmin, admin.ID)
	}
}
