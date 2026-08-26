package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestIPLimiter_WithinBurst_AllRequestsPass(t *testing.T) {
	limiter := NewIPLimiter(60, 3, time.Minute)
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
	limiter := NewIPLimiter(60, 2, time.Minute)
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
}

// TestIPLimiter_DifferentIPs_TrackedIndependently confirms one client
// hammering the endpoint never exhausts another client's budget - each IP
// gets its own bucket.
func TestIPLimiter_DifferentIPs_TrackedIndependently(t *testing.T) {
	limiter := NewIPLimiter(60, 1, time.Minute)
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
	limiter := NewIPLimiter(60, 1, time.Millisecond)

	// Exhausts the one-IP bucket, then forces it stale enough to be swept
	// on the next allow() call once sweepThreshold is crossed.
	if !limiter.allow("203.0.113.5") {
		t.Fatal("first allow() = false, want true (fresh bucket)")
	}
	if limiter.allow("203.0.113.5") {
		t.Fatal("second allow() = true, want false (burst of 1 exhausted)")
	}

	limiter.mu.Lock()
	limiter.limiters["203.0.113.5"].lastSeen = time.Now().Add(-time.Hour)
	for i := 0; i <= sweepThreshold; i++ {
		limiter.limiters[string(rune(i))] = &entry{limiter: limiter.limiters["203.0.113.5"].limiter, lastSeen: time.Now().Add(-time.Hour)}
	}
	limiter.mu.Unlock()

	if !limiter.allow("203.0.113.5") {
		t.Error("allow() after sweep = false, want true (stale entry evicted, fresh bucket granted)")
	}
}
