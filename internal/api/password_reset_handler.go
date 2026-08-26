package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// resetTokenTTL is how long a password reset token stays valid, per SP-23.
const resetTokenTTL = 1 * time.Hour

// passwordResetTokenBytes is the size of the raw random token before
// encoding, chosen to make guessing infeasible.
const passwordResetTokenBytes = 32

// passwordResetRepo is the subset of *db.PasswordResetRepository the
// password reset handler depends on.
type passwordResetRepo interface {
	Create(ctx context.Context, token *db.PasswordResetToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*db.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id string) error
}

// adminByEmailAndIDUpdater is the subset of *db.AdminRepository the password
// reset handler depends on.
type adminByEmailAndIDUpdater interface {
	adminGetter
	UpdatePasswordHash(ctx context.Context, adminID, passwordHash string) error
}

// PasswordResetHandler serves the password reset request/confirm routes.
type PasswordResetHandler struct {
	admins          adminByEmailAndIDUpdater
	tokens          passwordResetRepo
	logger          *zap.Logger
	devTokenLogging bool
}

// NewPasswordResetHandler builds a PasswordResetHandler. devTokenLogging
// gates whether the raw reset token is logged (see Request) - it stands in
// for real email delivery and must default to off, since the token is a
// bearer credential for an account takeover.
func NewPasswordResetHandler(admins adminByEmailAndIDUpdater, tokens passwordResetRepo, logger *zap.Logger, devTokenLogging bool) *PasswordResetHandler {
	return &PasswordResetHandler{admins: admins, tokens: tokens, logger: logger, devTokenLogging: devTokenLogging}
}

type passwordResetRequestBody struct {
	Email string `json:"email"`
}

// Request handles POST /api/auth/password-reset/request. It always
// responds 200 regardless of whether the email is registered, so the
// endpoint cannot be used to enumerate accounts (same principle as SP-22).
// Email delivery is out of scope for the MVP: the raw token is only logged,
// standing in for a real provider.
func (h *PasswordResetHandler) Request(w http.ResponseWriter, r *http.Request) {
	var body passwordResetRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	admin, err := h.admins.GetByEmail(r.Context(), body.Email)
	if err != nil {
		// Covers ErrNotFound and any other lookup error identically -
		// the response never reveals whether the email exists.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	rawToken, err := generateResetToken()
	if err != nil {
		h.logger.Error("password-reset: failed to generate token", zap.Error(err))
		writeInternalError(w)
		return
	}

	resetToken := &db.PasswordResetToken{
		AdminID:   admin.ID,
		TokenHash: hashResetToken(rawToken),
		ExpiresAt: time.Now().Add(resetTokenTTL),
	}
	if err := h.tokens.Create(r.Context(), resetToken); err != nil {
		h.logger.Error("password-reset: failed to persist token", zap.Error(err))
		writeInternalError(w)
		return
	}

	// Stub for real email delivery (out of scope for the MVP). The raw
	// token is a bearer credential for this admin's account, so it is
	// only logged when VANE_DEV_TOKEN_LOGGING=true is explicitly set -
	// never by default, since LOG_LEVEL=info output routinely reaches a
	// wider audience (log aggregators, `docker logs`) than the database
	// itself. The raw token itself is never persisted (see
	// PasswordResetToken).
	if h.devTokenLogging {
		h.logger.Info("password-reset: token issued",
			zap.String("admin_id", admin.ID), zap.String("token", rawToken))
	} else {
		h.logger.Info("password-reset: token issued",
			zap.String("admin_id", admin.ID))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type passwordResetConfirmBody struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

const resetConfirmErrorBody = `{"error":"invalid or expired reset token"}`

// Confirm handles POST /api/auth/password-reset/confirm. It rejects an
// expired or already-used token (SP-24) and otherwise sets the admin's new
// password and marks the token used so it cannot be replayed.
func (h *PasswordResetHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	var body passwordResetConfirmBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeResetConfirmError(w)
		return
	}

	resetToken, err := h.tokens.GetByTokenHash(r.Context(), hashResetToken(body.Token))
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeResetConfirmError(w)
		return
	case err != nil:
		h.logger.Error("password-reset: failed to look up token", zap.Error(err))
		writeInternalError(w)
		return
	}

	if resetToken.UsedAt != nil || time.Now().After(resetToken.ExpiresAt) {
		writeResetConfirmError(w)
		return
	}

	if err := auth.ValidatePassword(body.NewPassword); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, weakPasswordBody)
		return
	}

	newHash, err := auth.HashPassword(body.NewPassword)
	if err != nil {
		h.logger.Error("password-reset: failed to hash new password", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.admins.UpdatePasswordHash(r.Context(), resetToken.AdminID, newHash); err != nil {
		h.logger.Error("password-reset: failed to update password", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.tokens.MarkUsed(r.Context(), resetToken.ID); err != nil {
		h.logger.Error("password-reset: failed to mark token used", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func writeResetConfirmError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(resetConfirmErrorBody))
}

func generateResetToken() (string, error) {
	raw := make([]byte, passwordResetTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashResetToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
