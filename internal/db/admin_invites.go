package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminInvite tracks a single admin invitation. TokenHash is always a hash
// of the invite token, never the raw token itself - the raw token exists
// only transiently in the request/response path and is never persisted
// (same convention as PasswordResetToken).
type AdminInvite struct {
	ID          string
	Email       string
	Role        string
	TokenHash   string
	InvitedByID string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
}

// AdminInviteRepository accesses the admin_invites table.
type AdminInviteRepository struct {
	pool *Pool
}

// NewAdminInviteRepository builds an AdminInviteRepository backed by pool.
func NewAdminInviteRepository(pool *Pool) *AdminInviteRepository {
	return &AdminInviteRepository{pool: pool}
}

// Create inserts invite, filling in its generated ID and CreatedAt. Only the
// hash is ever written - callers must never pass a plaintext token in
// TokenHash.
func (r *AdminInviteRepository) Create(ctx context.Context, invite *AdminInvite) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO admin_invites (email, role, token_hash, invited_by_id, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		invite.Email, invite.Role, invite.TokenHash, invite.InvitedByID, invite.ExpiresAt,
	)

	if err := row.Scan(&invite.ID, &invite.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create admin invite: %w", err)
	}

	return nil
}

// GetByTokenHash looks up an invite by its hash, returning ErrNotFound if
// none exists.
func (r *AdminInviteRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*AdminInvite, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at
		 FROM admin_invites WHERE token_hash = $1`,
		tokenHash,
	)

	var invite AdminInvite
	if err := row.Scan(
		&invite.ID, &invite.Email, &invite.Role, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get admin invite by hash: %w", err)
	}

	return &invite, nil
}

// ClaimForUse atomically finds an unused, unexpired invite by tokenHash and
// marks it used in the same statement, returning ErrNotFound if no such
// invite exists (covers "no such token", "already used", and "expired"
// alike - AcceptInvite's caller already treats all three identically).
// AcceptInvite used to do this as a separate GetByTokenHash SELECT, an
// in-Go used_at/expiry check, and a later unconditional MarkUsed (L24) -
// two concurrent requests for the same token could both pass the Go-side
// check before either called MarkUsed, both proceeding to create an admin
// account from a single-use invite. The WHERE clause here makes the claim
// itself the concurrency gate: only one concurrent UPDATE can match a given
// row, so only one caller ever gets a non-ErrNotFound result back.
func (r *AdminInviteRepository) ClaimForUse(ctx context.Context, tokenHash string) (*AdminInvite, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE admin_invites SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at`,
		tokenHash,
	)

	var invite AdminInvite
	if err := row.Scan(
		&invite.ID, &invite.Email, &invite.Role, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to claim admin invite: %w", err)
	}

	return &invite, nil
}

// Refresh atomically replaces an invite's token hash and expiry, provided
// the invite hasn't already been accepted/canceled (used_at IS NULL). Same
// atomic-guard shape as ClaimForUse: only one concurrent Refresh/Cancel call
// on the same id can match the row, so a losing caller gets ErrNotFound
// instead of silently overwriting a settled invite.
func (r *AdminInviteRepository) Refresh(ctx context.Context, id, newTokenHash string, newExpiresAt time.Time) (*AdminInvite, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE admin_invites SET token_hash = $2, expires_at = $3
		 WHERE id = $1 AND used_at IS NULL
		 RETURNING id, email, role, token_hash, invited_by_id, expires_at, used_at, created_at`,
		id, newTokenHash, newExpiresAt,
	)

	var invite AdminInvite
	if err := row.Scan(
		&invite.ID, &invite.Email, &invite.Role, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to refresh admin invite: %w", err)
	}

	return &invite, nil
}

// Cancel atomically marks an invite used (so its token becomes permanently
// unacceptable) without creating an admin account for it, returning
// ErrNotFound if no unused invite with the given id exists.
func (r *AdminInviteRepository) Cancel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE admin_invites SET used_at = now() WHERE id = $1 AND used_at IS NULL", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to cancel admin invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkUsed sets used_at on the invite with the given ID to now, returning
// ErrNotFound if no such invite exists.
func (r *AdminInviteRepository) MarkUsed(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE admin_invites SET used_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark admin invite used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List returns every pending invite - not used and not expired - most
// recent first. TokenHash is never selected: the raw list is exposed via
// the admins API and must never leak the hash needed to accept an invite.
func (r *AdminInviteRepository) List(ctx context.Context) ([]AdminInvite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, email, role, invited_by_id, expires_at, created_at
		 FROM admin_invites
		 WHERE used_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list admin invites: %w", err)
	}
	defer rows.Close()

	var invites []AdminInvite
	for rows.Next() {
		var invite AdminInvite
		if err := rows.Scan(
			&invite.ID, &invite.Email, &invite.Role,
			&invite.InvitedByID, &invite.ExpiresAt, &invite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("db: failed to scan admin invite: %w", err)
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list admin invites: %w", err)
	}

	return invites, nil
}

// InvalidatePendingForEmail marks every still-pending (unused) invite for
// email as used, so a new invite can be created for the same address
// without leaving more than one live token. It is a no-op (not an error) if
// no pending invite exists for email.
func (r *AdminInviteRepository) InvalidatePendingForEmail(ctx context.Context, email string) error {
	if _, err := r.pool.Exec(ctx,
		"UPDATE admin_invites SET used_at = now() WHERE email = $1 AND used_at IS NULL", email,
	); err != nil {
		return fmt.Errorf("db: failed to invalidate pending admin invites for email: %w", err)
	}

	return nil
}
