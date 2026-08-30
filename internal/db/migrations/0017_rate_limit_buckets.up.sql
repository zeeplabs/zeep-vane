-- rate_limit_buckets backs internal/ratelimit.IPLimiter's per-IP
-- token-bucket rate limiting across replicas (ha-multi-replica HA-08).
-- One row per client IP; tokens/last_refill let allow() re-derive the
-- exact refill-then-consume formula golang.org/x/time/rate.Limiter already
-- computes in memory, now shared across every replica hitting the same
-- Postgres database instead of living in one process's map.
CREATE TABLE rate_limit_buckets (
    ip          TEXT PRIMARY KEY,
    tokens      DOUBLE PRECISION NOT NULL,
    last_refill TIMESTAMPTZ NOT NULL DEFAULT now()
);
