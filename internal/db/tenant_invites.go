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

// TenantInvite tracks a single invitation to join a tenant. It replaces
// admin_invites (multi-tenancy-core, AD-022): same token-hash + TTL
// mechanism, plus the tenant the invite grants membership of. TokenHash is
// always a hash of the invite token, never the raw token itself - the raw
// token exists only transiently in the request/response path and is never
// persisted (same convention as PasswordResetToken).
type TenantInvite struct {
	ID          string
	TenantID    string
	Email       string
	Role        string
	Name        string
	Phone       *string // nil when the inviter left phone blank (optional field)
	TokenHash   string
	InvitedByID string
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CreatedAt   time.Time
}

// TenantInviteRepository accesses the tenant_invites table.
type TenantInviteRepository struct {
	pool *Pool
}

// NewTenantInviteRepository builds a TenantInviteRepository backed by pool.
func NewTenantInviteRepository(pool *Pool) *TenantInviteRepository {
	return &TenantInviteRepository{pool: pool}
}

// Create inserts invite, filling in its generated ID, TenantID, and
// CreatedAt. Only the hash is ever written - callers must never pass a
// plaintext token in TokenHash. TenantID is left to the column's default
// (the session's app.tenant_id) when the caller doesn't set it, so an
// invite always lands on the tenant whose context created it.
func (r *TenantInviteRepository) Create(ctx context.Context, invite *TenantInvite) error {
	var tenantID any
	if invite.TenantID != "" {
		tenantID = invite.TenantID
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenant_invites (tenant_id, email, role, name, phone, token_hash, invited_by_id, expires_at)
		 VALUES (COALESCE($1::uuid, NULLIF(current_setting('app.tenant_id', true), '')::uuid), $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, tenant_id, created_at`,
		tenantID, invite.Email, invite.Role, invite.Name, invite.Phone, invite.TokenHash, invite.InvitedByID, invite.ExpiresAt,
	)

	if err := row.Scan(&invite.ID, &invite.TenantID, &invite.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.NotNullViolation {
			return fmt.Errorf("db: failed to create tenant invite: no active tenant: %w", err)
		}
		return fmt.Errorf("db: failed to create tenant invite: %w", err)
	}

	return nil
}

// GetByTokenHash looks up an invite by its hash, returning ErrNotFound if
// none exists.
func (r *TenantInviteRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*TenantInvite, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, role, name, phone, token_hash, invited_by_id, expires_at, used_at, created_at
		 FROM tenant_invites WHERE token_hash = $1`,
		tokenHash,
	)

	var invite TenantInvite
	if err := row.Scan(
		&invite.ID, &invite.TenantID, &invite.Email, &invite.Role, &invite.Name, &invite.Phone, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get tenant invite by hash: %w", err)
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
// check before either called MarkUsed, both proceeding to create an account
// from a single-use invite. The WHERE clause here makes the claim itself
// the concurrency gate: only one concurrent UPDATE can match a given row,
// so only one caller ever gets a non-ErrNotFound result back.
func (r *TenantInviteRepository) ClaimForUse(ctx context.Context, tokenHash string) (*TenantInvite, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE tenant_invites SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING id, tenant_id, email, role, name, phone, token_hash, invited_by_id, expires_at, used_at, created_at`,
		tokenHash,
	)

	var invite TenantInvite
	if err := row.Scan(
		&invite.ID, &invite.TenantID, &invite.Email, &invite.Role, &invite.Name, &invite.Phone, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to claim tenant invite: %w", err)
	}

	return &invite, nil
}

// isInvalidUUIDText reports whether err is Postgres rejecting id as a
// malformed uuid literal (22P02) before the WHERE clause ever runs - a
// caller-supplied id like "not-a-uuid" fails at the type-input stage, not
// as a no-rows result, so it would otherwise surface as a 500 instead of
// the ErrNotFound/404 spec.md requires for any unrecognized id.
func isInvalidUUIDText(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.InvalidTextRepresentation
}

// Refresh atomically replaces an invite's token hash and expiry, provided
// the invite hasn't already been accepted/canceled (used_at IS NULL). Same
// atomic-guard shape as ClaimForUse: only one concurrent Refresh/Cancel call
// on the same id can match the row, so a losing caller gets ErrNotFound
// instead of silently overwriting a settled invite.
func (r *TenantInviteRepository) Refresh(ctx context.Context, id, newTokenHash string, newExpiresAt time.Time) (*TenantInvite, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE tenant_invites SET token_hash = $2, expires_at = $3
		 WHERE id = $1 AND used_at IS NULL
		 RETURNING id, tenant_id, email, role, name, phone, token_hash, invited_by_id, expires_at, used_at, created_at`,
		id, newTokenHash, newExpiresAt,
	)

	var invite TenantInvite
	if err := row.Scan(
		&invite.ID, &invite.TenantID, &invite.Email, &invite.Role, &invite.Name, &invite.Phone, &invite.TokenHash,
		&invite.InvitedByID, &invite.ExpiresAt, &invite.UsedAt, &invite.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isInvalidUUIDText(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to refresh tenant invite: %w", err)
	}

	return &invite, nil
}

// Cancel atomically marks an invite used (so its token becomes permanently
// unacceptable) without creating an account for it, returning ErrNotFound
// if no unused invite with the given id exists (including a malformed,
// non-uuid id - see isInvalidUUIDText).
func (r *TenantInviteRepository) Cancel(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE tenant_invites SET used_at = now() WHERE id = $1 AND used_at IS NULL", id,
	)
	if err != nil {
		if isInvalidUUIDText(err) {
			return ErrNotFound
		}
		return fmt.Errorf("db: failed to cancel tenant invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkUsed sets used_at on the invite with the given ID to now, returning
// ErrNotFound if no such invite exists.
func (r *TenantInviteRepository) MarkUsed(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE tenant_invites SET used_at = now() WHERE id = $1", id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark tenant invite used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List returns every pending invite - not used, regardless of expiry - most
// recent first. Expired-but-unused invites stay listed so an owner can
// resend/cancel them instead of them silently disappearing (spec P2).
// TokenHash is never selected: the raw list is exposed via the admins API
// and must never leak the hash needed to accept an invite.
func (r *TenantInviteRepository) List(ctx context.Context) ([]TenantInvite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, email, role, name, phone, invited_by_id, expires_at, created_at
		 FROM tenant_invites
		 WHERE used_at IS NULL
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenant invites: %w", err)
	}
	defer rows.Close()

	var invites []TenantInvite
	for rows.Next() {
		var invite TenantInvite
		if err := rows.Scan(
			&invite.ID, &invite.TenantID, &invite.Email, &invite.Role, &invite.Name, &invite.Phone,
			&invite.InvitedByID, &invite.ExpiresAt, &invite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("db: failed to scan tenant invite: %w", err)
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list tenant invites: %w", err)
	}

	return invites, nil
}

// InvalidatePendingForEmail marks every still-pending (unused) invite for
// email as used, so a new invite can be created for the same address
// without leaving more than one live token. It is a no-op (not an error) if
// no pending invite exists for email.
func (r *TenantInviteRepository) InvalidatePendingForEmail(ctx context.Context, email string) error {
	if _, err := r.pool.Exec(ctx,
		"UPDATE tenant_invites SET used_at = now() WHERE email = $1 AND used_at IS NULL", email,
	); err != nil {
		return fmt.Errorf("db: failed to invalidate pending tenant invites for email: %w", err)
	}

	return nil
}
