package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeBucket mirrors rate_limit_buckets' two mutable columns for a single
// IP, in memory.
type fakeBucket struct {
	tokens     float64
	lastRefill time.Time
}

// fakeBucketStore is an in-memory bucketStore implementing the exact same
// refill-then-consume token-bucket formula postgresBucketStore runs in SQL
// (see its own doc comment), so this package's unit tests exercise real
// limiting behavior without a Postgres dependency. err, if set, is
// returned by allow() instead of computing anything - used to test
// IPLimiter's fail-open behavior (HA-10).
type fakeBucketStore struct {
	mu      sync.Mutex
	buckets map[string]*fakeBucket
	err     error
}

func newFakeBucketStore() *fakeBucketStore {
	return &fakeBucketStore{buckets: map[string]*fakeBucket{}}
}

func (s *fakeBucketStore) allow(_ context.Context, ip string, burst int, refillPerSec float64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return false, s.err
	}

	now := time.Now()
	b, ok := s.buckets[ip]
	if !ok {
		// A brand-new bucket starts full, matching
		// rate.NewLimiter(r, burst)'s own starting state.
		b = &fakeBucket{tokens: float64(burst), lastRefill: now}
		s.buckets[ip] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	tokens := b.tokens + elapsed*refillPerSec
	if tokens > float64(burst) {
		tokens = float64(burst)
	}

	allowed := tokens >= 1
	if allowed {
		tokens--
	}
	if tokens < 0 {
		tokens = 0
	}

	b.tokens = tokens
	b.lastRefill = now

	return allowed, nil
}

func (s *fakeBucketStore) cleanup(_ context.Context, idleTTL time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-idleTTL)
	for ip, b := range s.buckets {
		if b.lastRefill.Before(cutoff) {
			delete(s.buckets, ip)
		}
	}
	return nil
}

func newTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// TestIPLimiter_SingleInstance_BurstThenReject_UnchangedFromBeforeHA covers
// HA-12: with only one IPLimiter instance (the single-replica, pre-feature
// case), burst/reject behavior must be observably identical to before this
// feature's cross-replica changes - the Postgres-backed store's shared
// token-bucket formula must not alter single-instance behavior in any way.
// This is the direct, dedicated test HA-12 previously lacked; it does not
// rely on inferring single-replica correctness from the cross-replica test
// in ip_limiter_integration_test.go.
func TestIPLimiter_SingleInstance_BurstThenReject_UnchangedFromBeforeHA(t *testing.T) {
	limiter := newIPLimiterWithStore(newFakeBucketStore(), 60, 3, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	const ip = "203.0.113.210:1"

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d within burst: status = %d, want %d (HA-12: single-replica behavior unchanged)", i, rec.Code, http.StatusOK)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request past burst: status = %d, want %d (HA-12: single-replica behavior unchanged)", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Body.String(); got != rateLimitedBody {
		t.Errorf("429 body = %q, want %q (HA-12: byte-for-byte unchanged from before this feature)", got, rateLimitedBody)
	}
}

func TestIPLimiter_WithinBurst_AllRequestsPass(t *testing.T) {
	limiter := newIPLimiterWithStore(newFakeBucketStore(), 60, 3, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "203.0.113.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}

func TestIPLimiter_ExceedsBurst_429TooManyRequests(t *testing.T) {
	limiter := newIPLimiterWithStore(newFakeBucketStore(), 60, 2, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "203.0.113.2:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.2:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Body.String(); got != rateLimitedBody {
		t.Errorf("body = %q, want %q (byte-for-byte identical to before this feature, HA-09)", got, rateLimitedBody)
	}
}

// TestIPLimiter_DifferentIPs_TrackedIndependently confirms one client
// hammering the endpoint never exhausts another client's budget - each IP
// gets its own bucket.
func TestIPLimiter_DifferentIPs_TrackedIndependently(t *testing.T) {
	limiter := newIPLimiterWithStore(newFakeBucketStore(), 60, 1, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	req1 := httptest.NewRequest(http.MethodPost, "/", nil)
	req1.RemoteAddr = "203.0.113.3:1"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first IP's request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.RemoteAddr = "203.0.113.4:1"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("second IP's request: status = %d, want %d (must not be limited by the first IP's usage)", rec2.Code, http.StatusOK)
	}
}

func TestClientIP_RemoteAddrWithPort_ReturnsHostOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:54321"

	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP() = %q, want %q", got, "198.51.100.7")
	}
}

func TestClientIP_RemoteAddrWithoutPort_ReturnsAsIs(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-host-port"

	if got := clientIP(req); got != "not-a-host-port" {
		t.Errorf("clientIP() = %q, want %q (falls back to RemoteAddr verbatim)", got, "not-a-host-port")
	}
}

func TestIPLimiter_IdleEntrySwept_BucketResetsAfterThresholdExceeded(t *testing.T) {
	store := newFakeBucketStore()
	limiter := newIPLimiterWithStore(store, 60, 1, time.Millisecond)
	ctx := context.Background()

	// Exhausts the one-IP bucket, then forces it stale enough to be swept
	// once sweepThreshold is crossed.
	if !limiter.allow(ctx, "203.0.113.5") {
		t.Fatal("first allow() = false, want true (fresh bucket)")
	}
	if limiter.allow(ctx, "203.0.113.5") {
		t.Fatal("second allow() = true, want false (burst of 1 exhausted)")
	}

	store.mu.Lock()
	store.buckets["203.0.113.5"].lastRefill = time.Now().Add(-time.Hour)
	store.mu.Unlock()

	// Force the next allow() call to cross sweepThreshold, the same
	// condition that already triggers IPLimiter's cleanup sweep in
	// production.
	limiter.mu.Lock()
	limiter.callCount = sweepThreshold
	limiter.mu.Unlock()

	if !limiter.allow(ctx, "203.0.113.5") {
		t.Error("allow() after sweep = false, want true (stale entry evicted, fresh bucket granted)")
	}
}

// TestIPLimiter_StoreError_FailsOpen covers HA-10: a bucketStore error
// (simulating a Postgres outage/timeout) must let the request through
// rather than block legitimate traffic.
func TestIPLimiter_StoreError_FailsOpen(t *testing.T) {
	store := newFakeBucketStore()
	store.err = errors.New("simulated postgres outage")
	limiter := newIPLimiterWithStore(store, 60, 1, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.6:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (fail-open on store error, HA-10)", rec.Code, http.StatusOK)
	}
}
