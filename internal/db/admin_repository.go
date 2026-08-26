package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Owner, Operator, and Viewer are the 3 fixed admin roles (AD-003 - no
// configurable permission matrix).
const (
	RoleOwner    = "owner"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("db: not found")

// ErrDuplicateEmail is returned when creating an admin whose email already
// exists.
var ErrDuplicateEmail = errors.New("db: email already registered")

// Admin is a registered dashboard administrator.
type Admin struct {
	ID                string
	Email             string
	PasswordHash      string
	Role              string     // "owner" | "operator" | "viewer" - see Role* constants
	SessionsRevokedAt *time.Time // nil = no session ever revoked; a JWT issued before this timestamp is rejected by RequireAuth
	CreatedAt         time.Time
}

// AdminRepository accesses the admins table.
type AdminRepository struct {
	pool *Pool
}

// NewAdminRepository builds an AdminRepository backed by pool.
func NewAdminRepository(pool *Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

// Create inserts admin, filling in its generated ID and CreatedAt. It
// returns ErrDuplicateEmail if the email is already registered.
func (r *AdminRepository) Create(ctx context.Context, admin *Admin) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO admins (email, password_hash) VALUES ($1, $2) RETURNING id, created_at",
		admin.Email, admin.PasswordHash,
	)

	if err := row.Scan(&admin.ID, &admin.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateEmail
		}
		return fmt.Errorf("db: failed to create admin: %w", err)
	}

	return nil
}

// CreateWithRole inserts admin with role set atomically in the same INSERT,
// filling in its generated ID, Role, and CreatedAt. It returns
// ErrDuplicateEmail if the email is already registered. Unlike Create
// (which always lands on the admins.role column's default) followed by a
// separate UpdateRole call, this is one statement - AcceptInvite (M12) used
// to do Create-then-UpdateRole as two non-transactional steps, so a crash
// or error between them left an admin permanently stuck on the default role
// (reportedly owner) instead of the invite's actual intended role.
func (r *AdminRepository) CreateWithRole(ctx context.Context, admin *Admin, role string) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO admins (email, password_hash, role) VALUES ($1, $2, $3) RETURNING id, created_at",
		admin.Email, admin.PasswordHash, role,
	)

	if err := row.Scan(&admin.ID, &admin.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateEmail
		}
		return fmt.Errorf("db: failed to create admin with role: %w", err)
	}
	admin.Role = role

	return nil
}

// GetByEmail looks up an admin by email, returning ErrNotFound if none
// exists.
func (r *AdminRepository) GetByEmail(ctx context.Context, email string) (*Admin, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, email, password_hash, created_at FROM admins WHERE email = $1",
		email,
	)

	var admin Admin
	if err := row.Scan(&admin.ID, &admin.Email, &admin.PasswordHash, &admin.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get admin by email: %w", err)
	}

	return &admin, nil
}

// UpdatePasswordHash sets a new password hash for the admin with the given
// ID, returning ErrNotFound if no such admin exists.
func (r *AdminRepository) UpdatePasswordHash(ctx context.Context, adminID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE admins SET password_hash = $1 WHERE id = $2", passwordHash, adminID,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update admin password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// GetByID looks up an admin by ID, returning ErrNotFound if none exists.
// RequireAuth (T5) uses this to load the current Role and
// SessionsRevokedAt for the admin identified by a request's JWT.
func (r *AdminRepository) GetByID(ctx context.Context, id string) (*Admin, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, email, password_hash, role, sessions_revoked_at, created_at FROM admins WHERE id = $1",
		id,
	)

	var admin Admin
	if err := row.Scan(&admin.ID, &admin.Email, &admin.PasswordHash, &admin.Role, &admin.SessionsRevokedAt, &admin.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get admin by id: %w", err)
	}

	return &admin, nil
}

// UpdateRole sets a new role for the admin with the given ID, returning
// ErrNotFound if no such admin exists.
func (r *AdminRepository) UpdateRole(ctx context.Context, id, role string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE admins SET role = $1 WHERE id = $2", role, id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update admin role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RevokeSessions marks every session currently issued for the admin with
// the given ID as revoked (sets sessions_revoked_at to now), returning
// ErrNotFound if no such admin exists. RequireAuth (T5) rejects any token
// whose iat predates this timestamp.
func (r *AdminRepository) RevokeSessions(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE admins SET sessions_revoked_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to revoke admin sessions: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete removes the admin with the given ID, returning ErrNotFound if no
// such admin exists.
func (r *AdminRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM admins WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("db: failed to delete admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// CountActiveOwners counts admins currently holding the owner role, using
// SELECT ... FOR UPDATE on the underlying rows so the count is safe to base
// a lockout-prevention decision on: called within tx, the owner rows stay
// locked until tx commits or rolls back, so no concurrent role change or
// removal can slip in between this count and the caller's UPDATE/DELETE and
// drop the active-owner count to zero. PostgreSQL rejects FOR UPDATE
// combined directly with an aggregate, so the lock is taken in a subquery
// and counted in the outer query.
func (r *AdminRepository) CountActiveOwners(ctx context.Context, tx pgx.Tx) (int, error) {
	row := tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM (SELECT id FROM admins WHERE role = $1 FOR UPDATE) locked_owners",
		RoleOwner,
	)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("db: failed to count active owners: %w", err)
	}

	return count, nil
}

// BootstrapFirst creates admin as the very first admin in the table, or
// refuses (created=false, err=nil - not an error) if any admin already
// exists. It opens its own transaction and takes LOCK TABLE admins IN
// EXCLUSIVE MODE before counting, not SELECT ... FOR UPDATE: an empty
// table has no existing row to lock, so a row-level lock cannot prevent
// two concurrent callers from both observing zero and both inserting.
// The table-level lock serializes every concurrent BootstrapFirst call
// against this exact table, mirroring zeep-orbit's
// BootstrapFirstSuperadmin (internal/dashboard/store.go).
func (r *AdminRepository) BootstrapFirst(ctx context.Context, admin *Admin) (created bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("db: failed to begin bootstrap transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "LOCK TABLE admins IN EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("db: failed to lock admins table for bootstrap: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return false, fmt.Errorf("db: failed to count admins for bootstrap: %w", err)
	}
	if count > 0 {
		// Rolled back by the deferred Rollback above - no state change,
		// same "no-op" contract as an admin already existing.
		return false, nil
	}

	row := tx.QueryRow(ctx,
		"INSERT INTO admins (email, password_hash) VALUES ($1, $2) RETURNING id, created_at",
		admin.Email, admin.PasswordHash,
	)
	if err := row.Scan(&admin.ID, &admin.CreatedAt); err != nil {
		return false, fmt.Errorf("db: failed to insert first admin for bootstrap: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("db: failed to commit bootstrap transaction: %w", err)
	}

	return true, nil
}
