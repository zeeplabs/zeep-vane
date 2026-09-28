//go:build integration

package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/notify"
)

type fakeRDAPClient struct {
	expiresAt map[string]*time.Time
	registrar map[string]*string
	errs      map[string]error
}

func (f *fakeRDAPClient) Lookup(_ context.Context, hostname string) (*time.Time, *string, error) {
	if err := f.errs[hostname]; err != nil {
		return nil, nil, err
	}
	return f.expiresAt[hostname], f.registrar[hostname], nil
}

type fakeDomainHealthNotifier struct {
	expiring []notify.DomainExpiringSummary
	drift    []notify.DomainNSDriftSummary
}

func (n *fakeDomainHealthNotifier) NotifyDomainExpiring(_ context.Context, _ string, summary notify.DomainExpiringSummary) error {
	n.expiring = append(n.expiring, summary)
	return nil
}

func (n *fakeDomainHealthNotifier) NotifyDomainNSDrift(_ context.Context, _ string, summary notify.DomainNSDriftSummary) error {
	n.drift = append(n.drift, summary)
	return nil
}

func newDomainHealthSchedulerForTest(t *testing.T, pool *db.Pool, tenants domainHealthTenantLister, repo *db.DomainRepository, rdap rdapClient, notifier domainHealthNotifier) *DomainHealthScheduler {
	t.Helper()
	return NewDomainHealthScheduler(testDatabaseURL(t), pool, tenants, repo, rdap, notifier, zap.NewNop())
}

// seedHealthDomain creates one domain owned by the pool's fixture tenant and
// overwrites its health columns directly, so a test can control the
// "previous check" state (expires_at/last_rdap_check_at/baseline) that the
// scheduler derives its crossing decision from. lastCheckAt seeds both
// last_rdap_check_at and last_rdap_success_at (the common case: the
// previous cycle was a success) - use seedHealthDomainWithSuccess directly
// when a test needs them to diverge (an intervening RDAP failure).
func seedHealthDomain(t *testing.T, pool *db.Pool, repo *db.DomainRepository, hostname string, expiresAt *time.Time, ns []string, lastCheckAt *time.Time) *db.Domain {
	t.Helper()
	return seedHealthDomainWithSuccess(t, pool, repo, hostname, expiresAt, ns, lastCheckAt, lastCheckAt)
}

// seedHealthDomainWithSuccess is seedHealthDomain with last_rdap_check_at
// (every attempt) and last_rdap_success_at (only successful attempts) set
// independently.
func seedHealthDomainWithSuccess(t *testing.T, pool *db.Pool, repo *db.DomainRepository, hostname string, expiresAt *time.Time, ns []string, lastCheckAt, lastSuccessAt *time.Time) *db.Domain {
	t.Helper()
	ctx := context.Background()

	domain := &db.Domain{Hostname: hostname}
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("setup Create(%q) returned unexpected error: %v", hostname, err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	if _, err := pool.Exec(ctx,
		`UPDATE domains SET expires_at = $2, expected_ns = $3, current_ns = $3, ns_drift_detected = false, last_rdap_check_at = $4, last_rdap_success_at = $5 WHERE id = $1`,
		domain.ID, expiresAt, ns, lastCheckAt, lastSuccessAt,
	); err != nil {
		t.Fatalf("seeding health columns for %q returned unexpected error: %v", hostname, err)
	}
	return domain
}

// TestDomainHealthScheduler_LeaderElection_ExactlyOneReplicaRuns proves the
// advisory lock lets exactly one of two concurrent schedulers run a cycle,
// mirroring TestDigestScheduler_LeaderElection_ExactlyOneReplicaRuns.
func TestDomainHealthScheduler_LeaderElection_ExactlyOneReplicaRuns(t *testing.T) {
	pool := newServeTestPool(t)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	leaderTenants := &blockingTenantLister{entered: entered, release: release}
	followerTenants := &staticTenantLister{}

	leader := newDomainHealthSchedulerForTest(t, pool, leaderTenants, db.NewDomainRepository(pool), &fakeRDAPClient{}, &fakeDomainHealthNotifier{})
	follower := newDomainHealthSchedulerForTest(t, pool, followerTenants, db.NewDomainRepository(pool), &fakeRDAPClient{}, &fakeDomainHealthNotifier{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		leader.runCycle(context.Background())
	}()

	// Wait until the leader holds the lock and is inside its cycle.
	<-entered

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

// TestDomainHealthScheduler_ZeroDomains_NoOpNoNotification: a tenant with no
// domains completes the cycle with no error and sends nothing.
func TestDomainHealthScheduler_ZeroDomains_NoOpNoNotification(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		db.NewDomainRepository(pool), &fakeRDAPClient{}, notifier)

	if err := s.runOnce(context.Background(), time.Now()); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.drift) != 0 || len(notifier.expiring) != 0 {
		t.Errorf("notifications = drift %d / expiring %d, want none for zero domains", len(notifier.drift), len(notifier.expiring))
	}
}

// TestDomainHealthScheduler_FirstCheck_LearnsBaselineNoAlert covers DHM-05:
// the first successful NS check stores the baseline, marks no drift, and
// sends no drift notification; a far-out expiration sends no expiration
// notification either.
func TestDomainHealthScheduler_FirstCheck_LearnsBaselineNoAlert(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-first.example.com"
	seedHealthDomain(t, pool, repo, hostname, nil, nil, nil)

	expires := now.Add(100 * 24 * time.Hour)
	registrar := "GoDaddy.com, LLC"
	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{
			expiresAt: map[string]*time.Time{hostname: &expires},
			registrar: map[string]*string{hostname: &registrar},
		},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns1.example.net", "ns2.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, mustDomainID(t, repo, hostname))
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if !nsSetsEqual(got.ExpectedNS, []string{"ns1.example.net", "ns2.example.net"}) {
		t.Errorf("ExpectedNS = %v, want the baseline learned on the first check", got.ExpectedNS)
	}
	if !nsSetsEqual(got.CurrentNS, []string{"ns1.example.net", "ns2.example.net"}) {
		t.Errorf("CurrentNS = %v, want the resolved set", got.CurrentNS)
	}
	if got.NSDriftDetected {
		t.Error("NSDriftDetected = true, want false on the baseline-learning check")
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
	if got.Registrar == nil || *got.Registrar != registrar {
		t.Errorf("Registrar = %v, want %q", got.Registrar, registrar)
	}
	if got.RDAPLastError != nil {
		t.Errorf("RDAPLastError = %v, want nil on a successful lookup", got.RDAPLastError)
	}
	if len(notifier.drift) != 0 || len(notifier.expiring) != 0 {
		t.Errorf("notifications = drift %d / expiring %d, want none", len(notifier.drift), len(notifier.expiring))
	}
}

// TestDomainHealthScheduler_NSChanged_MarksDriftAndNotifiesOnce covers
// DHM-06: a current NS set differing from the learned baseline marks drift
// and sends exactly one drift notification carrying both sets.
func TestDomainHealthScheduler_NSChanged_MarksDriftAndNotifiesOnce(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-drift.example.com"
	baseline := []string{"ns1.example.net", "ns2.example.net"}
	seedHealthDomain(t, pool, repo, hostname, nil, baseline, &now)

	expires := now.Add(100 * 24 * time.Hour)
	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{expiresAt: map[string]*time.Time{hostname: &expires}},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns3.example.net", "ns4.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, mustDomainID(t, repo, hostname))
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if !got.NSDriftDetected {
		t.Error("NSDriftDetected = false, want true when current NS differs from the baseline")
	}
	if len(notifier.drift) != 1 {
		t.Fatalf("drift notifications = %d, want exactly 1", len(notifier.drift))
	}
	if !nsSetsEqual(notifier.drift[0].ExpectedNS, baseline) {
		t.Errorf("notification ExpectedNS = %v, want %v", notifier.drift[0].ExpectedNS, baseline)
	}
	if !nsSetsEqual(notifier.drift[0].CurrentNS, []string{"ns3.example.net", "ns4.example.net"}) {
		t.Errorf("notification CurrentNS = %v, want the resolved set", notifier.drift[0].CurrentNS)
	}
}

// TestDomainHealthScheduler_Failures_DoNotAbortCycle is the load-bearing
// assertion for DHM-03/DHM-07: in a multi-domain cycle, a domain whose RDAP
// and NS lookups both fail records the RDAP error, keeps its learned NS
// baseline, and does not stop a second, healthy domain from being processed
// and persisted.
func TestDomainHealthScheduler_Failures_DoNotAbortCycle(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	failingHost := "dhm-failing.example.com"
	healthyHost := "dhm-healthy.example.com"
	baseline := []string{"ns1.failing.example.net"}
	seedHealthDomain(t, pool, repo, failingHost, nil, baseline, &now)
	seedHealthDomain(t, pool, repo, healthyHost, nil, nil, nil)

	healthyExpires := now.Add(90 * 24 * time.Hour)
	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{
			expiresAt: map[string]*time.Time{healthyHost: &healthyExpires},
			errs:      map[string]error{failingHost: errors.New("rdap: timeout")},
		},
		notifier,
	).WithNSLookup(func(_ context.Context, hostname string) ([]string, error) {
		if hostname == failingHost {
			return nil, errors.New("no such host")
		}
		return []string{"ns1.example.net", "ns2.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	failing, err := repo.GetByID(ctx, mustDomainID(t, repo, failingHost))
	if err != nil {
		t.Fatalf("GetByID(failing) returned unexpected error: %v", err)
	}
	if failing.RDAPLastError == nil {
		t.Error("failing domain RDAPLastError = nil, want the RDAP failure recorded")
	}
	if !nsSetsEqual(failing.ExpectedNS, baseline) {
		t.Errorf("failing domain ExpectedNS = %v, want the baseline %v preserved across an NS-lookup failure", failing.ExpectedNS, baseline)
	}
	if !nsSetsEqual(failing.CurrentNS, baseline) {
		t.Errorf("failing domain CurrentNS = %v, want the prior set %v preserved", failing.CurrentNS, baseline)
	}

	healthy, err := repo.GetByID(ctx, mustDomainID(t, repo, healthyHost))
	if err != nil {
		t.Fatalf("GetByID(healthy) returned unexpected error: %v", err)
	}
	if healthy.ExpiresAt == nil || !healthy.ExpiresAt.Equal(healthyExpires) {
		t.Errorf("healthy domain ExpiresAt = %v, want %v (the cycle must still process it)", healthy.ExpiresAt, healthyExpires)
	}
	if !nsSetsEqual(healthy.ExpectedNS, []string{"ns1.example.net", "ns2.example.net"}) {
		t.Errorf("healthy domain ExpectedNS = %v, want the learned baseline", healthy.ExpectedNS)
	}
}

// TestDomainHealthScheduler_RDAPFailureWithStoredExpiry_NoSpuriousAlert covers
// DHM-03's "record the failure without alerting spuriously" clause: when RDAP
// fails, the last known expiration is preserved for display but must NOT be
// re-evaluated for a threshold crossing. Seeding a stored expiry inside the
// 30-day band with a previous check 15 days earlier (when it was still >30
// days out) means re-evaluating the preserved expiry at `now` would look like
// a fresh 30-day crossing - the exact spurious alert the guard must prevent.
func TestDomainHealthScheduler_RDAPFailureWithStoredExpiry_NoSpuriousAlert(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-rdap-fail-stored.example.com"
	expires := now.Add(20 * 24 * time.Hour)
	prevCheck := now.Add(-15 * 24 * time.Hour)
	registrar := "GoDaddy.com, LLC"
	domain := seedHealthDomain(t, pool, repo, hostname, &expires, []string{"ns1.example.net"}, &prevCheck)
	if _, err := pool.Exec(ctx, "UPDATE domains SET registrar = $2 WHERE id = $1", domain.ID, registrar); err != nil {
		t.Fatalf("seeding registrar returned unexpected error: %v", err)
	}

	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{errs: map[string]error{hostname: errors.New("rdap: timeout")}},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns1.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	if len(notifier.expiring) != 0 {
		t.Errorf("expiration notifications = %d, want 0: an RDAP failure must never re-alert off the preserved expiry", len(notifier.expiring))
	}

	got, err := repo.GetByID(ctx, mustDomainID(t, repo, hostname))
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.RDAPLastError == nil {
		t.Error("RDAPLastError = nil, want the RDAP failure recorded")
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want the last known %v preserved across the failure", got.ExpiresAt, expires)
	}
	if got.Registrar == nil || *got.Registrar != registrar {
		t.Errorf("Registrar = %v, want %q preserved across the failure", got.Registrar, registrar)
	}
}

// TestDomainHealthScheduler_ExpirationCrossing_FiresOnceNotEveryCycle covers
// DHM-02 end to end: a crossing fires exactly one notification, and a second
// cycle inside the same band sends none.
func TestDomainHealthScheduler_ExpirationCrossing_FiresOnceNotEveryCycle(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-expiring.example.com"
	// At the previous check (16 days ago) the domain was 30 days out
	// (band 30); the same fixed expiration date is now 14 days out (band 15).
	expires := now.Add(14 * 24 * time.Hour)
	prevCheck := now.Add(-16 * 24 * time.Hour)
	seedHealthDomain(t, pool, repo, hostname, &expires, nil, &prevCheck)

	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{expiresAt: map[string]*time.Time{hostname: &expires}},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns1.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("first runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.expiring) != 1 {
		t.Fatalf("expiration notifications after first cycle = %d, want exactly 1", len(notifier.expiring))
	}
	if notifier.expiring[0].ThresholdDays != expirationThreshold15 {
		t.Errorf("crossed threshold = %d, want %d", notifier.expiring[0].ThresholdDays, expirationThreshold15)
	}

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("second runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.expiring) != 1 {
		t.Errorf("expiration notifications after second same-band cycle = %d, want still 1 (alerts once per crossing)", len(notifier.expiring))
	}
}

// TestDomainHealthScheduler_ExpirationCrossing_NotMaskedByInterveningRDAPFailure
// covers a bug where a threshold crossing landing on a day RDAP happened to
// fail could be lost forever: last_rdap_check_at (which the crossing
// calculation used to read as "the previous check") advances on every
// attempt, success or failure, so a failed attempt sitting between the
// previous successful check and today made the "previous band" look like
// today's band instead of the real one - the crossing that occurred is never
// re-detected. The domain's last successful check (16 days ago) saw it 30
// days out (band 30); a failed attempt one day ago updated
// last_rdap_check_at without ever seeing the current 14-day-out state
// (band 15). Today's RDAP call succeeds and must still fire the 30->15
// crossing by comparing against the last *successful* check, not the last
// attempt.
func TestDomainHealthScheduler_ExpirationCrossing_NotMaskedByInterveningRDAPFailure(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-crossing-masked.example.com"
	expires := now.Add(14 * 24 * time.Hour)
	lastSuccess := now.Add(-16 * 24 * time.Hour)
	lastAttempt := now.Add(-1 * 24 * time.Hour)
	seedHealthDomainWithSuccess(t, pool, repo, hostname, &expires, nil, &lastAttempt, &lastSuccess)

	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		&fakeRDAPClient{expiresAt: map[string]*time.Time{hostname: &expires}},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns1.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}
	if len(notifier.expiring) != 1 {
		t.Fatalf("expiration notifications = %d, want exactly 1: the 30->15 crossing must not be masked by the intervening RDAP failure", len(notifier.expiring))
	}
	if notifier.expiring[0].ThresholdDays != expirationThreshold15 {
		t.Errorf("crossed threshold = %d, want %d", notifier.expiring[0].ThresholdDays, expirationThreshold15)
	}
}

// TestDomainHealthScheduler_RDAPSuccessNoExpirationEvent_PreservesLastKnown
// covers a bug where an RDAP lookup that succeeds but returns no expiration
// event (rdap.Client.Lookup's documented (nil, nil, nil) case for a registry
// that omits it) was persisted as expires_at = NULL, erasing a previously
// known expiration from the UI and resetting the threshold-crossing dedupe
// so the next successful lookup could re-alert an already-handled crossing.
func TestDomainHealthScheduler_RDAPSuccessNoExpirationEvent_PreservesLastKnown(t *testing.T) {
	pool, tenantID := newServeTestPoolWithTenant(t)
	repo := db.NewDomainRepository(pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hostname := "dhm-no-expiration-event.example.com"
	expires := now.Add(100 * 24 * time.Hour)
	prevCheck := now.Add(-1 * 24 * time.Hour)
	seedHealthDomain(t, pool, repo, hostname, &expires, nil, &prevCheck)

	notifier := &fakeDomainHealthNotifier{}
	s := newDomainHealthSchedulerForTest(t, pool,
		&staticTenantLister{tenants: []db.Tenant{{ID: tenantID, Name: "cli-test-tenant"}}},
		repo,
		// No entry for hostname in expiresAt/errs: Lookup succeeds (nil err)
		// but returns a nil expiration, mirroring a registry response with
		// no expiration event.
		&fakeRDAPClient{},
		notifier,
	).WithNSLookup(func(context.Context, string) ([]string, error) {
		return []string{"ns1.example.net"}, nil
	})

	if err := s.runOnce(ctx, now); err != nil {
		t.Fatalf("runOnce() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, mustDomainID(t, repo, hostname))
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want the last known %v preserved when RDAP returns no expiration event", got.ExpiresAt, expires)
	}
	if got.RDAPLastError != nil {
		t.Errorf("RDAPLastError = %v, want nil - this is a successful lookup", got.RDAPLastError)
	}
}

// TestExpirationAlert_Thresholds covers DHM-02's threshold-crossing logic:
// each of 30/15/7 fires once, a same-band repeat does not, a widening band
// (renewal) does not re-alert, and a first check already inside a band
// alerts.
func TestExpirationAlert_Thresholds(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	at := func(days int) *time.Time { tm := now.Add(time.Duration(days) * 24 * time.Hour); return &tm }

	cases := []struct {
		name          string
		prevExpiresAt *time.Time
		prevCheckedAt *time.Time
		expiresAt     time.Time
		wantThreshold int
		wantDays      int
		wantCrossed   bool
	}{
		{"first check inside 7-day band alerts", nil, nil, *at(6), expirationThreshold7, 6, true},
		{"first check inside 30-day band alerts", nil, nil, *at(20), expirationThreshold30, 20, true},
		{"crossing 30 to 15", at(14), at(-16), *at(14), expirationThreshold15, 14, true},
		{"crossing 15 to 7", at(6), at(-9), *at(6), expirationThreshold7, 6, true},
		{"same 7-day band does not re-alert", at(6), at(-1), *at(6), expirationThreshold7, 6, false},
		{"same 30-day band does not re-alert", at(29), at(0), *at(25), expirationThreshold30, 25, false},
		{"renewal widening 7 to 30 does not re-alert", at(5), at(-1), *at(20), expirationThreshold30, 20, false},
		{"renewal widening 15 to 30 does not re-alert", at(10), at(-5), *at(20), expirationThreshold30, 20, false},
		{"farther than 30 days never alerts", nil, nil, *at(100), 0, 100, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			threshold, days, crossed := expirationAlert(now, tc.prevExpiresAt, tc.prevCheckedAt, tc.expiresAt)
			if threshold != tc.wantThreshold {
				t.Errorf("threshold = %d, want %d", threshold, tc.wantThreshold)
			}
			if days != tc.wantDays {
				t.Errorf("daysRemaining = %d, want %d", days, tc.wantDays)
			}
			if crossed != tc.wantCrossed {
				t.Errorf("crossed = %v, want %v", crossed, tc.wantCrossed)
			}
		})
	}
}

// TestNextDomainHealthFire covers the daily 00:00 UTC computation, including
// the exact-midnight boundary (next fire is the following day, strictly
// after now).
func TestNextDomainHealthFire(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"mid-day", time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)},
		{"exact midnight rolls to next day", time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)},
		{"just before midnight", time.Date(2026, 9, 28, 23, 59, 0, 0, time.UTC), time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDomainHealthFire(tc.now); !got.Equal(tc.want) {
				t.Errorf("nextDomainHealthFire(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

// TestNewDomainHealthScheduler_BootWiring_RunStopsOnContextCancel covers the
// boot wiring: the scheduler the serve path constructs starts and stops
// cleanly when the serve context is canceled, mirroring
// TestNewDigestScheduler_BootWiring_RunStopsOnContextCancel.
func TestNewDomainHealthScheduler_BootWiring_RunStopsOnContextCancel(t *testing.T) {
	pool := newServeTestPool(t)

	s, err := newDomainHealthScheduler(pool, config.Config{DatabaseURL: testDatabaseURL(t), MasterKey: "cli-domain-health-test-master-key"}, zap.NewNop())
	if err != nil {
		t.Fatalf("newDomainHealthScheduler() returned unexpected error: %v", err)
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
		t.Fatal("DomainHealthScheduler.Run did not stop on context cancellation")
	}
}

// mustDomainID looks up a domain's id by hostname for the pool's tenant.
func mustDomainID(t *testing.T, repo *db.DomainRepository, hostname string) string {
	t.Helper()
	ctx := context.Background()
	items, _, err := repo.ListPaginated(ctx, 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	for _, d := range items {
		if d.Hostname == hostname {
			return d.ID
		}
	}
	t.Fatalf("domain %q not found", hostname)
	return ""
}
