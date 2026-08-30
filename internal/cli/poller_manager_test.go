//go:build integration

package cli

import (
	"context"
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

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if started {
		t.Fatal("Restart() started = true, want false - no integration is stored")
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
