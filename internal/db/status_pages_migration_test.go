//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestStatusPagesMigration_NullDomainAndSubdomain_Allowed asserts SPD-05:
// status_pages.domain_id and .subdomain both accept NULL after migration
// 0013, since a status page can now be created with no domain attached.
func TestStatusPagesMigration_NullDomainAndSubdomain_Allowed(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	name := fmt.Sprintf("status-pages-migration-test-%d", time.Now().UnixNano())
	var id string
	row := pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, NULL, NULL) RETURNING id", name)
	if err := row.Scan(&id); err != nil {
		t.Fatalf("insert with NULL domain_id/subdomain returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", id) })
}

// TestStatusPagesMigration_DuplicateDomainSubdomainPair_RejectedByPartialIndex
// asserts SPD-09: the partial unique index on (domain_id, subdomain) WHERE
// domain_id IS NOT NULL rejects a second row that collides with an
// existing non-null pair.
func TestStatusPagesMigration_DuplicateDomainSubdomainPair_RejectedByPartialIndex(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	hostname := fmt.Sprintf("status-pages-migration-test-%d.example.com", time.Now().UnixNano())
	var domainID string
	row := pool.QueryRow(ctx, "INSERT INTO domains (hostname) VALUES ($1) RETURNING id", hostname)
	if err := row.Scan(&domainID); err != nil {
		t.Fatalf("domain fixture insert returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domainID) })

	subdomain := "status"
	name := fmt.Sprintf("status-pages-migration-test-page-%d", time.Now().UnixNano())

	var firstID string
	row = pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3) RETURNING id",
		name, subdomain, domainID)
	if err := row.Scan(&firstID); err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", firstID) })

	_, err = pool.Exec(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3)",
		name+"-second", subdomain, domainID)
	if err == nil {
		t.Fatal("second insert with duplicate (domain_id, subdomain) pair returned nil error, want unique constraint violation")
	}
}

// TestStatusPagesMigration_MultipleNullDomainRows_NeverBlocked asserts the
// partial index never applies to domain-less rows: any number of rows with
// domain_id IS NULL (regardless of subdomain value) succeed.
func TestStatusPagesMigration_MultipleNullDomainRows_NeverBlocked(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	baseName := fmt.Sprintf("status-pages-migration-test-nulls-%d", time.Now().UnixNano())

	var firstID, secondID string
	row := pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, NULL, NULL) RETURNING id", baseName+"-1")
	if err := row.Scan(&firstID); err != nil {
		t.Fatalf("first NULL-domain insert returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", firstID) })

	row = pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, NULL, NULL) RETURNING id", baseName+"-2")
	if err := row.Scan(&secondID); err != nil {
		t.Fatalf("second NULL-domain insert returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", secondID) })
}
