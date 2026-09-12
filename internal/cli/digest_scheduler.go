package cli

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
	"github.com/zeeplabs/zeep-vane/internal/history"
	"github.com/zeeplabs/zeep-vane/internal/notify"
	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// digestLeaderLockKey guards the weekly digest fire so exactly one replica
// sends it (notification-preferences NOTIFPREF-11). Distinct from
// db.PollerLeaderLockKey (727200001) within pglock's reserved
// 727200000-727299999 block.
const digestLeaderLockKey int64 = 727200002

// digestWindow is how far back the weekly digest summarizes.
const digestWindow = 7 * 24 * time.Hour

type digestTenantLister interface {
	List(ctx context.Context) ([]db.Tenant, error)
}

type digestServiceLister interface {
	List(ctx context.Context) ([]db.Service, error)
}

type digestIntervalLister interface {
	ListOverlapping(ctx context.Context, serviceIDs []string, windowStart, now time.Time) ([]db.StatusInterval, error)
}

type digestIncidentCounter interface {
	CountOpenedResolvedBetween(ctx context.Context, from, to time.Time) (opened, resolved int, err error)
}

type digestNotifier interface {
	WeeklyDigestRecipients(ctx context.Context, tenantID string) ([]db.TenantMember, error)
	SendWeeklyDigest(ctx context.Context, to string, data email.WeeklyDigestEmailData) error
}

// DigestScheduler fires the weekly digest once per tenant per week, from a
// single replica. It is the digest counterpart of PollerManager's leader loop:
// on each fire it takes a dedicated advisory lock, and whichever replica wins
// runs the cycle while the others skip it.
type DigestScheduler struct {
	dsn       string
	pool      *db.Pool
	tenants   digestTenantLister
	services  digestServiceLister
	intervals digestIntervalLister
	incidents digestIncidentCounter
	notifier  digestNotifier
	logger    *zap.Logger

	now func() time.Time
}

// NewDigestScheduler builds a DigestScheduler.
func NewDigestScheduler(dsn string, pool *db.Pool, tenants digestTenantLister, services digestServiceLister, intervals digestIntervalLister, incidents digestIncidentCounter, notifier digestNotifier, logger *zap.Logger) *DigestScheduler {
	return &DigestScheduler{
		dsn:       dsn,
		pool:      pool,
		tenants:   tenants,
		services:  services,
		intervals: intervals,
		incidents: incidents,
		notifier:  notifier,
		logger:    logger,
		now:       time.Now,
	}
}

// Run blocks until ctx is canceled, firing the digest job at each Monday 00:00
// UTC and every 7 days after. The wait uses a cancelable timer, never a busy
// loop.
func (s *DigestScheduler) Run(ctx context.Context) {
	for {
		now := s.now()
		next := nextDigestFire(now)
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

// nextDigestFire returns the next Monday 00:00 UTC strictly after now.
func nextDigestFire(now time.Time) time.Time {
	utc := now.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	daysUntilMonday := (int(time.Monday) - int(utc.Weekday()) + 7) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	return midnight.AddDate(0, 0, daysUntilMonday)
}

// runCycle takes the digest leader lock for one fire. If another replica holds
// it (expected steady state in a multi-replica deployment), it is a no-op.
func (s *DigestScheduler) runCycle(ctx context.Context) {
	handle, acquired, err := pglock.TryAcquire(ctx, s.dsn, digestLeaderLockKey)
	if err != nil {
		s.logger.Error("digest: failed to acquire leader lock", zap.Error(err))
		return
	}
	if !acquired {
		return
	}
	defer func() {
		if err := handle.Release(ctx); err != nil {
			s.logger.Error("digest: failed to release leader lock", zap.Error(err))
		}
	}()

	if err := s.runOnce(ctx, s.now()); err != nil {
		s.logger.Error("digest: run failed", zap.Error(err))
	}
}

func (s *DigestScheduler) runOnce(ctx context.Context, now time.Time) error {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("digest: failed to list tenants: %w", err)
	}

	from := now.Add(-digestWindow)
	for _, tenant := range tenants {
		if err := s.runTenant(ctx, tenant, from, now); err != nil {
			// One tenant's failure never blocks the others.
			s.logger.Error("digest: tenant run failed", zap.String("tenant_id", tenant.ID), zap.Error(err))
		}
	}
	return nil
}

func (s *DigestScheduler) runTenant(ctx context.Context, tenant db.Tenant, from, to time.Time) error {
	tx, err := s.pool.BeginTenantTx(ctx, "", tenant.ID)
	if err != nil {
		return fmt.Errorf("digest: failed to begin tenant transaction: %w", err)
	}
	tenantCtx := db.WithTenantTx(ctx, tx)

	recipients, err := s.notifier.WeeklyDigestRecipients(tenantCtx, tenant.ID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if len(recipients) == 0 {
		// AC3: nobody opted in - send nothing and build no content.
		return tx.Commit(ctx)
	}

	data, err := s.buildDigest(tenantCtx, tenant, from, to)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	// Commit the read transaction before the network sends, so a slow email
	// provider never holds the tenant connection open.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("digest: failed to commit tenant transaction: %w", err)
	}

	for _, recipient := range recipients {
		if err := s.notifier.SendWeeklyDigest(ctx, recipient.Email, data); err != nil {
			s.logger.Error("digest: failed to send digest",
				zap.String("tenant_id", tenant.ID),
				zap.String("user_id", recipient.UserID),
				zap.Error(err))
		}
	}
	return nil
}

func (s *DigestScheduler) buildDigest(ctx context.Context, tenant db.Tenant, from, to time.Time) (email.WeeklyDigestEmailData, error) {
	services, err := s.services.List(ctx)
	if err != nil {
		return email.WeeklyDigestEmailData{}, fmt.Errorf("digest: failed to list services: %w", err)
	}

	serviceIDs := make([]string, 0, len(services))
	for _, svc := range services {
		serviceIDs = append(serviceIDs, svc.ID)
	}

	var uptimes []float64
	if len(serviceIDs) > 0 {
		intervals, err := s.intervals.ListOverlapping(ctx, serviceIDs, from, to)
		if err != nil {
			return email.WeeklyDigestEmailData{}, fmt.Errorf("digest: failed to list status intervals: %w", err)
		}
		byService := make(map[string][]db.StatusInterval)
		for _, interval := range intervals {
			byService[interval.ServiceID] = append(byService[interval.ServiceID], interval)
		}
		for _, svc := range services {
			if percent, ok := history.UptimePercent(byService[svc.ID], from, to); ok {
				uptimes = append(uptimes, percent)
			}
		}
	}

	opened, resolved, err := s.incidents.CountOpenedResolvedBetween(ctx, from, to)
	if err != nil {
		return email.WeeklyDigestEmailData{}, fmt.Errorf("digest: failed to count incidents: %w", err)
	}

	return notify.BuildWeeklyDigestData(tenant.Name, uptimes, opened, resolved, from, to), nil
}
