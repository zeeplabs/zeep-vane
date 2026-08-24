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

	// This test's before/after counts are only valid if the admins table
	// isn't concurrently bulk-cleared or repopulated by another package's
	// bootstrap tests - see LockAdminsTable's doc comment.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

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

// rawRow captures every column of a row (as text, via a query the caller
// supplies) so it can be re-inserted byte-for-byte later.
type rawRow struct {
	values []any
}

// snapshotAndClearAdmins captures every row currently in the shared
// admins table AND the two tables with a foreign key into it
// (admin_invites.invited_by_id, password_reset_tokens.admin_id - admins'
// own Referenced-by list, confirmed via pg_constraint), deletes all three
// in FK-safe order, and returns a restore function that deletes whatever
// the test itself inserted and re-inserts every original row exactly as
// it was (children last, since they reference admins by id).
//
// BootstrapFirst's whole contract is "the table has zero admins" - a
// precondition this codebase's other test suites do not maintain (several
// existing tests create admin rows without a matching cleanup, and some
// invite/token rows reference them). Without this snapshot/restore, a
// BootstrapFirst test asserting created=true would be at the mercy of
// however many admins happen to already exist in the shared
// TEST_DATABASE_URL database at the moment it runs - and a naive
// "DELETE FROM admins" alone fails outright on the foreign key
// violation once any invite/token row references an existing admin.
func snapshotAndClearAdmins(t *testing.T, pool *Pool) func() {
	t.Helper()
	ctx := context.Background()

	// Serialize against every other package's tests that bulk-clear or
	// exact-count the shared `admins` table - see LockAdminsTable's doc
	// comment for why this is needed across concurrently-run packages.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

	invites := snapshotTable(t, pool, ctx,
		"SELECT id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at FROM admin_invites")
	tokens := snapshotTable(t, pool, ctx,
		"SELECT id, admin_id, token_hash, expires_at, used_at FROM password_reset_tokens")
	admins := snapshotTable(t, pool, ctx,
		"SELECT id, email, password_hash, role, sessions_revoked_at, created_at FROM admins")

	clearAll := func() {
		if _, err := pool.Exec(ctx, "DELETE FROM admin_invites"); err != nil {
			t.Fatalf("failed to clear admin_invites: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM password_reset_tokens"); err != nil {
			t.Fatalf("failed to clear password_reset_tokens: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM admins"); err != nil {
			t.Fatalf("failed to clear admins table for BootstrapFirst test: %v", err)
		}
	}
	clearAll()

	return func() {
		clearAll()
		for _, a := range admins {
			_, err := pool.Exec(ctx,
				"INSERT INTO admins (id, email, password_hash, role, sessions_revoked_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
				a.values...,
			)
			if err != nil {
				t.Fatalf("failed to restore snapshotted admin: %v", err)
			}
		}
		for _, inv := range invites {
			_, err := pool.Exec(ctx,
				"INSERT INTO admin_invites (id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
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
// values, so snapshotAndClearAdmins can restore them after the test.
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

func TestAdminRepository_BootstrapFirst_EmptyTable_CreatesAndReturnsTrue(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	restore := snapshotAndClearAdmins(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	admin := &Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	created, err := repo.BootstrapFirst(ctx, admin)
	if err != nil {
		t.Fatalf("BootstrapFirst() returned unexpected error: %v", err)
	}
	if !created {
		t.Fatal("BootstrapFirst() created = false on an admin-less table, want true")
	}
	if admin.ID == "" {
		t.Error("BootstrapFirst() did not populate ID")
	}

	got, err := repo.GetByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetByID() after BootstrapFirst() returned unexpected error: %v", err)
	}
	if got.Role != RoleOwner {
		t.Errorf("created admin Role = %q, want %q (column default)", got.Role, RoleOwner)
	}
}

func TestAdminRepository_BootstrapFirst_AdminAlreadyExists_ReturnsFalseNoSecondAdmin(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	restore := snapshotAndClearAdmins(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	first := &Admin{Email: uniqueTestEmail(t), PasswordHash: "hash-1"}
	created, err := repo.BootstrapFirst(ctx, first)
	if err != nil || !created {
		t.Fatalf("first BootstrapFirst() = (created=%v, err=%v), want (true, nil)", created, err)
	}

	second := &Admin{Email: uniqueTestEmail(t), PasswordHash: "hash-2"}
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
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after refused bootstrap = %d, want 1", count)
	}
}

// TestAdminRepository_BootstrapFirst_ConcurrentCalls_RealLockContention
// proves the LOCK TABLE admins IN EXCLUSIVE MODE actually serializes
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
func TestAdminRepository_BootstrapFirst_ConcurrentCalls_RealLockContention(t *testing.T) {
	repo, pool := newAdminRepositoryForTest(t)
	restore := snapshotAndClearAdmins(t, pool)
	t.Cleanup(restore)
	ctx := context.Background()

	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	if _, err := holderTx.Exec(ctx, "LOCK TABLE admins IN EXCLUSIVE MODE"); err != nil {
		t.Fatalf("holder LOCK TABLE failed: %v", err)
	}

	holderEmail := uniqueTestEmail(t)
	row := holderTx.QueryRow(ctx,
		"INSERT INTO admins (email, password_hash) VALUES ($1, $2) RETURNING id",
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
		created, err := repo.BootstrapFirst(context.Background(), &Admin{Email: racingEmail, PasswordHash: "racing-hash"})
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
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		t.Fatalf("counting admins returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admins row count after contended bootstrap = %d, want 1 (only the holder's admin, no double-create)", count)
	}
	if _, err := repo.GetByEmail(ctx, racingEmail); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByEmail() for the losing racer's email = %v, want ErrNotFound", err)
	}
}
