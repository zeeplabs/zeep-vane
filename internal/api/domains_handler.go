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
	GetByID(ctx context.Context, id string) (*db.Domain, error)
	SetVerificationResult(ctx context.Context, id, status, sslStatus string, lastError *string, verifiedAt time.Time) (*db.Domain, error)
}

// DomainsHandler serves the domain admin routes.
type DomainsHandler struct {
	domains  domainCreatorLister
	audit    *audit.Log
	logger   *zap.Logger
	verifier domainVerifier
	// dnsTarget is config.Config.PublicDNSTarget - the real CNAME target
	// shown to the operator (domain-verification-state DOMVER-03), never a
	// hardcoded example. Empty means the operator never configured it.
	dnsTarget string

	lastVerifyMu sync.Mutex
	lastVerifyAt map[string]time.Time
}

// NewDomainsHandler builds a DomainsHandler backed by domains.
func NewDomainsHandler(domains domainCreatorLister, auditLog *audit.Log, dnsTarget string, logger *zap.Logger) *DomainsHandler {
	return &DomainsHandler{
		domains:      domains,
		audit:        auditLog,
		logger:       logger,
		verifier:     newNetDomainVerifier(),
		dnsTarget:    dnsTarget,
		lastVerifyAt: make(map[string]time.Time),
	}
}

type createDomainRequest struct {
	Hostname string `json:"hostname"`
}

type domainResponse struct {
	ID         string     `json:"id"`
	Hostname   string     `json:"hostname"`
	CreatedAt  time.Time  `json:"created_at"`
	DomainType string     `json:"domain_type"`
	Status     string     `json:"status"`
	SSLStatus  string     `json:"ssl_status"`
	VerifiedAt *time.Time `json:"verified_at"`
	LastError  *string    `json:"last_error"`
}

func toDomainResponse(domain *db.Domain) domainResponse {
	return domainResponse{
		ID: domain.ID, Hostname: domain.Hostname, CreatedAt: domain.CreatedAt,
		DomainType: domain.DomainType, Status: domain.Status, SSLStatus: domain.SSLStatus,
		VerifiedAt: domain.VerifiedAt, LastError: domain.LastError,
	}
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
	_ = json.NewEncoder(w).Encode(toDomainResponse(domain))
}

// domainsPageResponse carries dns_target loose alongside the paginated
// items list, per this codebase's convention for endpoints mixing list and
// non-list fields (AGENTS.md: same pattern as email-providers'
// active_provider) rather than forcing the generic Page[T] wrapper.
type domainsPageResponse struct {
	Items     []domainResponse `json:"items"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
	DNSTarget *string          `json:"dns_target"`
}

// List handles GET /api/domains, returning one page of registered root
// domains (20 per page, PAG-08) plus the operator's configured CNAME target
// (DOMVER-01/03) - null if PUBLIC_DNS_TARGET was never configured, so the
// frontend can show "not configured" rather than a misleading empty string.
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
		resp[i] = toDomainResponse(&domain)
	}

	var dnsTarget *string
	if h.dnsTarget != "" {
		dnsTarget = &h.dnsTarget
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(domainsPageResponse{Items: resp, Total: total, Page: page, PageSize: domainsPageSize, DNSTarget: dnsTarget})
}

// verifyDomainCooldown is shared with StatusPagesHandler's own cooldown of
// the same name and duration (domain-verification-state's Assumptions:
// reuse the pattern, applied per-domain here instead of per-status-page).
// checkVerifyCooldown reports whether id was verified more recently than
// verifyDomainCooldown allows, recording this attempt's timestamp as a side
// effect when it isn't.
func (h *DomainsHandler) checkVerifyCooldown(id string) bool {
	h.lastVerifyMu.Lock()
	defer h.lastVerifyMu.Unlock()

	if last, ok := h.lastVerifyAt[id]; ok && time.Since(last) < verifyDomainCooldown {
		return false
	}
	h.lastVerifyAt[id] = time.Now()
	return true
}

const dnsNotResolvedError = "DNS not resolved: no record found for this hostname"
const dnsMismatchError = "DNS resolved but does not point to the configured target"

// mapDomainVerificationResult derives status/ssl_status/last_error from a
// domainVerificationResult (domain-verification-state's Assumptions table):
// DNS resolving and either matching the configured target or having no
// target to compare against maps to status "verified"; anything else maps
// to "error" with a description. ssl_status mirrors TLSCertValid
// independently. Both fields' failure messages are combined into the single
// last_error column when both stages fail.
func mapDomainVerificationResult(result domainVerificationResult) (status, sslStatus string, lastError *string) {
	var dnsErr, tlsErr *string

	switch {
	case !result.DNSResolved:
		status = "error"
		msg := dnsNotResolvedError
		dnsErr = &msg
	case result.DNSMatchesTarget != nil && !*result.DNSMatchesTarget:
		status = "error"
		msg := dnsMismatchError
		dnsErr = &msg
	default:
		status = "verified"
	}

	if result.TLSCertValid {
		sslStatus = "active"
	} else {
		sslStatus = "error"
		tlsErr = result.TLSError
		if tlsErr == nil {
			msg := "TLS certificate could not be verified"
			tlsErr = &msg
		}
	}

	switch {
	case dnsErr != nil && tlsErr != nil:
		combined := *dnsErr + "; " + *tlsErr
		lastError = &combined
	case dnsErr != nil:
		lastError = dnsErr
	case tlsErr != nil:
		lastError = tlsErr
	}
	return status, sslStatus, lastError
}

// Verify handles POST /api/domains/{id}/verify, performing a real DNS+TLS
// check via the existing domainVerifier and persisting the result
// (DOMVER-04/07/08). Returns 404 if the domain doesn't exist. Within
// verifyDomainCooldown of the previous check for the same domain, it
// returns the existing persisted state instead of making a fresh network
// call (DOMVER-06) - unlike StatusPagesHandler.VerifyDomain's 429, this
// endpoint's spec calls for a 200 with the last-known state on cooldown.
func (h *DomainsHandler) Verify(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	domain, err := h.domains.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("domains: failed to look up domain for verification", zap.Error(err))
		writeInternalError(w)
		return
	}

	if !h.checkVerifyCooldown(id) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(toDomainResponse(domain))
		return
	}

	result := h.verifier.Verify(r.Context(), domain.Hostname, h.dnsTarget)
	status, sslStatus, lastError := mapDomainVerificationResult(result)

	updated, err := h.domains.SetVerificationResult(r.Context(), id, status, sslStatus, lastError, time.Now())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("domains: failed to persist domain verification result", zap.Error(err))
		writeInternalError(w)
		return
	}

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, "domain_verified"); err != nil {
			h.logger.Error("domains: failed to record audit entry", zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toDomainResponse(updated))
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

	if actor, ok := UserFromContext(r.Context()); ok {
		if err := h.audit.Record(r.Context(), actor.ID, id, "domain_deleted"); err != nil {
			h.logger.Error("domains: failed to record audit entry", zap.Error(err))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
