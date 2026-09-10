package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/auth"
)

// TwoFactorSecret is a user's TOTP secret, encrypted at rest via
// internal/crypto.Encrypt (auth-2fa-totp design.md). EnabledAt is nil until
// the enrollment-confirm step succeeds.
type TwoFactorSecret struct {
	UserID          string
	EncryptedSecret []byte
	EnabledAt       *time.Time
	CreatedAt       time.Time
}

// TwoFactorRepository accesses the two_factor_secrets and
// two_factor_recovery_codes tables.
type TwoFactorRepository struct {
	pool *Pool
}

// NewTwoFactorRepository builds a TwoFactorRepository backed by pool.
func NewTwoFactorRepository(pool *Pool) *TwoFactorRepository {
	return &TwoFactorRepository{pool: pool}
}

// CreatePendingSecret upserts a two_factor_secrets row for userID with
// enabled_at reset to NULL - a fresh enroll always overwrites any previous
// unconfirmed secret (spec.md Edge Cases), and the "already enabled -> 409"
// rule (AC4) is enforced by the caller checking GetSecret first, not by this
// method.
func (r *TwoFactorRepository) CreatePendingSecret(ctx context.Context, userID string, encryptedSecret []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO two_factor_secrets (user_id, encrypted_secret, enabled_at)
		 VALUES ($1, $2, NULL)
		 ON CONFLICT (user_id) DO UPDATE SET
		   encrypted_secret = EXCLUDED.encrypted_secret,
		   enabled_at = NULL`,
		userID, encryptedSecret,
	)
	if err != nil {
		return fmt.Errorf("db: failed to create pending 2FA secret: %w", err)
	}
	return nil
}

// GetSecret returns userID's two_factor_secrets row, or ErrNotFound if none
// exists.
func (r *TwoFactorRepository) GetSecret(ctx context.Context, userID string) (*TwoFactorSecret, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT user_id, encrypted_secret, enabled_at, created_at
		 FROM two_factor_secrets WHERE user_id = $1`,
		userID,
	)

	var s TwoFactorSecret
	if err := row.Scan(&s.UserID, &s.EncryptedSecret, &s.EnabledAt, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get 2FA secret: %w", err)
	}

	return &s, nil
}

// ConfirmSecret sets enabled_at = now() for userID's pending secret, only if
// it was still NULL. Returns ErrNotFound if no row matched (no pending
// secret, or one already confirmed).
func (r *TwoFactorRepository) ConfirmSecret(ctx context.Context, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE two_factor_secrets SET enabled_at = now() WHERE user_id = $1 AND enabled_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("db: failed to confirm 2FA secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSecret removes userID's two_factor_secrets row, idempotently - no
// error if none exists (auth-2fa-totp Disable2FA's no-op-when-not-enabled
// requirement, AC3).
func (r *TwoFactorRepository) DeleteSecret(ctx context.Context, userID string) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM two_factor_secrets WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("db: failed to delete 2FA secret: %w", err)
	}
	return nil
}

// CreateRecoveryCodes replaces any existing recovery codes for userID with a
// fresh batch of hashes, atomically (delete-then-insert in one transaction)
// - every `confirm` issues a brand new batch (spec.md Assumptions).
func (r *TwoFactorRepository) CreateRecoveryCodes(ctx context.Context, userID string, hashes []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin create-recovery-codes transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM two_factor_recovery_codes WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("db: failed to clear prior recovery codes: %w", err)
	}

	for _, hash := range hashes {
		if _, err := tx.Exec(ctx,
			"INSERT INTO two_factor_recovery_codes (user_id, code_hash) VALUES ($1, $2)",
			userID, hash,
		); err != nil {
			return fmt.Errorf("db: failed to insert recovery code: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit create-recovery-codes transaction: %w", err)
	}
	return nil
}

// ConsumeRecoveryCode checks code against every unused recovery-code hash
// stored for userID (bounded at 10 rows - no timing/DoS concern), and on a
// match atomically claims that row (UPDATE ... WHERE used_at IS NULL
// RETURNING, closing the double-submit race the same way
// TwoFactorChallengeRepository.MarkUsed does). Returns (true, nil) on a
// successful single-use consumption, (false, nil) if no unused code
// matches.
func (r *TwoFactorRepository) ConsumeRecoveryCode(ctx context.Context, userID, code string) (bool, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, code_hash FROM two_factor_recovery_codes WHERE user_id = $1 AND used_at IS NULL",
		userID,
	)
	if err != nil {
		return false, fmt.Errorf("db: failed to list unused recovery codes: %w", err)
	}

	type candidate struct {
		id   string
		hash string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.hash); err != nil {
			rows.Close()
			return false, fmt.Errorf("db: failed to scan recovery code: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("db: failed to iterate recovery codes: %w", err)
	}
	rows.Close()

	for _, c := range candidates {
		if !auth.VerifyPassword(c.hash, code) {
			continue
		}

		var claimedID string
		row := r.pool.QueryRow(ctx,
			"UPDATE two_factor_recovery_codes SET used_at = now() WHERE id = $1 AND used_at IS NULL RETURNING id",
			c.id,
		)
		if err := row.Scan(&claimedID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Already consumed by a concurrent request between the
				// SELECT above and this claim - treat as no match, not an
				// error (same one-shot semantics as
				// TwoFactorChallengeRepository.MarkUsed).
				return false, nil
			}
			return false, fmt.Errorf("db: failed to claim recovery code: %w", err)
		}
		return true, nil
	}

	return false, nil
}

// DeleteRecoveryCodes removes every recovery code for userID.
func (r *TwoFactorRepository) DeleteRecoveryCodes(ctx context.Context, userID string) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM two_factor_recovery_codes WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("db: failed to delete recovery codes: %w", err)
	}
	return nil
}
