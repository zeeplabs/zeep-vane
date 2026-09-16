package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TwoFactorChallenge is a server-side row backing a signed 2FA challenge
// token's one-time-use and revocability semantics (auth-2fa-totp
// design.md) - a bare JWT can't express "already consumed" on its own.
type TwoFactorChallenge struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// TwoFactorChallengeRepository accesses the two_factor_challenges table.
type TwoFactorChallengeRepository struct {
	pool *Pool
}

// NewTwoFactorChallengeRepository builds a TwoFactorChallengeRepository
// backed by pool.
func NewTwoFactorChallengeRepository(pool *Pool) *TwoFactorChallengeRepository {
	return &TwoFactorChallengeRepository{pool: pool}
}

// Create inserts a new challenge row for userID, expiring after ttl, and
// returns its generated ID (embedded as the challenge JWT's jti claim) and
// expiry.
func (r *TwoFactorChallengeRepository) Create(ctx context.Context, userID string, ttl time.Duration) (jti string, expiresAt time.Time, err error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO two_factor_challenges (user_id, expires_at)
		 VALUES ($1, now() + make_interval(secs => $2))
		 RETURNING id, expires_at`,
		userID, ttl.Seconds(),
	)
	if err := row.Scan(&jti, &expiresAt); err != nil {
		return "", time.Time{}, fmt.Errorf("db: failed to create 2FA challenge: %w", err)
	}
	return jti, expiresAt, nil
}

// Lookup returns the challenge row identified by jti, or ErrNotFound if
// none exists. It is read-only - a wrong-code retry must not consume the
// row (spec.md AC4), so callers check ExpiresAt/UsedAt themselves before
// deciding whether to call MarkUsed.
func (r *TwoFactorChallengeRepository) Lookup(ctx context.Context, jti string) (*TwoFactorChallenge, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, user_id, expires_at, used_at, created_at FROM two_factor_challenges WHERE id = $1",
		jti,
	)

	var c TwoFactorChallenge
	if err := row.Scan(&c.ID, &c.UserID, &c.ExpiresAt, &c.UsedAt, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to look up 2FA challenge: %w", err)
	}

	return &c, nil
}

// MarkUsed atomically claims the challenge row identified by jti - the
// one-shot claim that closes the concurrent-double-verify race (design.md
// Risks & Concerns). It returns ok=false (no error) if the row was already
// consumed, expired, or does not exist. Callers must only call MarkUsed
// after the submitted code/recovery_code has already validated - a wrong
// code must never reach this method (AC4: token stays usable for retry).
func (r *TwoFactorChallengeRepository) MarkUsed(ctx context.Context, jti string) (userID string, ok bool, err error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE two_factor_challenges
		 SET used_at = now()
		 WHERE id = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING user_id`,
		jti,
	)
	if err := row.Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("db: failed to mark 2FA challenge used: %w", err)
	}
	return userID, true, nil
}
