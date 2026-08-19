//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// createStatusPageFixture inserts a domain and a status page pointed at
// hostname (subdomain "status-page-repo-test-<n>" + a freshly created
// domain), registering cleanup for both. It returns the created status
// page's hostname.
func createStatusPageFixture(t *testing.T, pool *Pool) string {
	t.Helper()
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootHostname := fmt.Sprintf("status-page-repo-test-%s.example.com", suffix)
	subdomain := "status"
	hostname := subdomain + "." + rootHostname

	domains := NewDomainRepository(pool)
	domain := &Domain{Hostname: rootHostname}
	if err := domains.Create(ctx, domain); err != nil {
		t.Fatalf("failed to create domain fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	statusPages := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: "repo-test-page", Subdomain: subdomain, DomainID: domain.ID}
	if err := statusPages.Create(ctx, statusPage, nil); err != nil {
		t.Fatalf("failed to create status page fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	return hostname
}

func TestStatusPageRepository_StateByHostname_UnknownHostname_ErrNotFound(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := NewStatusPageRepository(pool)
	_, err = repo.StateByHostname(context.Background(), "no-such-page.example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("StateByHostname() error = %v, want ErrNotFound", err)
	}
}

func TestStatusPageRepository_MarkPublished_SetsPublishedStateAndClearsError(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	hostname := createStatusPageFixture(t, pool)
	repo := NewStatusPageRepository(pool)
	ctx := context.Background()

	// Seed a prior tls_failed error, so MarkPublished's job of clearing it
	// on success (SP-12) has something real to clear.
	if err := repo.MarkTLSFailed(ctx, hostname, "seed: dns not propagated yet"); err != nil {
		t.Fatalf("seed MarkTLSFailed() returned unexpected error: %v", err)
	}

	if err := repo.MarkPublished(ctx, hostname); err != nil {
		t.Fatalf("MarkPublished() returned unexpected error: %v", err)
	}

	var state string
	var tlsLastError *string
	row := pool.QueryRow(ctx,
		"SELECT sp.state, sp.tls_last_error FROM status_pages sp JOIN domains d ON d.id = sp.domain_id WHERE sp.subdomain || '.' || d.hostname = $1",
		hostname,
	)
	if err := row.Scan(&state, &tlsLastError); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}

	if state != "published" {
		t.Errorf("state = %q, want %q", state, "published")
	}
	if tlsLastError != nil {
		t.Errorf("tls_last_error = %v, want nil (cleared on publish)", *tlsLastError)
	}
}

func TestStatusPageRepository_MarkTLSFailed_SetsTLSFailedStateWithReason(t *testing.T) {
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	hostname := createStatusPageFixture(t, pool)
	repo := NewStatusPageRepository(pool)
	ctx := context.Background()

	const reason = "acme: challenge failed - dns not propagated"
	if err := repo.MarkTLSFailed(ctx, hostname, reason); err != nil {
		t.Fatalf("MarkTLSFailed() returned unexpected error: %v", err)
	}

	var state string
	var tlsLastError *string
	row := pool.QueryRow(ctx,
		"SELECT sp.state, sp.tls_last_error FROM status_pages sp JOIN domains d ON d.id = sp.domain_id WHERE sp.subdomain || '.' || d.hostname = $1",
		hostname,
	)
	if err := row.Scan(&state, &tlsLastError); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}

	if state != "tls_failed" {
		t.Errorf("state = %q, want %q", state, "tls_failed")
	}
	if tlsLastError == nil || *tlsLastError != reason {
		t.Errorf("tls_last_error = %v, want %q", tlsLastError, reason)
	}
}
