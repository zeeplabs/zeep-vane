package poller

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// maxFetchAttempts is the total number of attempts FetchWithRetry makes per
// service per cycle (SP-05).
const maxFetchAttempts = 3

// serviceLister is the subset of *db.ServiceRepository the poller depends on
// to discover which services to poll.
type serviceLister interface {
	List(ctx context.Context) ([]db.Service, error)
}

// serviceStatusUpdater is the subset of *db.ServiceRepository the poller
// depends on to persist a service's newly observed status.
type serviceStatusUpdater interface {
	UpdateStatus(ctx context.Context, serviceID, status string) error
}

// snapshotCreator is the subset of *db.StatusSnapshotRepository the poller
// depends on to persist a point-in-time status snapshot.
type snapshotCreator interface {
	Create(ctx context.Context, snapshot *db.StatusSnapshot) error
}

// integrationInvalidator is the subset of *db.IntegrationRepository the
// poller depends on to record a connection failure for the admin (SP-09).
type integrationInvalidator interface {
	MarkDatadogInvalid(ctx context.Context, lastError string) error
}

// Poller periodically fetches SLO status for every configured service and
// updates its cached current status. It is the only path that talks to the
// SLO provider: pollOnce/pollService are unexported and reached only via
// Run's own ticker, never from a public request (SP-06 - the public status
// page must only ever read the cache, never call Datadog on demand).
type Poller struct {
	services     serviceLister
	statuses     serviceStatusUpdater
	snapshots    snapshotCreator
	integrations integrationInvalidator
	provider     datadog.SLOProvider
	interval     time.Duration
	logger       *zap.Logger
}

// NewPoller builds a Poller that fetches SLO status via provider every
// interval, persisting results through statuses/snapshots and recording
// connection failures through integrations.
func NewPoller(services serviceLister, statuses serviceStatusUpdater, snapshots snapshotCreator, integrations integrationInvalidator, provider datadog.SLOProvider, interval time.Duration, logger *zap.Logger) *Poller {
	return &Poller{
		services:     services,
		statuses:     statuses,
		snapshots:    snapshots,
		integrations: integrations,
		provider:     provider,
		interval:     interval,
		logger:       logger,
	}
}

// Run ticks every p.interval, polling all configured services each cycle,
// until ctx is canceled - at which point it returns, letting the caller
// (cmd/vane serve) shut down cleanly without leaking the goroutine.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

// pollOnce polls every registered service once.
func (p *Poller) pollOnce(ctx context.Context) {
	services, err := p.services.List(ctx)
	if err != nil {
		p.logger.Error("poller: failed to list services", zap.Error(err))
		return
	}

	for _, svc := range services {
		p.pollService(ctx, svc)
	}
}

// pollService fetches svc's SLO status (with retry), persists a snapshot,
// and updates the service's cached current_status. On a failed fetch it
// records the connection failure on the Datadog integration for the admin
// (SP-09) and returns early without touching current_status, so the last
// known valid status stays visible on the public page (SP-08) - it never
// writes a status derived from a failure.
func (p *Poller) pollService(ctx context.Context, svc db.Service) error {
	status, err := FetchWithRetry(ctx, p.provider, svc.SLOID, maxFetchAttempts)
	if err != nil {
		p.logger.Error("poller: failed to fetch slo status",
			zap.String("service_id", svc.ID), zap.String("slo_id", svc.SLOID), zap.Error(err))

		if markErr := p.integrations.MarkDatadogInvalid(ctx, err.Error()); markErr != nil {
			p.logger.Error("poller: failed to mark datadog integration invalid",
				zap.Error(markErr))
		}

		return err
	}

	current := normalizeStatus(status.State)

	if err := p.snapshots.Create(ctx, &db.StatusSnapshot{
		ServiceID:            svc.ID,
		Status:               current,
		ErrorBudgetRemaining: status.ErrorBudgetRemaining,
	}); err != nil {
		p.logger.Error("poller: failed to persist status snapshot",
			zap.String("service_id", svc.ID), zap.Error(err))
		return err
	}

	if err := p.statuses.UpdateStatus(ctx, svc.ID, current); err != nil {
		p.logger.Error("poller: failed to update service status",
			zap.String("service_id", svc.ID), zap.Error(err))
		return err
	}

	return nil
}

// normalizeStatus maps a Datadog SLO state to vane's Service.CurrentStatus
// values (SP-06/SP-07).
func normalizeStatus(state string) string {
	switch state {
	case "ok":
		return "operational"
	case "warning":
		return "degraded"
	case "breached":
		return "outage"
	default:
		// SPEC_DEVIATION: Datadog's SLO state enum also includes "no_data"
		// (not enough data to compute the SLI yet), which design.md's
		// Service.CurrentStatus doesn't have a matching value for. The spec
		// never allows claiming "operational" on indeterminate data, so any
		// unrecognized/no_data state is treated as "degraded" rather than
		// silently keeping a stale healthy status.
		return "degraded"
	}
}
