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

// statusIntervalWriter is the subset of *db.StatusIntervalRepository the
// poller depends on to persist an observed status as an open/extended
// interval.
type statusIntervalWriter interface {
	OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) error
}

// integrationStatusUpdater is the subset of *db.IntegrationRepository the
// poller depends on to record each cycle's outcome for the admin (SP-09):
// MarkDatadogInvalid when every service failed, MarkDatadogChecked when at
// least one succeeded (H5/H6).
type integrationStatusUpdater interface {
	MarkDatadogInvalid(ctx context.Context, lastError string) error
	MarkDatadogChecked(ctx context.Context) error
}

// Poller periodically fetches SLO status for every configured service and
// updates its cached current status. It is the only path that talks to the
// SLO provider: pollOnce/pollService are unexported and reached only via
// Run's own ticker, never from a public request (SP-06 - the public status
// page must only ever read the cache, never call Datadog on demand).
type Poller struct {
	services        serviceLister
	statuses        serviceStatusUpdater
	statusIntervals statusIntervalWriter
	integrations    integrationStatusUpdater
	provider        datadog.SLOProvider
	interval        time.Duration
	logger          *zap.Logger
}

// NewPoller builds a Poller that fetches SLO status via provider every
// interval, persisting results through statuses/statusIntervals and
// recording connection failures through integrations.
func NewPoller(services serviceLister, statuses serviceStatusUpdater, statusIntervals statusIntervalWriter, integrations integrationStatusUpdater, provider datadog.SLOProvider, interval time.Duration, logger *zap.Logger) *Poller {
	return &Poller{
		services:        services,
		statuses:        statuses,
		statusIntervals: statusIntervals,
		integrations:    integrations,
		provider:        provider,
		interval:        interval,
		logger:          logger,
	}
}

// Run polls once immediately, then ticks every p.interval polling all
// configured services each cycle, until ctx is canceled - at which point it
// returns, letting the caller (cmd/vane serve) shut down cleanly without
// leaking the goroutine. The immediate poll (M17) matters because Run is
// also what PollerManager.Restart starts right after an admin connects
// Datadog (PLD-01) - without it, time.NewTicker's first tick doesn't fire
// until a full p.interval has elapsed, leaving every service silently
// unconfirmed for up to POLL_INTERVAL_SECONDS right when an admin is
// actively watching for the connection to start working.
func (p *Poller) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
		p.pollOnce(ctx)
	}

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

// pollOnce polls every registered service once, then records the cycle's
// outcome on the Datadog integration exactly once (H5/H6): MarkDatadogInvalid
// only when every service failed this cycle, MarkDatadogChecked when at
// least one succeeded. A single misconfigured SLO among several reachable
// services must not mark the whole integration invalid - the invalid state
// is reserved for a cycle where nothing could be reached at all. Likewise, a
// cycle with at least one success clears any invalid state left over from an
// earlier failure, so a poller that has recovered doesn't stay stuck
// reporting invalid forever (previously nothing ever reverted it).
func (p *Poller) pollOnce(ctx context.Context) {
	services, err := p.services.List(ctx)
	if err != nil {
		p.logger.Error("poller: failed to list services", zap.Error(err))
		return
	}

	var anySuccess bool
	var lastErr error
	for _, svc := range services {
		if err := p.pollService(ctx, svc); err != nil {
			lastErr = err
			continue
		}
		anySuccess = true
	}

	switch {
	case anySuccess:
		if err := p.integrations.MarkDatadogChecked(ctx); err != nil {
			p.logger.Error("poller: failed to mark datadog integration checked", zap.Error(err))
		}
	case lastErr != nil:
		if err := p.integrations.MarkDatadogInvalid(ctx, lastErr.Error()); err != nil {
			p.logger.Error("poller: failed to mark datadog integration invalid", zap.Error(err))
		}
	}
}

// pollService fetches svc's SLO status (with retry), opens or extends its
// status interval, and updates the service's cached current_status. On a
// failed fetch it returns early without touching current_status, so the
// last known valid status stays visible on the public page (SP-08) - it
// never writes a status derived from a failure. It does not itself record
// the failure on the Datadog integration - pollOnce aggregates every
// service's outcome for the cycle first (H5/H6), since a single service's
// failure must not by itself mark the whole integration invalid.
func (p *Poller) pollService(ctx context.Context, svc db.Service) error {
	status, err := FetchWithRetry(ctx, p.provider, svc.SLOID, maxFetchAttempts)
	if err != nil {
		p.logger.Error("poller: failed to fetch slo status",
			zap.String("service_id", svc.ID), zap.String("slo_id", svc.SLOID), zap.Error(err))
		return err
	}

	current := normalizeStatus(status.State)

	if err := p.statusIntervals.OpenOrExtend(ctx, svc.ID, current, status.ErrorBudgetRemaining, time.Now()); err != nil {
		p.logger.Error("poller: failed to open or extend status interval",
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
