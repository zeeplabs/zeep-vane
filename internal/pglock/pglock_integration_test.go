//go:build integration

package pglock

import (
	"context"
	"os"
	"testing"
	"time"
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

// pglockTestKey is a dedicated advisory lock key namespace for this test
// file, distinct from internal/dbtest's test-only keys
// (727100001-727100003) and from any production key this package's
// callers use (727200000-727299999 block, per this package's doc
// comment) - so this file's own concurrent tests never contend with an
// unrelated lock.
const pglockTestKey int64 = 727300001

// TestTryAcquire_SecondAttemptFailsWhileFirstHolds covers HA-01/HA-02: a
// second TryAcquire for the same key must fail while the first holds it,
// and must succeed once the first releases.
func TestTryAcquire_SecondAttemptFailsWhileFirstHolds(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	h1, ok, err := TryAcquire(ctx, dsn, pglockTestKey)
	if err != nil {
		t.Fatalf("first TryAcquire() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("first TryAcquire() ok = false, want true (nothing else holds this key)")
	}
	t.Cleanup(func() { _ = h1.Release(context.Background()) })

	h2, ok2, err := TryAcquire(ctx, dsn, pglockTestKey)
	if err != nil {
		t.Fatalf("second TryAcquire() returned unexpected error: %v", err)
	}
	if ok2 {
		t.Fatal("second TryAcquire() ok = true, want false while the first handle still holds the lock")
	}
	if h2 != nil {
		t.Error("second TryAcquire() returned a non-nil handle despite ok=false")
	}

	if err := h1.Release(context.Background()); err != nil {
		t.Fatalf("Release() returned unexpected error: %v", err)
	}

	h3, ok3, err := TryAcquire(ctx, dsn, pglockTestKey)
	if err != nil {
		t.Fatalf("third TryAcquire() returned unexpected error: %v", err)
	}
	if !ok3 {
		t.Fatal("third TryAcquire() ok = false, want true after the first handle released")
	}
	_ = h3.Release(context.Background())
}

// TestHandle_Healthy_FalseAfterConnectionClosedOutOfBand covers HA-04's
// underlying mechanism: PollerManager's leader loop detects lock loss via
// Healthy() - this proves Healthy() actually flips to false once the
// handle's session ends, whether via a clean Release or (as simulated
// here) the connection dying out from under it (crash/network partition).
func TestHandle_Healthy_FalseAfterConnectionClosedOutOfBand(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	h, ok, err := TryAcquire(ctx, dsn, pglockTestKey+1)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want true")
	}

	if !h.Healthy(ctx) {
		t.Fatal("Healthy() = false immediately after a successful acquire, want true")
	}

	// Simulate a crash: close the underlying connection directly, without
	// going through Release's graceful unlock - this is what happens when
	// a replica's process dies or its network connection to Postgres is
	// severed. Postgres auto-releases the advisory lock once the session
	// ends, so a subsequent TryAcquire for the same key must succeed.
	if err := h.conn.Close(context.Background()); err != nil {
		t.Fatalf("simulated crash: conn.Close() returned unexpected error: %v", err)
	}

	if h.Healthy(ctx) {
		t.Error("Healthy() = true after the underlying connection was closed out-of-band, want false")
	}

	h2, ok2, err := TryAcquire(ctx, dsn, pglockTestKey+1)
	if err != nil {
		t.Fatalf("TryAcquire() after simulated crash returned unexpected error: %v", err)
	}
	if !ok2 {
		t.Fatal("TryAcquire() after simulated crash ok = false, want true - Postgres should have auto-released the dead session's lock")
	}
	_ = h2.Release(context.Background())
}

// TestAcquire_HonorsContextCancellation covers CertMagic's Lock(ctx, name)
// contract (HA-15..HA-17's blocking variant): a canceled context must
// return promptly instead of waiting indefinitely for a lock someone else
// holds.
func TestAcquire_HonorsContextCancellation(t *testing.T) {
	dsn := testDatabaseURL(t)
	name := "pglock-test-acquire-cancel"

	holder, err := Acquire(context.Background(), dsn, name)
	if err != nil {
		t.Fatalf("holder Acquire() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = holder.Release(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = Acquire(ctx, dsn, name)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Acquire() with a canceled context returned nil error, want an error - the lock is held by another session")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Acquire() took %v to return after context cancellation, want it to return promptly", elapsed)
	}
}

// TestHandle_Release_NormalCase_NoError covers the ordinary, successful
// path: releasing a lock this handle actually holds must not error.
func TestHandle_Release_NormalCase_NoError(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	h, ok, err := TryAcquire(ctx, dsn, pglockTestKey+2)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want true")
	}

	if err := h.Release(context.Background()); err != nil {
		t.Fatalf("Release() returned unexpected error: %v", err)
	}
}

// TestHandle_Release_KeyMismatch_ReturnsError covers the sensor-found gap:
// Release must check pg_advisory_unlock's own returned boolean and fail
// when the key it's told to unlock isn't one this session actually holds,
// rather than silently succeeding because the connection gets closed right
// after anyway. This proves the boundary Release now checks, using the
// exact mismatch shape the discrimination sensor injected (Acquire and
// Release disagreeing about which key/hash is held).
func TestHandle_Release_KeyMismatch_ReturnsError(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx := context.Background()

	h, ok, err := TryAcquire(ctx, dsn, pglockTestKey+3)
	if err != nil {
		t.Fatalf("TryAcquire() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("TryAcquire() ok = false, want true")
	}

	// Simulate the sensor's mutation directly on this handle: make Release
	// target a key this session never acquired. pglockTestKey+4 is
	// deliberately unheld by anyone, so pg_advisory_unlock for it must
	// report false.
	h.key = pglockTestKey + 4

	err = h.Release(context.Background())
	if err == nil {
		t.Fatal("Release() with a mismatched key returned nil error, want an error - this session never held that key")
	}

	// The real lock (pglockTestKey+3) is still technically held by Postgres
	// from this test's point of view, but the connection Release already
	// closed releases it via session teardown regardless of the explicit
	// unlock's outcome - confirm a fresh acquire of it succeeds.
	h2, ok2, err := TryAcquire(ctx, dsn, pglockTestKey+3)
	if err != nil {
		t.Fatalf("TryAcquire() after mismatched Release returned unexpected error: %v", err)
	}
	if !ok2 {
		t.Fatal("TryAcquire() after mismatched Release ok = false, want true - the session (and its lock) should be gone once the connection closed")
	}
	_ = h2.Release(context.Background())
}

// TestAcquire_SecondCallerBlocksUntilReleased proves the blocking variant's
// core mutual-exclusion contract (HA-15): a second Acquire for the same
// name only succeeds once the first caller releases.
func TestAcquire_SecondCallerBlocksUntilReleased(t *testing.T) {
	dsn := testDatabaseURL(t)
	name := "pglock-test-acquire-block"

	first, err := Acquire(context.Background(), dsn, name)
	if err != nil {
		t.Fatalf("first Acquire() returned unexpected error: %v", err)
	}

	acquired := make(chan *Handle, 1)
	errs := make(chan error, 1)
	go func() {
		h, err := Acquire(context.Background(), dsn, name)
		if err != nil {
			errs <- err
			return
		}
		acquired <- h
	}()

	select {
	case <-acquired:
		t.Fatal("second Acquire() succeeded while the first handle still holds the lock")
	case err := <-errs:
		t.Fatalf("second Acquire() returned unexpected error: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked.
	}

	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("first Release() returned unexpected error: %v", err)
	}

	select {
	case h := <-acquired:
		_ = h.Release(context.Background())
	case err := <-errs:
		t.Fatalf("second Acquire() returned unexpected error after first released: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second Acquire() did not succeed within 5s of the first releasing")
	}
}
