package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusPagesPageSize is the fixed page size for /api/status-pages
// (spec.md Assumptions: 20 for domains/services/status-pages/
// email-providers/poller-status/admins).
const statusPagesPageSize = 20

// statusPageCreatorLister is the subset of *db.StatusPageRepository the
// status pages handler depends on.
type statusPageCreatorLister interface {
	Create(ctx context.Context, statusPage *db.StatusPage, serviceIDs []string) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.StatusPage, int, error)
	AttachDomain(ctx context.Context, id, domainID, subdomain string) (*db.StatusPage, error)
	SetServices(ctx context.Context, id string, serviceIDs []string) error
	GetByID(ctx context.Context, id string) (*db.StatusPage, error)
	Delete(ctx context.Context, id string) error
}

// StatusPagesHandler serves the status page admin routes.
type StatusPagesHandler struct {
	statusPages statusPageCreatorLister
	audit       *audit.Log
	logger      *zap.Logger
}

// NewStatusPagesHandler builds a StatusPagesHandler backed by statusPages.
func NewStatusPagesHandler(statusPages statusPageCreatorLister, auditLog *audit.Log, logger *zap.Logger) *StatusPagesHandler {
	return &StatusPagesHandler{statusPages: statusPages, audit: auditLog, logger: logger}
}

type createStatusPageRequest struct {
	Name       string   `json:"name"`
	Subdomain  string   `json:"subdomain"`
	DomainID   string   `json:"domain_id"`
	ServiceIDs []string `json:"service_ids"`
}

type statusPageResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Subdomain    *string   `json:"subdomain"`
	DomainID     *string   `json:"domain_id"`
	State        string    `json:"state"`
	TLSLastError *string   `json:"tls_last_error"`
	CreatedAt    time.Time `json:"created_at"`
	ServiceIDs   []string  `json:"service_ids"`
}

// toStatusPageResponse converts sp to its wire shape, normalizing a nil
// ServiceIDs (no links) to an empty array rather than JSON null - the
// frontend's StatusPage type declares service_ids as always an array.
func toStatusPageResponse(sp *db.StatusPage) statusPageResponse {
	serviceIDs := sp.ServiceIDs
	if serviceIDs == nil {
		serviceIDs = []string{}
	}
	return statusPageResponse{
		ID:           sp.ID,
		Name:         sp.Name,
		Subdomain:    sp.Subdomain,
		DomainID:     sp.DomainID,
		State:        sp.State,
		TLSLastError: sp.TLSLastError,
		CreatedAt:    sp.CreatedAt,
		ServiceIDs:   serviceIDs,
	}
}

const invalidStatusPageRequestBody = `{"error":"name is required"}`
const partialDomainStatusPageRequestBody = `{"error":"subdomain and domain_id must be set together, or not at all"}`

// Create handles POST /api/status-pages, creating a status page starting
// in the "draft" state (SP-15), optionally bound to a domain and a set of
// services. SPD-01: domain_id/subdomain are both optional - a status page
// can be created domain-less and have a domain attached later via
// AttachDomain. SPD-05: if exactly one of domain_id/subdomain is given,
// the request is rejected - a domain without a subdomain (or vice versa)
// is a meaningless combination (design.md Data Models). The system places
// no technical limit on the number of status pages or root domains a
// single install can have - this handler never rejects a second status
// page or a second domain, it only validates the request shape.
func (h *StatusPagesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createStatusPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidStatusPageRequestBody))
		return
	}
	if (req.Subdomain == "") != (req.DomainID == "") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(partialDomainStatusPageRequestBody))
		return
	}

	statusPage := &db.StatusPage{Name: req.Name}
	if req.Subdomain != "" {
		statusPage.Subdomain = &req.Subdomain
		statusPage.DomainID = &req.DomainID
	}
	if err := h.statusPages.Create(r.Context(), statusPage, req.ServiceIDs); err != nil {
		h.logger.Error("status-pages: failed to create status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

type attachDomainRequest struct {
	DomainID  string `json:"domain_id"`
	Subdomain string `json:"subdomain"`
}

const invalidAttachDomainRequestBody = `{"error":"domain_id and subdomain are required"}`
const domainAlreadyAttachedBody = `{"error":"this status page already has a domain attached"}`
const invalidDomainIDBody = `{"error":"domain_id does not reference an existing domain"}`
const duplicateDomainSubdomainBody = `{"error":"this domain/subdomain pair is already in use"}`

// AttachDomain handles PATCH /api/status-pages/{id}/domain, setting
// domain_id/subdomain on a status page that doesn't have one yet (SPD-06).
// It is the only way a domain-less status page (SPD-01) can go on to
// actually publish, since the existing on-demand TLS/HostPolicy flow only
// starts once domain_id/subdomain are set.
func (h *StatusPagesHandler) AttachDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req attachDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DomainID == "" || req.Subdomain == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidAttachDomainRequestBody))
		return
	}

	statusPage, err := h.statusPages.AttachDomain(r.Context(), id, req.DomainID, req.Subdomain)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, db.ErrDomainAlreadyAttached):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(domainAlreadyAttachedBody))
		case errors.Is(err, db.ErrInvalidDomain):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(invalidDomainIDBody))
		case errors.Is(err, db.ErrDuplicateDomainSubdomain):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(duplicateDomainSubdomainBody))
		default:
			h.logger.Error("status-pages: failed to attach domain", zap.Error(err))
			writeInternalError(w)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

const invalidSetServicesRequestBody = `{"error":"service_ids is required"}`

type setServicesRequest struct {
	ServiceIDs []string `json:"service_ids"`
}

// SetServices handles PATCH /api/status-pages/{id}/services, replacing the
// full set of services shown on this status page with req.ServiceIDs
// (SPD-15) - an empty array is valid (unlinks every service), so only a
// missing/malformed field is rejected, not an empty one.
func (h *StatusPagesHandler) SetServices(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req setServicesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServiceIDs == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidSetServicesRequestBody))
		return
	}

	if err := h.statusPages.SetServices(r.Context(), id, req.ServiceIDs); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to set linked services", zap.Error(err))
		writeInternalError(w)
		return
	}

	statusPage, err := h.statusPages.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("status-pages: failed to reload status page after setting services", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

// Delete handles DELETE /api/status-pages/{id}, removing the status page and
// its linked service associations. It returns 404 if no status page matches
// id.
func (h *StatusPagesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.statusPages.Delete(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to delete status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := AdminFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, "status_page_deleted"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /api/status-pages, returning one page of registered
// status pages (20 per page, PAG-08).
func (h *StatusPagesHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	statusPages, total, err := h.statusPages.ListPaginated(r.Context(), page, statusPagesPageSize)
	if err != nil {
		h.logger.Error("status-pages: failed to list status pages", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]statusPageResponse, len(statusPages))
	for i, sp := range statusPages {
		resp[i] = toStatusPageResponse(&sp)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[statusPageResponse]{Items: resp, Total: total, Page: page, PageSize: statusPagesPageSize})
}
