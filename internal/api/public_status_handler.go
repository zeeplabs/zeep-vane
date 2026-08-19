package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// serviceLister is the subset of *db.ServiceRepository the public status
// handler depends on.
type serviceLister interface {
	List(ctx context.Context) ([]db.Service, error)
}

// latestSnapshotFetcher is the subset of *db.StatusSnapshotRepository the
// public status handler depends on.
type latestSnapshotFetcher interface {
	LatestFetchedAtByService(ctx context.Context) (map[string]time.Time, error)
}

// PublicStatusHandler serves the public, unauthenticated status page
// endpoint (SP-10). It never talks to Datadog directly - it only reads
// what the poller (Phase 4) has already persisted, so a Datadog outage
// (including the connected Integration being marked "invalid") never takes
// the public page down: it keeps serving the last snapshot on record, with
// its real fetched_at timestamp, never a fabricated "now" (SP-08, SP-09).
type PublicStatusHandler struct {
	services  serviceLister
	snapshots latestSnapshotFetcher
	logger    *zap.Logger
}

// NewPublicStatusHandler builds a PublicStatusHandler backed by services and
// snapshots.
func NewPublicStatusHandler(services serviceLister, snapshots latestSnapshotFetcher, logger *zap.Logger) *PublicStatusHandler {
	return &PublicStatusHandler{services: services, snapshots: snapshots, logger: logger}
}

type publicServiceResponse struct {
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

type publicStatusResponse struct {
	Services []publicServiceResponse `json:"services"`
}

// Get handles GET / on a published status page's hostname, returning every
// registered service's current status and the timestamp it last changed.
// It requires no authentication (SP-10).
func (h *PublicStatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	services, err := h.services.List(r.Context())
	if err != nil {
		h.logger.Error("public-status: failed to list services", zap.Error(err))
		writeInternalError(w)
		return
	}

	latestFetchedAt, err := h.snapshots.LatestFetchedAtByService(r.Context())
	if err != nil {
		h.logger.Error("public-status: failed to load latest status snapshots", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := publicStatusResponse{Services: []publicServiceResponse{}}
	for _, service := range services {
		// A service with no SLO linked yet stays "not_configured" and is
		// never shown publicly (spec.md edge case) - it's an admin-side
		// concept only, until a poller cycle gives it a real status.
		if service.CurrentStatus == "not_configured" {
			continue
		}

		// A service the poller has never successfully reached yet has no
		// entry here; LastUpdatedAt then stays the zero value rather than a
		// fabricated "now" (SP-08, SP-09, edge case in spec.md).
		resp.Services = append(resp.Services, publicServiceResponse{
			Name:          service.Name,
			Status:        service.CurrentStatus,
			LastUpdatedAt: latestFetchedAt[service.ID],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
