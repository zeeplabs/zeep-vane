package tls

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// PostgresStorage implements certmagic.Storage (Store/Load/Delete/Exists/
// List/Stat/Lock/Unlock) backed by the certmagic_storage table
// (ha-multi-replica HA-13..HA-18), replacing certmagic.FileStorage's
// local-disk layout so every replica sharing one Postgres database sees the
// same certificate state immediately, with no shared volume and no local
// cache (design.md's "no local disk cache" decision).
//
// Keys mirror FileStorage's path-based layout (e.g.
// "certificates/acme-v02.../example.com/example.com.crt"); "directory"
// semantics (a key that is itself a prefix of other keys) are implemented
// via LIKE 'prefix/%' queries against the certmagic_storage_key_prefix_idx
// index the 0018 migration creates for this purpose.
type PostgresStorage struct {
	pool *db.Pool
	dsn  string

	mu    sync.Mutex
	locks map[string]*pglock.Handle
}

// NewPostgresStorage builds a PostgresStorage. dsn is used only by
// Lock/Unlock (internal/pglock needs a dedicated, non-pooled connection per
// held lock - advisory locks are session-scoped); every other method goes
// through pool.
func NewPostgresStorage(pool *db.Pool, dsn string) *PostgresStorage {
	return &PostgresStorage{
		pool:  pool,
		dsn:   dsn,
		locks: make(map[string]*pglock.Handle),
	}
}

// Store puts value at key, creating it if it does not exist and
// overwriting any existing value.
func (s *PostgresStorage) Store(ctx context.Context, key string, value []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO certmagic_storage (key, value, modified_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, modified_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("tls: failed to store %s: %w", key, err)
	}
	return nil
}

// Load retrieves the value at key. A missing key returns an error
// satisfying errors.Is(err, fs.ErrNotExist), per certmagic.Storage's
// contract.
func (s *PostgresStorage) Load(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("tls: %s: %w", key, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("tls: failed to load %s: %w", key, err)
	}
	return value, nil
}

// Delete deletes key. If key is a "directory" (a prefix of other keys),
// every key prefixed by key+"/" is deleted too. Returns an error
// satisfying errors.Is(err, fs.ErrNotExist) if nothing existed to delete.
func (s *PostgresStorage) Delete(ctx context.Context, key string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM certmagic_storage WHERE key = $1 OR key LIKE $2", key, key+"/%")
	if err != nil {
		return fmt.Errorf("tls: failed to delete %s: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tls: %s: %w", key, fs.ErrNotExist)
	}
	return nil
}

// Exists returns true if key exists either as an exact match or as a
// "directory" (prefix of other keys).
func (s *PostgresStorage) Exists(ctx context.Context, key string) bool {
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM certmagic_storage WHERE key = $1 OR key LIKE $2)",
		key, key+"/%",
	).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// List returns every key under path. If recursive, every key prefixed by
// path+"/" is returned in full; otherwise only the immediate next path
// segment for each match is returned (deduplicated), mirroring
// FileStorage's one-level-directory-listing semantics. A path matching no
// keys returns an empty (non-nil) slice, not an error.
func (s *PostgresStorage) List(ctx context.Context, path string, recursive bool) ([]string, error) {
	prefix := path + "/"

	rows, err := s.pool.Query(ctx, "SELECT key FROM certmagic_storage WHERE key LIKE $1", prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("tls: failed to list %s: %w", path, err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("tls: failed to scan list row for %s: %w", path, err)
		}

		result := key
		if !recursive {
			suffix := strings.TrimPrefix(key, prefix)
			if idx := strings.Index(suffix, "/"); idx >= 0 {
				suffix = suffix[:idx]
			}
			result = prefix + suffix
		}

		if _, ok := seen[result]; ok {
			continue
		}
		seen[result] = struct{}{}
		keys = append(keys, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tls: failed to list %s: %w", path, err)
	}

	return keys, nil
}

// Stat returns information about key. An exact match reports a terminal
// (file-like) KeyInfo with the stored value's size and modification time; a
// key that only exists as a "directory" (a prefix of other keys, no exact
// row of its own) reports a non-terminal KeyInfo instead, mirroring
// FileStorage's file-vs-directory distinction. A key that is neither
// returns an error satisfying errors.Is(err, fs.ErrNotExist).
func (s *PostgresStorage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {
	var value []byte
	var modified time.Time
	err := s.pool.QueryRow(ctx, "SELECT value, modified_at FROM certmagic_storage WHERE key = $1", key).Scan(&value, &modified)
	if err == nil {
		return certmagic.KeyInfo{
			Key:        key,
			Modified:   modified,
			Size:       int64(len(value)),
			IsTerminal: true,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return certmagic.KeyInfo{}, fmt.Errorf("tls: failed to stat %s: %w", key, err)
	}

	if s.Exists(ctx, key) {
		return certmagic.KeyInfo{
			Key:        key,
			IsTerminal: false,
		}, nil
	}

	return certmagic.KeyInfo{}, fmt.Errorf("tls: %s: %w", key, fs.ErrNotExist)
}
