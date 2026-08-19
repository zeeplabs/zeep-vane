package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PasswordResetToken tracks a single password reset attempt. TokenHash is
// always a hash of the reset token, never the raw token itself - the raw
// token exists only transiently in the request/response path (T14) and is
// never persisted.
type PasswordResetToken struct {
	ID        string
	AdminID   string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// PasswordResetRepository accesses the password_reset_tokens table.
type PasswordResetRepository struct {
	pool *Pool
}

// NewPasswordResetRepository builds a PasswordResetRepository backed by
// pool.
func NewPasswordResetRepository(pool *Pool) *PasswordResetRepository {
	return &PasswordResetRepository{pool: pool}
}

// Create inserts token, filling in its generated ID. Only the hash is ever
// written - callers must never pass a plaintext token in TokenHash.
func (r *PasswordResetRepository) Create(ctx context.Context, token *PasswordResetToken) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO password_reset_tokens (admin_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING id",
		token.AdminID, token.TokenHash, token.ExpiresAt,
	)

	if err := row.Scan(&token.ID); err != nil {
		return fmt.Errorf("db: failed to create password reset token: %w", err)
	}

	return nil
}

// GetByTokenHash looks up a reset token by its hash, returning ErrNotFound
// if none exists.
func (r *PasswordResetRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, admin_id, token_hash, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1",
		tokenHash,
	)

	var token PasswordResetToken
	if err := row.Scan(&token.ID, &token.AdminID, &token.TokenHash, &token.ExpiresAt, &token.UsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get password reset token by hash: %w", err)
	}

	return &token, nil
}

// MarkUsed sets used_at on the token with the given ID to now.
func (r *PasswordResetRepository) MarkUsed(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE password_reset_tokens SET used_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark password reset token used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
