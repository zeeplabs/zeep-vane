//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newAdminRepositoryForTest(t *testing.T) (*AdminRepository, *Pool) {
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

	return NewAdminRepository(pool), pool
}

func uniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("admin-repo-test-%d@example.com", time.Now().UnixNano())
}

func TestAdminRepository_Create_Success(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if admin.ID == "" {
		t.Error("Create() did not populate ID")
	}
	if admin.CreatedAt.IsZero() {
		t.Error("Create() did not populate CreatedAt")
	}
}

func TestAdminRepository_Create_DuplicateEmail_ErrDuplicateEmail(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	first := &Admin{Email: email, PasswordHash: "hash-1"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create() returned unexpected error: %v", err)
	}

	second := &Admin{Email: email, PasswordHash: "hash-2"}
	err := repo.Create(ctx, second)
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("second Create() error = %v, want ErrDuplicateEmail", err)
	}
}

func TestAdminRepository_GetByEmail_Existing_ReturnsAdmin(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	created := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	got, err := repo.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() returned unexpected error: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("GetByEmail() ID = %q, want %q", got.ID, created.ID)
	}
	if got.Email != email {
		t.Errorf("GetByEmail() Email = %q, want %q", got.Email, email)
	}
	if got.PasswordHash != "hash" {
		t.Errorf("GetByEmail() PasswordHash = %q, want %q", got.PasswordHash, "hash")
	}
}

func TestAdminRepository_GetByEmail_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newAdminRepositoryForTest(t)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "does-not-exist-"+uniqueTestEmail(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_GetByID_Existing_ReturnsAdminWithRoleAndRevocation(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	created := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetByID() ID = %q, want %q", got.ID, created.ID)
	}
	if got.Role != RoleOwner {
		t.Errorf("GetByID() Role = %q, want %q (column default)", got.Role, RoleOwner)
	}
	if got.SessionsRevokedAt != nil {
		t.Errorf("GetByID() SessionsRevokedAt = %v, want nil (never revoked)", got.SessionsRevokedAt)
	}
}

func TestAdminRepository_GetByID_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newAdminRepositoryForTest(t)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_UpdateRole_PersistsNewRole(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := repo.UpdateRole(ctx, admin.ID, RoleOperator); err != nil {
		t.Fatalf("UpdateRole() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.Role != RoleOperator {
		t.Errorf("Role after UpdateRole() = %q, want %q", got.Role, RoleOperator)
	}
}

func TestAdminRepository_UpdateRole_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newAdminRepositoryForTest(t)

	err := repo.UpdateRole(context.Background(), "00000000-0000-0000-0000-000000000000", RoleViewer)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateRole() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_RevokeSessions_SetsSessionsRevokedAt(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	before := time.Now().Add(-1 * time.Second)
	if err := repo.RevokeSessions(ctx, admin.ID); err != nil {
		t.Fatalf("RevokeSessions() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.SessionsRevokedAt == nil {
		t.Fatal("SessionsRevokedAt after RevokeSessions() = nil, want a timestamp")
	}
	if got.SessionsRevokedAt.Before(before) {
		t.Errorf("SessionsRevokedAt = %v, want a timestamp at or after %v", got.SessionsRevokedAt, before)
	}
}

func TestAdminRepository_RevokeSessions_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newAdminRepositoryForTest(t)

	err := repo.RevokeSessions(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("RevokeSessions() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_Delete_RemovesAdmin(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email) })

	admin := &Admin{Email: email, PasswordHash: "hash"}
	if err := repo.Create(ctx, admin); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := repo.Delete(ctx, admin.ID); err != nil {
		t.Fatalf("Delete() returned unexpected error: %v", err)
	}

	_, err := repo.GetByID(ctx, admin.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() after Delete() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_Delete_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newAdminRepositoryForTest(t)

	err := repo.Delete(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestAdminRepository_CountActiveOwners_CountsOnlyOwners(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	ctx := context.Background()

	ownerEmail := uniqueTestEmail(t)
	operatorEmail := uniqueTestEmail(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email IN ($1, $2)", ownerEmail, operatorEmail)
	})

	before, err := countOwners(ctx, pool)
	if err != nil {
		t.Fatalf("countOwners() before returned unexpected error: %v", err)
	}

	owner := &Admin{Email: ownerEmail, PasswordHash: "hash"}
	if err := repo.Create(ctx, owner); err != nil {
		t.Fatalf("Create() owner returned unexpected error: %v", err)
	}
	operator := &Admin{Email: operatorEmail, PasswordHash: "hash"}
	if err := repo.Create(ctx, operator); err != nil {
		t.Fatalf("Create() operator returned unexpected error: %v", err)
	}
	if err := repo.UpdateRole(ctx, operator.ID, RoleOperator); err != nil {
		t.Fatalf("UpdateRole() returned unexpected error: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	got, err := repo.CountActiveOwners(ctx, tx)
	if err != nil {
		t.Fatalf("CountActiveOwners() returned unexpected error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit() returned unexpected error: %v", err)
	}

	// This test's own owner row is 1 of however many owners already exist
	// in the shared test database (e.g. an admin created by another
	// test's Create() call, which defaults to owner) - so it asserts the
	// exact delta this test introduced (+1 owner, +0 from the operator),
	// not an absolute count.
	if got != before+1 {
		t.Errorf("CountActiveOwners() = %d, want %d (before=%d, +1 for the owner created here, operator excluded)", got, before+1, before)
	}
}

// countOwners is a direct, non-locking count used only to establish the
// baseline in TestAdminRepository_CountActiveOwners_CountsOnlyOwners.
func countOwners(ctx context.Context, pool *Pool) (int, error) {
	var count int
	row := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admins WHERE role = $1", RoleOwner)
	err := row.Scan(&count)
	return count, err
}
