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

func newLoginRouter(t *testing.T) (http.Handler, *db.UserRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)

	// Every test using this router creates an admin via createTestAdmin,
	// and creating identity rows here races other packages' bulk clears
	// of the shared `users` table - see
	// LockUsersTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockUsersTable(t, context.Background(), dsn)

	repo := db.NewUserRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	// secureCookies=true: this file's cookie assertions expect the default,
	// Secure-only behavior. The off case is covered separately by
	// TestLogin_SecureCookiesDisabled_CookieNotSecure.
	handler := NewAuthHandler(repo, memberships, pool, zap.NewNop(), testSessionSecret, true)

	r := chi.NewRouter()
	r.Post("/api/auth/login", handler.Login)

	return r, repo, pool
}

func uniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("auth-handler-test-%d@example.com", time.Now().UnixNano())
}

// createTestAdmin creates an admin with exactly one tenant_membership
// (role owner) - Login now refuses an admin with zero memberships
// (TENANT-19 session half), so every test exercising a *successful* login
// needs one. Tests specifically about the zero/multi-membership cases seed
// those directly instead of using this helper.
func createTestAdmin(t *testing.T, repo *db.UserRepository, pool *db.Pool, email, plainPassword string) (tenantID string) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	hash, err := auth.HashPassword(plainPassword)
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}

	// email_verified_at must be set: Login now refuses any user whose email
	// isn't verified (T10, SaaS signup gate) - this helper is used by tests
	// exercising a *successful* login, not the verification gate itself.
	verifiedAt := time.Now()
	admin := &db.User{Email: email, PasswordHash: hash, EmailVerifiedAt: &verifiedAt}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	return seedSoleTenantMembership(t, pool, admin.ID, email)
}

// seedSoleTenantMembership creates a tenant named after namePrefix and an
// owner membership linking userID to it, registering cleanup for both.
func seedSoleTenantMembership(t *testing.T, pool *db.Pool, userID, namePrefix string) (tenantID string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "auth-test-tenant-"+namePrefix).Scan(&tenantID); err != nil {
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(db.WithTenantTx(ctx, tx),
		"INSERT INTO tenant_memberships (user_id, tenant_id, role) VALUES ($1, $2, $3)", userID, tenantID, db.RoleOwner,
	); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seeding tenant membership returned unexpected error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	return tenantID
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

	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Token == "" {
		t.Error("response body has no token, want a non-empty session token")
	}
	// TENANT-19 (session half): exactly one membership sets that tenant
	// active in the response.
	if body.TenantID == "" {
		t.Error("response body has no tenant_id, want the sole tenant_membership's tenant active")
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
	dbtest.LockUsersTable(t, context.Background(), dsn)

	repo := db.NewUserRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	handler := NewAuthHandler(repo, memberships, pool, zap.NewNop(), testSessionSecret, false)
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

func newMeRouter(t *testing.T) (http.Handler, *db.UserRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)

	// Every test using this router creates an admin via createTestAdmin,
	// and creating identity rows here races other packages' bulk clears
	// of the shared `users` table - see
	// LockUsersTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockUsersTable(t, context.Background(), dsn)

	repo := db.NewUserRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	handler := NewAuthHandler(repo, memberships, pool, zap.NewNop(), testSessionSecret, true)

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(testSessionSecret, repo), TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Get("/api/auth/me", handler.Me)
		protected.Patch("/api/auth/me", handler.UpdateProfile)
		protected.Post("/api/auth/switch-tenant", handler.SwitchTenant)
		protected.Post("/api/auth/change-password", handler.ChangePassword)
	})

	return r, repo, pool
}

func postSwitchTenant(t *testing.T, r http.Handler, token, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(switchTenantRequest{TenantID: tenantID})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/switch-tenant", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestMe_ValidSession_200WithIdentity(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	tenantID := createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token, err := auth.IssueSessionWithTenant(admin.ID, tenantID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSessionWithTenant() returned unexpected error: %v", err)
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
	// The role comes from the caller's tenant_membership (owner, seeded by
	// createTestAdmin), resolved by TenantContext - not from the user row,
	// which no longer carries one.
	if body.ID != admin.ID || body.Email != admin.Email || body.Role != db.RoleOwner {
		t.Errorf("body = %+v, want {ID:%q Email:%q Role:%q}", body, admin.ID, admin.Email, db.RoleOwner)
	}
	if body.ActiveTenantID != tenantID {
		t.Errorf("body.ActiveTenantID = %q, want %q", body.ActiveTenantID, tenantID)
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

func newLogoutRouter(t *testing.T) (http.Handler, *db.UserRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)

	// Every test using this router creates an admin via createTestAdmin,
	// and creating identity rows here races other packages' bulk clears
	// of the shared `users` table - see
	// LockUsersTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockUsersTable(t, context.Background(), dsn)

	repo := db.NewUserRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	handler := NewAuthHandler(repo, memberships, pool, zap.NewNop(), testSessionSecret, true)

	r := chi.NewRouter()
	protected := chi.NewRouter()
	protected.Use(RequireAuth(testSessionSecret, repo))
	protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
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

// TestLogin_ZeroMemberships_403NoSessionIssued proves the spec.md edge
// case: a user removed from every tenant (or one that somehow never got a
// membership) must never see an empty dashboard - login itself refuses,
// with no session cookie/token issued at all.
func TestLogin_ZeroMemberships_403NoSessionIssued(t *testing.T) {
	r, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)

	ctx := context.Background()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}
	// email_verified_at must be set so this test actually exercises the
	// zero-membership gate rather than tripping the (separate) T10
	// verification gate first.
	verifiedAt := time.Now()
	admin := &db.User{Email: email, PasswordHash: hash, EmailVerifiedAt: &verifiedAt}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	// Deliberately no tenant_membership seeded for this admin.

	rec := postLogin(t, r, email, "correct-horse-battery-staple")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%q)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if rec.Body.String() != noTenantAccessBody {
		t.Errorf("body = %q, want %q", rec.Body.String(), noTenantAccessBody)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set on a refused login = %v, want none", rec.Result().Cookies())
	}
}

// TestLogin_MultipleMemberships_SucceedsWithNoActiveTenant proves TENANT-19
// (session half): a user with more than one tenant_membership still logs
// in successfully, but with no active tenant set - pending the (P2,
// out-of-batch) tenant-selection screen.
func TestLogin_MultipleMemberships_SucceedsWithNoActiveTenant(t *testing.T) {
	r, repo, pool := newLoginRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	// createTestAdmin already seeded one membership - add a second tenant
	// so this admin has exactly two.
	seedSoleTenantMembership(t, pool, admin.ID, email+"-second")

	rec := postLogin(t, r, email, "correct-horse-battery-staple")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Token == "" {
		t.Error("response body has no token, want login to still succeed")
	}
	if body.TenantID != "" {
		t.Errorf("response tenant_id = %q, want empty (no active tenant with >1 membership)", body.TenantID)
	}
}

// TestMe_MultipleMemberships_ListsAllWithNoActiveTenant proves T7's
// /api/auth/me contract for the multi-membership case: every membership is
// listed, and active_tenant_id is absent even though the session is
// otherwise valid.
func TestMe_MultipleMemberships_ListsAllWithNoActiveTenant(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	secondTenantID := seedSoleTenantMembership(t, pool, admin.ID, email+"-second")

	// IssueSession (no tenant claim) mirrors what Login would have issued
	// for this admin (>1 membership, active tenant left unset).
	token, err := auth.IssueSession(admin.ID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.ActiveTenantID != "" {
		t.Errorf("ActiveTenantID = %q, want empty (no tenant selected)", body.ActiveTenantID)
	}
	if len(body.Memberships) != 2 {
		t.Fatalf("len(Memberships) = %d, want 2", len(body.Memberships))
	}
	seen := map[string]bool{}
	for _, m := range body.Memberships {
		seen[m.TenantID] = true
		if m.Role != db.RoleOwner {
			t.Errorf("membership role = %q, want %q", m.Role, db.RoleOwner)
		}
	}
	if !seen[secondTenantID] {
		t.Errorf("Memberships = %+v, want the second seeded tenant %q among them", body.Memberships, secondTenantID)
	}
}

// TestSwitchTenant_ValidMembership_UpdatesCookieNoReloginRequired proves
// TENANT-20: switching to a tenant the caller has a membership in updates
// the session cookie to make that tenant active, without a new login.
func TestSwitchTenant_ValidMembership_UpdatesCookieNoReloginRequired(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	secondTenantID := seedSoleTenantMembership(t, pool, admin.ID, email+"-second")

	token, err := auth.IssueSession(admin.ID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	rec := postSwitchTenant(t, r, token, secondTenantID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.TenantID != secondTenantID {
		t.Errorf("response tenant_id = %q, want %q", body.TenantID, secondTenantID)
	}
	if body.Token == "" {
		t.Error("response has no token, want a new session token")
	}

	var newCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("no vane_session cookie in switch-tenant response, want one set")
	}

	// The new cookie must authenticate a follow-up request scoped to the
	// newly active tenant - no re-login needed.
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(newCookie)
	meRec := httptest.NewRecorder()
	r.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("follow-up /api/auth/me with the new cookie status = %d, want %d", meRec.Code, http.StatusOK)
	}
	var meBody meResponse
	if err := json.Unmarshal(meRec.Body.Bytes(), &meBody); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if meBody.ActiveTenantID != secondTenantID {
		t.Errorf("follow-up /api/auth/me ActiveTenantID = %q, want %q", meBody.ActiveTenantID, secondTenantID)
	}
}

// TestSwitchTenant_NoMembership_403SessionUnchanged proves TENANT-21: a
// tenant_id the caller has no membership for is refused with 403, and the
// current session's cookie is left alone (no cookie set at all in the
// response).
func TestSwitchTenant_NoMembership_403SessionUnchanged(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")

	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}

	// A tenant admin has no membership in at all - another admin's sole
	// tenant, seeded independently.
	otherEmail := uniqueTestEmail(t)
	createTestAdmin(t, repo, pool, otherEmail, "another-horse-battery-staple")
	otherAdmin, err := repo.GetByEmail(context.Background(), otherEmail)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	memberships := db.NewTenantMembershipRepository(pool)
	othersOnly, err := memberships.ListForUser(context.Background(), otherAdmin.ID)
	if err != nil || len(othersOnly) != 1 {
		t.Fatalf("expected exactly one membership for the other admin, got %v (err=%v)", othersOnly, err)
	}
	foreignTenantID := othersOnly[0].TenantID

	token, err := auth.IssueSession(admin.ID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	rec := postSwitchTenant(t, r, token, foreignTenantID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%q)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if rec.Body.String() != noMembershipForTenantBody {
		t.Errorf("body = %q, want %q", rec.Body.String(), noMembershipForTenantBody)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("cookies set on a refused switch-tenant = %v, want none (session unchanged)", rec.Result().Cookies())
	}
}

// TestSwitchTenant_NoSession_401 proves the endpoint requires
// authentication like every other protected route.
func TestSwitchTenant_NoSession_401(t *testing.T) {
	r, _, _ := newMeRouter(t)

	rec := postSwitchTenant(t, r, "", "00000000-0000-0000-0000-000000000000")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func patchProfile(t *testing.T, r http.Handler, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/auth/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func issueTestTokenFor(t *testing.T, admin *db.User, tenantID string) string {
	t.Helper()
	token, err := auth.IssueSessionWithTenant(admin.ID, tenantID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSessionWithTenant() returned unexpected error: %v", err)
	}
	return token
}

// TestUpdateProfile_ValidName_200UpdatesName covers PROFSS-01: a non-empty
// name updates the user's Name and is reflected in the response.
func TestUpdateProfile_ValidName_200UpdatesName(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	tenantID := createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token := issueTestTokenFor(t, admin, tenantID)

	body, err := json.Marshal(updateProfileRequest{Name: "New Display Name"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	rec := patchProfile(t, r, token, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Name != "New Display Name" {
		t.Errorf("Name = %q, want %q", resp.Name, "New Display Name")
	}

	updated, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if updated.Name != "New Display Name" {
		t.Errorf("persisted Name = %q, want %q", updated.Name, "New Display Name")
	}
}

// TestUpdateProfile_EmptyName_422NoChange covers PROFSS-02.
func TestUpdateProfile_EmptyName_422NoChange(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	tenantID := createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token := issueTestTokenFor(t, admin, tenantID)
	originalName := admin.Name

	body, err := json.Marshal(updateProfileRequest{Name: ""})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	rec := patchProfile(t, r, token, body)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	unchanged, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if unchanged.Name != originalName {
		t.Errorf("Name = %q, want unchanged %q", unchanged.Name, originalName)
	}
}

// TestUpdateProfile_ExtraFieldsIgnored covers PROFSS-03: a body containing
// fields other than name (email, role) has those fields ignored, not
// applied.
func TestUpdateProfile_ExtraFieldsIgnored(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	tenantID := createTestAdmin(t, repo, pool, email, "correct-horse-battery-staple")
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token := issueTestTokenFor(t, admin, tenantID)

	body := []byte(`{"name":"Scoped Update","email":"attacker@example.com","role":"owner"}`)
	rec := patchProfile(t, r, token, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if updated.Name != "Scoped Update" {
		t.Errorf("Name = %q, want %q", updated.Name, "Scoped Update")
	}
	if updated.Email != admin.Email {
		t.Errorf("Email = %q, want unchanged %q (email field must be ignored)", updated.Email, admin.Email)
	}
}

// TestUpdateProfile_NoSession_401 proves the endpoint requires
// authentication like every other protected route.
func TestUpdateProfile_NoSession_401(t *testing.T) {
	r, _, _ := newMeRouter(t)

	body, err := json.Marshal(updateProfileRequest{Name: "New Name"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	rec := patchProfile(t, r, "", body)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func postChangePassword(t *testing.T, r http.Handler, token, currentPassword, newPassword string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(changePasswordRequest{CurrentPassword: currentPassword, NewPassword: newPassword})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestChangePassword_CorrectCurrentAndValidNew_200RevokesOtherSessions
// covers PROFSS-04: a correct current password and a policy-valid new
// password updates PasswordHash, responds 200, and revokes every other
// active session for that user - proven here by a second, previously
// issued session (session B) being rejected on its next authenticated
// request afterward.
func TestChangePassword_CorrectCurrentAndValidNew_200RevokesOtherSessions(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	const currentPassword = "correct-horse-battery-staple"
	tenantID := createTestAdmin(t, repo, pool, email, currentPassword)
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}

	tokenA := issueTestTokenFor(t, admin, tenantID)
	tokenB := issueTestTokenFor(t, admin, tenantID)

	const newPassword = "brand-new-correct-horse-password"
	rec := postChangePassword(t, r, tokenA, currentPassword, newPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(updated.PasswordHash, newPassword) {
		t.Error("stored PasswordHash does not verify against the new password")
	}

	// Session B, issued before the change, must now be rejected.
	meRec := patchProfile(t, r, tokenB, []byte(`{"name":"should not apply"}`))
	if meRec.Code != http.StatusUnauthorized {
		t.Errorf("session B status = %d, want %d (revoked by the password change)", meRec.Code, http.StatusUnauthorized)
	}
}

// TestChangePassword_WrongCurrentPassword_401NoChange covers PROFSS-05.
func TestChangePassword_WrongCurrentPassword_401NoChange(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	const currentPassword = "correct-horse-battery-staple"
	tenantID := createTestAdmin(t, repo, pool, email, currentPassword)
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token := issueTestTokenFor(t, admin, tenantID)
	originalHash := admin.PasswordHash

	rec := postChangePassword(t, r, token, "totally-wrong-password", "brand-new-correct-horse-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}

	unchanged, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if unchanged.PasswordHash != originalHash {
		t.Error("PasswordHash changed despite a wrong current password")
	}

	// The session used for this failed attempt must still work (nothing
	// revoked).
	meRec := patchProfile(t, r, token, []byte(`{"name":"still valid"}`))
	if meRec.Code != http.StatusOK {
		t.Errorf("session status after failed change = %d, want %d (nothing should be revoked)", meRec.Code, http.StatusOK)
	}
}

// TestChangePassword_WeakNewPassword_422NoChange covers PROFSS-06.
func TestChangePassword_WeakNewPassword_422NoChange(t *testing.T) {
	r, repo, pool := newMeRouter(t)
	email := uniqueTestEmail(t)
	const currentPassword = "correct-horse-battery-staple"
	tenantID := createTestAdmin(t, repo, pool, email, currentPassword)
	admin, err := repo.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	token := issueTestTokenFor(t, admin, tenantID)
	originalHash := admin.PasswordHash

	rec := postChangePassword(t, r, token, currentPassword, "short")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	unchanged, err := repo.GetByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if unchanged.PasswordHash != originalHash {
		t.Error("PasswordHash changed despite a weak new password")
	}
}

// TestChangePassword_NoSession_401 proves the endpoint requires
// authentication.
func TestChangePassword_NoSession_401(t *testing.T) {
	r, _, _ := newMeRouter(t)

	rec := postChangePassword(t, r, "", "anything", "brand-new-correct-horse-password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
