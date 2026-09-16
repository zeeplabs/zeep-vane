//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPollerLeadershipRepository_CurrentLeader_NoLeader_ReturnsNilNil covers
// POLLST-04: when nobody holds PollerLeaderLockKey, CurrentLeader reports a
// legitimate nil state, not an error.
func TestPollerLeadershipRepository_CurrentLeader_NoLeader_ReturnsNilNil(t *testing.T) {
	pool, _ := newTenantScopedPool(t)
	repo := NewPollerLeadershipRepository(pool)

	leader, err := repo.CurrentLeader(context.Background())
	if err != nil {
		t.Fatalf("CurrentLeader() returned unexpected error: %v", err)
	}
	if leader != nil {
		t.Errorf("leader = %+v, want nil (nobody holds the lock)", leader)
	}
}

// TestPollerLeadershipRepository_CurrentLeader_LockHeld_ReturnsApplicationNameAndBackendStart
// covers POLLST-01: a session holding PollerLeaderLockKey with a given
// application_name is reported by application_name and backend_start.
func TestPollerLeadershipRepository_CurrentLeader_LockHeld_ReturnsApplicationNameAndBackendStart(t *testing.T) {
	pool, _ := newTenantScopedPool(t)
	repo := NewPollerLeadershipRepository(pool)
	dsn := testDatabaseURL(t)

	const applicationName = "poller-leadership-test-replica"
	conn, err := pgx.ConnectConfig(context.Background(), mustParseConfigWithApplicationName(t, dsn, applicationName))
	if err != nil {
		t.Fatalf("pgx.ConnectConfig() returned unexpected error: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	var acquired bool
	if err := conn.QueryRow(context.Background(), "SELECT pg_try_advisory_lock($1)", PollerLeaderLockKey).Scan(&acquired); err != nil {
		t.Fatalf("pg_try_advisory_lock() returned unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("pg_try_advisory_lock() = false, want true (nothing else holds this key)")
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", PollerLeaderLockKey)
	})

	leader, err := repo.CurrentLeader(context.Background())
	if err != nil {
		t.Fatalf("CurrentLeader() returned unexpected error: %v", err)
	}
	if leader == nil {
		t.Fatal("leader = nil, want a non-nil PollerLeader")
	}
	if leader.ApplicationName != applicationName {
		t.Errorf("ApplicationName = %q, want %q", leader.ApplicationName, applicationName)
	}
	if leader.BackendStart.IsZero() {
		t.Error("BackendStart is zero, want a real timestamp")
	}
}

// TestPollerLeadershipRepository_CurrentLeader_WaitingRequestIgnored covers
// the `granted = true` filter: a second session blocked waiting on the same
// key (granted = false in pg_locks) must never be reported as the leader -
// only the session that actually holds the lock.
func TestPollerLeadershipRepository_CurrentLeader_WaitingRequestIgnored(t *testing.T) {
	pool, _ := newTenantScopedPool(t)
	repo := NewPollerLeadershipRepository(pool)
	dsn := testDatabaseURL(t)

	const holderName = "poller-leadership-test-holder"
	const waiterName = "poller-leadership-test-waiter"

	holder, err := pgx.ConnectConfig(context.Background(), mustParseConfigWithApplicationName(t, dsn, holderName))
	if err != nil {
		t.Fatalf("holder pgx.ConnectConfig() returned unexpected error: %v", err)
	}
	defer func() { _ = holder.Close(context.Background()) }()
	var acquired bool
	if err := holder.QueryRow(context.Background(), "SELECT pg_try_advisory_lock($1)", PollerLeaderLockKey).Scan(&acquired); err != nil {
		t.Fatalf("pg_try_advisory_lock() returned unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("pg_try_advisory_lock() = false, want true (nothing else holds this key)")
	}
	t.Cleanup(func() { _, _ = holder.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", PollerLeaderLockKey) })

	waiter, err := pgx.ConnectConfig(context.Background(), mustParseConfigWithApplicationName(t, dsn, waiterName))
	if err != nil {
		t.Fatalf("waiter pgx.ConnectConfig() returned unexpected error: %v", err)
	}
	defer func() { _ = waiter.Close(context.Background()) }()

	waiting := make(chan struct{})
	go func() {
		close(waiting)
		// Blocks until holder releases - exercised only to put a
		// granted=false row in pg_locks for this key; the test never waits
		// for this to return.
		_, _ = waiter.Exec(context.Background(), "SELECT pg_advisory_lock($1)", PollerLeaderLockKey)
	}()
	<-waiting

	// Give the waiter a moment to actually register its blocked request in
	// pg_locks before asserting - there is no synchronous signal for "now
	// waiting", only for "goroutine started".
	deadline := time.Now().Add(2 * time.Second)
	var waiterRegistered bool
	for time.Now().Before(deadline) {
		var count int
		row := pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
			 WHERE l.locktype = 'advisory' AND l.classid = 0 AND l.objid = $1 AND l.objsubid = 1
			   AND l.granted = false AND a.application_name = $2`,
			PollerLeaderLockKey, waiterName,
		)
		if err := row.Scan(&count); err == nil && count == 1 {
			waiterRegistered = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiterRegistered {
		t.Fatal("waiter's blocked lock request never appeared in pg_locks with granted=false")
	}

	leader, err := repo.CurrentLeader(context.Background())
	if err != nil {
		t.Fatalf("CurrentLeader() returned unexpected error: %v", err)
	}
	if leader == nil {
		t.Fatal("leader = nil, want the holder")
	}
	if leader.ApplicationName != holderName {
		t.Errorf("ApplicationName = %q, want %q (the waiting request must never be reported)", leader.ApplicationName, holderName)
	}
}

// mustParseConfigWithApplicationName parses dsn and sets application_name,
// so the dedicated test connection is identifiable in pg_stat_activity the
// same way PollerManager's own dsnWithApplicationName makes its connections
// identifiable.
func mustParseConfigWithApplicationName(t *testing.T, dsn, applicationName string) *pgx.ConnConfig {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgx.ParseConfig() returned unexpected error: %v", err)
	}
	cfg.RuntimeParams["application_name"] = applicationName
	return cfg
}
