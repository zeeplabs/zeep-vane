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
// sends it (notification-preferences NOTIFPREF-11). Value from pglock's
// reserved-key registry.
const digestLeaderLockKey int64 = pglock.DigestLeaderLockKey

// Weekly digest timing. Each tenant's digest fires at digestFireHour on
// Monday in the tenant's own timezone (email.TenantLocation), not at one
// global instant. Run ticks hourly (digestTickOffset past each UTC hour) and
// sends every tenant whose fire instant has passed and who hasn't been sent
// this week yet - see digestDue.
const (
	digestFireHour     = 9
	digestTickInterval = time.Hour
	digestTickOffset   = time.Minute
	// digestCatchUpWindow bounds how late a missed fire (process down,
	// leader lock contention, a failed tenant pass) is still sent: a tick
	// within 24h of the tenant's Monday 09:00 sends it; after that the week
	// is skipped rather than mailing a "weekly" digest days late.
	digestCatchUpWindow = 24 * time.Hour
)

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
	ListActiveBetween(ctx context.Context, from, to time.Time) ([]db.DigestIncident, error)
}

type digestNotifier interface {
	WeeklyDigestRecipients(ctx context.Context, tenantID string) ([]db.TenantMember, error)
	SendWeeklyDigest(ctx context.Context, to string, data email.WeeklyDigestEmailData) error
}

// digestMarker persists the per-tenant "digest sent this week" marker
// (tenants.last_digest_sent_at), written inside the tenant transaction.
type digestMarker interface {
	MarkDigestSent(ctx context.Context, tenantID string, at time.Time) error
}

// DigestScheduler sends the weekly digest once per tenant per week, at ~09:00
// Monday tenant-local time, from a single replica. It is the digest
// counterpart of PollerManager's leader loop: on each hourly tick it takes a
// dedicated advisory lock, and whichever replica wins runs the cycle while
// the others skip it. Which tenants a cycle actually sends is decided per
// tenant by digestDue against the persisted last_digest_sent_at marker.
type DigestScheduler struct {
	dsn       string
	pool      *db.Pool
	tenants   digestTenantLister
	services  digestServiceLister
	intervals digestIntervalLister
	incidents digestIncidentCounter
	notifier  digestNotifier
	marker    digestMarker
	logger    *zap.Logger

	now func() time.Time
}

// NewDigestScheduler builds a DigestScheduler.
func NewDigestScheduler(dsn string, pool *db.Pool, tenants digestTenantLister, services digestServiceLister, intervals digestIntervalLister, incidents digestIncidentCounter, notifier digestNotifier, marker digestMarker, logger *zap.Logger) *DigestScheduler {
	return &DigestScheduler{
		dsn:       dsn,
		pool:      pool,
		tenants:   tenants,
		services:  services,
		intervals: intervals,
		incidents: incidents,
		notifier:  notifier,
		marker:    marker,
		logger:    logger,
		now:       time.Now,
	}
}

// Run blocks until ctx is canceled, running a digest cycle at every tick
// (digestTickOffset past each UTC hour - see nextDigestTick). The wait uses
// a cancelable timer, never a busy loop.
func (s *DigestScheduler) Run(ctx context.Context) {
	for {
		now := s.now()
		timer := time.NewTimer(nextDigestTick(now).Sub(now))

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.runCycle(ctx)
		}
	}
}

// nextDigestTick returns the next tick strictly after now: digestTickOffset
// past the next hour boundary. Hour boundaries are absolute (UTC-based), so
// every whole-hour timezone sees its Monday 09:00 fire on the 09:01 tick;
// half/quarter-hour zones are picked up within the hour.
func nextDigestTick(now time.Time) time.Time {
	next := now.Truncate(digestTickInterval).Add(digestTickOffset)
	if !next.After(now) {
		next = next.Add(digestTickInterval)
	}
	return next
}

// weeklyDigestFire returns this week's digest fire instant for loc: the most
// recent Monday digestFireHour:00 in loc at or before now. Computed with
// time.Date in loc, so it stays at local 09:00 across DST changes.
func weeklyDigestFire(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	daysSinceMonday := (int(local.Weekday()) - int(time.Monday) + 7) % 7
	fire := time.Date(local.Year(), local.Month(), local.Day()-daysSinceMonday, digestFireHour, 0, 0, 0, loc)
	if fire.After(now) {
		fire = time.Date(local.Year(), local.Month(), local.Day()-daysSinceMonday-7, digestFireHour, 0, 0, 0, loc)
	}
	return fire
}

// digestDue reports whether tenant's digest should be sent at now, and the
// fire instant it would be sent for. It is due when:
//   - now is within [fire, fire+digestCatchUpWindow) - so a missed tick
//     (restart, lock contention, a failed pass) is retried on the next tick
//     instead of skipping the week, but never mailed days late; and
//   - the tenant's marker is before fire - last_digest_sent_at, or its
//     created_at when it was never sent, so a tenant created after this
//     week's fire waits for next week instead of getting an immediate digest.
//
// Because the marker is written in the same tenant transaction that commits
// the pass (and before any email goes out), a second tick in the same week -
// or a second replica after the leader lock is released - sees marker >= fire
// and skips: never double-sent in the same week.
func digestDue(tenant db.Tenant, now time.Time) (fire time.Time, due bool) {
	fire = weeklyDigestFire(now, email.TenantLocation(tenant.Timezone))
	if now.Sub(fire) >= digestCatchUpWindow {
		return fire, false
	}
	marker := tenant.CreatedAt
	if tenant.LastDigestSentAt != nil {
		marker = *tenant.LastDigestSentAt
	}
	return fire, marker.Before(fire)
}

// digestPeriod returns the window a digest fired at fire summarizes: the
// previous calendar week in the tenant's zone, Monday 00:00 to Monday 00:00
// local (DST-safe via AddDate). Anchoring to the tenant-local week rather
// than "7 days back from the tick" keeps every digest covering exactly the
// Mon-Sun week its PeriodLabel names, with no gap or overlap between
// consecutive weeks, independent of when within the catch-up window the tick
// actually ran.
func digestPeriod(fire time.Time, loc *time.Location) (from, to time.Time) {
	local := fire.In(loc)
	to = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return to.AddDate(0, 0, -7), to
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

	for _, tenant := range tenants {
		fire, due := digestDue(tenant, now)
		if !due {
			continue
		}
		from, to := digestPeriod(fire, email.TenantLocation(tenant.Timezone))
		if err := s.runTenant(ctx, tenant, from, to, now); err != nil {
			// One tenant's failure never blocks the others; with no marker
			// written, the next tick inside the catch-up window retries it.
			s.logger.Error("digest: tenant run failed", zap.String("tenant_id", tenant.ID), zap.Error(err))
		}
	}
	return nil
}

// runTenant runs one tenant's digest pass for the [from, to) window and
// records sentAt as its last_digest_sent_at.
//
// The marker is written in the same tenant transaction as the reads, just
// before it commits and before any email goes out - i.e. once the pass has
// reached the send attempts (or found nobody opted in, which also settles
// the week). Consequences, deliberately:
//   - a failure before that point (recipient lookup, buildDigest, the
//     commit) rolls back with no marker, so the next hourly tick retries -
//     at most once an hour for the catch-up window, not a retry storm;
//   - individual recipient send failures after the commit are logged, not
//     retried: the week is marked, so a flaky provider never re-mails the
//     recipients that did get it. At-most-once per recipient per week.
func (s *DigestScheduler) runTenant(ctx context.Context, tenant db.Tenant, from, to, sentAt time.Time) error {
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
		// AC3: nobody opted in - send nothing and build no content. The week
		// is still settled, so later ticks don't re-check it every hour.
		if err := s.marker.MarkDigestSent(tenantCtx, tenant.ID, sentAt); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("digest: failed to mark digest sent: %w", err)
		}
		return tx.Commit(ctx)
	}

	data, err := s.buildDigest(tenantCtx, tenant, from, to)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := s.marker.MarkDigestSent(tenantCtx, tenant.ID, sentAt); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("digest: failed to mark digest sent: %w", err)
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

	var uptimes []notify.DigestServiceUptime
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
				uptimes = append(uptimes, notify.DigestServiceUptime{Name: svc.Name, UptimePercent: percent})
			}
		}
	}

	opened, resolved, err := s.incidents.CountOpenedResolvedBetween(ctx, from, to)
	if err != nil {
		return email.WeeklyDigestEmailData{}, fmt.Errorf("digest: failed to count incidents: %w", err)
	}

	incidents, err := s.incidents.ListActiveBetween(ctx, from, to)
	if err != nil {
		return email.WeeklyDigestEmailData{}, fmt.Errorf("digest: failed to list incidents: %w", err)
	}

	return notify.BuildWeeklyDigestData(notify.WeeklyDigestInput{
		Tenant:            tenant,
		Services:          uptimes,
		Incidents:         incidents,
		IncidentsOpened:   opened,
		IncidentsResolved: resolved,
		PeriodStart:       from,
		PeriodEnd:         to,
	}), nil
}
