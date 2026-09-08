package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// postgresBucketStore implements bucketStore against the rate_limit_buckets
// table (ha-multi-replica design.md Data Models), making IPLimiter's
// token-bucket state visible to and shared by every replica hitting the
// same Postgres database.
type postgresBucketStore struct {
	pool *db.Pool
}

func newPostgresBucketStore(pool *db.Pool) *postgresBucketStore {
	return &postgresBucketStore{pool: pool}
}

// allow refills then consumes one token from ip's bucket - refill-then-
// consume, clamped at [0, burst], mirroring golang.org/x/time/rate.Limiter's
// own floor/ceiling behavior so externally observable rate-limit behavior
// is unchanged from before this feature (HA-09's byte-for-byte parity
// requirement). A brand-new IP starts with a full bucket (burst tokens)
// minus the one consumed by this call, exactly like
// rate.NewLimiter(r, burst).Allow()'s first call would.
//
// The read (SELECT ... FOR UPDATE) and write happen inside one
// transaction: a single INSERT ... ON CONFLICT DO UPDATE ... RETURNING
// statement cannot correctly report whether *this* call consumed a token,
// because Postgres's RETURNING clause always reflects the row's final
// (post-update) state, not the pre-update value the "allowed" decision
// must be based on - a first attempt at a single-statement UPSERT here
// silently miscounted (RETURNING recomputed the formula against the
// already-decremented new tokens value, undercounting one request per
// bucket). The row lock this transaction takes also serializes concurrent
// requests for the same IP across replicas, which the single-statement
// version would not have needed but this version does.
func (s *postgresBucketStore) allow(ctx context.Context, ip string, burst int, refillPerSec float64) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("ratelimit: begin tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	// lockTimeout bounds how long this call can sit blocked on the row lock
	// below. Without it, a burst of concurrent requests hammering the same
	// IP each pin a pool connection (MaxConns = 4, internal/db/pool.go)
	// waiting on the same lock indefinitely - on a shared pool this starves
	// every other query in the process, not just rate-limiting, for as long
	// as the offending IP keeps sending requests. Bounding the wait doesn't
	// change the fail-open policy below (HA-10, a deliberate decision -
	// flagged, not silently changed here); it only limits how long any one
	// caller can occupy a connection before that policy kicks in.
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '3s'"); err != nil {
		return false, fmt.Errorf("ratelimit: set lock_timeout failed: %w", err)
	}

	now := time.Now()

	// Ensure a row exists before locking it. Without this, concurrent
	// first-ever requests for the same brand-new IP each hit ErrNoRows
	// below with nothing to lock, independently compute a fresh full
	// bucket, and each blindly overwrite via the final UPDATE - none of
	// them actually serialize against each other, so a burst of concurrent
	// first requests for a new IP could all be "allowed" past the
	// configured burst ceiling. Seeding the row first (a no-op via ON
	// CONFLICT if one already exists) guarantees the SELECT ... FOR UPDATE
	// below always has a real row to lock, so even the very first requests
	// for a new IP are properly serialized.
	if _, err := tx.Exec(ctx, `
INSERT INTO rate_limit_buckets (ip, tokens, last_refill)
VALUES ($1, $2, $3)
ON CONFLICT (ip) DO NOTHING`, ip, float64(burst), now); err != nil {
		return false, fmt.Errorf("ratelimit: seed insert failed: %w", err)
	}

	var tokens float64
	var lastRefill time.Time
	if err := tx.QueryRow(ctx, "SELECT tokens, last_refill FROM rate_limit_buckets WHERE ip = $1 FOR UPDATE", ip).Scan(&tokens, &lastRefill); err != nil {
		return false, fmt.Errorf("ratelimit: select failed: %w", err)
	}

	// elapsed is 0 when this call's own seed INSERT just created the row
	// (lastRefill == now), so a brand-new IP correctly starts with a full
	// bucket instead of an extra refill on top of it.
	elapsed := now.Sub(lastRefill).Seconds()
	if elapsed > 0 {
		tokens += elapsed * refillPerSec
		if tokens > float64(burst) {
			tokens = float64(burst)
		}
	}

	allowed := tokens >= 1
	if allowed {
		tokens--
	}
	if tokens < 0 {
		tokens = 0
	}

	if _, err := tx.Exec(ctx, "UPDATE rate_limit_buckets SET tokens = $2, last_refill = $3 WHERE ip = $1", ip, tokens, now); err != nil {
		return false, fmt.Errorf("ratelimit: update failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("ratelimit: commit failed: %w", err)
	}

	return allowed, nil
}

// cleanup deletes buckets idle longer than idleTTL, bounding the table's
// growth the same way the previous in-memory map's sweep did (HA-11).
func (s *postgresBucketStore) cleanup(ctx context.Context, idleTTL time.Duration) error {
	cutoff := time.Now().Add(-idleTTL)
	if _, err := s.pool.Exec(ctx, "DELETE FROM rate_limit_buckets WHERE last_refill < $1", cutoff); err != nil {
		return fmt.Errorf("ratelimit: cleanup delete failed: %w", err)
	}
	return nil
}
