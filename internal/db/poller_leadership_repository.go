package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PollerLeaderLockKey identifies the Postgres advisory lock guarding poller
// leadership across replicas (ha-multi-replica AD-013). Exported here -
// rather than kept internal to internal/cli, the actual lock holder - so
// PollerLeadershipRepository can query pg_locks for the same key without an
// import cycle: internal/cli already imports internal/db, not the other way
// around.
const PollerLeaderLockKey int64 = 727200001

// PollerLeader is the replica currently holding PollerLeaderLockKey.
type PollerLeader struct {
	ApplicationName string
	BackendStart    time.Time
}

// PollerLeadershipRepository reads live poller-leadership state directly
// from Postgres's own lock/session catalogs - no new table, per
// poller-status-real-state's decision to never persist leadership state
// separately (POLLST-01/05).
type PollerLeadershipRepository struct {
	pool *Pool
}

// NewPollerLeadershipRepository builds a PollerLeadershipRepository backed
// by pool.
func NewPollerLeadershipRepository(pool *Pool) *PollerLeadershipRepository {
	return &PollerLeadershipRepository{pool: pool}
}

// CurrentLeader returns the replica currently holding PollerLeaderLockKey,
// or (nil, nil) if nobody currently holds it (POLLST-04) - a legitimate,
// momentary state (e.g. between a lock release and the next acquisition),
// not an error. PollerLeaderLockKey fits in 32 bits, so
// pg_advisory_lock(bigint) stores it as classid=0, objid=<key>, objsubid=1
// in pg_locks - the same shape internal/cli's own
// killPollerLeaderBackend test helper already relies on.
func (r *PollerLeadershipRepository) CurrentLeader(ctx context.Context) (*PollerLeader, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT a.application_name, a.backend_start
		 FROM pg_locks l
		 JOIN pg_stat_activity a ON a.pid = l.pid
		 WHERE l.locktype = 'advisory' AND l.classid = 0 AND l.objid = $1 AND l.objsubid = 1 AND l.granted = true
		 LIMIT 1`,
		PollerLeaderLockKey,
	)

	var leader PollerLeader
	if err := row.Scan(&leader.ApplicationName, &leader.BackendStart); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: failed to query poller leadership: %w", err)
	}

	return &leader, nil
}
