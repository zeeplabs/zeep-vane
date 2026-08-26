//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newPasswordResetRouter(t *testing.T) (http.Handler, *db.AdminRepository, *db.Pool, *observer.ObservedLogs) {
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

	observedCore, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observedCore)

	// devTokenLogging=true: this file's tests need the raw token back out
	// of the log to drive Confirm, mirroring an operator who has opted
	// into VANE_DEV_TOKEN_LOGGING for local use. The off-by-default case
	// is covered separately by TestRequest_DevTokenLoggingDisabled_TokenNotLogged.
	handler := NewPasswordResetHandler(admins, tokens, logger, true)

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
	observedCore, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observedCore)

	handler := NewPasswordResetHandler(admins, tokens, logger, false)
	r := chi.NewRouter()
	r.Post("/api/auth/password-reset/request", handler.Request)

	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: email})
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
	r, admins, pool, _ := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	before := time.Now()
	rec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: email})
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
	r, admins, pool, logs := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	reqRec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: email})
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

func TestPasswordResetConfirm_ExpiredToken_Rejected(t *testing.T) {
	r, admins, pool, _ := newPasswordResetRouter(t)
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
	r, admins, pool, logs := newPasswordResetRouter(t)
	email := uniqueTestEmail(t)
	createTestAdmin(t, admins, pool, email, "old-password")

	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id IN (SELECT id FROM admins WHERE email = $1)", email)
	})

	reqRec := postJSON(t, r, "/api/auth/password-reset/request", passwordResetRequestBody{Email: email})
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
