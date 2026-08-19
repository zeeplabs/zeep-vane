package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// incidentCreator is the subset of *db.IncidentRepository the incidents
// handler depends on.
type incidentCreator interface {
	Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

func toIncidentResponse(incident *db.Incident) incidentResponse {
	return incidentResponse{
		ID:         incident.ID,
		Title:      incident.Title,
		Status:     incident.Status,
		CreatedAt:  incident.CreatedAt,
		ResolvedAt: incident.ResolvedAt,
	}
}
