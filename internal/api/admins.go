package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// adminInviteTTL is how long an admin invite token stays valid (ADM-01).
const adminInviteTTL = 1 * time.Hour

// adminInviteTokenBytes is the size of the raw random invite token before
// encoding, chosen to make guessing infeasible (same size as the password
// reset token).
const adminInviteTokenBytes = 32

// AdminsHandler serves the tenant member-management routes: invite, invite
// acceptance, role change, removal, and listing. It takes *db.Pool directly
// (unlike other handlers' narrow repository interfaces) because List joins
// users to tenant_memberships, which isn't expressible through the
// existing per-repository interfaces.
type AdminsHandler struct {
	pool            *db.Pool
	users           *db.UserRepository
	memberships     *db.TenantMembershipRepository
	invites         *db.TenantInviteRepository
	emailSvc        *email.Service
	tenants         *db.TenantRepository
	audit           *audit.Log
	logger          *zap.Logger
	devTokenLogging bool
	adminBaseURL    string
	sessionSecret   string
	secureCookies   bool
}

// NewAdminsHandler builds an AdminsHandler. devTokenLogging gates whether
// the raw invite token is logged when an invite is created (see the
// PasswordResetHandler doc for why this defaults to off). adminBaseURL
// (cfg.AdminBaseURL) is the scheme+host the invite AcceptURL sent by email
// is built from - never the incoming request's Host header, which is
// attacker-controlled (see adminBaseURL() in admin_base_url.go for why).
// sessionSecret/secureCookies authenticate the admin created by
// AcceptInvite, same pair AuthHandler/BootstrapHandler already take.
func NewAdminsHandler(pool *db.Pool, users *db.UserRepository, memberships *db.TenantMembershipRepository, invites *db.TenantInviteRepository, emailSvc *email.Service, tenants *db.TenantRepository, auditLog *audit.Log, logger *zap.Logger, devTokenLogging bool, adminBaseURL string, sessionSecret string, secureCookies bool) *AdminsHandler {
	return &AdminsHandler{
		pool: pool, users: users, memberships: memberships, invites: invites,
		emailSvc: emailSvc, tenants: tenants,
		audit: auditLog, logger: logger,
		devTokenLogging: devTokenLogging, adminBaseURL: adminBaseURL,
		sessionSecret: sessionSecret, secureCookies: secureCookies,
	}
}

// sendAdminInviteEmail looks up the company display name and sends the
// admin-invite email for rawToken via h.emailSvc, returning whether the send
// succeeded. It never returns an error to the caller - a lookup or send
// failure is logged and treated as email_sent:false, matching the
// non-blocking convention (spec.md: invite/resend must never fail on email).
func (h *AdminsHandler) sendAdminInviteEmail(r *http.Request, inviteID, to, role, rawToken string) bool {
	tenant, err := h.tenants.Active(r.Context())
	if err != nil {
		h.logger.Error("admins: failed to load tenant for invite email", zap.String("invite_id", inviteID), zap.Error(err))
		return false
	}

	data := email.AdminInviteEmailData{
		CompanyName: tenant.Name,
		Role:        role,
		AcceptURL:   fmt.Sprintf("%s/accept-invite/%s", adminBaseURL(h.adminBaseURL), rawToken),
	}

	if err := h.emailSvc.SendAdminInvite(r.Context(), to, data); err != nil {
		h.logger.Error("admins: failed to send admin invite email", zap.String("invite_id", inviteID), zap.Error(err))
		return false
	}

	return true
}

// The ADM-06 lockout decision (never leave a tenant with zero owners) now
// lives in db.TenantMembershipRepository, which owns the FOR UPDATE
// owner-count and returns db.ErrLastOwner - the check and the write have
// to be atomic (design.md Risks & Concerns), which only the repository can
// guarantee now that a role is a tenant_memberships row rather than an
// admins column.

func isValidAdminRole(role string) bool {
	switch role {
	case db.RoleOwner, db.RoleOperator, db.RoleViewer:
		return true
	default:
		return false
	}
}

type inviteAdminRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
}

const invalidInviteAdminRequestBody = `{"error":"name and email are required, and role must be one of owner, operator, viewer"}`
const adminAlreadyActiveBody = `{"error":"an active admin already exists for this email"}`

// Invite handles POST /api/admins (role: owner). It rejects an email that
// already belongs to an active admin (spec.md edge case), otherwise
// invalidates any pending invite for the same email (ADM-02) before issuing
// a new one, and records an "invited" audit entry (ADM-08).
//
// SPEC_DEVIATION: spec.md AC1 says inviting "cria o registro do admin em
// estado pending", but design.md (already implemented in T1-T4) models no
// pending Admin row and no status column - only a separate admin_invites
// row. The Admin row is created only at accept time (AcceptInvite). This
// keeps this handler consistent with the schema already committed for this
// feature; the "already active" edge case is served by checking for an
// existing Admin row by email instead of a pending-status Admin row.
//
// SPEC_DEVIATION: admin_audit_log.target_id is NOT NULL and there is no
// Admin row yet for an invited email, so the "invited" audit entry uses the
// AdminInvite's own ID as target_id rather than an Admin ID.
func (h *AdminsHandler) Invite(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}
	tenantID, _ := ActiveTenantIDFromContext(r.Context())

	var req inviteAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Email == "" || !isValidAdminRole(req.Role) {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidInviteAdminRequestBody)
		return
	}
	if err := ValidatePhone(req.Phone); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidPhoneBody)
		return
	}

	if _, err := h.users.GetByEmail(r.Context(), req.Email); err == nil {
		writeAdminError(w, http.StatusConflict, adminAlreadyActiveBody)
		return
	} else if !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("admins: failed to look up user by email", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.invites.InvalidatePendingForEmail(r.Context(), tenantID, req.Email); err != nil {
		h.logger.Error("admins: failed to invalidate pending invites", zap.Error(err))
		writeInternalError(w)
		return
	}

	rawToken, err := generateAdminInviteToken()
	if err != nil {
		h.logger.Error("admins: failed to generate invite token", zap.Error(err))
		writeInternalError(w)
		return
	}

	invite := &db.TenantInvite{
		TenantID:    tenantID,
		Email:       req.Email,
		Role:        req.Role,
		Name:        req.Name,
		Phone:       nilIfEmpty(req.Phone),
		TokenHash:   hashAdminInviteToken(rawToken),
		InvitedByID: actor.ID,
		ExpiresAt:   time.Now().Add(adminInviteTTL),
	}
	if err := h.invites.Create(r.Context(), invite); err != nil {
		h.logger.Error("admins: failed to create invite", zap.Error(err))
		writeInternalError(w)
		return
	}

	// The raw token grants account creation for the invited role, so it is
	// only logged when VANE_DEV_TOKEN_LOGGING=true is explicitly set (see
	// PasswordResetHandler.Request for why). The raw token itself is never
	// persisted (see AdminInvite.TokenHash) or included in this response -
	// it only ever reaches the invitee via the email sent below.
	if h.devTokenLogging {
		h.logger.Info("admins: invite issued",
			zap.String("email", req.Email), zap.String("role", req.Role), zap.String("token", rawToken))
	} else {
		h.logger.Info("admins: invite issued",
			zap.String("email", req.Email), zap.String("role", req.Role))
	}

	emailSent := h.sendAdminInviteEmail(r, invite.ID, req.Email, req.Role, rawToken)

	if err := h.audit.Record(r.Context(), actor.ID, invite.ID, "invited"); err != nil {
		h.logger.Error("admins: failed to record invite audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "invited", "email_sent": emailSent})
}

type acceptAdminInviteRequest struct {
	Password string `json:"password"`
}

const invalidAcceptInviteRequestBody = `{"error":"password is required"}`
const acceptInviteErrorBody = `{"error":"invalid or expired invite token"}`

type acceptAdminInviteResponse struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// AcceptInvite handles POST /api/admins/invite/{token}/accept (public). A
// missing, expired, or already-used token is rejected with 401 (ADM-04)
// without altering any state. A valid token creates the Admin account with
// the role the invite specified (ADM-03) and marks the invite used.
func (h *AdminsHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	var req acceptAdminInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidAcceptInviteRequestBody)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, weakPasswordBody)
		return
	}

	if token == "" {
		writeAdminError(w, http.StatusUnauthorized, acceptInviteErrorBody)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("admins: failed to hash invite password", zap.Error(err))
		writeInternalError(w)
		return
	}

	// ClaimForUse atomically checks unused+unexpired and marks the invite
	// used in one statement (M12/L24) - unlike the old GetByTokenHash +
	// in-Go check + later MarkUsed sequence, two concurrent requests for
	// the same token can no longer both pass the check before either
	// claims it.
	invite, err := h.invites.ClaimForUse(r.Context(), hashAdminInviteToken(token))
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeAdminError(w, http.StatusUnauthorized, acceptInviteErrorBody)
		return
	case err != nil:
		h.logger.Error("admins: failed to claim invite", zap.Error(err))
		writeInternalError(w)
		return
	}

	// The account is created already email-verified: following the invite
	// link is itself proof the invitee controls the address it was sent
	// to, so a second verification round trip would prove nothing.
	verifiedAt := time.Now()
	user := &db.User{Email: invite.Email, PasswordHash: passwordHash, Name: invite.Name, Phone: invite.Phone, EmailVerifiedAt: &verifiedAt}
	if err := h.users.Create(r.Context(), user); err != nil {
		h.logger.Error("admins: failed to create invited user", zap.Error(err))
		writeInternalError(w)
		return
	}

	// The membership carries the invite's role (tenant_memberships.role -
	// a role is per tenant since multi-tenancy-core). This route is public,
	// ahead of the tenant-context middleware, so it opens its own
	// transaction with app.tenant_id set to the invite's tenant, which is
	// what the membership's RLS WITH CHECK requires.
	tx, err := h.pool.BeginTenantTx(r.Context(), user.ID, invite.TenantID)
	if err != nil {
		h.logger.Error("admins: failed to begin tenant transaction for invite acceptance", zap.Error(err))
		writeInternalError(w)
		return
	}
	membership := &db.TenantMembership{UserID: user.ID, TenantID: invite.TenantID, Role: invite.Role}
	if err := h.memberships.Create(db.WithTenantTx(r.Context(), tx), membership); err != nil {
		_ = tx.Rollback(r.Context())
		h.logger.Error("admins: failed to create membership for invited user", zap.Error(err))
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("admins: failed to commit invite acceptance", zap.Error(err))
		writeInternalError(w)
		return
	}

	// Authenticate the newly created user immediately (accept-invite-page
	// AIP-01/02) - same issue-then-set-cookie sequence
	// BootstrapHandler.Create already uses, so the invitee lands on an
	// active session without a separate login step, already scoped to the
	// tenant that invited them.
	sessionToken, err := auth.IssueSessionWithTenant(user.ID, invite.TenantID, h.sessionSecret)
	if err != nil {
		h.logger.Error("admins: failed to issue session after invite acceptance", zap.Error(err))
		writeInternalError(w)
		return
	}
	http.SetCookie(w, sessionCookie(sessionToken, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(acceptAdminInviteResponse{Email: user.Email, Role: invite.Role})
}

const inviteNotFoundBody = `{"error":"invite not found"}`

// ResendInvite handles POST /api/admins/invites/{id}/resend (role: owner).
// It mints a fresh token, extends the invite's expiry by another
// adminInviteTTL, and re-sends the invite email - invalidating the old
// token in the same atomic update (Refresh). Works on an expired-but-unused
// invite exactly like a not-yet-expired one (spec P2/P1 resend story); an
// unknown, already-accepted, already-canceled, or another tenant's id gets
// 404 (INVITE-03, INVITE-04, INVITE-08, INVITE-09, TENANT-14/17).
func (h *AdminsHandler) ResendInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}
	tenantID, _ := ActiveTenantIDFromContext(r.Context())

	rawToken, err := generateAdminInviteToken()
	if err != nil {
		h.logger.Error("admins: failed to generate resend token", zap.Error(err))
		writeInternalError(w)
		return
	}

	invite, err := h.invites.Refresh(r.Context(), tenantID, id, hashAdminInviteToken(rawToken), time.Now().Add(adminInviteTTL))
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, inviteNotFoundBody)
		return
	case err != nil:
		h.logger.Error("admins: failed to refresh invite", zap.String("invite_id", id), zap.Error(err))
		writeInternalError(w)
		return
	}

	emailSent := h.sendAdminInviteEmail(r, invite.ID, invite.Email, invite.Role, rawToken)

	if err := h.audit.Record(r.Context(), actor.ID, invite.ID, "resent"); err != nil {
		h.logger.Error("admins: failed to record resend audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "resent", "email_sent": emailSent})
}

// CancelInvite handles DELETE /api/admins/invites/{id} (role: owner). It
// marks the invite used (without creating an admin account), so its token
// is subsequently rejected by AcceptInvite exactly like an already-used one
// (falls out of ClaimForUse's existing WHERE used_at IS NULL - no change
// needed there). An unknown, already-accepted, already-canceled, or another
// tenant's id gets 404 (INVITE-05, INVITE-06, INVITE-09, TENANT-14/17).
func (h *AdminsHandler) CancelInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}
	tenantID, _ := ActiveTenantIDFromContext(r.Context())

	if err := h.invites.Cancel(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAdminError(w, http.StatusNotFound, inviteNotFoundBody)
			return
		}
		h.logger.Error("admins: failed to cancel invite", zap.String("invite_id", id), zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.audit.Record(r.Context(), actor.ID, id, "canceled"); err != nil {
		h.logger.Error("admins: failed to record cancel audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "canceled"})
}

type updateAdminRoleRequest struct {
	Role string `json:"role"`
}

const invalidUpdateAdminRoleRequestBody = `{"error":"role must be one of owner, operator, viewer"}`
const adminNotFoundBody = `{"error":"admin not found"}`
const adminLockoutBody = `{"error":"this action would leave zero active owners"}`

type adminResponse struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      string     `json:"name,omitempty"`
	Phone     *string    `json:"phone,omitempty"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Expired   bool       `json:"expired,omitempty"`
}

// UpdateRole handles PATCH /api/admins/{id}/role (role: owner). It changes
// the target's role in the caller's active tenant - a role is a
// tenant_memberships row since multi-tenancy-core, so there is no
// installation-wide role to change. The lockout check and the write are
// atomic inside TenantMembershipRepository.UpdateRole (design.md Risks &
// Concerns), which refuses with db.ErrLastOwner - answered here with 409
// and no state change - when the change would leave the tenant with zero
// owners, including an owner demoting themselves (ADM-06). A successful
// change revokes the affected user's sessions immediately (ADM-05) and
// records a "role_changed" audit entry (ADM-08).
func (h *AdminsHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}
	tenantID, _ := ActiveTenantIDFromContext(r.Context())

	var req updateAdminRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !isValidAdminRole(req.Role) {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidUpdateAdminRoleRequestBody)
		return
	}

	ctx := r.Context()
	err := h.memberships.UpdateRole(ctx, targetID, tenantID, req.Role)
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, adminNotFoundBody)
		return
	case errors.Is(err, db.ErrLastOwner):
		writeAdminError(w, http.StatusConflict, adminLockoutBody)
		return
	case err != nil:
		h.logger.Error("admins: failed to update membership role", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.users.RevokeSessions(ctx, targetID); err != nil {
		h.logger.Error("admins: failed to revoke user sessions", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.audit.Record(ctx, actor.ID, targetID, "role_changed"); err != nil {
		h.logger.Error("admins: failed to record role-change audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(adminResponse{ID: targetID, Role: req.Role})
}

// Delete handles DELETE /api/admins/{id} (role: owner). It removes the
// target's membership of the caller's active tenant, with the same atomic
// lockout protection as UpdateRole: 409 and no state change if that would
// leave the tenant with zero owners (ADM-06). The account row itself is
// deleted only when that was the target's last membership anywhere -
// preserving the pre-multi-tenancy behaviour of "removing an admin removes
// the account" (ADM-07) without destroying an account that still belongs
// to another tenant. Either way the target's sessions are revoked, and a
// "removed" audit entry is recorded (ADM-08).
func (h *AdminsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}
	tenantID, _ := ActiveTenantIDFromContext(r.Context())

	ctx := r.Context()
	err := h.memberships.Delete(ctx, targetID, tenantID)
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, adminNotFoundBody)
		return
	case errors.Is(err, db.ErrLastOwner):
		writeAdminError(w, http.StatusConflict, adminLockoutBody)
		return
	case err != nil:
		h.logger.Error("admins: failed to remove membership", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.users.RevokeSessions(ctx, targetID); err != nil && !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("admins: failed to revoke user sessions", zap.Error(err))
		writeInternalError(w)
		return
	}

	remaining, err := h.memberships.CountForUser(ctx, targetID)
	if err != nil {
		h.logger.Error("admins: failed to count remaining memberships", zap.Error(err))
		writeInternalError(w)
		return
	}
	if remaining == 0 {
		if err := h.users.Delete(ctx, targetID); err != nil && !errors.Is(err, db.ErrNotFound) {
			h.logger.Error("admins: failed to delete user", zap.Error(err))
			writeInternalError(w)
			return
		}
	}

	if err := h.audit.Record(ctx, actor.ID, targetID, "removed"); err != nil {
		h.logger.Error("admins: failed to record removal audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
}

// adminsPageSize is the fixed page size for GET /api/admins (spec.md
// Assumptions: 20 for admins/domains/services/status-pages/
// email-providers/poller-status).
const adminsPageSize = 20

// List handles GET /api/admins (role: owner), returning one page (PAG-08,
// page_size 20) of every member of the caller's active tenant - email and
// role, the latter read from their tenant_membership - merged with
// pending admin invites - not yet accepted and not expired - each item
// tagged with Status ("active" or "pending") (AF-38). The merge itself is
// unbounded (both queries still fetch everything); only the resulting
// slice is paginated in Go, per spec Assumption - no repository signature
// change. ADM-09 scopes this to owner via router-level RequireRole, not a
// check here.
func (h *AdminsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := parsePage(r)

	tenantID, _ := ActiveTenantIDFromContext(ctx)

	rows, err := h.pool.Query(ctx,
		`SELECT u.id, u.email, u.name, u.phone, m.role
		 FROM tenant_memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.tenant_id = $1
		 ORDER BY u.email`, tenantID)
	if err != nil {
		h.logger.Error("admins: failed to list tenant members", zap.Error(err))
		writeInternalError(w)
		return
	}
	defer rows.Close()

	list := []adminResponse{}
	for rows.Next() {
		var item adminResponse
		if err := rows.Scan(&item.ID, &item.Email, &item.Name, &item.Phone, &item.Role); err != nil {
			h.logger.Error("admins: failed to scan tenant member row", zap.Error(err))
			writeInternalError(w)
			return
		}
		item.Status = "active"
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("admins: failed reading tenant member rows", zap.Error(err))
		writeInternalError(w)
		return
	}

	invites, err := h.invites.List(ctx, tenantID)
	if err != nil {
		h.logger.Error("admins: failed to list pending invites", zap.Error(err))
		writeInternalError(w)
		return
	}
	for _, invite := range invites {
		list = append(list, adminResponse{
			ID:        invite.ID,
			Email:     invite.Email,
			Name:      invite.Name,
			Phone:     invite.Phone,
			Role:      invite.Role,
			Status:    "pending",
			ExpiresAt: &invite.ExpiresAt,
			Expired:   invite.ExpiresAt.Before(time.Now()),
		})
	}

	total := len(list)
	start := (page - 1) * adminsPageSize
	items := []adminResponse{}
	if start < total {
		end := start + adminsPageSize
		if end > total {
			end = total
		}
		items = list[start:end]
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[adminResponse]{Items: items, Total: total, Page: page, PageSize: adminsPageSize})
}

func writeAdminError(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func generateAdminInviteToken() (string, error) {
	raw := make([]byte, adminInviteTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashAdminInviteToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
