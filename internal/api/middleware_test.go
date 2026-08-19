package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zeeplabs/zeep-vane/internal/auth"
)

const middlewareTestSecret = "middleware-test-secret-32-bytes-long!!"

func newProtectedHandler(gotAdminID *string) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAdminID, _ = AdminIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return RequireAuth(middlewareTestSecret)(next)
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
	const adminID = "admin-42"
	token, err := auth.IssueSession(adminID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("IssueSession() returned unexpected error: %v", err)
	}

	var gotAdminID string
	handler := newProtectedHandler(&gotAdminID)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotAdminID != adminID {
		t.Errorf("admin ID stored in context = %q, want %q", gotAdminID, adminID)
	}
}

func TestRequireAuth_MissingToken_401(t *testing.T) {
	var gotAdminID string
	handler := newProtectedHandler(&gotAdminID)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_InvalidToken_401(t *testing.T) {
	var gotAdminID string
	handler := newProtectedHandler(&gotAdminID)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_ExpiredToken_401(t *testing.T) {
	var gotAdminID string
	handler := newProtectedHandler(&gotAdminID)

	token := expiredToken(t, "admin-1", middlewareTestSecret)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
