package api

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// bootstrapCreator is the subset of *db.UserRepository BootstrapHandler
// depends on for creating the first user.
type bootstrapCreator interface {
	BootstrapFirst(ctx context.Context, user *db.User) (created bool, err error)
}

// bootstrapTenantCreator is the subset of *db.TenantRepository
// BootstrapHandler depends on for provisioning the first user's tenant
// (TENANT-05/06 - multi-tenancy-core, AD-022).
type bootstrapTenantCreator interface {
	Create(ctx context.Context, tenant *db.Tenant) error
}

// bootstrapMembershipCreator is the subset of
// *db.TenantMembershipRepository BootstrapHandler depends on for linking
// the first user to their tenant as owner.
type bootstrapMembershipCreator interface {
	Create(ctx context.Context, m *db.TenantMembership) error
}

// BootstrapHandler serves the two public, unauthenticated bootstrap
// routes that let a fresh, user-less instance create its first owner
// from the browser instead of a manual SQL insert (SHD-14 through
// SHD-18).
type BootstrapHandler struct {
	pool          *db.Pool
	users         bootstrapCreator
	tenants       bootstrapTenantCreator
	memberships   bootstrapMembershipCreator
	logger        *zap.Logger
	sessionSecret string
	secureCookies bool
}

// NewBootstrapHandler builds a BootstrapHandler. pool backs Status's
// existence check directly (a single COUNT query, no need for a
// dedicated repository method) and the tenant+membership transaction
// Create opens; users backs the first user's race-safe insert; tenants
// and memberships provision that user's single auto-created tenant
// (TENANT-05/06/07) - self-hosted's "zero new friction" contract: no
// tenant-selection UI, ever, for an account with exactly one membership.
// sessionSecret signs the session token Create issues on success, same as
// AuthHandler. secureCookies controls the vane_session cookie's Secure
// attribute (H9), same as AuthHandler.
func NewBootstrapHandler(pool *db.Pool, users bootstrapCreator, tenants bootstrapTenantCreator, memberships bootstrapMembershipCreator, logger *zap.Logger, sessionSecret string, secureCookies bool) *BootstrapHandler {
	return &BootstrapHandler{pool: pool, users: users, tenants: tenants, memberships: memberships, logger: logger, sessionSecret: sessionSecret, secureCookies: secureCookies}
}

type bootstrapStatusResponse struct {
	Bootstrapped bool `json:"bootstrapped"`
}

// Status reports whether any user exists yet, for the SPA's boot-time
// redirect decision (SHD-14, SHD-19). Public, unauthenticated - it must
// be reachable before any user (and therefore any session) exists.
func (h *BootstrapHandler) Status(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := h.pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		h.logger.Error("bootstrap: failed to count users", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(bootstrapStatusResponse{Bootstrapped: count > 0})
}

type bootstrapCreateRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

const invalidBootstrapRequestBody = `{"error":"name, email, and password are required"}`
const alreadyBootstrappedBody = `{"error":"already bootstrapped"}`
const invalidPhoneBody = `{"error":"phone is invalid"}`

// weakPasswordBody is returned by every handler that sets a new password
// (bootstrap, invite-accept, password-reset-confirm) when it fails
// auth.ValidatePassword (H11).
const weakPasswordBody = `{"error":"password must be between 8 and 72 characters"}`

// Create creates the first user, their tenant, and their owner membership
// of it, and on success logs them in immediately by setting the same
// session cookie Login sets (AD-004) - the new owner never has to log in
// separately after bootstrapping (SHD-16, SHD-18). The user is created
// already email-verified (UserRepository.BootstrapFirst): self-hosted
// bootstrap must never depend on an email provider that has not been
// configured yet. WHILE a user already exists, it refuses with 409 and
// never creates a second one (SHD-15), whether that's because one existed
// before this request or because a concurrent bootstrap request won the
// race (SHD-17, enforced by UserRepository.BootstrapFirst's table lock).
func (h *BootstrapHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req bootstrapCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Email == "" || req.Password == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidBootstrapRequestBody)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, weakPasswordBody)
		return
	}
	if err := ValidatePhone(req.Phone); err != nil {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidPhoneBody)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("bootstrap: failed to hash password", zap.Error(err))
		writeInternalError(w)
		return
	}

	user := &db.User{Email: req.Email, PasswordHash: hash, Name: req.Name, Phone: nilIfEmpty(req.Phone)}
	created, err := h.users.BootstrapFirst(r.Context(), user)
	if err != nil {
		h.logger.Error("bootstrap: failed to create first user", zap.Error(err))
		writeInternalError(w)
		return
	}
	if !created {
		writeAdminError(w, http.StatusConflict, alreadyBootstrappedBody)
		return
	}

	// Provision the new user's single auto-created tenant + owner
	// membership, atomically with each other (TENANT-05/06) - a separate
	// transaction from the user insert above (which needs its own
	// table-lock-guarded transaction for bootstrap's race-safety,
	// UserRepository.BootstrapFirst), not one all-encompassing
	// transaction; the spec requires the tenant and membership to land
	// together, not that they share a transaction with the user row
	// too. tenants' RLS policy checks its own id, so the id is generated
	// here and app.tenant_id is set to match before either insert.
	var tenantID string
	if err := h.pool.QueryRow(r.Context(), "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		h.logger.Error("bootstrap: failed to generate tenant id", zap.Error(err))
		writeInternalError(w)
		return
	}
	tx, err := h.pool.BeginTenantTx(r.Context(), user.ID, tenantID)
	if err != nil {
		h.logger.Error("bootstrap: failed to begin tenant transaction", zap.Error(err))
		writeInternalError(w)
		return
	}
	tenantCtx := db.WithTenantTx(r.Context(), tx)

	tenant := &db.Tenant{ID: tenantID, Name: req.Name, ContactEmail: req.Email}
	if err := h.tenants.Create(tenantCtx, tenant); err != nil {
		_ = tx.Rollback(r.Context())
		h.logger.Error("bootstrap: failed to create tenant", zap.Error(err))
		writeInternalError(w)
		return
	}
	if err := h.memberships.Create(tenantCtx, &db.TenantMembership{UserID: user.ID, TenantID: tenantID, Role: db.RoleOwner}); err != nil {
		_ = tx.Rollback(r.Context())
		h.logger.Error("bootstrap: failed to create owner membership", zap.Error(err))
		writeInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.logger.Error("bootstrap: failed to commit tenant transaction", zap.Error(err))
		writeInternalError(w)
		return
	}

	token, err := auth.IssueSessionWithTenant(user.ID, tenantID, h.sessionSecret)
	if err != nil {
		h.logger.Error("bootstrap: failed to issue session token", zap.Error(err))
		writeInternalError(w)
		return
	}
	http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, Phone: user.Phone, Role: db.RoleOwner,
		ActiveTenantID: tenantID, Memberships: []meMembership{{TenantID: tenantID, Role: db.RoleOwner}},
	})
}
