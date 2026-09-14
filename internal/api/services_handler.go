package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/history"
)

// servicesPageSize is the fixed page size for /api/services (spec.md
// Assumptions: 20 for domains/services/status-pages/email-providers/
// poller-status/admins).
const servicesPageSize = 20

// servicesUptimeWindowDays is the list/detail "Uptime 30d" window
// (monitored-services-page SVC-01/SVC-14), the same window
// OverviewHandler's uptime card uses.
const servicesUptimeWindowDays = 30

// serviceCreatorLister is the subset of *db.ServiceRepository the services
// handler depends on.
type serviceCreatorLister interface {
	Create(ctx context.Context, service *db.Service) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Service, int, error)
}

// ServicesHandler serves the service admin routes.
type ServicesHandler struct {
	services  serviceCreatorLister
	intervals statusIntervalReader
	logger    *zap.Logger
}

// NewServicesHandler builds a ServicesHandler backed by services and
// intervals.
func NewServicesHandler(services serviceCreatorLister, intervals statusIntervalReader, logger *zap.Logger) *ServicesHandler {
	return &ServicesHandler{services: services, intervals: intervals, logger: logger}
}

type createServiceRequest struct {
	Name    string `json:"name"`
	SLOID   string `json:"slo_id"`
	SLOName string `json:"slo_name"`
}

type serviceResponse struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	SLOID              string     `json:"slo_id"`
	SLOName            string     `json:"slo_name"`
	CurrentStatus      string     `json:"current_status"`
	LastStatusChangeAt time.Time  `json:"last_status_change_at"`
	Uptime30d          *float64   `json:"uptime_30d"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
}

const invalidServiceRequestBody = `{"error":"name and slo_id are required"}`

// Create handles POST /api/services, linking a new service to a Datadog SLO
// (SP-03). slo_name is optional (validation is unchanged: only name and
// slo_id are required) - a freshly created service has no interval data
// yet, so its response always has uptime_30d/last_seen_at nil.
func (h *ServicesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.SLOID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidServiceRequestBody))
		return
	}

	service := &db.Service{Name: req.Name, SLOID: req.SLOID, SLOName: req.SLOName}
	if err := h.services.Create(r.Context(), service); err != nil {
		h.logger.Error("services: failed to create service", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toServiceResponse(service, nil, nil))
}

// List handles GET /api/services, returning one page of registered
// services with their current status, 30d uptime, and last-seen time (20
// per page, PAG-08; SVC-01/SVC-06). uptime_30d/last_seen_at are computed
// via one batch ListOverlapping call for the whole page (OverviewHandler's
// pattern), never per-row - a service with no interval data in the window
// gets nil for both (rendered "—" by the frontend), not zero.
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	services, total, err := h.services.ListPaginated(r.Context(), page, servicesPageSize)
	if err != nil {
		h.logger.Error("services: failed to list services", zap.Error(err))
		writeInternalError(w)
		return
	}

	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		serviceIDs = append(serviceIDs, service.ID)
	}

	now := time.Now()
	windowStart := now.AddDate(0, 0, -servicesUptimeWindowDays)
	overlapping, err := h.intervals.ListOverlapping(r.Context(), serviceIDs, windowStart, now)
	if err != nil {
		h.logger.Error("services: failed to list overlapping status intervals", zap.Error(err))
		writeInternalError(w)
		return
	}
	intervalsByService := map[string][]db.StatusInterval{}
	for _, interval := range overlapping {
		intervalsByService[interval.ServiceID] = append(intervalsByService[interval.ServiceID], interval)
	}

	resp := make([]serviceResponse, len(services))
	for i, service := range services {
		uptime30d, lastSeenAt := uptimeAndLastSeen(intervalsByService[service.ID], windowStart, now)
		resp[i] = toServiceResponse(&service, uptime30d, lastSeenAt)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[serviceResponse]{Items: resp, Total: total, Page: page, PageSize: servicesPageSize})
}

// uptimeAndLastSeen computes a single service's uptime_30d (nil when
// history.UptimePercent has no data) and last_seen_at (the LastSeenAt of
// intervals' open interval - EndsAt nil - or nil when the service has
// never been polled).
func uptimeAndLastSeen(intervals []db.StatusInterval, windowStart, asOf time.Time) (uptime30d *float64, lastSeenAt *time.Time) {
	if pct, ok := history.UptimePercent(intervals, windowStart, asOf); ok {
		uptime30d = &pct
	}

	for _, interval := range intervals {
		if interval.EndsAt == nil {
			lastSeen := interval.LastSeenAt
			lastSeenAt = &lastSeen
			break
		}
	}

	return uptime30d, lastSeenAt
}

func toServiceResponse(service *db.Service, uptime30d *float64, lastSeenAt *time.Time) serviceResponse {
	return serviceResponse{
		ID:                 service.ID,
		Name:               service.Name,
		SLOID:              service.SLOID,
		SLOName:            service.SLOName,
		CurrentStatus:      service.CurrentStatus,
		LastStatusChangeAt: service.LastStatusChangeAt,
		Uptime30d:          uptime30d,
		LastSeenAt:         lastSeenAt,
	}
}
