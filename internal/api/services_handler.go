package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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

// servicesHourlyBucketCount/servicesHourlyBucketWidth are the detail
// drawer's "24 status bars" strip (SVC-17): 24 one-hour buckets, the same
// history.BuildBuckets call public-status-hourly-history already uses.
const servicesHourlyBucketCount = 24

const servicesHourlyBucketWidth = time.Hour

// serviceCreatorLister is the subset of *db.ServiceRepository the services
// handler depends on.
type serviceCreatorLister interface {
	Create(ctx context.Context, service *db.Service) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Service, int, error)
	Get(ctx context.Context, id string) (*db.Service, bool, error)
}

// serviceIncidentCounter is the subset of *db.IncidentRepository the
// services handler depends on (SVC-14's "Incidentes (30d)" stat).
type serviceIncidentCounter interface {
	CountByServiceSince(ctx context.Context, serviceID string, since time.Time) (int, error)
}

// ServicesHandler serves the service admin routes.
type ServicesHandler struct {
	services   serviceCreatorLister
	intervals  statusIntervalReader
	incidents  serviceIncidentCounter
	logger     *zap.Logger
	historyLoc *time.Location
}

// NewServicesHandler builds a ServicesHandler backed by services,
// intervals, and incidents. It loads America/Sao_Paulo once here (same
// tzdata assumption and load-once-panic-on-failure pattern as
// NewOverviewHandler): a load failure is a build defect, so it panics at
// construction rather than turning every request into a 500.
func NewServicesHandler(services serviceCreatorLister, intervals statusIntervalReader, incidents serviceIncidentCounter, logger *zap.Logger) *ServicesHandler {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(fmt.Sprintf("services: failed to load America/Sao_Paulo location: %v", err))
	}
	return &ServicesHandler{services: services, intervals: intervals, incidents: incidents, logger: logger, historyLoc: loc}
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

// serviceNotFoundBody is the fixed generic 404 body for GET
// /api/services/{id} - never leaks err.Error() or reveals whether id is
// merely malformed vs. genuinely absent (AGENTS.md §4).
const serviceNotFoundBody = `{"error":"service not found"}`

// hourlyBucketResponse is one bucket of the detail drawer's 24-hour status
// history strip (SVC-17).
type hourlyBucketResponse struct {
	Start  time.Time `json:"start"`
	Status string    `json:"status"`
}

// serviceDetailResponse is the GET /api/services/{id} contract
// (design.md's ServiceDetail) - a flat DTO extending serviceResponse, not
// Page[T]: a single-resource read, same precedent as OverviewResponse.
type serviceDetailResponse struct {
	serviceResponse
	StatusAnalysis *string                `json:"status_analysis"`
	Incidents30d   int                    `json:"incidents_30d"`
	HourlyBuckets  []hourlyBucketResponse `json:"hourly_buckets"`
}

// Get handles GET /api/services/{id} (SVC-14..19), the monitored-services
// detail drawer's read: the same uptime_30d/last_seen_at as List (scoped to
// one service), incidents_30d via IncidentRepository.CountByServiceSince,
// and hourly_buckets - always exactly servicesHourlyBucketCount entries,
// built via history.BuildBuckets the same way public-status-hourly-history
// does. status_analysis passes through Service.StatusAnalysis unchanged:
// non-nil only while current_status is "degraded" and an analysis has been
// stored (db.Service's own invariant), nil otherwise. Returns a fixed
// generic 404 if id doesn't exist (AGENTS.md §4).
func (h *ServicesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	service, found, err := h.services.Get(ctx, id)
	if err != nil {
		h.logger.Error("services: failed to get service", zap.Error(err))
		writeInternalError(w)
		return
	}
	if !found {
		writeAdminError(w, http.StatusNotFound, serviceNotFoundBody)
		return
	}

	now := time.Now()
	windowStart := now.AddDate(0, 0, -servicesUptimeWindowDays)
	overlapping30d, err := h.intervals.ListOverlapping(ctx, []string{id}, windowStart, now)
	if err != nil {
		h.logger.Error("services: failed to list overlapping status intervals for detail", zap.Error(err))
		writeInternalError(w)
		return
	}
	uptime30d, lastSeenAt := uptimeAndLastSeen(overlapping30d, windowStart, now)

	incidents30d, err := h.incidents.CountByServiceSince(ctx, id, windowStart)
	if err != nil {
		h.logger.Error("services: failed to count incidents for service", zap.Error(err))
		writeInternalError(w)
		return
	}

	hourlyWindowStart := now.Add(-servicesHourlyBucketCount * servicesHourlyBucketWidth)
	overlapping24h, err := h.intervals.ListOverlapping(ctx, []string{id}, hourlyWindowStart, now)
	if err != nil {
		h.logger.Error("services: failed to list overlapping status intervals for hourly buckets", zap.Error(err))
		writeInternalError(w)
		return
	}
	buckets := history.BuildBuckets(overlapping24h, now, now, h.historyLoc, servicesHourlyBucketCount, servicesHourlyBucketWidth)

	resp := serviceDetailResponse{
		serviceResponse: toServiceResponse(service, uptime30d, lastSeenAt),
		StatusAnalysis:  service.StatusAnalysis,
		Incidents30d:    incidents30d,
		HourlyBuckets:   toHourlyBucketResponses(buckets),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// toHourlyBucketResponses maps history.Bucket rows into the JSON response
// shape, preserving order (oldest first, per history.BuildBuckets).
func toHourlyBucketResponses(buckets []history.Bucket) []hourlyBucketResponse {
	out := make([]hourlyBucketResponse, len(buckets))
	for i, bucket := range buckets {
		out[i] = hourlyBucketResponse{Start: bucket.Start, Status: bucket.Status}
	}
	return out
}
