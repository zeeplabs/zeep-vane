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
