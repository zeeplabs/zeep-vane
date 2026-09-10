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

// userGetter is the subset of *db.UserRepository AuthHandler depends on.
type userGetter interface {
	GetByEmail(ctx context.Context, email string) (*db.User, error)
	UpdateName(ctx context.Context, userID, name string) error
	UpdatePasswordHash(ctx context.Context, userID, passwordHash string) error
	RevokeSessions(ctx context.Context, id string) error
}

// authMembershipLister is the subset of *db.TenantMembershipRepository
// AuthHandler depends on - resolving the active tenant at login, and
// listing every membership for /api/auth/me (multi-tenancy-core, AD-022).
type authMembershipLister interface {
	ListForUser(ctx context.Context, userID string) ([]db.TenantMembership, error)
}

// AuthHandler serves the auth-related admin routes.
type AuthHandler struct {
	users         userGetter
	memberships   authMembershipLister
	pool          *db.Pool
	logger        *zap.Logger
	sessionSecret string
	secureCookies bool
}

// NewAuthHandler builds an AuthHandler backed by users and memberships.
// pool backs Login's own tenant-membership lookup (a public route, ahead of
// the tenant-context middleware - it manages its own short-lived
// transaction with app.user_id set to resolve which tenant(s) the
// authenticating user belongs to). sessionSecret signs issued session
// tokens (see internal/auth.IssueSession). secureCookies controls the
// vane_session cookie's Secure attribute (H9) - false only for an operator
// who has explicitly accepted plaintext-network session risk via
// VANE_SECURE_COOKIES=false.
func NewAuthHandler(users userGetter, memberships authMembershipLister, pool *db.Pool, logger *zap.Logger, sessionSecret string, secureCookies bool) *AuthHandler {
	return &AuthHandler{users: users, memberships: memberships, pool: pool, logger: logger, sessionSecret: sessionSecret, secureCookies: secureCookies}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// genericLoginErrorBody is returned byte-for-byte for both a wrong password
// and a nonexistent email, so a caller can never distinguish the two
// (SP-22, anti user-enumeration).
const genericLoginErrorBody = `{"error":"invalid email or password"}`

// emailNotVerifiedBody is returned when an otherwise-valid login (correct
// email + password) belongs to a user whose email_verified_at is still
// null - the SaaS signup flow's verification gate (T10, spec.md AC2). It is
// checked only after the password itself is confirmed correct, so it never
// becomes a second account-enumeration oracle on top of genericLoginErrorBody.
const emailNotVerifiedBody = `{"error":"email not verified, check your inbox for the verification link"}`

// noTenantAccessBody is returned when an otherwise-valid login belongs to a
// user with zero tenant_memberships (e.g. removed from every tenant) -
// spec.md's edge case: never a dashboard with nothing in it, a clear
// rejection instead (TENANT-19 session half).
const noTenantAccessBody = `{"error":"account has no tenant access"}`

type loginResponse struct {
	Token string `json:"token"`
	// TenantID is the session's active tenant - set only when the user has
	// exactly one tenant_membership (self-hosted's every-day case); empty
	// when they have more than one, pending tenant selection (P2, not
	// built in this batch) via /api/auth/me's Memberships list.
	TenantID string `json:"tenant_id,omitempty"`
}

// Login validates email+password and reports success or a generic
// authentication failure. It never reveals whether the submitted email is
// registered. On success it also resolves the user's tenant_memberships:
// exactly one sets that tenant active in the issued session; zero refuses
// the login entirely (no session issued); more than one still succeeds,
// active tenant left unset.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoginError(w)
		return
	}

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeLoginError(w)
		return
	case err != nil:
		h.logger.Error("auth: failed to look up user by email", zap.Error(err))
		writeInternalError(w)
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		writeLoginError(w)
		return
	}

	if user.EmailVerifiedAt == nil {
		writeAdminError(w, http.StatusForbidden, emailNotVerifiedBody)
		return
	}

	// Login is public, ahead of the tenant-context middleware - it manages
	// its own transaction here, setting only app.user_id (no active tenant
	// is known yet; that's exactly what this query determines).
	tx, err := h.pool.BeginTenantTx(r.Context(), user.ID, "")
	if err != nil {
		h.logger.Error("auth: failed to begin tenant transaction", zap.Error(err))
		writeInternalError(w)
		return
	}
	memberships, err := h.memberships.ListForUser(db.WithTenantTx(r.Context(), tx), user.ID)
	if err != nil {
		_ = tx.Rollback(r.Context())
		h.logger.Error("auth: failed to list tenant memberships", zap.Error(err))
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("auth: failed to commit tenant transaction", zap.Error(err))
		writeInternalError(w)
		return
	}

	if len(memberships) == 0 {
		writeAdminError(w, http.StatusForbidden, noTenantAccessBody)
		return
	}

	activeTenantID := ""
	if len(memberships) == 1 {
		activeTenantID = memberships[0].TenantID
	}

	token, err := auth.IssueSessionWithTenant(user.ID, activeTenantID, h.sessionSecret)
	if err != nil {
		h.logger.Error("auth: failed to issue session token", zap.Error(err))
		writeInternalError(w)
		return
	}

	http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token, TenantID: activeTenantID})
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

// meMembership is one entry in meResponse.Memberships - a tenant the
// authenticated user belongs to, and their role in it.
type meMembership struct {
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
}

type meResponse struct {
	ID    string  `json:"id"`
	Email string  `json:"email"`
	Name  string  `json:"name"`
	Phone *string `json:"phone,omitempty"`
	Role  string  `json:"role"`
	// ActiveTenantID is the session's active tenant, "" if none selected
	// yet (more than one membership, pending tenant selection - P2, not
	// built in this batch).
	ActiveTenantID string `json:"active_tenant_id,omitempty"`
	// Memberships lists every tenant the user belongs to - always
	// non-empty for an authenticated session (Login refuses zero
	// memberships outright), used by the tenant-selection screen (P2) to
	// decide whether one exists to show.
	Memberships []meMembership `json:"memberships"`
}

// Me returns the authenticated user's identity, as loaded into context by
// RequireAuth, plus their active tenant and full membership list. The
// identity itself never re-queries the database - RequireAuth already did
// that lookup for authorization purposes - but the membership list does,
// scoped by the request's own transaction (tenant-context middleware, T3).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	memberships, err := h.memberships.ListForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("auth: failed to list tenant memberships for me", zap.Error(err))
		writeInternalError(w)
		return
	}
	activeTenantID, _ := ActiveTenantIDFromContext(r.Context())
	role, _ := RoleFromContext(r.Context())

	out := make([]meMembership, len(memberships))
	for i, m := range memberships {
		out[i] = meMembership{TenantID: m.TenantID, Role: m.Role}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, Phone: user.Phone, Role: role,
		ActiveTenantID: activeTenantID, Memberships: out,
	})
}

type updateProfileRequest struct {
	Name string `json:"name"`
}

const invalidUpdateProfileRequestBody = `{"error":"name is required"}`

// UpdateProfile handles PATCH /api/auth/me, letting the authenticated user
// change their own display name only (profile-self-service PROFSS-01/02/03).
// Any other field in the request body is ignored (updateProfileRequest has
// no field for them, so json.Decode simply drops them - matches this
// codebase's existing JSON-decoding convention).
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidUpdateProfileRequestBody)
		return
	}

	if err := h.users.UpdateName(r.Context(), user.ID, req.Name); err != nil {
		h.logger.Error("auth: failed to update user name", zap.Error(err))
		writeInternalError(w)
		return
	}
	user.Name = req.Name

	memberships, err := h.memberships.ListForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("auth: failed to list tenant memberships for update-profile", zap.Error(err))
		writeInternalError(w)
		return
	}
	activeTenantID, _ := ActiveTenantIDFromContext(r.Context())
	role, _ := RoleFromContext(r.Context())

	out := make([]meMembership, len(memberships))
	for i, m := range memberships {
		out[i] = meMembership{TenantID: m.TenantID, Role: m.Role}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, Phone: user.Phone, Role: role,
		ActiveTenantID: activeTenantID, Memberships: out,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

const invalidCurrentPasswordBody = `{"error":"current password is incorrect"}`

// ChangePassword handles POST /api/auth/change-password, letting the
// authenticated user rotate their own password by proving they know the
// current one (profile-self-service PROFSS-04/05/06) - independent of the
// unauthenticated email-token reset flow. On success it revokes every other
// session for this user (RevokeSessions), exactly like
// PasswordResetHandler.Confirm already does; the caller's own current
// request is unaffected since it already passed RequireAuth before this
// handler ran.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusUnauthorized, invalidCurrentPasswordBody)
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		writeAdminError(w, http.StatusUnauthorized, invalidCurrentPasswordBody)
		return
	}

	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, weakPasswordBody)
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		h.logger.Error("auth: failed to hash new password", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.users.UpdatePasswordHash(r.Context(), user.ID, newHash); err != nil {
		h.logger.Error("auth: failed to update password", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.users.RevokeSessions(r.Context(), user.ID); err != nil {
		h.logger.Error("auth: failed to revoke sessions after password change", zap.String("user_id", user.ID), zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Logout expires the vane_session cookie set at login. It requires no
// role beyond being authenticated - any user can end their own session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie("", -1, h.secureCookies))
	w.WriteHeader(http.StatusOK)
}

type switchTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

const invalidSwitchTenantRequestBody = `{"error":"tenant_id is required"}`

// noMembershipForTenantBody is returned when the caller has no
// tenant_membership for the requested tenant_id (TENANT-21) - deliberately
// distinct from noTenantAccessBody (login's "zero memberships anywhere"
// case), since this one means "you have access somewhere, just not here".
const noMembershipForTenantBody = `{"error":"no access to that tenant"}`

// SwitchTenant updates the session's active tenant to req.TenantID,
// provided the authenticated user has a tenant_membership for it - no new
// login required (TENANT-20). It rejects with 403 and leaves the current
// session's cookie untouched if the user has no membership there
// (TENANT-21), including for a syntactically valid but nonexistent
// tenant_id - ListForUser simply won't return a match for one, the same
// fail-closed shape as everywhere else in this feature.
func (h *AuthHandler) SwitchTenant(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	var req switchTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidSwitchTenantRequestBody)
		return
	}

	memberships, err := h.memberships.ListForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("auth: failed to list tenant memberships for switch-tenant", zap.Error(err))
		writeInternalError(w)
		return
	}

	hasMembership := false
	for _, m := range memberships {
		if m.TenantID == req.TenantID {
			hasMembership = true
			break
		}
	}
	if !hasMembership {
		writeAdminError(w, http.StatusForbidden, noMembershipForTenantBody)
		return
	}

	token, err := auth.IssueSessionWithTenant(user.ID, req.TenantID, h.sessionSecret)
	if err != nil {
		h.logger.Error("auth: failed to issue session token for switch-tenant", zap.Error(err))
		writeInternalError(w)
		return
	}
	http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token, TenantID: req.TenantID})
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
