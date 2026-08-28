package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// servicesPageSize is the fixed page size for /api/services (spec.md
// Assumptions: 20 for domains/services/status-pages/email-providers/
// poller-status/admins).
const servicesPageSize = 20

// serviceCreatorLister is the subset of *db.ServiceRepository the services
// handler depends on.
type serviceCreatorLister interface {
	Create(ctx context.Context, service *db.Service) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Service, int, error)
}

// ServicesHandler serves the service admin routes.
type ServicesHandler struct {
	services serviceCreatorLister
	logger   *zap.Logger
}

// NewServicesHandler builds a ServicesHandler backed by services.
func NewServicesHandler(services serviceCreatorLister, logger *zap.Logger) *ServicesHandler {
	return &ServicesHandler{services: services, logger: logger}
}

type createServiceRequest struct {
	Name  string `json:"name"`
	SLOID string `json:"slo_id"`
}

type serviceResponse struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	SLOID              string    `json:"slo_id"`
	CurrentStatus      string    `json:"current_status"`
	LastStatusChangeAt time.Time `json:"last_status_change_at"`
}

const invalidServiceRequestBody = `{"error":"name and slo_id are required"}`

// Create handles POST /api/services, linking a new service to a Datadog SLO
// (SP-03).
func (h *ServicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.SLOID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidServiceRequestBody))
		return
	}

	service := &db.Service{Name: req.Name, SLOID: req.SLOID}
	if err := h.services.Create(r.Context(), service); err != nil {
		h.logger.Error("services: failed to create service", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toServiceResponse(service))
}

// List handles GET /api/services, returning one page of registered
// services with their current status (20 per page, PAG-08).
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	services, total, err := h.services.ListPaginated(r.Context(), page, servicesPageSize)
	if err != nil {
		h.logger.Error("services: failed to list services", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]serviceResponse, len(services))
	for i, service := range services {
		resp[i] = toServiceResponse(&service)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[serviceResponse]{Items: resp, Total: total, Page: page, PageSize: servicesPageSize})
}

func toServiceResponse(service *db.Service) serviceResponse {
	return serviceResponse{
		ID:                 service.ID,
		Name:               service.Name,
		SLOID:              service.SLOID,
		CurrentStatus:      service.CurrentStatus,
		LastStatusChangeAt: service.LastStatusChangeAt,
	}
}
