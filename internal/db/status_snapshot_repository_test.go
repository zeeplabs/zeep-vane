//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

func newStatusSnapshotRepositoryTestPool(t *testing.T) *Pool {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createStatusSnapshotRepositoryTestService(t *testing.T, pool *Pool, name string) string {
	t.Helper()
	ctx := context.Background()
	var serviceID string
	row := pool.QueryRow(ctx, "INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", name, "slo-list-recent-test")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("insert service returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })
	return serviceID
}

func insertStatusSnapshotAt(t *testing.T, pool *Pool, serviceID, status string, fetchedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_snapshots (service_id, status, error_budget_remaining, fetched_at) VALUES ($1, $2, $3, $4)",
		serviceID, status, 0.9, fetchedAt,
	); err != nil {
		t.Fatalf("insert status snapshot returned unexpected error: %v", err)
	}
}

// TestListRecentByServices_RowWithinWindow_Returned covers UPT-01/UPT-07: a
// snapshot at or after the since cutoff must be returned.
func TestListRecentByServices_RowWithinWindow_Returned(t *testing.T) {
	pool := newStatusSnapshotRepositoryTestPool(t)
	repo := NewStatusSnapshotRepository(pool)
	serviceID := createStatusSnapshotRepositoryTestService(t, pool, "list-recent-within-window-test")

	fetchedAt := time.Now().Add(-2 * time.Hour)
	insertStatusSnapshotAt(t, pool, serviceID, "degraded", fetchedAt)

	got, err := repo.ListRecentByServices(context.Background(), []string{serviceID}, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListRecentByServices() returned unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].ServiceID != serviceID || got[0].Status != "degraded" {
		t.Errorf("got[0] = {ServiceID: %q, Status: %q}, want {%q, %q}", got[0].ServiceID, got[0].Status, serviceID, "degraded")
	}
}

// TestListRecentByServices_RowOutsideWindow_Excluded covers UPT-01/UPT-07: a
// snapshot older than the since cutoff must never leak into the result.
func TestListRecentByServices_RowOutsideWindow_Excluded(t *testing.T) {
	pool := newStatusSnapshotRepositoryTestPool(t)
	repo := NewStatusSnapshotRepository(pool)
	serviceID := createStatusSnapshotRepositoryTestService(t, pool, "list-recent-outside-window-test")

	insertStatusSnapshotAt(t, pool, serviceID, "outage", time.Now().Add(-72*time.Hour))

	got, err := repo.ListRecentByServices(context.Background(), []string{serviceID}, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListRecentByServices() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 (snapshot is outside the window)", len(got))
	}
}

// TestListRecentByServices_EmptyServiceIDs_ReturnsEmptyNotError covers T1's
// done-when: an empty serviceIDs slice must short-circuit to an empty
// result, never an error and never a full-table scan.
func TestListRecentByServices_EmptyServiceIDs_ReturnsEmptyNotError(t *testing.T) {
	pool := newStatusSnapshotRepositoryTestPool(t)
	repo := NewStatusSnapshotRepository(pool)

	got, err := repo.ListRecentByServices(context.Background(), []string{}, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListRecentByServices() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

// TestListRecentByServices_NoMatchingServiceID_ReturnsEmptyNotError covers
// the case where snapshots exist but for a different service - the filter
// must exclude them, not error.
func TestListRecentByServices_NoMatchingServiceID_ReturnsEmptyNotError(t *testing.T) {
	pool := newStatusSnapshotRepositoryTestPool(t)
	repo := NewStatusSnapshotRepository(pool)
	serviceID := createStatusSnapshotRepositoryTestService(t, pool, "list-recent-no-match-test")
	insertStatusSnapshotAt(t, pool, serviceID, "operational", time.Now())

	got, err := repo.ListRecentByServices(context.Background(), []string{"00000000-0000-0000-0000-000000000000"}, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListRecentByServices() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

// TestListRecentByServices_MultipleServices_OrderedByServiceThenFetchedAt
// covers the repository's documented ordering contract, which
// history.BuildHourly's caller relies on for grouping by service.
func TestListRecentByServices_MultipleServices_OrderedByServiceThenFetchedAt(t *testing.T) {
	pool := newStatusSnapshotRepositoryTestPool(t)
	repo := NewStatusSnapshotRepository(pool)
	serviceA := createStatusSnapshotRepositoryTestService(t, pool, "list-recent-multi-a-test")
	serviceB := createStatusSnapshotRepositoryTestService(t, pool, "list-recent-multi-b-test")

	now := time.Now()
	insertStatusSnapshotAt(t, pool, serviceA, "operational", now.Add(-3*time.Hour))
	insertStatusSnapshotAt(t, pool, serviceA, "outage", now.Add(-1*time.Hour))
	insertStatusSnapshotAt(t, pool, serviceB, "degraded", now.Add(-2*time.Hour))

	got, err := repo.ListRecentByServices(context.Background(), []string{serviceA, serviceB}, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ListRecentByServices() returned unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].ServiceID < got[i-1].ServiceID {
			t.Fatalf("rows not grouped by service_id at index %d", i)
		}
	}
	if got[0].ServiceID == serviceA && got[1].ServiceID == serviceA {
		if !got[1].FetchedAt.After(got[0].FetchedAt) {
			t.Errorf("service A rows not ordered by fetched_at ascending: %v then %v", got[0].FetchedAt, got[1].FetchedAt)
		}
	}
}
