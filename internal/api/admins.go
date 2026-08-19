package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
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
	pool    *db.Pool
	admins  *db.AdminRepository
	invites *db.AdminInviteRepository
	audit   *audit.Log
	logger  *zap.Logger
}

// NewAdminsHandler builds an AdminsHandler.
func NewAdminsHandler(pool *db.Pool, admins *db.AdminRepository, invites *db.AdminInviteRepository, auditLog *audit.Log, logger *zap.Logger) *AdminsHandler {
	return &AdminsHandler{pool: pool, admins: admins, invites: invites, audit: auditLog, logger: logger}
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

	// Email delivery is out of scope for the MVP (same convention as the
	// password reset flow, T14 of mvp-core): the raw token is only logged,
	// standing in for a real provider. The raw token itself is never
	// persisted (see AdminInvite.TokenHash).
	h.logger.Info("admins: invite issued",
		zap.String("email", req.Email), zap.String("role", req.Role), zap.String("token", rawToken))

	if err := h.audit.Record(r.Context(), actor.ID, invite.ID, "invited"); err != nil {
		h.logger.Error("admins: failed to record invite audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "invited"})
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

	if token == "" {
		writeAdminError(w, http.StatusUnauthorized, acceptInviteErrorBody)
		return
	}

	invite, err := h.invites.GetByTokenHash(r.Context(), hashAdminInviteToken(token))
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeAdminError(w, http.StatusUnauthorized, acceptInviteErrorBody)
		return
	case err != nil:
		h.logger.Error("admins: failed to look up invite by token", zap.Error(err))
		writeInternalError(w)
		return
	}

	if invite.UsedAt != nil || time.Now().After(invite.ExpiresAt) {
		writeAdminError(w, http.StatusUnauthorized, acceptInviteErrorBody)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("admins: failed to hash invite password", zap.Error(err))
		writeInternalError(w)
		return
	}

	admin := &db.Admin{Email: invite.Email, PasswordHash: passwordHash}
	if err := h.admins.Create(r.Context(), admin); err != nil {
		h.logger.Error("admins: failed to activate invited admin", zap.Error(err))
		writeInternalError(w)
		return
	}

	// admins.Create always creates with the "owner" column default
	// (unchanged from mvp-core, T9) - apply the invite's role explicitly
	// whenever it isn't owner.
	if invite.Role != db.RoleOwner {
		if err := h.admins.UpdateRole(r.Context(), admin.ID, invite.Role); err != nil {
			h.logger.Error("admins: failed to apply invited role", zap.Error(err))
			writeInternalError(w)
			return
		}
	}

	if err := h.invites.MarkUsed(r.Context(), invite.ID); err != nil {
		h.logger.Error("admins: failed to mark invite used", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(acceptAdminInviteResponse{Email: admin.Email, Role: invite.Role})
}

type updateAdminRoleRequest struct {
	Role string `json:"role"`
}

const invalidUpdateAdminRoleRequestBody = `{"error":"role must be one of owner, operator, viewer"}`
const adminNotFoundBody = `{"error":"admin not found"}`
const adminLockoutBody = `{"error":"this action would leave zero active owners"}`

type adminResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
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
