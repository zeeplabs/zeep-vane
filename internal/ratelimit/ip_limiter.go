// Package ratelimit provides a per-client-IP request rate limiter for
// vane's unauthenticated, credential-sensitive routes (login,
// password-reset, invite-accept, bootstrap) - none of which had any limit
// before (H10), leaving them open to unbounded brute force.
package ratelimit

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// bucketStore is IPLimiter's storage abstraction (ha-multi-replica
// HA-08..HA-12) - swapping the concrete implementation is what lets the
// exact same token-bucket limiting logic run either against an in-memory
// fake (unit tests) or Postgres (production, shared across every replica
// hitting the same database).
type bucketStore interface {
	// allow atomically refills ip's bucket (capacity burst, refill
	// refillPerSec tokens/second) then attempts to consume one token,
	// returning whether a token was available. Tokens never go negative
	// (floor at 0) or above burst (ceiling at burst), matching
	// golang.org/x/time/rate.Limiter's own semantics.
	allow(ctx context.Context, ip string, burst int, refillPerSec float64) (bool, error)
	// cleanup deletes buckets idle longer than idleTTL, bounding storage
	// growth the same way the previous in-memory map's sweep did. It is
	// best-effort: a returned error is logged and otherwise ignored by the
	// caller, never surfaced to the request path.
	cleanup(ctx context.Context, idleTTL time.Duration) error
}

// sweepThreshold caps how many allow() calls IPLimiter serves before
// paying the cost of a cleanup sweep - a churn of distinct client IPs (or
// a single attacker cycling source addresses) must not grow the
// underlying store forever.
const sweepThreshold = 10_000

// IPLimiter rate-limits requests per client IP using a token-bucket per IP,
// backed by store (Postgres in production - see NewIPLimiter - or a fake in
// unit tests). allow() opportunistically triggers a cleanup sweep once
// every sweepThreshold calls, evicting buckets idle longer than idleTTL.
type IPLimiter struct {
	store   bucketStore
	r       float64 // refill rate, tokens/second
	b       int     // bucket capacity (burst)
	idleTTL time.Duration

	mu        sync.Mutex
	callCount int
}

// NewIPLimiter builds an IPLimiter allowing perMinute requests sustained,
// with an initial burst of burst, per client IP, backed by pool
// (ha-multi-replica HA-08 - state lives in Postgres so the limit holds
// across every replica sharing the same database, not just this process).
// idleTTL controls how long an IP's bucket is kept once it stops sending
// requests.
func NewIPLimiter(pool *db.Pool, perMinute, burst int, idleTTL time.Duration) *IPLimiter {
	return newIPLimiterWithStore(newPostgresBucketStore(pool), perMinute, burst, idleTTL)
}

// newIPLimiterWithStore builds an IPLimiter against an arbitrary
// bucketStore - used by NewIPLimiter (Postgres) and by this package's own
// unit tests (a fake, in-memory store) to exercise the exact same
// token-bucket logic without a real database.
func newIPLimiterWithStore(store bucketStore, perMinute, burst int, idleTTL time.Duration) *IPLimiter {
	return &IPLimiter{
		store:   store,
		r:       float64(perMinute) / 60,
		b:       burst,
		idleTTL: idleTTL,
	}
}

func (l *IPLimiter) allow(ctx context.Context, ip string) bool {
	l.mu.Lock()
	l.callCount++
	shouldSweep := l.callCount > sweepThreshold
	if shouldSweep {
		l.callCount = 0
	}
	l.mu.Unlock()

	if shouldSweep {
		// Best-effort (spec.md Edge Cases): a cleanup failure is logged
		// and skipped for this cycle, never blocks the request path.
		if err := l.store.cleanup(ctx, l.idleTTL); err != nil {
			log.Printf("ratelimit: cleanup sweep failed, skipping this cycle: %v", err)
		}
	}

	allowed, err := l.store.allow(ctx, ip, l.b, l.r)
	if err != nil {
		// Fail-open (HA-10): a rate-limit backend outage must not become
		// an extra availability failure on top of an already-degraded
		// instance - the request proceeds as if under the limit.
		log.Printf("ratelimit: store error, failing open for ip=%s: %v", ip, err)
		return true
	}
	return allowed
}

// rateLimitedBody is returned byte-for-byte on every 429, matching the
// plain {"error": "..."} shape every other handler in this package uses.
const rateLimitedBody = `{"error":"too many requests, try again later"}`

// Middleware rejects a request with 429 once its client IP has exceeded the
// limit, otherwise passes it through unchanged.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(r.Context(), clientIP(r)) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(rateLimitedBody))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP reads the client IP from r.RemoteAddr - never from
// X-Forwarded-For/X-Real-IP, which any direct client can set to an
// arbitrary value unless a trusted reverse proxy is guaranteed to overwrite
// them first. A self-hosted deploy that terminates TLS behind its own
// reverse proxy should configure that proxy to preserve the real client
// address in RemoteAddr (most do this by default for a Go net/http
// backend); see README.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
