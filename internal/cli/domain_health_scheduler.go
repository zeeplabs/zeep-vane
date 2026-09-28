package cli

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/notify"
	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// domainHealthLeaderLockKey guards the daily domain-health fire so exactly
// one replica runs it (domain-health-monitoring). Distinct from
// db.PollerLeaderLockKey (727200001) and digestLeaderLockKey (727200002)
// within pglock's reserved 727200000-727299999 block.
const domainHealthLeaderLockKey int64 = 727200003

// domainHealthPageSize is the page size used when listing every domain of a
// tenant; the loop pages until all are read.
const domainHealthPageSize = 100

// Expiration alert thresholds in days (spec.md P1-AC2): 30 (notice), 15
// (notice), 7 (urgent).
const (
	expirationThreshold7  = 7
	expirationThreshold15 = 15
	expirationThreshold30 = 30
)

type domainHealthTenantLister interface {
	List(ctx context.Context) ([]db.Tenant, error)
}

type domainHealthRepository interface {
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Domain, int, error)
	SetHealthCheckResult(ctx context.Context, id string, expiresAt *time.Time, registrar *string, currentNS []string, driftDetected bool, rdapErr *string) error
}

// rdapClient is the subset of *rdap.Client the scheduler depends on.
type rdapClient interface {
	Lookup(ctx context.Context, hostname string) (*time.Time, *string, error)
}

type domainHealthNotifier interface {
	NotifyDomainExpiring(ctx context.Context, tenantID string, summary notify.DomainExpiringSummary) error
	NotifyDomainNSDrift(ctx context.Context, tenantID string, summary notify.DomainNSDriftSummary) error
}

// nsLookupFunc resolves a hostname's current nameserver set. Injected as a
// field so tests avoid real DNS, mirroring domain_verifier.go's checkDNS.
type nsLookupFunc func(ctx context.Context, hostname string) ([]string, error)

// DomainHealthScheduler runs the daily domain health check: for every domain
// of every tenant it fetches RDAP expiration/registrar data, resolves the
// current nameservers, detects expiration-threshold crossings and NS drift,
// dispatches alerts, and persists the outcome
// (domain-health-monitoring DHM-01..DHM-09). It is the domain counterpart of
// DigestScheduler: one dedicated advisory lock, one fire per day, one
// replica actually running.
type DomainHealthScheduler struct {
	dsn      string
	pool     *db.Pool
	tenants  domainHealthTenantLister
	domains  domainHealthRepository
	rdap     rdapClient
	notifier domainHealthNotifier
	logger   *zap.Logger

	lookupNS nsLookupFunc
	now      func() time.Time
}

// NewDomainHealthScheduler builds a DomainHealthScheduler.
func NewDomainHealthScheduler(dsn string, pool *db.Pool, tenants domainHealthTenantLister, domains domainHealthRepository, rdapClient rdapClient, notifier domainHealthNotifier, logger *zap.Logger) *DomainHealthScheduler {
	return &DomainHealthScheduler{
		dsn:      dsn,
		pool:     pool,
		tenants:  tenants,
		domains:  domains,
		rdap:     rdapClient,
		notifier: notifier,
		logger:   logger,
		lookupNS: defaultLookupNS,
		now:      time.Now,
	}
}

// WithNSLookup returns s with its NS resolver replaced - tests inject a
// deterministic lookup instead of hitting real DNS.
func (s *DomainHealthScheduler) WithNSLookup(fn nsLookupFunc) *DomainHealthScheduler {
	s.lookupNS = fn
	return s
}

// Run blocks until ctx is canceled, firing the health check at each 00:00
// UTC and every 24h after, using a cancelable timer (never a busy loop).
func (s *DomainHealthScheduler) Run(ctx context.Context) {
	for {
		now := s.now()
		next := nextDomainHealthFire(now)
		timer := time.NewTimer(next.Sub(now))

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.runCycle(ctx)
		}
	}
}

// nextDomainHealthFire returns the next 00:00 UTC strictly after now (a
// daily fire).
func nextDomainHealthFire(now time.Time) time.Time {
	utc := now.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if !midnight.After(utc) {
		midnight = midnight.AddDate(0, 0, 1)
	}
	return midnight
}

// runCycle takes the domain-health leader lock for one fire. If another
// replica holds it, this is a no-op.
func (s *DomainHealthScheduler) runCycle(ctx context.Context) {
	handle, acquired, err := pglock.TryAcquire(ctx, s.dsn, domainHealthLeaderLockKey)
	if err != nil {
		s.logger.Error("domain-health: failed to acquire leader lock", zap.Error(err))
		return
	}
	if !acquired {
		return
	}
	defer func() {
		if err := handle.Release(ctx); err != nil {
			s.logger.Error("domain-health: failed to release leader lock", zap.Error(err))
		}
	}()

	if err := s.runOnce(ctx, s.now()); err != nil {
		s.logger.Error("domain-health: run failed", zap.Error(err))
	}
}

func (s *DomainHealthScheduler) runOnce(ctx context.Context, now time.Time) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("domain-health: failed to list tenants: %w", err)
	}

	for _, tenant := range tenants {
		if err := s.runTenant(ctx, tenant, now); err != nil {
			// One tenant's failure never blocks the others.
			s.logger.Error("domain-health: tenant run failed", zap.String("tenant_id", tenant.ID), zap.Error(err))
		}
	}
	return nil
}

func (s *DomainHealthScheduler) runTenant(ctx context.Context, tenant db.Tenant, now time.Time) error {
	domains, err := s.listDomains(ctx, tenant.ID)
	if err != nil {
		return err
	}

	for i := range domains {
		if err := s.checkAndPersist(ctx, tenant, domains[i], now); err != nil {
			// DHM-03/DHM-07: one domain's failure never aborts the cycle.
			s.logger.Error("domain-health: domain check failed",
				zap.String("tenant_id", tenant.ID),
				zap.String("hostname", domains[i].Hostname),
				zap.Error(err))
		}
	}
	return nil
}

// listDomains reads every domain of tenantID (regardless of whether it has
// an attached status page - DHM-09), paging through ListPaginated in one
// tenant-scoped transaction, then commits the read before the network work
// begins.
func (s *DomainHealthScheduler) listDomains(ctx context.Context, tenantID string) ([]db.Domain, error) {
	tx, err := s.pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		return nil, fmt.Errorf("domain-health: failed to begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantCtx := db.WithTenantTx(ctx, tx)

	var all []db.Domain
	for page := 1; ; page++ {
		items, total, err := s.domains.ListPaginated(tenantCtx, page, domainHealthPageSize)
		if err != nil {
			return nil, fmt.Errorf("domain-health: failed to list domains: %w", err)
		}
		all = append(all, items...)
		if len(items) == 0 || len(all) >= total {
			break
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("domain-health: failed to commit tenant transaction: %w", err)
	}
	return all, nil
}

// checkAndPersist runs one domain's RDAP and NS checks, persists the result
// in its own short tenant transaction (never holding a connection across the
// network calls), then dispatches any alert after the commit.
func (s *DomainHealthScheduler) checkAndPersist(ctx context.Context, tenant db.Tenant, domain db.Domain, now time.Time) error {
	expiresAt, registrar, rdapErr := s.rdap.Lookup(ctx, domain.Hostname)
	var rdapErrMsg *string
	if rdapErr != nil {
		// Preserve the last known expiration/registrar so a transient RDAP
		// failure never erases them from the UI or re-arms an alert; only
		// the error is recorded and the next cycle retries (DHM-03).
		msg := rdapErr.Error()
		rdapErrMsg = &msg
		expiresAt, registrar = domain.ExpiresAt, domain.Registrar
	}

	currentNS, nsErr := s.lookupNS(ctx, domain.Hostname)
	if nsErr != nil {
		// DHM-07: a transient DNS failure must never overwrite or clear the
		// learned baseline - pass nil so the repository keeps both columns.
		s.logger.Warn("domain-health: NS lookup failed", zap.String("hostname", domain.Hostname), zap.Error(nsErr))
		currentNS = nil
	}

	drift := false
	var driftSummary *notify.DomainNSDriftSummary
	if nsErr == nil {
		drift = nsDrift(domain.ExpectedNS, currentNS)
		if drift {
			driftSummary = &notify.DomainNSDriftSummary{
				DomainID:   domain.ID,
				Hostname:   domain.Hostname,
				ExpectedNS: domain.ExpectedNS,
				CurrentNS:  currentNS,
				TenantName: tenant.Name,
			}
		}
	}

	var expiringSummary *notify.DomainExpiringSummary
	if rdapErrMsg == nil && expiresAt != nil {
		if threshold, days, crossed := expirationAlert(now, domain.ExpiresAt, domain.LastRDAPCheckAt, *expiresAt); crossed {
			expiringSummary = &notify.DomainExpiringSummary{
				DomainID:      domain.ID,
				Hostname:      domain.Hostname,
				DaysRemaining: days,
				ThresholdDays: threshold,
				TenantName:    tenant.Name,
			}
		}
	}

	if err := s.persist(ctx, tenant.ID, domain.ID, expiresAt, registrar, currentNS, drift, rdapErrMsg); err != nil {
		return err
	}

	if expiringSummary != nil {
		if err := s.notifier.NotifyDomainExpiring(ctx, tenant.ID, *expiringSummary); err != nil {
			s.logger.Error("domain-health: failed to send expiration notification",
				zap.String("hostname", domain.Hostname), zap.Error(err))
		}
	}
	if driftSummary != nil {
		if err := s.notifier.NotifyDomainNSDrift(ctx, tenant.ID, *driftSummary); err != nil {
			s.logger.Error("domain-health: failed to send NS drift notification",
				zap.String("hostname", domain.Hostname), zap.Error(err))
		}
	}
	return nil
}

// persist writes one domain's health-check result under its tenant's own
// transaction, never a bare pool.Begin that would discard the RLS session
// settings.
func (s *DomainHealthScheduler) persist(ctx context.Context, tenantID, domainID string, expiresAt *time.Time, registrar *string, currentNS []string, drift bool, rdapErr *string) error {
	tx, err := s.pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		return fmt.Errorf("domain-health: failed to begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.domains.SetHealthCheckResult(db.WithTenantTx(ctx, tx), domainID, expiresAt, registrar, currentNS, drift, rdapErr); err != nil {
		return fmt.Errorf("domain-health: failed to persist health check result: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("domain-health: failed to commit tenant transaction: %w", err)
	}
	return nil
}

// expirationAlert reports whether a new expiration threshold crossing
// happened since the previous check, plus the crossed threshold and days
// remaining (DHM-02). The previous threshold is derived from the domain's
// previously stored ExpiresAt at the time of its previous check
// (LastRDAPCheckAt): a crossing is new only when the current band is
// narrower than the previous one, so a domain sitting inside a band for
// several daily checks alerts once, not every day. A domain whose first
// successful check already sits inside a band (previous state empty, band 0)
// alerts on that first check.
func expirationAlert(now time.Time, prevExpiresAt, prevCheckedAt *time.Time, expiresAt time.Time) (threshold, daysRemaining int, crossed bool) {
	daysRemaining = daysUntil(now, expiresAt)
	threshold = expirationThreshold(daysRemaining)
	if threshold == 0 {
		return 0, daysRemaining, false
	}

	previous := 0
	if prevExpiresAt != nil && prevCheckedAt != nil {
		previous = expirationThreshold(daysUntil(*prevCheckedAt, *prevExpiresAt))
	}
	if threshold == previous {
		return threshold, daysRemaining, false
	}
	return threshold, daysRemaining, true
}

// expirationThreshold returns the smallest configured threshold
// (7/15/30) daysRemaining falls within, or 0 when farther than 30 days.
func expirationThreshold(daysRemaining int) int {
	switch {
	case daysRemaining <= expirationThreshold7:
		return expirationThreshold7
	case daysRemaining <= expirationThreshold15:
		return expirationThreshold15
	case daysRemaining <= expirationThreshold30:
		return expirationThreshold30
	default:
		return 0
	}
}

// daysUntil is the whole-day distance from from to to (negative once past).
func daysUntil(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

// nsDrift reports whether current differs from the learned expected
// baseline. An empty baseline means none has been learned yet, which is not
// drift (DHM-05).
func nsDrift(expected, current []string) bool {
	if len(expected) == 0 {
		return false
	}
	return !nsSetsEqual(expected, current)
}

// nsSetsEqual compares two nameserver sets order- and case-insensitively.
func nsSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// defaultLookupNS resolves hostname's nameservers through the system
// resolver, normalizing to lowercase without a trailing dot and sorted, so a
// baseline comparison is stable across lookups.
func defaultLookupNS(ctx context.Context, hostname string) ([]string, error) {
	records, err := net.DefaultResolver.LookupNS(ctx, hostname)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(records))
	for _, record := range records {
		names = append(names, strings.ToLower(strings.TrimSuffix(record.Host, ".")))
	}
	sort.Strings(names)
	return names, nil
}
