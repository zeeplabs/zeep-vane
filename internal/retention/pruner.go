// Package retention prunes closed status_intervals rows older than a
// configured retention window, on its own ticker independent of the
// per-service poller ticker (design.md: pruning and polling are unrelated
// responsibilities).
package retention

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// pruneLeaderLockKey guards the retention prune so exactly one replica
// deletes on a given tick (HA: the admin API and every background job run on
// every replica, but the pruner is a single-writer job). Distinct from
// db.PollerLeaderLockKey (727200001) and the digest scheduler's key
// (727200002) within pglock's reserved 727200000-727299999 block.
const pruneLeaderLockKey int64 = 727200003

// intervalDeleter is the subset of *db.StatusIntervalRepository the Pruner
// depends on to delete old, closed interval rows.
type intervalDeleter interface {
	DeleteClosedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Pruner periodically deletes status_intervals rows closed before
// now-retention, on its own tick ticker (SHU-16/17). Open intervals
// (ends_at IS NULL) are never touched by this - the retention cutoff only
// ever applies to ends_at.
type Pruner struct {
	intervals intervalDeleter
	// dsn opens the dedicated connection the per-tick leadership lock is
	// held on (pglock requires a non-pooled connection, see its doc).
	dsn       string
	tick      time.Duration
	retention time.Duration
	logger    *zap.Logger
}

// NewPruner builds a Pruner that deletes closed intervals older than
// retention, checking every tick. dsn is the raw Postgres connection string
// used for the per-tick leadership lock (pglock's contract, not the pool).
func NewPruner(intervals intervalDeleter, dsn string, tick, retention time.Duration, logger *zap.Logger) *Pruner {
	return &Pruner{
		intervals: intervals,
		dsn:       dsn,
		tick:      tick,
		retention: retention,
		logger:    logger,
	}
}

// Run ticks every p.tick, deleting closed intervals older than p.retention
// each cycle, until ctx is canceled - at which point it returns, letting
// the caller (cmd/vane serve) shut down cleanly without leaking the
// goroutine. A failed delete is logged and does not stop the loop; the
// next scheduled tick retries (SHU-20).
func (p *Pruner) Run(ctx context.Context) {
	ticker := time.NewTicker(p.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runCycle(ctx)
		}
	}
}

// runCycle takes the prune leadership lock for one tick, then prunes. With
// more than one replica sharing a database only the lock holder deletes;
// the others skip the tick (expected steady state, not an error). A failed
// lock acquisition (e.g. the database is unreachable) is logged and skips
// the tick, so the next scheduled tick retries rather than deleting without
// having proven single-writer ownership.
func (p *Pruner) runCycle(ctx context.Context) {
	handle, acquired, err := pglock.TryAcquire(ctx, p.dsn, pruneLeaderLockKey)
	if err != nil {
		p.logger.Error("retention: failed to acquire leader lock", zap.Error(err))
		return
	}
	if !acquired {
		return
	}
	defer func() {
		if err := handle.Release(ctx); err != nil {
			p.logger.Error("retention: failed to release leader lock", zap.Error(err))
		}
	}()

	p.prune(ctx)
}

// prune deletes every closed interval older than now-p.retention.
func (p *Pruner) prune(ctx context.Context) {
	cutoff := time.Now().Add(-p.retention)

	deleted, err := p.intervals.DeleteClosedBefore(ctx, cutoff)
	if err != nil {
		p.logger.Error("retention: failed to delete closed status intervals", zap.Error(err))
		return
	}

	if deleted > 0 {
		p.logger.Info("retention: pruned closed status intervals", zap.Int64("deleted", deleted))
	}
}
