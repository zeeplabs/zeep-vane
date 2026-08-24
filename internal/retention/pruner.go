// Package retention prunes closed status_intervals rows older than a
// configured retention window, on its own ticker independent of the
// per-service poller ticker (design.md: pruning and polling are unrelated
// responsibilities).
package retention

import (
	"context"
	"time"

	"go.uber.org/zap"
)

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
	tick      time.Duration
	retention time.Duration
	logger    *zap.Logger
}

// NewPruner builds a Pruner that deletes closed intervals older than
// retention, checking every tick.
func NewPruner(intervals intervalDeleter, tick, retention time.Duration, logger *zap.Logger) *Pruner {
	return &Pruner{
		intervals: intervals,
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
			p.prune(ctx)
		}
	}
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
