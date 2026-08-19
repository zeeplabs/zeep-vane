//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newPasswordResetRepositoryForTest(t *testing.T) (*PasswordResetRepository, *AdminRepository, *Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewPasswordResetRepository(pool), NewAdminRepository(pool), pool
}

func createTestAdminForReset(t *testing.T, admins *AdminRepository, pool *Pool) *Admin {
	t.Helper()
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &Admin{Email: email, PasswordHash: "hash"}
	if err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	return admin
}

func TestPasswordResetRepository_Create_Success(t *testing.T) {
	repo, admins, pool := newPasswordResetRepositoryForTest(t)
	admin := createTestAdminForReset(t, admins, pool)
	ctx := context.Background()

	token := &PasswordResetToken{
		AdminID:   admin.ID,
		TokenHash: "hash-" + admin.ID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id = $1", admin.ID) })

	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if token.ID == "" {
		t.Error("Create() did not populate ID")
	}
}

func TestPasswordResetRepository_GetByTokenHash_Existing_ReturnsToken(t *testing.T) {
	repo, admins, pool := newPasswordResetRepositoryForTest(t)
	admin := createTestAdminForReset(t, admins, pool)
	ctx := context.Background()

	expiresAt := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	created := &PasswordResetToken{
		AdminID:   admin.ID,
		TokenHash: "hash-" + admin.ID,
		ExpiresAt: expiresAt,
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id = $1", admin.ID) })

	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, created.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash() returned unexpected error: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("GetByTokenHash() ID = %q, want %q", got.ID, created.ID)
	}
	if got.AdminID != admin.ID {
		t.Errorf("GetByTokenHash() AdminID = %q, want %q", got.AdminID, admin.ID)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("GetByTokenHash() ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
	if got.UsedAt != nil {
		t.Errorf("GetByTokenHash() UsedAt = %v, want nil (unused)", got.UsedAt)
	}
}

func TestPasswordResetRepository_GetByTokenHash_Missing_ErrNotFound(t *testing.T) {
	repo, _, _ := newPasswordResetRepositoryForTest(t)

	_, err := repo.GetByTokenHash(context.Background(), "does-not-exist-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByTokenHash() error = %v, want ErrNotFound", err)
	}
}

func TestPasswordResetRepository_MarkUsed_SetsUsedAt(t *testing.T) {
	repo, admins, pool := newPasswordResetRepositoryForTest(t)
	admin := createTestAdminForReset(t, admins, pool)
	ctx := context.Background()

	token := &PasswordResetToken{
		AdminID:   admin.ID,
		TokenHash: "hash-" + admin.ID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM password_reset_tokens WHERE admin_id = $1", admin.ID) })

	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := repo.MarkUsed(ctx, token.ID); err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, token.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash() returned unexpected error: %v", err)
	}
	if got.UsedAt == nil {
		t.Error("GetByTokenHash() UsedAt = nil after MarkUsed(), want non-nil")
	}
}
