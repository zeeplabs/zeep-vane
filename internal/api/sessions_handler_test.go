//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// newSessionsRouterForTest wires SessionsHandler behind the same
// RequireAuth/TenantContext chain buildAdminRouter uses, but only for
// the two new endpoints this test cares about (GET/DELETE
// /api/auth/sessions*). Returns (router, pool, users) so each test can
// issue tokens via issueTestSessionTokenWithID and assert on the DB
// state directly.
//
// Delegates the migration + fixture-row setup to newAPITenantScopedPool,
// since issueTestSessionTokenWithID (via seedSessionForRole) depends
// on the package-level currentAPITestTenant that helper populates -
// building a brand-new pool here without going through
// newAPITenantScopedPool would leave that var empty and every token
// issuance would fail with "no fixture tenant to bind the session to".
func newSessionsRouterForTest(t *testing.T, sessionsH *SessionsHandler) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()

	pool, _ := newAPITenantScopedPool(t)

	users := db.NewUserRepository(pool)
	sessions := db.NewSessionRepository(pool)
	requireAuth := RequireAuth(middlewareTestSecret, users, sessions, zap.NewNop())
	tenantCtx := TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(requireAuth)
		protected.Use(tenantCtx)
		protected.Get("/api/auth/sessions", sessionsH.List)
		protected.Delete("/api/auth/sessions/{id}", sessionsH.Revoke)
	})

	return r, pool, users
}

// createSessionRow inserts a session row for userID with the given
// metadata, returning the generated id. Used to set up "other sessions"
// alongside the test's current session (which uses the shared
// IssueTestSessionID fixture row inserted by newAPITenantScopedPool's
// setup).
func createSessionRow(t *testing.T, pool *db.Pool, userID, userAgent, ip string) string {
	t.Helper()
	ctx := context.Background()
	var sid string
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, user_agent, ip) VALUES ($1, NULLIF($2, ''), NULLIF($3, '')) RETURNING id`,
		userID, userAgent, ip,
	).Scan(&sid); err != nil {
		t.Fatalf("createSessionRow returned unexpected error: %v", err)
	}
	return sid
}

// createSessionRowBackdated inserts a session row with an explicit
// older created_at, for the "expired session excluded from list" test.
func createSessionRowBackdated(t *testing.T, pool *db.Pool, userID, userAgent, ip string, createdAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	var sid string
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, user_agent, ip, created_at) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4) RETURNING id`,
		userID, userAgent, ip, createdAt,
	).Scan(&sid); err != nil {
		t.Fatalf("createSessionRowBackdated returned unexpected error: %v", err)
	}
	return sid
}

func decodeSessionViews(t *testing.T, body []byte) []SessionView {
	t.Helper()
	var out []SessionView
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("json.Unmarshal returned unexpected error: %v, body = %s", err, string(body))
	}
	return out
}

func doSessionsRequest(t *testing.T, r http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestSessionsHandler_List_ReturnsCurrentAndOthers proves SESS-05/06: the
// list returns every non-revoked session for the caller, with exactly
// one row marked current (the one matching the request's sid claim).
func TestSessionsHandler_List_ReturnsCurrentAndOthers(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	// Single source of truth for the user - issueTestSessionTokenWithID
	// creates the user + membership + token atomically, returning both.
	// Sessions created for userID are visible to the token whose sub is
	// userID; using two different helpers (createMiddlewareTestAdmin +
	// issueTestSessionToken) would create two different users and the
	// test would never see its own session rows in the list.
	currentToken, userID := issueTestSessionTokenWithID(t, users)
	bindFixtureSessionToUser(t, pool, userID)

	// Create 2 additional sessions for the same user; their sids are
	// different from IssueTestSessionID so neither is current.
	other1 := createSessionRow(t, pool, userID, "ua-1", "10.0.0.1")
	other2 := createSessionRow(t, pool, userID, "ua-2", "10.0.0.2")

	rec := doSessionsRequest(t, r, http.MethodGet, "/api/auth/sessions", currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	views := decodeSessionViews(t, rec.Body.Bytes())
	if len(views) != 3 {
		t.Fatalf("len(views) = %d, want 3 (got: %+v)", len(views), views)
	}

	// Exactly one row is current, and it's the row matching the
	// IssueTestSessionID sentinel (the currentToken's sid).
	var currentCount int
	var currentID string
	for _, v := range views {
		if v.Current {
			currentCount++
			currentID = v.ID
		}
	}
	if currentCount != 1 {
		t.Errorf("current rows = %d, want 1 (views: %+v)", currentCount, views)
	}
	if currentID != auth.IssueTestSessionID {
		t.Errorf("current id = %q, want %q", currentID, auth.IssueTestSessionID)
	}

	// The two other sessions appear in the list.
	gotOther1 := false
	gotOther2 := false
	for _, v := range views {
		if v.ID == other1 {
			gotOther1 = true
			if v.Current {
				t.Errorf("other1 marked current, want false")
			}
			if v.UserAgent == nil || *v.UserAgent != "ua-1" {
				t.Errorf("other1 user_agent = %v, want %q", v.UserAgent, "ua-1")
			}
			if v.IP == nil || *v.IP != "10.0.0.1" {
				t.Errorf("other1 ip = %v, want %q", v.IP, "10.0.0.1")
			}
		}
		if v.ID == other2 {
			gotOther2 = true
			if v.Current {
				t.Errorf("other2 marked current, want false")
			}
		}
	}
	if !gotOther1 || !gotOther2 {
		t.Errorf("missing rows: other1=%v other2=%v (views: %+v)", gotOther1, gotOther2, views)
	}
}

// TestSessionsHandler_List_OnlyCurrentSession proves the single-session
// edge case (F4 in validation.md): a user whose only session is their
// current one gets a one-item list, marked current, with no error.
func TestSessionsHandler_List_OnlyCurrentSession(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, userID := issueTestSessionTokenWithID(t, users)
	bindFixtureSessionToUser(t, pool, userID)

	rec := doSessionsRequest(t, r, http.MethodGet, "/api/auth/sessions", currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	views := decodeSessionViews(t, rec.Body.Bytes())
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1 (only the current session) (views: %+v)", len(views), views)
	}
	if !views[0].Current {
		t.Errorf("single session Current = false, want true (id = %q)", views[0].ID)
	}
	if views[0].ID != auth.IssueTestSessionID {
		t.Errorf("single session id = %q, want %q", views[0].ID, auth.IssueTestSessionID)
	}
}

// TestSessionsHandler_List_ExcludesRevoked proves the revoked_at filter
// in ListForUser: a session revoked between list calls disappears from
// the next response.
func TestSessionsHandler_List_ExcludesRevoked(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, userID := issueTestSessionTokenWithID(t, users)
	bindFixtureSessionToUser(t, pool, userID)

	revoked := createSessionRow(t, pool, userID, "revoked-ua", "10.0.0.99")
	if _, err := pool.Exec(context.Background(),
		`UPDATE sessions SET revoked_at = now() WHERE id = $1`, revoked,
	); err != nil {
		t.Fatalf("revoking fixture session returned unexpected error: %v", err)
	}

	live := createSessionRow(t, pool, userID, "live-ua", "10.0.0.98")

	rec := doSessionsRequest(t, r, http.MethodGet, "/api/auth/sessions", currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	views := decodeSessionViews(t, rec.Body.Bytes())

	for _, v := range views {
		if v.ID == revoked {
			t.Errorf("revoked session %q appeared in ListForUser, want excluded", revoked)
		}
	}

	var foundLive bool
	for _, v := range views {
		if v.ID == live {
			foundLive = true
		}
	}
	if !foundLive {
		t.Errorf("live session %q missing from list", live)
	}
}

// TestSessionsHandler_List_ExcludesExpired proves the >24h filter: a row
// backdated past SessionTTL disappears from the list.
func TestSessionsHandler_List_ExcludesExpired(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, userID := issueTestSessionTokenWithID(t, users)
	bindFixtureSessionToUser(t, pool, userID)

	expired := createSessionRowBackdated(t, pool, userID, "expired-ua", "10.0.0.50", time.Now().Add(-25*time.Hour))
	live := createSessionRow(t, pool, userID, "live-ua", "10.0.0.51")

	rec := doSessionsRequest(t, r, http.MethodGet, "/api/auth/sessions", currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	views := decodeSessionViews(t, rec.Body.Bytes())

	for _, v := range views {
		if v.ID == expired {
			t.Errorf("expired session %q appeared in ListForUser, want excluded", expired)
		}
	}

	var foundLive bool
	for _, v := range views {
		if v.ID == live {
			foundLive = true
		}
	}
	if !foundLive {
		t.Errorf("live session %q missing from list", live)
	}
}

// TestSessionsHandler_Revoke_OtherSession_200AndRevokesRow proves the
// happy path: revoking an other-session sets revoked_at, returns 200,
// and the other (still-current) session is unaffected.
func TestSessionsHandler_Revoke_OtherSession_200AndRevokesRow(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, userID := issueTestSessionTokenWithID(t, users)

	victim := createSessionRow(t, pool, userID, "victim-ua", "10.0.0.10")

	rec := doSessionsRequest(t, r, http.MethodDelete, "/api/auth/sessions/"+victim, currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var revokedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT revoked_at FROM sessions WHERE id = $1`, victim,
	).Scan(&revokedAt); err != nil {
		t.Fatalf("reading revoked_at returned unexpected error: %v", err)
	}
	if revokedAt == nil {
		t.Error("revoked_at still NULL after DELETE 200, want set")
	}
}

// TestSessionsHandler_Revoke_CurrentSession_409 proves SESS-09: trying
// to revoke the caller's own current session returns 409 and the row
// stays un-revoked. The user must call POST /api/auth/logout instead.
func TestSessionsHandler_Revoke_CurrentSession_409(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, _ := issueTestSessionTokenWithID(t, users)

	rec := doSessionsRequest(t, r, http.MethodDelete,
		"/api/auth/sessions/"+auth.IssueTestSessionID, currentToken)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}

	// Row is unchanged - revoked_at still NULL.
	var revokedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT revoked_at FROM sessions WHERE id = $1`, auth.IssueTestSessionID,
	).Scan(&revokedAt); err != nil {
		t.Fatalf("reading revoked_at returned unexpected error: %v", err)
	}
	if revokedAt != nil {
		t.Errorf("current session revoked_at = %v after 409, want NULL (handler must not have touched it)", revokedAt)
	}
}

// TestSessionsHandler_Revoke_OtherUserSession_404 proves the
// anti-enumeration posture (SESS-10): trying to revoke a session that
// belongs to a different user returns 404 (NOT 403), so the response
// can't be used to discover whether a given id exists.
func TestSessionsHandler_Revoke_OtherUserSession_404(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	// owner creates a session that the bystander will try to revoke.
	ownerID := mustCreateUserWithMembership(t, users, pool)
	victim := createSessionRow(t, pool, ownerID, "owner-ua", "10.0.0.20")

	// bystander is a different user with their own token.
	bystanderToken, _ := issueTestSessionTokenWithID(t, users)

	rec := doSessionsRequest(t, r, http.MethodDelete,
		"/api/auth/sessions/"+victim, bystanderToken)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (anti-enumeration), body = %s", rec.Code, rec.Body.String())
	}

	// Row is unchanged.
	var revokedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT revoked_at FROM sessions WHERE id = $1`, victim,
	).Scan(&revokedAt); err != nil {
		t.Fatalf("reading revoked_at returned unexpected error: %v", err)
	}
	if revokedAt != nil {
		t.Errorf("other-user's session was revoked: revoked_at = %v, want NULL", revokedAt)
	}
}

// TestSessionsHandler_Revoke_MalformedUUID_404 proves the third
// anti-enumeration case: a non-UUID path param returns the same 404
// body as "doesn't exist", so the caller can't distinguish "typo" from
// "real id" from "other-user's id".
func TestSessionsHandler_Revoke_MalformedUUID_404(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, _, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, _ := issueTestSessionTokenWithID(t, users)

	rec := doSessionsRequest(t, r, http.MethodDelete,
		"/api/auth/sessions/not-a-uuid-at-all", currentToken)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

// TestSessionsHandler_Revoke_ThenRequireAuthRejects proves SESS-08: a
// revoked session's token is rejected on its very next authenticated
// request by RequireAuth (the wired-up integration the design promises).
func TestSessionsHandler_Revoke_ThenRequireAuthRejects(t *testing.T) {
	sessionsH := NewSessionsHandler(db.NewSessionRepository(nil), zap.NewNop())
	r, pool, users := newSessionsRouterForTest(t, sessionsH)

	currentToken, userID := issueTestSessionTokenWithID(t, users)

	// Create a non-current session and a token whose sid is its id.
	victimSID := createSessionRow(t, pool, userID, "ua", "10.0.0.30")
	victimToken, err := auth.IssueSessionWithTenant(userID, "", victimSID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSessionWithTenant(victim) returned unexpected error: %v", err)
	}

	// The "current" admin token (sid = IssueTestSessionID) does the revoke.
	rec := doSessionsRequest(t, r, http.MethodDelete,
		"/api/auth/sessions/"+victimSID, currentToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	// Replay the victim's token - RequireAuth must 401 because the
	// session row is revoked.
	rec = doSessionsRequest(t, r, http.MethodGet, "/api/auth/sessions", victimToken)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("post-revoke status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

// bindFixtureSessionToUser rebinds the shared IssueTestSessionID
// fixture session row's user_id to point at userID. The fixture is
// inserted by newAPITenantScopedPool with IssueTestSessionUserID (a
// fixed placeholder user) so the table isn't empty across all tests
// in this package - but sessions_handler tests need the row to appear
// in ListForUser(userID) so the "current" marking matches the token.
// Without this rebind the list query filters out the row (different
// user_id) and Current: true never appears.
//
// IMPORTANT: registers a t.Cleanup that rebinds the fixture back to
// IssueTestSessionUserID. Without this, the rebind leaves the fixture
// pointing at the test's freshly-created user, and that user's t.Cleanup
// deletes the row via ON DELETE CASCADE on the FK - taking the fixture
// with it and breaking every test that follows in this package that
// depends on IssueTestSessionID resolving to a live row.
func bindFixtureSessionToUser(t *testing.T, pool *db.Pool, userID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE sessions SET user_id = $1 WHERE id = $2`,
		userID, auth.IssueTestSessionID,
	); err != nil {
		t.Fatalf("bindFixtureSessionToUser returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`UPDATE sessions SET user_id = $1 WHERE id = $2`,
			auth.IssueTestSessionUserID, auth.IssueTestSessionID,
		); err != nil {
			t.Logf("bindFixtureSessionToUser cleanup rebind failed: %v", err)
		}
	})
}

// mustCreateUserWithMembership creates a fresh user with a tenant
// membership in the fixture tenant (so the user can be referenced by
// tests that need a second distinct account, like the anti-enumeration
// "other-user" case). Returns the user's id.
func mustCreateUserWithMembership(t *testing.T, users *db.UserRepository, pool *db.Pool) string {
	t.Helper()
	ctx := context.Background()
	user := &db.User{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("users.Create returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = users.Delete(context.Background(), user.ID) })

	seedMembership(t, user.ID, currentAPITestTenant, db.RoleOwner)
	return user.ID
}
