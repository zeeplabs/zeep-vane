package api

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// bootstrapCreator is the subset of *db.AdminRepository BootstrapHandler
// depends on for creating the first admin.
type bootstrapCreator interface {
	BootstrapFirst(ctx context.Context, admin *db.Admin) (created bool, err error)
}

// BootstrapHandler serves the two public, unauthenticated bootstrap
// routes that let a fresh, admin-less instance create its first owner
// from the browser instead of a manual SQL insert (SHD-14 through
// SHD-18).
type BootstrapHandler struct {
	pool          *db.Pool
	admins        bootstrapCreator
	logger        *zap.Logger
	sessionSecret string
	secureCookies bool
}

// NewBootstrapHandler builds a BootstrapHandler. pool backs Status's
// existence check directly (a single COUNT query, no need for a
// dedicated repository method); admins backs Create's race-safe insert.
// sessionSecret signs the session token Create issues on success, same as
// AuthHandler. secureCookies controls the vane_session cookie's Secure
// attribute (H9), same as AuthHandler.
func NewBootstrapHandler(pool *db.Pool, admins bootstrapCreator, logger *zap.Logger, sessionSecret string, secureCookies bool) *BootstrapHandler {
	return &BootstrapHandler{pool: pool, admins: admins, logger: logger, sessionSecret: sessionSecret, secureCookies: secureCookies}
}

type bootstrapStatusResponse struct {
	Bootstrapped bool `json:"bootstrapped"`
}

// Status reports whether any admin exists yet, for the SPA's boot-time
// redirect decision (SHD-14, SHD-19). Public, unauthenticated - it must
// be reachable before any admin (and therefore any session) exists.
func (h *BootstrapHandler) Status(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := h.pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		h.logger.Error("bootstrap: failed to count admins", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(bootstrapStatusResponse{Bootstrapped: count > 0})
}

type bootstrapCreateRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

const invalidBootstrapRequestBody = `{"error":"email and password are required"}`
const alreadyBootstrappedBody = `{"error":"already bootstrapped"}`

// Create creates the first admin (owner role, by the admins table's
// existing column default) and, on success, logs them in immediately by
// setting the same session cookie Login sets (AD-004) - the new owner
// never has to log in separately after bootstrapping (SHD-16, SHD-18).
// WHILE an admin already exists, it refuses with 409 and never creates a
// second admin (SHD-15), whether that's because one existed before this
// request or because a concurrent bootstrap request won the race
// (SHD-17, enforced by AdminRepository.BootstrapFirst's table lock).
func (h *BootstrapHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req bootstrapCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeAdminError(w, http.StatusUnprocessableEntity, invalidBootstrapRequestBody)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("bootstrap: failed to hash password", zap.Error(err))
		writeInternalError(w)
		return
	}

	admin := &db.Admin{Email: req.Email, PasswordHash: hash}
	created, err := h.admins.BootstrapFirst(r.Context(), admin)
	if err != nil {
		h.logger.Error("bootstrap: failed to create first admin", zap.Error(err))
		writeInternalError(w)
		return
	}
	if !created {
		writeAdminError(w, http.StatusConflict, alreadyBootstrappedBody)
		return
	}

	token, err := auth.IssueSession(admin.ID, h.sessionSecret)
	if err != nil {
		h.logger.Error("bootstrap: failed to issue session token", zap.Error(err))
		writeInternalError(w)
		return
	}
	http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{ID: admin.ID, Email: admin.Email, Role: db.RoleOwner})
}
