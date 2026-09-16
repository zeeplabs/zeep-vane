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
	"github.com/zeeplabs/zeep-vane/internal/notify"
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
	AddUpdate(ctx context.Context, incidentID, body string, authorID *string, isAISummary bool) (*db.IncidentUpdate, error)
	ListUpdatesPaginated(ctx context.Context, incidentID string, page, pageSize int) ([]db.IncidentUpdate, int, error)
	Transition(ctx context.Context, incidentID, status string) (*db.Incident, error)
	ConfirmPendingClose(ctx context.Context, incidentID, expectedComment string) (*db.Incident, error)
	DiscardCloseProposal(ctx context.Context, incidentID string) error
	SetSeverity(ctx context.Context, incidentID, severity string) (*db.Incident, error)
}

// incidentNotifier is the subset of *notify.Service the incidents handler
// depends on for lifecycle email notifications (notification-preferences
// NOTIFPREF-04/07).
type incidentNotifier interface {
	NotifyIncidentOpened(ctx context.Context, tenantID string, summary notify.IncidentSummary) error
	NotifyIncidentResolved(ctx context.Context, tenantID string, summary notify.IncidentSummary) error
}

// IncidentsHandler serves the incident admin routes.
type IncidentsHandler struct {
	incidents incidentCreator
	notify    incidentNotifier
	logger    *zap.Logger
}

// NewIncidentsHandler builds an IncidentsHandler backed by incidents.
func NewIncidentsHandler(incidents incidentCreator, notifier incidentNotifier, logger *zap.Logger) *IncidentsHandler {
	return &IncidentsHandler{incidents: incidents, notify: notifier, logger: logger}
}

// notifyIncidentOpened fires the incident-opened notification for a
// just-created incident. It is best-effort: a lookup or send failure is logged
// and never changes the incident response (spec's non-fatal requirement).
func (h *IncidentsHandler) notifyIncidentOpened(ctx context.Context, incident *db.Incident) {
	h.notifyIncident(ctx, incident, false)
}

// notifyIncidentResolved fires the incident-resolved notification.
func (h *IncidentsHandler) notifyIncidentResolved(ctx context.Context, incident *db.Incident) {
	h.notifyIncident(ctx, incident, true)
}

func (h *IncidentsHandler) notifyIncident(ctx context.Context, incident *db.Incident, resolved bool) {
	tenantID, ok := ActiveTenantIDFromContext(ctx)
	if !ok || h.notify == nil {
		return
	}
	summary := notify.IncidentSummary{
		IncidentID: incident.ID,
		Title:      incident.Title,
		Severity:   incident.Severity,
	}
	var err error
	if resolved {
		err = h.notify.NotifyIncidentResolved(ctx, tenantID, summary)
	} else {
		err = h.notify.NotifyIncidentOpened(ctx, tenantID, summary)
	}
	if err != nil {
		h.logger.Error("incidents: failed to send incident notification", zap.Error(err))
	}
}

type createIncidentRequest struct {
	Title       string   `json:"title"`
	ServiceIDs  []string `json:"service_ids"`
	Severity    string   `json:"severity"`
	Description string   `json:"description"`
}

type incidentResponse struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	ServiceIDs []string   `json:"service_ids"`
	// Description, PendingCloseComment, and AutoCreated are admin-only
	// fields never present on the public incident response
	// (publicIncidentResponse in public_status_handler.go) - AI-09,
	// AI-19/AI-20, AI-12.
	Description         *string `json:"description"`
	PendingCloseComment *string `json:"pending_close_comment"`
	AutoCreated         bool    `json:"auto_created"`
	// Severity is one of "minor"/"moderate"/"critical" (INCSEV-01).
	Severity string `json:"severity"`
}

const invalidIncidentRequestBody = `{"error":"title and at least one service_id are required"}`
const invalidIncidentSeverityBody = `{"error":"severity must be one of minor, moderate, critical"}`

var validIncidentSeverities = map[string]bool{
	"minor":    true,
	"moderate": true,
	"critical": true,
}

// Create handles POST /api/incidents, creating an incident bound to one or
// more services (SP-16), with a required severity (INCSEV-01/02) and an
// optional initial description (INCSEV-03).
func (h *IncidentsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" || len(req.ServiceIDs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentRequestBody))
		return
	}
	if !validIncidentSeverities[req.Severity] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentSeverityBody))
		return
	}

	incident := &db.Incident{Title: req.Title, Severity: req.Severity}
	// An empty description on create stores NULL, matching SetDescription's
	// existing NULL/absent convention (spec.md edge case).
	if req.Description != "" {
		incident.Description = &req.Description
	}
	if err := h.incidents.Create(r.Context(), incident, req.ServiceIDs); err != nil {
		h.logger.Error("incidents: failed to create incident", zap.Error(err))
		writeInternalError(w)
		return
	}
	incident.ServiceIDs = req.ServiceIDs
	h.notifyIncidentOpened(r.Context(), incident)

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
	// AuthorID and IsAISummary attribute the entry to a human or the AI
	// (INCSEV-05/06).
	AuthorID    *string `json:"author_id"`
	IsAISummary bool    `json:"is_ai_summary"`
}

type addIncidentUpdateRequest struct {
	Body string `json:"body"`
}

const invalidIncidentUpdateRequestBody = `{"error":"body is required"}`
const incidentNotFoundBody = `{"error":"incident not found"}`

// AddUpdate handles POST /api/incidents/{id}/updates, appending an update to
// the incident's timeline - attributed to the authenticated actor
// (INCSEV-05) - and returning the full timeline, most recent first (SP-17).
// It returns 404 if the incident doesn't exist.
func (h *IncidentsHandler) AddUpdate(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeForbidden(w)
		return
	}

	var req addIncidentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentUpdateRequestBody))
		return
	}

	if _, err := h.incidents.AddUpdate(r.Context(), incidentID, req.Body, &actor.ID, false); err != nil {
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
		resp[i] = incidentUpdateResponse{ID: update.ID, IncidentID: update.IncidentID, Body: update.Body, CreatedAt: update.CreatedAt, AuthorID: update.AuthorID, IsAISummary: update.IsAISummary}
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
		resp[i] = incidentUpdateResponse{ID: update.ID, IncidentID: update.IncidentID, Body: update.Body, CreatedAt: update.CreatedAt, AuthorID: update.AuthorID, IsAISummary: update.IsAISummary}
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

	if req.Status == "resolved" {
		h.notifyIncidentResolved(r.Context(), incident)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

const noPendingCloseProposalBody = `{"error":"incident has no pending close proposal"}`
const invalidConfirmCloseRequestBody = `{"error":"comment is required and must match the currently pending close proposal"}`
const closeProposalChangedBody = `{"error":"the pending close proposal changed since it was displayed - reload and try again"}`

type confirmCloseRequest struct {
	// Comment must equal the incident's current pending_close_comment
	// exactly - the frontend sends back whatever text it last displayed to
	// the operator, so a proposal silently regenerated by a later poll
	// cycle (or already discarded/confirmed) between page load and the
	// click can never be resolved with text nobody actually reviewed.
	Comment string `json:"comment"`
}

// ConfirmClose handles POST /api/incidents/{id}/confirm-close, accepting the
// LLM-drafted closing comment awaiting confirmation: it is appended as the
// incident's final update and the incident transitions to resolved, all in
// one transaction (AI-20). The request body's comment must match the
// incident's current pending_close_comment - see confirmCloseRequest.
// Returns 404 if the incident doesn't exist, 422 if the body is malformed
// or there is no pending close proposal (AI-21), 409 if comment no longer
// matches the stored proposal.
func (h *IncidentsHandler) ConfirmClose(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	var req confirmCloseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Comment == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidConfirmCloseRequestBody))
		return
	}

	incident, err := h.incidents.ConfirmPendingClose(r.Context(), incidentID, req.Comment)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		if errors.Is(err, db.ErrNoPendingProposal) {
			writeNoPendingCloseProposal(w)
			return
		}
		if errors.Is(err, db.ErrCloseProposalChanged) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(closeProposalChangedBody))
			return
		}
		h.logger.Error("incidents: failed to confirm pending close", zap.Error(err))
		writeInternalError(w)
		return
	}

	h.notifyIncidentResolved(r.Context(), incident)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

type setIncidentSeverityRequest struct {
	Severity string `json:"severity"`
}

// SetSeverity handles PATCH /api/incidents/{id}/severity, changing an
// incident's severity independent of its status (INCSEV-04). Returns 422 for
// an invalid severity, 404 if the incident doesn't exist.
func (h *IncidentsHandler) SetSeverity(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	var req setIncidentSeverityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validIncidentSeverities[req.Severity] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidIncidentSeverityBody))
		return
	}

	incident, err := h.incidents.SetSeverity(r.Context(), incidentID, req.Severity)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		h.logger.Error("incidents: failed to set incident severity", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toIncidentResponse(incident))
}

// DiscardCloseProposal handles POST /api/incidents/{id}/discard-close-proposal,
// clearing the pending closing-comment proposal and leaving the incident
// otherwise unchanged (AI-22). Returns 404 if the incident doesn't exist,
// 422 if it has no pending close proposal.
func (h *IncidentsHandler) DiscardCloseProposal(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "id")

	if err := h.incidents.DiscardCloseProposal(r.Context(), incidentID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeIncidentNotFound(w)
			return
		}
		if errors.Is(err, db.ErrNoPendingProposal) {
			writeNoPendingCloseProposal(w)
			return
		}
		h.logger.Error("incidents: failed to discard pending close proposal", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"discarded"}`))
}

func writeIncidentNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(incidentNotFoundBody))
}

func writeNoPendingCloseProposal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(noPendingCloseProposalBody))
}

func toIncidentResponse(incident *db.Incident) incidentResponse {
	serviceIDs := incident.ServiceIDs
	if serviceIDs == nil {
		serviceIDs = []string{}
	}
	return incidentResponse{
		ID:                  incident.ID,
		Title:               incident.Title,
		Status:              incident.Status,
		CreatedAt:           incident.CreatedAt,
		ResolvedAt:          incident.ResolvedAt,
		ServiceIDs:          serviceIDs,
		Description:         incident.Description,
		PendingCloseComment: incident.PendingCloseComment,
		Severity:            incident.Severity,
		AutoCreated:         incident.AutoCreated,
	}
}
