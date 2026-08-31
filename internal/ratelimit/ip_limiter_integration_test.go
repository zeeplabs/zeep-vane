//go:build integration

package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// testDatabaseURL returns TEST_DATABASE_URL, skipping the test if unset -
// matching the pattern used by every other package's integration tests
// (see internal/db/pool_test.go).
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newRateLimitTestPool(t *testing.T) *db.Pool {
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

// TestIPLimiter_TwoInstances_SameDatabase_ShareLimitPerIP covers HA-08/HA-09:
// two IPLimiter instances backed by the same Postgres database (simulating
// two replicas behind a load balancer) must enforce one shared limit per
// client IP - a client hammering one replica past its burst must also be
// rejected by the other, while a different IP on the second instance is
// unaffected.
func TestIPLimiter_TwoInstances_SameDatabase_ShareLimitPerIP(t *testing.T) {
	pool := newRateLimitTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM rate_limit_buckets") })

	const burst = 3
	limiterA := NewIPLimiter(pool, 60, burst, time.Minute)
	limiterB := NewIPLimiter(pool, 60, burst, time.Minute)
	handlerA := limiterA.Middleware(newTestHandler())
	handlerB := limiterB.Middleware(newTestHandler())

	const sharedIP = "203.0.113.100:1"
	const otherIP = "203.0.113.101:1"

	for i := 0; i < burst; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = sharedIP
		rec := httptest.NewRecorder()
		handlerA.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("replica A request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	// The burst is now exhausted on replica A. A request for the same IP
	// routed to replica B must also be rejected - it shares the same
	// Postgres-backed bucket.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = sharedIP
	rec := httptest.NewRecorder()
	handlerB.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("replica B, same IP after replica A exhausted the burst: status = %d, want %d (HA-08/HA-09 cross-replica enforcement)", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Body.String(); got != rateLimitedBody {
		t.Errorf("429 body = %q, want %q (byte-for-byte match, HA-09)", got, rateLimitedBody)
	}

	// A different IP on replica B must be entirely unaffected.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.RemoteAddr = otherIP
	rec2 := httptest.NewRecorder()
	handlerB.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("replica B, different IP: status = %d, want %d (must not share replica A's exhausted budget)", rec2.Code, http.StatusOK)
	}
}

// TestIPLimiter_Cleanup_RemovesIdleRows covers HA-11: table growth is
// bounded the same way the previous in-memory map's sweep was - once a
// cleanup cycle runs, buckets idle longer than idleTTL are gone.
func TestIPLimiter_Cleanup_RemovesIdleRows(t *testing.T) {
	pool := newRateLimitTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM rate_limit_buckets") })

	store := newPostgresBucketStore(pool)
	ctx := context.Background()

	const idleIP = "203.0.113.102"
	const freshIP = "203.0.113.103"

	if _, err := store.allow(ctx, idleIP, 5, 1); err != nil {
		t.Fatalf("allow(idleIP) returned unexpected error: %v", err)
	}
	if _, err := store.allow(ctx, freshIP, 5, 1); err != nil {
		t.Fatalf("allow(freshIP) returned unexpected error: %v", err)
	}

	// Backdate the idle row directly - idleTTL is normally minutes, and
	// cleanup compares last_refill against now()-idleTTL, so this is the
	// simplest way to make it look old without waiting.
	if _, err := pool.Exec(ctx, "UPDATE rate_limit_buckets SET last_refill = now() - interval '1 hour' WHERE ip = $1", idleIP); err != nil {
		t.Fatalf("backdating idleIP's last_refill returned unexpected error: %v", err)
	}

	if err := store.cleanup(ctx, 10*time.Minute); err != nil {
		t.Fatalf("cleanup() returned unexpected error: %v", err)
	}

	var idleCount, freshCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM rate_limit_buckets WHERE ip = $1", idleIP).Scan(&idleCount); err != nil {
		t.Fatalf("counting idleIP rows returned unexpected error: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM rate_limit_buckets WHERE ip = $1", freshIP).Scan(&freshCount); err != nil {
		t.Fatalf("counting freshIP rows returned unexpected error: %v", err)
	}

	if idleCount != 0 {
		t.Errorf("idle row count = %d after cleanup, want 0 (idle longer than idleTTL)", idleCount)
	}
	if freshCount != 1 {
		t.Errorf("fresh row count = %d after cleanup, want 1 (not idle, must survive cleanup)", freshCount)
	}
}
