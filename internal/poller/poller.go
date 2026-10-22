package poller

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// maxFetchAttempts is the total number of attempts FetchWindowsWithRetry
// makes per service per cycle (SP-05).
const maxFetchAttempts = 3

// recentWindowLag offsets the windows' end away from time.Now() so they
// never include the most recent minute of not-yet-aggregated Datadog data.
// Live-measured: the freshest 60s under-reported request volume by ~6x
// versus the steady rate, while data 60-120s old already matched it. Both
// burn-rate windows (shortWindow, longWindow) end at now - recentWindowLag
// (BURN-01).
const recentWindowLag = 60 * time.Second

// serviceLister is the subset of *db.ServiceRepository the poller depends on
// to discover which services to poll.
type serviceLister interface {
	List(ctx context.Context) ([]db.Service, error)
}

// tenantLister is the subset of *db.TenantRepository the poller depends on
// to discover which tenants to iterate (T15, TENANT-04) - never a
// BYPASSRLS role, one tenant's app.tenant_id set at a time via TenantTxFunc.
type tenantLister interface {
	List(ctx context.Context) ([]db.Tenant, error)
}

// TenantTxFunc opens a transaction scoped to tenantID's RLS session
// (app.tenant_id) and returns a context carrying it - every repository
// call issued against the returned context runs on that same transaction,
// so RLS's session settings apply (see db.WithTenantTx) - plus functions to
// commit or roll it back. The poller depends on this function type rather
// than *db.Pool directly (TENANT-04: iterate tenants one at a time via a
// real transaction per tenant, never a role that bypasses RLS), which also
// lets this package's own unit tests fake tenant iteration without a
// database connection. Production wires it to Pool.BeginTenantTx via
// cli.poolTenantTx (internal/cli/serve.go).
type TenantTxFunc func(ctx context.Context, tenantID string) (tenantCtx context.Context, commit func(context.Context) error, rollback func(context.Context), err error)

// DatadogClient is the subset of *datadog.Client this package depends on:
// SLO status fetch (Poller.pollService, via the embedded datadog.SLOProvider)
// and the error-tracking cause-hint lookup (SLOAnalyzer.resolveCauseHint,
// via the embedded errorCauseProvider) alike. Exported (unlike
// errorCauseProvider) so a resolver built outside this package
// (cli.newPollerFromStoredIntegration) can declare a matching return type,
// and so this package's own tests can fake both methods on one double.
// Satisfied by the real *datadog.Client with no adapter needed.
type DatadogClient interface {
	datadog.SLOProvider
	errorCauseProvider
}

// DatadogClientResolver resolves the Datadog client for whichever tenant is
// active on ctx (integrations-tenant-scope, TENANT-05) - production
// resolves this by reading that tenant's own `integrations` row
// (RLS-scoped: ctx must carry that tenant's BeginTenantTx, same as every
// other tenant-scoped repository call) and decrypting its stored
// credentials fresh every cycle, never a client fixed once at boot for
// every tenant. Returns db.ErrNotFound when this tenant hasn't connected
// Datadog yet - pollCycle treats that as "nothing to poll this tenant this
// cycle", not a failure.
type DatadogClientResolver func(ctx context.Context) (DatadogClient, error)

// serviceStatusUpdater is the subset of *db.ServiceRepository the poller
// depends on to persist a service's newly observed status.
type serviceStatusUpdater interface {
	UpdateStatus(ctx context.Context, serviceID, status string) error
}

// serviceStatusReadingWriter is the subset of *db.ServiceRepository the
// Datadog Poller depends on: UpdateStatus for the state-based fallback
// (no counts, so no reading), UpdateStatusAndReading for a burn-rate
// classification, which persists the status and that cycle's reading in
// one UPDATE (BURN-13, BURN-17). ManualScheduler keeps depending on
// serviceStatusUpdater alone - it never produces a reading.
type serviceStatusReadingWriter interface {
	serviceStatusUpdater
	UpdateStatusAndReading(ctx context.Context, serviceID, status string, rd db.ServiceReading) error
}

// statusIntervalWriter is the subset of *db.StatusIntervalRepository the
// poller depends on to persist an observed status as an open/extended
// interval.
type statusIntervalWriter interface {
	OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) (string, error)
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
	statuses        serviceStatusReadingWriter
	statusIntervals statusIntervalWriter
	integrations    integrationStatusUpdater
	provider        datadog.SLOProvider
	interval        time.Duration
	analyzer        *SLOAnalyzer
	logger          *zap.Logger

	// tenants/tenantTx enable per-tenant iteration (T15, TENANT-04) when
	// both are set via EnableTenantIteration; nil (the default for every
	// existing NewPoller caller) preserves the original single
	// ambient-context poll cycle unchanged.
	tenants  tenantLister
	tenantTx TenantTxFunc

	// resolveDatadogClient, when set via EnableTenantDatadogClients, swaps
	// p.provider (and the causeProvider threaded into HandleTransition) for
	// whichever tenant pollCycle is currently iterating, instead of the
	// single client fixed at NewPoller construction
	// (integrations-tenant-scope). nil (the default) preserves the
	// original single ambient provider for every existing test/caller that
	// doesn't opt in.
	resolveDatadogClient DatadogClientResolver
}

// NewPoller builds a Poller that fetches SLO status via provider every
// interval, persisting results through statuses/statusIntervals and
// recording connection failures through integrations. analyzer is notified
// of every status transition (pollService, AI-08 through AI-24 - the
// integration point for the whole SLO-analysis feature).
func NewPoller(services serviceLister, statuses serviceStatusReadingWriter, statusIntervals statusIntervalWriter, integrations integrationStatusUpdater, provider datadog.SLOProvider, interval time.Duration, analyzer *SLOAnalyzer, logger *zap.Logger) *Poller {
	return &Poller{
		services:        services,
		statuses:        statuses,
		statusIntervals: statusIntervals,
		integrations:    integrations,
		provider:        provider,
		interval:        interval,
		analyzer:        analyzer,
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
		p.pollCycle(ctx)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollCycle(ctx)
		}
	}
}

// EnableTenantIteration wires per-tenant iteration into this Poller
// (T15, TENANT-04): once both are set, every subsequent pollCycle
// enumerates tenants.List and processes each one's services inside its own
// tenant-scoped transaction (tenantTx), never a single ambient/shared
// session. Production calls this from newPollerFromStoredIntegration
// (internal/cli/serve.go) with db.SystemTenantLister and cli.poolTenantTx.
func (p *Poller) EnableTenantIteration(tenants tenantLister, tenantTx TenantTxFunc) {
	p.tenants = tenants
	p.tenantTx = tenantTx
}

// EnableTenantDatadogClients wires per-tenant Datadog credential resolution
// (integrations-tenant-scope, TENANT-05): once set, every pollCycle
// iteration resolves and swaps in that tenant's own Datadog client - for
// both SLO status fetches and HandleTransition's root-cause enrichment
// lookup - before polling its services, instead of the single client fixed
// at NewPoller construction. A tenant whose resolve returns db.ErrNotFound
// (hasn't connected Datadog yet) is skipped for this cycle, not an error.
func (p *Poller) EnableTenantDatadogClients(resolve DatadogClientResolver) {
	p.resolveDatadogClient = resolve
}

// TenantIterationEnabled reports whether EnableTenantIteration has wired
// both a tenant lister and a tenant transaction opener, i.e. whether
// pollCycle iterates tenants instead of falling back to the single
// ambient-context poll. Exported so the production construction path
// (internal/cli) can be asserted on from its own package's tests rather
// than only being verifiable by reading the code.
func (p *Poller) TenantIterationEnabled() bool {
	return p.tenants != nil && p.tenantTx != nil
}

// pollCycle runs one polling cycle. When tenant iteration is enabled
// (EnableTenantIteration), it enumerates every tenant tenants.List returns
// and runs pollOnce once per tenant, each inside that tenant's own
// app.tenant_id-scoped transaction (TENANT-04) - a tenant with zero
// services configured is skipped without error, since pollOnce's own loop
// over an empty service list is already a no-op. Otherwise it falls back
// to the single ambient-context behavior pollOnce always had, unchanged
// for every existing caller of NewPoller.
//
// Each tenant's transaction (and the Datadog calls pollOnce makes while it
// is open, per AD-024's trade-off note) is bounded to p.interval so one
// slow/hanging tenant can never hold its connection past the next tick -
// worst case that tenant's cycle is cut short and retried next interval,
// it never blocks the tenants after it in the same cycle indefinitely.
func (p *Poller) pollCycle(ctx context.Context) {
	if p.tenants == nil || p.tenantTx == nil {
		p.pollOnce(ctx)
		return
	}

	tenants, err := p.tenants.List(ctx)
	if err != nil {
		p.logger.Error("poller: failed to list tenants", zap.Error(err))
		return
	}

	for _, tenant := range tenants {
		tenantCtx, cancel := context.WithTimeout(ctx, p.interval)

		tenantTxCtx, commit, rollback, err := p.tenantTx(tenantCtx, tenant.ID)
		if err != nil {
			p.logger.Error("poller: failed to begin tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			cancel()
			continue
		}
		tenantCtx = tenantTxCtx
		tenantCtx = withTenantID(tenantCtx, tenant.ID)

		if p.resolveDatadogClient != nil {
			client, err := p.resolveDatadogClient(tenantCtx)
			if err != nil {
				if !errors.Is(err, db.ErrNotFound) {
					p.logger.Error("poller: failed to resolve datadog client for tenant", zap.String("tenant_id", tenant.ID), zap.Error(err))
				}
				rollback(tenantCtx)
				cancel()
				continue
			}
			p.provider = client
		}

		p.pollOnce(tenantCtx)

		if err := commit(tenantCtx); err != nil {
			p.logger.Error("poller: failed to commit tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			rollback(tenantCtx)
		}
		cancel()
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
		// A polling-manual service (manual-polling-monitoring) has no
		// SLOID at all - it is checked by poller.ManualScheduler instead,
		// never by this Datadog-specific poller (design.md: "zero change
		// to the existing Datadog poll path"). services.List is
		// intentionally unfiltered (Overview/digest still need every
		// service), so this poller must filter it out itself.
		if svc.MonitorMode == "polling" {
			continue
		}
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

// pollService fetches svc's short/long SLO windows (with retry),
// classifies its status, opens or extends its status interval, and updates
// the service's cached current_status. On a failed fetch it returns early
// without touching current_status, so the last known valid status stays
// visible on the public page (SP-08, BURN-11) - it never writes a status
// derived from a failure. It does not itself record the failure on the
// Datadog integration - pollOnce aggregates every service's outcome for the
// cycle first (H5/H6), since a single service's failure must not by itself
// mark the whole integration invalid.
//
// Classification (AD-049, superseding AD-019's window-SLI heuristics): a
// metric-based SLO with a usable target is classified by multi-window burn
// rate (classifyBurn) and its reading persisted with the status; an SLO with
// no target (Target <= 0, BURN-09) or no request counts at all
// (monitor-based, !HasSeries, NB-7) falls back to Datadog's own state over
// the short window (classifyByState), persisted without a reading.
func (p *Poller) pollService(ctx context.Context, svc db.Service) error {
	now := time.Now()
	end := now.Add(-recentWindowLag)

	windows, err := FetchWindowsWithRetry(ctx, p.provider, svc.SLOID, end, shortWindow, longWindow, maxFetchAttempts)
	if err != nil {
		p.logger.Error("poller: failed to fetch slo status",
			zap.String("service_id", svc.ID), zap.String("slo_id", svc.SLOID), zap.Error(err))
		return err
	}

	short := windowCounts{Requests: windows.Short.Requests, Errors: windows.Short.Errors}
	long := windowCounts{Requests: windows.Long.Requests, Errors: windows.Long.Errors}

	// reading stays nil on the state-based fallback: without counts (or
	// without a target to budget against) there is no burn rate to record,
	// and the previous reading/last_error_at are left as they are.
	var current string
	var reading *db.ServiceReading
	var status datadog.SLOStatus
	if windows.HasSeries && windows.Target > 0 {
		current = classifyBurn(burnInput{
			Short:        short,
			Long:         long,
			Target:       windows.Target,
			Previous:     svc.CurrentStatus,
			LastErrorAt:  svc.LastErrorAt,
			LastChangeAt: svc.LastStatusChangeAt,
			Now:          now,
		})
		reading = newServiceReading(short, long, windows.Target, svc.LastErrorAt, now)
		status = sloStatusFromWindows(windows)
	} else {
		// The windows call's State covers its whole ~1h range, which would
		// hold a recovered service in degraded/outage for up to an hour.
		// The fallback instead asks Datadog for its state over the short
		// window only, the same 5-minute range it classified from before
		// AD-049. This second /history call is made only for fallback
		// services (monitor-based SLOs and SLOs without a target); the burn
		// path stays one call per service per cycle. Its failure follows
		// the same contract as the first call's: log, return, no writes.
		status, err = FetchStatusWithRetry(ctx, p.provider, svc.SLOID, end.Add(-shortWindow), end, maxFetchAttempts)
		if err != nil {
			p.logger.Error("poller: failed to fetch slo status",
				zap.String("service_id", svc.ID), zap.String("slo_id", svc.SLOID), zap.Error(err))
			return err
		}
		current = classifyByState(status.State)
	}

	transitioned := current != svc.CurrentStatus

	// Debug-level, every cycle regardless of transition: the only record of
	// what classification inputs pollService actually saw. Without this, a
	// disputed status (public page shows degraded/outage, Datadog's own
	// monitors/SLO never fired) is unfalsifiable after the fact - the public
	// status interval records the outcome, never the counts/burn rates that
	// produced it, and pod logs don't survive a redeploy. Logged before the
	// writes below so it's captured even if OpenOrExtend/the status write
	// fails. burn_short/burn_long are 0 on the state-based fallback, and may
	// be +Inf for a 100% target (zap's JSON encoder writes it as "+Inf").
	var burnShort, burnLong float64
	if reading != nil {
		burnShort, burnLong = reading.BurnShort, reading.BurnLong
	}
	p.logger.Debug("poller: classified service",
		zap.String("service_id", svc.ID),
		zap.String("slo_id", svc.SLOID),
		zap.String("previous_status", svc.CurrentStatus),
		zap.String("new_status", current),
		zap.Bool("transitioned", transitioned),
		zap.Bool("burn_rate_classified", reading != nil),
		zap.Bool("has_series", windows.HasSeries),
		zap.Float64("target", status.Target),
		zap.String("datadog_state", status.State),
		zap.Int64("short_requests", short.Requests),
		zap.Int64("short_errors", short.Errors),
		zap.Int64("long_requests", long.Requests),
		zap.Int64("long_errors", long.Errors),
		zap.Float64("burn_short", burnShort),
		zap.Float64("burn_long", burnLong),
	)

	intervalID, err := p.statusIntervals.OpenOrExtend(ctx, svc.ID, current, status.ErrorBudgetRemaining, now)
	if err != nil {
		p.logger.Error("poller: failed to open or extend status interval",
			zap.String("service_id", svc.ID), zap.Error(err))
		return err
	}

	if reading != nil {
		err = p.statuses.UpdateStatusAndReading(ctx, svc.ID, current, *reading)
	} else {
		err = p.statuses.UpdateStatus(ctx, svc.ID, current)
	}
	if err != nil {
		p.logger.Error("poller: failed to update service status",
			zap.String("service_id", svc.ID), zap.Error(err))
		return err
	}

	// SLOAnalyzer is notified exactly once per service per cycle, only when
	// a transition actually occurred (AI-06/AI-18) - a service whose status
	// is unchanged this cycle must make zero calls into it, not rely on
	// HandleTransition's own no-op guard to absorb a redundant call every
	// cycle a service stays in the same state. Dispatched only after
	// current_status has actually been persisted above: if OpenOrExtend or
	// the status write had failed, svc.CurrentStatus in storage would still
	// be the old value and next cycle's comparison would see the same
	// transition again, re-dispatching an LLM call for every subsequent
	// cycle until the write finally succeeds - persisting first makes the
	// transition idempotent from HandleTransition's point of view. reading
	// (nil on the state-based fallback) carries this cycle's burn numbers
	// into the analyzer's prompts (BURN-15) - including the closing comment
	// on a recovery, which gets the reading of the cycle that recovered.
	if transitioned {
		// causeProvider is derived from p.provider (already the current
		// tenant's own client when EnableTenantDatadogClients is wired,
		// integrations-tenant-scope) rather than read from a field inside
		// HandleTransition's own async enrichment goroutine - the goroutine
		// captures this value in its closure at dispatch time, so it can
		// never race against pollCycle swapping p.provider for the next
		// tenant while an earlier tenant's enrichment call is still in
		// flight.
		var causeProvider errorCauseProvider
		if cp, ok := p.provider.(errorCauseProvider); ok {
			causeProvider = cp
		}
		p.analyzer.HandleTransition(ctx, svc, svc.CurrentStatus, current, status, reading, intervalID, causeProvider)
	}

	return nil
}

// newServiceReading builds the reading persisted with a burn-rate
// classification (BURN-13). The burn rates match what classifyBurn decided
// on: an empty long window takes the short burn (BURN-17). lastErrorAt is
// the cycle time when the short window had any failed request - the
// moment the poller observed it, the same clock basis as
// last_status_change_at, which the zero-traffic grace falls back to
// (BURN-06) - and otherwise the previously persisted value, carried forward
// because UpdateStatusAndReading writes NULL for a nil LastErrorAt.
func newServiceReading(short, long windowCounts, target float64, previousErrorAt *time.Time, now time.Time) *db.ServiceReading {
	burnShort := burnRate(short.Errors, short.Requests, target)
	burnLong := burnShort
	if long.Requests > 0 {
		burnLong = burnRate(long.Errors, long.Requests, target)
	}

	lastErrorAt := previousErrorAt
	if short.Errors > 0 {
		observed := now
		lastErrorAt = &observed
	}

	return &db.ServiceReading{
		BurnShort:     burnShort,
		BurnLong:      burnLong,
		ShortErrors:   short.Errors,
		ShortRequests: short.Requests,
		ReadAt:        now,
		LastErrorAt:   lastErrorAt,
	}
}

// sloStatusFromWindows adapts this cycle's windows to the datadog.SLOStatus
// shape status_intervals.error_budget_remaining and
// SLOAnalyzer.HandleTransition still consume. SLI is the short window's own
// success ratio when it has traffic (the window classification is driven
// by), otherwise Datadog's overall SLI for the fetched range;
// ErrorBudgetRemaining keeps its SLI - Target approximation (AD-019
// addendum 2) and RequestCount is the short window's.
func sloStatusFromWindows(w datadog.SLOWindows) datadog.SLOStatus {
	status := datadog.SLOStatus{
		State:        w.State,
		SLI:          w.SLI,
		Target:       w.Target,
		Timeframe:    w.Timeframe,
		RequestCount: w.Short.Requests,
	}
	if w.HasSeries && w.Short.Requests > 0 {
		errs := min(max(w.Short.Errors, 0), w.Short.Requests)
		status.SLI = 100 * float64(w.Short.Requests-errs) / float64(w.Short.Requests)
	}
	status.ErrorBudgetRemaining = status.SLI - status.Target
	return status
}

// classifyByState is the state-based fallback for an SLO burn rate can't be
// computed for: no usable target (Target <= 0, BURN-09) or no request
// counts at all (monitor-based SLO, NB-7). It maps Datadog's own state
// through normalizeStatus. The consecutive-breach hysteresis it used to
// apply was removed with the rest of AD-019's in-memory classification
// state (AD-049): it lived only in memory, so it was lost on restart/HA
// handover.
func classifyByState(state string) string {
	return normalizeStatus(state)
}

// normalizeStatus maps a Datadog SLO state to vane's Service.CurrentStatus
// values (SP-06/SP-07). Only the state-based fallback (classifyByState)
// reaches it; burn-rate classification never maps Datadog's state, so a
// metric SLO's no_data is never read as "degraded" (BURN-08).
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
