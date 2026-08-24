//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newStatusIntervalRepositoryTestPool(t *testing.T) *Pool {
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

func createStatusIntervalRepositoryTestService(t *testing.T, pool *Pool, name string) string {
	t.Helper()
	ctx := context.Background()
	var serviceID string
	row := pool.QueryRow(ctx, "INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", name, "slo-status-interval-test")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("insert service returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })
	return serviceID
}

func selectIntervalsByService(t *testing.T, pool *Pool, serviceID string) []StatusInterval {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT id, service_id, status, error_budget_remaining, starts_at, last_seen_at, ends_at FROM status_intervals WHERE service_id = $1 ORDER BY starts_at ASC",
		serviceID,
	)
	if err != nil {
		t.Fatalf("query status intervals returned unexpected error: %v", err)
	}
	defer rows.Close()

	var intervals []StatusInterval
	for rows.Next() {
		var si StatusInterval
		if err := rows.Scan(&si.ID, &si.ServiceID, &si.Status, &si.ErrorBudgetRemaining, &si.StartsAt, &si.LastSeenAt, &si.EndsAt); err != nil {
			t.Fatalf("scan status interval returned unexpected error: %v", err)
		}
		intervals = append(intervals, si)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate status intervals returned unexpected error: %v", err)
	}
	return intervals
}

func TestOpenOrExtend_FirstObservation_InsertsOneOpenInterval(t *testing.T) {
	pool := newStatusIntervalRepositoryTestPool(t)
	serviceID := createStatusIntervalRepositoryTestService(t, pool, "open-or-extend-first-observation")
	repo := NewStatusIntervalRepository(pool)

	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.OpenOrExtend(context.Background(), serviceID, "operational", 95.0, at); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}

	intervals := selectIntervalsByService(t, pool, serviceID)
	if len(intervals) != 1 {
		t.Fatalf("intervals count = %d, want 1", len(intervals))
	}
	got := intervals[0]
	if got.EndsAt != nil {
		t.Errorf("EndsAt = %v, want nil", got.EndsAt)
	}
	if !got.StartsAt.Equal(at) {
		t.Errorf("StartsAt = %v, want %v", got.StartsAt, at)
	}
	if got.Status != "operational" {
		t.Errorf("Status = %q, want %q", got.Status, "operational")
	}
}

func TestOpenOrExtend_SameStatus_UpdatesOpenIntervalInPlace(t *testing.T) {
	pool := newStatusIntervalRepositoryTestPool(t)
	serviceID := createStatusIntervalRepositoryTestService(t, pool, "open-or-extend-same-status")
	repo := NewStatusIntervalRepository(pool)

	first := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.OpenOrExtend(context.Background(), serviceID, "operational", 95.0, first); err != nil {
		t.Fatalf("first OpenOrExtend() returned unexpected error: %v", err)
	}

	second := first.Add(1 * time.Minute)
	if err := repo.OpenOrExtend(context.Background(), serviceID, "operational", 90.0, second); err != nil {
		t.Fatalf("second OpenOrExtend() returned unexpected error: %v", err)
	}

	intervals := selectIntervalsByService(t, pool, serviceID)
	if len(intervals) != 1 {
		t.Fatalf("intervals count = %d, want 1 (no new row on same status)", len(intervals))
	}
	got := intervals[0]
	if got.EndsAt != nil {
		t.Errorf("EndsAt = %v, want nil", got.EndsAt)
	}
	if !got.StartsAt.Equal(first) {
		t.Errorf("StartsAt = %v, want unchanged %v", got.StartsAt, first)
	}
	if !got.LastSeenAt.Equal(second) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, second)
	}
	if got.ErrorBudgetRemaining != 90.0 {
		t.Errorf("ErrorBudgetRemaining = %v, want 90.0", got.ErrorBudgetRemaining)
	}
}

func TestOpenOrExtend_DifferentStatus_ClosesOldRowAndOpensNew(t *testing.T) {
	pool := newStatusIntervalRepositoryTestPool(t)
	serviceID := createStatusIntervalRepositoryTestService(t, pool, "open-or-extend-different-status")
	repo := NewStatusIntervalRepository(pool)

	first := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.OpenOrExtend(context.Background(), serviceID, "operational", 95.0, first); err != nil {
		t.Fatalf("first OpenOrExtend() returned unexpected error: %v", err)
	}

	second := first.Add(1 * time.Minute)
	if err := repo.OpenOrExtend(context.Background(), serviceID, "outage", 40.0, second); err != nil {
		t.Fatalf("second OpenOrExtend() returned unexpected error: %v", err)
	}

	intervals := selectIntervalsByService(t, pool, serviceID)
	if len(intervals) != 2 {
		t.Fatalf("intervals count = %d, want 2 (old closed, new opened)", len(intervals))
	}

	closed, open := intervals[0], intervals[1]
	if closed.Status != "operational" {
		t.Errorf("closed.Status = %q, want %q", closed.Status, "operational")
	}
	if closed.EndsAt == nil || !closed.EndsAt.Equal(second) {
		t.Errorf("closed.EndsAt = %v, want %v", closed.EndsAt, second)
	}
	if open.Status != "outage" {
		t.Errorf("open.Status = %q, want %q", open.Status, "outage")
	}
	if open.EndsAt != nil {
		t.Errorf("open.EndsAt = %v, want nil", open.EndsAt)
	}
	if !open.StartsAt.Equal(second) {
		t.Errorf("open.StartsAt = %v, want %v", open.StartsAt, second)
	}
}

// TestOpenOrExtend_ConcurrentWriters_RaceLoserGetsError simulates the
// PollerManager.Restart race (design.md Risks & Concerns): two writers try
// to open an interval for the same, brand-new service at nearly the same
// moment. It drives the contention deterministically rather than hoping two
// bare goroutines interleave: a "holder" transaction inserts the open
// interval itself and stays open (uncommitted) while a real OpenOrExtend
// call runs concurrently in a goroutine. Because status_intervals'
// unique partial index makes the second INSERT wait on the first's
// uncommitted key, OpenOrExtend's own INSERT genuinely blocks until the
// holder commits, at which point it must fail with ErrIntervalRaceLost -
// not silently duplicate an open interval.
func TestOpenOrExtend_ConcurrentWriters_RaceLoserGetsError(t *testing.T) {
	pool := newStatusIntervalRepositoryTestPool(t)
	serviceID := createStatusIntervalRepositoryTestService(t, pool, "open-or-extend-race")
	repo := NewStatusIntervalRepository(pool)

	at := time.Now().UTC().Truncate(time.Millisecond)

	ctx := context.Background()
	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	if _, err := holderTx.Exec(ctx,
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at) VALUES ($1, $2, $3, $4, $4)",
		serviceID, "operational", 95.0, at,
	); err != nil {
		t.Fatalf("holder INSERT failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- repo.OpenOrExtend(context.Background(), serviceID, "outage", 40.0, at)
	}()

	select {
	case err := <-done:
		t.Fatalf("OpenOrExtend() returned (err=%v) while the holder transaction was still open and uncommitted - the unique index did not block it", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked behind the holder's uncommitted insert.
	}

	if err := holderTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit holder transaction: %v", err)
	}

	var raceErr error
	select {
	case raceErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OpenOrExtend() did not return after the holder transaction committed")
	}

	if !errors.Is(raceErr, ErrIntervalRaceLost) {
		t.Fatalf("OpenOrExtend() error = %v, want ErrIntervalRaceLost", raceErr)
	}

	intervals := selectIntervalsByService(t, pool, serviceID)
	if len(intervals) != 1 {
		t.Fatalf("intervals count = %d, want 1 (loser must not leave a duplicate open interval)", len(intervals))
	}
	if intervals[0].Status != "operational" {
		t.Errorf("surviving interval status = %q, want holder's %q (no lost update)", intervals[0].Status, "operational")
	}
	if intervals[0].EndsAt != nil {
		t.Errorf("surviving interval EndsAt = %v, want nil", intervals[0].EndsAt)
	}
}
