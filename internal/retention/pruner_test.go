//go:build integration

package retention

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func createTestService(t *testing.T, pool *db.Pool, name string) string {
	t.Helper()
	ctx := context.Background()
	var serviceID string
	row := pool.QueryRow(ctx, "INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", name, "slo-pruner-test")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("insert service returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })
	return serviceID
}

// TestPruner_Run_TickDeletesClosedIntervalsOlderThan35Days confirms a tick
// deletes intervals closed before now-35d and leaves the open interval
// (regardless of age) untouched (SHU-16..19).
func TestPruner_Run_TickDeletesClosedIntervalsOlderThan35Days(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	serviceID := createTestService(t, pool, "pruner-tick-deletes")

	now := time.Now().UTC()
	oldEndsAt := now.Add(-40 * 24 * time.Hour)
	if _, err := pool.Exec(ctx,
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at, ends_at) VALUES ($1, $2, $3, $4, $4, $5)",
		serviceID, "operational", 90.0, oldEndsAt.Add(-time.Hour), oldEndsAt,
	); err != nil {
		t.Fatalf("insert old closed interval failed: %v", err)
	}

	veryOldOpenStart := now.Add(-100 * 24 * time.Hour)
	if _, err := pool.Exec(ctx,
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at) VALUES ($1, $2, $3, $4, $4)",
		serviceID, "outage", 10.0, veryOldOpenStart,
	); err != nil {
		t.Fatalf("insert open interval failed: %v", err)
	}

	intervals := db.NewStatusIntervalRepository(pool)
	pruner := NewPruner(intervals, 20*time.Millisecond, 35*24*time.Hour, zap.NewNop())

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pruner.Run(runCtx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	var remaining int
	for time.Now().Before(deadline) {
		row := pool.QueryRow(ctx, "SELECT count(*) FROM status_intervals WHERE service_id = $1", serviceID)
		if err := row.Scan(&remaining); err != nil {
			t.Fatalf("Scan() returned unexpected error: %v", err)
		}
		if remaining == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if remaining != 1 {
		t.Fatalf("remaining intervals = %d, want 1 (old closed row deleted, open row kept)", remaining)
	}

	var openCount int
	row := pool.QueryRow(ctx, "SELECT count(*) FROM status_intervals WHERE service_id = $1 AND ends_at IS NULL", serviceID)
	if err := row.Scan(&openCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if openCount != 1 {
		t.Errorf("open interval count = %d, want 1 (never deleted regardless of age)", openCount)
	}
}

// TestPruner_Run_ReturnsPromptlyOnContextCancel confirms the ticker loop
// exits cleanly when its context is canceled mid-tick-wait, matching
// Poller.Run's shutdown contract.
func TestPruner_Run_ReturnsPromptlyOnContextCancel(t *testing.T) {
	pool := newTestPool(t)
	intervals := db.NewStatusIntervalRepository(pool)
	pruner := NewPruner(intervals, time.Hour, 35*24*time.Hour, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pruner.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

// fakeFailingDeleter fails its first N calls, then delegates to a real
// DeleteClosedBefore-shaped success, letting the test confirm a failing
// tick is logged (not crashing) and a subsequent tick still runs.
type fakeFailingDeleter struct {
	failCount int32
	failTimes int32
	calls     int32
}

func (f *fakeFailingDeleter) DeleteClosedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	atomic.AddInt32(&f.calls, 1)
	if atomic.LoadInt32(&f.failCount) < f.failTimes {
		atomic.AddInt32(&f.failCount, 1)
		return 0, errors.New("simulated delete failure")
	}
	return 0, nil
}

// TestPruner_Run_DeleteErrorIsLoggedAndLoopContinues forces the first tick's
// DeleteClosedBefore call to fail and confirms Run's loop does not stop -
// a later tick still calls DeleteClosedBefore again (SHU-20).
func TestPruner_Run_DeleteErrorIsLoggedAndLoopContinues(t *testing.T) {
	deleter := &fakeFailingDeleter{failTimes: 1}
	pruner := NewPruner(deleter, 20*time.Millisecond, 35*24*time.Hour, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pruner.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&deleter.calls) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if calls := atomic.LoadInt32(&deleter.calls); calls < 2 {
		t.Fatalf("DeleteClosedBefore call count = %d, want >= 2 (a failing tick must not stop the loop)", calls)
	}
}
