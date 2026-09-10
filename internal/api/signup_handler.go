package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// signupVerificationTTL is how long a signup email-verification token stays
// valid - same TTL as admin invites (adminInviteTTL), no new decision
// needed. The token this issues is consumed by T10's verify endpoint, not
// yet built in this task.
const signupVerificationTTL = 1 * time.Hour

// signupUserStore is the subset of *db.UserRepository SignupHandler depends
// on.
type signupUserStore interface {
	GetByEmail(ctx context.Context, email string) (*db.User, error)
	Create(ctx context.Context, user *db.User) error
}

// signupTenantCreator is the subset of *db.TenantRepository SignupHandler
// depends on to provision a new signup's tenant.
type signupTenantCreator interface {
	Create(ctx context.Context, tenant *db.Tenant) error
}

// signupMembershipCreator is the subset of *db.TenantMembershipRepository
// SignupHandler depends on to link a signup's user to their new tenant as
// owner.
type signupMembershipCreator interface {
	Create(ctx context.Context, m *db.TenantMembership) error
}

// signupVerificationStore is the subset of *db.EmailVerificationRepository
// SignupHandler depends on.
type signupVerificationStore interface {
	Create(ctx context.Context, token *db.EmailVerificationToken) error
}

// SignupHandler serves the public SaaS signup routes (multi-tenancy-core,
// T9: account/tenant creation and the verification email it triggers).
type SignupHandler struct {
	pool            *db.Pool
	users           signupUserStore
	tenants         signupTenantCreator
	memberships     signupMembershipCreator
	verifications   signupVerificationStore
	emailSvc        *email.Service
	logger          *zap.Logger
	devTokenLogging bool
	adminBaseURL    string
}

// NewSignupHandler builds a SignupHandler. pool backs the tenant+membership
// transaction Signup opens, same pattern as BootstrapHandler.Create.
// devTokenLogging/adminBaseURL mirror AdminsHandler's own fields - the raw
// verification token is only logged when explicitly enabled, and the
// verification link is built from cfg.AdminBaseURL, never the incoming
// request's Host header.
func NewSignupHandler(pool *db.Pool, users signupUserStore, tenants signupTenantCreator, memberships signupMembershipCreator, verifications signupVerificationStore, emailSvc *email.Service, logger *zap.Logger, devTokenLogging bool, adminBaseURL string) *SignupHandler {
	return &SignupHandler{
		pool: pool, users: users, tenants: tenants, memberships: memberships,
		verifications: verifications, emailSvc: emailSvc, logger: logger,
		devTokenLogging: devTokenLogging, adminBaseURL: adminBaseURL,
	}
}

type signupRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TenantName string `json:"tenant_name"`
}

const invalidSignupRequestBody = `{"error":"email, password, and tenant_name are required"}`
const signupPendingVerificationBody = `{"error":"a signup for this email is already pending verification"}`

type signupResponse struct {
	Status    string `json:"status"`
	Email     string `json:"email"`
	EmailSent bool   `json:"email_sent,omitempty"`
}

// Signup handles POST /api/signup (public). A new email creates a tenant,
// user, and owner membership atomically (with each other - see
// createTenantAndOwnerMembership) and sends a verification email; the new
// user's email_verified_at stays null until that link is followed (login
// gating on it is T10's job, not yet built). An email that already belongs
// to a verified user instead creates a new tenant + owner membership for
// that existing user, without touching their password (spec.md AC4 - the
// "consultant with multiple tenants" case). The same email retried while a
// previous signup is still unverified is rejected with 409 and creates
// nothing (spec.md edge case).
func (h *SignupHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" || req.TenantName == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidSignupRequestBody)
		return
	}

	existing, err := h.users.GetByEmail(r.Context(), req.Email)
	switch {
	case errors.Is(err, db.ErrNotFound):
		h.signupNewUser(w, r, req)
		return
	case err != nil:
		h.logger.Error("signup: failed to look up user by email", zap.Error(err))
		writeInternalError(w)
		return
	}

	if existing.EmailVerifiedAt == nil {
		writeAdminError(w, http.StatusConflict, signupPendingVerificationBody)
		return
	}

	h.signupExistingVerifiedUser(w, r, req, existing)
}

// signupNewUser is Signup's path for an email that has never signed up
// before: it validates the submitted password (this is the only path where
// a caller-supplied password is ever used), creates the user unverified,
// provisions their tenant + owner membership, and sends the verification
// email.
func (h *SignupHandler) signupNewUser(w http.ResponseWriter, r *http.Request, req signupRequest) {
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, weakPasswordBody)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("signup: failed to hash password", zap.Error(err))
		writeInternalError(w)
		return
	}

	user := &db.User{Email: req.Email, PasswordHash: hash}
	if err := h.users.Create(r.Context(), user); err != nil {
		if errors.Is(err, db.ErrDuplicateEmail) {
			// A concurrent signup for the same email won the race between
			// this handler's own GetByEmail and this Create - same outcome
			// as the sequential duplicate-pending check above.
			writeAdminError(w, http.StatusConflict, signupPendingVerificationBody)
			return
		}
		h.logger.Error("signup: failed to create user", zap.Error(err))
		writeInternalError(w)
		return
	}

	if _, err := h.createTenantAndOwnerMembership(r, user.ID, req.TenantName, req.Email); err != nil {
		h.logger.Error("signup: failed to provision tenant", zap.Error(err))
		writeInternalError(w)
		return
	}

	emailSent := h.issueAndSendVerification(r, user.ID, req.Email, req.TenantName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(signupResponse{Status: "pending_verification", Email: req.Email, EmailSent: emailSent})
}

// signupExistingVerifiedUser is Signup's path for an email that already
// belongs to a verified user (spec.md AC4): it provisions a new tenant +
// owner membership for that user without creating a second user row or
// asking for a new password - same "existing user, membership only"
// principle AcceptInvite applies (T14).
func (h *SignupHandler) signupExistingVerifiedUser(w http.ResponseWriter, r *http.Request, req signupRequest, user *db.User) {
	if _, err := h.createTenantAndOwnerMembership(r, user.ID, req.TenantName, req.Email); err != nil {
		h.logger.Error("signup: failed to provision tenant for existing user", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(signupResponse{Status: "created", Email: user.Email})
}

// createTenantAndOwnerMembership provisions a brand new tenant and links
// userID to it as owner, atomically with each other - same
// generate-id/BeginTenantTx/insert-both/commit shape as
// BootstrapHandler.Create, since tenants' RLS WITH CHECK requires
// app.tenant_id to already equal the row's own id.
func (h *SignupHandler) createTenantAndOwnerMembership(r *http.Request, userID, tenantName, contactEmail string) (string, error) {
	var tenantID string
	if err := h.pool.QueryRow(r.Context(), "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		return "", fmt.Errorf("signup: failed to generate tenant id: %w", err)
	}

	tx, err := h.pool.BeginTenantTx(r.Context(), userID, tenantID)
	if err != nil {
		return "", fmt.Errorf("signup: failed to begin tenant transaction: %w", err)
	}
	tenantCtx := db.WithTenantTx(r.Context(), tx)

	tenant := &db.Tenant{ID: tenantID, Name: tenantName, ContactEmail: contactEmail}
	if err := h.tenants.Create(tenantCtx, tenant); err != nil {
		_ = tx.Rollback(r.Context())
		return "", fmt.Errorf("signup: failed to create tenant: %w", err)
	}
	if err := h.memberships.Create(tenantCtx, &db.TenantMembership{UserID: userID, TenantID: tenantID, Role: db.RoleOwner}); err != nil {
		_ = tx.Rollback(r.Context())
		return "", fmt.Errorf("signup: failed to create owner membership: %w", err)
	}
	if err := tx.Commit(r.Context()); err != nil {
		return "", fmt.Errorf("signup: failed to commit tenant transaction: %w", err)
	}

	return tenantID, nil
}

// issueAndSendVerification mints a verification token for userID, persists
// it, and sends the verification email to "to". A failure at any step -
// including the email send itself (spec.md AC5) - is logged and reported
// back as emailSent=false; it never rolls back the tenant/user/membership
// already committed by the caller, mirroring
// AdminsHandler.sendAdminInviteEmail's non-blocking convention.
func (h *SignupHandler) issueAndSendVerification(r *http.Request, userID, to, tenantName string) (emailSent bool) {
	rawToken, err := generateAdminInviteToken()
	if err != nil {
		h.logger.Error("signup: failed to generate verification token", zap.String("user_id", userID), zap.Error(err))
		return false
	}

	token := &db.EmailVerificationToken{
		UserID:    userID,
		TokenHash: hashAdminInviteToken(rawToken),
		ExpiresAt: time.Now().Add(signupVerificationTTL),
	}
	if err := h.verifications.Create(r.Context(), token); err != nil {
		h.logger.Error("signup: failed to persist verification token", zap.String("user_id", userID), zap.Error(err))
		return false
	}

	// The raw token grants email verification for this account, so it is
	// only logged when VANE_DEV_TOKEN_LOGGING=true is explicitly set - same
	// reasoning as AdminsHandler.Invite/PasswordResetHandler.Request.
	if h.devTokenLogging {
		h.logger.Info("signup: verification token issued", zap.String("user_id", userID), zap.String("token", rawToken))
	} else {
		h.logger.Info("signup: verification token issued", zap.String("user_id", userID))
	}

	data := email.SignupVerificationEmailData{
		TenantName: tenantName,
		VerifyURL:  fmt.Sprintf("%s/verify-email/%s", adminBaseURL(h.adminBaseURL), rawToken),
	}
	if err := h.emailSvc.SendSignupVerification(r.Context(), to, data); err != nil {
		h.logger.Error("signup: failed to send verification email", zap.String("user_id", userID), zap.Error(err))
		return false
	}

	return true
}
