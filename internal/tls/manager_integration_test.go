//go:build integration

package tls

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

// setUpStatusPageFixture creates a domain + draft status page and returns
// its public hostname and a *db.StatusPageRepository backed by pool,
// registering cleanup for both fixture rows.
func setUpStatusPageFixture(t *testing.T) (hostname string, repo *db.StatusPageRepository, pool *db.Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootHostname := fmt.Sprintf("tls-manager-test-%s.example.com", suffix)
	subdomain := "status"
	hostname = subdomain + "." + rootHostname

	domains := db.NewDomainRepository(pool)
	domain := &db.Domain{Hostname: rootHostname}
	if err := domains.Create(ctx, domain); err != nil {
		t.Fatalf("failed to create domain fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	repo = db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "tls-manager-test-page", Subdomain: &subdomain, DomainID: &domain.ID}
	if err := repo.Create(ctx, statusPage, nil); err != nil {
		t.Fatalf("failed to create status page fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	return hostname, repo, pool
}

// TestNewManager_UsesPostgresStorage proves NewManager wires its
// certmagic.Storage parameter straight through to CertMagic (AD-013/HA-13):
// a value Stored via the same PostgresStorage instance passed into
// NewManager is readable back through certmagic.Default.Storage, and the
// returned *certmagic.Config's OnDemand/HostPolicy gate is still wired
// (SP-11/SP-13) exactly as before this feature's signature change from
// storagePath string to certmagic.Storage.
func TestNewManager_UsesPostgresStorage(t *testing.T) {
	hostname, repo, pool := setUpStatusPageFixture(t)
	dsn := testDatabaseURL(t)
	storage := NewPostgresStorage(pool, dsn)

	cfg := NewManager(repo, storage)
	if cfg == nil {
		t.Fatal("NewManager() returned nil *certmagic.Config")
	}
	if cfg.OnDemand == nil {
		t.Fatal("NewManager() returned a Config with nil OnDemand - HostPolicy gate is not wired")
	}

	key := fmt.Sprintf("manager-test-%d/cert.crt", time.Now().UnixNano())
	want := []byte("fake-cert-bytes")
	if err := storage.Store(context.Background(), key, want); err != nil {
		t.Fatalf("storage.Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = storage.Delete(context.Background(), key) })

	got, err := certmagic.Default.Storage.Load(context.Background(), key)
	if err != nil {
		t.Fatalf("certmagic.Default.Storage.Load() returned unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("certmagic.Default.Storage.Load() = %q, want %q", got, want)
	}

	// HostPolicy is still wired and still gates on state - the fixture
	// starts in "draft" (setUpStatusPageFixture's default), so the same
	// SP-11/SP-13 rejection behavior from before this signature change
	// must still hold: this proves NewManager's OnDemand.DecisionFunc is
	// unaffected by swapping storagePath for a certmagic.Storage.
	if err := cfg.OnDemand.DecisionFunc(context.Background(), hostname); err == nil {
		t.Error("OnDemand.DecisionFunc() for a draft-state fixture returned nil error, want rejection")
	}
}

// TestOnEvent_CertObtained_MarksStatusPagePublished simulates the
// "cert_obtained" event CertMagic emits after a successful ACME issuance -
// a real ACME/Let's Encrypt handshake isn't reasonably available in this
// environment, so the event payload is constructed directly, exactly as
// certmagic/config.go builds it, and dispatched into OnEvent backed by a
// real Postgres-backed StatusPageRepository. This proves the SP-12 state
// transition end to end against the real repository, without depending on
// network ACME issuance succeeding.
func TestOnEvent_CertObtained_MarksStatusPagePublished(t *testing.T) {
	hostname, repo, pool := setUpStatusPageFixture(t)
	onEvent := OnEvent(repo)

	err := onEvent(context.Background(), certObtainedEvent, map[string]any{
		"identifier": hostname,
		"renewal":    false,
	})
	if err != nil {
		t.Fatalf("OnEvent(cert_obtained) returned unexpected error: %v", err)
	}

	var state string
	row := pool.QueryRow(context.Background(),
		"SELECT sp.state FROM status_pages sp JOIN domains d ON d.id = sp.domain_id WHERE sp.subdomain || '.' || d.hostname = $1",
		hostname,
	)
	if err := row.Scan(&state); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if state != "published" {
		t.Errorf("state = %q, want %q", state, "published")
	}
}

// TestOnEvent_CertFailed_MarksStatusPageTLSFailedWithReason simulates the
// "cert_failed" event CertMagic emits after a failed ACME issuance (e.g.
// DNS not pointing at this server yet), the same way the previous test
// simulates the successful path.
func TestOnEvent_CertFailed_MarksStatusPageTLSFailedWithReason(t *testing.T) {
	hostname, repo, pool := setUpStatusPageFixture(t)
	onEvent := OnEvent(repo)

	const failureReason = "acme: authorization failed: dns record not found"
	err := onEvent(context.Background(), certFailedEvent, map[string]any{
		"identifier": hostname,
		"renewal":    false,
		"error":      failureReason,
	})
	if err != nil {
		t.Fatalf("OnEvent(cert_failed) returned unexpected error: %v", err)
	}

	var state string
	var tlsLastError *string
	row := pool.QueryRow(context.Background(),
		"SELECT sp.state, sp.tls_last_error FROM status_pages sp JOIN domains d ON d.id = sp.domain_id WHERE sp.subdomain || '.' || d.hostname = $1",
		hostname,
	)
	if err := row.Scan(&state, &tlsLastError); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if state != "tls_failed" {
		t.Errorf("state = %q, want %q", state, "tls_failed")
	}
	if tlsLastError == nil || *tlsLastError != failureReason {
		t.Errorf("tls_last_error = %v, want %q", tlsLastError, failureReason)
	}
}
