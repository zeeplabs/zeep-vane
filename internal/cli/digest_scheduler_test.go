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
}

func (c *countingIncidentCounter) CountOpenedResolvedBetween(context.Context, time.Time, time.Time) (int, int, error) {
	c.calls++
	return c.opened, c.resolved, nil
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
	return NewDigestScheduler(testDatabaseURL(t), pool, tenants, services, intervals, incidents, notifier, zap.NewNop())
}

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

	if err := s.runOnce(context.Background(), time.Now()); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}
	if services.calls != 0 || intervals.calls != 0 || incidents.calls != 0 {
		t.Errorf("content builder ran (%d/%d/%d), want no content built when nobody opted in", services.calls, intervals.calls, incidents.calls)
	}
	if len(notifier.sentTo) != 0 {
		t.Errorf("sentTo = %v, want no sends", notifier.sentTo)
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

	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	if err := s.runOnce(context.Background(), now); err != nil {
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
	if notifier.lastData.PeriodEnd != "2026-09-08" || notifier.lastData.PeriodStart != "2026-09-01" {
		t.Errorf("digest period = %q..%q, want 2026-09-01..2026-09-08", notifier.lastData.PeriodStart, notifier.lastData.PeriodEnd)
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

// TestNextDigestFire covers the Monday 00:00 UTC computation.
func TestNextDigestFire(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"sunday", time.Date(2026, 9, 6, 23, 0, 0, 0, time.UTC), time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)},
		{"monday after midnight", time.Date(2026, 9, 7, 0, 30, 0, 0, time.UTC), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
		{"tuesday", time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDigestFire(tc.now); !got.Equal(tc.want) {
				t.Errorf("nextDigestFire(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
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
