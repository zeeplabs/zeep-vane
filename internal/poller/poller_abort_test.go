//go:build integration

package poller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// pollerAbortTestLockKey is a dedicated advisory lock key for this file,
// fitting in 32 bits (classid = 0 in pg_locks, matching
// internal/cli/poller_manager_test.go's killPollerLeaderBackend
// convention) and distinct from every other block documented in
// internal/pglock's own doc comment (dbtest: 727100001-727100003;
// internal/pglock's own tests: 727300001+; poller_manager's production
// key: 727200001) - this file only ever needs one key of its own, held for
// the lifetime of a single test at a time.
const pollerAbortTestLockKey int64 = 727400001

// killAdvisoryLockHolder terminates the Postgres backend currently holding
// the session-scoped advisory lock identified by key, simulating a
// replica's process dying (rather than gracefully releasing) while
// leading - the same out-of-band pg_terminate_backend mechanism
// internal/cli's HA-04 test (killPollerLeaderBackend) uses, reused here
// directly since key fits in 32 bits (classid = 0, objid = key,
// objsubid = 1, per pg_advisory_lock(bigint)'s pg_locks representation).
func killAdvisoryLockHolder(t *testing.T, pool *db.Pool, key int64) bool {
	t.Helper()
	ctx := context.Background()
	var pid int
	err := pool.QueryRow(ctx,
		`SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND classid = 0 AND objid = $1 AND objsubid = 1 LIMIT 1`,
		key,
	).Scan(&pid)
	if err != nil {
		return false
	}
	if _, err := pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("pg_terminate_backend() returned unexpected error: %v", err)
	}
	return true
}

// waitUntilPoller polls cond every 10ms until it returns true or timeout
// elapses, returning whether cond became true in time - mirrors
// internal/cli/poller_manager_test.go's waitUntil helper (unexported,
// different package, so reimplemented here rather than imported).
func waitUntilPoller(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// abortTestProvider fetches an immediate result for every SLO ID except
// blockedSLOID, which it blocks on until unblock is closed - signaling on
// reached the moment it starts blocking. This is what lets the test kill
// the leadership lock at the exact moment a multi-service poll cycle is
// mid-flight on a specific service, rather than merely between cycles.
type abortTestProvider struct {
	blockedSLOID string
	reached      chan struct{}
	unblock      chan struct{}
}

func (p *abortTestProvider) FetchSLOStatus(ctx context.Context, sloID string) (datadog.SLOStatus, error) {
	if sloID != p.blockedSLOID {
		return datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 100}, nil
	}

	close(p.reached)
	select {
	case <-ctx.Done():
		return datadog.SLOStatus{}, ctx.Err()
	case <-p.unblock:
	}
	// By the time we're unblocked, the test has already canceled ctx (that
	// is what triggers the unblock) - report whatever ctx says now, exactly
	// as a real HTTP client using ctx for its request would.
	if err := ctx.Err(); err != nil {
		return datadog.SLOStatus{}, err
	}
	return datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 100}, nil
}

// TestPoller_PollOnce_AbortsMidCycle_NoWritesForServicesAfterLeadershipLoss
// covers HA-05 at the data level (spec.md P1 AC5): "WHEN a replica loses
// the lock mid-cycle (renewal fails) THEN it SHALL abort the in-flight poll
// cycle without writing partial results, rather than letting it complete."
//
// Unlike internal/cli/poller_manager_test.go's HA-04/HA-05 test (which
// configures zero services and only asserts a leadership boolean),  this
// test configures three real services and blocks the second one's fetch
// mid-flight, kills the leader's advisory-lock backend out-of-band (the
// same pg_terminate_backend mechanism as the existing HA-04 test) while
// that fetch is in progress, and only then lets it proceed - proving no
// status_intervals row is written for the service whose fetch was aborted
// nor for the service after it in pollOnce's sequential loop, while the
// service that completed before the abort keeps its write (HA-05 is about
// not completing writes *after* losing the lock, not full-cycle atomicity).
//
// pollOnce's per-service loop
// (internal/poller/poller.go's `for _, svc := range services` block) has no
// explicit ctx.Done() check between iterations - this test's finding is
// that the abort still lands correctly today because every write path
// underneath it (FetchWithRetry's provider call, and both
// statusIntervalWriter/serviceStatusUpdater's Postgres calls) is
// ctx-threaded: once ctx is canceled, the in-flight fetch/write fails
// immediately, and every subsequent service's calls fail the same way
// before ever reaching a write, since pgx and a real HTTP client both
// reject an already-canceled context before doing any I/O.
func TestPoller_PollOnce_AbortsMidCycle_NoWritesForServicesAfterLeadershipLoss(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	createTestIntegration(t, pool, dsn, integrations)

	svc1 := createTestServiceWithSLO(t, pool, services, fmt.Sprintf("abort-slo-1-%d", time.Now().UnixNano()))
	svc2 := createTestServiceWithSLO(t, pool, services, fmt.Sprintf("abort-slo-2-%d", time.Now().UnixNano()))
	svc3 := createTestServiceWithSLO(t, pool, services, fmt.Sprintf("abort-slo-3-%d", time.Now().UnixNano()))

	// Leader election is layered on top of Poller by internal/cli in
	// production (RunLeaderLoop -> Restart/Stop); reproduce the same
	// pglock-Acquire + Healthy-heartbeat + cancel-on-loss shape directly
	// here so this test exercises the real pglock primitive and the real
	// pollOnce write-abort path together, without needing to inject a fake
	// SLOProvider through internal/cli's PollerManager (which always builds
	// a real Datadog HTTP client from the stored integration - not
	// controllable for a deterministic mid-fetch block).
	handle, ok, err := pglock.TryAcquire(ctx, dsn, pollerAbortTestLockKey)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want true - nothing else should hold this test's lock key")
	}
	t.Cleanup(func() { _ = handle.Release(context.Background()) })

	provider := &abortTestProvider{
		blockedSLOID: svc2.SLOID,
		reached:      make(chan struct{}),
		unblock:      make(chan struct{}),
	}

	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, zap.NewNop())

	runCtx, cancelRun := context.WithCancel(ctx)
	t.Cleanup(cancelRun)

	// Mirrors internal/cli/poller_manager.go's heartbeatUntilLost: cancel
	// runCtx the moment the held lock's session is found unhealthy.
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if !handle.Healthy(context.Background()) {
					cancelRun()
					return
				}
			}
		}
	}()

	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		p.pollOnce(runCtx)
	}()

	select {
	case <-provider.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("provider never reached the blocked service's fetch within 3s")
	}

	if !killAdvisoryLockHolder(t, pool, pollerAbortTestLockKey) {
		t.Fatal("could not find this test's advisory-lock backend to kill - test setup problem")
	}

	if !waitUntilPoller(3*time.Second, func() bool { return runCtx.Err() != nil }) {
		t.Fatal("runCtx was not canceled within 3s of the leadership lock being killed out-of-band - heartbeat did not detect the loss")
	}

	close(provider.unblock)

	select {
	case <-pollDone:
	case <-time.After(3 * time.Second):
		t.Fatal("pollOnce() did not return within 3s of being unblocked after leadership loss")
	}
	<-heartbeatDone

	// svc1 completed before the abort: its write is expected to have
	// landed (HA-05 is about not completing writes *after* losing the
	// lock, not full-cycle atomicity).
	assertStatusIntervalCount(t, pool, svc1.ID, 1)
	assertCurrentStatus(t, pool, services, svc1.ID, "operational")

	// svc2's fetch was in flight at the moment of the abort and only
	// unblocked after runCtx was already canceled - it must not have
	// written a status interval or updated current_status.
	assertStatusIntervalCount(t, pool, svc2.ID, 0)
	assertCurrentStatus(t, pool, services, svc2.ID, "not_configured")

	// svc3 is sequenced after svc2 in pollOnce's loop and is only ever
	// reached (if at all) with runCtx already canceled - it must not have
	// written anything either.
	assertStatusIntervalCount(t, pool, svc3.ID, 0)
	assertCurrentStatus(t, pool, services, svc3.ID, "not_configured")
}

func assertStatusIntervalCount(t *testing.T, pool *db.Pool, serviceID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM status_intervals WHERE service_id = $1", serviceID,
	).Scan(&count); err != nil {
		t.Fatalf("counting status_intervals for %s returned unexpected error: %v", serviceID, err)
	}
	if count != want {
		t.Errorf("status_intervals rows for service %s = %d, want %d", serviceID, count, want)
	}
}

func assertCurrentStatus(t *testing.T, pool *db.Pool, services *db.ServiceRepository, serviceID, want string) {
	t.Helper()
	all, err := services.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	for i := range all {
		if all[i].ID == serviceID {
			if all[i].CurrentStatus != want {
				t.Errorf("service %s CurrentStatus = %q, want %q", serviceID, all[i].CurrentStatus, want)
			}
			return
		}
	}
	t.Fatalf("service %s not found", serviceID)
}
