package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryBucketStore_NewBucketStartsFull(t *testing.T) {
	store := newMemoryBucketStore()
	ctx := context.Background()

	if allowed, err := store.allow(ctx, "203.0.113.1", 1, 0); err != nil || !allowed {
		t.Fatalf("first allow() = (%v, %v), want (true, nil) (brand-new bucket starts full)", allowed, err)
	}
	if allowed, err := store.allow(ctx, "203.0.113.1", 1, 0); err != nil || allowed {
		t.Fatalf("second allow() = (%v, %v), want (false, nil) (burst of 1 exhausted)", allowed, err)
	}
}

func TestMemoryBucketStore_RefillsOverTime(t *testing.T) {
	store := newMemoryBucketStore()
	ctx := context.Background()
	const ip = "203.0.113.2"

	if allowed, _ := store.allow(ctx, ip, 2, 1); !allowed {
		t.Fatal("first allow() = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 1); !allowed {
		t.Fatal("second allow() = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 1); allowed {
		t.Fatal("third allow() = true, want false (burst of 2 exhausted)")
	}

	// Rewind lastRefill by 2s at 1 token/s: the bucket must be full again.
	store.mu.Lock()
	store.buckets[ip].lastRefill = time.Now().Add(-2 * time.Second)
	store.mu.Unlock()

	if allowed, _ := store.allow(ctx, ip, 2, 1); !allowed {
		t.Error("allow() after refill = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 1); !allowed {
		t.Error("second allow() after refill = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 1); allowed {
		t.Error("third allow() after refill = true, want false (refill clamped at burst)")
	}
}

func TestMemoryBucketStore_ClampsTokensAtBurst(t *testing.T) {
	store := newMemoryBucketStore()
	ctx := context.Background()
	const ip = "203.0.113.3"

	if allowed, _ := store.allow(ctx, ip, 2, 100); !allowed {
		t.Fatal("first allow() = false, want true")
	}

	// 10s at 100 tokens/s would be 1000 tokens; the clamp must keep it at 2.
	store.mu.Lock()
	store.buckets[ip].lastRefill = time.Now().Add(-10 * time.Second)
	store.mu.Unlock()

	if allowed, _ := store.allow(ctx, ip, 2, 100); !allowed {
		t.Fatal("allow() after long idle = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 100); !allowed {
		t.Fatal("second allow() after long idle = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 2, 100); allowed {
		t.Error("third allow() after long idle = true, want false (tokens clamped at burst=2)")
	}
}

func TestMemoryBucketStore_NeverGoesNegative(t *testing.T) {
	store := newMemoryBucketStore()
	ctx := context.Background()
	const ip = "203.0.113.4"

	if allowed, _ := store.allow(ctx, ip, 1, 0); !allowed {
		t.Fatal("first allow() = false, want true")
	}
	if allowed, _ := store.allow(ctx, ip, 1, 0); allowed {
		t.Fatal("second allow() = true, want false")
	}

	store.mu.Lock()
	tokens := store.buckets[ip].tokens
	store.mu.Unlock()

	if tokens < 0 {
		t.Errorf("tokens = %v, want >= 0 (floor at 0)", tokens)
	}
}

func TestMemoryBucketStore_CleanupEvictsIdleOnly(t *testing.T) {
	store := newMemoryBucketStore()
	ctx := context.Background()

	store.allow(ctx, "stale", 1, 0)
	store.allow(ctx, "fresh", 1, 0)

	store.mu.Lock()
	store.buckets["stale"].lastRefill = time.Now().Add(-2 * time.Hour)
	store.mu.Unlock()

	if err := store.cleanup(ctx, time.Hour); err != nil {
		t.Fatalf("cleanup() = %v, want nil", err)
	}

	store.mu.Lock()
	_, staleKept := store.buckets["stale"]
	_, freshKept := store.buckets["fresh"]
	store.mu.Unlock()

	if staleKept {
		t.Error("stale bucket still present after cleanup, want evicted")
	}
	if !freshKept {
		t.Error("fresh bucket evicted by cleanup, want kept")
	}
}

// TestMemoryBucketStore_ParityWithFake proves RLF-09: the production
// in-memory fallback and the primary store's semantics agree call-for-call
// over an identical sequence, no sleeps involved.
func TestMemoryBucketStore_ParityWithFake(t *testing.T) {
	mem := newMemoryBucketStore()
	fake := newFakeBucketStore()
	ctx := context.Background()
	const ip = "203.0.113.9"

	for i := 0; i < 6; i++ {
		memAllowed, err := mem.allow(ctx, ip, 3, 1)
		if err != nil {
			t.Fatalf("call %d: memoryBucketStore.allow error = %v, want nil", i, err)
		}
		fakeAllowed, err := fake.allow(ctx, ip, 3, 1)
		if err != nil {
			t.Fatalf("call %d: fakeBucketStore.allow error = %v, want nil", i, err)
		}
		if memAllowed != fakeAllowed {
			t.Fatalf("call %d: memory=%v fake=%v, want identical verdicts", i, memAllowed, fakeAllowed)
		}
	}
}
