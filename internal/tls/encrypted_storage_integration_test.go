//go:build integration

package tls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

// TLSKEY-05, TLSKEY-07: a real PEM round-trips through the decorator and
// Postgres, and a database that predates encryption is sealed by the backfill
// and then readable through the decorator.
func TestEncryptedStorage_Integration_RoundTripAndBackfill(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	root := fmt.Sprintf("tls-enc-storage-test-%d", time.Now().UnixNano())
	cleanupBackfillRows(t, pool, root)

	s := NewEncryptedStorage(NewPostgresStorage(pool, dsn), testMasterKey)
	keyPath := root + "/status.example.key"
	plaintext := pemPrivateKey("integration-round-trip")

	if err := s.Store(ctx, keyPath, plaintext); err != nil {
		t.Fatalf("Store: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", keyPath).Scan(&raw); err != nil {
		t.Fatalf("reading raw row: %v", err)
	}
	if !isSealed(raw) {
		t.Fatalf("stored row is not sealed: %q", raw)
	}
	if bytes.Contains(raw, []byte("integration-round-trip")) {
		t.Fatal("stored row still contains plaintext")
	}
	got, err := s.Load(ctx, keyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Load = %q, want %q", got, plaintext)
	}

	// A legacy plaintext row inserted directly (pre-AD-030 database) is sealed
	// by the backfill and then loads through the decorator unchanged.
	legacyPath := root + "/legacy.example.key"
	legacy := pemPrivateKey("legacy-integration")
	insertLegacyRow(t, pool, legacyPath, legacy, time.Now())
	if n, err := EncryptLegacyKeys(ctx, pool, testMasterKey, zap.NewNop()); err != nil || n != 1 {
		t.Fatalf("EncryptLegacyKeys: n=%d err=%v, want n=1 err=nil", n, err)
	}
	legacyGot, err := s.Load(ctx, legacyPath)
	if err != nil {
		t.Fatalf("Load(legacy): %v", err)
	}
	if !bytes.Equal(legacyGot, legacy) {
		t.Fatalf("Load(legacy) = %q, want %q", legacyGot, legacy)
	}
}

// TLSKEY-05: a sealed row read with the wrong master key fails explicitly and
// is never reported as a missing key (which would trigger ACME reissue).
func TestEncryptedStorage_Integration_WrongMasterKeyFailsExplicitly(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	root := fmt.Sprintf("tls-enc-wrong-key-%d", time.Now().UnixNano())
	cleanupBackfillRows(t, pool, root)

	keyPath := root + "/status.example.key"
	writer := NewEncryptedStorage(NewPostgresStorage(pool, dsn), "writer-master-key-012345678901234")
	if err := writer.Store(ctx, keyPath, pemPrivateKey("material")); err != nil {
		t.Fatalf("Store: %v", err)
	}

	reader := NewEncryptedStorage(NewPostgresStorage(pool, dsn), "reader-master-key-012345678901234")
	_, err := reader.Load(ctx, keyPath)
	if !errors.Is(err, crypto.ErrDecryptionFailed) {
		t.Fatalf("Load error = %v, want crypto.ErrDecryptionFailed", err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("decrypt failure must not be reported as fs.ErrNotExist")
	}
}

// TLSKEY-03: public artifacts written through the decorator stay readable in
// the table as-is (no encryption attempted).
func TestEncryptedStorage_Integration_PublicArtifactUntouched(t *testing.T) {
	pool := newBackfillTestPool(t)
	ctx := context.Background()
	dsn := testDatabaseURL(t)
	root := fmt.Sprintf("tls-enc-public-%d", time.Now().UnixNano())
	cleanupBackfillRows(t, pool, root)

	s := NewEncryptedStorage(NewPostgresStorage(pool, dsn), testMasterKey)
	crtPath := root + "/status.example.crt"
	crt := []byte("-----BEGIN CERTIFICATE-----\npublic-integration\n-----END CERTIFICATE-----\n")
	if err := s.Store(ctx, crtPath, crt); err != nil {
		t.Fatalf("Store: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT value FROM certmagic_storage WHERE key = $1", crtPath).Scan(&raw); err != nil {
		t.Fatalf("reading raw row: %v", err)
	}
	if !bytes.Equal(raw, crt) {
		t.Fatalf("public artifact changed at rest: %q", raw)
	}
}
