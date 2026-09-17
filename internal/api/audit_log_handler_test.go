package api

import (
	"context"
	"database/sql"
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

// auditLogHandlerTestSecret is a local secret for this file's tokens -
// intentionally not the shared middlewareTestSecret (middleware_test.go
// is //go:build integration; this file is a plain unit test, so it can't
// depend on anything gated behind that tag).
const auditLogHandlerTestSecret = "audit-log-handler-test-secret-32b!!"

// fakeAuditLogRepo is an in-memory stand-in for *db.AuditLogRepository,
// letting AuditLogHandler's tests assert exactly what limit the handler
// passed through and control what ListRecent returns, without a real
// database (Test Coverage Matrix: "unit + integration for the route
// wiring" - the DB-touching half is covered by
// internal/db/audit_log_repository_test.go's own integration suite).
type fakeAuditLogRepo struct {
	entries     []db.AuditLogEntry
	err         error
	gotCtx      context.Context
	gotLimit    int
	limitCalled bool
}

func (f *fakeAuditLogRepo) ListRecent(ctx context.Context, limit int) ([]db.AuditLogEntry, error) {
	f.gotCtx = ctx
	f.gotLimit = limit
	f.limitCalled = true
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// fakeAuditLogUserLoader/fakeAuditLogSessionLoader are minimal
// userLoader/sessionLoader stand-ins so RequireAuth can be exercised
// without a real database - same technique as middleware_role_test.go's
// direct-context approach, one layer up (RequireAuth itself, not just
// RequireRole).
type fakeAuditLogUserLoader struct {
	user *db.User
	err  error
}

func (f *fakeAuditLogUserLoader) GetByID(ctx context.Context, id string) (*db.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

type fakeAuditLogSessionLoader struct {
	session *db.Session
	err     error
}

func (f *fakeAuditLogSessionLoader) GetByID(ctx context.Context, id string) (*db.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.session, nil
}

func (f *fakeAuditLogSessionLoader) TouchLastSeen(ctx context.Context, id string) error {
	return nil
}

// newAuditLogTestRouter wires AuditLogHandler behind RequireAuth (fakes,
// no DB) exactly like buildAdminRouter's protected group does, minus
// TenantContext/anyRole - the handler itself carries no auth or role
// logic (that's RequireAuth/RequireRole's job, covered by their own unit
// suites plus the routes_test.go reachability check this task also
// requires), so this router only needs enough middleware to prove the
// handler's own behavior: 401 with no session, 200 otherwise.
func newAuditLogTestRouter(repo auditLogLister) (http.Handler, *fakeAuditLogRepo) {
	fake, ok := repo.(*fakeAuditLogRepo)
	if !ok {
		fake = &fakeAuditLogRepo{}
	}
	handler := NewAuditLogHandler(repo, zap.NewNop())

	user := &db.User{ID: "11111111-1111-1111-1111-111111111111", Email: "viewer@example.com"}
	session := &db.Session{ID: auth.IssueTestSessionID}

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(auditLogHandlerTestSecret, &fakeAuditLogUserLoader{user: user}, &fakeAuditLogSessionLoader{session: session}, zap.NewNop()))
		protected.Get("/api/audit-log", handler.Get)
	})

	return r, fake
}

func issueAuditLogTestToken(t *testing.T) string {
	t.Helper()
	token, err := auth.IssueSession("11111111-1111-1111-1111-111111111111", auth.IssueTestSessionID, auditLogHandlerTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}
	return token
}

func getAuditLog(t *testing.T, r http.Handler, token, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/audit-log"
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestAuditLogHandler_Get_NoAuth_401 covers spec.md AC7.
func TestAuditLogHandler_Get_NoAuth_401(t *testing.T) {
	r, _ := newAuditLogTestRouter(&fakeAuditLogRepo{})

	rec := getAuditLog(t, r, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestAuditLogHandler_Get_DefaultLimit_5 covers spec.md AC6: an omitted
// limit param defaults to 5.
func TestAuditLogHandler_Get_DefaultLimit_5(t *testing.T) {
	r, fake := newAuditLogTestRouter(&fakeAuditLogRepo{})
	token := issueAuditLogTestToken(t)

	rec := getAuditLog(t, r, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !fake.limitCalled || fake.gotLimit != 5 {
		t.Errorf("ListRecent called with limit = %d (called=%v), want 5", fake.gotLimit, fake.limitCalled)
	}
}

// TestAuditLogHandler_Get_ExplicitLimit_PassedThrough covers spec.md AC6.
func TestAuditLogHandler_Get_ExplicitLimit_PassedThrough(t *testing.T) {
	r, fake := newAuditLogTestRouter(&fakeAuditLogRepo{})
	token := issueAuditLogTestToken(t)

	rec := getAuditLog(t, r, token, "limit=3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if fake.gotLimit != 3 {
		t.Errorf("ListRecent called with limit = %d, want 3", fake.gotLimit)
	}
}

// TestAuditLogHandler_Get_LimitAboveMax_CappedAt20 covers spec.md's edge
// case: a requested limit above 20 is silently capped, not rejected.
func TestAuditLogHandler_Get_LimitAboveMax_CappedAt20(t *testing.T) {
	r, fake := newAuditLogTestRouter(&fakeAuditLogRepo{})
	token := issueAuditLogTestToken(t)

	rec := getAuditLog(t, r, token, "limit=500")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if fake.gotLimit != 20 {
		t.Errorf("ListRecent called with limit = %d, want 20 (capped)", fake.gotLimit)
	}
}

// TestAuditLogHandler_Get_InvalidLimit_FallsBackToDefault covers spec.md's
// edge case: 0, negative, or non-numeric limit falls back to 5, never a
// 4xx.
func TestAuditLogHandler_Get_InvalidLimit_FallsBackToDefault(t *testing.T) {
	for _, rawLimit := range []string{"0", "-1", "not-a-number"} {
		t.Run(rawLimit, func(t *testing.T) {
			r, fake := newAuditLogTestRouter(&fakeAuditLogRepo{})
			token := issueAuditLogTestToken(t)

			rec := getAuditLog(t, r, token, "limit="+rawLimit)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if fake.gotLimit != 5 {
				t.Errorf("ListRecent called with limit = %d, want 5 (fallback)", fake.gotLimit)
			}
		})
	}
}

// TestAuditLogHandler_Get_Success_ReturnsMappedShape covers spec.md AC6's
// response shape: action, target_label, actor_name, actor_deleted,
// created_at, for both a labeled/named entry and a NULL-label/
// actor-deleted entry.
func TestAuditLogHandler_Get_Success_ReturnsMappedShape(t *testing.T) {
	createdAt := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	label := "example.com"
	repo := &fakeAuditLogRepo{entries: []db.AuditLogEntry{
		{Action: "domain_verified", TargetLabel: &label, ActorName: "Jane Doe", ActorDeleted: false, CreatedAt: createdAt},
		{Action: "removed", TargetLabel: nil, ActorName: "", ActorDeleted: true, CreatedAt: createdAt},
	}}
	r, _ := newAuditLogTestRouter(repo)
	token := issueAuditLogTestToken(t)

	rec := getAuditLog(t, r, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []auditLogEntryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}

	first := got[0]
	if first.Action != "domain_verified" {
		t.Errorf("got[0].Action = %q, want %q", first.Action, "domain_verified")
	}
	if first.TargetLabel == nil || *first.TargetLabel != label {
		t.Errorf("got[0].TargetLabel = %v, want %q", first.TargetLabel, label)
	}
	if first.ActorName != "Jane Doe" {
		t.Errorf("got[0].ActorName = %q, want %q", first.ActorName, "Jane Doe")
	}
	if first.ActorDeleted {
		t.Error("got[0].ActorDeleted = true, want false")
	}
	if !first.CreatedAt.Equal(createdAt) {
		t.Errorf("got[0].CreatedAt = %v, want %v", first.CreatedAt, createdAt)
	}

	second := got[1]
	if second.TargetLabel != nil {
		t.Errorf("got[1].TargetLabel = %v, want nil", second.TargetLabel)
	}
	if !second.ActorDeleted {
		t.Error("got[1].ActorDeleted = false, want true")
	}
	if second.ActorName != "" {
		t.Errorf("got[1].ActorName = %q, want empty string", second.ActorName)
	}
}

// TestAuditLogHandler_Get_RepositoryError_500 covers the standard
// generic-500 posture (AGENTS.md §4): a repository failure never leaks
// err.Error() to the client.
func TestAuditLogHandler_Get_RepositoryError_500(t *testing.T) {
	r, _ := newAuditLogTestRouter(&fakeAuditLogRepo{err: sql.ErrConnDone})
	token := issueAuditLogTestToken(t)

	rec := getAuditLog(t, r, token, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
