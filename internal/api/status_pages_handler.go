package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// verifyDomainCooldown is the minimum interval between two VerifyDomain
// attempts for the same status page. Every attempt performs a real TLS
// handshake that can trigger a fresh ACME issuance attempt on failure
// (Let's Encrypt's failed-validation rate limit is per hostname, per
// account, per hour) - this is a best-effort, in-memory, per-replica
// guard against an admin mashing the "verificar" button, not a
// distributed rate limiter (SPEC_DEVIATION: with more than one replica, a
// client alternating requests across pods can still exceed this - low
// severity, since the endpoint is RBAC-gated to owner/operator).
const verifyDomainCooldown = 15 * time.Second

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
	UpdateEnvironment(ctx context.Context, id, environment string) (*db.StatusPage, error)
	SetServices(ctx context.Context, id string, placements []db.ServicePlacement) error
	GetByID(ctx context.Context, id string) (*db.StatusPage, error)
	Delete(ctx context.Context, id string) error
	PublicHostnameByID(ctx context.Context, id string) (string, error)
	CreateGroup(ctx context.Context, statusPageID, name string) (*db.StatusPageServiceGroup, error)
	UpdateGroup(ctx context.Context, tenantID, statusPageID, id, name string, position int) (*db.StatusPageServiceGroup, error)
	DeleteGroup(ctx context.Context, tenantID, statusPageID, id string) error
	ListGroups(ctx context.Context, statusPageID string) ([]db.StatusPageServiceGroup, error)
	ListServicePlacements(ctx context.Context, statusPageID string) ([]db.ServicePlacement, error)
}

// StatusPagesHandler serves the status page admin routes.
type StatusPagesHandler struct {
	statusPages statusPageCreatorLister
	audit       *audit.Log
	logger      *zap.Logger
	verifier    domainVerifier
	// dnsTarget is config.Config.PublicDNSTarget - VerifyDomain passes it
	// to the verifier so DNSMatchesTarget can be computed by IP overlap,
	// not string comparison (domain_verifier.go's doc comment on
	// DNSMatchesTarget explains why string comparison against a raw CNAME
	// record is wrong). Empty means the operator never configured it.
	dnsTarget string

	lastVerifyMu sync.Mutex
	lastVerifyAt map[string]time.Time
}

// NewStatusPagesHandler builds a StatusPagesHandler backed by statusPages.
func NewStatusPagesHandler(statusPages statusPageCreatorLister, auditLog *audit.Log, dnsTarget string, logger *zap.Logger) *StatusPagesHandler {
	return &StatusPagesHandler{
		statusPages:  statusPages,
		audit:        auditLog,
		logger:       logger,
		verifier:     newNetDomainVerifier(),
		dnsTarget:    dnsTarget,
		lastVerifyAt: make(map[string]time.Time),
	}
}

type createStatusPageRequest struct {
	Name       string   `json:"name"`
	Subdomain  string   `json:"subdomain"`
	DomainID   string   `json:"domain_id"`
	ServiceIDs []string `json:"service_ids"`
	// Environment is an optional banner tag
	// (status-page-environment-banner SPEB-01/02): "" (default when
	// omitted - no banner, production implied), "staging", or
	// "development". Validated against validStatusPageEnvironments, 422
	// on any other value.
	Environment string `json:"environment"`
}

// validStatusPageEnvironments are the only accepted values for the
// environment field on create/update (status-page-environment-banner
// SPEB-01/02), "" included (no banner). Deliberately has no "production"
// entry - "" already means production-implicit (spec.md Decisões).
var validStatusPageEnvironments = map[string]bool{
	"":            true,
	"staging":     true,
	"development": true,
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
	// Environment mirrors db.StatusPage.Environment (status-page-
	// environment-banner SPEB-02) - always serialized here (admin-only
	// response, unlike the public response's omitempty).
	Environment string `json:"environment"`
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
		Environment:  sp.Environment,
	}
}

const invalidStatusPageRequestBody = `{"error":"name is required"}`
const partialDomainStatusPageRequestBody = `{"error":"subdomain and domain_id must be set together, or not at all"}`
const invalidStatusPageEnvironmentBody = `{"error":"environment must be \"\", \"staging\", or \"development\""}`

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
	if !validStatusPageEnvironments[req.Environment] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidStatusPageEnvironmentBody))
		return
	}

	statusPage := &db.StatusPage{Name: req.Name, Environment: req.Environment}
	if req.Subdomain != "" {
		statusPage.Subdomain = &req.Subdomain
		statusPage.DomainID = &req.DomainID
	}
	if err := h.statusPages.Create(r.Context(), statusPage, req.ServiceIDs); err != nil {
		h.logger.Error("status-pages: failed to create status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, statusPage.ID, statusPage.Name, "status_page_created"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
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

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, statusPage.ID, statusPage.Name, "status_page_domain_attached"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

type updateStatusPageEnvironmentRequest struct {
	Environment string `json:"environment"`
}

// UpdateEnvironment handles PATCH /api/status-pages/{id}/environment,
// setting the public banner environment tag
// (status-page-environment-banner SPEB-02) on an existing status page. A
// dedicated single-purpose endpoint, mirroring AttachDomain above, rather
// than folding into Create's request shape - this codebase has no generic
// "update status page" endpoint; every field is set through its own
// narrow PATCH (domain, services, now environment).
func (h *StatusPagesHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateStatusPageEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidStatusPageEnvironmentBody))
		return
	}
	if !validStatusPageEnvironments[req.Environment] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidStatusPageEnvironmentBody))
		return
	}

	statusPage, err := h.statusPages.UpdateEnvironment(r.Context(), id, req.Environment)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to update status page environment", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, statusPage.ID, statusPage.Name, "status_page_environment_updated"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

const invalidSetServicesRequestBody = `{"error":"services is required"}`
const groupCrossPageRequestBody = `{"error":"group_id does not belong to this status page"}`
const selfReferenceRequestBody = `{"error":"a service cannot be its own parent"}`
const parentNotInSetRequestBody = `{"error":"parent_service_id must reference another service in this same request"}`
const maxDepthExceededRequestBody = `{"error":"nesting is limited to one level: a service that is already a parent cannot also have a parent"}`

// servicePlacementRequest is the wire shape for a single service's
// placement within a status page's service list (SGD-02, SGD-03, SGD-06,
// SGD-07): which group (if any) it belongs to, which service (if any) is
// its parent, and its display position.
type servicePlacementRequest struct {
	ServiceID       string  `json:"service_id"`
	GroupID         *string `json:"group_id"`
	ParentServiceID *string `json:"parent_service_id"`
	Position        int     `json:"position"`
}

type setServicesRequest struct {
	Services []servicePlacementRequest `json:"services"`
}

// mapSetServicesValidationError maps one of validateServicePlacements' 4
// sentinel errors (SGD-08, SGD-10, SGD-11, SGD-12) to its own distinct,
// fixed 422 message - same pattern as domains_handler.go's
// mapDomainVerificationResult: a raw Go/DB error string is never leaked to
// the client. ok is false when err doesn't match any of the 4.
func mapSetServicesValidationError(err error) (body []byte, ok bool) {
	switch {
	case errors.Is(err, db.ErrGroupCrossPage):
		return []byte(groupCrossPageRequestBody), true
	case errors.Is(err, db.ErrSelfReference):
		return []byte(selfReferenceRequestBody), true
	case errors.Is(err, db.ErrParentNotInSet):
		return []byte(parentNotInSetRequestBody), true
	case errors.Is(err, db.ErrMaxDepthExceeded):
		return []byte(maxDepthExceededRequestBody), true
	default:
		return nil, false
	}
}

// SetServices handles PATCH /api/status-pages/{id}/services, replacing the
// full set of services shown on this status page with req.Services,
// including each service's group and parent placement (SPD-15, SGD-02,
// SGD-03, SGD-06, SGD-07) - an empty array is valid (unlinks every
// service), so only a missing/malformed field is rejected, not an empty
// one.
func (h *StatusPagesHandler) SetServices(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req setServicesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Services == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidSetServicesRequestBody))
		return
	}

	placements := make([]db.ServicePlacement, len(req.Services))
	for i, s := range req.Services {
		placements[i] = db.ServicePlacement{
			ServiceID:       s.ServiceID,
			GroupID:         s.GroupID,
			ParentServiceID: s.ParentServiceID,
			Position:        s.Position,
		}
	}

	if err := h.statusPages.SetServices(r.Context(), id, placements); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if body, ok := mapSetServicesValidationError(err); ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write(body)
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

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, statusPage.ID, statusPage.Name, "status_page_services_updated"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageResponse(statusPage))
}

const invalidGroupNameRequestBody = `{"error":"name is required"}`

type createGroupRequest struct {
	Name string `json:"name"`
}

type updateGroupRequest struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type statusPageServiceGroupResponse struct {
	ID           string    `json:"id"`
	StatusPageID string    `json:"status_page_id"`
	Name         string    `json:"name"`
	Position     int       `json:"position"`
	CreatedAt    time.Time `json:"created_at"`
}

func toStatusPageServiceGroupResponse(g *db.StatusPageServiceGroup) statusPageServiceGroupResponse {
	return statusPageServiceGroupResponse{
		ID:           g.ID,
		StatusPageID: g.StatusPageID,
		Name:         g.Name,
		Position:     g.Position,
		CreatedAt:    g.CreatedAt,
	}
}

// toStatusPageServiceGroupResponses converts groups to its wire shape,
// normalizing a nil/empty slice (no groups) to an empty JSON array rather
// than null - same convention as toStatusPageResponse's ServiceIDs
// normalization.
func toStatusPageServiceGroupResponses(groups []db.StatusPageServiceGroup) []statusPageServiceGroupResponse {
	resp := make([]statusPageServiceGroupResponse, len(groups))
	for i := range groups {
		resp[i] = toStatusPageServiceGroupResponse(&groups[i])
	}
	return resp
}

// CreateGroup handles POST /api/status-pages/{id}/groups, creating a new
// named, ordered service group scoped to this status page (SGD-01). It
// checks the status page exists first via GetByID rather than letting a
// nonexistent status_page_id hit status_page_service_groups' foreign key
// constraint, which would otherwise surface as a raw DB error (500) instead
// of a clean 404 - CreateGroup's repository method has no existence check
// of its own, it only INSERTs.
func (h *StatusPagesHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidGroupNameRequestBody))
		return
	}

	if _, err := h.statusPages.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to look up status page before creating group", zap.Error(err))
		writeInternalError(w)
		return
	}

	group, err := h.statusPages.CreateGroup(r.Context(), id, req.Name)
	if err != nil {
		h.logger.Error("status-pages: failed to create service group", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, group.ID, group.Name, "status_page_group_created"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toStatusPageServiceGroupResponse(group))
}

// UpdateGroup handles PATCH /api/status-pages/{id}/groups/{groupId},
// renaming and/or repositioning an existing service group (SGD-05). The
// repository's UpdateGroup already returns db.ErrNotFound when groupId
// doesn't match any row scoped to statusPageID, so no separate existence
// check is needed here - the {id} path param is passed straight through as
// the scoping filter, so a groupId that exists but belongs to a different
// status page (even one in the caller's own tenant) 404s exactly like an
// unknown groupId would (security review finding - see UpdateGroup's own
// doc comment in status_page_repository.go).
func (h *StatusPagesHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	groupID := chi.URLParam(r, "groupId")

	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidGroupNameRequestBody))
		return
	}

	tenantID, _ := ActiveTenantIDFromContext(r.Context())
	group, err := h.statusPages.UpdateGroup(r.Context(), tenantID, id, groupID, req.Name, req.Position)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to update service group", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, group.ID, group.Name, "status_page_group_updated"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageServiceGroupResponse(group))
}

// DeleteGroup handles DELETE /api/status-pages/{id}/groups/{groupId},
// removing the group itself while leaving its member services in place,
// ungrouped (SGD-04) - group_id's ON DELETE SET NULL foreign key (migration
// 0046) does the ungrouping, not Go code here. The group's name is captured
// before the delete for the audit entry, mirroring Delete's own
// before-the-fact name capture (once the row is gone there's nothing left
// to read it back from). The repository's DeleteGroup is scoped to id (the
// status page), so a groupId belonging to a different status page 404s
// instead of silently deleting nothing or, worse, something that was never
// this caller's to delete (security review finding - see UpdateGroup's doc
// comment in status_page_repository.go).
func (h *StatusPagesHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	groupID := chi.URLParam(r, "groupId")

	var name string
	if groups, err := h.statusPages.ListGroups(r.Context(), id); err == nil {
		for _, g := range groups {
			if g.ID == groupID {
				name = g.Name
				break
			}
		}
	}

	tenantID, _ := ActiveTenantIDFromContext(r.Context())
	if err := h.statusPages.DeleteGroup(r.Context(), tenantID, id, groupID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to delete service group", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, groupID, name, "status_page_group_deleted"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListGroups handles GET /api/status-pages/{id}/groups, returning every
// service group on this status page, position-ordered (SGD-01). It checks
// the status page exists (and, via RLS, belongs to the caller's tenant)
// first via GetByID, same as CreateGroup - without this, a request for a
// status_page_id in another tenant would RLS-filter to a silent empty
// array (200) instead of a 404, making it impossible for a caller to tell
// "this page has no groups" apart from "this page isn't yours" (security
// review finding).
func (h *StatusPagesHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.statusPages.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to look up status page before listing groups", zap.Error(err))
		writeInternalError(w)
		return
	}

	groups, err := h.statusPages.ListGroups(r.Context(), id)
	if err != nil {
		h.logger.Error("status-pages: failed to list service groups", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toStatusPageServiceGroupResponses(groups))
}

// servicePlacementResponse is the wire shape for a single service's
// placement within a status page's service list, returned by
// GET /api/status-pages/{id}/services (T7). It mirrors
// servicePlacementRequest field-for-field, in the read direction: this is
// the full per-service detail the admin tree editor needs alongside T5's
// GET /groups, kept out of the existing GET /api/status-pages/{id} response
// (whose ServiceIDs stays a flat []string) per design.md's explicit
// decision not to grow that response.
type servicePlacementResponse struct {
	ServiceID       string  `json:"service_id"`
	GroupID         *string `json:"group_id"`
	ParentServiceID *string `json:"parent_service_id"`
	Position        int     `json:"position"`
}

// toServicePlacementResponses converts placements to its wire shape,
// normalizing a nil/empty slice (no linked services) to an empty JSON array
// rather than null - same convention as toStatusPageResponse's ServiceIDs
// and toStatusPageServiceGroupResponses.
func toServicePlacementResponses(placements []db.ServicePlacement) []servicePlacementResponse {
	resp := make([]servicePlacementResponse, len(placements))
	for i, p := range placements {
		resp[i] = servicePlacementResponse{
			ServiceID:       p.ServiceID,
			GroupID:         p.GroupID,
			ParentServiceID: p.ParentServiceID,
			Position:        p.Position,
		}
	}
	return resp
}

// ListServicePlacements handles GET /api/status-pages/{id}/services,
// returning the full per-service placement detail (service_id, group_id,
// parent_service_id, position) for this status page (SGD-02, SGD-03,
// SGD-06, SGD-07) - distinct from GET /api/status-pages/{id}'s unchanged
// service_ids []string. It checks the status page exists first via GetByID,
// same existence-check pattern as CreateGroup, so an unknown id surfaces a
// clean 404 instead of a bare empty array indistinguishable from "no
// services linked".
func (h *StatusPagesHandler) ListServicePlacements(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.statusPages.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to look up status page before listing service placements", zap.Error(err))
		writeInternalError(w)
		return
	}

	placements, err := h.statusPages.ListServicePlacements(r.Context(), id)
	if err != nil {
		h.logger.Error("status-pages: failed to list service placements", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toServicePlacementResponses(placements))
}

// Delete handles DELETE /api/status-pages/{id}, removing the status page and
// its linked service associations. It returns 404 if no status page matches
// id.
func (h *StatusPagesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Captured before Delete runs (spec AC4/ACTIVITY-04): once the delete
	// succeeds the status_pages row is gone, so a name fetched afterward
	// would always be empty.
	var name string
	if statusPage, err := h.statusPages.GetByID(r.Context(), id); err == nil {
		name = statusPage.Name
	}

	if err := h.statusPages.Delete(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("status-pages: failed to delete status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, name, "status_page_deleted"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type verifyDomainResponse struct {
	Hostname         string    `json:"hostname"`
	ResolvedIPs      []string  `json:"resolved_ips"`
	DNSResolved      bool      `json:"dns_resolved"`
	DNSMatchesTarget *bool     `json:"dns_matches_target"`
	TLSReachable     bool      `json:"tls_reachable"`
	TLSCertValid     bool      `json:"tls_cert_valid"`
	TLSError         *string   `json:"tls_error"`
	State            string    `json:"state"`
	TLSLastError     *string   `json:"tls_last_error"`
	CheckedAt        time.Time `json:"checked_at"`
}

const noDomainAttachedBody = `{"error":"this status page has no domain attached yet"}`
const verifyDomainRateLimitedBody = `{"error":"please wait a few seconds before checking again"}`

// checkVerifyCooldown reports whether id was verified more recently than
// verifyDomainCooldown allows, recording this attempt's timestamp as a
// side effect when it isn't.
func (h *StatusPagesHandler) checkVerifyCooldown(id string) bool {
	h.lastVerifyMu.Lock()
	defer h.lastVerifyMu.Unlock()

	if last, ok := h.lastVerifyAt[id]; ok && time.Since(last) < verifyDomainCooldown {
		return false
	}
	h.lastVerifyAt[id] = time.Now()
	return true
}

// VerifyDomain handles POST /api/status-pages/{id}/verify-domain: an
// on-demand "check DNS/SSL" action mirroring what Vercel/Render offer for
// custom domains. It performs a real DNS lookup and TLS handshake against
// the status page's public hostname - the only way to know for sure
// whether DNS has propagated and a certificate has actually been issued
// and is being served, since the StatusPage row can otherwise sit at
// "pending_tls" indefinitely until a real visitor's browser (or this
// endpoint) triggers the on-demand ACME issuance CertMagic performs
// synchronously during a TLS handshake (tls.HostPolicy/OnEvent).
func (h *StatusPagesHandler) VerifyDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	hostname, err := h.statusPages.PublicHostnameByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, db.ErrNoDomainAttached):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(noDomainAttachedBody))
		default:
			h.logger.Error("status-pages: failed to look up hostname for domain verification", zap.Error(err))
			writeInternalError(w)
		}
		return
	}

	if !h.checkVerifyCooldown(id) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(verifyDomainRateLimitedBody))
		return
	}

	result := h.verifier.Verify(r.Context(), hostname, h.dnsTarget)

	statusPage, err := h.statusPages.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("status-pages: failed to reload status page after domain verification", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, statusPage.Name, "status_page_domain_verified"); err != nil {
			h.logger.Error("status-pages: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(verifyDomainResponse{
		Hostname:         hostname,
		ResolvedIPs:      result.ResolvedIPs,
		DNSResolved:      result.DNSResolved,
		DNSMatchesTarget: result.DNSMatchesTarget,
		TLSReachable:     result.TLSReachable,
		TLSCertValid:     result.TLSCertValid,
		TLSError:         result.TLSError,
		State:            statusPage.State,
		TLSLastError:     statusPage.TLSLastError,
		CheckedAt:        time.Now(),
	})
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
