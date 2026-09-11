package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session is one row of the `sessions` table created by migration 0031
// (user-sessions). It maps a JWT (via the row's id, embedded in the
// token's `sid` claim) to the device/browser that issued it, so the
// "Sessões ativas" list and per-device revoke of the redesigned Meu
// Perfil screen can list and end individual sessions instead of the
// coarse-grained `users.sessions_revoked_at` global timestamp.
//
// user_agent and ip come from the issuing HTTP request (r.UserAgent()
// and the host portion of r.RemoteAddr, per AGENTS.md §4's IP-sourcing
// rule - never X-Forwarded-For). Both are nullable: missing headers or
// connections that don't surface an IP become NULL rather than empty
// strings, so the row genuinely means "not recorded" rather than "empty".
//
// ip is TEXT (not INET) - design.md initially chose INET, but pgx
// returns INET as "host/netmask" (e.g. "10.0.0.42/32"), which would
// surface in the UI's "Sessões ativas" list as "/32" suffixes that
// no operator wants to read. No CIDR queries are needed here (the
// "Sessões ativas" list shows the raw address, doesn't filter by
// subnet), so TEXT is the right shape for the actual use.
type Session struct {
	ID         string
	UserID     string
	UserAgent  sql.NullString
	IP         sql.NullString
	CreatedAt  time.Time
	LastSeenAt sql.NullTime
	RevokedAt  sql.NullTime
}

// SessionRepository accesses the `sessions` table. The same pool as the
// rest of internal/db is shared; ownership and per-tenant scoping come
// from the caller (RequireAuth / SessionsHandler), never from session-
// table RLS - matching tenant_memberships' "no policy, scope via
// app.user_id" posture (see context.md §"Agent's Discretion").
type SessionRepository struct {
	pool *Pool
}

// NewSessionRepository builds a SessionRepository backed by pool.
func NewSessionRepository(pool *Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// Create inserts a new session row for userID and returns the generated
// id (gen_random_uuid() at the DB level). userAgent and ip are passed
// through as empty strings converted to NULL via NULLIF, so callers
// can pass "" directly for "not recorded" without separate plumbing.
func (r *SessionRepository) Create(ctx context.Context, userID, userAgent, ip string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, user_agent, ip)
		 VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		 RETURNING id`,
		userID, userAgent, ip,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("db: failed to create session: %w", err)
	}
	return id, nil
}

// GetByID looks up a session row by its primary key. Returns ErrNotFound
// if no row exists with the given id; returns the row as-is including
// any revoked_at value - the caller (RequireAuth) decides whether a
// revoked session should reject the request.
func (r *SessionRepository) GetByID(ctx context.Context, id string) (*Session, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, user_id, user_agent, ip, created_at, last_seen_at, revoked_at
		 FROM sessions WHERE id = $1`,
		id,
	)

	var s Session
	if err := row.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP, &s.CreatedAt, &s.LastSeenAt, &s.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get session by id: %w", err)
	}
	return &s, nil
}

// ListForUser returns every non-revoked, non-expired session for userID.
// A row is treated as expired when created_at is older than 24h, matching
// SessionTTL - the JWT backing that row is past its `exp` claim by then
// regardless of what RequireAuth does or doesn't check, so showing it in
// the list would be a lie about state.
//
// Ordered created_at DESC so the user's most recent activity renders at
// the top of the "Sessões ativas" list without a client-side sort.
func (r *SessionRepository) ListForUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, user_agent, ip, created_at, last_seen_at, revoked_at
		 FROM sessions
		 WHERE user_id = $1
		   AND revoked_at IS NULL
		   AND created_at >= now() - INTERVAL '24 hours'
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list sessions for user: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP, &s.CreatedAt, &s.LastSeenAt, &s.RevokedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan session row: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: error iterating session rows: %w", err)
	}
	return sessions, nil
}

// GetByIDAndUser returns the session row only when it belongs to userID.
// Used by DELETE /api/auth/sessions/{id} to enforce ownership before
// revocation; ErrNotFound in either case (id missing OR owned by a
// different user) so the response never reveals whether the id exists -
// anti-enumeration, matching the existing login-error generic-body
// posture (AGENTS.md §4 / genericLoginErrorBody).
func (r *SessionRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*Session, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, user_id, user_agent, ip, created_at, last_seen_at, revoked_at
		 FROM sessions
		 WHERE id = $1 AND user_id = $2`,
		id, userID,
	)

	var s Session
	if err := row.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP, &s.CreatedAt, &s.LastSeenAt, &s.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get session by id and user: %w", err)
	}
	return &s, nil
}

// Revoke sets revoked_at on the session with the given id. Idempotent:
// re-invoking on an already-revoked session updates 0 rows but returns
// nil - Logout can be called twice (browser double-click, retry after
// network blip) without surfacing a spurious error to the client.
//
// Returns nil for both "id doesn't exist" and "already revoked" - the
// caller (Logout, DELETE handler) has already verified the row exists
// via the JWT's sid claim or the path's id + ownership check, so a
// missing id is a no-op rather than a 404-worthy error.
func (r *SessionRepository) Revoke(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to revoke session: %w", err)
	}
	return nil
}

// TouchLastSeen updates last_seen_at on the session with the given id,
// but only when the current last_seen_at is NULL or older than 5
// minutes - throttling the write so an active user producing 1 request
// per second doesn't generate 60 writes/min just to keep a coarse
// "last active" signal up to date.
//
// 0 rows affected (either the id doesn't exist or the throttle held)
// is not an error - the warm path is best-effort: RequireAuth treats
// TouchLastSeen failure as advisory (logged, request still passes),
// not correctness-critical.
func (r *SessionRepository) TouchLastSeen(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions
		 SET last_seen_at = now()
		 WHERE id = $1
		   AND (last_seen_at IS NULL OR last_seen_at < now() - INTERVAL '5 minutes')`,
		id,
	)
	if err != nil {
		return fmt.Errorf("db: failed to touch session last_seen_at: %w", err)
	}
	return nil
}

// RevokeAllForUser sets revoked_at on every non-revoked session for
// userID. Substitutes users.RevokeSessions (the global timestamp) in
// the two callers where the event is admin-driven - UpdateRole and
// Delete (admins.go:571, :619) - per context.md decision #1: a per-tenant
// admin action shouldn't appear to affect cross-tenant sessions, but
// the observable behavior (every session for this user becomes invalid)
// is preserved. ChangePassword and password_reset keep using the global
// timestamp because those are user-initiated credential events where
// "kill everything" is the desired blast radius.
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("db: failed to revoke all sessions for user: %w", err)
	}
	return nil
}
