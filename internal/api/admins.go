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
	"github.com/jackc/pgx/v5"
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

// AdminsHandler serves the admin management routes: invite, invite
// acceptance, role change, removal, and listing. It takes *db.Pool directly
// (unlike other handlers' narrow repository interfaces) because
// UpdateRole/Delete need to run CountActiveOwners and the resulting write in
// the same transaction (design.md Risks & Concerns - lockout check must be
// atomic), which isn't expressible through the existing per-repository
// interfaces.
type AdminsHandler struct {
	pool            *db.Pool
	admins          *db.AdminRepository
	invites         *db.AdminInviteRepository
	emailSvc        *email.Service
	companySettings *db.CompanySettingsRepository
	audit           *audit.Log
	logger          *zap.Logger
	devTokenLogging bool
	httpsEnabled    bool
}

// NewAdminsHandler builds an AdminsHandler. devTokenLogging gates whether
// the raw invite token is logged when an invite is created (see the
// PasswordResetHandler doc for why this defaults to off). httpsEnabled
// (mirrors cfg.HTTPSEnabled) picks the scheme used to build the invite
// AcceptURL sent by email.
func NewAdminsHandler(pool *db.Pool, admins *db.AdminRepository, invites *db.AdminInviteRepository, emailSvc *email.Service, companySettings *db.CompanySettingsRepository, auditLog *audit.Log, logger *zap.Logger, devTokenLogging, httpsEnabled bool) *AdminsHandler {
	return &AdminsHandler{
		pool: pool, admins: admins, invites: invites,
		emailSvc: emailSvc, companySettings: companySettings,
		audit: auditLog, logger: logger,
		devTokenLogging: devTokenLogging, httpsEnabled: httpsEnabled,
	}
}

// sendAdminInviteEmail looks up the company display name and sends the
// admin-invite email for rawToken via h.emailSvc, returning whether the send
// succeeded. It never returns an error to the caller - a lookup or send
// failure is logged and treated as email_sent:false, matching the
// non-blocking convention (spec.md: invite/resend must never fail on email).
func (h *AdminsHandler) sendAdminInviteEmail(r *http.Request, inviteID, to, role, rawToken string) bool {
	settings, err := h.companySettings.Get(r.Context())
	if err != nil {
		h.logger.Error("admins: failed to load company settings for invite email", zap.String("invite_id", inviteID), zap.Error(err))
		return false
	}

	scheme := "http"
	if h.httpsEnabled {
		scheme = "https"
	}
	data := email.AdminInviteEmailData{
		CompanyName: settings.Name,
		Role:        role,
		AcceptURL:   fmt.Sprintf("%s://%s/accept-invite/%s", scheme, r.Host, rawToken),
	}

	if err := h.emailSvc.SendAdminInvite(r.Context(), to, data); err != nil {
		h.logger.Error("admins: failed to send admin invite email", zap.String("invite_id", inviteID), zap.Error(err))
		return false
	}

	return true
}

// wouldLeaveZeroOwners is the ADM-06 lockout decision: true when applying
// an action to an admin currently holding the owner role, that does not
// keep them an owner, would leave the system with zero active owners
// (ownerCount here already includes the target admin themselves, since
// CountActiveOwners counts every admin currently holding the owner role).
// It applies identically to a role change (keepsOwnerRole = new role ==
// owner) and a removal (keepsOwnerRole = false, always), including the
// owner acting on themselves.
func wouldLeaveZeroOwners(currentRole string, keepsOwnerRole bool, ownerCount int) bool {
	return currentRole == db.RoleOwner && !keepsOwnerRole && ownerCount <= 1
}

func isValidAdminRole(role string) bool {
	switch role {
	case db.RoleOwner, db.RoleOperator, db.RoleViewer:
		return true
	default:
		return false
	}
}

type inviteAdminRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

const invalidInviteAdminRequestBody = `{"error":"email is required and role must be one of owner, operator, viewer"}`
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
	actor, ok := AdminFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	var req inviteAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || !isValidAdminRole(req.Role) {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidInviteAdminRequestBody)
		return
	}

	if _, err := h.admins.GetByEmail(r.Context(), req.Email); err == nil {
		writeAdminError(w, http.StatusConflict, adminAlreadyActiveBody)
		return
	} else if !errors.Is(err, db.ErrNotFound) {
		h.logger.Error("admins: failed to look up admin by email", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.invites.InvalidatePendingForEmail(r.Context(), req.Email); err != nil {
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

	invite := &db.AdminInvite{
		Email:       req.Email,
		Role:        req.Role,
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

	// CreateWithRole sets the invite's actual role in the same INSERT
	// (M12) - no longer a separate UpdateRole call that could leave the
	// account stuck on the admins.role column's default (owner) if it
	// never ran.
	admin := &db.Admin{Email: invite.Email, PasswordHash: passwordHash}
	if err := h.admins.CreateWithRole(r.Context(), admin, invite.Role); err != nil {
		h.logger.Error("admins: failed to activate invited admin", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(acceptAdminInviteResponse{Email: admin.Email, Role: invite.Role})
}

const inviteNotFoundBody = `{"error":"invite not found"}`

// ResendInvite handles POST /api/admins/invites/{id}/resend (role: owner).
// It mints a fresh token, extends the invite's expiry by another
// adminInviteTTL, and re-sends the invite email - invalidating the old
// token in the same atomic update (Refresh). Works on an expired-but-unused
// invite exactly like a not-yet-expired one (spec P2/P1 resend story); an
// unknown, already-accepted, or already-canceled id gets 404 (INVITE-03,
// INVITE-04, INVITE-08, INVITE-09).
func (h *AdminsHandler) ResendInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor, ok := AdminFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	rawToken, err := generateAdminInviteToken()
	if err != nil {
		h.logger.Error("admins: failed to generate resend token", zap.Error(err))
		writeInternalError(w)
		return
	}

	invite, err := h.invites.Refresh(r.Context(), id, hashAdminInviteToken(rawToken), time.Now().Add(adminInviteTTL))
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
// needed there). An unknown, already-accepted, or already-canceled id gets
// 404 (INVITE-05, INVITE-06, INVITE-09).
func (h *AdminsHandler) CancelInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor, ok := AdminFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	if err := h.invites.Cancel(r.Context(), id); err != nil {
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
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// UpdateRole handles PATCH /api/admins/{id}/role (role: owner). The role
// change and the lockout check run inside a single transaction using
// CountActiveOwners' SELECT ... FOR UPDATE (design.md Risks & Concerns), so
// a concurrent request can't slip in between the count and the write. A
// change that would leave zero active owners - including the owner
// demoting themselves - is rejected with 409 and no state changes (ADM-06).
// A successful change revokes the affected admin's sessions immediately
// (ADM-05) and records a "role_changed" audit entry (ADM-08).
func (h *AdminsHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	actor, ok := AdminFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	var req updateAdminRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !isValidAdminRole(req.Role) {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidUpdateAdminRoleRequestBody)
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.logger.Error("admins: failed to begin role-change transaction", zap.Error(err))
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentRole string
	row := tx.QueryRow(ctx, "SELECT role FROM admins WHERE id = $1 FOR UPDATE", targetID)
	if err := row.Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAdminError(w, http.StatusNotFound, adminNotFoundBody)
			return
		}
		h.logger.Error("admins: failed to load target admin", zap.Error(err))
		writeInternalError(w)
		return
	}

	ownerCount, err := h.admins.CountActiveOwners(ctx, tx)
	if err != nil {
		h.logger.Error("admins: failed to count active owners", zap.Error(err))
		writeInternalError(w)
		return
	}

	if wouldLeaveZeroOwners(currentRole, req.Role == db.RoleOwner, ownerCount) {
		writeAdminError(w, http.StatusConflict, adminLockoutBody)
		return
	}

	if _, err := tx.Exec(ctx, "UPDATE admins SET role = $1 WHERE id = $2", req.Role, targetID); err != nil {
		h.logger.Error("admins: failed to update admin role", zap.Error(err))
		writeInternalError(w)
		return
	}
	if _, err := tx.Exec(ctx, "UPDATE admins SET sessions_revoked_at = now() WHERE id = $1", targetID); err != nil {
		h.logger.Error("admins: failed to revoke admin sessions", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("admins: failed to commit role change", zap.Error(err))
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

// Delete handles DELETE /api/admins/{id} (role: owner). Same atomic lockout
// protection as UpdateRole: the count and the removal run in one
// transaction, rejecting with 409 if removing this admin would leave zero
// active owners (ADM-06). A successful removal revokes the admin's
// sessions and deletes the account (ADM-07), and records a "removed" audit
// entry (ADM-08).
func (h *AdminsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	actor, ok := AdminFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.logger.Error("admins: failed to begin removal transaction", zap.Error(err))
		writeInternalError(w)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentRole string
	row := tx.QueryRow(ctx, "SELECT role FROM admins WHERE id = $1 FOR UPDATE", targetID)
	if err := row.Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAdminError(w, http.StatusNotFound, adminNotFoundBody)
			return
		}
		h.logger.Error("admins: failed to load target admin", zap.Error(err))
		writeInternalError(w)
		return
	}

	ownerCount, err := h.admins.CountActiveOwners(ctx, tx)
	if err != nil {
		h.logger.Error("admins: failed to count active owners", zap.Error(err))
		writeInternalError(w)
		return
	}

	if wouldLeaveZeroOwners(currentRole, false, ownerCount) {
		writeAdminError(w, http.StatusConflict, adminLockoutBody)
		return
	}

	if _, err := tx.Exec(ctx, "UPDATE admins SET sessions_revoked_at = now() WHERE id = $1", targetID); err != nil {
		h.logger.Error("admins: failed to revoke admin sessions", zap.Error(err))
		writeInternalError(w)
		return
	}
	if _, err := tx.Exec(ctx, "DELETE FROM admins WHERE id = $1", targetID); err != nil {
		h.logger.Error("admins: failed to delete admin", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("admins: failed to commit admin removal", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.audit.Record(ctx, actor.ID, targetID, "removed"); err != nil {
		h.logger.Error("admins: failed to record removal audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
}

// List handles GET /api/admins (role: owner), returning every active
// admin's email and current role merged with pending admin invites - not
// yet accepted and not expired - each item tagged with Status ("active" or
// "pending") (AF-38). ADM-09 scopes this to owner via router-level
// RequireRole, not a check here.
func (h *AdminsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := h.pool.Query(ctx, "SELECT id, email, role FROM admins ORDER BY email")
	if err != nil {
		h.logger.Error("admins: failed to list admins", zap.Error(err))
		writeInternalError(w)
		return
	}
	defer rows.Close()

	list := []adminResponse{}
	for rows.Next() {
		var item adminResponse
		if err := rows.Scan(&item.ID, &item.Email, &item.Role); err != nil {
			h.logger.Error("admins: failed to scan admin row", zap.Error(err))
			writeInternalError(w)
			return
		}
		item.Status = "active"
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("admins: failed reading admin rows", zap.Error(err))
		writeInternalError(w)
		return
	}

	invites, err := h.invites.List(ctx)
	if err != nil {
		h.logger.Error("admins: failed to list pending invites", zap.Error(err))
		writeInternalError(w)
		return
	}
	for _, invite := range invites {
		list = append(list, adminResponse{
			ID:        invite.ID,
			Email:     invite.Email,
			Role:      invite.Role,
			Status:    "pending",
			ExpiresAt: &invite.ExpiresAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(list)
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
