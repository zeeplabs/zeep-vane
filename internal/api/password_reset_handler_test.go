//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// newPasswordResetRouter also returns the *fakeEmailProvider backing the
// router's email service, pre-armed with a buffered notifySent channel: a
// test must waitForSend after posting to /request before reading anything
// issueAndSendPasswordReset writes (the reset token, its log entry) - that
// work all happens in a goroutine dispatched after Request has already
// responded (see issueAndSendPasswordReset's doc comment), so there is no
// other synchronization point.
func newPasswordResetRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool, *observer.ObservedLogs, *fakeEmailProvider) {
	t.Helper()
	emailSvc, provider := newTestEmailService(t)
	provider.notifySent = make(chan struct{}, 1)
	r, admins, pool, logs := newPasswordResetRouterWithEmail(t, emailSvc)
	return r, admins, pool, logs, provider
}

// requestPasswordReset posts to /api/auth/password-reset/request and blocks
// until issueAndSendPasswordReset's goroutine has run to completion (see
// newPasswordResetRouter), so it's safe for the caller to read the token
// from logs or the database immediately after this returns.
func requestPasswordReset(t *testing.T, r http.Handler, provider *fakeEmailProvider, email string) *httptest.ResponseRecorder {
	t.Helper()
	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: email})
	waitForSend(t, provider.notifySent)
	return rec
}

// newPasswordResetRouterWithEmail is newPasswordResetRouter with the email
// service as a parameter, so a test can supply one wired to a
// *fakeEmailProvider it can inspect (e.g. with notifySent set) - same
// split as newAdminsRouterWithEmail/newAdminsRouter in admins_test.go.
func newPasswordResetRouterWithEmail(t *testing.T, emailSvc *email.Service) (http.Handler, *db.AdminRepository, *db.Pool, *observer.ObservedLogs) {
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

	admins := db.NewAdminRepository(pool)
	tokens := db.NewPasswordResetRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)

	observedCore, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observedCore)

	// devTokenLogging=true: this file's tests need the raw token back out
	// of the log to drive Confirm, mirroring an operator who has opted
	// into VANE_DEV_TOKEN_LOGGING for local use. The off-by-default case
	// is covered separately by TestRequest_DevTokenLoggingDisabled_TokenNotLogged.
	handler := NewPasswordResetHandler(admins, tokens, emailSvc, companySettings, logger, true, testAdminBaseURL)

	r := chi.NewRouter()
	r.Post("/api/auth/password-reset/request", handler.Request)
	r.Post("/api/auth/password-reset/confirm", handler.Confirm)

	return r, admins, pool, observedLogs
}

// rawTokenFromLogs extracts the raw reset token the handler logged as a
// stand-in for real email delivery.
func rawTokenFromLogs(t *testing.T, logs *observer.ObservedLogs) string {
	t.Helper()
	for _, entry := range logs.All() {
		for _, field := range entry.Context {
			if field.Key == "token" {
				return field.String
			}
		}
	}
	t.Fatal("no logged reset token found")
	return ""
}

func postJSON(t *testing.T, r http.Handler, path string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestPasswordResetRequest_DevTokenLoggingDisabled_TokenNotLogged asserts
// the secure default: with devTokenLogging=false (VANE_DEV_TOKEN_LOGGING
// unset), the raw reset token - a bearer credential for account takeover -
// never reaches the log, even though a log line is still emitted.
func TestPasswordResetRequest_DevTokenLoggingDisabled_TokenNotLogged(t *testing.T) {
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

	admins := db.NewAdminRepository(pool)
	tokens := db.NewPasswordResetRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	emailSvc, provider := newTestEmailService(t)
	provider.notifySent = make(chan struct{}, 1)
	observedCore, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observedCore)

	handler := NewPasswordResetHandler(admins, tokens, emailSvc, companySettings, logger, false, testAdminBaseURL)
	r := chi.NewRouter()
	r.Post("/api/auth/password-reset/request", handler.Request)

	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	rec := requestPasswordReset(t, r, provider, email)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	found := false
	for _, entry := range observedLogs.All() {
		for _, field := range entry.Context {
			if field.Key == "token" {
				found = true
			}
		}
	}
	if found {
		t.Error("log entry contains a \"token\" field with devTokenLogging=false, want token never logged")
	}
	if observedLogs.Len() == 0 {
		t.Error("no log entry emitted for password-reset request, want an admin_id-only entry")
	}
}

func TestPasswordResetRequest_GeneratesTokenWithOneHourExpiry(t *testing.T) {
	r, admins, pool, _, provider := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	before := time.Now()
	rec := requestPasswordReset(t, r, provider, email)
	after := time.Now()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ctx := context.Background()
	var expiresAt time.Time
	err := pool.QueryRow(ctx,
		`SELECT prt.expires_at FROM password_reset_tokens prt
		 JOIN admins a ON a.id = prt.admin_id WHERE a.email = $1`, email,
	).Scan(&expiresAt)
	if err != nil {
		t.Fatalf("querying password_reset_tokens returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	wantMin := before.Add(resetTokenTTL)
	wantMax := after.Add(resetTokenTTL)
	if expiresAt.Before(wantMin) || expiresAt.After(wantMax) {
		t.Errorf("expires_at = %v, want between %v and %v (1h from request time)", expiresAt, wantMin, wantMax)
	}
}

func TestPasswordResetConfirm_ValidUnexpiredToken_ChangesPassword(t *testing.T) {
	r, admins, pool, logs, provider := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	reqRec := requestPasswordReset(t, r, provider, email)
	if reqRec.Code != http.StatusOK {
		t.Fatalf("request status = %d, want %d", reqRec.Code, http.StatusOK)
	}
	rawToken := rawTokenFromLogs(t, logs)

	confirmRec := postJSON(t, r, "/api/auth/password-reset/confirm", passwordResetConfirmBody{
		Token: rawToken, NewPassword: "new-password",
	})
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d", confirmRec.Code, http.StatusOK)
	}

	updated, err := admins.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(updated.PasswordHash, "new-password") {
		t.Error("VerifyPassword(new password) = false, want true after confirm")
	}
	if auth.VerifyPassword(updated.PasswordHash, "old-password") {
		t.Error("VerifyPassword(old password) = true, want false after confirm")
	}
}

// TestPasswordResetConfirm_WeakPassword_422NoChange is the H11 regression
// guard: a password below auth.MinPasswordLength must be rejected before
// the admin's password hash is updated, even with a valid unexpired token.
func TestPasswordResetConfirm_WeakPassword_422NoChange(t *testing.T) {
	r, admins, pool, logs, provider := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	reqRec := requestPasswordReset(t, r, provider, email)
	if reqRec.Code != http.StatusOK {
		t.Fatalf("request status = %d, want %d", reqRec.Code, http.StatusOK)
	}
	rawToken := rawTokenFromLogs(t, logs)

	confirmRec := postJSON(t, r, "/api/auth/password-reset/confirm", passwordResetConfirmBody{
		Token: rawToken, NewPassword: "1234567",
	})
	if confirmRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("confirm status = %d, want %d", confirmRec.Code, http.StatusUnprocessableEntity)
	}

	unchanged, err := admins.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if !auth.VerifyPassword(unchanged.PasswordHash, "old-password") {
		t.Error("password was changed despite a weak new password, want unchanged")
	}
}

func TestPasswordResetConfirm_ExpiredToken_Rejected(t *testing.T) {
	r, admins, pool, _, _ := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	admin, err := admins.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}

	tokens := db.NewPasswordResetRepository(pool)
	rawToken := "expired-raw-token-" + email
	resetToken := &db.PasswordResetToken{
		AdminID:   admin.ID,
		TokenHash: hashResetToken(rawToken),
		ExpiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}
	if err := tokens.Create(ctx, resetToken); err != nil {
		t.Fatalf("tokens.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE id = $1", resetToken.ID) })

	confirmRec := postJSON(t, r, "/api/auth/password-reset/confirm", passwordResetConfirmBody{
		Token: rawToken, NewPassword: "new-password",
	})

	if confirmRec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", confirmRec.Code, http.StatusUnauthorized)
	}
	if confirmRec.Body.String() != resetConfirmErrorBody {
		t.Errorf("body = %q, want %q", confirmRec.Body.String(), resetConfirmErrorBody)
	}

	unchanged, err := admins.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if auth.VerifyPassword(unchanged.PasswordHash, "new-password") {
		t.Error("password was changed despite an expired token, want unchanged")
	}
}

func TestPasswordResetConfirm_AlreadyUsedToken_Rejected(t *testing.T) {
	r, admins, pool, logs, provider := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	reqRec := requestPasswordReset(t, r, provider, email)
	if reqRec.Code != http.StatusOK {
		t.Fatalf("request status = %d, want %d", reqRec.Code, http.StatusOK)
	}
	rawToken := rawTokenFromLogs(t, logs)

	firstConfirm := postJSON(t, r, "/api/auth/password-reset/confirm", passwordResetConfirmBody{
		Token: rawToken, NewPassword: "new-password",
	})
	if firstConfirm.Code != http.StatusOK {
		t.Fatalf("first confirm status = %d, want %d", firstConfirm.Code, http.StatusOK)
	}

	secondConfirm := postJSON(t, r, "/api/auth/password-reset/confirm", passwordResetConfirmBody{
		Token: rawToken, NewPassword: "yet-another-password",
	})

	if secondConfirm.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", secondConfirm.Code, http.StatusUnauthorized)
	}
	if secondConfirm.Body.String() != resetConfirmErrorBody {
		t.Errorf("body = %q, want %q", secondConfirm.Body.String(), resetConfirmErrorBody)
	}

	admin, err := admins.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if auth.VerifyPassword(admin.PasswordHash, "yet-another-password") {
		t.Error("password was changed by a reused token, want unchanged from first confirm")
	}
}

// waitForSend blocks until fakeEmailProvider.Send has run once (signaled on
// ch, see fakeEmailProvider.notifySent), or fails the test after 2s -
// Request dispatches the send in its own goroutine (F2: response time must
// not depend on email delivery), so a test can't just read
// provider.lastMessage/sendCalls immediately after the HTTP call returns.
func waitForSend(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for password-reset email send")
	}
}

// TestPasswordResetRequest_SendsEmailWithResetURL is the F3 regression
// guard for the email actually being sent at all (previously Request only
// logged the raw token - see the package doc history) and for the reset
// link being built from the configured admin base URL rather than the
// request's Host header (F1: an attacker-controlled Host must never reach
// an emailed link).
func TestPasswordResetRequest_SendsEmailWithResetURL(t *testing.T) {
	emailSvc, provider := newTestEmailService(t)
	provider.notifySent = make(chan struct{}, 1)
	r, admins, pool, logs := newPasswordResetRouterWithEmail(t, emailSvc)
	adminEmail := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, adminEmail, "old-password")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", adminEmail)
	})

	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: adminEmail})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	waitForSend(t, provider.notifySent)

	if provider.lastMessage.To != adminEmail {
		t.Errorf("sent email To = %q, want %q", provider.lastMessage.To, adminEmail)
	}
	rawToken := rawTokenFromLogs(t, logs)
	wantURL := testAdminBaseURL + "/reset-password/" + rawToken
	if !strings.Contains(provider.lastMessage.TextBody, wantURL) {
		t.Errorf("sent email TextBody = %q, want it to contain reset URL %q", provider.lastMessage.TextBody, wantURL)
	}
	if strings.Contains(provider.lastMessage.TextBody, "vane-admin-base-url-not-configured") {
		t.Error("sent email TextBody used the unconfigured-base-URL placeholder despite testAdminBaseURL being set")
	}
}

// TestPasswordResetRequest_EmailSendFails_StillReturns200 is the F2/F3
// regression guard: a send failure must be logged and otherwise invisible
// to the caller, same account-enumeration-protection reasoning as an
// unknown email - the two cases must be indistinguishable from the
// response alone.
func TestPasswordResetRequest_EmailSendFails_StillReturns200(t *testing.T) {
	emailSvc, provider := newTestEmailService(t)
	provider.notifySent = make(chan struct{}, 1)
	provider.sendErr = errors.New("provider: send failed")
	r, admins, pool, _ := newPasswordResetRouterWithEmail(t, emailSvc)
	adminEmail := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, adminEmail, "old-password")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", adminEmail)
	})

	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: adminEmail})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", rec.Body.String(), `{"status":"ok"}`)
	}

	// Confirms the send was actually attempted (and failed), rather than
	// this test passing vacuously because nothing ever tried to send.
	waitForSend(t, provider.notifySent)
	if provider.sendCalls != 1 {
		t.Errorf("provider.sendCalls = %d, want 1", provider.sendCalls)
	}
}

// TestPasswordResetRequest_NoActiveEmailProvider_StillReturns200 covers the
// self-hosted-with-no-email-configured-yet case: SendPasswordReset returns
// email.ErrNoActiveProvider before ever building a Provider, and Request's
// response must be identical to every other case regardless.
func TestPasswordResetRequest_NoActiveEmailProvider_StillReturns200(t *testing.T) {
	emailSvc := newTestEmailServiceNoActiveProvider(t)
	r, admins, pool, _ := newPasswordResetRouterWithEmail(t, emailSvc)
	adminEmail := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, adminEmail, "old-password")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", adminEmail)
	})

	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: adminEmail})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", rec.Body.String(), `{"status":"ok"}`)
	}
}
