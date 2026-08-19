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

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("db: not found")

// ErrDuplicateEmail is returned when creating an admin whose email already
// exists.
var ErrDuplicateEmail = errors.New("db: email already registered")

// Admin is a registered dashboard administrator.
type Admin struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
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
