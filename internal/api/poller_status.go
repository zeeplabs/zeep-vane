package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// pollerStatusPageSize is the fixed page size for /api/poller/status
// (spec.md Assumptions: 20 for domains/services/status-pages/
// email-providers/poller-status/admins).
const pollerStatusPageSize = 20

// pollerCheckWindow is the "checks in the last minute" window
// (poller-status-real-state POLLST-05).
const pollerCheckWindow = 60 * time.Second

// integrationLister is the subset of *db.IntegrationRepository the poller
// status handler depends on.
type integrationLister interface {
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Integration, int, error)
	GetDatadog(ctx context.Context) (*db.Integration, error)
}

// pollerLeaderReader is the subset of *db.PollerLeadershipRepository the
// poller status handler depends on.
type pollerLeaderReader interface {
	CurrentLeader(ctx context.Context) (*db.PollerLeader, error)
}

// checkCounter is the subset of *db.StatusIntervalRepository the poller
// status handler depends on.
type checkCounter interface {
	CountUpdatedSince(ctx context.Context, since time.Time) (int, error)
}

// PollerStatusHandler serves the poller status route (poller-status-real-state):
// a read-only view of the real, live poller leadership state (queried
// directly from Postgres, never persisted separately) plus what the poller
// (internal/poller) already persisted onto each Integration and
// status_intervals row - no new fetch against the SLO provider.
type PollerStatusHandler struct {
	integrations integrationLister
	leadership   pollerLeaderReader
	intervals    checkCounter
	logger       *zap.Logger
}

// NewPollerStatusHandler builds a PollerStatusHandler backed by
// integrations, leadership, and intervals.
func NewPollerStatusHandler(integrations integrationLister, leadership pollerLeaderReader, intervals checkCounter, logger *zap.Logger) *PollerStatusHandler {
	return &PollerStatusHandler{integrations: integrations, leadership: leadership, intervals: intervals, logger: logger}
}

type pollerIntegrationStatus struct {
	Provider      string     `json:"provider"`
	Status        string     `json:"status"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	LastError     *string    `json:"last_error"`
}

// pollerReplica identifies the replica currently holding poller leadership
// (POLLST-01).
type pollerReplica struct {
	ApplicationName string    `json:"application_name"`
	BackendStart    time.Time `json:"backend_start"`
}

// pollerStatusResponse carries non-list fields (leadership state, checks
// count) loose alongside the paginated integrations list, per this
// codebase's convention for endpoints mixing list and non-list fields
// (AGENTS.md: same pattern as email-providers' active_provider) rather than
// forcing the generic Page[T] wrapper.
type pollerStatusResponse struct {
	LeaderElected    bool                      `json:"leader_elected"`
	PollerRunning    bool                      `json:"poller_running"`
	Replica          *pollerReplica            `json:"replica"`
	ChecksLastMinute int                       `json:"checks_last_minute"`
	Items            []pollerIntegrationStatus `json:"items"`
	Total            int                       `json:"total"`
	Page             int                       `json:"page"`
	PageSize         int                       `json:"page_size"`
}

// List handles GET /api/poller/status (role: owner, operator, viewer). It
// reports live poller leadership state (queried directly against
// pg_locks/pg_stat_activity on every call - POLLST-05), each connected
// integration's persisted status/last_error/last_checked_at unchanged from
// before (POLLST-06), and real per-tenant polling activity from
// status_intervals (POLLST-05).
func (h *PollerStatusHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)
	ctx := r.Context()

	integrations, total, err := h.integrations.ListPaginated(ctx, page, pollerStatusPageSize)
	if err != nil {
		h.logger.Error("poller-status: failed to list integrations", zap.Error(err))
		writeInternalError(w)
		return
	}

	leader, err := h.leadership.CurrentLeader(ctx)
	if err != nil {
		h.logger.Error("poller-status: failed to query poller leadership", zap.Error(err))
		writeInternalError(w)
		return
	}

	checksLastMinute, err := h.intervals.CountUpdatedSince(ctx, time.Now().Add(-pollerCheckWindow))
	if err != nil {
		h.logger.Error("poller-status: failed to count recent status intervals", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := pollerStatusResponse{
		ChecksLastMinute: checksLastMinute,
		Items:            make([]pollerIntegrationStatus, len(integrations)),
		Total:            total,
		Page:             page,
		PageSize:         pollerStatusPageSize,
	}
	for i, integration := range integrations {
		resp.Items[i] = pollerIntegrationStatus{
			Provider:      integration.Provider,
			Status:        integration.Status,
			LastCheckedAt: integration.LastCheckedAt,
			LastError:     integration.LastError,
		}
	}

	if leader != nil {
		resp.LeaderElected = true
		resp.Replica = &pollerReplica{ApplicationName: leader.ApplicationName, BackendStart: leader.BackendStart}

		// poller_running additionally requires a stored Datadog integration
		// (POLLST-02/03): RunLeaderLoop acquires the lock unconditionally,
		// but Restart is a no-op until one exists (mirrors
		// newPollerFromStoredIntegration's own "started" contract).
		if _, err := h.integrations.GetDatadog(ctx); err == nil {
			resp.PollerRunning = true
		} else if !errors.Is(err, db.ErrNotFound) {
			h.logger.Error("poller-status: failed to check stored datadog integration", zap.Error(err))
			writeInternalError(w)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
