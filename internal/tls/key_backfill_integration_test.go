//go:build integration

package tls

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newBackfillTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertLegacyRow(t *testing.T, pool *db.Pool, key string, value []byte, modified time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO certmagic_storage (key, value, modified_at) VALUES ($1, $2, $3)",
		key, value, modified,
	); err != nil {
		t.Fatalf("inserting %s returned unexpected error: %v", key, err)
	}
}

func cleanupBackfillRows(t *testing.T, pool *db.Pool, root string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM certmagic_storage WHERE key LIKE $1", root+"%")
	})
}

// TLSKEY-07, TLSKEY-08: the backfill seals a legacy plaintext .key row (so it
// decrypts to the original), leaves a public .crt row byte-identical, and
// preserves modified_at.
func TestEncryptLegacyKeys_SealsPrivateKeyLeavesPublicUntouched(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	root := fmt.Sprintf("tls-backfill-test-%d", time.Now().UnixNano())
	keyPath := root + "/status.example.key"
	crtPath := root + "/status.example.crt"
	cleanupBackfillRows(t, pool, root)

	legacy := pemPrivateKey("legacy-material")
	crt := []byte("-----BEGIN CERTIFICATE-----\npublic-material\n-----END CERTIFICATE-----\n")
	modified := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	insertLegacyRow(t, pool, keyPath, legacy, modified)
	insertLegacyRow(t, pool, crtPath, crt, modified)

	sealed, err := EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop())
	if err != nil {
		t.Fatalf("EncryptLegacyKeys() returned unexpected error: %v", err)
	}
	if sealed != 1 {
		t.Fatalf("EncryptLegacyKeys() sealed %d rows, want 1", sealed)
	}

	var sealedValue []byte
	var keyModified time.Time
	if err := pool.QueryRow(ctx, "SELECT value, modified_at FROM certmagic_storage WHERE key = $1", keyPath).Scan(&sealedValue, &keyModified); err != nil {
		t.Fatalf("reading sealed row: %v", err)
	}
	if !isSealed(sealedValue) {
		t.Fatalf("legacy .key row was not sealed: %q", sealedValue)
	}
	if bytes.Contains(sealedValue, []byte("legacy-material")) {
		t.Fatal("sealed .key row still contains plaintext")
	}
	decrypted, err := crypto.Decrypt(testMasterKey, sealedValue[len(secretEnvelopePrefix):])
	if err != nil {
		t.Fatalf("sealed .key row does not decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, legacy) {
		t.Fatalf("decrypted .key = %q, want original %q", decrypted, legacy)
	}
	if !keyModified.Equal(modified) {
		t.Fatalf("modified_at changed: %v -> %v, want unchanged", modified, keyModified)
	}

	var crtValue []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", crtPath).Scan(&crtValue); err != nil {
		t.Fatalf("reading .crt row: %v", err)
	}
	if !bytes.Equal(crtValue, crt) {
		t.Fatalf("public .crt row changed: %q", crtValue)
	}
}

// TLSKEY-07: a second run is a no-op and leaves the sealed value unchanged.
func TestEncryptLegacyKeys_Idempotent(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	root := fmt.Sprintf("tls-backfill-idem-%d", time.Now().UnixNano())
	keyPath := root + "/status.example.key"
	cleanupBackfillRows(t, pool, root)

	insertLegacyRow(t, pool, keyPath, pemPrivateKey("material"), time.Now())

	if n, err := EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop()); err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v, want n=1 err=nil", n, err)
	}
	var first []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", keyPath).Scan(&first); err != nil {
		t.Fatalf("reading sealed row: %v", err)
	}

	if n, err := EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop()); err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want n=0 err=nil", n, err)
	}
	var second []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", keyPath).Scan(&second); err != nil {
		t.Fatalf("reading row after second run: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("second run changed an already-sealed value")
	}
}

// TLSKEY-08: while another replica holds the advisory lock, the backfill skips
// and leaves legacy plaintext in place; once released, it seals.
func TestEncryptLegacyKeys_SkipsWhenAdvisoryLockHeld(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	root := fmt.Sprintf("tls-backfill-lock-%d", time.Now().UnixNano())
	keyPath := root + "/status.example.key"
	cleanupBackfillRows(t, pool, root)

	legacy := pemPrivateKey("material")
	insertLegacyRow(t, pool, keyPath, legacy, time.Now())

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", backfillAdvisoryLockKey); err != nil {
		conn.Release()
		t.Fatalf("acquiring advisory lock: %v", err)
	}

	n, err := EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop())
	if err != nil {
		conn.Release()
		t.Fatalf("EncryptLegacyKeys() while locked returned unexpected error: %v", err)
	}
	if n != 0 {
		conn.Release()
		t.Fatalf("EncryptLegacyKeys() sealed %d rows while the lock was held, want 0", n)
	}
	var stillPlain []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", keyPath).Scan(&stillPlain); err != nil {
		conn.Release()
		t.Fatalf("reading row: %v", err)
	}
	if isSealed(stillPlain) {
		conn.Release()
		t.Fatal("row was sealed despite the advisory lock being held elsewhere")
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", backfillAdvisoryLockKey); err != nil {
		conn.Release()
		t.Fatalf("releasing advisory lock: %v", err)
	}
	conn.Release()

	n, err = EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop())
	if err != nil || n != 1 {
		t.Fatalf("after releasing the lock: n=%d err=%v, want n=1 err=nil", n, err)
	}
}
