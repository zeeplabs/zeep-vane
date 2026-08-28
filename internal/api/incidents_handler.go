package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// incidentsPageSize is the fixed page size for both /api/incidents and
// /api/incidents/{id}/updates (spec.md Assumptions: 25 for incidents and
// incident updates).
const incidentsPageSize = 25

// incidentCreator is the subset of *db.IncidentRepository the incidents
// handler depends on.
type incidentCreator interface {
	Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Incident, int, error)
	AddUpdate(ctx context.Context, incidentID, body string) (*db.IncidentUpdate, error)
	ListUpdatesPaginated(ctx context.Context, incidentID string, page, pageSize int) ([]db.IncidentUpdate, int, error)
	Transition(ctx context.Context, incidentID, status string) (*db.Incident, error)
}

// IncidentsHandler serves the incident admin routes.
type IncidentsHandler struct {
	incidents incidentCreator
	logger    *zap.Logger
}

// NewIncidentsHandler builds an IncidentsHandler backed by incidents.
func NewIncidentsHandler(incidents incidentCreator, logger *zap.Logger) *IncidentsHandler {
	return &IncidentsHandler{incidents: incidents, logger: logger}
}

type createIncidentRequest struct {
	Title      string   `json:"title"`
	ServiceIDs []string `json:"service_ids"`
}

type incidentResponse struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	ServiceIDs []string   `json:"service_ids"`
}

const invalidIncidentRequestBody = `{"error":"title and at least one service_id are required"}`

// Create handles POST /api/incidents, creating an incident bound to one or
// more services (SP-16).
func (h *IncidentsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" || len(req.ServiceIDs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentRequestBody))
		return
	}

	incident := &db.Incident{Title: req.Title}
	if err := h.incidents.Create(r.Context(), incident, req.ServiceIDs); err != nil {
		h.logger.Error("incidents: failed to create incident", zap.Error(err))
		writeInternalError(w)
		return
	}
	incident.ServiceIDs = req.ServiceIDs

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

// List handles GET /api/incidents, returning one page of incidents (25 per
// page, PAG-01), most recently created first, each with the service_ids it's
// linked to (I16).
func (h *IncidentsHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	incidents, total, err := h.incidents.ListPaginated(r.Context(), page, incidentsPageSize)
	if err != nil {
		h.logger.Error("incidents: failed to list incidents", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]incidentResponse, len(incidents))
	for i, incident := range incidents {
		resp[i] = toIncidentResponse(&incident)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[incidentResponse]{Items: resp, Total: total, Page: page, PageSize: incidentsPageSize})
}

type incidentUpdateResponse struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incident_id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type addIncidentUpdateRequest struct {
	Body string `json:"body"`
}

const invalidIncidentUpdateRequestBody = `{"error":"body is required"}`
const incidentNotFoundBody = `{"error":"incident not found"}`

// AddUpdate handles POST /api/incidents/{id}/updates, appending an update to
// the incident's timeline and returning the full timeline, most recent
// first (SP-17). It returns 404 if the incident doesn't exist.
func (h *IncidentsHandler) AddUpdate(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	var req addIncidentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentUpdateRequestBody))
		return
	}

	if _, err := h.incidents.AddUpdate(r.Context(), incidentID, req.Body); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		h.logger.Error("incidents: failed to add incident update", zap.Error(err))
		writeInternalError(w)
		return
	}

	// Re-fetch page 1 of the timeline to return in the response (design.md:
	// this is the same page the client re-fetches anyway; an incident with
	// more than incidentsPageSize updates shows only the most recent page
	// here, already true today for the initial render since both orderings
	// are created_at DESC).
	updates, _, err := h.incidents.ListUpdatesPaginated(r.Context(), incidentID, 1, incidentsPageSize)
	if err != nil {
		h.logger.Error("incidents: failed to list incident updates", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]incidentUpdateResponse, len(updates))
	for i, update := range updates {
		resp[i] = incidentUpdateResponse{ID: update.ID, IncidentID: update.IncidentID, Body: update.Body, CreatedAt: update.CreatedAt}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ListUpdates handles GET /api/incidents/{id}/updates, returning one page of
// the incident's timeline (25 per page, PAG-05), most recent first (I16).
// Returns 404 if the incident doesn't exist.
func (h *IncidentsHandler) ListUpdates(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")
	page := parsePage(r)

	updates, total, err := h.incidents.ListUpdatesPaginated(r.Context(), incidentID, page, incidentsPageSize)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		h.logger.Error("incidents: failed to list incident updates", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]incidentUpdateResponse, len(updates))
	for i, update := range updates {
		resp[i] = incidentUpdateResponse{ID: update.ID, IncidentID: update.IncidentID, Body: update.Body, CreatedAt: update.CreatedAt}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[incidentUpdateResponse]{Items: resp, Total: total, Page: page, PageSize: incidentsPageSize})
}

type transitionIncidentRequest struct {
	Status string `json:"status"`
}

const invalidIncidentStatusBody = `{"error":"status must be one of investigating, identified, monitoring, resolved"}`

var validIncidentStatuses = map[string]bool{
	"investigating": true,
	"identified":    true,
	"monitoring":    true,
	"resolved":      true,
}

// Transition handles PATCH /api/incidents/{id}, moving the incident to a new
// status (SP-19). Reopening a resolved incident (e.g. back to
// "investigating") is a legitimate transition, not rejected (SP-20), and is
// recorded on the timeline by IncidentRepository.Transition.
func (h *IncidentsHandler) Transition(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	var req transitionIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validIncidentStatuses[req.Status] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentStatusBody))
		return
	}

	incident, err := h.incidents.Transition(r.Context(), incidentID, req.Status)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		h.logger.Error("incidents: failed to transition incident", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

func writeIncidentNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(incidentNotFoundBody))
}

func toIncidentResponse(incident *db.Incident) incidentResponse {
	serviceIDs := incident.ServiceIDs
	if serviceIDs == nil {
		serviceIDs = []string{}
	}
	return incidentResponse{
		ID:         incident.ID,
		Title:      incident.Title,
		Status:     incident.Status,
		CreatedAt:  incident.CreatedAt,
		ResolvedAt: incident.ResolvedAt,
		ServiceIDs: serviceIDs,
	}
}
