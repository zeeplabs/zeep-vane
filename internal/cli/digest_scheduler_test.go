//go:build integration

package cli

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

type staticTenantLister struct {
	tenants []db.Tenant
	calls   int
}

func (l *staticTenantLister) List(context.Context) ([]db.Tenant, error) {
	l.calls++
	return l.tenants, nil
}

// blockingTenantLister signals entered then blocks until release is closed, so
// a leader-election test can hold the digest cycle open while a second
// scheduler tries (and fails) to acquire the same lock.
type blockingTenantLister struct {
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (l *blockingTenantLister) List(context.Context) ([]db.Tenant, error) {
	l.calls++
	l.entered <- struct{}{}
	<-l.release
	return nil, nil
}

type countingServiceLister struct {
	calls int
}

func (l *countingServiceLister) List(context.Context) ([]db.Service, error) {
	l.calls++
	return nil, nil
}

type countingIntervalLister struct {
	calls int
}

func (l *countingIntervalLister) ListOverlapping(context.Context, []string, time.Time, time.Time) ([]db.StatusInterval, error) {
	l.calls++
	return nil, nil
}

type countingIncidentCounter struct {
	opened, resolved int
	calls            int
	listCalls        int
}

func (c *countingIncidentCounter) CountOpenedResolvedBetween(context.Context, time.Time, time.Time) (int, int, error) {
	c.calls++
	return c.opened, c.resolved, nil
}

func (c *countingIncidentCounter) ListActiveBetween(context.Context, time.Time, time.Time) ([]db.DigestIncident, error) {
	c.listCalls++
	return nil, nil
}

type fakeDigestNotifier struct {
	recipients []db.TenantMember
	sentTo     []string
	lastData   email.WeeklyDigestEmailData
}

func (n *fakeDigestNotifier) WeeklyDigestRecipients(context.Context, string) ([]db.TenantMember, error) {
	return n.recipients, nil
}

func (n *fakeDigestNotifier) SendWeeklyDigest(_ context.Context, to string, data email.WeeklyDigestEmailData) error {
	n.sentTo = append(n.sentTo, to)
	n.lastData = data
	return nil
}

func newDigestSchedulerForTest(t *testing.T, pool *db.Pool, tenants digestTenantLister, services digestServiceLister, intervals digestIntervalLister, incidents digestIncidentCounter, notifier digestNotifier) *DigestScheduler {
	t.Helper()
	return NewDigestScheduler(testDatabaseURL(t), pool, tenants, services, intervals, incidents, notifier, db.NewTenantRepository(pool), zap.NewNop())
}

// rereadTenantLister returns just the fixture tenant, re-read from the
// database on every call - so a second runOnce sees the last_digest_sent_at
// the first one persisted, like SystemTenantLister would in production.
type rereadTenantLister struct {
	pool     *db.Pool
	tenantID string
}

func (l *rereadTenantLister) List(ctx context.Context) ([]db.Tenant, error) {
	tenant, err := db.NewTenantRepository(l.pool).Get(ctx, l.tenantID)
	if err != nil {
		return nil, err
	}
	return []db.Tenant{*tenant}, nil
}

func lastDigestSentAt(t *testing.T, pool *db.Pool, tenantID string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT last_digest_sent_at FROM tenants WHERE id = $1", tenantID).Scan(&at); err != nil {
		t.Fatalf("reading last_digest_sent_at returned unexpected error: %v", err)
	}
	return at
}

// digestTestNow is a Monday 09:01 in America/Sao_Paulo (12:01 UTC) - inside
// the fixture tenant's digest window (nil timezone resolves to the default
// Sao Paulo zone, created_at is the zero value in static fixtures).
var digestTestNow = time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC)

// TestDigestScheduler_LeaderElection_ExactlyOneReplicaRuns proves NOTIFPREF-11:
// with two schedulers firing the same cycle against the same database, the
// advisory lock lets exactly one run.
func TestDigestScheduler_LeaderElection_ExactlyOneReplicaRuns(t *testing.T) {
	pool := newServeTestPool(t)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	leaderTenants := &blockingTenantLister{entered: entered, release: release}
	followerTenants := &staticTenantLister{}

	leader := newDigestSchedulerForTest(t, pool, leaderTenants, &countingServiceLister{}, &countingIntervalLister{}, &countingIncidentCounter{}, &fakeDigestNotifier{})
	follower := newDigestSchedulerForTest(t, pool, followerTenants, &countingServiceLister{}, &countingIntervalLister{}, &countingIncidentCounter{}, &fakeDigestNotifier{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		leader.runCycle(context.Background())
	}()

	// Wait until the leader holds the lock and is inside its cycle.
	<-entered

	// The follower fires while the leader is still running - it must skip.
	follower.runCycle(context.Background())
	if followerTenants.calls != 0 {
		t.Errorf("follower ran its cycle (%d tenant-list calls), want 0 - the leader held the lock", followerTenants.calls)
	}

	close(release)
	wg.Wait()

	if leaderTenants.calls != 1 {
		t.Errorf("leader tenant-list calls = %d, want exactly 1", leaderTenants.calls)
	}
}

// TestDigestScheduler_ZeroRecipients_NoContentNoSend proves NOTIFPREF-12: a
// tenant with nobody opted into the digest builds no content and sends
// nothing.
func TestDigestScheduler_ZeroRecipients_NoContentNoSend(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)

	services := &countingServiceLister{}
	intervals := &countingIntervalLister{}
	incidents := &countingIncidentCounter{}
	notifier := &fakeDigestNotifier{recipients: nil}

	s := newDigestSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		services, intervals, incidents, notifier)

	if err := s.runOnce(context.Background(), digestTestNow); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}
	if services.calls != 0 || intervals.calls != 0 || incidents.calls != 0 {
		t.Errorf("content builder ran (%d/%d/%d), want no content built when nobody opted in", services.calls, intervals.calls, incidents.calls)
	}
	if len(notifier.sentTo) != 0 {
		t.Errorf("sentTo = %v, want no sends", notifier.sentTo)
	}
	// Nobody opted in still settles the week, so later ticks don't re-check.
	if at := lastDigestSentAt(t, pool, tenantID); at == nil || !at.Equal(digestTestNow) {
		t.Errorf("last_digest_sent_at = %v, want %s", at, digestTestNow)
	}
}

// TestDigestScheduler_WithRecipients_SendsAssembledDigest proves NOTIFPREF-10:
// each recipient gets one digest carrying the tenant's incident counts.
func TestDigestScheduler_WithRecipients_SendsAssembledDigest(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)

	notifier := &fakeDigestNotifier{recipients: []db.TenantMember{
		{UserID: "u1", Email: "owner@example.com", Role: "owner"},
		{UserID: "u2", Email: "ops@example.com", Role: "operator"},
	}}
	incidents := &countingIncidentCounter{opened: 4, resolved: 3}

	s := newDigestSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		&countingServiceLister{}, &countingIntervalLister{}, incidents, notifier)

	if err := s.runOnce(context.Background(), digestTestNow); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	if len(notifier.sentTo) != 2 {
		t.Fatalf("sentTo = %v, want one send per recipient (2)", notifier.sentTo)
	}
	if notifier.lastData.IncidentsOpened != 4 || notifier.lastData.IncidentsResolved != 3 {
		t.Errorf("digest data counts = %d/%d, want 4/3", notifier.lastData.IncidentsOpened, notifier.lastData.IncidentsResolved)
	}
	if notifier.lastData.TenantName != "cli-test-tenant" {
		t.Errorf("digest TenantName = %q, want %q", notifier.lastData.TenantName, "cli-test-tenant")
	}
	// The previous Sao Paulo calendar week, Mon Aug 31 00:00 - Mon Sep 7
	// 00:00 local (03:00 UTC), not "7 days back from the tick".
	wantStart := time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC)
	if !notifier.lastData.PeriodEnd.Equal(wantEnd) || !notifier.lastData.PeriodStart.Equal(wantStart) {
		t.Errorf("digest period = %s..%s, want %s..%s", notifier.lastData.PeriodStart, notifier.lastData.PeriodEnd, wantStart, wantEnd)
	}
	if got := notifier.lastData.PeriodLabel(); got != "Aug 31 – Sep 6, 2026" {
		t.Errorf("digest PeriodLabel() = %q, want %q", got, "Aug 31 – Sep 6, 2026")
	}
	if incidents.listCalls != 1 {
		t.Errorf("ListActiveBetween calls = %d, want 1 (the incidents list is built once per tenant, not per recipient)", incidents.listCalls)
	}
}

// TestDigestScheduler_SystemTenantLister_ReturnsRealTenants proves the digest's
// tenant enumeration works under RLS through db.SystemTenantLister (AD-024) -
// not silently zero rows.
func TestDigestScheduler_SystemTenantLister_ReturnsRealTenants(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)

	tenants, err := db.NewSystemTenantLister(pool).List(context.Background())
	if err != nil {
		t.Fatalf("SystemTenantLister.List() returned unexpected error: %v", err)
	}
	found := false
	for _, tenant := range tenants {
		if tenant.ID == tenantID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tenant %q not present in SystemTenantLister.List() result (%d tenants) - the digest would enumerate nothing", tenantID, len(tenants))
	}
}

// TestDigestScheduler_SecondTickSameWeek_NoDoubleSend proves the persisted
// marker: the first due tick sends and records last_digest_sent_at, a later
// tick the same week (re-reading the tenant from the database, as
// SystemTenantLister does) sends nothing, and the next week's tick sends
// again.
func TestDigestScheduler_SecondTickSameWeek_NoDoubleSend(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	if _, err := pool.Exec(context.Background(), "UPDATE tenants SET timezone = $2, created_at = $3 WHERE id = $1",
		tenantID, "America/Sao_Paulo", digestTestNow.AddDate(0, -1, 0)); err != nil {
		t.Fatalf("setup tenant timezone returned unexpected error: %v", err)
	}

	notifier := &fakeDigestNotifier{recipients: []db.TenantMember{{UserID: "u1", Email: "owner@example.com", Role: "owner"}}}
	s := newDigestSchedulerForTest(t, pool, &rereadTenantLister{pool: pool, tenantID: tenantID},
		&countingServiceLister{}, &countingIntervalLister{}, &countingIncidentCounter{}, notifier)

	ctx := context.Background()
	if err := s.runOnce(ctx, digestTestNow); err != nil {
		t.Fatalf("first runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.sentTo) != 1 {
		t.Fatalf("after first tick sentTo = %v, want exactly 1 send", notifier.sentTo)
	}
	if at := lastDigestSentAt(t, pool, tenantID); at == nil || !at.Equal(digestTestNow) {
		t.Errorf("last_digest_sent_at = %v, want %s", at, digestTestNow)
	}

	if err := s.runOnce(ctx, digestTestNow.Add(time.Hour)); err != nil {
		t.Fatalf("second runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.sentTo) != 1 {
		t.Errorf("after second tick the same week sentTo = %v, want still 1 (no double send)", notifier.sentTo)
	}

	if err := s.runOnce(ctx, digestTestNow.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("next-week runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.sentTo) != 2 {
		t.Errorf("after next week's tick sentTo = %v, want 2", notifier.sentTo)
	}
}

// TestDigestScheduler_NotDue_NoWorkNoMarker: a tick outside the tenant's
// Monday window builds and sends nothing and leaves the marker untouched.
func TestDigestScheduler_NotDue_NoWorkNoMarker(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)

	notifier := &fakeDigestNotifier{recipients: []db.TenantMember{{UserID: "u1", Email: "owner@example.com", Role: "owner"}}}
	incidents := &countingIncidentCounter{}
	s := newDigestSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		&countingServiceLister{}, &countingIntervalLister{}, incidents, notifier)

	wednesday := time.Date(2026, 9, 9, 12, 1, 0, 0, time.UTC)
	if err := s.runOnce(context.Background(), wednesday); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.sentTo) != 0 || incidents.calls != 0 {
		t.Errorf("sentTo = %v, incident counts = %d, want no work outside the window", notifier.sentTo, incidents.calls)
	}
	if at := lastDigestSentAt(t, pool, tenantID); at != nil {
		t.Errorf("last_digest_sent_at = %s, want NULL", at)
	}
}

// TestNewDigestScheduler_BootWiring_RunStopsOnContextCancel covers NOTIFPREF-10
// wiring: the scheduler the boot path constructs starts and stops cleanly when
// the serve context is canceled.
func TestNewDigestScheduler_BootWiring_RunStopsOnContextCancel(t *testing.T) {
	pool := newServeTestPool(t)

	s, err := newDigestScheduler(pool, config.Config{DatabaseURL: testDatabaseURL(t), MasterKey: "cli-digest-test-master-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("newDigestScheduler() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DigestScheduler.Run did not stop on context cancellation")
	}
}
