package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

// serviceLister is the subset of *db.ServiceRepository the public status
// handler depends on. It is scoped to a single status page (SP-15): the
// public page must show only the services linked to it, never every
// service in the installation.
type serviceLister interface {
	ListForStatusPage(ctx context.Context, statusPageID string) ([]db.Service, error)
}

// latestSnapshotFetcher is the subset of *db.StatusSnapshotRepository the
// public status handler depends on.
type latestSnapshotFetcher interface {
	LatestFetchedAtByService(ctx context.Context) (map[string]time.Time, error)
}

// publicIncidentLister is the subset of *db.IncidentRepository the public
// status handler depends on. It is scoped to a single status page (SP-15):
// only incidents linked to a service that status page publishes may appear.
type publicIncidentLister interface {
	ListPublicForStatusPage(ctx context.Context, statusPageID string, retentionDays int) (active, resolved []db.IncidentPublic, err error)
}

// companySettingsGetter is the subset of *db.CompanySettingsRepository the
// public status handler depends on, to surface the real company identity
// instead of mockData.companySettings (SET-15, SET-16).
type companySettingsGetter interface {
	Get(ctx context.Context) (*db.CompanySettings, error)
}

// incidentRetentionDays is the public status page's incident history
// retention window (spec.md Assumptions: "Janela de retenção de histórico
// de incidentes/uptime" - 90 dias).
const incidentRetentionDays = 90

// PublicStatusHandler serves the public, unauthenticated status page
// endpoint (SP-10). It never talks to Datadog directly - it only reads
// what the poller (Phase 4) has already persisted, so a Datadog outage
// (including the connected Integration being marked "invalid") never takes
// the public page down: it keeps serving the last snapshot on record, with
// its real fetched_at timestamp, never a fabricated "now" (SP-08, SP-09).
type PublicStatusHandler struct {
	services        serviceLister
	snapshots       latestSnapshotFetcher
	incidents       publicIncidentLister
	companySettings companySettingsGetter
	logger          *zap.Logger
}

// NewPublicStatusHandler builds a PublicStatusHandler backed by services,
// snapshots, incidents, and companySettings.
func NewPublicStatusHandler(services serviceLister, snapshots latestSnapshotFetcher, incidents publicIncidentLister, companySettings companySettingsGetter, logger *zap.Logger) *PublicStatusHandler {
	return &PublicStatusHandler{services: services, snapshots: snapshots, incidents: incidents, companySettings: companySettings, logger: logger}
}

type publicServiceResponse struct {
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
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
}

type publicIncidentsResponse struct {
	Active   []publicIncidentResponse `json:"active"`
	Resolved []publicIncidentResponse `json:"resolved"`
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

	resp, err := h.composeResponse(r.Context(), statusPageID)
	if err != nil {
		h.logger.Error("public-status: failed to compose response", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// composeResponse builds the public status response for statusPageID -
// shared by Get (resolved by Host header, production) and
// PublicStatusPreviewHandler.Get (resolved by ID, dev/preview - I12).
func (h *PublicStatusHandler) composeResponse(ctx context.Context, statusPageID string) (publicStatusResponse, error) {
	services, err := h.services.ListForStatusPage(ctx, statusPageID)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list services: %w", err)
	}

	latestFetchedAt, err := h.snapshots.LatestFetchedAtByService(ctx)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to load latest status snapshots: %w", err)
	}

	activeIncidents, resolvedIncidents, err := h.incidents.ListPublicForStatusPage(ctx, statusPageID, incidentRetentionDays)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to list public incidents: %w", err)
	}

	companySettings, err := h.companySettings.Get(ctx)
	if err != nil {
		return publicStatusResponse{}, fmt.Errorf("failed to get company settings: %w", err)
	}

	resp := publicStatusResponse{
		Company:  publicCompanyResponse{Name: companySettings.Name, LogoURL: companySettings.LogoURL},
		Services: []publicServiceResponse{},
		Incidents: publicIncidentsResponse{
			Active:   toPublicIncidentResponses(activeIncidents),
			Resolved: toPublicIncidentResponses(resolvedIncidents),
		},
	}
	for _, service := range services {
		// A service with no SLO linked yet stays "not_configured" and is
		// never shown publicly (spec.md edge case) - it's an admin-side
		// concept only, until a poller cycle gives it a real status.
		if service.CurrentStatus == "not_configured" {
			continue
		}

		// A service the poller has never successfully reached yet has no
		// entry here; LastUpdatedAt then stays the zero value rather than a
		// fabricated "now" (SP-08, SP-09, edge case in spec.md).
		resp.Services = append(resp.Services, publicServiceResponse{
			Name:          service.Name,
			Status:        service.CurrentStatus,
			LastUpdatedAt: latestFetchedAt[service.ID],
		})
	}

	return resp, nil
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
			ID:         incident.ID,
			Title:      incident.Title,
			Status:     incident.Status,
			CreatedAt:  incident.CreatedAt,
			ResolvedAt: incident.ResolvedAt,
			Updates:    updates,
		}
	}
	return resp
}
