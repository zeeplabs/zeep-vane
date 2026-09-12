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

// breakerCooldown bounds how long the circuit stays open after the primary
// store errors, before a single half-open probe is allowed to test it again
// (AD-029). 5s sits just above the store's own 3s lock_timeout, the shortest
// error it masks. A package constant, matching this package's existing
// tuned-constant convention - no new environment variable.
const breakerCooldown = 5 * time.Second

// IPLimiter rate-limits requests per client IP using a token-bucket per IP,
// backed by store (Postgres in production - see NewIPLimiter - or a fake in
// unit tests). When store errors, requests are evaluated against fallback
// (an in-process bucket store) rather than allowed unconditionally, and a
// circuit breaker quarantines a failing store so it is not called on every
// request (AD-029, supersedes HA-10's fail-open). allow() opportunistically
// triggers a cleanup sweep once every sweepThreshold calls, evicting buckets
// idle longer than idleTTL from the store in use.
type IPLimiter struct {
	store    bucketStore
	fallback bucketStore
	r        float64 // refill rate, tokens/second
	b        int     // bucket capacity (burst)
	idleTTL  time.Duration

	mu        sync.Mutex
	callCount int

	// Circuit breaker state, guarded by mu. breakerOpenUntil zero means
	// closed; non-zero in the future means open; non-zero in the past means
	// half-open. breakerProbing is true only while the single half-open
	// probe is in flight.
	breakerOpenUntil time.Time
	breakerProbing   bool
}

// NewIPLimiter builds an IPLimiter allowing perMinute requests sustained,
// with an initial burst of burst, per client IP, backed by pool
// (ha-multi-replica HA-08 - state lives in Postgres so the limit holds
// across every replica sharing the same database, not just this process).
// idleTTL controls how long an IP's bucket is kept once it stops sending
// requests.
func NewIPLimiter(pool *db.Pool, perMinute, burst int, idleTTL time.Duration) *IPLimiter {
	return newIPLimiterWithStore(newPostgresBucketStore(pool), newMemoryBucketStore(), perMinute, burst, idleTTL)
}

// newIPLimiterWithStore builds an IPLimiter against arbitrary bucket stores
// (a primary and a fallback) - used by NewIPLimiter (Postgres primary +
// in-memory fallback) and by this package's own unit tests to exercise the
// exact same token-bucket logic without a real database.
func newIPLimiterWithStore(store, fallback bucketStore, perMinute, burst int, idleTTL time.Duration) *IPLimiter {
	return &IPLimiter{
		store:    store,
		fallback: fallback,
		r:        float64(perMinute) / 60,
		b:        burst,
		idleTTL:  idleTTL,
	}
}

// allow applies the per-IP limit, selecting between the shared primary store
// and the in-process fallback. On a primary error it opens the circuit for
// breakerCooldown and evaluates the request against the fallback instead of
// allowing it unconditionally (AD-029, superseding HA-10's fail-open).
func (l *IPLimiter) allow(ctx context.Context, ip string) bool {
	now := time.Now()
	store, usePrimary, sweep := l.pickStore(now)

	if sweep {
		// Best-effort (spec.md Edge Cases): a cleanup failure is logged
		// and skipped for this cycle, never blocks the request path.
		if err := store.cleanup(ctx, l.idleTTL); err != nil {
			log.Printf("ratelimit: cleanup sweep failed, skipping this cycle: %v", err)
		}
	}

	allowed, err := store.allow(ctx, ip, l.b, l.r)
	if err == nil {
		if usePrimary {
			l.onPrimarySuccess()
		}
		return allowed
	}

	if !usePrimary {
		// The fallback itself failed. This should not happen
		// (memoryBucketStore never errors); allow as a last resort rather
		// than turn an impossible failure into a self-inflicted 429.
		log.Printf("ratelimit: fallback store error, failing open for ip=%s: %v", ip, err)
		return true
	}

	l.onPrimaryError(ip, now, err)
	allowed, ferr := l.fallback.allow(ctx, ip, l.b, l.r)
	if ferr != nil {
		log.Printf("ratelimit: fallback store error, failing open for ip=%s: %v (primary error: %v)", ip, ferr, err)
		return true
	}
	return allowed
}

// pickStore decides which store handles this request and returns it, along
// with whether the primary was chosen and whether this call is the periodic
// cleanup sweep. All breaker state is read and advanced under l.mu, so the
// single half-open probe is race-free: once a probe is in flight, concurrent
// calls see l.breakerProbing and take the fallback.
func (l *IPLimiter) pickStore(now time.Time) (store bucketStore, usePrimary, sweep bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.callCount++
	if l.callCount > sweepThreshold {
		l.callCount = 0
		sweep = true
	}

	if !l.breakerOpenUntil.IsZero() && now.Before(l.breakerOpenUntil) {
		return l.fallback, false, sweep
	}
	if l.breakerProbing {
		return l.fallback, false, sweep
	}

	if !l.breakerOpenUntil.IsZero() {
		// Half-open: this call becomes the single probe.
		l.breakerProbing = true
	}
	return l.store, true, sweep
}

// onPrimarySuccess closes the circuit after a successful primary call,
// logging recovery when it was previously open.
func (l *IPLimiter) onPrimarySuccess() {
	l.mu.Lock()
	wasOpen := !l.breakerOpenUntil.IsZero()
	l.breakerOpenUntil = time.Time{}
	l.breakerProbing = false
	l.mu.Unlock()

	if wasOpen {
		log.Printf("ratelimit: primary store recovered, resuming shared limiter")
	}
}

// onPrimaryError opens the circuit for breakerCooldown after a primary error
// and logs that the in-process fallback now handles requests.
func (l *IPLimiter) onPrimaryError(ip string, now time.Time, err error) {
	l.mu.Lock()
	l.breakerOpenUntil = now.Add(breakerCooldown)
	l.breakerProbing = false
	l.mu.Unlock()

	log.Printf("ratelimit: store error, using in-memory fallback for ip=%s: %v", ip, err)
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
