package poller

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/checks"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// manualDiscoveryInterval is how often ManualScheduler's reconciliation
// loop re-lists polling-manual services to discover newly created ones
// (design.md Tech Decisions): a cheap query, and well inside the spec's
// own "starts being checked within one interval" budget since the
// smallest configurable poll_interval_seconds is 30s.
const manualDiscoveryInterval = 15 * time.Second

// manualCheckTimeout is the fixed per-check timeout every polling-manual
// check uses, regardless of check type (spec.md Assumptions: 10s for
// HTTP(S)/TCP/Ping alike - MP-07/MP-08).
const manualCheckTimeout = 10 * time.Second

// manualFailureThreshold is how many consecutive failed checks a polling-
// manual service must accumulate before its status flips to "outage"
// (MP-06/MP-11 - mirrors the Datadog poller's own breachHysteresisCycles,
// AD-019).
const manualFailureThreshold = 2

// pollingServiceLister is the subset of *db.ServiceRepository
// ManualScheduler depends on to discover polling-manual services -
// mirrors serviceLister's existing shape (design.md).
type pollingServiceLister interface {
	ListPollingManual(ctx context.Context) ([]db.Service, error)
}

// ManualScheduler discovers polling-manual services (per tenant) and
// checks each one on its own configured interval, entirely independent of
// the Datadog-based Poller (manual-polling-monitoring design.md): it has
// its own reconciliation loop, its own per-service goroutines, and never
// calls SLOAnalyzer (polling-manual services never reach "degraded" and
// never get an AI-authored StatusAnalysis - design.md Risks & Concerns).
type ManualScheduler struct {
	services        pollingServiceLister
	statuses        serviceStatusUpdater
	statusIntervals statusIntervalWriter
	tenants         tenantLister
	tenantTx        TenantTxFunc
	logger          *zap.Logger

	// discoveryInterval defaults to manualDiscoveryInterval; only ever
	// overridden directly (unexported field, same-package tests) to keep
	// tests fast without changing production behavior.
	discoveryInterval time.Duration
}

// NewManualScheduler builds a ManualScheduler backed by services (for
// discovery), statuses/statusIntervals (for persisting check results), and
// tenants/tenantTx (for per-tenant iteration - TENANT-04, the same
// contract Poller itself uses).
func NewManualScheduler(services pollingServiceLister, statuses serviceStatusUpdater, statusIntervals statusIntervalWriter, tenants tenantLister, tenantTx TenantTxFunc, logger *zap.Logger) *ManualScheduler {
	return &ManualScheduler{
		services:          services,
		statuses:          statuses,
		statusIntervals:   statusIntervals,
		tenants:           tenants,
		tenantTx:          tenantTx,
		logger:            logger,
		discoveryInterval: manualDiscoveryInterval,
	}
}

// trackedService is what reconcile remembers about a service ID it has
// already spawned a goroutine for: which tenant owns it (so a tenant whose
// list call fails this round never has its services mistaken for deleted -
// see reconcile) and the context.CancelFunc that stops its goroutine
// (service-delete SVCDEL-06).
type trackedService struct {
	tenantID string
	cancel   context.CancelFunc
}

// Run starts the reconciliation loop: an immediate first pass, then every
// s.discoveryInterval, until ctx is canceled - the same immediate-first-
// pass-then-tick shape as Poller.Run. Every per-service goroutine spawned
// along the way runs under its own context.WithCancel child of ctx (so
// reconcile can stop one service's goroutine independently of the others
// when it's soft-deleted, SVCDEL-06) and is waited on before Run returns,
// so canceling ctx deterministically leaves no goroutine running past this
// call (design.md Risks & Concerns: goroutine-leak guard) - callers never
// need to guess how long shutdown takes.
func (s *ManualScheduler) Run(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	defer wg.Wait()

	tracked := make(map[string]trackedService)

	select {
	case <-ctx.Done():
		return
	default:
		s.reconcile(runCtx, tracked, &wg)
	}

	ticker := time.NewTicker(s.discoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(runCtx, tracked, &wg)
		}
	}
}

// reconcile lists every tenant, then that tenant's polling-manual services
// (inside a short tenant-scoped transaction, TENANT-04 - services carries
// RLS same as every other tenant-scoped table), spawns one goroutine per
// service ID not already in tracked, and cancels the goroutine of any
// tracked service ID that no longer appears in its tenant's live list
// (service-delete SVCDEL-06: the service was soft-deleted, so
// ListPollingManual - which filters deleted_at IS NULL - stopped returning
// it). A tenant whose transaction or list call fails is logged and skipped
// for this pass, same failure posture as Poller.pollCycle - its
// already-tracked services are left untouched (neither respawned nor
// canceled) rather than mistaken for deleted, since a failed list call
// can't tell "deleted" apart from "transiently unreadable".
func (s *ManualScheduler) reconcile(ctx context.Context, tracked map[string]trackedService, wg *sync.WaitGroup) {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		s.logger.Error("manual_scheduler: failed to list tenants", zap.Error(err))
		return
	}

	liveIDs := make(map[string]bool)
	failedTenantIDs := make(map[string]bool)

	for _, tenant := range tenants {
		tenantCtx, commit, rollback, err := s.tenantTx(ctx, tenant.ID)
		if err != nil {
			s.logger.Error("manual_scheduler: failed to begin tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			failedTenantIDs[tenant.ID] = true
			continue
		}

		services, err := s.services.ListPollingManual(tenantCtx)
		if err != nil {
			s.logger.Error("manual_scheduler: failed to list polling-manual services", zap.String("tenant_id", tenant.ID), zap.Error(err))
			rollback(tenantCtx)
			failedTenantIDs[tenant.ID] = true
			continue
		}

		if err := commit(tenantCtx); err != nil {
			s.logger.Error("manual_scheduler: failed to commit tenant transaction", zap.String("tenant_id", tenant.ID), zap.Error(err))
			rollback(tenantCtx)
			failedTenantIDs[tenant.ID] = true
			continue
		}

		for _, svc := range services {
			liveIDs[svc.ID] = true
			if _, ok := tracked[svc.ID]; ok {
				continue
			}

			svcCtx, svcCancel := context.WithCancel(ctx)
			tracked[svc.ID] = trackedService{tenantID: tenant.ID, cancel: svcCancel}

			wg.Add(1)
			go func(tenantID string, svc db.Service) {
				defer wg.Done()
				s.runServiceLoop(svcCtx, tenantID, svc)
			}(tenant.ID, svc)
		}
	}

	for id, svc := range tracked {
		if liveIDs[id] || failedTenantIDs[svc.tenantID] {
			continue
		}
		svc.cancel()
		delete(tracked, id)
	}
}

// runServiceLoop runs svc's own check loop: an immediate first check, then
// every svc.PollIntervalSeconds, until ctx is canceled. failureStreak and
// currentStatus are local to this one goroutine - no shared map/lock is
// needed (an improvement over the Datadog poller's necessarily-shared
// breachStreak map, design.md Approach Exploration), and both reset to
// zero/not_configured's-successor-status on every new goroutine, matching
// the spec's Edge Cases: a leadership change starts every service's
// consecutive-failure count at 0.
func (s *ManualScheduler) runServiceLoop(ctx context.Context, tenantID string, svc db.Service) {
	failureStreak := 0
	currentStatus := svc.CurrentStatus
	pollType := ""
	if svc.PollType != nil {
		pollType = *svc.PollType
	}
	pollTarget := ""
	if svc.PollTarget != nil {
		pollTarget = *svc.PollTarget
	}
	intervalSeconds := 0
	if svc.PollIntervalSeconds != nil {
		intervalSeconds = *svc.PollIntervalSeconds
	}
	if intervalSeconds <= 0 {
		// Defensive: the DB constraint (0034_service_polling_mode)
		// guarantees a polling-mode row always carries a positive
		// poll_interval_seconds, but time.NewTicker panics on a
		// non-positive duration - never let a malformed row take the
		// whole scheduler down.
		s.logger.Error("manual_scheduler: service has no positive poll_interval_seconds, skipping", zap.String("service_id", svc.ID))
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second

	runOnce := func() {
		currentStatus = s.runCheck(ctx, tenantID, svc.ID, pollType, pollTarget, currentStatus, &failureStreak)
	}

	select {
	case <-ctx.Done():
		return
	default:
		runOnce()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// runCheck performs one check (checks.RunCheck, MP-07/MP-08 - no DB access
// during the network call itself) and, based on its result plus
// failureStreak, computes the new status: success -> "operational" and
// failureStreak reset to 0 (MP-06 first half); failure -> failureStreak+1,
// status unchanged (carried forward) until failureStreak reaches
// manualFailureThreshold, then "outage" (MP-06/MP-10/MP-11 - the failure's
// *reason* never resets or affects the streak, spec.md Edge Cases). The
// computed status is then persisted via a short-lived tenant-scoped
// transaction: OpenOrExtend (errorBudgetRemaining 0 - no SLO error-budget
// concept for a polling-manual service, design.md Tech Decisions) followed
// by UpdateStatus, the exact same two calls and the exact same
// status_intervals/services tables the Datadog poller's own pollService
// writes to (MP-12). Returns the status that should be carried into the
// next check (the newly computed one on a successful DB write, unchanged
// otherwise so a transient DB hiccup doesn't corrupt the in-memory carry-
// forward state).
func (s *ManualScheduler) runCheck(ctx context.Context, tenantID, serviceID, pollType, pollTarget, currentStatus string, failureStreak *int) string {
	checkErr := checks.RunCheck(ctx, pollType, pollTarget, manualCheckTimeout)

	var status string
	if checkErr == nil {
		*failureStreak = 0
		status = "operational"
	} else {
		*failureStreak++
		if *failureStreak >= manualFailureThreshold {
			status = "outage"
		} else {
			status = currentStatus
		}
	}

	tenantCtx, commit, rollback, err := s.tenantTx(ctx, tenantID)
	if err != nil {
		s.logger.Error("manual_scheduler: failed to begin tenant transaction for check result", zap.String("service_id", serviceID), zap.Error(err))
		return currentStatus
	}

	if err := s.statusIntervals.OpenOrExtend(tenantCtx, serviceID, status, 0, time.Now()); err != nil {
		s.logger.Error("manual_scheduler: failed to open or extend status interval", zap.String("service_id", serviceID), zap.Error(err))
		rollback(tenantCtx)
		return currentStatus
	}
	if err := s.statuses.UpdateStatus(tenantCtx, serviceID, status); err != nil {
		s.logger.Error("manual_scheduler: failed to update service status", zap.String("service_id", serviceID), zap.Error(err))
		rollback(tenantCtx)
		return currentStatus
	}
	if err := commit(tenantCtx); err != nil {
		s.logger.Error("manual_scheduler: failed to commit tenant transaction for check result", zap.String("service_id", serviceID), zap.Error(err))
		rollback(tenantCtx)
		return currentStatus
	}

	return status
}
