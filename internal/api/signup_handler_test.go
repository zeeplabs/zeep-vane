//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
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

	r := chi.NewRouter()
	r.Post("/api/signup", signupHandler.Signup)

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
