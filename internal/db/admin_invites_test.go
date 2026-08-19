//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newAdminInviteRepositoryForTest(t *testing.T) (*AdminInviteRepository, *AdminRepository, *Pool) {
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

	return NewAdminInviteRepository(pool), NewAdminRepository(pool), pool
}

func createTestAdminForInvite(t *testing.T, admins *AdminRepository, pool *Pool) *Admin {
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

func TestAdminInviteRepository_Create_Success(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email:       email,
		Role:        "operator",
		TokenHash:   "hash-" + email,
		InvitedByID: inviter.ID,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if invite.ID == "" {
		t.Error("Create() did not populate ID")
	}
	if invite.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}
}

func TestAdminInviteRepository_GetByTokenHash_Existing_ReturnsInvite(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	expiresAt := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	created := &AdminInvite{
		Email:       email,
		Role:        "viewer",
		TokenHash:   "hash-" + email,
		InvitedByID: inviter.ID,
		ExpiresAt:   expiresAt,
	}
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
	if got.Email != email {
		t.Errorf("GetByTokenHash() Email = %q, want %q", got.Email, email)
	}
	if got.Role != "viewer" {
		t.Errorf("GetByTokenHash() Role = %q, want %q", got.Role, "viewer")
	}
	if got.InvitedByID != inviter.ID {
		t.Errorf("GetByTokenHash() InvitedByID = %q, want %q", got.InvitedByID, inviter.ID)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("GetByTokenHash() ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
	if got.UsedAt != nil {
		t.Errorf("GetByTokenHash() UsedAt = %v, want nil (unused)", got.UsedAt)
	}
}

func TestAdminInviteRepository_GetByTokenHash_Missing_ErrNotFound(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	_, err := repo.GetByTokenHash(context.Background(), "does-not-exist-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByTokenHash() error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_MarkUsed_SetsUsedAt(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email:       email,
		Role:        "operator",
		TokenHash:   "hash-" + email,
		InvitedByID: inviter.ID,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := repo.MarkUsed(ctx, invite.ID); err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, invite.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash() returned unexpected error: %v", err)
	}
	if got.UsedAt == nil {
		t.Error("GetByTokenHash() UsedAt = nil after MarkUsed(), want non-nil")
	}
}

func TestAdminInviteRepository_InvalidatePendingForEmail_MarksPendingUsed_LeavesOthersAlone(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	otherEmail := uniqueTestEmail(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email IN ($1, $2)", email, otherEmail)
	})

	pending := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-pending-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, pending); err != nil {
		t.Fatalf("Create() pending returned unexpected error: %v", err)
	}

	otherAdminInvite := &AdminInvite{
		Email: otherEmail, Role: "operator", TokenHash: "hash-other-" + otherEmail,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, otherAdminInvite); err != nil {
		t.Fatalf("Create() other returned unexpected error: %v", err)
	}

	if err := repo.InvalidatePendingForEmail(ctx, email); err != nil {
		t.Fatalf("InvalidatePendingForEmail() returned unexpected error: %v", err)
	}

	gotPending, err := repo.GetByTokenHash(ctx, pending.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash(pending) returned unexpected error: %v", err)
	}
	if gotPending.UsedAt == nil {
		t.Error("InvalidatePendingForEmail() did not mark the matching-email invite as used")
	}

	gotOther, err := repo.GetByTokenHash(ctx, otherAdminInvite.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash(other) returned unexpected error: %v", err)
	}
	if gotOther.UsedAt != nil {
		t.Error("InvalidatePendingForEmail() incorrectly marked a different email's invite as used")
	}
}

func TestAdminInviteRepository_InvalidatePendingForEmail_NoPending_NoError(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	if err := repo.InvalidatePendingForEmail(context.Background(), "no-such-invite-"+uniqueTestEmail(t)); err != nil {
		t.Errorf("InvalidatePendingForEmail() with no pending invite returned unexpected error: %v", err)
	}
}
