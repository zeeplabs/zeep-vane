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

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// newSignupRouterWithEmail builds a router exercising Signup, backed by
// emailSvc.
func newSignupRouterWithEmail(t *testing.T, emailSvc *email.Service) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()
	dsn := testDatabaseURL(t)
	pool, _ := newAPITenantScopedPool(t)
	dbtest.LockUsersTable(t, context.Background(), dsn)

	users := db.NewUserRepository(pool)
	tenants := db.NewTenantRepository(pool)
	memberships := db.NewTenantMembershipRepository(pool)
	verifications := db.NewEmailVerificationRepository(pool)

	signupHandler := NewSignupHandler(pool, users, tenants, memberships, verifications, emailSvc, zap.NewNop(), false, testAdminBaseURL)
	authHandler := NewAuthHandler(users, memberships, db.NewTwoFactorRepository(pool), pool, zap.NewNop(), testSessionSecret, true, testMasterKey)

	r := chi.NewRouter()
	r.Post("/api/signup", signupHandler.Signup)
	r.Get("/api/signup/verify/{token}", signupHandler.Verify)
	r.Post("/api/signup/resend-verification", signupHandler.ResendVerification)
	r.Post("/api/auth/login", authHandler.Login)

	return r, pool, users
}

// newSignupRouter builds a router with a default working email service
// (active provider connected).
func newSignupRouter(t *testing.T) (http.Handler, *db.Pool, *db.UserRepository, *fakeEmailProvider) {
	t.Helper()
	svc, provider := newTestEmailService(t)
	r, pool, users := newSignupRouterWithEmail(t, svc)
	return r, pool, users, provider
}

// cleanupSignupTestData registers cleanup for every tenant the user with
// email owns (found via tenant_memberships) plus the user row itself -
// Signup creates a fresh tenant id per call, so the test can't know it in
// advance the way most other fixtures in this package do.
func cleanupSignupTestData(t *testing.T, pool *db.Pool, email string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		rows, err := pool.Query(ctx,
			`SELECT tm.tenant_id FROM tenant_memberships tm JOIN users u ON u.id = tm.user_id WHERE u.email = $1`, email)
		if err == nil {
			var tenantIDs []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					tenantIDs = append(tenantIDs, id)
				}
			}
			rows.Close()
			for _, id := range tenantIDs {
				_, _ = pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", id)
			}
		}
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})
}

func postSignup(t *testing.T, r http.Handler, email, password, tenantName string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(signupRequest{Email: email, Password: password, TenantName: tenantName})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getVerify(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/signup/verify/"+token, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// extractVerifyToken pulls the raw verification token out of a VerifyURL
// embedded in a sent email's TextBody ("...://.../verify-email/<token>").
func extractVerifyToken(t *testing.T, textBody string) string {
	t.Helper()
	const marker = "/verify-email/"
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

func postResendVerification(t *testing.T, r http.Handler, email string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(resendVerificationRequest{Email: email})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/signup/resend-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// --- T9: POST /api/signup ---

func TestSignup_NewEmail_201_CreatesTenantUserMembershipUnverifiedSendsEmail(t *testing.T) {
	r, pool, users, provider := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	rec := postSignup(t, r, email, "correct-horse-battery-staple", "Acme Inc")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp signupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Status != "pending_verification" {
		t.Errorf("status = %q, want %q", resp.Status, "pending_verification")
	}
	if !resp.EmailSent {
		t.Error("email_sent = false, want true (active provider connected)")
	}

	created, err := users.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if created.EmailVerifiedAt != nil {
		t.Errorf("EmailVerifiedAt = %v, want nil (unverified until the link is followed)", created.EmailVerifiedAt)
	}

	var tenantName, role string
	row := pool.QueryRow(context.Background(),
		`SELECT t.name, tm.role FROM tenant_memberships tm JOIN tenants t ON t.id = tm.tenant_id WHERE tm.user_id = $1`, created.ID)
	if err := row.Scan(&tenantName, &role); err != nil {
		t.Fatalf("querying created tenant/membership returned unexpected error: %v", err)
	}
	if tenantName != "Acme Inc" {
		t.Errorf("tenant name = %q, want %q", tenantName, "Acme Inc")
	}
	if role != db.RoleOwner {
		t.Errorf("membership role = %q, want %q", role, db.RoleOwner)
	}

	if provider.lastMessage.To != email {
		t.Errorf("sent email To = %q, want %q", provider.lastMessage.To, email)
	}
	if !strings.Contains(provider.lastMessage.TextBody, "/verify-email/") {
		t.Errorf("sent email TextBody = %q, want it to contain a verify-email link", provider.lastMessage.TextBody)
	}
}

func TestSignup_ExistingVerifiedEmail_201_CreatesNewTenantMembershipNoPasswordRequired(t *testing.T) {
	r, pool, users, provider := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	verifiedAt := time.Now()
	existing := &db.User{Email: email, PasswordHash: "existing-hash", EmailVerifiedAt: &verifiedAt}
	if err := users.Create(context.Background(), existing); err != nil {
		t.Fatalf("users.Create() returned unexpected error: %v", err)
	}

	rec := postSignup(t, r, email, "irrelevant-password", "Second Co")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp signupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Status != "created" {
		t.Errorf("status = %q, want %q", resp.Status, "created")
	}

	var userCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM users WHERE email = $1", email).Scan(&userCount); err != nil {
		t.Fatalf("counting users returned unexpected error: %v", err)
	}
	if userCount != 1 {
		t.Errorf("users rows for %q = %d, want 1 (no duplicate)", email, userCount)
	}

	var membershipCount int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM tenant_memberships WHERE user_id = $1", existing.ID).Scan(&membershipCount); err != nil {
		t.Fatalf("counting memberships returned unexpected error: %v", err)
	}
	if membershipCount != 1 {
		t.Errorf("tenant_memberships rows for existing user = %d, want 1 (new tenant only, this was their first)", membershipCount)
	}

	if provider.sendCalls != 0 {
		t.Errorf("provider.sendCalls = %d, want 0 (already-verified user needs no verification email)", provider.sendCalls)
	}
}

func TestSignup_SameUnverifiedEmailRetried_409_NoSecondTenant(t *testing.T) {
	r, pool, users, _ := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	first := postSignup(t, r, email, "correct-horse-battery-staple", "First Co")
	if first.Code != http.StatusCreated {
		t.Fatalf("first signup status = %d, want %d, body = %s", first.Code, http.StatusCreated, first.Body.String())
	}

	second := postSignup(t, r, email, "another-strong-password", "Second Co")
	if second.Code != http.StatusConflict {
		t.Fatalf("second signup status = %d, want %d, body = %s", second.Code, http.StatusConflict, second.Body.String())
	}
	if second.Body.String() != signupPendingVerificationBody {
		t.Errorf("body = %q, want %q", second.Body.String(), signupPendingVerificationBody)
	}

	created, err := users.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	var membershipCount int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM tenant_memberships WHERE user_id = $1", created.ID).Scan(&membershipCount); err != nil {
		t.Fatalf("counting memberships returned unexpected error: %v", err)
	}
	if membershipCount != 1 {
		t.Errorf("tenant_memberships rows = %d, want 1 (no second tenant created)", membershipCount)
	}
}

func TestSignup_MissingFields_422(t *testing.T) {
	r, _, _, _ := newSignupRouter(t)

	rec := postSignup(t, r, uniqueTestEmail(t), "correct-horse-battery-staple", "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestSignup_WeakPassword_422_NoUserCreated(t *testing.T) {
	r, _, users, _ := newSignupRouter(t)
	email := uniqueTestEmail(t)

	rec := postSignup(t, r, email, "short", "Acme Inc")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if _, err := users.GetByEmail(context.Background(), email); err == nil {
		t.Error("GetByEmail() found a user, want none created for a weak-password-rejected signup")
	}
}

// --- T10: email verification gates login ---

func TestSignupVerify_ValidToken_200_MarksVerifiedAllowsLogin(t *testing.T) {
	r, pool, users, provider := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	signupRec := postSignup(t, r, email, "correct-horse-battery-staple", "Acme Inc")
	if signupRec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, want %d", signupRec.Code, http.StatusCreated)
	}
	rawToken := extractVerifyToken(t, provider.lastMessage.TextBody)

	verifyRec := getVerify(t, r, rawToken)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want %d, body = %s", verifyRec.Code, http.StatusOK, verifyRec.Body.String())
	}

	created, err := users.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if created.EmailVerifiedAt == nil {
		t.Fatal("EmailVerifiedAt = nil after verify, want a timestamp")
	}

	loginRec := postLogin(t, r, email, "correct-horse-battery-staple")
	if loginRec.Code != http.StatusOK {
		t.Errorf("login after verification status = %d, want %d, body = %s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}
}

func TestSignupVerify_ExpiredToken_401_NoChange(t *testing.T) {
	r, pool, users, _ := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	user := &db.User{Email: email, PasswordHash: "hash"}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("users.Create() returned unexpected error: %v", err)
	}

	verifications := db.NewEmailVerificationRepository(pool)
	rawToken := "expired-raw-token-" + email
	token := &db.EmailVerificationToken{
		UserID:    user.ID,
		TokenHash: hashAdminInviteToken(rawToken),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := verifications.Create(context.Background(), token); err != nil {
		t.Fatalf("verifications.Create() returned unexpected error: %v", err)
	}

	rec := getVerify(t, r, rawToken)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Body.String() != verifyTokenErrorBody {
		t.Errorf("body = %q, want %q", rec.Body.String(), verifyTokenErrorBody)
	}

	got, err := users.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}
	if got.EmailVerifiedAt != nil {
		t.Errorf("EmailVerifiedAt = %v after rejected expired token, want nil (no state change)", got.EmailVerifiedAt)
	}
}

func TestSignupVerify_InvalidToken_401(t *testing.T) {
	r, _, _, _ := newSignupRouter(t)

	rec := getVerify(t, r, "not-a-real-token")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Body.String() != verifyTokenErrorBody {
		t.Errorf("body = %q, want %q", rec.Body.String(), verifyTokenErrorBody)
	}
}

func TestLogin_UnverifiedEmail_403_ClearMessage(t *testing.T) {
	r, pool, _, _ := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	signupRec := postSignup(t, r, email, "correct-horse-battery-staple", "Acme Inc")
	if signupRec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, want %d", signupRec.Code, http.StatusCreated)
	}

	loginRec := postLogin(t, r, email, "correct-horse-battery-staple")
	if loginRec.Code != http.StatusForbidden {
		t.Fatalf("login status = %d, want %d, body = %s", loginRec.Code, http.StatusForbidden, loginRec.Body.String())
	}
	if loginRec.Body.String() != emailNotVerifiedBody {
		t.Errorf("body = %q, want %q", loginRec.Body.String(), emailNotVerifiedBody)
	}
}

// --- T11: resend verification email ---

func TestResendVerification_Success_NewTokenInvalidatesOld(t *testing.T) {
	r, pool, _, provider := newSignupRouter(t)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	signupRec := postSignup(t, r, email, "correct-horse-battery-staple", "Acme Inc")
	if signupRec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, want %d", signupRec.Code, http.StatusCreated)
	}
	oldToken := extractVerifyToken(t, provider.lastMessage.TextBody)

	resendRec := postResendVerification(t, r, email)
	if resendRec.Code != http.StatusOK {
		t.Fatalf("resend status = %d, want %d, body = %s", resendRec.Code, http.StatusOK, resendRec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(resendRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp["email_sent"] != true {
		t.Errorf(`response["email_sent"] = %v, want true`, resp["email_sent"])
	}
	newToken := extractVerifyToken(t, provider.lastMessage.TextBody)
	if newToken == oldToken {
		t.Fatal("resend produced the same token as the original signup, want a fresh one")
	}

	if rec := getVerify(t, r, oldToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("verify with old (invalidated) token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec := getVerify(t, r, newToken); rec.Code != http.StatusOK {
		t.Errorf("verify with new token status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestResendVerification_EmailSendFails_TenantUserMembershipIntact(t *testing.T) {
	svc, provider := newTestEmailService(t)
	provider.sendErr = errors.New("provider: send failed")
	r, pool, users := newSignupRouterWithEmail(t, svc)
	email := uniqueTestEmail(t)
	cleanupSignupTestData(t, pool, email)

	signupRec := postSignup(t, r, email, "correct-horse-battery-staple", "Acme Inc")
	if signupRec.Code != http.StatusCreated {
		t.Fatalf("signup status = %d, want %d, body = %s", signupRec.Code, http.StatusCreated, signupRec.Body.String())
	}
	var signupResp signupResponse
	if err := json.Unmarshal(signupRec.Body.Bytes(), &signupResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if signupResp.EmailSent {
		t.Fatal("signup email_sent = true, want false (provider configured to fail sends)")
	}

	resendRec := postResendVerification(t, r, email)
	if resendRec.Code != http.StatusOK {
		t.Fatalf("resend status = %d, want %d, body = %s", resendRec.Code, http.StatusOK, resendRec.Body.String())
	}
	var resendResp map[string]any
	if err := json.Unmarshal(resendRec.Body.Bytes(), &resendResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resendResp["email_sent"] != false {
		t.Errorf(`response["email_sent"] = %v, want false (send configured to fail)`, resendResp["email_sent"])
	}

	created, err := users.GetByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v (user/tenant/membership must survive a send failure)", err)
	}
	var membershipCount int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM tenant_memberships WHERE user_id = $1", created.ID).Scan(&membershipCount); err != nil {
		t.Fatalf("counting memberships returned unexpected error: %v", err)
	}
	if membershipCount != 1 {
		t.Errorf("tenant_memberships rows = %d, want 1 (tenant/membership from original signup intact)", membershipCount)
	}
}

func TestResendVerification_UnknownEmail_404(t *testing.T) {
	r, _, _, _ := newSignupRouter(t)

	rec := postResendVerification(t, r, uniqueTestEmail(t))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
