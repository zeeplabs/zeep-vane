//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func newUserRepositoryForTest(t *testing.T) (*UserRepository, *Pool) {
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

	// Every test in this file goes through this constructor, and
	// Creating identity rows here races other packages' bulk clears of the
	// shared `users` table -
	// regardless of whether the individual test cares about role at all.
	// `go test ./...` runs internal/db, internal/api, and internal/cli as
	// separate concurrent processes against the same TEST_DATABASE_URL,
	// so any of these tests can otherwise race another package's
	// owner-count-sensitive test. Centralizing the lock here (idempotent
	// per *testing.T - see LockUsersTable's doc comment) means every
	// test in this file is covered without each one having to remember
	// to take it individually. Note: deliberately passed
	// context.Background(), not the bounded `ctx` above, which is
	// canceled by the deferred cancel() as soon as this function
	// returns - the lock's dedicated connection must outlive it.
	dbtest.LockUsersTable(t, context.Background(), dsn)

	return NewUserRepository(pool), pool
}

func uniqueTestEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("admin-repo-test-%d@example.com", time.Now().UnixNano())
}

func TestUserRepository_Create_Success(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	admin := &User{Email: email, PasswordHash: "hash"}
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

func TestUserRepository_Create_DuplicateEmail_ErrDuplicateEmail(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	first := &User{Email: email, PasswordHash: "hash-1"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create() returned unexpected error: %v", err)
	}

	second := &User{Email: email, PasswordHash: "hash-2"}
	err := repo.Create(ctx, second)
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("second Create() error = %v, want ErrDuplicateEmail", err)
	}
}

func TestUserRepository_GetByEmail_Existing_ReturnsAdmin(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	created := &User{Email: email, PasswordHash: "hash"}
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

func TestUserRepository_GetByEmail_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newUserRepositoryForTest(t)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "does-not-exist-"+uniqueTestEmail(t))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByID_Existing_ReturnsUserWithRevocationState(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	created := &User{Email: email, PasswordHash: "hash"}
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
	if got.EmailVerifiedAt != nil {
		t.Errorf("GetByID() EmailVerifiedAt = %v, want nil (Create left it unset)", got.EmailVerifiedAt)
	}
	if got.SessionsRevokedAt != nil {
		t.Errorf("GetByID() SessionsRevokedAt = %v, want nil (never revoked)", got.SessionsRevokedAt)
	}
}

func TestUserRepository_GetByID_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newUserRepositoryForTest(t)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

// Role is no longer a users column - it moved to tenant_memberships
// (multi-tenancy-core, AD-022). The behaviour the two former
// TestUserRepository_UpdateRole_* tests covered (a role change persists;
// an unknown target is ErrNotFound) is covered against the table that
// owns it now, by TestTenantMembershipRepository_UpdateRole_PersistsRole
// and TestTenantMembershipRepository_UpdateRole_Unknown_ErrNotFound.

func TestUserRepository_RevokeSessions_SetsSessionsRevokedAt(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	admin := &User{Email: email, PasswordHash: "hash"}
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

func TestUserRepository_RevokeSessions_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newUserRepositoryForTest(t)

	err := repo.RevokeSessions(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("RevokeSessions() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete_RemovesAdmin(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	ctx := context.Background()
	email := uniqueTestEmail(t)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email) })

	admin := &User{Email: email, PasswordHash: "hash"}
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

func TestUserRepository_Delete_Missing_ErrNotFound(t *testing.T) {
	repo, _ := newUserRepositoryForTest(t)

	err := repo.Delete(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

// CountActiveOwners is gone with the users.role column: the "never leave
// zero owners" decision is per tenant now and lives in
// TenantMembershipRepository (isLastOwner + ErrLastOwner), covered by
// TestTenantMembershipRepository_Delete_LastOwner_ErrLastOwner and
// TestTenantMembershipRepository_UpdateRole_LastOwnerDemotion_ErrLastOwner.

// rawRow captures every column of a row (as text, via a query the caller
// supplies) so it can be re-inserted byte-for-byte later.
type rawRow struct {
	values []any
}

// snapshotAndClearUsers captures every row currently in the shared
// admins table AND the two tables with a foreign key into it
// (tenant_invites.invited_by_id, password_reset_tokens.user_id - users'
// own Referenced-by list, confirmed via pg_constraint), deletes all three
// in FK-safe order, and returns a restore function that deletes whatever
// the test itself inserted and re-inserts every original row exactly as
// it was (children last, since they reference users by id).
//
// BootstrapFirst's whole contract is "the table has zero users" - a
// precondition this codebase's other test suites do not maintain (several
// existing tests create user rows without a matching cleanup, and some
// invite/token rows reference them). Without this snapshot/restore, a
// BootstrapFirst test asserting created=true would be at the mercy of
// however many users happen to already exist in the shared
// TEST_DATABASE_URL database at the moment it runs - and a naive
// "DELETE FROM users" alone fails outright on the foreign key
// violation once any invite/token row references an existing user.
func snapshotAndClearUsers(t *testing.T, pool *Pool) func() {
	t.Helper()
	ctx := context.Background()

	// Serialize against every other package's tests that bulk-clear or
	// exact-count the shared `users` table - see LockUsersTable's doc
	// comment for why this is needed across concurrently-run packages.
	dbtest.LockUsersTable(t, ctx, testDatabaseURL(t))

	invites := snapshotTable(t, pool, ctx,
		"SELECT id, tenant_id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at FROM tenant_invites")
	tokens := snapshotTable(t, pool, ctx,
		"SELECT id, user_id, token_hash, expires_at, used_at FROM password_reset_tokens")
	admins := snapshotTable(t, pool, ctx,
		"SELECT id, email, password_hash, sessions_revoked_at, email_verified_at, created_at FROM users")

	clearAll := func() {
		if _, err := pool.Exec(ctx, "DELETE FROM tenant_invites"); err != nil {
			t.Fatalf("failed to clear tenant_invites: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM password_reset_tokens"); err != nil {
			t.Fatalf("failed to clear password_reset_tokens: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM users"); err != nil {
			t.Fatalf("failed to clear admins table for BootstrapFirst test: %v", err)
		}
	}
	clearAll()

	return func() {
		clearAll()
		for _, a := range admins {
			_, err := pool.Exec(ctx,
				"INSERT INTO users (id, email, password_hash, role, sessions_revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
				a.values...,
			)
			if err != nil {
				t.Fatalf("failed to restore snapshotted admin: %v", err)
			}
		}
		for _, inv := range invites {
			_, err := pool.Exec(ctx,
				"INSERT INTO tenant_invites (id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
				inv.values...,
			)
			if err != nil {
				t.Fatalf("failed to restore snapshotted admin_invite: %v", err)
			}
		}
		for _, tok := range tokens {
			_, err := pool.Exec(ctx,
				"INSERT INTO password_reset_tokens (id, admin_id, token_hash, expires_at, used_at) VALUES ($1, $2, $3, $4, $5)",
				tok.values...,
			)
			if err != nil {
				t.Fatalf("failed to restore snapshotted password_reset_token: %v", err)
			}
		}
	}
}

// snapshotTable runs query (a plain SELECT of the exact columns the
// caller will later re-INSERT in the same order) and returns every row's
// values, so snapshotAndClearUsers can restore them after the test.
func snapshotTable(t *testing.T, pool *Pool, ctx context.Context, query string) []rawRow {
	t.Helper()

	rows, err := pool.Query(ctx, query)
	if err != nil {
		t.Fatalf("failed to snapshot table (%s): %v", query, err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var saved []rawRow
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatalf("failed to scan snapshotted row (%s): %v", query, err)
		}
		if len(values) != len(fields) {
			t.Fatalf("snapshotted row column count = %d, want %d", len(values), len(fields))
		}
		saved = append(saved, rawRow{values: values})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed while iterating snapshotted rows (%s): %v", query, err)
	}
	return saved
}

func TestUserRepository_BootstrapFirst_EmptyTable_CreatesAndReturnsTrue(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	restore := snapshotAndClearUsers(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	admin := &User{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	created, err := repo.BootstrapFirst(ctx, admin)
	if err != nil {
		t.Fatalf("BootstrapFirst() returned unexpected error: %v", err)
	}
	if !created {
		t.Fatal("BootstrapFirst() created = false on a user-less table, want true")
	}
	if admin.ID == "" {
		t.Error("BootstrapFirst() did not populate ID")
	}

	got, err := repo.GetByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetByID() after BootstrapFirst() returned unexpected error: %v", err)
	}
	if got.EmailVerifiedAt == nil {
		t.Error("bootstrapped user EmailVerifiedAt = nil, want a timestamp (self-hosted bootstrap must not require email verification)")
	}
}

func TestUserRepository_BootstrapFirst_AdminAlreadyExists_ReturnsFalseNoSecondAdmin(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	restore := snapshotAndClearUsers(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	first := &User{Email: uniqueTestEmail(t), PasswordHash: "hash-1"}
	created, err := repo.BootstrapFirst(ctx, first)
	if err != nil || !created {
		t.Fatalf("first BootstrapFirst() = (created=%v, err=%v), want (true, nil)", created, err)
	}

	second := &User{Email: uniqueTestEmail(t), PasswordHash: "hash-2"}
	created, err = repo.BootstrapFirst(ctx, second)
	if err != nil {
		t.Fatalf("second BootstrapFirst() returned unexpected error: %v", err)
	}
	if created {
		t.Fatal("second BootstrapFirst() created = true against a table that already has an admin, want false")
	}

	if _, err := repo.GetByEmail(ctx, second.Email); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail() for the refused bootstrap email = %v, want ErrNotFound (no second admin created)", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after refused bootstrap = %d, want 1", count)
	}
}

// TestUserRepository_BootstrapFirst_ConcurrentCalls_RealLockContention
// proves the LOCK TABLE users IN EXCLUSIVE MODE actually serializes
// concurrent bootstrap attempts, rather than merely happening to pass
// because the two calls never truly overlapped. Per the
// status-page-domain-attach lesson (two bare goroutines racing a
// pool-backed call can pass 20/20 even with the lock removed, because the
// first transaction commits before the second even starts): this test
// drives contention deterministically with an explicit "holder"
// transaction that takes the real table lock and stays open
// (uncommitted) while a real BootstrapFirst call runs concurrently in a
// goroutine, proving that call cannot complete until the holder releases
// the lock.
func TestUserRepository_BootstrapFirst_ConcurrentCalls_RealLockContention(t *testing.T) {
	repo, pool := newUserRepositoryForTest(t)
	restore := snapshotAndClearUsers(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	if _, err := holderTx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		t.Fatalf("holder LOCK TABLE failed: %v", err)
	}

	holderEmail := uniqueTestEmail(t)
	row := holderTx.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		holderEmail, "holder-hash",
	)
	var holderID string
	if err := row.Scan(&holderID); err != nil {
		t.Fatalf("holder INSERT failed: %v", err)
	}

	// Run the real BootstrapFirst call while holderTx is still open and
	// uncommitted. With the production LOCK TABLE in place, this second
	// call cannot even complete its own LOCK TABLE statement until
	// holderTx releases the table lock below.
	type result struct {
		created bool
		err     error
	}
	done := make(chan result, 1)
	racingEmail := uniqueTestEmail(t)
	go func() {
		created, err := repo.BootstrapFirst(context.Background(), &User{Email: racingEmail, PasswordHash: "racing-hash"})
		done <- result{created: created, err: err}
	}()

	select {
	case r := <-done:
		t.Fatalf("BootstrapFirst() returned (created=%v, err=%v) while the holder transaction was still open - LOCK TABLE did not block it", r.created, r.err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked behind the holder's uncommitted table lock.
	}

	if err := holderTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit holder transaction: %v", err)
	}

	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BootstrapFirst() did not return after the holder transaction committed")
	}

	if r.err != nil {
		t.Fatalf("BootstrapFirst() after contention returned unexpected error: %v", r.err)
	}
	if r.created {
		t.Fatal("BootstrapFirst() created = true after the holder already committed the first admin, want false")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after contended bootstrap = %d, want 1 (only the holder's admin, no double-create)", count)
	}
	if _, err := repo.GetByEmail(ctx, racingEmail); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail() for the losing racer's email = %v, want ErrNotFound", err)
	}
}
