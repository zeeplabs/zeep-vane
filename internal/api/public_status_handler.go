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

// PublicStatusHandler serves the public, unauthenticated status page
// endpoint (SP-10). It never talks to Datadog directly - it only reads
// what the poller (Phase 4) has already persisted, so a Datadog outage
// never takes the public page down with it.
type PublicStatusHandler struct {
	services serviceLister
	logger   *zap.Logger
}

// NewPublicStatusHandler builds a PublicStatusHandler backed by services.
func NewPublicStatusHandler(services serviceLister, logger *zap.Logger) *PublicStatusHandler {
	return &PublicStatusHandler{services: services, logger: logger}
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

	resp := publicStatusResponse{Services: []publicServiceResponse{}}
	for _, service := range services {
		resp.Services = append(resp.Services, publicServiceResponse{
			Name:          service.Name,
			Status:        service.CurrentStatus,
			LastUpdatedAt: service.LastStatusChangeAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
