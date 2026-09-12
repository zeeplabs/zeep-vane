package tls

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// backfillAdvisoryLockKey is a fixed advisory-lock key that serializes the
// legacy-key backfill across replicas booting at the same time (AD-030). It
// is unrelated to the pglock keys CertMagic uses for storage locking.
const backfillAdvisoryLockKey int64 = 727200003

// EncryptLegacyKeys seals every private-key row ("key" ending in ".key") that
// is still plaintext, so databases created before key encryption (AD-030) do
// not keep readable private keys after an upgrade. It is idempotent: sealed
// rows are skipped, so a second run (or a concurrent replica) changes nothing.
// The write is scoped to the value it read, so it cannot clobber a concurrent
// renewal write, and modified_at is deliberately left untouched. A replica
// that cannot take the advisory lock returns (0, nil) and lets the holder do
// the work. The caller treats any error as best-effort (warn and continue).
func EncryptLegacyKeys(ctx context.Context, pool *db.Pool, masterKey string, logger *zap.Logger) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("tls: backfill: failed to begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var acquired bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", backfillAdvisoryLockKey).Scan(&acquired); err != nil {
		return 0, fmt.Errorf("tls: backfill: failed to take advisory lock: %w", err)
	}
	if !acquired {
		if logger != nil {
			logger.Debug("tls: backfill: another replica holds the key-encryption lock; skipping")
		}
		return 0, nil
	}

	rows, err := tx.Query(ctx, "SELECT key, value FROM certmagic_storage WHERE key LIKE '%.key'")
	if err != nil {
		return 0, fmt.Errorf("tls: backfill: failed to list private keys: %w", err)
	}

	type legacyRow struct {
		key   string
		value []byte
	}
	var legacy []legacyRow
	for rows.Next() {
		var r legacyRow
		if err := rows.Scan(&r.key, &r.value); err != nil {
			rows.Close()
			return 0, fmt.Errorf("tls: backfill: failed to scan private key row: %w", err)
		}
		if !isSealed(r.value) {
			legacy = append(legacy, r)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("tls: backfill: failed reading private keys: %w", err)
	}
	rows.Close()

	sealed := 0
	for _, r := range legacy {
		sealedValue, err := sealSecret(masterKey, r.value)
		if err != nil {
			return 0, fmt.Errorf("tls: backfill: failed to seal %s: %w", r.key, err)
		}
		tag, err := tx.Exec(ctx,
			"UPDATE certmagic_storage SET value = $2 WHERE key = $1 AND value = $3",
			r.key, sealedValue, r.value,
		)
		if err != nil {
			return 0, fmt.Errorf("tls: backfill: failed to update %s: %w", r.key, err)
		}
		sealed += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("tls: backfill: failed to commit: %w", err)
	}
	if logger != nil {
		logger.Info("tls: sealed legacy private keys at rest", zap.Int("sealed", sealed))
	}
	return sealed, nil
}
