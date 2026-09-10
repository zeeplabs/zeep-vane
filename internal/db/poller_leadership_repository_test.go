//go:build integration

package db

import (
	"context"
	"testing"

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
