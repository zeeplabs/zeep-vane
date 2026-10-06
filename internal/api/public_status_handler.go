package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/history"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

// serviceLister is the subset of *db.ServiceRepository the public status
// handler depends on. It is scoped to a single status page (SP-15): the
// public page must show only the services linked to it, never every
// service in the installation.
type serviceLister interface {
	ListForStatusPage(ctx context.Context, statusPageID string) ([]db.Service, error)
}

// statusIntervalReader is the subset of *db.StatusIntervalRepository the
// public status handler depends on.
type statusIntervalReader interface {
	OpenIntervalsByService(ctx context.Context) (map[string]db.StatusInterval, error)
	ListOverlapping(ctx context.Context, serviceIDs []string, windowStart, now time.Time) ([]db.StatusInterval, error)
}

// publicIncidentLister is the subset of *db.IncidentRepository the public
// status handler depends on. It is scoped to a single status page (SP-15):
// only incidents linked to a service that status page publishes may appear.
type publicIncidentLister interface {
	ListPublicForStatusPage(ctx context.Context, statusPageID string, retentionDays, page, pageSize int) (active, resolved []db.IncidentPublic, resolvedTotal int, err error)
}

// companySettingsGetter is the subset of *db.TenantRepository the public
// status handler (and the other unauthenticated readers of the old
// company_settings singleton) depend on, to surface the real company
// identity instead of mockData.companySettings (SET-15, SET-16). Active
// resolves the session's tenant, falling back to the installation's single
// tenant on these public routes, which have no session at all - see
// TenantRepository.activeTenantPredicate.
type companySettingsGetter interface {
	Active(ctx context.Context) (*db.Tenant, error)
}

// statusPageGroupLister is the subset of *db.StatusPageRepository the
// public status handler depends on, to surface a status page's service
// groups (service-groups-and-dependencies SGD-02/SGD-09) alongside its
// services, and (public-status-domain-health PSDH-01) to look up the
// status page's own row for its DomainID. Scoped to a single statusPageID,
// same as serviceLister and publicIncidentLister.
type statusPageGroupLister interface {
	ListGroups(ctx context.Context, statusPageID string) ([]db.StatusPageServiceGroup, error)
	GetByID(ctx context.Context, id string) (*db.StatusPage, error)
}

// domainHealthGetter is the subset of *db.DomainRepository the public
// status handler depends on, to read the health-check state a status
// page's attached domain already has (AD-039's domain-health-monitoring -
// this handler never performs its own RDAP/NS lookup, only reads what the
// daily scheduler already persisted, same no-live-external-call guarantee
// as the rest of this handler, AGENTS.md S4).
type domainHealthGetter interface {
	GetByID(ctx context.Context, id string) (*db.Domain, error)
}

// domainHealthExpiryAttentionDays is the days-until-expiration threshold
// below which domain_health.status becomes "attention" (PSDH-02) - reuses
// AD-039's first alert band (30/15/7 days) rather than introducing a new
// number.
const domainHealthExpiryAttentionDays = 30

// incidentRetentionDays is the public status page's incident history
// retention window (spec.md Assumptions: "Janela de retenção de histórico
// de incidentes/uptime" - 90 days).
const incidentRetentionDays = 90

// publicResolvedIncidentsPageSize is the fixed page size for the public
// status page's resolved-incidents history (spec.md Assumptions: 10 for
// public status page incidents - tuned for "Carregar mais" click cadence).
const publicResolvedIncidentsPageSize = 10

// PublicStatusHandler serves the public, unauthenticated status page
// endpoint (SP-10). It never talks to Datadog directly - it only reads
// what the poller has already persisted, so a Datadog outage
// (including the connected Integration being marked "invalid") never takes
// the public page down: it keeps serving the last interval on record, with
// its real last_seen_at timestamp, never a fabricated "now" (SP-08, SP-09).
// This holds for the hourly bars and uptime percentage too (H7 fix,
// composeResponse's asOf) - a stalled poller's open interval is never
// extrapolated past the last time it was actually confirmed.
type PublicStatusHandler struct {
	services        serviceLister
	intervals       statusIntervalReader
	incidents       publicIncidentLister
	companySettings companySettingsGetter
	statusPages     statusPageGroupLister
	domains         domainHealthGetter
	logger          *zap.Logger
	historyLoc      *time.Location
}

// NewPublicStatusHandler builds a PublicStatusHandler backed by services,
// intervals, incidents, companySettings, statusPages, and domains.
//
// It loads the America/Sao_Paulo location once here (UPT-04) rather than
// per-request - cmd/vane/main.go blank-imports time/tzdata specifically so
// this LoadLocation always succeeds regardless of host OS tzdata
// availability; a failure here is a build/deployment defect, not a
// per-request condition, so it panics at construction time instead of
// turning every request into a 500.
func NewPublicStatusHandler(services serviceLister, intervals statusIntervalReader, incidents publicIncidentLister, companySettings companySettingsGetter, statusPages statusPageGroupLister, domains domainHealthGetter, logger *zap.Logger) *PublicStatusHandler {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(fmt.Sprintf("public-status: failed to load America/Sao_Paulo location: %v", err))
	}
	return &PublicStatusHandler{services: services, intervals: intervals, incidents: incidents, companySettings: companySettings, statusPages: statusPages, domains: domains, logger: logger, historyLoc: loc}
}

type publicServiceResponse struct {
	ID            string                        `json:"id"`
	Name          string                        `json:"name"`
	Status        string                        `json:"status"`
	LastUpdatedAt time.Time                     `json:"last_updated_at"`
	History       []publicHistoryBucketResponse `json:"history"`
	// UptimePercent is nil ("undefined", render a dash) when the service
	// has zero recorded intervals within the window (SHU-15) - never a
	// fabricated 0 or 100.
	UptimePercent *float64 `json:"uptime_percent"`
	// StatusAnalysis is the LLM-generated degraded-tooltip text (AI-14,
	// AI-16), present only when Status is "degraded" and an analysis has
	// finished generating - omitted (not null) otherwise, including while
	// a degraded service's analysis is still pending. Defense-in-depth
	// re-check of the "degraded" condition at the DTO boundary even though
	// SLOAnalyzer already clears db.Service.StatusAnalysis to NULL on any
	// other transition (AI-15/AI-17) - a stale non-nil value on a
	// non-degraded service must never leak here.
	StatusAnalysis *string `json:"status_analysis,omitempty"`
	// GroupID/GroupName and ParentServiceID/ParentServiceName describe this
	// service's placement on this status page (service-groups-and-
	// dependencies SGD-02/SGD-03/SGD-06/SGD-09), straight from db.Service's
	// T8 fields - nil/omitted for an ungrouped, parentless service.
	GroupID   *string `json:"group_id,omitempty"`
	GroupName *string `json:"group_name,omitempty"`
	// ParentServiceID is carried through even when the referenced parent
	// service itself got filtered out of this response (e.g. the parent is
	// "not_configured", see composeResponse's shownServices filter) - the
	// id is still meaningful to a client that wants to know "this service
	// depends on something", and ParentServiceName is resolved from the
	// *unfiltered* full service list for the same reason, so the name is
	// never silently dropped just because the parent itself isn't publicly
	// listed.
	ParentServiceID   *string `json:"parent_service_id,omitempty"`
	ParentServiceName *string `json:"parent_service_name,omitempty"`
}

// publicHistoryBucketResponse is one bucket in a service's uptime history
// row (UPT-01..06). Its width varies with the selected range (TRS-01..04) -
// it is no longer always an hour, hence the name (renamed from
// publicHourlyStatusResponse).
type publicHistoryBucketResponse struct {
	Start  time.Time `json:"start"`
	Status string    `json:"status"`
	// Episodes lists every degraded episode this bucket overlaps
	// (degraded-interval-analysis DEGINT-11), most-recent-first - omitted
	// (not null or []) when the bucket has none, whether because it never
	// went degraded or because it did but this feature predates that
	// episode (no analysis column value to report, still an empty slice
	// only if history.BuildBuckets attached nothing).
	Episodes []episodeResponse `json:"episodes,omitempty"`
}

// episodeResponse is one degraded episode a bucket overlaps.
// Analysis is null when the AI-generated text is still pending, failed,
// or predates this column (degraded-interval-analysis DEGINT-06).
type episodeResponse struct {
	StartsAt time.Time  `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
	Analysis *string    `json:"analysis"`
}

type publicIncidentUpdateResponse struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type publicIncidentResponse struct {
	ID         string                         `json:"id"`
	Title      string                         `json:"title"`
	Status     string                         `json:"status"`
	CreatedAt  time.Time                      `json:"created_at"`
	ResolvedAt *time.Time                     `json:"resolved_at"`
	Updates    []publicIncidentUpdateResponse `json:"updates"`
	// Description is an optional longer body (AI-09/AI-11/AI-18), present
	// whenever db.Incident.Description is non-nil. pending_close_comment
	// is deliberately never exposed here - admin-only (T17's DTO is its
	// only intended consumer).
	Description *string `json:"description,omitempty"`
}

type publicIncidentsResponse struct {
	Active   []publicIncidentResponse     `json:"active"`
	Resolved Page[publicIncidentResponse] `json:"resolved"`
}

// publicCompanyResponse carries the real company identity (SET-15) - a
// null LogoURL means no logo has ever been uploaded (SET-16), never a
// fabricated placeholder.
type publicCompanyResponse struct {
	Name    string  `json:"name"`
	LogoURL *string `json:"logo_url"`
}

type publicStatusResponse struct {
	Company   publicCompanyResponse   `json:"company"`
	Services  []publicServiceResponse `json:"services"`
	Incidents publicIncidentsResponse `json:"incidents"`
	// Groups lists every service group on this status page, including ones
	// with zero services placed in them - operators can see and manage an
	// empty group from the public page's own data (service-groups-and-
	// dependencies SGD-09's "operator-visible empty state" decision), so
	// this is ListGroups's raw, unfiltered output.
	Groups []publicGroupResponse `json:"groups"`
	// DomainHealth is present only when this status page has a domain
	// attached AND that domain has had at least one successful RDAP check
	// (PSDH-01/03/04) - omitted entirely otherwise, never a guessed "ok".
	DomainHealth *publicDomainHealthResponse `json:"domain_health,omitempty"`
	// Environment is present only when the status page's environment tag
	// is non-empty (status-page-environment-banner SPEB-04) - omitted
	// entirely (never serialized as "") when the page has no tag set, the
	// overwhelming majority of status pages. Drives
	// PublicStatusPage.tsx's fixed banner (SPEB-05).
	Environment string `json:"environment,omitempty"`
}

// publicDomainHealthResponse is the derived, public-safe view of a status
// page's attached domain's health (public-status-domain-health). It
// deliberately carries only the derived ok/attention verdict - never the
// raw expiration date, registrar, or NS values AD-039 persists (PSDH-05's
// "no literal data exposed publicly" decision).
type publicDomainHealthResponse struct {
	Status string `json:"status"`
}

// publicGroupResponse is one status_page_service_groups row.
type publicGroupResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// Get handles GET / on a published status page's hostname, returning every
// registered service's current status and the timestamp it last changed.
// It requires no authentication (SP-10).
func (h *PublicStatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	statusPageID, ok := router.StatusPageIDFromContext(r.Context())
	if !ok {
		// Reached without a StatusPage resolved on the context - only
		// possible if this handler is wired up without going through
		// router.HostRouter first. That is a routing bug, not a client
		// error: never fall back to listing every service/incident in the
		// installation (that would silently reintroduce the SP-15 scoping
		// gap).
		h.logger.Error("public-status: no StatusPageID on request context")
		writeInternalError(w)
		return
	}

	rng, ok := parseRange(r)
	if !ok {
		writeInvalidRangeError(w)
		return
	}

	page := parsePage(r)
	resp, err := h.composeResponse(r.Context(), statusPageID, page, rng)
	if err != nil {
		h.logger.Error("public-status: failed to compose response", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// composeResponse builds the public status response for statusPageID, with
// resolvedPage selecting the page of resolved-incident history to include
// (PAG-12) and rng selecting the history/uptime window and bucket width
// (TRS-01..07) - shared by Get (resolved by Host header, production) and
// PublicStatusPreviewHandler.Get (resolved by ID, dev/preview - I12).
func (h *PublicStatusHandler) composeResponse(ctx context.Context, statusPageID string, resolvedPage int, rng rangeSpec) (publicStatusResponse, error) {
	services, err := h.services.ListForStatusPage(ctx, statusPageID)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list services: %w", err)
	}

	openIntervals, err := h.intervals.OpenIntervalsByService(ctx)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to load open status intervals: %w", err)
	}

	activeIncidents, resolvedIncidents, resolvedTotal, err := h.incidents.ListPublicForStatusPage(ctx, statusPageID, incidentRetentionDays, resolvedPage, publicResolvedIncidentsPageSize)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list public incidents: %w", err)
	}

	companySettings, err := h.companySettings.Active(ctx)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to get company settings: %w", err)
	}

	groups, err := h.statusPages.ListGroups(ctx, statusPageID)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list status page service groups: %w", err)
	}

	// Fetched once and reused by both resolveDomainHealth (DomainID) and
	// the Environment field below (status-page-environment-banner SPEB-04)
	// - avoids a second GetByID round trip for the same row.
	statusPage, err := h.statusPages.GetByID(ctx, statusPageID)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to get status page: %w", err)
	}

	domainHealth, err := h.resolveDomainHealth(ctx, statusPage)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to resolve domain health: %w", err)
	}

	// serviceNameByID is built from the full, unfiltered services list (not
	// shownServices below) so a service's ParentServiceName still resolves
	// even if the parent itself is "not_configured" and gets filtered out
	// of the public response - see publicServiceResponse.ParentServiceID's
	// doc comment.
	serviceNameByID := make(map[string]string, len(services))
	for _, service := range services {
		serviceNameByID[service.ID] = service.Name
	}

	// A service with no SLO linked yet stays "not_configured" and is never
	// shown publicly (spec.md edge case) - it's an admin-side concept only,
	// until a poller cycle gives it a real status. Filter before querying
	// history so a not_configured service never costs an interval lookup.
	shownServices := make([]db.Service, 0, len(services))
	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		if service.CurrentStatus == "not_configured" {
			continue
		}
		shownServices = append(shownServices, service)
		serviceIDs = append(serviceIDs, service.ID)
	}

	now := time.Now()
	windowStart := now.Add(-rng.window)
	overlapping, err := h.intervals.ListOverlapping(ctx, serviceIDs, windowStart, now)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list overlapping status intervals: %w", err)
	}
	intervalsByService := map[string][]db.StatusInterval{}
	for _, interval := range overlapping {
		intervalsByService[interval.ServiceID] = append(intervalsByService[interval.ServiceID], interval)
	}

	resp := publicStatusResponse{
		Company:      publicCompanyResponse{Name: companySettings.Name, LogoURL: companySettings.LogoServedURL()},
		Services:     []publicServiceResponse{},
		Groups:       toPublicGroupResponses(groups),
		DomainHealth: domainHealth,
		Environment:  statusPage.Environment,
		Incidents: publicIncidentsResponse{
			Active: toPublicIncidentResponses(activeIncidents),
			Resolved: Page[publicIncidentResponse]{
				Items:    toPublicIncidentResponses(resolvedIncidents),
				Total:    resolvedTotal,
				Page:     resolvedPage,
				PageSize: publicResolvedIncidentsPageSize,
			},
		},
	}
	for _, service := range shownServices {
		// A service the poller has never successfully reached yet has no
		// entry here; LastUpdatedAt then stays the zero value rather than a
		// fabricated "now" (SP-08, SP-09, edge case in spec.md). Its
		// history is likewise all no_data - history.BuildBuckets handles a
		// nil/empty interval slice with no special-case branch here.
		serviceIntervals := intervalsByService[service.ID]

		// asOf clamps the hourly bars and uptime percentage to the last
		// time the poller actually confirmed this service's status, not
		// wall-clock now (H7). Without this, a service left with an open
		// interval by a poller that has since crashed would show
		// fabricated green bars / 100% uptime for however long it's been
		// silently unconfirmed - directly contradicting this handler's own
		// promise to never fabricate "now" (SP-08, SP-09). Only an open
		// interval's LastSeenAt qualifies: no open interval (never polled,
		// or every interval already closed) leaves asOf at now, matching
		// prior behavior and leaving closed-interval history unaffected -
		// every closed interval already carries its own real EndsAt.
		asOf := now
		if openInterval, ok := openIntervals[service.ID]; ok && openInterval.LastSeenAt.Before(now) {
			asOf = openInterval.LastSeenAt
		}

		buckets := history.BuildBuckets(serviceIntervals, now, asOf, h.historyLoc, rng.bucketCount, rng.bucketWidth)

		var uptimePercent *float64
		if pct, ok := history.UptimePercent(serviceIntervals, windowStart, asOf); ok {
			uptimePercent = &pct
		}

		// status_analysis is only ever surfaced for a currently-degraded
		// service (AI-16) - defense-in-depth re-check here even though
		// SLOAnalyzer already clears the stored column to NULL on any
		// other transition (AI-15/AI-17), so a stale value can never leak
		// through this DTO for a non-degraded service.
		var statusAnalysis *string
		if service.CurrentStatus == "degraded" {
			statusAnalysis = service.StatusAnalysis
		}

		var parentServiceName *string
		if service.ParentServiceID != nil {
			if name, ok := serviceNameByID[*service.ParentServiceID]; ok {
				parentServiceName = &name
			}
		}

		resp.Services = append(resp.Services, publicServiceResponse{
			ID:                service.ID,
			Name:              service.Name,
			Status:            service.CurrentStatus,
			LastUpdatedAt:     openIntervals[service.ID].LastSeenAt,
			History:           toPublicHistoryResponses(buckets),
			UptimePercent:     uptimePercent,
			StatusAnalysis:    statusAnalysis,
			GroupID:           service.GroupID,
			GroupName:         service.GroupName,
			ParentServiceID:   service.ParentServiceID,
			ParentServiceName: parentServiceName,
		})
	}

	return resp, nil
}

// toPublicHistoryResponses converts history.BuildBuckets's buckets into their
// public response shape.
func toPublicHistoryResponses(buckets []history.Bucket) []publicHistoryBucketResponse {
	resp := make([]publicHistoryBucketResponse, len(buckets))
	for i, bucket := range buckets {
		var episodes []episodeResponse
		if len(bucket.Episodes) > 0 {
			episodes = make([]episodeResponse, len(bucket.Episodes))
			for j, ep := range bucket.Episodes {
				episodes[j] = episodeResponse{StartsAt: ep.StartsAt, EndsAt: ep.EndsAt, Analysis: ep.Analysis}
			}
		}
		resp[i] = publicHistoryBucketResponse{Start: bucket.Start, Status: bucket.Status, Episodes: episodes}
	}
	return resp
}

// toPublicGroupResponses converts db.StatusPageServiceGroup rows (already
// ordered by position, ListGroups's own contract) into their public
// response shape, unfiltered - including groups with zero services placed
// in them (SGD-09).
func toPublicGroupResponses(groups []db.StatusPageServiceGroup) []publicGroupResponse {
	resp := make([]publicGroupResponse, len(groups))
	for i, group := range groups {
		resp[i] = publicGroupResponse{ID: group.ID, Name: group.Name, Position: group.Position}
	}
	return resp
}

// resolveDomainHealth derives the public domain_health field for
// statusPage (PSDH-01..04), reading only what AD-039's daily
// DomainHealthScheduler has already persisted on the domains table - it
// never performs its own RDAP/NS lookup (AGENTS.md S4). Returns nil
// (field omitted) when the status page has no domain attached, or its
// domain has never had a successful RDAP check yet. statusPage is already
// loaded by composeResponse (also used for the Environment field), so
// this no longer fetches it itself.
func (h *PublicStatusHandler) resolveDomainHealth(ctx context.Context, statusPage *db.StatusPage) (*publicDomainHealthResponse, error) {
	if statusPage.DomainID == nil {
		return nil, nil
	}

	domain, err := h.domains.GetByID(ctx, *statusPage.DomainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get domain: %w", err)
	}
	if domain.LastRDAPSuccessAt == nil || domain.LastRDAPSuccessAt.IsZero() {
		return nil, nil
	}

	status := "ok"
	expiringSoon := domain.ExpiresAt != nil && !domain.ExpiresAt.IsZero() &&
		time.Until(*domain.ExpiresAt) <= domainHealthExpiryAttentionDays*24*time.Hour
	if domain.NSDriftDetected || expiringSoon {
		status = "attention"
	}

	return &publicDomainHealthResponse{Status: status}, nil
}

// toPublicIncidentResponses converts a slice of db.IncidentPublic (already
// ordered most-recent-first, timeline included) into their public response
// shape.
func toPublicIncidentResponses(incidents []db.IncidentPublic) []publicIncidentResponse {
	resp := make([]publicIncidentResponse, len(incidents))
	for i, incident := range incidents {
		updates := make([]publicIncidentUpdateResponse, len(incident.Updates))
		for j, update := range incident.Updates {
			updates[j] = publicIncidentUpdateResponse{Body: update.Body, CreatedAt: update.CreatedAt}
		}
		resp[i] = publicIncidentResponse{
			ID:          incident.ID,
			Title:       incident.Title,
			Status:      incident.Status,
			CreatedAt:   incident.CreatedAt,
			ResolvedAt:  incident.ResolvedAt,
			Updates:     updates,
			Description: incident.Description,
		}
	}
	return resp
}
