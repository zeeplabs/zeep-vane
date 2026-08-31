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

// domainsPageSize is the fixed page size for /api/domains (spec.md
// Assumptions: 20 for domains/services/status-pages/email-providers/
// poller-status/admins).
const domainsPageSize = 20

// domainCreatorLister is the subset of *db.DomainRepository the domains
// handler depends on.
type domainCreatorLister interface {
	Create(ctx context.Context, domain *db.Domain) error
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Domain, int, error)
	Delete(ctx context.Context, id string) error
}

// DomainsHandler serves the domain admin routes.
type DomainsHandler struct {
	domains domainCreatorLister
	audit   *audit.Log
	logger  *zap.Logger
}

// NewDomainsHandler builds a DomainsHandler backed by domains.
func NewDomainsHandler(domains domainCreatorLister, auditLog *audit.Log, logger *zap.Logger) *DomainsHandler {
	return &DomainsHandler{domains: domains, audit: auditLog, logger: logger}
}

type createDomainRequest struct {
	Hostname string `json:"hostname"`
}

type domainResponse struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	CreatedAt time.Time `json:"created_at"`
}

const invalidDomainRequestBody = `{"error":"hostname is required"}`
const duplicateDomainBody = `{"error":"hostname already registered"}`

// Create handles POST /api/domains, registering a root domain a status
// page's subdomain can later be published under (SP-14). The system allows
// registering multiple root domains without a technical limit - this
// handler rejects only an exact duplicate hostname (spec.md edge case),
// never a second, different domain.
func (h *DomainsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hostname == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidDomainRequestBody))
		return
	}

	domain := &db.Domain{Hostname: req.Hostname}
	if err := h.domains.Create(r.Context(), domain); err != nil {
		if errors.Is(err, db.ErrDuplicateHostname) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(duplicateDomainBody))
			return
		}
		h.logger.Error("domains: failed to create domain", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(domainResponse{ID: domain.ID, Hostname: domain.Hostname, CreatedAt: domain.CreatedAt})
}

// List handles GET /api/domains, returning one page of registered root
// domains (20 per page, PAG-08).
func (h *DomainsHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)

	domains, total, err := h.domains.ListPaginated(r.Context(), page, domainsPageSize)
	if err != nil {
		h.logger.Error("domains: failed to list domains", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]domainResponse, len(domains))
	for i, domain := range domains {
		resp[i] = domainResponse{ID: domain.ID, Hostname: domain.Hostname, CreatedAt: domain.CreatedAt}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Page[domainResponse]{Items: resp, Total: total, Page: page, PageSize: domainsPageSize})
}

const domainInUseBody = `{"error":"domain is still attached to a status page"}`

// Delete handles DELETE /api/domains/{id}, removing a registered root
// domain. It returns 404 if no domain matches id, or 409 if the domain is
// still attached to a status page (deleting it out from under a published
// page would silently break its public URL - the operator must detach the
// domain from the status page first).
func (h *DomainsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.domains.Delete(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, db.ErrDomainInUse) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(domainInUseBody))
			return
		}
		h.logger.Error("domains: failed to delete domain", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := AdminFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, "domain_deleted"); err != nil {
			h.logger.Error("domains: failed to record audit entry", zap.Error(err))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
