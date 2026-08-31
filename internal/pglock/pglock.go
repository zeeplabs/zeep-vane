// Package pglock provides a Postgres session-advisory-lock primitive
// shared by the poller's leader election (internal/cli) and CertMagic's
// Locker (internal/tls), per ha-multi-replica's design (AD-013).
//
// Advisory locks are session-scoped: they live and die with the physical
// connection that took them, not with any higher-level "session" concept.
// Because of that, every lock in this package is held on a dedicated
// connection opened directly via dsn (pgx.Connect), never on a connection
// borrowed from a *db.Pool - returning such a connection to a pool would
// let pgxpool silently recycle it, releasing the lock without anyone
// calling Release. This mirrors the pattern internal/dbtest/lock.go
// already uses for the same reason in tests.
//
// Lock key namespace: this package's own callers use int64/hashed lock
// keys in the 727200000-727299999 block, deliberately distinct from
// internal/dbtest's test-only keys (727100001-727100003) so a production
// lock can never collide with (and deadlock against) a test-only one that
// happens to share a database.
package pglock

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Handle represents a held Postgres advisory lock on its own dedicated
// connection. It is not safe for concurrent use by multiple goroutines.
type Handle struct {
	conn *pgx.Conn
	// key/name identify how the lock was acquired, so Release issues the
	// matching unlock statement.
	key    int64
	name   string
	byName bool
}

// TryAcquire attempts to acquire the named advisory lock identified by key
// without blocking, on a fresh dedicated connection. It returns
// (handle, true, nil) if the lock was acquired, (nil, false, nil) if it is
// already held by someone else (the connection is closed in that case -
// TryAcquire never leaks a connection on a failed attempt), and
// (nil, false, err) if opening the connection or running the query itself
// failed.
func TryAcquire(ctx context.Context, dsn string, key int64) (*Handle, bool, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, false, fmt.Errorf("pglock: failed to open dedicated connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		_ = conn.Close(ctx)
		return nil, false, fmt.Errorf("pglock: pg_try_advisory_lock failed: %w", err)
	}

	if !acquired {
		_ = conn.Close(ctx)
		return nil, false, nil
	}

	return &Handle{conn: conn, key: key}, true, nil
}

// Acquire blocks until the advisory lock identified by name is acquired, on
// a fresh dedicated connection. name is hashed into a 64-bit lock key via
// Postgres's own hashtextextended(name, 0) - computed in SQL so Acquire and
// Release never need to keep a separate Go-side hash in sync.
//
// Acquire honors ctx: canceling ctx sends a Postgres cancel request for the
// in-flight pg_advisory_lock call, so a canceled context returns promptly
// with an error instead of waiting for the lock indefinitely.
func Acquire(ctx context.Context, dsn string, name string) (*Handle, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pglock: failed to open dedicated connection: %w", err)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", name); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("pglock: pg_advisory_lock failed: %w", err)
	}

	return &Handle{conn: conn, name: name, byName: true}, nil
}

// Healthy reports whether the handle's dedicated connection - and
// therefore the lock it holds - is still alive. false means the session
// (and the lock with it) is gone, e.g. the connection died or was closed
// out-of-band (crash, network partition).
func (h *Handle) Healthy(ctx context.Context) bool {
	if h.conn.IsClosed() {
		return false
	}
	var one int
	if err := h.conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return false
	}
	return true
}

// Release unlocks the advisory lock and closes the handle's dedicated
// connection. It is safe to call exactly once per successful acquire; a
// second call is a no-op error (the connection is already closed).
//
// pg_advisory_unlock returns a boolean reporting whether the calling
// session actually held (and therefore released) the lock under the
// key/name being unlocked - Release checks it and returns an error when
// it's false, rather than silently ignoring it. Without this check, a
// hash-key mismatch between the Acquire and Release call sites (e.g. a
// future refactor that changes how a name is hashed in one place but not
// the other) would ship completely undetected: Postgres auto-releases
// every session-scoped advisory lock when the connection closes,
// regardless of whether the explicit unlock targeted the right key, and
// Release always closes its own connection right after issuing the
// unlock - so the close alone would mask a wrong-key unlock in every
// caller that (like this package's own callers) tears the connection down
// immediately afterward anyway.
func (h *Handle) Release(ctx context.Context) error {
	if h.conn.IsClosed() {
		return nil
	}

	var released bool
	var err error
	if h.byName {
		err = h.conn.QueryRow(ctx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", h.name).Scan(&released)
	} else {
		err = h.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", h.key).Scan(&released)
	}

	closeErr := h.conn.Close(context.Background())
	if err != nil {
		return fmt.Errorf("pglock: pg_advisory_unlock failed: %w", err)
	}
	if !released {
		return fmt.Errorf("pglock: pg_advisory_unlock reported this session did not hold the lock it was asked to release (key/name mismatch or already released)")
	}
	if closeErr != nil {
		return fmt.Errorf("pglock: failed to close dedicated connection: %w", closeErr)
	}
	return nil
}
