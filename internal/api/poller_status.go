package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// pollerStatusPageSize is the fixed page size for /api/poller/status
// (spec.md Assumptions: 20 for domains/services/status-pages/
// email-providers/poller-status/admins).
const pollerStatusPageSize = 20

// integrationLister is the subset of *db.IntegrationRepository the poller
// status handler depends on.
type integrationLister interface {
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Integration, int, error)
}

// PollerStatusHandler serves the poller status route (admin-dashboard
// ADM-13/ADM-14): a read-only view of what the poller (internal/poller,
// mvp-core T22-T25) already persisted onto each Integration - no new fetch
// against the SLO provider.
type PollerStatusHandler struct {
	integrations integrationLister
	logger       *zap.Logger
}

// NewPollerStatusHandler builds a PollerStatusHandler backed by
// integrations.
func NewPollerStatusHandler(integrations integrationLister, logger *zap.Logger) *PollerStatusHandler {
	return &PollerStatusHandler{integrations: integrations, logger: logger}
}

type pollerIntegrationStatus struct {
	Provider      string     `json:"provider"`
	Status        string     `json:"status"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	LastError     *string    `json:"last_error"`
}

// List handles GET /api/poller/status (role: owner, operator, viewer). It
// reflects exactly what the poller last persisted per Integration - the
// most recent successful check's status/last_checked_at when the last
// attempt succeeded, or "invalid" with last_error populated when it didn't
// (SP-09, reused verbatim, no new logic).
func (h *PollerStatusHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	integrations, total, err := h.integrations.ListPaginated(r.Context(), page, pollerStatusPageSize)
	if err != nil {
		h.logger.Error("poller-status: failed to list integrations", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]pollerIntegrationStatus, len(integrations))
	for i, integration := range integrations {
		resp[i] = pollerIntegrationStatus{
			Provider:      integration.Provider,
			Status:        integration.Status,
			LastCheckedAt: integration.LastCheckedAt,
			LastError:     integration.LastError,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[pollerIntegrationStatus]{Items: resp, Total: total, Page: page, PageSize: pollerStatusPageSize})
}
