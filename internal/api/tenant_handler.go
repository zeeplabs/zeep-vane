package api

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// tenantMembershipLister is the subset of *db.TenantMembershipRepository
// TenantHandler.Delete depends on to count the caller's remaining active
// tenants (settings-page CFGPG-11).
type tenantMembershipLister interface {
	ListForUser(ctx context.Context, userID string) ([]db.TenantMembership, error)
}

// tenantSoftDeleter is the subset of *db.TenantRepository TenantHandler
// depends on.
type tenantSoftDeleter interface {
	SoftDelete(ctx context.Context, tenantID string) error
}

// TenantHandler serves DELETE /api/tenants/current (settings-page's "excluir
// conta" danger zone action).
type TenantHandler struct {
	memberships tenantMembershipLister
	tenants     tenantSoftDeleter
	logger      *zap.Logger
}

// NewTenantHandler builds a TenantHandler.
func NewTenantHandler(memberships tenantMembershipLister, tenants tenantSoftDeleter, logger *zap.Logger) *TenantHandler {
	return &TenantHandler{memberships: memberships, tenants: tenants, logger: logger}
}

const lastActiveTenantResponseBody = `{"error":"this is your only active account - it cannot be deleted"}`

// Delete handles DELETE /api/tenants/current: soft-deletes the session's
// active tenant (CFGPG-09), unless it is the requesting user's only active
// tenant (CFGPG-11, 409). Mounted behind ownerOnly - no additional role
// check is done here, matching every other destructive route in routes.go.
func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	tenantID, ok := ActiveTenantIDFromContext(r.Context())
	if !ok || tenantID == "" {
		writeUnauthorized(w)
		return
	}

	activeTenants, err := h.memberships.ListForUser(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("tenants: failed to list memberships before delete", zap.Error(err))
		writeInternalError(w)
		return
	}
	if len(activeTenants) <= 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(lastActiveTenantResponseBody))
		return
	}

	if err := h.tenants.SoftDelete(r.Context(), tenantID); err != nil {
		h.logger.Error("tenants: failed to soft-delete tenant", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
}
