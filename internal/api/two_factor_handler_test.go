//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/pquerna/otp/totp"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// newTwoFactorRouter builds a router exposing the 2FA enroll/confirm routes
// behind RequireAuth, backed by a fresh TwoFactorRepository.
func newTwoFactorRouter(t *testing.T) (http.Handler, *db.UserRepository, *db.TwoFactorRepository, *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)
	dbtest.LockUsersTable(t, context.Background(), dsn)

	users := db.NewUserRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	twoFactor := db.NewTwoFactorRepository(pool)
	handler := NewAuthHandler(users, memberships, twoFactor, db.NewTwoFactorChallengeRepository(pool), pool, zap.NewNop(), testSessionSecret, true, testMasterKey)

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(testSessionSecret, users))
		protected.Post("/api/auth/2fa/enroll", handler.Enroll)
		protected.Post("/api/auth/2fa/confirm", handler.Confirm2FA)
		protected.Post("/api/auth/2fa/disable", handler.Disable2FA)
	})

	return r, users, twoFactor, pool
}

// authedRequest builds req, authenticated as userID via a signed session
// token in the Authorization header (RequireAuth accepts either the cookie
// or a bearer token - see middleware.go).
func authedRequest(t *testing.T, method, target string, body []byte, userID string) *http.Request {
	t.Helper()
	token, err := auth.IssueSession(userID, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func createTwoFactorTestUser(t *testing.T, users *db.UserRepository, pool *db.Pool) *db.User {
	t.Helper()
	ctx := context.Background()
	email := uniqueTestEmail(t)
	hash, err := auth.HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword() returned unexpected error: %v", err)
	}
	user := &db.User{Email: email, PasswordHash: hash}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })
	return user
}

func TestEnroll_FirstTime_200WithOtpauthURIAndSecret(t *testing.T) {
	r, users, _, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Secret == "" {
		t.Error("response has empty secret, want a non-empty base32 secret")
	}
	if body.OtpauthURI == "" {
		t.Error("response has empty otpauth_uri, want a non-empty otpauth:// URI")
	}
}

func TestEnroll_CalledTwiceBeforeConfirm_OverwritesPendingSecret(t *testing.T) {
	r, users, _, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	var body1 enrollResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second enroll status = %d, want %d", rec2.Code, http.StatusOK)
	}
	var body2 enrollResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if body1.Secret == body2.Secret {
		t.Error("second enroll returned the same secret as the first, want a fresh overwrite (spec.md Edge Cases: no second valid pending secret left behind)")
	}
}

func TestEnroll_AlreadyEnabled_409NoMutation(t *testing.T) {
	r, users, twoFactor, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	// Drive the user into an enabled state directly via the repository
	// (T4, already committed) - the HTTP confirm endpoint is T7, not yet
	// built in this task.
	if err := twoFactor.CreatePendingSecret(context.Background(), user.ID, []byte("ciphertext")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := twoFactor.ConfirmSecret(context.Background(), user.ID); err != nil {
		t.Fatalf("ConfirmSecret() returned unexpected error: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}

	// No row mutated: the confirmed secret must be exactly as it was.
	got, err := twoFactor.GetSecret(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if string(got.EncryptedSecret) != "ciphertext" {
		t.Errorf("EncryptedSecret = %q, want unchanged %q after a 409 enroll attempt", got.EncryptedSecret, "ciphertext")
	}
	if got.EnabledAt == nil {
		t.Error("EnabledAt = nil, want still set after a 409 enroll attempt")
	}
}

func TestConfirm2FA_CorrectCode_200WithTenRecoveryCodesAndEnabled(t *testing.T) {
	r, users, _, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	var enrolled enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &enrolled); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	code, err := totp.GenerateCode(enrolled.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}
	confirmBody, _ := json.Marshal(confirm2FARequest{Code: code})

	confirmRec := httptest.NewRecorder()
	r.ServeHTTP(confirmRec, authedRequest(t, http.MethodPost, "/api/auth/2fa/confirm", confirmBody, user.ID))
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}

	var body confirm2FAResponse
	if err := json.Unmarshal(confirmRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(body.RecoveryCodes) != recoveryCodeCount {
		t.Errorf("len(RecoveryCodes) = %d, want %d", len(body.RecoveryCodes), recoveryCodeCount)
	}
	seen := map[string]bool{}
	for _, c := range body.RecoveryCodes {
		if c == "" {
			t.Error("a recovery code is empty, want a non-empty plaintext code")
		}
		if seen[c] {
			t.Errorf("recovery code %q appeared more than once, want all 10 distinct", c)
		}
		seen[c] = true
	}
}

func TestConfirm2FA_WrongCode_422NoEnableNoRecoveryCodes(t *testing.T) {
	r, users, twoFactor, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	var enrolled enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &enrolled); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	confirmBody, _ := json.Marshal(confirm2FARequest{Code: "000000"})
	confirmRec := httptest.NewRecorder()
	r.ServeHTTP(confirmRec, authedRequest(t, http.MethodPost, "/api/auth/2fa/confirm", confirmBody, user.ID))
	if confirmRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", confirmRec.Code, http.StatusUnprocessableEntity, confirmRec.Body.String())
	}

	// enabled_at must still be NULL, and no recovery codes generated: a
	// direct repository check confirms neither mutation happened.
	got, err := twoFactor.GetSecret(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if got.EnabledAt != nil {
		t.Error("EnabledAt is set after a wrong confirm code, want nil")
	}
	ok, err := twoFactor.ConsumeRecoveryCode(context.Background(), user.ID, "any-code")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if ok {
		t.Error("ConsumeRecoveryCode() found a match after a wrong confirm code, want none generated")
	}

	// A subsequent confirm with the correct code must still succeed.
	code, err := totp.GenerateCode(enrolled.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}
	retryBody, _ := json.Marshal(confirm2FARequest{Code: code})
	retryRec := httptest.NewRecorder()
	r.ServeHTTP(retryRec, authedRequest(t, http.MethodPost, "/api/auth/2fa/confirm", retryBody, user.ID))
	if retryRec.Code != http.StatusOK {
		t.Fatalf("retry with the correct code status = %d, want %d, body = %s", retryRec.Code, http.StatusOK, retryRec.Body.String())
	}
}

func postDisable2FA(t *testing.T, r http.Handler, userID, currentPassword string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(disable2FARequest{CurrentPassword: currentPassword})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/disable", body, userID))
	return rec
}

// TestDisable2FA_CorrectPassword_200ClearsSecretAndRecoveryCodes proves
// TOTP-12: a subsequent login for that user no longer requires 2FA.
func TestDisable2FA_CorrectPassword_200ClearsSecretAndRecoveryCodes(t *testing.T) {
	r, users, twoFactor, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authedRequest(t, http.MethodPost, "/api/auth/2fa/enroll", nil, user.ID))
	var enrolled enrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &enrolled); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	code, err := totp.GenerateCode(enrolled.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode() returned unexpected error: %v", err)
	}
	confirmBody, _ := json.Marshal(confirm2FARequest{Code: code})
	confirmRec := httptest.NewRecorder()
	r.ServeHTTP(confirmRec, authedRequest(t, http.MethodPost, "/api/auth/2fa/confirm", confirmBody, user.ID))
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d, body = %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}

	disableRec := postDisable2FA(t, r, user.ID, "correct-horse-battery-staple")
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d, body = %s", disableRec.Code, http.StatusOK, disableRec.Body.String())
	}

	secret, err := twoFactor.GetSecret(context.Background(), user.ID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetSecret() after disable = (%+v, %v), want (nil, db.ErrNotFound)", secret, err)
	}
	stillMatches, err := twoFactor.ConsumeRecoveryCode(context.Background(), user.ID, "any-code")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if stillMatches {
		t.Error("ConsumeRecoveryCode() found a match after disable, want every recovery code deleted")
	}
}

// TestDisable2FA_WrongPassword_401TwoFactorRemainsEnabled proves TOTP-13.
func TestDisable2FA_WrongPassword_401TwoFactorRemainsEnabled(t *testing.T) {
	r, users, twoFactor, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	if err := twoFactor.CreatePendingSecret(context.Background(), user.ID, []byte("ciphertext")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := twoFactor.ConfirmSecret(context.Background(), user.ID); err != nil {
		t.Fatalf("ConfirmSecret() returned unexpected error: %v", err)
	}

	rec := postDisable2FA(t, r, user.ID, "totally-wrong-password")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	got, err := twoFactor.GetSecret(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if got.EnabledAt == nil {
		t.Error("EnabledAt = nil after a wrong-password disable attempt, want still enabled")
	}
}

// TestDisable2FA_NoTwoFactorEnabled_200Noop proves TOTP-13's no-op branch.
func TestDisable2FA_NoTwoFactorEnabled_200Noop(t *testing.T) {
	r, users, _, pool := newTwoFactorRouter(t)
	user := createTwoFactorTestUser(t, users, pool)

	rec := postDisable2FA(t, r, user.ID, "correct-horse-battery-staple")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
