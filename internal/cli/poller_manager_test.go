//go:build integration

package cli

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const pollerManagerTestMasterKey = "poller-manager-test-master-key"

// storeTestDatadogIntegration upserts a fake (never dialed - see the large
// PollIntervalSeconds used by these tests) Datadog integration row so
// newPollerFromStoredIntegration has something to build from.
func storeTestDatadogIntegration(t *testing.T, pool *db.Pool) {
	t.Helper()
	encAPIKey, err := crypto.Encrypt(pollerManagerTestMasterKey, []byte("fake-api-key"))
	if err != nil {
		t.Fatalf("crypto.Encrypt(api key) returned unexpected error: %v", err)
	}
	encAppKey, err := crypto.Encrypt(pollerManagerTestMasterKey, []byte("fake-app-key"))
	if err != nil {
		t.Fatalf("crypto.Encrypt(app key) returned unexpected error: %v", err)
	}

	repo := db.NewIntegrationRepository(pool)
	if err := repo.UpsertDatadog(context.Background(), encAPIKey, encAppKey); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}
}

// pollerManagerTestConfig returns a config.Config whose poll interval is
// large enough that no test in this file ever waits for a real tick - the
// poller's ticker never fires within the test's lifetime, so Run only ever
// exits via ctx cancellation, never via pollOnce (which would otherwise
// need real network access to Datadog).
func pollerManagerTestConfig() config.Config {
	return config.Config{MasterKey: pollerManagerTestMasterKey, PollIntervalSeconds: 3600}
}

// TestPollerManager_Restart_WithStoredIntegration_StartsAndTracksRunning
// covers PLD-01/PLD-04 at the PollerManager level directly (a real gap the
// Verifier found: no test exercised PollerManager itself, only the spy
// used by the HTTP-layer tests). A no-op Restart (Mutant B from the
// Verifier's sensor) would return started=false or leave m.cancel unset;
// this test fails against that mutant.
func TestPollerManager_Restart_WithStoredIntegration_StartsAndTracksRunning(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	// Restart is a no-op for a non-leader replica (see the field's own doc
	// comment); these tests exercise Restart's own lifecycle behavior
	// directly rather than going through RunLeaderLoop, so they assume
	// leadership already granted.
	mgr.leading.Store(true)

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if !started {
		t.Fatal("Restart() started = false, want true - a stored integration exists")
	}

	mgr.mu.Lock()
	running := mgr.cancel != nil && mgr.done != nil
	mgr.mu.Unlock()
	if !running {
		t.Fatal("PollerManager has no cancel/done tracked after a successful Restart, want the poller goroutine to be tracked as running")
	}
}

// TestPollerManager_Restart_NoIntegration_ReturnsFalseWithoutError covers
// PLD-02/PLD-03's "nothing to start" contract at the manager level.
func TestPollerManager_Restart_NoIntegration_ReturnsFalseWithoutError(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	// Deliberately no storeTestDatadogIntegration call.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	mgr.leading.Store(true)

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if started {
		t.Fatal("Restart() started = true, want false - no integration is stored")
	}
}

// TestPollerManager_Restart_NotLeading_IsNoOp covers the double-poller fix:
// IntegrationsHandler.ConnectDatadog calls Restart on whichever replica the
// admin API load balancer happened to route the request to, not necessarily
// the one currently leading. Restart must not start a poller on a replica
// that isn't leading - RunLeaderLoop is solely responsible for starting one
// once this replica (or whichever one is actually leading) acquires the
// lock.
func TestPollerManager_Restart_NotLeading_IsNoOp(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	// Deliberately not setting mgr.leading - default zero value is false.

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if started {
		t.Fatal("Restart() started = true on a non-leading replica, want false (no-op) even though an integration is stored")
	}

	mgr.mu.Lock()
	running := mgr.cancel != nil || mgr.done != nil
	mgr.mu.Unlock()
	if running {
		t.Error("PollerManager tracks a running poller after a non-leading Restart, want no poller started")
	}
}

// TestPollerManager_Stop_ExitsPromptlyAndClearsState covers PLD-04: once
// Stop returns, no poller goroutine is left tracked as running, and a
// subsequent Restart is unaffected by whatever ran before it.
func TestPollerManager_Stop_ExitsPromptlyAndClearsState(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	mgr.leading.Store(true)

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("Restart() = (%v, %v), want (true, nil)", started, err)
	}

	stopped := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s, want it to cancel and wait for the running poller promptly")
	}

	mgr.mu.Lock()
	cleared := mgr.cancel == nil && mgr.done == nil
	mgr.mu.Unlock()
	if !cleared {
		t.Error("PollerManager still tracks a running poller after Stop(), want cancel/done cleared")
	}
}

// TestPollerManager_Restart_CalledTwice_TearsDownPreviousBeforeStartingNew
// covers PLD-05's "replace the running poller" contract and the Edge
// Cases' "two pollers never run concurrently" guarantee: a second Restart
// must fully tear down whatever is running before tracking a new one, so
// there is never a moment with two independently-tracked pollers.
func TestPollerManager_Restart_CalledTwice_TearsDownPreviousBeforeStartingNew(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	mgr.leading.Store(true)

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("first Restart() = (%v, %v), want (true, nil)", started, err)
	}
	mgr.mu.Lock()
	firstDone := mgr.done
	mgr.mu.Unlock()

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("second Restart() = (%v, %v), want (true, nil)", started, err)
	}

	select {
	case <-firstDone:
	default:
		t.Error("first poller's done channel is not closed after a second Restart, want the previous poller torn down before a new one starts")
	}

	mgr.mu.Lock()
	secondDone := mgr.done
	running := mgr.cancel != nil && secondDone != nil
	mgr.mu.Unlock()
	if !running {
		t.Fatal("no cancel/done tracked after second Restart, want the new poller tracked as running")
	}
	if secondDone == firstDone {
		t.Error("second Restart reused the first run's done channel, want a fresh one per Restart")
	}
}

// isManualRunning reports whether mgr currently believes it has a manual
// scheduler tracked as running (manualCancel/manualDone both set).
func isManualRunning(mgr *PollerManager) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.manualCancel != nil && mgr.manualDone != nil
}

// TestPollerManager_RunLeaderLoop_ManualScheduler_StartsWithoutDatadogIntegration
// covers MP-08/T7: the manual scheduler starts on leadership acquisition
// even when no Datadog integration is connected at all - the existing
// "poller not started" warning path for Datadog (PLD-02/PLD-03) is
// untouched and independent of the manual scheduler's own lifecycle.
func TestPollerManager_RunLeaderLoop_ManualScheduler_StartsWithoutDatadogIntegration(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	// Deliberately no storeTestDatadogIntegration call.

	ctx, cancel := context.WithCancel(context.Background())

	mgr := leaderTestPollerManager(t, ctx, pool, testDatabaseURL(t))
	go mgr.RunLeaderLoop(ctx)
	// Cancel and wait for RunLeaderLoop to actually relinquish the poller
	// leadership advisory lock before this test's cleanup returns - a bare
	// "defer cancel()" only asks RunLeaderLoop's goroutine to stop, it
	// doesn't wait for it to finish releasing the lock, which shares its
	// single global key (pollerLeaderLockKey) with every other test in this
	// file. Left unsynchronized, a subsequent test's own
	// killPollerLeaderBackend could find and kill this stale leftover
	// session instead of its own current leader's.
	t.Cleanup(func() {
		cancel()
		waitUntil(2*time.Second, func() bool { return !mgr.leading.Load() })
		mgr.Stop()
		waitForLeaderLockReleased(t, pool)
	})

	if !waitUntil(3*time.Second, func() bool { return mgr.leading.Load() }) {
		t.Fatal("single-replica RunLeaderLoop did not acquire leadership within 3s")
	}
	if !waitUntil(3*time.Second, func() bool { return isManualRunning(mgr) }) {
		t.Fatal("manual scheduler did not start on leadership acquisition despite no Datadog integration being connected, want it to start regardless (MP-08)")
	}
}

// TestPollerManager_ManualScheduler_StopsOnStop covers the manual
// scheduler's own stop path: PollerManager.Stop() must tear it down and
// wait for it to exit, same guarantee already made for the Datadog poller.
func TestPollerManager_ManualScheduler_StopsOnStop(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	mgr.leading.Store(true)
	mgr.startManualScheduler()

	if !isManualRunning(mgr) {
		t.Fatal("manual scheduler not tracked as running right after startManualScheduler()")
	}

	stopped := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s, want it to cancel and wait for the manual scheduler promptly")
	}

	if isManualRunning(mgr) {
		t.Error("manual scheduler still tracked as running after Stop(), want manualCancel/manualDone cleared")
	}
}

// TestPollerManager_RunLeaderLoop_ManualScheduler_StopsOnLeadershipLoss
// covers the leadership-loss branch: when the leader's session dies, the
// manual scheduler must stop just like the Datadog poller does, not keep
// running orphaned under a lock this replica no longer holds - and the new
// leader (a second replica) starts its own. Deliberately uses two replicas,
// the same shape as
// TestPollerManager_RunLeaderLoop_LeaderBackendKilled_FailoverAndAbort: with
// only a single replica in the test, the very same instance would just
// re-acquire the now-free lock and restart its manual scheduler on its next
// loop iteration (no delay imposed before retrying), often faster than a
// polling assertion could ever observe the intermediate "stopped" state -
// not a bug, just not what a single-instance version of this test could
// reliably prove.
func TestPollerManager_RunLeaderLoop_ManualScheduler_StopsOnLeadershipLoss(t *testing.T) {
	pool := newServeTestPool(t)
	dsn := testDatabaseURL(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgrA := leaderTestPollerManager(t, ctx, pool, dsn)
	mgrB := leaderTestPollerManager(t, ctx, pool, dsn)
	go mgrA.RunLeaderLoop(ctx)
	go mgrB.RunLeaderLoop(ctx)
	t.Cleanup(mgrA.Stop)
	t.Cleanup(mgrB.Stop)

	if !waitUntil(3*time.Second, func() bool { return isManualRunning(mgrA) || isManualRunning(mgrB) }) {
		t.Fatal("neither replica started its manual scheduler within 3s of leadership being available")
	}

	leader, standby := mgrA, mgrB
	if isManualRunning(mgrB) {
		leader, standby = mgrB, mgrA
	}

	if !killPollerLeaderBackend(t, pool) {
		t.Fatal("could not find the leader's advisory-lock backend to kill - test setup problem")
	}

	if !waitUntil(3*time.Second, func() bool { return !isManualRunning(leader) }) {
		t.Fatal("killed leader's manual scheduler still tracked as running after leadership was lost, want it stopped symmetrically with the Datadog poller")
	}
	if !waitUntil(3*time.Second, func() bool { return isManualRunning(standby) }) {
		t.Fatal("standby's manual scheduler did not start after taking over leadership")
	}
}

// TestPollerManager_Restart_DoesNotDisturbRunningManualScheduler covers the
// core design decision behind T7 (Approach Exploration: "not folded into
// Restart"): calling Restart repeatedly (e.g. a Datadog key rotation via
// IntegrationsHandler.ConnectDatadog) must never restart or otherwise touch
// an already-running manual scheduler - it has no credentials to rotate and
// would otherwise lose its in-memory per-service failure-streak state on
// every unrelated Datadog reconnect.
func TestPollerManager_Restart_DoesNotDisturbRunningManualScheduler(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	mgr.leading.Store(true)
	mgr.startManualScheduler()

	mgr.mu.Lock()
	firstManualDone := mgr.manualDone
	mgr.mu.Unlock()

	for i := 0; i < 3; i++ {
		if _, err := mgr.Restart(context.Background()); err != nil {
			t.Fatalf("Restart() call %d returned unexpected error: %v", i, err)
		}
	}

	mgr.mu.Lock()
	secondManualDone := mgr.manualDone
	mgr.mu.Unlock()

	select {
	case <-firstManualDone:
		t.Fatal("manual scheduler's done channel closed after Restart(), want Restart to leave it untouched")
	default:
	}
	if secondManualDone != firstManualDone {
		t.Error("manual scheduler's done channel changed identity across Restart() calls, want the same running scheduler untouched")
	}
	if !isManualRunning(mgr) {
		t.Error("manual scheduler no longer tracked as running after repeated Restart() calls")
	}
}

// leaderTestPollerManager builds a PollerManager with short leader
// retry/heartbeat intervals (same package as poller_manager.go, so the
// unexported fields are directly settable) - the production defaults
// (10s each) would make these tests unnecessarily slow.
func leaderTestPollerManager(t *testing.T, parentCtx context.Context, pool *db.Pool, dsn string) *PollerManager {
	t.Helper()
	mgr := NewPollerManager(parentCtx, pool, pollerManagerTestConfig(), zap.NewNop(), dsn)
	mgr.leaderRetryInterval = 50 * time.Millisecond
	mgr.leaderHeartbeatInterval = 100 * time.Millisecond
	return mgr
}

// isLeading reports whether mgr currently believes it is running a poller
// (i.e. currently holds leadership and Restart succeeded).
func isLeading(mgr *PollerManager) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.cancel != nil && mgr.done != nil
}

// waitUntil polls cond every 20ms until it returns true or timeout elapses,
// returning whether cond became true in time.
func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// killPollerLeaderBackend terminates the Postgres backend currently holding
// the poller leadership advisory lock, simulating a replica crashing
// (rather than gracefully releasing) while leader. pollerLeaderLockKey fits
// in 32 bits, so pg_advisory_lock(bigint) stores it as classid=0,
// objid=pollerLeaderLockKey, objsubid=1 in pg_locks - this queries that
// shape directly rather than reaching into PollerManager's internal
// pglock.Handle (which RunLeaderLoop keeps unexported and unreachable from
// a test), the same way an operator killing a pod has no cooperation from
// the process being killed.
// waitForLeaderLockReleased polls pg_locks until no session holds the
// poller leadership advisory lock (pollerLeaderLockKey) or timeout elapses.
// A test whose PollerManager just gave up leadership must call this (not
// just poll its own in-process `leading` flag) before letting a later test
// in the same file attempt its own acquisition: `leading` flips to false the
// instant RunLeaderLoop's heartbeat loop sees ctx canceled, but the physical
// pg_advisory_unlock + connection close (pglock.Handle.Release) is a
// separate network round trip that can still be in flight for a short
// window afterward - a later test's TryAcquire racing that window would
// find the lock still held by the departing test's now-orphaned session,
// not its own.
func waitForLeaderLockReleased(t *testing.T, pool *db.Pool) {
	t.Helper()
	waitUntil(2*time.Second, func() bool {
		var pid int
		err := pool.QueryRow(context.Background(),
			`SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND classid = 0 AND objid = $1 AND objsubid = 1 LIMIT 1`,
			pollerLeaderLockKey,
		).Scan(&pid)
		return err != nil // ErrNoRows once released
	})
}

func killPollerLeaderBackend(t *testing.T, pool *db.Pool) bool {
	t.Helper()
	ctx := context.Background()
	var pid int
	err := pool.QueryRow(ctx,
		`SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND classid = 0 AND objid = $1 AND objsubid = 1 LIMIT 1`,
		pollerLeaderLockKey,
	).Scan(&pid)
	if err != nil {
		return false
	}
	if _, err := pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("pg_terminate_backend() returned unexpected error: %v", err)
	}
	return true
}

// TestPollerManager_RunLeaderLoop_SingleReplica_AcquiresAndPolls covers
// HA-07: with a single replica, RunLeaderLoop acquires the lock immediately
// and starts polling, same as the unconditional boot-time Restart it
// replaces.
func TestPollerManager_RunLeaderLoop_SingleReplica_AcquiresAndPolls(t *testing.T) {
	pool := newServeTestPool(t)
	dsn := testDatabaseURL(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := leaderTestPollerManager(t, ctx, pool, dsn)
	go mgr.RunLeaderLoop(ctx)
	t.Cleanup(mgr.Stop)

	if !waitUntil(3*time.Second, func() bool { return isLeading(mgr) }) {
		t.Fatal("single-replica RunLeaderLoop did not start polling within 3s, want immediate acquisition")
	}
}

// TestPollerManager_RunLeaderLoop_SetsApplicationNameFromHostname covers
// poller-status-real-state's POLLST-01: the connection RunLeaderLoop uses to
// hold the leadership lock carries this replica's identity in
// pg_stat_activity.application_name, sourced from HOSTNAME, so
// PollerLeadershipRepository.CurrentLeader can report it.
func TestPollerManager_RunLeaderLoop_SetsApplicationNameFromHostname(t *testing.T) {
	pool := newServeTestPool(t)
	dsn := testDatabaseURL(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)
	t.Setenv("HOSTNAME", "test-replica-hostname-xyz")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := leaderTestPollerManager(t, ctx, pool, dsn)
	go mgr.RunLeaderLoop(ctx)
	t.Cleanup(mgr.Stop)

	if !waitUntil(3*time.Second, func() bool { return isLeading(mgr) }) {
		t.Fatal("RunLeaderLoop did not acquire leadership within 3s")
	}

	leadership := db.NewPollerLeadershipRepository(pool)
	leader, err := leadership.CurrentLeader(context.Background())
	if err != nil {
		t.Fatalf("CurrentLeader() returned unexpected error: %v", err)
	}
	if leader == nil {
		t.Fatal("CurrentLeader() = nil, want the leading replica")
	}
	if leader.ApplicationName != "test-replica-hostname-xyz" {
		t.Errorf("ApplicationName = %q, want %q", leader.ApplicationName, "test-replica-hostname-xyz")
	}
}

// TestReplicaApplicationName_HostnameUnset_FallsBackToPlaceholder covers the
// edge case: outside Kubernetes (HOSTNAME unset), application_name falls
// back to a fixed placeholder rather than an empty string.
func TestReplicaApplicationName_HostnameUnset_FallsBackToPlaceholder(t *testing.T) {
	t.Setenv("HOSTNAME", "")

	if got := replicaApplicationName(); got != "unknown" {
		t.Errorf("replicaApplicationName() = %q, want %q", got, "unknown")
	}
}

// TestDSNWithApplicationName_URLDSN_AppendsAsQueryParam covers the common
// case: a URL-style DSN gets application_name set as a query parameter,
// preserving existing parameters.
func TestDSNWithApplicationName_URLDSN_AppendsAsQueryParam(t *testing.T) {
	got := dsnWithApplicationName("postgres://user:pass@host:5432/db?sslmode=disable", "my-replica")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q) returned unexpected error: %v", got, err)
	}
	if u.Query().Get("application_name") != "my-replica" {
		t.Errorf("application_name = %q, want %q (got DSN %q)", u.Query().Get("application_name"), "my-replica", got)
	}
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("sslmode = %q, want %q preserved (got DSN %q)", u.Query().Get("sslmode"), "disable", got)
	}
}

// TestPollerManager_RunLeaderLoop_TwoReplicas_OnlyOneRuns covers
// HA-01/HA-02: with two PollerManager instances sharing the same database,
// only one ever runs the poller at a time; the other keeps retrying
// acquisition without polling.
func TestPollerManager_RunLeaderLoop_TwoReplicas_OnlyOneRuns(t *testing.T) {
	pool := newServeTestPool(t)
	dsn := testDatabaseURL(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgrA := leaderTestPollerManager(t, ctx, pool, dsn)
	mgrB := leaderTestPollerManager(t, ctx, pool, dsn)
	go mgrA.RunLeaderLoop(ctx)
	go mgrB.RunLeaderLoop(ctx)
	t.Cleanup(mgrA.Stop)
	t.Cleanup(mgrB.Stop)

	if !waitUntil(3*time.Second, func() bool { return isLeading(mgrA) || isLeading(mgrB) }) {
		t.Fatal("neither replica acquired leadership within 3s")
	}

	// Give the loser every chance to (wrongly) also start polling before
	// asserting exclusivity - a flaky implementation racing both Restarts
	// through would likely show it within a few retry intervals.
	time.Sleep(300 * time.Millisecond)

	leadingA, leadingB := isLeading(mgrA), isLeading(mgrB)
	if leadingA == leadingB {
		t.Fatalf("exactly one replica should be leading, got A=%v B=%v", leadingA, leadingB)
	}
}

// TestPollerManager_RunLeaderLoop_LeaderBackendKilled_FailoverAndAbort
// covers HA-04 (failover: the standby acquires and starts polling once the
// leader's session dies) and HA-05 (the leader aborts rather than
// continuing to run once it can no longer prove it holds the lock) in one
// scenario, since killing the leader's backend is the single event both
// requirements react to.
func TestPollerManager_RunLeaderLoop_LeaderBackendKilled_FailoverAndAbort(t *testing.T) {
	pool := newServeTestPool(t)
	dsn := testDatabaseURL(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgrA := leaderTestPollerManager(t, ctx, pool, dsn)
	mgrB := leaderTestPollerManager(t, ctx, pool, dsn)
	go mgrA.RunLeaderLoop(ctx)
	go mgrB.RunLeaderLoop(ctx)
	t.Cleanup(mgrA.Stop)
	t.Cleanup(mgrB.Stop)

	if !waitUntil(3*time.Second, func() bool { return isLeading(mgrA) || isLeading(mgrB) }) {
		t.Fatal("neither replica acquired leadership within 3s")
	}

	leader, standby := mgrA, mgrB
	if isLeading(mgrB) {
		leader, standby = mgrB, mgrA
	}

	if !killPollerLeaderBackend(t, pool) {
		t.Fatal("could not find the leader's advisory-lock backend to kill - test setup problem")
	}

	// HA-05: the killed leader must stop believing it's running within
	// roughly one heartbeat interval of its session dying.
	if !waitUntil(3*time.Second, func() bool { return !isLeading(leader) }) {
		t.Fatal("killed leader still reports itself as leading, want it to detect lock loss and stop (HA-05)")
	}

	// HA-04: the standby must take over within roughly one heartbeat/retry
	// interval of the leader's session dying.
	if !waitUntil(3*time.Second, func() bool { return isLeading(standby) }) {
		t.Fatal("standby did not acquire leadership and start polling after the leader's session died (HA-04)")
	}
}

// TestPollerManager_ConcurrentRestarts_NeverLeavesTwoPollers covers the Edge
// Cases' "two pollers never run concurrently" guarantee under contention:
// N goroutines racing Restart behind a start barrier must all return, and the
// manager must end tracking a consistent cancel/done pair - never two
// independently-tracked pollers. Run under -race, this also proves Restart's
// m.mu critical section has no data race.
func TestPollerManager_ConcurrentRestarts_NeverLeavesTwoPollers(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	mgr.leading.Store(true)

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			if _, err := mgr.Restart(context.Background()); err != nil {
				t.Errorf("concurrent Restart() returned unexpected error: %v", err)
			}
		}()
	}
	close(start)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Restarts did not all return within 10s, want no deadlock")
	}

	mgr.mu.Lock()
	cancelSet, doneSet := mgr.cancel != nil, mgr.done != nil
	mgr.mu.Unlock()
	if cancelSet != doneSet {
		t.Errorf("cancel/done inconsistent after concurrent Restarts: cancel=%v done=%v", cancelSet, doneSet)
	}

	stopped := make(chan struct{})
	go func() { mgr.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() after concurrent Restarts did not return within 5s")
	}
	mgr.mu.Lock()
	cleared := mgr.cancel == nil && mgr.done == nil
	mgr.mu.Unlock()
	if !cleared {
		t.Error("manager still tracks a poller after Stop(), want cancel/done cleared")
	}
}

// TestPollerManager_ConcurrentRestartAndStop_NoDeadlockOrInconsistentState
// races Restart against Stop, the two paths that share m.mu and the
// stopLocked helper. Every goroutine must return (no deadlock), the tracked
// cancel/done pair must stay consistent, and a final Stop must clear it.
func TestPollerManager_ConcurrentRestartAndStop_NoDeadlockOrInconsistentState(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop(), testDatabaseURL(t))
	t.Cleanup(mgr.Stop)
	mgr.leading.Store(true)

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				if _, err := mgr.Restart(context.Background()); err != nil {
					t.Errorf("concurrent Restart() returned unexpected error: %v", err)
				}
				return
			}
			mgr.Stop()
		}(i)
	}
	close(start)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Restart/Stop did not all return within 10s, want no deadlock")
	}

	mgr.mu.Lock()
	cancelSet, doneSet := mgr.cancel != nil, mgr.done != nil
	mgr.mu.Unlock()
	if cancelSet != doneSet {
		t.Errorf("cancel/done inconsistent after concurrent Restart/Stop: cancel=%v done=%v", cancelSet, doneSet)
	}

	stopped := make(chan struct{})
	go func() { mgr.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("final Stop() did not return within 5s")
	}
	mgr.mu.Lock()
	cleared := mgr.cancel == nil && mgr.done == nil
	mgr.mu.Unlock()
	if !cleared {
		t.Error("manager still tracks a poller after the final Stop(), want cancel/done cleared")
	}
}
