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

// Owner, Operator, and Viewer are the 3 fixed roles (AD-003 - no
// configurable permission matrix). Since multi-tenancy-core (AD-022) a
// role is held per tenant, on tenant_memberships.role - never on the user
// themselves.
const (
	RoleOwner    = "owner"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("db: not found")

// ErrDuplicateEmail is returned when creating a user whose email already
// exists.
var ErrDuplicateEmail = errors.New("db: email already registered")

// User is a global dashboard identity: one email, one account, regardless
// of how many tenants it belongs to (multi-tenancy-core, AD-022). It
// carries no tenant_id and no role - membership and role both live on
// tenant_memberships.
type User struct {
	ID                string
	Email             string
	PasswordHash      string
	Name              string
	Phone             *string    // nil when the user never gave a phone number (optional field)
	SessionsRevokedAt *time.Time // nil = no session ever revoked; a JWT issued before this timestamp is rejected by RequireAuth
	EmailVerifiedAt   *time.Time // nil until the SaaS signup verification link is followed; bootstrap/invite-created users are verified on creation
	CreatedAt         time.Time
}

// UserRepository accesses the users table.
type UserRepository struct {
	pool *Pool
}

// NewUserRepository builds a UserRepository backed by pool.
func NewUserRepository(pool *Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts user, filling in its generated ID and CreatedAt. It
// returns ErrDuplicateEmail if the email is already registered.
// EmailVerifiedAt is written as given: a caller that has already proven
// the address (invite acceptance) passes a timestamp, the SaaS signup
// flow passes nil and verifies later.
func (r *UserRepository) Create(ctx context.Context, user *User) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, name, phone, email_verified_at) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at",
		user.Email, user.PasswordHash, user.Name, user.Phone, user.EmailVerifiedAt,
	)

	if err := row.Scan(&user.ID, &user.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateEmail
		}
		return fmt.Errorf("db: failed to create user: %w", err)
	}

	return nil
}

// GetByEmail looks up a user by email, returning ErrNotFound if none
// exists.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, email, password_hash, name, phone, sessions_revoked_at, email_verified_at, created_at FROM users WHERE email = $1",
		email,
	)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Phone,
		&user.SessionsRevokedAt, &user.EmailVerifiedAt, &user.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get user by email: %w", err)
	}

	return &user, nil
}

// GetByID looks up a user by ID, returning ErrNotFound if none exists.
// RequireAuth uses this to load the current SessionsRevokedAt for the user
// identified by a request's JWT.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, email, password_hash, name, phone, sessions_revoked_at, email_verified_at, created_at FROM users WHERE id = $1",
		id,
	)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Phone,
		&user.SessionsRevokedAt, &user.EmailVerifiedAt, &user.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get user by id: %w", err)
	}

	return &user, nil
}

// UpdatePasswordHash sets a new password hash for the user with the given
// ID, returning ErrNotFound if no such user exists.
func (r *UserRepository) UpdatePasswordHash(ctx context.Context, userID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE users SET password_hash = $1 WHERE id = $2", passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update user password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RevokeSessions marks every session currently issued for the user with
// the given ID as revoked (sets sessions_revoked_at to now), returning
// ErrNotFound if no such user exists. RequireAuth rejects any token whose
// iat predates this timestamp.
func (r *UserRepository) RevokeSessions(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE users SET sessions_revoked_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to revoke user sessions: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete removes the user with the given ID, returning ErrNotFound if no
// such user exists. Their tenant_memberships cascade with them.
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM users WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("db: failed to delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// BootstrapFirst creates user as the very first user in the table, or
// refuses (created=false, err=nil - not an error) if any user already
// exists. It opens its own transaction and takes LOCK TABLE users IN
// EXCLUSIVE MODE before counting, not SELECT ... FOR UPDATE: an empty
// table has no existing row to lock, so a row-level lock cannot prevent
// two concurrent callers from both observing zero and both inserting.
// The table-level lock serializes every concurrent BootstrapFirst call
// against this exact table, mirroring zeep-orbit's
// BootstrapFirstSuperadmin (internal/dashboard/store.go).
//
// The created user is already email-verified: self-hosted bootstrap is
// performed by the operator who owns the installation, so gating their
// own first login behind an email round trip (which needs an email
// provider they have not configured yet) would lock them out of the
// instance they just installed.
func (r *UserRepository) BootstrapFirst(ctx context.Context, user *User) (created bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("db: failed to begin bootstrap transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return false, fmt.Errorf("db: failed to lock users table for bootstrap: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false, fmt.Errorf("db: failed to count users for bootstrap: %w", err)
	}
	if count > 0 {
		// Rolled back by the deferred Rollback above - no state change,
		// same "no-op" contract as a user already existing.
		return false, nil
	}

	row := tx.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, name, phone, email_verified_at) VALUES ($1, $2, $3, $4, now()) RETURNING id, email_verified_at, created_at",
		user.Email, user.PasswordHash, user.Name, user.Phone,
	)
	if err := row.Scan(&user.ID, &user.EmailVerifiedAt, &user.CreatedAt); err != nil {
		return false, fmt.Errorf("db: failed to insert first user for bootstrap: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("db: failed to commit bootstrap transaction: %w", err)
	}

	return true, nil
}
