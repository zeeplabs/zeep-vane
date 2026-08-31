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

// allow atomically refills then consumes one token from ip's bucket in a
// single UPSERT statement - refill-then-consume, clamped at [0, burst],
// mirroring golang.org/x/time/rate.Limiter's own floor/ceiling behavior so
// externally observable rate-limit behavior is unchanged from before this
// feature (HA-09's byte-for-byte parity requirement). A brand-new IP starts
// with a full bucket (burst tokens) minus the one consumed by this call,
// exactly like rate.NewLimiter(r, burst).Allow()'s first call would.
func (s *postgresBucketStore) allow(ctx context.Context, ip string, burst int, refillPerSec float64) (bool, error) {
	const query = `
INSERT INTO rate_limit_buckets (ip, tokens, last_refill)
VALUES ($1, $2 - 1, now())
ON CONFLICT (ip) DO UPDATE SET
    tokens = GREATEST(
        0,
        LEAST($2::double precision, rate_limit_buckets.tokens
                  + $3 * EXTRACT(EPOCH FROM (now() - rate_limit_buckets.last_refill)))
        - CASE WHEN LEAST($2::double precision, rate_limit_buckets.tokens
                             + $3 * EXTRACT(EPOCH FROM (now() - rate_limit_buckets.last_refill))) >= 1
               THEN 1 ELSE 0 END
    ),
    last_refill = now()
RETURNING
    (xmax = 0) OR LEAST($2::double precision, rate_limit_buckets.tokens
             + $3 * EXTRACT(EPOCH FROM (now() - rate_limit_buckets.last_refill))) >= 1 AS allowed`

	var allowed bool
	if err := s.pool.QueryRow(ctx, query, ip, float64(burst), refillPerSec).Scan(&allowed); err != nil {
		return false, fmt.Errorf("ratelimit: allow query failed: %w", err)
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
