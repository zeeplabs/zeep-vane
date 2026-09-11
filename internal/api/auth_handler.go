// Package api implements vane's admin-facing REST handlers.
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// userGetter is the subset of *db.UserRepository AuthHandler depends on.
type userGetter interface {
	GetByEmail(ctx context.Context, email string) (*db.User, error)
	GetByID(ctx context.Context, id string) (*db.User, error)
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

// twoFactorStore is the subset of *db.TwoFactorRepository AuthHandler
// depends on for TOTP secret and recovery-code lifecycle (auth-2fa-totp).
type twoFactorStore interface {
	CreatePendingSecret(ctx context.Context, userID string, encryptedSecret []byte) error
	GetSecret(ctx context.Context, userID string) (*db.TwoFactorSecret, error)
	ConfirmSecret(ctx context.Context, userID string) error
	DeleteSecret(ctx context.Context, userID string) error
	CreateRecoveryCodes(ctx context.Context, userID string, hashes []string) error
	ConsumeRecoveryCode(ctx context.Context, userID, code string) (bool, error)
	DeleteRecoveryCodes(ctx context.Context, userID string) error
}

// twoFactorChallengeStore is the subset of *db.TwoFactorChallengeRepository
// AuthHandler depends on for the pre-2FA challenge token's server-side
// one-time-use state (auth-2fa-totp T9).
type twoFactorChallengeStore interface {
	Create(ctx context.Context, userID string, ttl time.Duration) (jti string, expiresAt time.Time, err error)
	Lookup(ctx context.Context, jti string) (*db.TwoFactorChallenge, error)
	MarkUsed(ctx context.Context, jti string) (userID string, ok bool, err error)
}

// twoFactorIssuer is the otpauth:// issuer name shown in an authenticator
// app's entry for an enrolled account. Not configurable - this codebase has
// no instance-name/branding config field to source it from (auth-2fa-totp
// design.md scope).
const twoFactorIssuer = "Vane"

// AuthHandler serves the auth-related admin routes.
type AuthHandler struct {
	users         userGetter
	memberships   authMembershipLister
	twoFactor     twoFactorStore
	challenges    twoFactorChallengeStore
	sessions      *db.SessionRepository
	pool          *db.Pool
	logger        *zap.Logger
	sessionSecret string
	secureCookies bool
	masterKey     string
}

// NewAuthHandler builds an AuthHandler backed by users, memberships,
// twoFactor, challenges, and sessions. pool backs Login's own tenant-
// membership lookup (a public route, ahead of the tenant-context
// middleware - it manages its own short-lived transaction with
// app.user_id set to resolve which tenant(s) the authenticating user
// belongs to). sessions is the per-device session row repository
// (user-sessions) - issueSessionForUser creates a row here for every
// issued token, SwitchTenant reuses the current session's row id, and
// Logout revokes the current session's row. sessionSecret signs issued
// session tokens (see internal/auth.IssueSession) and 2FA challenge
// tokens (internal/auth.IssueTwoFactorChallenge) - the same HMAC secret
// backs both, distinguished only by claim shape (auth-2fa-totp
// design.md). secureCookies controls the vane_session cookie's Secure
// attribute (H9) - false only for an operator who has explicitly
// accepted plaintext-network session risk via VANE_SECURE_COOKIES=false.
// masterKey encrypts/decrypts TOTP secrets at rest (internal/crypto.Encrypt,
// same primitive email_provider_repository.go uses for provider API keys).
func NewAuthHandler(users userGetter, memberships authMembershipLister, twoFactor twoFactorStore, challenges twoFactorChallengeStore, sessions *db.SessionRepository, pool *db.Pool, logger *zap.Logger, sessionSecret string, secureCookies bool, masterKey string) *AuthHandler {
	return &AuthHandler{users: users, memberships: memberships, twoFactor: twoFactor, challenges: challenges, sessions: sessions, pool: pool, logger: logger, sessionSecret: sessionSecret, secureCookies: secureCookies, masterKey: masterKey}
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

	secret, err := h.twoFactor.GetSecret(r.Context(), user.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("auth: failed to look up 2FA secret at login", zap.Error(err))
		writeInternalError(w)
		return
	}
	if secret != nil && secret.EnabledAt != nil {
		h.issueTwoFactorChallenge(w, r, user.ID)
		return
	}

	h.issueSessionForUser(w, r, user)
}

type loginChallengeResponse struct {
	ChallengeToken string `json:"challenge_token"`
}

// issueTwoFactorChallenge responds 200 with a short-lived challenge token
// for userID instead of a session (TOTP-05) - Login takes this path only
// for a user whose 2FA is confirmed enabled. No vane_session cookie is set;
// the caller must complete POST /api/auth/login/verify-2fa to obtain one.
func (h *AuthHandler) issueTwoFactorChallenge(w http.ResponseWriter, r *http.Request, userID string) {
	jti, _, err := h.challenges.Create(r.Context(), userID, auth.TwoFactorChallengeTTL)
	if err != nil {
		h.logger.Error("auth: failed to create 2FA challenge", zap.Error(err))
		writeInternalError(w)
		return
	}

	token, err := auth.IssueTwoFactorChallenge(userID, jti, h.sessionSecret)
	if err != nil {
		h.logger.Error("auth: failed to issue 2FA challenge token", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginChallengeResponse{ChallengeToken: token})
}

type verifyTwoFactorRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
	// RecoveryCode, when non-empty, is checked instead of Code - the
	// device-loss fallback path (spec.md P2). Consuming it via
	// ConsumeRecoveryCode is itself the proof of a valid second factor, so a
	// request with a non-empty RecoveryCode never checks Code at all.
	RecoveryCode string `json:"recovery_code"`
}

// invalidTwoFactorChallengeBody is returned for every verify-2fa failure
// mode (expired/malformed/already-consumed token, wrong code) - a single
// generic body, matching this codebase's anti-enumeration convention for
// auth failures (genericLoginErrorBody).
const invalidTwoFactorChallengeBody = `{"error":"invalid or expired verification"}`

func writeInvalidTwoFactorChallenge(w http.ResponseWriter) {
	writeAdminError(w, http.StatusUnauthorized, invalidTwoFactorChallengeBody)
}

// VerifyTwoFactor handles POST /api/auth/login/verify-2fa, completing the
// login Login deferred when it issued a challenge token for a 2FA-enabled
// user. It validates req.ChallengeToken (signature, expiry, audience claim,
// and - via the backing two_factor_challenges row - not already consumed),
// then req.Code against that user's confirmed TOTP secret. On success it
// atomically claims the challenge row (so it can never be reused, TOTP-07)
// and issues a full session exactly like Login would (TOTP-06). A wrong
// code leaves the challenge unconsumed so the user may retry within its
// remaining validity (TOTP-08).
func (h *AuthHandler) VerifyTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req verifyTwoFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChallengeToken == "" {
		writeInvalidTwoFactorChallenge(w)
		return
	}

	claimUserID, jti, err := auth.VerifyTwoFactorChallenge(req.ChallengeToken, h.sessionSecret)
	if err != nil {
		writeInvalidTwoFactorChallenge(w)
		return
	}

	challenge, err := h.challenges.Lookup(r.Context(), jti)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeInvalidTwoFactorChallenge(w)
			return
		}
		h.logger.Error("auth: failed to look up 2FA challenge", zap.Error(err))
		writeInternalError(w)
		return
	}
	if challenge.UserID != claimUserID || challenge.UsedAt != nil || time.Now().After(challenge.ExpiresAt) {
		writeInvalidTwoFactorChallenge(w)
		return
	}

	secret, err := h.twoFactor.GetSecret(r.Context(), claimUserID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// 2FA was disabled after the challenge was issued (spec.md Edge
			// Cases) - the challenge's implied precondition no longer holds.
			writeInvalidTwoFactorChallenge(w)
			return
		}
		h.logger.Error("auth: failed to look up 2FA secret for verify-2fa", zap.Error(err))
		writeInternalError(w)
		return
	}
	if secret.EnabledAt == nil {
		writeInvalidTwoFactorChallenge(w)
		return
	}

	if req.RecoveryCode != "" {
		// The recovery-code path (TOTP-10/11): ConsumeRecoveryCode's atomic
		// claim (UPDATE ... WHERE used_at IS NULL RETURNING) is itself the
		// proof of a valid, not-yet-spent second factor - a used code is
		// permanently rejected, even on a later unrelated login attempt.
		consumed, err := h.twoFactor.ConsumeRecoveryCode(r.Context(), claimUserID, req.RecoveryCode)
		if err != nil {
			h.logger.Error("auth: failed to consume 2FA recovery code", zap.Error(err))
			writeInternalError(w)
			return
		}
		if !consumed {
			writeInvalidTwoFactorChallenge(w)
			return
		}
	} else {
		decryptedSecret, err := crypto.Decrypt(h.masterKey, secret.EncryptedSecret)
		if err != nil {
			h.logger.Error("auth: failed to decrypt 2FA secret for verify-2fa", zap.Error(err))
			writeInternalError(w)
			return
		}

		if !auth.ValidateTOTPCode(string(decryptedSecret), req.Code) {
			writeInvalidTwoFactorChallenge(w)
			return
		}
	}

	if _, ok, err := h.challenges.MarkUsed(r.Context(), jti); err != nil {
		h.logger.Error("auth: failed to mark 2FA challenge used", zap.Error(err))
		writeInternalError(w)
		return
	} else if !ok {
		// Lost a race with a concurrent verify-2fa call on the same
		// challenge (design.md Risks & Concerns) - the other request already
		// claimed it.
		writeInvalidTwoFactorChallenge(w)
		return
	}

	user, err := h.users.GetByID(r.Context(), claimUserID)
	if err != nil {
		h.logger.Error("auth: failed to look up user for verify-2fa", zap.Error(err))
		writeInternalError(w)
		return
	}

	h.issueSessionForUser(w, r, user)
}

// issueSessionForUser resolves user's tenant_memberships and, on success,
// issues a full session: exactly one membership sets that tenant active in
// the session; zero refuses the login entirely (no session issued,
// TENANT-19 session half); more than one still succeeds, active tenant left
// unset. It writes the HTTP response itself. Extracted from Login
// (auth-2fa-totp T8) so VerifyTwoFactor can perform the exact same
// tenant-resolution + session-issuance sequence once a challenge's second
// factor validates, instead of duplicating this security-sensitive logic at
// a second call site.
//
// On success it also creates a new row in the `sessions` table (user-
// sessions spec SESS-01) and embeds that row's id as the JWT's `sid`
// claim. RequireAuth then looks the row up on every authenticated
// request so per-device revocation (Logout, the "Encerrar" button on
// the redesigned Meu Perfil screen) can reject a single session without
// disturbing any other.
func (h *AuthHandler) issueSessionForUser(w http.ResponseWriter, r *http.Request, user *db.User) {
	// Called ahead of the tenant-context middleware (from both Login and
	// VerifyTwoFactor, both public routes) - it manages its own transaction
	// here, setting only app.user_id (no active tenant is known yet; that's
	// exactly what this query determines).
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

	// Capture the request's identifying signals (User-Agent, IP) so the
	// "Sessões ativas" list can show the device that opened the session.
	// Empty strings are stored as NULL by SessionRepository.Create, not as
	// empty strings (matches the schema's nullable shape).
	userAgent, ip := captureSessionContext(r)

	sid, err := h.sessions.Create(r.Context(), user.ID, userAgent, ip)
	if err != nil {
		h.logger.Error("auth: failed to create session row", zap.Error(err))
		writeInternalError(w)
		return
	}

	token, err := auth.IssueSessionWithTenant(user.ID, activeTenantID, sid, h.sessionSecret)
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

// captureSessionContext extracts the identifying signals the "Sessões
// ativas" list needs to display each row's device: the raw User-Agent
// header and the host portion of r.RemoteAddr. Both come back as
// empty strings when not present, which SessionRepository.Create
// translates to NULL at the row level - same posture as
// tenant_memberships (nullable, never empty).
//
// IP sourcing deliberately ignores X-Forwarded-For/X-Real-IP,
// matching AGENTS.md §4's rule and ratelimit.clientIP. A self-hosted
// deploy terminating TLS behind its own reverse proxy should configure
// the proxy to preserve the real client address in RemoteAddr.
func captureSessionContext(r *http.Request) (userAgent, ip string) {
	userAgent = r.UserAgent()
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	} else {
		ip = r.RemoteAddr
	}
	return userAgent, ip
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
	// Name and PlanTier are the tenant's display name and plan (new-layout-
	// migration, SHELL-20/21) - sourced from db.TenantMembership.Name/.Plan
	// (ListForUser's JOIN tenants), never a backend-invented default: an
	// empty PlanTier is passed through as-is.
	Name     string `json:"name"`
	PlanTier string `json:"plan_tier"`
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
	// TwoFactorEnabled reports whether the user has a confirmed TOTP
	// enrollment (profile-page PROFPAGE-23) - sourced from
	// two_factor_secrets.enabled_at, never a client-supplied value.
	TwoFactorEnabled bool `json:"two_factor_enabled"`
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

	// A missing secret is the normal "not enrolled" case; any other error
	// degrades to false rather than failing the whole identity payload
	// (profile-page PROFPAGE-23).
	twoFactorEnabled := false
	secret, err := h.twoFactor.GetSecret(r.Context(), user.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("auth: failed to load two-factor state for me", zap.Error(err))
	} else if secret != nil && secret.EnabledAt != nil {
		twoFactorEnabled = true
	}

	out := make([]meMembership, len(memberships))
	for i, m := range memberships {
		out[i] = meMembership{TenantID: m.TenantID, Role: m.Role, Name: m.Name, PlanTier: m.Plan}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, Phone: user.Phone, Role: role,
		ActiveTenantID: activeTenantID, Memberships: out, TwoFactorEnabled: twoFactorEnabled,
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
		out[i] = meMembership{TenantID: m.TenantID, Role: m.Role, Name: m.Name, PlanTier: m.Plan}
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

const alreadyEnrolledBody = `{"error":"two-factor authentication is already enabled"}`

type enrollResponse struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauth_uri"`
}

// Enroll handles POST /api/auth/2fa/enroll, generating a new TOTP secret for
// the authenticated user and storing it encrypted with enabled_at NULL
// (auth-2fa-totp TOTP-01). It responds 409 if the user already has 2FA
// enabled (TOTP-04) - an enrollment cannot silently replace an active one;
// the user must call Disable2FA first. Calling it again before confirming
// simply overwrites the still-pending secret (spec.md Edge Cases).
func (h *AuthHandler) Enroll(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	existing, err := h.twoFactor.GetSecret(r.Context(), user.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("auth: failed to look up existing 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}
	if existing != nil && existing.EnabledAt != nil {
		writeAdminError(w, http.StatusConflict, alreadyEnrolledBody)
		return
	}

	secret, otpauthURI, err := auth.GenerateTOTPSecret(user.Email, twoFactorIssuer)
	if err != nil {
		h.logger.Error("auth: failed to generate TOTP secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	encryptedSecret, err := crypto.Encrypt(h.masterKey, []byte(secret))
	if err != nil {
		h.logger.Error("auth: failed to encrypt TOTP secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.twoFactor.CreatePendingSecret(r.Context(), user.ID, encryptedSecret); err != nil {
		h.logger.Error("auth: failed to store pending 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(enrollResponse{Secret: secret, OtpauthURI: otpauthURI})
}

type confirm2FARequest struct {
	Code string `json:"code"`
}

const invalidConfirm2FACodeBody = `{"error":"invalid verification code"}`

// recoveryCodeCount is how many one-time recovery codes are generated on
// enrollment confirmation (spec.md Assumptions).
const recoveryCodeCount = 10

type confirm2FAResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// Confirm2FA handles POST /api/auth/2fa/confirm, validating req.Code against
// the authenticated user's pending TOTP secret. On a correct code it enables
// 2FA and issues 10 recovery codes, returned in plaintext exactly once
// (TOTP-02). On a wrong code it responds 422 without enabling 2FA or
// generating recovery codes (TOTP-03).
func (h *AuthHandler) Confirm2FA(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	var req confirm2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidConfirm2FACodeBody)
		return
	}

	pending, err := h.twoFactor.GetSecret(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAdminError(w, http.StatusUnprocessableEntity, invalidConfirm2FACodeBody)
			return
		}
		h.logger.Error("auth: failed to look up pending 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	secret, err := crypto.Decrypt(h.masterKey, pending.EncryptedSecret)
	if err != nil {
		h.logger.Error("auth: failed to decrypt pending 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	if !auth.ValidateTOTPCode(string(secret), req.Code) {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidConfirm2FACodeBody)
		return
	}

	if err := h.twoFactor.ConfirmSecret(r.Context(), user.ID); err != nil {
		h.logger.Error("auth: failed to confirm 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}

	plainCodes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range plainCodes {
		code, err := generateRecoveryCode()
		if err != nil {
			h.logger.Error("auth: failed to generate recovery code", zap.Error(err))
			writeInternalError(w)
			return
		}
		hash, err := auth.HashPassword(code)
		if err != nil {
			h.logger.Error("auth: failed to hash recovery code", zap.Error(err))
			writeInternalError(w)
			return
		}
		plainCodes[i] = code
		hashes[i] = hash
	}

	if err := h.twoFactor.CreateRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		h.logger.Error("auth: failed to store recovery codes", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(confirm2FAResponse{RecoveryCodes: plainCodes})
}

type disable2FARequest struct {
	CurrentPassword string `json:"current_password"`
}

// Disable2FA handles POST /api/auth/2fa/disable, requiring the
// authenticated user's current_password (same check ChangePassword already
// uses) before clearing their TOTP secret and every recovery code
// (TOTP-12). It is idempotent: called with no 2FA enabled it responds `200`
// as a no-op rather than erroring (TOTP-13) - disabling something already
// off is not a failure state. It does not call RevokeSessions - enabling or
// disabling 2FA is not itself evidence of a compromised password (spec.md
// Assumptions).
func (h *AuthHandler) Disable2FA(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	var req disable2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, http.StatusUnauthorized, invalidCurrentPasswordBody)
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		writeAdminError(w, http.StatusUnauthorized, invalidCurrentPasswordBody)
		return
	}

	if err := h.twoFactor.DeleteSecret(r.Context(), user.ID); err != nil {
		h.logger.Error("auth: failed to delete 2FA secret", zap.Error(err))
		writeInternalError(w)
		return
	}
	if err := h.twoFactor.DeleteRecoveryCodes(r.Context(), user.ID); err != nil {
		h.logger.Error("auth: failed to delete 2FA recovery codes", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Logout expires the vane_session cookie set at login AND revokes the
// backing sessions-table row (user-sessions spec SESS-11) - closing the
// existing gap where a copied/leaked token remained valid until its 24h
// SessionTTL expired even after the browser "logged out". The cookie
// clear and the row revoke are independent: if the DB call fails (e.g.
// transient connection error) the cookie is still cleared so the
// user-visible state matches what they expect, and the row stays
// revokable later. Revoke failures are logged but never fail the
// request - matching the rate-limiter fail-open posture on infra errors.
//
// Logout requires no role beyond being authenticated - any user can end
// their own session. The sid comes from the request context, placed
// there by RequireAuth when the token was verified.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sid, ok := SessionIDFromContext(r.Context())
	if !ok {
		// RequireAuth placed the sid; if it's missing here, the middleware
		// chain is misconfigured. Surface as 401 rather than silently
		// leaving the row alive.
		writeUnauthorized(w)
		return
	}
	if err := h.sessions.Revoke(r.Context(), sid); err != nil {
		h.logger.Warn("logout: revoke failed (cookie will still be cleared)", zap.Error(err), zap.String("session_id", sid))
	}
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
//
// Switching tenants reuses the same sessions-table row (user-sessions
// SESS-02): the new JWT carries the same sid as the old one, just with
// a different tid claim and a fresh iat. No second row is created, so
// the "Sessões ativas" list shows this device once (not once per
// tenant switch).
func (h *AuthHandler) SwitchTenant(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	sid, ok := SessionIDFromContext(r.Context())
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

	token, err := auth.IssueSessionWithTenant(user.ID, req.TenantID, sid, h.sessionSecret)
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

// recoveryCodeBytes is the amount of randomness backing each generated
// recovery code, same size class as generateResetToken's token
// (password_reset_handler.go).
const recoveryCodeBytes = 10

// generateRecoveryCode returns a random, URL-safe plaintext one-time
// recovery code, using the same crypto/rand + base64 pattern
// generateResetToken already established in this package.
func generateRecoveryCode() (string, error) {
	raw := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: failed to generate recovery code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
