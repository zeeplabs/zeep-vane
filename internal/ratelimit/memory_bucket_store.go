package ratelimit

import (
	"context"
	"sync"
	"time"
)

// memoryBucket mirrors rate_limit_buckets' two mutable columns for a single
// IP, in memory.
type memoryBucket struct {
	tokens     float64
	lastRefill time.Time
}

// memoryBucketStore is IPLimiter's in-process fallback bucketStore (AD-029),
// used when the Postgres-backed primary store errors and while the circuit
// breaker has the primary quarantined. It implements the exact same
// refill-then-consume token-bucket formula postgresBucketStore runs in SQL,
// so the limit's semantics do not change when the fallback engages; the only
// difference is scope, since this state lives in one process.
type memoryBucketStore struct {
	mu      sync.Mutex
	buckets map[string]*memoryBucket
}

func newMemoryBucketStore() *memoryBucketStore {
	return &memoryBucketStore{buckets: map[string]*memoryBucket{}}
}

// allow refills then consumes one token from ip's bucket, mirroring
// postgresBucketStore.allow (and, transitively, the client-side
// golang.org/x/time/rate.Limiter semantics HA-09 required). It never returns
// an error: the only failure mode of the primary store that matters here is
// transport/database failure, which does not exist in-process.
func (s *memoryBucketStore) allow(_ context.Context, ip string, burst int, refillPerSec float64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	b, ok := s.buckets[ip]
	if !ok {
		b = &memoryBucket{tokens: float64(burst), lastRefill: now}
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

// cleanup deletes buckets idle longer than idleTTL, bounding the map the same
// way postgresBucketStore.cleanup bounds rate_limit_buckets.
func (s *memoryBucketStore) cleanup(_ context.Context, idleTTL time.Duration) error {
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
