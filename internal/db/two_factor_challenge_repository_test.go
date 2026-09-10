//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTwoFactorChallengeRepoTestPool(t *testing.T) (*TwoFactorChallengeRepository, *Pool) {
	t.Helper()
	pool, _ := newTenantScopedPool(t)
	return NewTwoFactorChallengeRepository(pool), pool
}

func TestTwoFactorChallengeRepository_Create_InsertsRowWithTTL(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)

	before := time.Now()
	jti, expiresAt, err := repo.Create(context.Background(), userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if jti == "" {
		t.Error("Create() jti is empty, want a generated ID")
	}
	wantExpiry := before.Add(5 * time.Minute)
	if expiresAt.Before(wantExpiry.Add(-2*time.Second)) || expiresAt.After(wantExpiry.Add(2*time.Second)) {
		t.Errorf("Create() expiresAt = %v, want approximately %v (±2s)", expiresAt, wantExpiry)
	}
}

func TestTwoFactorChallengeRepository_Lookup_Existing_DoesNotConsume(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	jti, _, err := repo.Create(ctx, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	got, err := repo.Lookup(ctx, jti)
	if err != nil {
		t.Fatalf("Lookup() returned unexpected error: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("Lookup() UserID = %q, want %q", got.UserID, userID)
	}
	if got.UsedAt != nil {
		t.Errorf("Lookup() UsedAt = %v, want nil (a wrong-code retry must not consume the row)", got.UsedAt)
	}

	// Lookup again, then confirm MarkUsed still succeeds - proving the
	// first Lookup truly did not mutate the row.
	claimedUserID, ok, err := repo.MarkUsed(ctx, jti)
	if err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}
	if !ok || claimedUserID != userID {
		t.Fatalf("MarkUsed() after a prior Lookup() = (%q, %v), want (%q, true)", claimedUserID, ok, userID)
	}
}

func TestTwoFactorChallengeRepository_Lookup_Unknown_ErrNotFound(t *testing.T) {
	repo, _ := newTwoFactorChallengeRepoTestPool(t)

	_, err := repo.Lookup(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup() error = %v, want ErrNotFound", err)
	}
}

func TestTwoFactorChallengeRepository_MarkUsed_ValidRow_SucceedsOnce(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	jti, _, err := repo.Create(ctx, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	claimedUserID, ok, err := repo.MarkUsed(ctx, jti)
	if err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("MarkUsed() ok = false, want true for an unused, unexpired row")
	}
	if claimedUserID != userID {
		t.Errorf("MarkUsed() userID = %q, want %q", claimedUserID, userID)
	}
}

func TestTwoFactorChallengeRepository_MarkUsed_SecondCall_ReturnsFalseNoError(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	jti, _, err := repo.Create(ctx, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if _, ok, err := repo.MarkUsed(ctx, jti); err != nil || !ok {
		t.Fatalf("first MarkUsed() = (ok=%v, err=%v), want (true, nil)", ok, err)
	}

	_, ok, err := repo.MarkUsed(ctx, jti)
	if err != nil {
		t.Fatalf("second MarkUsed() returned unexpected error: %v, want nil", err)
	}
	if ok {
		t.Error("second MarkUsed() on an already-consumed row = true, want false")
	}
}

func TestTwoFactorChallengeRepository_MarkUsed_ExpiredRow_ReturnsFalseNoError(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	jti, _, err := repo.Create(ctx, userID, -1*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	_, ok, err := repo.MarkUsed(ctx, jti)
	if err != nil {
		t.Fatalf("MarkUsed() on an expired row returned unexpected error: %v, want nil", err)
	}
	if ok {
		t.Error("MarkUsed() on an expired row = true, want false")
	}
}

func TestTwoFactorChallengeRepository_MarkUsed_Unknown_ReturnsFalseNoError(t *testing.T) {
	repo, _ := newTwoFactorChallengeRepoTestPool(t)

	_, ok, err := repo.MarkUsed(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("MarkUsed() on an unknown row returned unexpected error: %v, want nil", err)
	}
	if ok {
		t.Error("MarkUsed() on an unknown row = true, want false")
	}
}

// TestTwoFactorChallengeRepository_ConcurrentMarkUsed_ExactlyOneWins proves
// real contention, not a same-goroutine sequential call (same discipline as
// status-page-domain-attach's corrected concurrency test, per this task's
// own Done-when): a holder transaction takes the row lock MarkUsed's UPDATE
// would need, and stays open while a real MarkUsed call runs concurrently
// in a goroutine. Because the holder genuinely holds the row lock, this
// proves the second call cannot complete until the holder releases it, and
// that on release it observes the row is already used and returns
// ok=false - not a lost update letting both calls "win".
func TestTwoFactorChallengeRepository_ConcurrentMarkUsed_ExactlyOneWins(t *testing.T) {
	repo, pool := newTwoFactorChallengeRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	jti, _, err := repo.Create(ctx, userID, 5*time.Minute)
	if err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	var lockedUsedAt *time.Time
	if err := holderTx.QueryRow(ctx,
		"SELECT used_at FROM two_factor_challenges WHERE id = $1 FOR UPDATE", jti,
	).Scan(&lockedUsedAt); err != nil {
		t.Fatalf("holder SELECT ... FOR UPDATE failed: %v", err)
	}
	if lockedUsedAt != nil {
		t.Fatalf("holder locked used_at = %v, want nil before either MarkUsed call", lockedUsedAt)
	}
	if _, err := holderTx.Exec(ctx,
		"UPDATE two_factor_challenges SET used_at = now() WHERE id = $1", jti,
	); err != nil {
		t.Fatalf("holder UPDATE failed: %v", err)
	}

	type result struct {
		userID string
		ok     bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		u, ok, err := repo.MarkUsed(context.Background(), jti)
		done <- result{u, ok, err}
	}()

	select {
	case r := <-done:
		t.Fatalf("MarkUsed() returned (userID=%q, ok=%v, err=%v) while the holder transaction was still open - the row lock did not block it", r.userID, r.ok, r.err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked behind the holder's uncommitted row lock.
	}

	if err := holderTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit holder transaction: %v", err)
	}

	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("MarkUsed() did not return after the holder transaction committed")
	}

	if r.err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", r.err)
	}
	if r.ok {
		t.Error("MarkUsed() concurrent with a holder that already claimed the row = true, want false (exactly one claim must win)")
	}
}
