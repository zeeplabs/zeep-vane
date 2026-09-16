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
	limiter := newIPLimiterWithStore(newFakeBucketStore(), newFakeBucketStore(), 60, 3, time.Minute)
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
	limiter := newIPLimiterWithStore(newFakeBucketStore(), newFakeBucketStore(), 60, 3, time.Minute)
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
	limiter := newIPLimiterWithStore(newFakeBucketStore(), newFakeBucketStore(), 60, 2, time.Minute)
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
	limiter := newIPLimiterWithStore(newFakeBucketStore(), newFakeBucketStore(), 60, 1, time.Minute)
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
	limiter := newIPLimiterWithStore(store, newFakeBucketStore(), 60, 1, time.Millisecond)
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

// spyBucketStore counts allow() calls and can be made to error and/or block,
// so tests observe circuit-breaker routing (how often the primary is really
// called) deterministically.
type spyBucketStore struct {
	mu         sync.Mutex
	calls      int
	cleanups   int
	err        error
	block      chan struct{}
	allowValue bool
}

func (s *spyBucketStore) allow(_ context.Context, _ string, _ int, _ float64) (bool, error) {
	s.mu.Lock()
	s.calls++
	err := s.err
	allowValue := s.allowValue
	block := s.block
	s.mu.Unlock()

	if block != nil {
		<-block
	}
	if err != nil {
		return false, err
	}
	return allowValue, nil
}

func (s *spyBucketStore) cleanup(_ context.Context, _ time.Duration) error {
	s.mu.Lock()
	s.cleanups++
	s.mu.Unlock()
	return nil
}

func (s *spyBucketStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *spyBucketStore) cleanupCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanups
}

func waitForCalls(t *testing.T, s *spyBucketStore, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.callCount() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("primary calls = %d, want >= %d within deadline", s.callCount(), want)
}

// TestIPLimiter_PrimaryError_UsesFallback covers RLF-01/RLF-02: a primary
// store error routes the request to the in-memory fallback, which enforces
// the same limit (a 429 once its burst is exhausted) instead of failing open.
func TestIPLimiter_PrimaryError_UsesFallback(t *testing.T) {
	primary := newFakeBucketStore()
	primary.err = errors.New("simulated postgres outage")
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 1, time.Minute)
	handler := limiter.Middleware(newTestHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.6:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d (fresh fallback bucket)", rec.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.6:1"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want %d (fallback enforces the limit, not fail-open)", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Body.String(); got != rateLimitedBody {
		t.Errorf("429 body = %q, want %q", got, rateLimitedBody)
	}
}

// TestIPLimiter_FallbackError_LastResortAllows covers RLF-03 on both
// fallback-error paths: the primary-error path (fallback error right after a
// primary failure) and the open-circuit path (fallback is the chosen store
// and errors on its own).
func TestIPLimiter_FallbackError_LastResortAllows(t *testing.T) {
	primary := newFakeBucketStore()
	primary.err = errors.New("primary down")
	fallback := newFakeBucketStore()
	fallback.err = errors.New("fallback down")
	limiter := newIPLimiterWithStore(primary, fallback, 60, 1, time.Minute)
	ctx := context.Background()

	// First call: primary errors, then the fallback errors (post-primary path).
	if !limiter.allow(ctx, "203.0.113.7") {
		t.Error("first allow() = false, want true (last-resort fail-open after a primary error)")
	}
	// Second call: the circuit is now open, so the fallback is the chosen
	// store; its error must fail open through the !usePrimary branch.
	if !limiter.allow(ctx, "203.0.113.7") {
		t.Error("second allow() = false, want true (last-resort fail-open when the chosen fallback errors)")
	}
}

// TestIPLimiter_CircuitOpen_SkipsPrimary covers RLF-04: within the cooldown,
// a failing primary is not called again.
func TestIPLimiter_CircuitOpen_SkipsPrimary(t *testing.T) {
	primary := &spyBucketStore{err: errors.New("primary down")}
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 10, time.Minute)
	ctx := context.Background()

	if !limiter.allow(ctx, "203.0.113.8") {
		t.Fatal("first allow() = false, want true (fallback)")
	}
	if got := primary.callCount(); got != 1 {
		t.Fatalf("primary calls = %d, want 1 (first error)", got)
	}

	for i := 0; i < 3; i++ {
		if !limiter.allow(ctx, "203.0.113.8") {
			t.Fatalf("call %d: allow() = false, want true (fallback)", i)
		}
	}
	if got := primary.callCount(); got != 1 {
		t.Errorf("primary calls = %d, want 1 (circuit open, primary skipped)", got)
	}
}

// TestIPLimiter_HalfOpen_OnlyOneProbe covers RLF-05: after the cooldown,
// exactly one request probes the primary; concurrent requests use the
// fallback until the probe resolves.
func TestIPLimiter_HalfOpen_OnlyOneProbe(t *testing.T) {
	primary := &spyBucketStore{err: errors.New("primary down")}
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 10, time.Minute)
	ctx := context.Background()

	if !limiter.allow(ctx, "probe") {
		t.Fatal("opening call: allow() = false, want true")
	}

	probeRelease := make(chan struct{})
	primary.mu.Lock()
	primary.block = probeRelease
	primary.err = nil
	primary.allowValue = true
	primary.mu.Unlock()

	limiter.mu.Lock()
	limiter.breakerOpenUntil = time.Now().Add(-time.Second)
	limiter.mu.Unlock()

	probeDone := make(chan bool, 1)
	go func() { probeDone <- limiter.allow(ctx, "probe") }()
	waitForCalls(t, primary, 2)

	otherDone := make(chan bool, 1)
	go func() { otherDone <- limiter.allow(ctx, "other") }()

	select {
	case <-otherDone:
	case <-time.After(time.Second):
		t.Fatal("concurrent request did not return while the probe was in flight; want it to use the fallback")
	}
	if got := primary.callCount(); got != 2 {
		t.Errorf("primary calls = %d, want 2 (only the single probe)", got)
	}

	close(probeRelease)
	<-probeDone
}

// TestIPLimiter_HalfOpenProbeSuccess_ClosesCircuit covers RLF-06.
func TestIPLimiter_HalfOpenProbeSuccess_ClosesCircuit(t *testing.T) {
	primary := &spyBucketStore{err: errors.New("primary down"), allowValue: true}
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 10, time.Minute)
	ctx := context.Background()

	if !limiter.allow(ctx, "ip") {
		t.Fatal("opening call: allow() = false, want true")
	}
	primary.mu.Lock()
	primary.err = nil
	primary.mu.Unlock()
	limiter.mu.Lock()
	limiter.breakerOpenUntil = time.Now().Add(-time.Second)
	limiter.mu.Unlock()

	if !limiter.allow(ctx, "ip") {
		t.Fatal("probe: allow() = false, want true")
	}
	if !limiter.allow(ctx, "ip") {
		t.Fatal("post-recovery: allow() = false, want true")
	}
	if got := primary.callCount(); got != 3 {
		t.Errorf("primary calls = %d, want 3 (probe success resumed the primary)", got)
	}

	limiter.mu.Lock()
	openUntil := limiter.breakerOpenUntil
	probing := limiter.breakerProbing
	limiter.mu.Unlock()
	if !openUntil.IsZero() || probing {
		t.Errorf("breaker state after success = (openUntil=%v, probing=%v), want closed", openUntil, probing)
	}
}

// TestIPLimiter_HalfOpenProbeFailure_RearmsCooldown covers RLF-07.
func TestIPLimiter_HalfOpenProbeFailure_RearmsCooldown(t *testing.T) {
	primary := &spyBucketStore{err: errors.New("primary down")}
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 10, time.Minute)
	ctx := context.Background()

	limiter.allow(ctx, "ip")
	limiter.mu.Lock()
	limiter.breakerOpenUntil = time.Now().Add(-time.Second)
	limiter.mu.Unlock()

	limiter.allow(ctx, "ip")
	if got := primary.callCount(); got != 2 {
		t.Fatalf("primary calls = %d, want 2 (probe attempted)", got)
	}

	limiter.allow(ctx, "ip")
	if got := primary.callCount(); got != 2 {
		t.Errorf("primary calls = %d, want 2 (cooldown re-armed, primary skipped)", got)
	}

	limiter.mu.Lock()
	openUntil := limiter.breakerOpenUntil
	limiter.mu.Unlock()
	if !openUntil.After(time.Now()) {
		t.Errorf("breakerOpenUntil = %v, want in the future (re-armed)", openUntil)
	}
}

// TestIPLimiter_Sweep_FallbackPathSweepsFallback covers RLF-08: when the
// circuit routes the sweep call to the fallback, the fallback's idle buckets
// are the ones evicted.
func TestIPLimiter_Sweep_FallbackPathSweepsFallback(t *testing.T) {
	primary := newFakeBucketStore()
	primary.err = errors.New("primary down")
	fallback := newFakeBucketStore()
	limiter := newIPLimiterWithStore(primary, fallback, 60, 1, time.Minute)
	ctx := context.Background()

	// First call opens the circuit and creates a fallback bucket.
	if !limiter.allow(ctx, "stale") {
		t.Fatal("first allow() = false, want true (fallback)")
	}

	fallback.mu.Lock()
	fallback.buckets["stale"].lastRefill = time.Now().Add(-2 * time.Hour)
	fallback.mu.Unlock()

	limiter.mu.Lock()
	limiter.callCount = sweepThreshold
	limiter.mu.Unlock()

	if !limiter.allow(ctx, "other") {
		t.Fatal("sweep call: allow() = false, want true (fallback)")
	}

	fallback.mu.Lock()
	_, kept := fallback.buckets["stale"]
	fallback.mu.Unlock()
	if kept {
		t.Error("stale fallback bucket still present, want evicted (sweep targeted the fallback)")
	}
}

// TestIPLimiter_Sweep_PrimaryPathSweepsPrimary covers RLF-08: with the
// circuit closed the sweep call cleans up the primary store, not the
// fallback.
func TestIPLimiter_Sweep_PrimaryPathSweepsPrimary(t *testing.T) {
	primary := &spyBucketStore{allowValue: true}
	fallback := &spyBucketStore{allowValue: true}
	limiter := newIPLimiterWithStore(primary, fallback, 60, 1, time.Minute)

	limiter.mu.Lock()
	limiter.callCount = sweepThreshold
	limiter.mu.Unlock()

	if !limiter.allow(context.Background(), "ip") {
		t.Fatal("sweep call: allow() = false, want true")
	}

	if got := primary.cleanupCount(); got != 1 {
		t.Errorf("primary cleanups = %d, want 1 (sweep targeted the primary)", got)
	}
	if got := fallback.cleanupCount(); got != 0 {
		t.Errorf("fallback cleanups = %d, want 0 (circuit closed)", got)
	}
}
