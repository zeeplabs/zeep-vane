//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

const adminsTestMasterKey = "admins-test-master-key"

// testAdminBaseURL stands in for cfg.AdminBaseURL in tests that build an
// admin-facing email link (admin-invite, password-reset) - shared with
// password_reset_handler_test.go, same package.
const testAdminBaseURL = "https://vane.test"

// fakeEmailProviderStore is an in-memory email.EmailProviderStore double, so
// admins_test.go can exercise real email.Service.SendAdminInvite calls
// without a real provider/DB row (same pattern as internal/email's own
// service_test.go fakeStore, duplicated here since that one is unexported).
type fakeEmailProviderStore struct {
	rows           map[string]*db.EmailProvider
	activeProvider string
}

func newFakeEmailProviderStore() *fakeEmailProviderStore {
	return &fakeEmailProviderStore{rows: map[string]*db.EmailProvider{}}
}

func (f *fakeEmailProviderStore) UpsertProvider(_ context.Context, provider string, encryptedAPIKey []byte, fromEmail, fromName string) error {
	f.rows[provider] = &db.EmailProvider{Provider: provider, EncryptedAPIKey: encryptedAPIKey, FromEmail: fromEmail, FromName: fromName, Status: "connected"}
	return nil
}
func (f *fakeEmailProviderStore) Get(_ context.Context, provider string) (*db.EmailProvider, error) {
	row, ok := f.rows[provider]
	if !ok {
		return nil, db.ErrNotFound
	}
	return row, nil
}
func (f *fakeEmailProviderStore) ListPaginated(_ context.Context, page, pageSize int) ([]db.EmailProvider, int, error) {
	rows := make([]db.EmailProvider, 0, len(f.rows))
	for _, row := range f.rows {
		rows = append(rows, *row)
	}
	total := len(rows)

	start := (page - 1) * pageSize
	if start >= total {
		return []db.EmailProvider{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return rows[start:end], total, nil
}
func (f *fakeEmailProviderStore) GetActiveProvider(_ context.Context) (string, error) {
	return f.activeProvider, nil
}
func (f *fakeEmailProviderStore) SetActiveProvider(_ context.Context, provider string) error {
	f.activeProvider = provider
	return nil
}

// fakeEmailProvider is an email.Provider double recording Send calls.
// notifySent, when non-nil, receives after each Send call - needed by
// password_reset_handler_test.go, whose PasswordResetHandler.Request
// dispatches the send in its own goroutine (so the request can respond
// without waiting on the email provider - see the F2 fix note in
// password_reset_handler.go): a test can't just read lastMessage/sendCalls
// right after the HTTP call returns, since nothing guarantees the goroutine
// has run yet, and doing so unsynchronized would be a data race on top of
// that. Every AdminsHandler test sends synchronously and leaves notifySent
// nil, so this is a no-op for them.
type fakeEmailProvider struct {
	sendErr     error
	sendCalls   int
	lastMessage email.Message
	notifySent  chan struct{}
}

func (f *fakeEmailProvider) ValidateCredentials(context.Context) error { return nil }
func (f *fakeEmailProvider) Send(_ context.Context, msg email.Message) error {
	f.sendCalls++
	f.lastMessage = msg
	if f.notifySent != nil {
		f.notifySent <- struct{}{}
	}
	return f.sendErr
}

// newTestEmailService builds a real *email.Service backed by an in-memory
// store, with "test" connected and active so SendAdminInvite succeeds
// (unless sendErr is set on the returned fakeEmailProvider) - lets handler
// tests exercise the actual send-then-log-failure code path instead of
// mocking AdminsHandler's email dependency away.
func newTestEmailService(t *testing.T) (*email.Service, *fakeEmailProvider) {
	t.Helper()
	store := newFakeEmailProviderStore()
	provider := &fakeEmailProvider{}
	encryptedKey, err := crypto.Encrypt(adminsTestMasterKey, []byte("fake-api-key"))
	if err != nil {
		t.Fatalf("crypto.Encrypt() returned unexpected error: %v", err)
	}
	if err := store.UpsertProvider(context.Background(), "test", encryptedKey, "noreply@example.com", "Vane"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}
	if err := store.SetActiveProvider(context.Background(), "test"); err != nil {
		t.Fatalf("SetActiveProvider() returned unexpected error: %v", err)
	}

	svc, err := email.NewService(store, func(string, string) (email.Provider, error) { return provider, nil }, adminsTestMasterKey, zap.NewNop())
	if err != nil {
		t.Fatalf("email.NewService() returned unexpected error: %v", err)
	}
	return svc, provider
}

// newTestEmailServiceNoActiveProvider builds a *email.Service with no
// connected/active provider, so SendAdminInvite always returns
// email.ErrNoActiveProvider (exercises the email_sent:false path without a
// send error).
func newTestEmailServiceNoActiveProvider(t *testing.T) *email.Service {
	t.Helper()
	svc, err := email.NewService(newFakeEmailProviderStore(), func(string, string) (email.Provider, error) {
		t.Fatal("factory should not be called with no active provider")
		return nil, nil
	}, adminsTestMasterKey, zap.NewNop())
	if err != nil {
		t.Fatalf("email.NewService() returned unexpected error: %v", err)
	}
	return svc
}

func newAdminsRouterWithEmail(t *testing.T, emailSvc *email.Service) (http.Handler, *db.Pool, *db.AdminRepository, *db.AdminInviteRepository) {
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

	// Every test in this file goes through this constructor and virtually
	// all of them create at least one admin, which - via
	// AdminRepository.Create's database default (owner, migration 0009) -
	// transiently creates an owner-role row regardless of the role the
	// test actually cares about. `go test ./...` runs internal/db,
	// internal/api, and internal/cli as separate concurrent processes
	// against the same TEST_DATABASE_URL, so any of these tests can
	// otherwise race another package's owner-count-sensitive test.
	// Centralizing the lock here (idempotent per *testing.T - see
	// LockAdminsTable's doc comment) covers every test in this file
	// without each one having to remember to take it individually. Note:
	// deliberately passed context.Background(), not the bounded `ctx`
	// above, which is canceled by the deferred cancel() as soon as this
	// function returns - the lock's dedicated connection must outlive it.
	dbtest.LockAdminsTable(t, context.Background(), dsn)

	admins := db.NewAdminRepository(pool)
	invites := db.NewAdminInviteRepository(pool)
	auditLog := audit.NewLog(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	handler := NewAdminsHandler(pool, admins, invites, emailSvc, companySettings, auditLog, zap.NewNop(), false, testAdminBaseURL, middlewareTestSecret, true)

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.With(RequireRole(db.RoleOwner)).Post("/api/admins", handler.Invite)
		protected.With(RequireRole(db.RoleOwner)).Patch("/api/admins/{id}/role", handler.UpdateRole)
		protected.With(RequireRole(db.RoleOwner)).Delete("/api/admins/{id}", handler.Delete)
		protected.With(RequireRole(db.RoleOwner)).Get("/api/admins", handler.List)
		protected.With(RequireRole(db.RoleOwner)).Post("/api/admins/invites/{id}/resend", handler.ResendInvite)
		protected.With(RequireRole(db.RoleOwner)).Delete("/api/admins/invites/{id}", handler.CancelInvite)
	})
	r.Post("/api/admins/invite/{token}/accept", handler.AcceptInvite)

	return r, pool, admins, invites
}

// newAdminsRouter builds a router with a default working email service
// (active provider connected) - used by every existing test that doesn't
// specifically exercise email send-success/failure behavior.
func newAdminsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository, *db.AdminInviteRepository) {
	t.Helper()
	svc, _ := newTestEmailService(t)
	return newAdminsRouterWithEmail(t, svc)
}

// issueTestSessionTokenWithRole is issueTestSessionToken, additionally
// setting the created admin's role - needed to exercise RequireRole's 403
// path for operator/viewer.
//
// admins.Create always inserts with the `admins.role` column's database
// default, which is `owner` (see migration 0009) - regardless of the
// `role` requested here, every call transiently creates an owner-role row
// until/unless the UpdateRole call below moves it away. That makes this
// helper the single common point every owner-sensitive test in this
// package (including poller_status_test.go, which shares it) goes
// through, so it takes LockAdminsTable itself rather than relying on
// each call site to remember to. See LockAdminsTable's doc comment for
// why this must be held across concurrently-run packages, not just
// within this one.
func issueTestSessionTokenWithRole(t *testing.T, admins *db.AdminRepository, role string) string {
	t.Helper()
	ctx := context.Background()
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))
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
	body, err := json.Marshal(inviteAdminRequest{Name: "Test Invitee", Email: email, Role: role})
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
	id        string
	role      string
	usedAt    *time.Time
	createdAt time.Time
	expiresAt time.Time
}

func latestInviteForEmail(t *testing.T, pool *db.Pool, email string) pendingInviteRow {
	t.Helper()
	row := pool.QueryRow(context.Background(),
		"SELECT id, role, used_at, created_at, expires_at FROM admin_invites WHERE email = $1 ORDER BY created_at DESC LIMIT 1", email)

	var got pendingInviteRow
	if err := row.Scan(&got.id, &got.role, &got.usedAt, &got.createdAt, &got.expiresAt); err != nil {
		t.Fatalf("querying latest admin_invites row for %q returned unexpected error: %v", email, err)
	}
	return got
}

func TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry(t *testing.T) {
	svc, provider := newTestEmailService(t)
	r, pool, admins, _ := newAdminsRouterWithEmail(t, svc)
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

	// ADM-01: the invite token is valid for 1 hour. This reads expires_at
	// from the row the Invite handler itself persisted (not a TTL fabricated
	// by a test helper like createTestInvite), so a regression in
	// adminInviteTTL is observed by this test.
	gotTTL := invite.expiresAt.Sub(invite.createdAt)
	const wantTTL = 1 * time.Hour
	const ttlTolerance = 2 * time.Second
	if diff := gotTTL - wantTTL; diff < -ttlTolerance || diff > ttlTolerance {
		t.Errorf("invite TTL (expires_at - created_at) = %v, want %v (+/- %v)", gotTTL, wantTTL, ttlTolerance)
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

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["status"] != "invited" {
		t.Errorf(`response["status"] = %v, want "invited"`, resp["status"])
	}
	if resp["email_sent"] != true {
		t.Errorf(`response["email_sent"] = %v, want true (active provider connected)`, resp["email_sent"])
	}
	if _, hasToken := resp["token"]; hasToken {
		t.Error("response body must never include the raw invite token")
	}

	// INVITE-01 AC1: the sent email must actually address the invitee, in
	// their invited role, with a *working* (raw, not hashed) accept link -
	// not merely "some email got sent somewhere" (provider.sendCalls == 1).
	if provider.lastMessage.To != email {
		t.Errorf("sent email To = %q, want %q", provider.lastMessage.To, email)
	}
	if !strings.Contains(provider.lastMessage.TextBody, db.RoleOperator) {
		t.Errorf("sent email TextBody = %q, want it to mention invited role %q", provider.lastMessage.TextBody, db.RoleOperator)
	}
	rawToken := extractAcceptToken(t, provider.lastMessage.TextBody)
	acceptRec := postAcceptInvite(t, r, rawToken, "correct-horse-battery-staple")
	if acceptRec.Code != http.StatusCreated {
		t.Errorf("accepting the token from the sent email status = %d, want %d (proves AcceptURL carries the raw, unhashed token) - body = %s", acceptRec.Code, http.StatusCreated, acceptRec.Body.String())
	}
}

// extractAcceptToken pulls the raw invite token out of an AcceptURL embedded
// in a sent email's TextBody ("...://.../accept-invite/<token>"), so a test
// can prove the link is genuinely usable rather than just present.
func extractAcceptToken(t *testing.T, textBody string) string {
	t.Helper()
	const marker = "/accept-invite/"
	idx := strings.Index(textBody, marker)
	if idx < 0 {
		t.Fatalf("email TextBody = %q, want it to contain %q", textBody, marker)
	}
	rest := textBody[idx+len(marker):]
	end := strings.IndexAny(rest, " \n\r\t")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

func TestInviteAdmin_EmailSendFails_StillCreatesInviteWithEmailSentFalse(t *testing.T) {
	svc, provider := newTestEmailService(t)
	provider.sendErr = errors.New("provider: send failed")
	r, pool, admins, _ := newAdminsRouterWithEmail(t, svc)
	token := issueTestSessionToken(t, admins)
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })

	rec := postInviteAdmin(t, r, token, email, db.RoleOperator)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["email_sent"] != false {
		t.Errorf(`response["email_sent"] = %v, want false (send failed)`, resp["email_sent"])
	}

	invite := latestInviteForEmail(t, pool, email)
	if invite.usedAt != nil {
		t.Error("invite.usedAt = non-nil, want nil - email send failure must not affect the created invite")
	}
	if provider.sendCalls != 1 {
		t.Errorf("provider.sendCalls = %d, want 1", provider.sendCalls)
	}
}

func TestInviteAdmin_NoActiveEmailProvider_StillCreatesInviteWithEmailSentFalse(t *testing.T) {
	svc := newTestEmailServiceNoActiveProvider(t)
	r, pool, admins, _ := newAdminsRouterWithEmail(t, svc)
	token := issueTestSessionToken(t, admins)
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })

	rec := postInviteAdmin(t, r, token, email, db.RoleOperator)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["email_sent"] != false {
		t.Errorf(`response["email_sent"] = %v, want false (no active provider)`, resp["email_sent"])
	}

	invite := latestInviteForEmail(t, pool, email)
	if invite.usedAt != nil {
		t.Error("invite.usedAt = non-nil, want nil - no active provider must not affect the created invite")
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

// createTestInvite inserts a pending admin_invites row directly (bypassing
// the Invite handler, whose raw token is only observable via logging), so
// AcceptInvite tests can drive the accept endpoint with a known raw token.
func createTestInvite(t *testing.T, invites *db.AdminInviteRepository, inviterID, email, role string, ttl time.Duration) string {
	t.Helper()
	rawToken, err := generateAdminInviteToken()
	if err != nil {
		t.Fatalf("generateAdminInviteToken() returned unexpected error: %v", err)
	}

	invite := &db.AdminInvite{
		Email:       email,
		Role:        role,
		TokenHash:   hashAdminInviteToken(rawToken),
		InvitedByID: inviterID,
		ExpiresAt:   time.Now().Add(ttl),
	}
	if err := invites.Create(context.Background(), invite); err != nil {
		t.Fatalf("invites.Create() returned unexpected error: %v", err)
	}
	return rawToken
}

func postAcceptInvite(t *testing.T, r http.Handler, token, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(acceptAdminInviteRequest{Password: password})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admins/invite/"+token+"/accept", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAcceptInvite_ValidToken_201_ActivatesAccountWithInvitedRole(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admins WHERE email = $1", email) })
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, email, db.RoleOperator, 1*time.Hour)

	rec := postAcceptInvite(t, r, rawToken, "a-strong-password")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	created, err := admins.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	// GetByEmail (mvp-core, unmodified by this feature) doesn't select
	// role - read it back directly to confirm the invited role was applied.
	var gotRole string
	roleRow := pool.QueryRow(context.Background(), "SELECT role FROM admins WHERE id = $1", created.ID)
	if err := roleRow.Scan(&gotRole); err != nil {
		t.Fatalf("querying created admin's role returned unexpected error: %v", err)
	}
	if gotRole != db.RoleOperator {
		t.Errorf("created admin role = %q, want %q", gotRole, db.RoleOperator)
	}

	invite, err := invites.GetByTokenHash(context.Background(), hashAdminInviteToken(rawToken))
	if err != nil {
		t.Fatalf("GetByTokenHash() returned unexpected error: %v", err)
	}
	if invite.UsedAt == nil {
		t.Error("invite.UsedAt = nil after accept, want non-nil (marked used)")
	}
}

// TestAcceptInvite_ValidToken_SetsAuthenticatingSessionCookie covers
// AIP-01/AIP-02: accepting an invite must authenticate the invitee
// immediately (mirrors BootstrapHandler.Create's issue-then-set-cookie
// sequence) - not merely set some cookie, but one that actually verifies
// back to the newly created admin's own ID.
func TestAcceptInvite_ValidToken_SetsAuthenticatingSessionCookie(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admins WHERE email = $1", email) })
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, email, db.RoleOperator, 1*time.Hour)

	rec := postAcceptInvite(t, r, rawToken, "a-strong-password")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("no %s cookie in response, want one set", sessionCookieName)
	}
	if sessionCookie.Value == "" {
		t.Error("session cookie has empty value, want the session token")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie HttpOnly = false, want true")
	}
	if !sessionCookie.Secure {
		t.Error("session cookie Secure = false, want true")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie SameSite = %v, want %v", sessionCookie.SameSite, http.SameSiteStrictMode)
	}
	wantMaxAge := int(auth.SessionTTL.Seconds())
	if sessionCookie.MaxAge != wantMaxAge {
		t.Errorf("session cookie MaxAge = %d, want %d", sessionCookie.MaxAge, wantMaxAge)
	}

	created, err := admins.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	gotAdminID, err := auth.VerifySession(sessionCookie.Value, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.VerifySession() on the accept-invite cookie returned unexpected error: %v", err)
	}
	if gotAdminID != created.ID {
		t.Errorf("session cookie authenticates admin %q, want the newly created admin %q", gotAdminID, created.ID)
	}
}

func TestAcceptInvite_ExpiredToken_401_NoStateChange(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	email := uniqueTestEmail(t)
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, email, db.RoleOperator, -1*time.Hour)

	rec := postAcceptInvite(t, r, rawToken, "a-strong-password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}

	if _, err := admins.GetByEmail(context.Background(), email); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetByEmail() after rejected accept error = %v, want ErrNotFound (no admin created)", err)
	}
}

func TestAcceptInvite_AlreadyUsedToken_401(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admins WHERE email = $1", email) })
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, email, db.RoleViewer, 1*time.Hour)

	first := postAcceptInvite(t, r, rawToken, "a-strong-password")
	if first.Code != http.StatusCreated {
		t.Fatalf("first accept status = %d, want %d", first.Code, http.StatusCreated)
	}

	second := postAcceptInvite(t, r, rawToken, "a-different-password")
	if second.Code != http.StatusUnauthorized {
		t.Errorf("second accept status = %d, want %d, body = %s", second.Code, http.StatusUnauthorized, second.Body.String())
	}
}

// TestAcceptInvite_ConcurrentAccept_OnlyOneSucceeds is the L24 regression
// guard: the same invite token submitted twice concurrently must produce
// exactly one created admin, never two - ClaimForUse's atomic
// UPDATE...WHERE used_at IS NULL is what enforces this, not the earlier
// in-Go used_at/expiry check that raced.
func TestAcceptInvite_ConcurrentAccept_OnlyOneSucceeds(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admins WHERE email = $1", email) })
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, email, db.RoleViewer, 1*time.Hour)

	body, err := json.Marshal(acceptAdminInviteRequest{Password: "a-strong-password"})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	// t.Fatalf/t.Helper must only run on the goroutine running the test, so
	// the concurrent requests below build their own httptest.NewRequest and
	// call r.ServeHTTP directly rather than going through the
	// postAcceptInvite helper (which does).
	const concurrency = 10
	codes := make([]int, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/admins/invite/"+rawToken+"/accept", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	created := 0
	for _, code := range codes {
		if code == http.StatusCreated {
			created++
		}
	}
	if created != 1 {
		t.Errorf("successful (201) accepts among %d concurrent requests = %d, want exactly 1", concurrency, created)
	}

	var count int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM admins WHERE email = $1", email).Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count for the invited email = %d, want 1", count)
	}
}

func TestAcceptInvite_MissingPassword_422(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	rawToken := createTestInvite(t, invites, inviterAdmin.ID, uniqueTestEmail(t), db.RoleViewer, 1*time.Hour)

	rec := postAcceptInvite(t, r, rawToken, "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestAcceptInvite_WeakPassword_422 is the H11 regression guard: a password
// below auth.MinPasswordLength must be rejected before an invited admin
// account is created.
func TestAcceptInvite_WeakPassword_422(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	inviterAdmin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviterAdmin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviterAdmin.ID) })

	inviteEmail := uniqueTestEmail(t)
	rawToken := createTestInvite(t, invites, inviterAdmin.ID, inviteEmail, db.RoleViewer, 1*time.Hour)

	rec := postAcceptInvite(t, r, rawToken, "1234567")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if _, err := admins.GetByEmail(context.Background(), inviteEmail); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetByEmail() err = %v, want db.ErrNotFound (no admin created for a weak-password-rejected invite)", err)
	}
}

func patchAdminRole(t *testing.T, r http.Handler, token, targetID, role string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(updateAdminRoleRequest{Role: role})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/admins/"+targetID+"/role", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUpdateAdminRole_ValidChange_200_AppliesRoleRevokesSessionsAndAudits(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	ctx := context.Background()

	actor := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, actor); err != nil {
		t.Fatalf("admins.Create() actor returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), actor.ID) })
	actorToken, err := auth.IssueSession(actor.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() actor returned unexpected error: %v", err)
	}

	// A second owner besides actor, so this change can never trip the
	// ADM-06 lockout guard regardless of any other ambient owner rows in
	// the shared test database.
	target := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, target); err != nil {
		t.Fatalf("admins.Create() target returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), target.ID) })
	targetOldToken, err := auth.IssueSession(target.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() target returned unexpected error: %v", err)
	}
	targetOldClaims, err := auth.VerifySessionClaims(targetOldToken, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.VerifySessionClaims() returned unexpected error: %v", err)
	}

	rec := patchAdminRole(t, r, actorToken, target.ID, db.RoleViewer)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp adminResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Role != db.RoleViewer {
		t.Errorf("response Role = %q, want %q", resp.Role, db.RoleViewer)
	}

	var gotRole string
	var revokedAt *time.Time
	row := pool.QueryRow(ctx, "SELECT role, sessions_revoked_at FROM admins WHERE id = $1", target.ID)
	if err := row.Scan(&gotRole, &revokedAt); err != nil {
		t.Fatalf("querying target admin returned unexpected error: %v", err)
	}
	if gotRole != db.RoleViewer {
		t.Errorf("stored role = %q, want %q", gotRole, db.RoleViewer)
	}
	if revokedAt == nil || revokedAt.Before(targetOldClaims.IssuedAt) {
		t.Errorf("sessions_revoked_at = %v, want a timestamp at or after the old token's issued-at %v", revokedAt, targetOldClaims.IssuedAt)
	}

	var gotActorID, gotAction string
	auditRow := pool.QueryRow(ctx,
		"SELECT actor_id, action FROM admin_audit_log WHERE target_id = $1 AND action = 'role_changed'", target.ID)
	if err := auditRow.Scan(&gotActorID, &gotAction); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if gotActorID != actor.ID {
		t.Errorf("admin_audit_log actor_id = %q, want %q", gotActorID, actor.ID)
	}
}

// quarantineAmbientOwners is the only reliable way to test the ADM-06
// lockout rejection end-to-end in this shared integration test database:
// the count this handler rejects on is a genuine SELECT ... FOR UPDATE
// COUNT(*) over the whole admins table (CountActiveOwners has no
// per-test scoping), and other tests/packages routinely leave owner rows
// behind (some pre-existing fixtures never clean up, e.g. auth_handler_test.go
// admins created across earlier runs of this same suite) - so "0 other
// active owners" cannot be assumed. This helper demotes every currently
// active owner to operator for the duration of the calling test and
// restores them via t.Cleanup, so the test's own single owner is
// deterministically the only one CountActiveOwners will see.
func quarantineAmbientOwners(t *testing.T, admins *db.AdminRepository, pool *db.Pool) {
	t.Helper()
	ctx := context.Background()

	// The snapshot-quarantine-restore window below only reflects an
	// accurate "last owner" state if no other package's test can create
	// or delete an owner-role row in the shared `admins` table while it
	// is open - see LockAdminsTable's doc comment for why this must be
	// held across concurrently-run packages, not just within this one.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

	rows, err := pool.Query(ctx, "SELECT id FROM admins WHERE role = $1", db.RoleOwner)
	if err != nil {
		t.Fatalf("querying ambient owners returned unexpected error: %v", err)
	}
	var ambientOwnerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scanning ambient owner id returned unexpected error: %v", err)
		}
		ambientOwnerIDs = append(ambientOwnerIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("reading ambient owners returned unexpected error: %v", err)
	}

	for _, id := range ambientOwnerIDs {
		if err := admins.UpdateRole(ctx, id, db.RoleOperator); err != nil {
			t.Fatalf("quarantining ambient owner %q returned unexpected error: %v", id, err)
		}
	}

	t.Cleanup(func() {
		for _, id := range ambientOwnerIDs {
			_ = admins.UpdateRole(context.Background(), id, db.RoleOwner)
		}
	})
}

func TestUpdateAdminRole_SelfDemotionAsLastOwner_409(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	ctx := context.Background()
	quarantineAmbientOwners(t, admins, pool)

	owner := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, owner); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), owner.ID) })
	token, err := auth.IssueSession(owner.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	rec := patchAdminRole(t, r, token, owner.ID, db.RoleViewer)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	got, err := admins.GetByID(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.Role != db.RoleOwner {
		t.Errorf("Role after rejected self-demotion = %q, want unchanged %q", got.Role, db.RoleOwner)
	}
	if got.SessionsRevokedAt != nil {
		t.Errorf("SessionsRevokedAt after rejected self-demotion = %v, want nil (no state change)", got.SessionsRevokedAt)
	}
}

func TestUpdateAdminRole_InvalidRole_422(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchAdminRole(t, r, token, "00000000-0000-0000-0000-000000000000", "superadmin")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestUpdateAdminRole_NotFound_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchAdminRole(t, r, token, "00000000-0000-0000-0000-000000000000", db.RoleViewer)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func deleteAdmin(t *testing.T, r http.Handler, token, targetID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/admins/"+targetID, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDeleteAdmin_ValidRemoval_200_RevokesSessionsDeletesAndAudits(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	ctx := context.Background()

	actor := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, actor); err != nil {
		t.Fatalf("admins.Create() actor returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), actor.ID) })
	actorToken, err := auth.IssueSession(actor.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() actor returned unexpected error: %v", err)
	}

	// A second owner besides actor, so this removal can never trip the
	// ADM-06 lockout guard regardless of ambient owner rows.
	target := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, target); err != nil {
		t.Fatalf("admins.Create() target returned unexpected error: %v", err)
	}

	rec := deleteAdmin(t, r, actorToken, target.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if _, err := admins.GetByID(ctx, target.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetByID() after Delete = %v, want ErrNotFound", err)
	}

	var gotActorID, gotAction string
	row := pool.QueryRow(ctx,
		"SELECT actor_id, action FROM admin_audit_log WHERE target_id = $1 AND action = 'removed'", target.ID)
	if err := row.Scan(&gotActorID, &gotAction); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if gotActorID != actor.ID {
		t.Errorf("admin_audit_log actor_id = %q, want %q", gotActorID, actor.ID)
	}
}

// TestDeleteAdmin_ValidRemoval_OldJWTRejected_401 is Fix 4 (ADM-07 +
// spec.md:97 edge case). TestDeleteAdmin_ValidRemoval_200... above only
// asserts the admin row is gone (GetByID -> ErrNotFound); it never proves a
// JWT issued to the removed admin before removal stops working. This drives
// that token through RequireAuth (the same middleware routes.go wires in
// front of every protected route) after the removal and confirms it is
// rejected with 401, not just that the row no longer exists.
func TestDeleteAdmin_ValidRemoval_OldJWTRejected_401(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	ctx := context.Background()

	actor := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, actor); err != nil {
		t.Fatalf("admins.Create() actor returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), actor.ID) })
	actorToken, err := auth.IssueSession(actor.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() actor returned unexpected error: %v", err)
	}

	// A second owner besides actor, so this removal can never trip the
	// ADM-06 lockout guard regardless of ambient owner rows.
	target := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, target); err != nil {
		t.Fatalf("admins.Create() target returned unexpected error: %v", err)
	}
	targetToken, err := auth.IssueSession(target.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() target returned unexpected error: %v", err)
	}

	rec := deleteAdmin(t, r, actorToken, target.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// target.ID no longer exists in admins - RequireAuth's admins.GetByID
	// lookup must treat "not found" as unauthenticated (401), not crash or
	// pass the request through.
	var gotAdmin *db.Admin
	handler := newProtectedHandler(admins, &gotAdmin)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+targetToken)
	protectedRec := httptest.NewRecorder()
	handler.ServeHTTP(protectedRec, req)

	if protectedRec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (JWT issued before removal must be rejected)", protectedRec.Code, http.StatusUnauthorized)
	}
	if gotAdmin != nil {
		t.Errorf("Admin stored in context = %+v, want nil (removed admin must not reach the handler)", gotAdmin)
	}
}

func TestDeleteAdmin_SelfRemovalAsLastOwner_409(t *testing.T) {
	r, pool, admins, _ := newAdminsRouter(t)
	ctx := context.Background()
	quarantineAmbientOwners(t, admins, pool)

	owner := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, owner); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), owner.ID) })
	token, err := auth.IssueSession(owner.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	rec := deleteAdmin(t, r, token, owner.ID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	got, err := admins.GetByID(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetByID() after rejected self-removal returned unexpected error: %v (admin should still exist)", err)
	}
	if got.SessionsRevokedAt != nil {
		t.Errorf("SessionsRevokedAt after rejected self-removal = %v, want nil (no state change)", got.SessionsRevokedAt)
	}
}

func TestDeleteAdmin_NotFound_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := deleteAdmin(t, r, token, "00000000-0000-0000-0000-000000000000")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func getAdminsList(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admins", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getAdminsListPage(t *testing.T, r http.Handler, token string, page int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/admins?page=%d", page), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// findAdminAcrossPages pages through every page of GET /api/admins looking
// for email - the shared dev DB this integration suite runs against can
// accumulate admins/invites across many tests, so page 1 alone is not
// guaranteed to include one created just now (PAG-08, same reasoning as
// status-pages/domains/services handler tests).
func findAdminAcrossPages(t *testing.T, r http.Handler, token, email string) *adminResponse {
	t.Helper()
	for page := 1; ; page++ {
		rec := getAdminsListPage(t, r, token, page)
		if rec.Code != http.StatusOK {
			t.Fatalf("page=%d status = %d, want %d, body = %s", page, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got Page[adminResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
		}
		for i := range got.Items {
			if got.Items[i].Email == email {
				return &got.Items[i]
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return nil
		}
	}
}

func TestListAdmins_Owner_200_IncludesEveryAdminWithRole(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	target := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), target); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), target.ID) })
	if err := admins.UpdateRole(context.Background(), target.ID, db.RoleViewer); err != nil {
		t.Fatalf("admins.UpdateRole() returned unexpected error: %v", err)
	}

	rec := getAdminsList(t, r, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[adminResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (default)", page.Page)
	}
	if page.PageSize != 20 {
		t.Errorf("page.PageSize = %d, want 20", page.PageSize)
	}

	found := findAdminAcrossPages(t, r, token, target.Email)
	if found == nil {
		t.Fatalf("admin %q not present across any page of GET /api/admins", target.ID)
	}
	if found.Role != db.RoleViewer {
		t.Errorf("listed admin Role = %q, want %q", found.Role, db.RoleViewer)
	}
}

func TestListAdmins_Owner_200_MergesPendingInviteWithStatus(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })

	pendingEmail := uniqueTestEmail(t)
	createTestInvite(t, invites, inviter.ID, pendingEmail, db.RoleOperator, 1*time.Hour)

	rec := getAdminsList(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	pendingItem := findAdminAcrossPages(t, r, token, pendingEmail)
	activeItem := findAdminAcrossPages(t, r, token, inviter.Email)

	if pendingItem == nil {
		t.Fatalf("pending invite %q not present in list response %s", pendingEmail, rec.Body.String())
	}
	if pendingItem.Status != "pending" {
		t.Errorf("pending invite Status = %q, want %q", pendingItem.Status, "pending")
	}
	if pendingItem.Role != db.RoleOperator {
		t.Errorf("pending invite Role = %q, want %q", pendingItem.Role, db.RoleOperator)
	}
	if pendingItem.ExpiresAt == nil {
		t.Error("pending invite ExpiresAt = nil, want non-nil")
	}

	if activeItem == nil {
		t.Fatalf("active admin %q not present in list response %s", inviter.ID, rec.Body.String())
	}
	if activeItem.Status != "active" {
		t.Errorf("active admin Status = %q, want %q", activeItem.Status, "active")
	}
}

func postResendInvite(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admins/invites/"+id+"/resend", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestResendInvite_Owner_200_NewTokenWorksOldTokenRejected(t *testing.T) {
	svc, provider := newTestEmailService(t)
	r, pool, admins, invites := newAdminsRouterWithEmail(t, svc)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token, err := auth.IssueSession(inviter.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	oldRawToken := createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	rec := postResendInvite(t, r, token, inviteID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["status"] != "resent" {
		t.Errorf(`response["status"] = %v, want "resent"`, resp["status"])
	}
	if resp["email_sent"] != true {
		t.Errorf(`response["email_sent"] = %v, want true`, resp["email_sent"])
	}

	oldAcceptRec := postAcceptInvite(t, r, oldRawToken, "correct-horse-battery-staple")
	if oldAcceptRec.Code != http.StatusUnauthorized {
		t.Errorf("accepting old token after resend status = %d, want %d", oldAcceptRec.Code, http.StatusUnauthorized)
	}

	// ADM-01/INVITE-04: resend resets expires_at to exactly now()+adminInviteTTL,
	// not merely "some future value" - same precision the Invite path already
	// asserts for its own TTL (see TestInviteAdmin_Owner_201's gotTTL check).
	refreshed := latestInviteForEmail(t, pool, email)
	gotExpiresIn := time.Until(refreshed.expiresAt)
	const ttlTolerance = 2 * time.Second
	if diff := gotExpiresIn - adminInviteTTL; diff < -ttlTolerance || diff > ttlTolerance {
		t.Errorf("resend expires_at - now() = %v, want %v (+/- %v)", gotExpiresIn, adminInviteTTL, ttlTolerance)
	}

	// INVITE-03/04: the resend email itself must address the invitee with a
	// working (raw) accept link, not merely trigger *a* send.
	if provider.lastMessage.To != email {
		t.Errorf("resent email To = %q, want %q", provider.lastMessage.To, email)
	}
	newRawToken := extractAcceptToken(t, provider.lastMessage.TextBody)
	if newRawToken == oldRawToken {
		t.Error("resend email carries the same token as before resend, want a freshly-minted one")
	}
	newAcceptRec := postAcceptInvite(t, r, newRawToken, "correct-horse-battery-staple")
	if newAcceptRec.Code != http.StatusCreated {
		t.Errorf("accepting the resend email's token status = %d, want %d - body = %s", newAcceptRec.Code, http.StatusCreated, newAcceptRec.Body.String())
	}

	var gotActorID, gotAction string
	row := pool.QueryRow(context.Background(),
		"SELECT actor_id, action FROM admin_audit_log WHERE target_id = $1 AND action = 'resent'", inviteID)
	if err := row.Scan(&gotActorID, &gotAction); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if gotActorID != inviter.ID {
		t.Errorf("admin_audit_log actor_id = %q, want %q", gotActorID, inviter.ID)
	}
}

func TestResendInvite_EmailSendFails_200WithEmailSentFalse(t *testing.T) {
	svc, provider := newTestEmailService(t)
	provider.sendErr = errors.New("provider: send failed")
	r, pool, admins, invites := newAdminsRouterWithEmail(t, svc)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token := issueTestSessionToken(t, admins)

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	rec := postResendInvite(t, r, token, inviteID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["email_sent"] != false {
		t.Errorf(`response["email_sent"] = %v, want false`, resp["email_sent"])
	}
}

func TestResendInvite_UnknownID_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postResendInvite(t, r, token, "00000000-0000-0000-0000-000000000000")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestResendInvite_AlreadyExpiredInvite_200_RefreshesExpiry covers spec.md
// P2 AC2: resending an invite whose expires_at has already passed behaves
// identically to resending a not-yet-expired one - new token, expires_at
// pushed back into the future - rather than being treated as unmanageable.
func TestResendInvite_AlreadyExpiredInvite_200_RefreshesExpiry(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token := issueTestSessionToken(t, admins)

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, -1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	rec := postResendInvite(t, r, token, inviteID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := latestInviteForEmail(t, pool, email)
	if !got.expiresAt.After(time.Now()) {
		t.Errorf("expires_at after resending an expired invite = %v, want in the future", got.expiresAt)
	}
}

func TestResendInvite_MalformedID_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postResendInvite(t, r, token, "not-a-uuid")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestResendInvite_AlreadyAccepted_404(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token := issueTestSessionToken(t, admins)

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id
	if err := invites.MarkUsed(context.Background(), inviteID); err != nil {
		t.Fatalf("invites.MarkUsed() returned unexpected error: %v", err)
	}

	rec := postResendInvite(t, r, token, inviteID)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestResendInvite_Concurrent_NoCorruption covers spec.md's "concurrent
// resend/resend" assumption: Refresh doesn't consume the invite (never sets
// used_at), so two concurrent resends are NOT required to be mutually
// exclusive like resend/cancel is - both may legitimately return 200. What
// must hold is no corruption: each request gets a definitive response, and
// exactly one of the two newly-issued tokens is left valid afterward
// (whichever UPDATE committed last), never both or neither.
func TestResendInvite_Concurrent_NoCorruption(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token, err := auth.IssueSession(inviter.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	for i := range 2 {
		go func(idx int) {
			defer wg.Done()
			codes[idx] = postResendInvite(t, r, token, inviteID).Code
		}(i)
	}
	wg.Wait()

	for _, code := range codes {
		if code != http.StatusOK {
			t.Errorf("concurrent resend returned unexpected status %d, want %d for both", code, http.StatusOK)
		}
	}

	var pendingCount int
	countRow := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM admin_invites WHERE id = $1 AND used_at IS NULL", inviteID)
	if err := countRow.Scan(&pendingCount); err != nil {
		t.Fatalf("querying admin_invites returned unexpected error: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("pending admin_invites rows for id %q after concurrent resend = %d, want 1 (single row, not duplicated/lost)", inviteID, pendingCount)
	}
}

func deleteCancelInvite(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/admins/invites/"+id, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCancelInvite_Owner_200_TokenRejectedAfterCancel(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token, err := auth.IssueSession(inviter.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	rawToken := createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	rec := deleteCancelInvite(t, r, token, inviteID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["status"] != "canceled" {
		t.Errorf(`response["status"] = %v, want "canceled"`, resp["status"])
	}

	acceptRec := postAcceptInvite(t, r, rawToken, "correct-horse-battery-staple")
	if acceptRec.Code != http.StatusUnauthorized {
		t.Errorf("accepting canceled invite status = %d, want %d", acceptRec.Code, http.StatusUnauthorized)
	}

	var gotActorID, gotAction string
	row := pool.QueryRow(context.Background(),
		"SELECT actor_id, action FROM admin_audit_log WHERE target_id = $1 AND action = 'canceled'", inviteID)
	if err := row.Scan(&gotActorID, &gotAction); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if gotActorID != inviter.ID {
		t.Errorf("admin_audit_log actor_id = %q, want %q", gotActorID, inviter.ID)
	}
}

func TestCancelInvite_MalformedID_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := deleteCancelInvite(t, r, token, "not-a-uuid")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCancelInvite_UnknownID_404(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := deleteCancelInvite(t, r, token, "00000000-0000-0000-0000-000000000000")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCancelInvite_AlreadyCanceled_404NoDuplicateAuditEntry(t *testing.T) {
	r, pool, admins, invites := newAdminsRouter(t)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })
	token, err := auth.IssueSession(inviter.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}

	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_invites WHERE email = $1", email) })
	createTestInvite(t, invites, inviter.ID, email, db.RoleOperator, 1*time.Hour)
	inviteID := latestInviteForEmail(t, pool, email).id

	first := deleteCancelInvite(t, r, token, inviteID)
	if first.Code != http.StatusOK {
		t.Fatalf("first cancel status = %d, want %d", first.Code, http.StatusOK)
	}

	second := deleteCancelInvite(t, r, token, inviteID)
	if second.Code != http.StatusNotFound {
		t.Errorf("second cancel status = %d, want %d", second.Code, http.StatusNotFound)
	}

	var canceledCount int
	countRow := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM admin_audit_log WHERE target_id = $1 AND action = 'canceled'", inviteID)
	if err := countRow.Scan(&canceledCount); err != nil {
		t.Fatalf("querying admin_audit_log returned unexpected error: %v", err)
	}
	if canceledCount != 1 {
		t.Errorf("canceled audit entries for invite %q = %d, want 1 (no duplicate on the failed second cancel)", inviteID, canceledCount)
	}
}

func TestListAdmins_Owner_200_ExcludesUsedIncludesExpiredInvites(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })

	usedEmail := uniqueTestEmail(t)
	usedInvite := &db.AdminInvite{
		Email: usedEmail, Role: db.RoleViewer, TokenHash: "hash-" + usedEmail,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := invites.Create(context.Background(), usedInvite); err != nil {
		t.Fatalf("invites.Create() returned unexpected error: %v", err)
	}
	if err := invites.MarkUsed(context.Background(), usedInvite.ID); err != nil {
		t.Fatalf("invites.MarkUsed() returned unexpected error: %v", err)
	}

	// Expired-but-unused invites stay listed (spec P2, INVITE-07) - only a
	// used_at (accepted or canceled) invite ever drops out of List().
	expiredEmail := uniqueTestEmail(t)
	createTestInvite(t, invites, inviter.ID, expiredEmail, db.RoleViewer, -1*time.Hour)

	rec := getAdminsList(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if used := findAdminAcrossPages(t, r, token, usedEmail); used != nil {
		t.Errorf("list included used invite %q, want excluded", usedEmail)
	}

	expired := findAdminAcrossPages(t, r, token, expiredEmail)
	if expired == nil {
		t.Fatalf("list did not include expired-but-unused invite %q, want included", expiredEmail)
	}
	if !expired.Expired {
		t.Errorf("expired invite %q has Expired = false, want true", expiredEmail)
	}
}

func TestListAdmins_Owner_200_PendingNotYetExpired_ExpiredFalse(t *testing.T) {
	r, _, admins, invites := newAdminsRouter(t)
	token := issueTestSessionToken(t, admins)
	inviter := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(context.Background(), inviter); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), inviter.ID) })

	pendingEmail := uniqueTestEmail(t)
	createTestInvite(t, invites, inviter.ID, pendingEmail, db.RoleViewer, 1*time.Hour)

	rec := getAdminsList(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	pending := findAdminAcrossPages(t, r, token, pendingEmail)
	if pending == nil {
		t.Fatalf("list did not include pending invite %q", pendingEmail)
	}
	if pending.Expired {
		t.Errorf("not-yet-expired invite %q has Expired = true, want false", pendingEmail)
	}
}

func TestListAdmins_Operator_403(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleOperator)

	rec := getAdminsList(t, r, token)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestListAdmins_Viewer_403(t *testing.T) {
	r, _, admins, _ := newAdminsRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := getAdminsList(t, r, token)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
