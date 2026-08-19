// Package api implements vane's admin-facing REST handlers.
package api

import (
	"context"
	"encoding/json"
	"errors"
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
	admins adminGetter
	logger *zap.Logger
}

// NewAuthHandler builds an AuthHandler backed by admins.
func NewAuthHandler(admins adminGetter, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{admins: admins, logger: logger}
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
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
