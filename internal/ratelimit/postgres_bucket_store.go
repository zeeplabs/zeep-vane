package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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

	var tokens float64
	var lastRefill time.Time
	err = tx.QueryRow(ctx, "SELECT tokens, last_refill FROM rate_limit_buckets WHERE ip = $1 FOR UPDATE", ip).Scan(&tokens, &lastRefill)

	now := time.Now()
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Brand-new IP: starts with a full bucket, as if it had always
		// been refilling up to now.
		tokens = float64(burst)
	case err != nil:
		return false, fmt.Errorf("ratelimit: select failed: %w", err)
	default:
		elapsed := now.Sub(lastRefill).Seconds()
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

	if _, err := tx.Exec(ctx, `
INSERT INTO rate_limit_buckets (ip, tokens, last_refill)
VALUES ($1, $2, $3)
ON CONFLICT (ip) DO UPDATE SET tokens = $2, last_refill = $3`, ip, tokens, now); err != nil {
		return false, fmt.Errorf("ratelimit: upsert failed: %w", err)
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
