package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EmailVerificationToken tracks a single pending email-verification attempt
// for the SaaS public signup flow (T9/T10, multi-tenancy-core). TokenHash
// is always a hash of the verification token, never the raw token itself -
// same convention as PasswordResetToken/TenantInvite.
type EmailVerificationToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// EmailVerificationRepository accesses the email_verification_tokens table.
type EmailVerificationRepository struct {
	pool *Pool
}

// NewEmailVerificationRepository builds an EmailVerificationRepository
// backed by pool.
func NewEmailVerificationRepository(pool *Pool) *EmailVerificationRepository {
	return &EmailVerificationRepository{pool: pool}
}

// Create inserts token, filling in its generated ID and CreatedAt. Only the
// hash is ever written - callers must never pass a plaintext token in
// TokenHash.
func (r *EmailVerificationRepository) Create(ctx context.Context, token *EmailVerificationToken) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO email_verification_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING id, created_at",
		token.UserID, token.TokenHash, token.ExpiresAt,
	)

	if err := row.Scan(&token.ID, &token.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create email verification token: %w", err)
	}

	return nil
}

// ClaimForUse atomically finds an unused, unexpired token by tokenHash and
// marks it used in the same statement, returning ErrNotFound if no such
// token exists (covers "no such token", "already used", and "expired"
// alike) - same concurrency-safe shape as TenantInviteRepository.ClaimForUse,
// so two concurrent requests for the same verification link can't both
// succeed.
func (r *EmailVerificationRepository) ClaimForUse(ctx context.Context, tokenHash string) (*EmailVerificationToken, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE email_verification_tokens SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING id, user_id, token_hash, expires_at, used_at, created_at`,
		tokenHash,
	)

	var token EmailVerificationToken
	if err := row.Scan(&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.UsedAt, &token.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to claim email verification token: %w", err)
	}

	return &token, nil
}

// InvalidatePendingForUser marks every still-pending (unused) verification
// token for userID as used, so a resend (T11) never leaves more than one
// live token for the same user. No-op (not an error) if none are pending.
func (r *EmailVerificationRepository) InvalidatePendingForUser(ctx context.Context, userID string) error {
	if _, err := r.pool.Exec(ctx,
		"UPDATE email_verification_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL", userID,
	); err != nil {
		return fmt.Errorf("db: failed to invalidate pending email verification tokens: %w", err)
	}

	return nil
}
