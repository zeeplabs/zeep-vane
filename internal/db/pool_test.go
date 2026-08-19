//go:build integration

package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func TestNewPool_InvalidDSN_Error(t *testing.T) {
	ctx := context.Background()

	_, err := NewPool(ctx, "postgres://user:pass@host:notaport/db")
	if err == nil {
		t.Fatal("NewPool with malformed DSN returned nil error, want error")
	}
}

func TestPing_ReachableDatabase_Success(t *testing.T) {
	dsn := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping() returned unexpected error: %v", err)
	}
}

func TestPing_UnreachableDatabase_Error(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, "postgres://zeep:zeep@localhost:1/zeep?sslmode=disable")
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping() against unreachable database returned nil error, want error")
	}
}
