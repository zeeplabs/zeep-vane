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

// minRecentWindowRequests is the minimum request volume a poll window must
// carry before its computed state is trusted. Below this, the window is too
// sparse to distinguish a real problem from noise, so pollService carries
// the previous status forward instead of recomputing (AD-019). Conservative
// floor, not derived from real traffic data - flagged in spec.md's
// Assumptions as unconfirmed and easy to tune later.
const minRecentWindowRequests = 10

// recentWindowWidth is how much SLO history pollService asks Datadog for on
// each fetch, independent of p.interval (AD-019 addendum). A window sized to
// the poll interval alone (originally 60s) was proven too narrow against
// live Datadog data: at ~51 req/min steady traffic, the freshest 60s
// reported as few as 8 requests because trace metrics for the last 1-2
// minutes aren't fully aggregated yet, tripping minRecentWindowRequests and
// carrying a stale status forward - the exact bug AD-019 was written to fix.
// 5 minutes of width absorbs that per-minute variance.
const recentWindowWidth = 5 * time.Minute

// recentWindowLag offsets the window's end away from time.Now() so it never
// includes the most recent minute of not-yet-aggregated Datadog data.
// Live-measured: the freshest 60s under-reported request volume by ~6x
// versus the steady rate, while data 60-120s old already matched it.
const recentWindowLag = 60 * time.Second

// breachHysteresisCycles is how many consecutive polling cycles a service
// must report Datadog's "breached" state before pollService actually flips
// its status to "outage" (AD-019 addendum: hysteresis). Without this, a
// single 5-minute window reporting "breached" instantly paints the service
// as down: Datadog computes state against the SLO's configured target
// (typically 99.5% over 30 days) applied unscaled to a 5-minute bucket, so
// on a high-traffic service a single unlucky burst of errors - well within
// normal noise for a 30-day budget - is enough to trip "breached" on its
// own. Live-measured against a real 30-day-healthy SLO: 2 of 6 consecutive
// 5-minute windows reported "breached" despite thousands of requests each,
// which without hysteresis would have flapped the public page (and, via
// internal/history's worst-status-wins hourly bucketing, painted the entire
// hour red) purely from normal variance. Requiring 2 consecutive breaches
// before committing to "outage" absorbs single-window noise while still
// reacting within 2 poll cycles (up to ~2*POLL_INTERVAL_SECONDS) to a real
// outage. See .specs/STATE.md AD-019 addendum for the full analysis and the
// longer-term fix this is a stopgap for.
const breachHysteresisCycles = 2

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
// database connection. Not wired into production (internal/cli/serve.go)
// in this batch - see T15's task notes for why.
type TenantTxFunc func(ctx context.Context, tenantID string) (tenantCtx context.Context, commit func(context.Context) error, rollback func(context.Context), err error)

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
	analyzer        *SLOAnalyzer
	logger          *zap.Logger

	// tenants/tenantTx enable per-tenant iteration (T15, TENANT-04) when
	// both are set via EnableTenantIteration; nil (the default for every
	// existing NewPoller caller) preserves the original single
	// ambient-context poll cycle unchanged.
	tenants  tenantLister
	tenantTx TenantTxFunc

	// breachStreak tracks, per service ID, how many consecutive cycles in a
	// row Datadog has reported "breached" for that service's most recent
	// window (breachHysteresisCycles). Only ever read/written from
	// pollOnce's sequential loop over services (Run drives one poll cycle
	// at a time on a single goroutine), so it needs no locking of its own.
	breachStreak map[string]int
}

// NewPoller builds a Poller that fetches SLO status via provider every
// interval, persisting results through statuses/statusIntervals and
// recording connection failures through integrations. analyzer is notified
// of every status transition (pollService, AI-08 through AI-24 - the
// integration point for the whole SLO-analysis feature).
func NewPoller(services serviceLister, statuses serviceStatusUpdater, statusIntervals statusIntervalWriter, integrations integrationStatusUpdater, provider datadog.SLOProvider, interval time.Duration, analyzer *SLOAnalyzer, logger *zap.Logger) *Poller {
	return &Poller{
		services:        services,
		statuses:        statuses,
		statusIntervals: statusIntervals,
		integrations:    integrations,
		provider:        provider,
		interval:        interval,
		analyzer:        analyzer,
		logger:          logger,
		breachStreak:    make(map[string]int),
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
// session. Not wired into production (internal/cli/serve.go) in this
// batch - see T15's task notes.
func (p *Poller) EnableTenantIteration(tenants tenantLister, tenantTx TenantTxFunc) {
	p.tenants = tenants
	p.tenantTx = tenantTx
}

// pollCycle runs one polling cycle. When tenant iteration is enabled
// (EnableTenantIteration), it enumerates every tenant tenants.List returns
// and runs pollOnce once per tenant, each inside that tenant's own
// app.tenant_id-scoped transaction (TENANT-04) - a tenant with zero
// services configured is skipped without error, since pollOnce's own loop
// over an empty service list is already a no-op. Otherwise it falls back
// to the single ambient-context behavior pollOnce always had, unchanged
// for every existing caller of NewPoller.
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
		tenantCtx, commit, rollback, err := p.tenantTx(ctx, tenant.ID)
		if err != nil {
			p.logger.Error("poller: failed to begin tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			continue
		}

		p.pollOnce(tenantCtx)

		if err := commit(tenantCtx); err != nil {
			p.logger.Error("poller: failed to commit tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			rollback(tenantCtx)
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
	to := time.Now().Add(-recentWindowLag)
	from := to.Add(-recentWindowWidth)

	status, err := FetchWithRetry(ctx, p.provider, svc.SLOID, from, to, maxFetchAttempts)
	if err != nil {
		p.logger.Error("poller: failed to fetch slo status",
			zap.String("service_id", svc.ID), zap.String("slo_id", svc.SLOID), zap.Error(err))
		return err
	}

	var current string
	switch {
	case status.RequestCount < minRecentWindowRequests:
		// Too little traffic in this window to trust a recompute - carry the
		// previous status forward rather than let a handful of requests
		// flip the public page (AD-019).
		current = svc.CurrentStatus
	case status.State == "breached":
		// Hysteresis (AD-019 addendum, breachHysteresisCycles): a single
		// breached window is not enough to commit to "outage" - see the
		// constant's doc comment for why. Carry the previous status forward
		// until the streak clears the threshold, then latch to "outage".
		p.breachStreak[svc.ID]++
		if p.breachStreak[svc.ID] >= breachHysteresisCycles {
			current = "outage"
		} else {
			current = svc.CurrentStatus
		}
	default:
		p.breachStreak[svc.ID] = 0
		current = normalizeStatus(status.State)
	}

	transitioned := current != svc.CurrentStatus

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

	// SLOAnalyzer is notified exactly once per service per cycle, only when
	// a transition actually occurred (AI-06/AI-18) - a service whose status
	// is unchanged this cycle must make zero calls into it, not rely on
	// HandleTransition's own no-op guard to absorb a redundant call every
	// cycle a service stays in the same state. Dispatched only after
	// current_status has actually been persisted above: if OpenOrExtend or
	// UpdateStatus had failed, svc.CurrentStatus in storage would still be
	// the old value and next cycle's comparison would see the same
	// transition again, re-dispatching an LLM call for every subsequent
	// cycle until the write finally succeeds - persisting first makes the
	// transition idempotent from HandleTransition's point of view.
	if transitioned {
		p.analyzer.HandleTransition(ctx, svc, svc.CurrentStatus, current, status)
	}

	return nil
}

// normalizeStatus maps a Datadog SLO state to vane's Service.CurrentStatus
// values (SP-06/SP-07). Documents the full mapping for every state Datadog
// can report, but pollService itself never reaches the "breached" case
// below: it intercepts status.State == "breached" earlier to apply
// breachHysteresisCycles, so this function only ever actually sees "ok"/
// "warning"/anything else in production. Kept here (not deleted) so the
// mapping stays complete and self-documenting, and so a future caller that
// doesn't need hysteresis can still use it correctly.
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
