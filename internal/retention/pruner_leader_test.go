//go:build integration

package retention

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// countingDeleter records how many times DeleteClosedBefore was invoked, so
// the leader-gate tests can prove a skipped tick did not delete.
type countingDeleter struct {
	calls int32
}

func (c *countingDeleter) DeleteClosedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	atomic.AddInt32(&c.calls, 1)
	return 0, nil
}

// TestPruner_RunCycle_LockHeldElsewhere_SkipsPrune covers the leader gate:
// when another session holds the prune lock, runCycle must skip the tick
// without deleting; once the lock is released, the next cycle prunes.
func TestPruner_RunCycle_LockHeldElsewhere_SkipsPrune(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	handle, acquired, err := pglock.TryAcquire(ctx, dsn, pruneLeaderLockKey)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("TryAcquire() = false, want the test to hold the prune lock")
	}

	deleter := &countingDeleter{}
	pruner := NewPruner(deleter, dsn, time.Hour, 35*24*time.Hour, zap.NewNop())

	pruner.runCycle(ctx)
	if calls := atomic.LoadInt32(&deleter.calls); calls != 0 {
		t.Fatalf("DeleteClosedBefore calls = %d, want 0 while another session holds the lock", calls)
	}

	if err := handle.Release(ctx); err != nil {
		t.Fatalf("Release() returned unexpected error: %v", err)
	}

	pruner.runCycle(ctx)
	if calls := atomic.LoadInt32(&deleter.calls); calls != 1 {
		t.Fatalf("DeleteClosedBefore calls = %d, want 1 after the lock is released", calls)
	}
}

// TestPruner_RunCycle_ReleasesLockAfterPrune covers the release half: once
// runCycle returns, the lock is free for another replica to acquire.
func TestPruner_RunCycle_ReleasesLockAfterPrune(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	deleter := &countingDeleter{}
	pruner := NewPruner(deleter, dsn, time.Hour, 35*24*time.Hour, zap.NewNop())

	pruner.runCycle(ctx)
	if calls := atomic.LoadInt32(&deleter.calls); calls != 1 {
		t.Fatalf("DeleteClosedBefore calls = %d, want 1", calls)
	}

	handle, acquired, err := pglock.TryAcquire(ctx, dsn, pruneLeaderLockKey)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("TryAcquire() = false after runCycle, want the lock released")
	}
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("Release() returned unexpected error: %v", err)
	}
}

// TestPruner_RunCycle_LockAcquireError_SkipsPrune covers the failure path:
// an unreachable database logs and skips the tick rather than deleting
// without having proven single-writer ownership.
func TestPruner_RunCycle_LockAcquireError_SkipsPrune(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	deleter := &countingDeleter{}
	// Port 1 is never listening, so pgx.Connect fails fast.
	pruner := NewPruner(deleter, "postgres://vane:vane@127.0.0.1:1/none?sslmode=disable", time.Hour, 35*24*time.Hour, zap.NewNop())

	pruner.runCycle(ctx)
	if calls := atomic.LoadInt32(&deleter.calls); calls != 0 {
		t.Fatalf("DeleteClosedBefore calls = %d, want 0 when the lock cannot be acquired", calls)
	}
}
