//go:build integration

package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
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

	// Tests in this file create an admin via createTestAdminForInvite,
	// and AdminRepository.Create always inserts with the `admins.role`
	// column's database default (owner, migration 0009) - see
	// LockAdminsTable's doc comment for why this must be held across
	// concurrently-run packages. Deliberately context.Background(), not
	// the bounded `ctx` above, which is canceled by the deferred cancel()
	// as soon as this function returns.
	dbtest.LockAdminsTable(t, context.Background(), dsn)

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

func TestAdminInviteRepository_List_ReturnsPendingIncludingExpiredMostRecentFirst(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()

	pendingOlder := &AdminInvite{
		Email: uniqueTestEmail(t), Role: "operator", TokenHash: "hash-" + uniqueTestEmail(t),
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, pendingOlder); err != nil {
		t.Fatalf("Create() pendingOlder returned unexpected error: %v", err)
	}
	pendingNewer := &AdminInvite{
		Email: uniqueTestEmail(t), Role: "viewer", TokenHash: "hash-" + uniqueTestEmail(t),
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if err := repo.Create(ctx, pendingNewer); err != nil {
		t.Fatalf("Create() pendingNewer returned unexpected error: %v", err)
	}
	used := &AdminInvite{
		Email: uniqueTestEmail(t), Role: "operator", TokenHash: "hash-" + uniqueTestEmail(t),
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, used); err != nil {
		t.Fatalf("Create() used returned unexpected error: %v", err)
	}
	if err := repo.MarkUsed(ctx, used.ID); err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}
	expired := &AdminInvite{
		Email: uniqueTestEmail(t), Role: "operator", TokenHash: "hash-" + uniqueTestEmail(t),
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO admin_invites (id, email, role, token_hash, invited_by_id, expires_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)`,
		expired.Email, expired.Role, expired.TokenHash, expired.InvitedByID, expired.ExpiresAt,
	); err != nil {
		t.Fatalf("insert expired invite returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email IN ($1, $2, $3, $4)",
			pendingOlder.Email, pendingNewer.Email, used.Email, expired.Email)
	})

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}

	var gotEmails []string
	for _, invite := range got {
		gotEmails = append(gotEmails, invite.Email)
		if invite.TokenHash != "" {
			t.Errorf("List() invite %q leaked TokenHash, want empty", invite.Email)
		}
	}

	foundOlder, foundNewer, foundExpired := false, false, false
	newerIdx, olderIdx := -1, -1
	for i, email := range gotEmails {
		if email == used.Email {
			t.Error("List() included a used invite, want excluded")
		}
		if email == expired.Email {
			foundExpired = true
		}
		if email == pendingOlder.Email {
			foundOlder = true
			olderIdx = i
		}
		if email == pendingNewer.Email {
			foundNewer = true
			newerIdx = i
		}
	}
	if !foundOlder {
		t.Error("List() did not include pendingOlder invite")
	}
	if !foundNewer {
		t.Error("List() did not include pendingNewer invite")
	}
	if !foundExpired {
		t.Error("List() did not include expired-but-unused invite, want included (spec P2)")
	}
	if foundOlder && foundNewer && newerIdx > olderIdx {
		t.Errorf("List() order = %v, want pendingNewer (created after) before pendingOlder", gotEmails)
	}
}

func TestAdminInviteRepository_Refresh_Success(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-old-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	newHash := "hash-new-" + email
	newExpiresAt := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	got, err := repo.Refresh(ctx, invite.ID, newHash, newExpiresAt)
	if err != nil {
		t.Fatalf("Refresh() returned unexpected error: %v", err)
	}
	if got.TokenHash != newHash {
		t.Errorf("Refresh() TokenHash = %q, want %q", got.TokenHash, newHash)
	}
	if !got.ExpiresAt.Equal(newExpiresAt) {
		t.Errorf("Refresh() ExpiresAt = %v, want %v", got.ExpiresAt, newExpiresAt)
	}

	if _, err := repo.GetByTokenHash(ctx, invite.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByTokenHash(old hash) after Refresh() error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByTokenHash(ctx, newHash); err != nil {
		t.Errorf("GetByTokenHash(new hash) after Refresh() returned unexpected error: %v", err)
	}
}

func TestAdminInviteRepository_Refresh_UnknownID_ErrNotFound(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	_, err := repo.Refresh(context.Background(), "00000000-0000-0000-0000-000000000000", "irrelevant-hash", time.Now().Add(1*time.Hour))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Refresh() error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Refresh_AlreadyAccepted_ErrNotFound(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := repo.MarkUsed(ctx, invite.ID); err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}

	_, err := repo.Refresh(ctx, invite.ID, "hash-new-"+email, time.Now().Add(1*time.Hour))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Refresh() on already-accepted invite error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Refresh_AlreadyCanceled_ErrNotFound(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := repo.Cancel(ctx, invite.ID); err != nil {
		t.Fatalf("Cancel() returned unexpected error: %v", err)
	}

	_, err := repo.Refresh(ctx, invite.ID, "hash-new-"+email, time.Now().Add(1*time.Hour))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Refresh() on already-canceled invite error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Refresh_MalformedID_ErrNotFound(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	_, err := repo.Refresh(context.Background(), "not-a-uuid", "irrelevant-hash", time.Now().Add(1*time.Hour))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Refresh() with malformed id error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Cancel_AlreadyAccepted_ErrNotFound(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := repo.MarkUsed(ctx, invite.ID); err != nil {
		t.Fatalf("MarkUsed() returned unexpected error: %v", err)
	}

	if err := repo.Cancel(ctx, invite.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel() on already-accepted invite error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Cancel_AlreadyCanceled_ErrNotFound(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := repo.Cancel(ctx, invite.ID); err != nil {
		t.Fatalf("first Cancel() returned unexpected error: %v", err)
	}

	if err := repo.Cancel(ctx, invite.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel() on already-canceled invite error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Cancel_MalformedID_ErrNotFound(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	err := repo.Cancel(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel() with malformed id error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_Cancel_Success(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := repo.Cancel(ctx, invite.ID); err != nil {
		t.Fatalf("Cancel() returned unexpected error: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, invite.TokenHash)
	if err != nil {
		t.Fatalf("GetByTokenHash() returned unexpected error: %v", err)
	}
	if got.UsedAt == nil {
		t.Error("Cancel() did not set UsedAt")
	}
}

func TestAdminInviteRepository_Cancel_UnknownID_ErrNotFound(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	err := repo.Cancel(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel() error = %v, want ErrNotFound", err)
	}
}

func TestAdminInviteRepository_RefreshCancel_Concurrent_OnlyOneSucceeds(t *testing.T) {
	repo, admins, pool := newAdminInviteRepositoryForTest(t)
	inviter := createTestAdminForInvite(t, admins, pool)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admin_invites WHERE email = $1", email) })

	invite := &AdminInvite{
		Email: email, Role: "operator", TokenHash: "hash-" + email,
		InvitedByID: inviter.ID, ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := repo.Create(ctx, invite); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = repo.Refresh(ctx, invite.ID, "hash-race-new-"+email, time.Now().Add(1*time.Hour))
	}()
	go func() {
		defer wg.Done()
		errs[1] = repo.Cancel(ctx, invite.ID)
	}()
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrNotFound) {
			t.Errorf("concurrent Refresh/Cancel returned unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("concurrent Refresh/Cancel successes = %d, want exactly 1", successes)
	}
}

func TestAdminInviteRepository_InvalidatePendingForEmail_NoPending_NoError(t *testing.T) {
	repo, _, _ := newAdminInviteRepositoryForTest(t)

	if err := repo.InvalidatePendingForEmail(context.Background(), "no-such-invite-"+uniqueTestEmail(t)); err != nil {
		t.Errorf("InvalidatePendingForEmail() with no pending invite returned unexpected error: %v", err)
	}
}
