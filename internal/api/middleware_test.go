//go:build integration

package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

const middlewareTestSecret = "middleware-test-secret-32-bytes-long!!"

// newMiddlewareTestAdmins builds a real *db.UserRepository against the
// test database - RequireAuth now loads the admin row itself (Role,
// SessionsRevokedAt), so its tests need a real backing repository, not just
// a signed JWT.
func newMiddlewareTestAdmins(t *testing.T) (*db.UserRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)

	// Every test in this file creates an admin via this constructor's
	// repository, and UserRepository.Create always inserts with the
	// shared `users` table, which other packages bulk-clear -
	// see LockUsersTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockUsersTable(t, context.Background(), dsn)

	return db.NewUserRepository(pool), pool
}

// createMiddlewareTestAdmin inserts a real admin row so RequireAuth's
// admins.GetByID lookup succeeds, and issues a session token for it.
func createMiddlewareTestAdmin(t *testing.T, repo *db.UserRepository, pool *db.Pool) *db.User {
	t.Helper()
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	admin := &db.User{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("repo.Create() returned unexpected error: %v", err)
	}
	return admin
}

func newProtectedHandler(admins *db.UserRepository, pool *db.Pool, gotAdmin **db.User) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAdmin, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop())(next)
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

	token, err := auth.IssueSession(admin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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

	token, err := auth.IssueSession(admin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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

	headerToken, err := auth.IssueSession(headerAdmin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}
	cookieToken, err := auth.IssueSession(cookieAdmin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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
	repo, pool := newMiddlewareTestAdmins(t)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_InvalidToken_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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

	token, err := auth.IssueSession(admin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	// Revoke sessions strictly after the token above was issued, so the
	// token's iat is guaranteed to predate sessions_revoked_at.
	time.Sleep(10 * time.Millisecond)
	if err := repo.RevokeSessions(context.Background(), admin.ID); err != nil {
		t.Fatalf("RevokeSessions() returned unexpected error: %v", err)
	}

	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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
	token, err := auth.IssueSession(admin.ID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

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

// TestRequireAuth_TokenWithoutSessionID_401 proves the user-sessions claim
// contract (SESS-01/03): a signed, non-expired token that carries no `sid`
// claim is rejected - such a token couldn't be individually revoked (no row
// to point at), so it must not authenticate. It is minted with the same
// signing primitive as expiredToken, but with a future expiry and no sid,
// so this exercises the missing-sid branch specifically.
func TestRequireAuth_TokenWithoutSessionID_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

	claims := jwt.RegisteredClaims{
		Subject:   admin.ID,
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(middlewareTestSecret))
	if err != nil {
		t.Fatalf("SignedString() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (token without sid claim must be rejected)", rec.Code, http.StatusUnauthorized)
	}
	if gotAdmin != nil {
		t.Error("Admin stored in context despite a sid-less token, want the request rejected")
	}
}

// TestRequireAuth_SessionIDWithoutRow_401 proves the user-sessions SESS-03
// contract: a validly-signed, non-expired token whose `sid` matches no
// sessions-table row is rejected - the row is the revocable unit, so a
// token without one must not authenticate.
func TestRequireAuth_SessionIDWithoutRow_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

	// A UUID guaranteed to have no backing row - generated by the DB, never
	// inserted.
	var sid string
	if err := pool.QueryRow(context.Background(), "SELECT gen_random_uuid()").Scan(&sid); err != nil {
		t.Fatalf("generating a fresh sid returned unexpected error: %v", err)
	}
	token, err := auth.IssueSession(admin.ID, sid, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (sid with no backing row must be rejected)", rec.Code, http.StatusUnauthorized)
	}
}

// TestRequireAuth_RevokedSessionRow_401 proves the user-sessions SESS-03
// contract: a validly-signed, non-expired token whose `sid` row has
// revoked_at set is rejected on its very next request - per-device
// revocation (Logout, the "Encerrar" button) is enforced at the row level.
func TestRequireAuth_RevokedSessionRow_401(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

	var sid string
	if err := pool.QueryRow(context.Background(), "SELECT gen_random_uuid()").Scan(&sid); err != nil {
		t.Fatalf("generating a fresh sid returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (id, user_id, revoked_at) VALUES ($1, $2, now())`, sid, admin.ID,
	); err != nil {
		t.Fatalf("inserting revoked session row returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, sid) })

	token, err := auth.IssueSession(admin.ID, sid, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (revoked row must be rejected)", rec.Code, http.StatusUnauthorized)
	}
}

// TestRequireAuth_LastSeenThrottle_SecondRequestWithinWindowLeavesRowAlone
// proves the user-sessions last_seen_at throttle: on a fresh row (NULL
// last_seen_at) the first authenticated request sets it, and a second
// request within the 5-minute window leaves it unchanged - the write is
// throttled, not per-request.
func TestRequireAuth_LastSeenThrottle_SecondRequestWithinWindowLeavesRowAlone(t *testing.T) {
	repo, pool := newMiddlewareTestAdmins(t)
	admin := createMiddlewareTestAdmin(t, repo, pool)
	var gotAdmin *db.User
	handler := newProtectedHandler(repo, pool, &gotAdmin)

	// Fresh row: last_seen_at starts NULL so the throttle window is clean.
	var sid string
	if err := pool.QueryRow(context.Background(), "SELECT gen_random_uuid()").Scan(&sid); err != nil {
		t.Fatalf("generating a fresh sid returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO sessions (id, user_id) VALUES ($1, $2)`, sid, admin.ID,
	); err != nil {
		t.Fatalf("inserting fresh session row returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, sid) })

	token, err := auth.IssueSession(admin.ID, sid, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	serve := func() {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	}

	serve()

	var afterFirst sql.NullTime
	if err := pool.QueryRow(context.Background(),
		"SELECT last_seen_at FROM sessions WHERE id = $1", sid).Scan(&afterFirst); err != nil {
		t.Fatalf("reading last_seen_at after first request returned unexpected error: %v", err)
	}
	if !afterFirst.Valid {
		t.Fatal("last_seen_at still NULL after the first authenticated request, want it set")
	}

	serve()

	var afterSecond sql.NullTime
	if err := pool.QueryRow(context.Background(),
		"SELECT last_seen_at FROM sessions WHERE id = $1", sid).Scan(&afterSecond); err != nil {
		t.Fatalf("reading last_seen_at after second request returned unexpected error: %v", err)
	}
	if !afterSecond.Valid {
		t.Fatal("last_seen_at NULL after the second authenticated request, want it still set")
	}
	if !afterSecond.Time.Equal(afterFirst.Time) {
		t.Errorf("last_seen_at changed on the second request within the throttle window: %v -> %v, want unchanged", afterFirst.Time, afterSecond.Time)
	}
}
