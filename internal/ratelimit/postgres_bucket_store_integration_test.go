//go:build integration

package ratelimit

import (
	"context"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// TestPostgresBucketStore_Allow_ExactOneTokenBoundary covers the exact
// tokens == 1.0 decision boundary in postgresBucketStore.allow's
// `tokens >= 1` clamp (HA-09's byte-for-byte parity with
// golang.org/x/time/rate.Limiter's inclusive-at-1 semantics). A mutation of
// that line to `tokens > 1` previously survived every existing test,
// because real wall-clock elapsed time between seeding a row and calling
// allow() always nudges the refilled `tokens` value fractionally above 1.0
// (elapsed * refillPerSec > 0), so the exact boundary was never
// deterministically exercised.
//
// This test eliminates that timing dependency entirely rather than trying
// to win a race against it: it seeds the bucket's stored tokens directly via
// SQL and calls allow() with refillPerSec = 0. With a zero refill rate,
// `tokens += elapsed * refillPerSec` always adds exactly zero regardless of
// how much real wall-clock time elapsed between the seed and the call, so
// the stored value is also, deterministically, the decision-time value -
// no shared transaction or clock-freezing trick is needed to hit the
// boundary exactly.
func TestPostgresBucketStore_Allow_ExactOneTokenBoundary(t *testing.T) {
	pool := newRateLimitTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM rate_limit_buckets") })

	store := newPostgresBucketStore(pool)
	ctx := context.Background()

	const burst = 5

	t.Run("exactly 1.0 tokens is allowed", func(t *testing.T) {
		const ip = "203.0.113.201"
		seedBucket(t, pool, ip, 1.0)

		allowed, err := store.allow(ctx, ip, burst, 0)
		if err != nil {
			t.Fatalf("allow() returned unexpected error: %v", err)
		}
		if !allowed {
			t.Error("allow() with tokens == 1.0 exactly = false, want true (inclusive threshold, tokens >= 1)")
		}

		remaining := readTokens(t, pool, ip)
		if remaining != 0 {
			t.Errorf("tokens after consuming from exactly 1.0 = %v, want 0", remaining)
		}
	})

	t.Run("a hair under 1.0 tokens is denied", func(t *testing.T) {
		const ip = "203.0.113.202"
		seedBucket(t, pool, ip, 0.999999)

		allowed, err := store.allow(ctx, ip, burst, 0)
		if err != nil {
			t.Fatalf("allow() returned unexpected error: %v", err)
		}
		if allowed {
			t.Error("allow() with tokens == 0.999999 = true, want false (below the inclusive threshold)")
		}

		remaining := readTokens(t, pool, ip)
		if remaining != 0.999999 {
			t.Errorf("tokens after a denied request = %v, want unchanged 0.999999 (a denied request must not consume a token)", remaining)
		}
	})
}

// seedBucket inserts ip's bucket row directly via SQL with the given token
// count and last_refill = now(), bypassing allow() entirely so the test
// controls the exact decision-time tokens value.
func seedBucket(t *testing.T, pool *db.Pool, ip string, tokens float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO rate_limit_buckets (ip, tokens, last_refill) VALUES ($1, $2, now())
ON CONFLICT (ip) DO UPDATE SET tokens = $2, last_refill = now()`,
		ip, tokens)
	if err != nil {
		t.Fatalf("seeding bucket for %s returned unexpected error: %v", ip, err)
	}
}

// readTokens reads back ip's current stored tokens value.
func readTokens(t *testing.T, pool *db.Pool, ip string) float64 {
	t.Helper()
	var tokens float64
	if err := pool.QueryRow(context.Background(), "SELECT tokens FROM rate_limit_buckets WHERE ip = $1", ip).Scan(&tokens); err != nil {
		t.Fatalf("reading tokens for %s returned unexpected error: %v", ip, err)
	}
	return tokens
}
