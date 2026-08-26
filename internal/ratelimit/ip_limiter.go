// Package ratelimit provides a per-client-IP request rate limiter for
// vane's unauthenticated, credential-sensitive routes (login,
// password-reset, invite-accept, bootstrap) - none of which had any limit
// before (H10), leaving them open to unbounded brute force.
package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// entry pairs a per-IP token bucket with the last time it was touched, so
// IPLimiter can evict entries nobody's used in a while.
type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// sweepThreshold caps how large limiters can grow before allow() pays the
// cost of a full sweep - a churn of distinct client IPs (or a single
// attacker cycling source addresses) must not grow this map forever.
const sweepThreshold = 10_000

// IPLimiter rate-limits requests per client IP using an in-memory
// token-bucket per IP. There is no background goroutine to manage: instead,
// allow() opportunistically evicts entries idle longer than idleTTL once the
// map exceeds sweepThreshold - simple, and sufficient for a single
// self-hosted instance's traffic (no cross-process/cross-replica state is
// needed here).
type IPLimiter struct {
	mu       sync.Mutex
	limiters map[string]*entry
	r        rate.Limit
	b        int
	idleTTL  time.Duration
}

// NewIPLimiter builds an IPLimiter allowing perMinute requests sustained,
// with an initial burst of burst, per client IP. idleTTL controls how long
// an IP's bucket is kept once it stops sending requests.
func NewIPLimiter(perMinute, burst int, idleTTL time.Duration) *IPLimiter {
	return &IPLimiter{
		limiters: make(map[string]*entry),
		r:        rate.Limit(float64(perMinute) / 60),
		b:        burst,
		idleTTL:  idleTTL,
	}
}

func (l *IPLimiter) allow(ip string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.limiters) > sweepThreshold {
		cutoff := now.Add(-l.idleTTL)
		for k, e := range l.limiters {
			if e.lastSeen.Before(cutoff) {
				delete(l.limiters, k)
			}
		}
	}

	e, ok := l.limiters[ip]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.r, l.b)}
		l.limiters[ip] = e
	}
	e.lastSeen = now

	return e.limiter.Allow()
}

// rateLimitedBody is returned byte-for-byte on every 429, matching the
// plain {"error": "..."} shape every other handler in this package uses.
const rateLimitedBody = `{"error":"too many requests, try again later"}`

// Middleware rejects a request with 429 once its client IP has exceeded the
// limit, otherwise passes it through unchanged.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
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
