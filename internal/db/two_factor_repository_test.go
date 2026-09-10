//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/auth"
)

// newTwoFactorRepoTestPool boots a migrated pool and a fresh
// *TwoFactorRepository backed by it.
func newTwoFactorRepoTestPool(t *testing.T) (*TwoFactorRepository, *Pool) {
	t.Helper()
	pool, _ := newTenantScopedPool(t)
	return NewTwoFactorRepository(pool), pool
}

// seedTwoFactorTestUser inserts and returns the id of a throwaway user row
// - two_factor_secrets/two_factor_recovery_codes FK against users(id), and
// users carries no tenant_id (identity is global, migration 0024).
func seedTwoFactorTestUser(t *testing.T, pool *Pool) string {
	t.Helper()
	ctx := context.Background()

	var id string
	email := fmt.Sprintf("two-factor-repo-test-%d@example.com", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		email, "irrelevant-hash",
	).Scan(&id); err != nil {
		t.Fatalf("seeding fixture user returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id) })

	return id
}

func TestTwoFactorRepository_CreatePendingSecret_GetSecret_RoundTrip(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-1")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}

	got, err := repo.GetSecret(ctx, userID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if string(got.EncryptedSecret) != "ciphertext-1" {
		t.Errorf("EncryptedSecret = %q, want %q", got.EncryptedSecret, "ciphertext-1")
	}
	if got.EnabledAt != nil {
		t.Errorf("EnabledAt = %v, want nil (pending, unconfirmed)", got.EnabledAt)
	}
}

func TestTwoFactorRepository_CreatePendingSecret_OverwritesPreviousUnconfirmed(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-old")); err != nil {
		t.Fatalf("first CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-new")); err != nil {
		t.Fatalf("second CreatePendingSecret() returned unexpected error: %v", err)
	}

	got, err := repo.GetSecret(ctx, userID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if string(got.EncryptedSecret) != "ciphertext-new" {
		t.Errorf("EncryptedSecret = %q, want %q (overwritten by second enroll)", got.EncryptedSecret, "ciphertext-new")
	}
}

func TestTwoFactorRepository_GetSecret_Unknown_ErrNotFound(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)

	_, err := repo.GetSecret(context.Background(), userID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret() error = %v, want ErrNotFound", err)
	}
}

func TestTwoFactorRepository_ConfirmSecret_PendingSecret_SetsEnabledAt(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-1")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := repo.ConfirmSecret(ctx, userID); err != nil {
		t.Fatalf("ConfirmSecret() returned unexpected error: %v", err)
	}

	got, err := repo.GetSecret(ctx, userID)
	if err != nil {
		t.Fatalf("GetSecret() returned unexpected error: %v", err)
	}
	if got.EnabledAt == nil {
		t.Error("EnabledAt = nil, want a timestamp after ConfirmSecret")
	}
}

func TestTwoFactorRepository_ConfirmSecret_NoRow_ErrNotFound(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)

	err := repo.ConfirmSecret(context.Background(), userID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmSecret() error = %v, want ErrNotFound", err)
	}
}

func TestTwoFactorRepository_ConfirmSecret_AlreadyConfirmed_ErrNotFound(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-1")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := repo.ConfirmSecret(ctx, userID); err != nil {
		t.Fatalf("first ConfirmSecret() returned unexpected error: %v", err)
	}

	err := repo.ConfirmSecret(ctx, userID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second ConfirmSecret() error = %v, want ErrNotFound (already confirmed, WHERE enabled_at IS NULL matches 0 rows)", err)
	}
}

func TestTwoFactorRepository_DeleteSecret_Existing_Removes(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	if err := repo.CreatePendingSecret(ctx, userID, []byte("ciphertext-1")); err != nil {
		t.Fatalf("CreatePendingSecret() returned unexpected error: %v", err)
	}
	if err := repo.DeleteSecret(ctx, userID); err != nil {
		t.Fatalf("DeleteSecret() returned unexpected error: %v", err)
	}

	_, err := repo.GetSecret(ctx, userID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret() after DeleteSecret() error = %v, want ErrNotFound", err)
	}
}

func TestTwoFactorRepository_DeleteSecret_NoRow_Idempotent(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)

	if err := repo.DeleteSecret(context.Background(), userID); err != nil {
		t.Fatalf("DeleteSecret() with no existing row returned unexpected error: %v, want nil (idempotent)", err)
	}
}

// hashRecoveryCodes hashes each plaintext code with the same bcrypt
// primitive PasswordHash uses (a recovery code is a credential, per
// design.md).
func hashRecoveryCodes(t *testing.T, codes []string) []string {
	t.Helper()
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hash, err := auth.HashPassword(c)
		if err != nil {
			t.Fatalf("HashPassword() returned unexpected error: %v", err)
		}
		hashes[i] = hash
	}
	return hashes
}

func TestTwoFactorRepository_CreateRecoveryCodes_ReplacesPriorBatch(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	oldHashes := hashRecoveryCodes(t, []string{"old-code-1", "old-code-2"})
	if err := repo.CreateRecoveryCodes(ctx, userID, oldHashes); err != nil {
		t.Fatalf("first CreateRecoveryCodes() returned unexpected error: %v", err)
	}

	newHashes := hashRecoveryCodes(t, []string{"new-code-1", "new-code-2", "new-code-3"})
	if err := repo.CreateRecoveryCodes(ctx, userID, newHashes); err != nil {
		t.Fatalf("second CreateRecoveryCodes() returned unexpected error: %v", err)
	}

	// The old batch must be gone: consuming an old code must fail.
	ok, err := repo.ConsumeRecoveryCode(ctx, userID, "old-code-1")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode(old) returned unexpected error: %v", err)
	}
	if ok {
		t.Error("ConsumeRecoveryCode(old code from replaced batch) = true, want false")
	}

	// A code from the new batch must still work.
	ok, err = repo.ConsumeRecoveryCode(ctx, userID, "new-code-1")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode(new) returned unexpected error: %v", err)
	}
	if !ok {
		t.Error("ConsumeRecoveryCode(new code from current batch) = false, want true")
	}
}

func TestTwoFactorRepository_ConsumeRecoveryCode_ValidUnused_ConsumesOnce(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	hashes := hashRecoveryCodes(t, []string{"recovery-code-1"})
	if err := repo.CreateRecoveryCodes(ctx, userID, hashes); err != nil {
		t.Fatalf("CreateRecoveryCodes() returned unexpected error: %v", err)
	}

	ok, err := repo.ConsumeRecoveryCode(ctx, userID, "recovery-code-1")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ConsumeRecoveryCode() = false, want true for a valid unused code")
	}
}

func TestTwoFactorRepository_ConsumeRecoveryCode_Reused_Rejected(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	hashes := hashRecoveryCodes(t, []string{"recovery-code-1"})
	if err := repo.CreateRecoveryCodes(ctx, userID, hashes); err != nil {
		t.Fatalf("CreateRecoveryCodes() returned unexpected error: %v", err)
	}

	if ok, err := repo.ConsumeRecoveryCode(ctx, userID, "recovery-code-1"); err != nil || !ok {
		t.Fatalf("first ConsumeRecoveryCode() = (%v, %v), want (true, nil)", ok, err)
	}

	ok, err := repo.ConsumeRecoveryCode(ctx, userID, "recovery-code-1")
	if err != nil {
		t.Fatalf("second ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if ok {
		t.Error("second ConsumeRecoveryCode() with the same, already-used code = true, want false")
	}
}

func TestTwoFactorRepository_ConsumeRecoveryCode_WrongCode_ReturnsFalse(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	hashes := hashRecoveryCodes(t, []string{"recovery-code-1"})
	if err := repo.CreateRecoveryCodes(ctx, userID, hashes); err != nil {
		t.Fatalf("CreateRecoveryCodes() returned unexpected error: %v", err)
	}

	ok, err := repo.ConsumeRecoveryCode(ctx, userID, "not-a-real-code")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if ok {
		t.Error("ConsumeRecoveryCode(wrong code) = true, want false")
	}
}

func TestTwoFactorRepository_DeleteRecoveryCodes_RemovesAll(t *testing.T) {
	repo, pool := newTwoFactorRepoTestPool(t)
	userID := seedTwoFactorTestUser(t, pool)
	ctx := context.Background()

	hashes := hashRecoveryCodes(t, []string{"recovery-code-1", "recovery-code-2"})
	if err := repo.CreateRecoveryCodes(ctx, userID, hashes); err != nil {
		t.Fatalf("CreateRecoveryCodes() returned unexpected error: %v", err)
	}
	if err := repo.DeleteRecoveryCodes(ctx, userID); err != nil {
		t.Fatalf("DeleteRecoveryCodes() returned unexpected error: %v", err)
	}

	ok, err := repo.ConsumeRecoveryCode(ctx, userID, "recovery-code-1")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode() returned unexpected error: %v", err)
	}
	if ok {
		t.Error("ConsumeRecoveryCode() after DeleteRecoveryCodes() = true, want false")
	}
}
