package api

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// tenantMembershipLister is the subset of *db.TenantMembershipRepository
// TenantHandler.Delete depends on to count the caller's remaining active
// tenants (settings-page CFGPG-11).
type tenantMembershipLister interface {
	ListForUser(ctx context.Context, userID string) ([]db.TenantMembership, error)
}

// tenantSoftDeleter is the subset of *db.TenantRepository TenantHandler
// depends on. Get is used only to fetch the tenant's name for the
// tenant_deleted audit entry (AUDITEXP-15) before SoftDelete runs.
type tenantSoftDeleter interface {
	Get(ctx context.Context, tenantID string) (*db.Tenant, error)
	SoftDelete(ctx context.Context, tenantID string) error
}

// TenantHandler serves DELETE /api/tenants/current (settings-page's "excluir
// conta" danger zone action).
type TenantHandler struct {
	memberships tenantMembershipLister
	tenants     tenantSoftDeleter
	audit       *audit.Log
	logger      *zap.Logger
}

// NewTenantHandler builds a TenantHandler.
func NewTenantHandler(memberships tenantMembershipLister, tenants tenantSoftDeleter, auditLog *audit.Log, logger *zap.Logger) *TenantHandler {
	return &TenantHandler{memberships: memberships, tenants: tenants, audit: auditLog, logger: logger}
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

	// Captured before SoftDelete runs (same reasoning as
	// StatusPagesHandler/ServicesHandler.Delete): the audit entry needs a
	// human-readable name, and a name fetched after the delete would see
	// the tenant already in its deleted state.
	var name string
	if tenant, err := h.tenants.Get(r.Context(), tenantID); err == nil {
		name = tenant.Name
	}

	if err := h.tenants.SoftDelete(r.Context(), tenantID); err != nil {
		h.logger.Error("tenants: failed to soft-delete tenant", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.audit.Record(r.Context(), user.ID, tenantID, name, "tenant_deleted"); err != nil {
		h.logger.Error("tenants: failed to record audit entry", zap.Error(err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
}
