// Package api implements vane's admin-facing REST handlers.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// adminGetter is the subset of *db.AdminRepository the login handler
// depends on.
type adminGetter interface {
	GetByEmail(ctx context.Context, email string) (*db.Admin, error)
}

// AuthHandler serves the auth-related admin routes.
type AuthHandler struct {
	admins        adminGetter
	logger        *zap.Logger
	sessionSecret string
	secureCookies bool
}

// NewAuthHandler builds an AuthHandler backed by admins. sessionSecret signs
// issued session tokens (see internal/auth.IssueSession). secureCookies
// controls the vane_session cookie's Secure attribute (H9) - false only for
// an operator who has explicitly accepted plaintext-network session risk via
// VANE_SECURE_COOKIES=false.
func NewAuthHandler(admins adminGetter, logger *zap.Logger, sessionSecret string, secureCookies bool) *AuthHandler {
	return &AuthHandler{admins: admins, logger: logger, sessionSecret: sessionSecret, secureCookies: secureCookies}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// genericLoginErrorBody is returned byte-for-byte for both a wrong password
// and a nonexistent email, so a caller can never distinguish the two
// (SP-22, anti user-enumeration).
const genericLoginErrorBody = `{"error":"invalid email or password"}`

// Login validates email+password and reports success or a generic
// authentication failure. It never reveals whether the submitted email is
// registered.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoginError(w)
		return
	}

	admin, err := h.admins.GetByEmail(r.Context(), req.Email)
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeLoginError(w)
		return
	case err != nil:
		h.logger.Error("auth: failed to look up admin by email", zap.Error(err))
		writeInternalError(w)
		return
	}

	if !auth.VerifyPassword(admin.PasswordHash, req.Password) {
		writeLoginError(w)
		return
	}

	token, err := auth.IssueSession(admin.ID, h.sessionSecret)
	if err != nil {
		h.logger.Error("auth: failed to issue session token", zap.Error(err))
		writeInternalError(w)
		return
	}

	http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"token":%q}`, token)))
}

// sessionCookieName is the name of the session cookie set on login and
// cleared on logout (AD-004).
const sessionCookieName = "vane_session"

// sessionCookie builds the vane_session cookie with the attributes AD-004
// requires. maxAge is in seconds - pass a negative value to build an
// already-expired cookie (used by logout). secure controls the Secure
// attribute - true unless the operator has opted out via
// VANE_SECURE_COOKIES=false (H9).
func sessionCookie(value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
}

type meResponse struct {
	ID    string  `json:"id"`
	Email string  `json:"email"`
	Name  string  `json:"name"`
	Phone *string `json:"phone,omitempty"`
	Role  string  `json:"role"`
}

// Me returns the authenticated admin's identity, as loaded into context by
// RequireAuth. It never re-queries the database - RequireAuth already did
// that lookup for authorization purposes.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	admin, ok := AdminFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{ID: admin.ID, Email: admin.Email, Name: admin.Name, Phone: admin.Phone, Role: admin.Role})
}

// Logout expires the vane_session cookie set at login. It requires no
// role beyond being authenticated - any admin can end their own session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie("", -1, h.secureCookies))
	w.WriteHeader(http.StatusOK)
}

func writeLoginError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(genericLoginErrorBody))
}

func writeInternalError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"error":"internal server error"}`))
}
