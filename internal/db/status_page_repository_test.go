//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	statusPage := &StatusPage{Name: "repo-test-page", Subdomain: &subdomain, DomainID: &domain.ID}
	if err := statusPages.Create(ctx, statusPage, nil); err != nil {
		t.Fatalf("failed to create status page fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	return hostname
}

func TestStatusPageRepository_StateByHostname_UnknownHostname_ErrNotFound(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	repo := NewStatusPageRepository(pool)
	_, err := repo.StateByHostname(context.Background(), "no-such-page.example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("StateByHostname() error = %v, want ErrNotFound", err)
	}
}

func TestStatusPageRepository_MarkPublished_SetsPublishedStateAndClearsError(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

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
	pool, _ := newTenantScopedPool(t)

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

// TestStatusPageRepository_Create_NoDomain_ReturnsNullDomainAndSubdomain
// asserts SPD-01: a status page can be created with DomainID and Subdomain
// both nil, and the returned row reflects both as nil (no domain forced on
// creation).
func TestStatusPageRepository_Create_NoDomain_ReturnsNullDomainAndSubdomain(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	repo := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: fmt.Sprintf("no-domain-page-%d", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	if statusPage.DomainID != nil {
		t.Errorf("DomainID = %v, want nil", *statusPage.DomainID)
	}
	if statusPage.Subdomain != nil {
		t.Errorf("Subdomain = %v, want nil", *statusPage.Subdomain)
	}
	if statusPage.State != "draft" {
		t.Errorf("State = %q, want %q", statusPage.State, "draft")
	}
}

// TestStatusPageRepository_Create_WithDomain_Unchanged asserts the
// existing with-domain create path (SPD-05: backward compatible) still
// returns the exact domain/subdomain provided.
func TestStatusPageRepository_Create_WithDomain_Unchanged(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	hostname := fmt.Sprintf("status-page-repo-create-test-%d.example.com", time.Now().UnixNano())
	domains := NewDomainRepository(pool)
	domain := &Domain{Hostname: hostname}
	if err := domains.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup domain Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	subdomain := "status"
	repo := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: "with-domain-page", Subdomain: &subdomain, DomainID: &domain.ID}
	if err := repo.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	if statusPage.DomainID == nil || *statusPage.DomainID != domain.ID {
		t.Errorf("DomainID = %v, want %q", statusPage.DomainID, domain.ID)
	}
	if statusPage.Subdomain == nil || *statusPage.Subdomain != subdomain {
		t.Errorf("Subdomain = %v, want %q", statusPage.Subdomain, subdomain)
	}
}

// TestStatusPageRepository_Create_PersistsEnvironment covers
// status-page-environment-banner SPEB-01: a non-empty Environment given to
// Create persists and reads back via GetByID.
func TestStatusPageRepository_Create_PersistsEnvironment(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	repo := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: fmt.Sprintf("env-page-%d", time.Now().UnixNano()), Environment: "staging"}
	if err := repo.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	got, err := repo.GetByID(context.Background(), statusPage.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.Environment != "staging" {
		t.Errorf("got.Environment = %q, want %q", got.Environment, "staging")
	}
}

// TestStatusPageRepository_Create_EmptyEnvironment_DefaultsToEmpty covers
// the ""-means-absent convention (SPEB-01): a StatusPage with no
// Environment set persists and reads back "" (no banner).
func TestStatusPageRepository_Create_EmptyEnvironment_DefaultsToEmpty(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	repo := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: fmt.Sprintf("env-page-empty-%d", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	got, err := repo.GetByID(context.Background(), statusPage.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.Environment != "" {
		t.Errorf("got.Environment = %q, want empty", got.Environment)
	}
}

// TestStatusPageRepository_UpdateEnvironment_SetsThenClears_RoundTrips
// covers SPEB-02's admin-facing round trip: set the environment, read it
// back via GetByID, then clear it via "" and confirm it reads back empty
// again (not left stale) - mirrors ServiceRepository's UpdateEnvironment
// round-trip test.
func TestStatusPageRepository_UpdateEnvironment_SetsThenClears_RoundTrips(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "update-env-page")

	updated, err := repo.UpdateEnvironment(context.Background(), pageID, "development")
	if err != nil {
		t.Fatalf("UpdateEnvironment() set returned unexpected error: %v", err)
	}
	if updated.Environment != "development" {
		t.Fatalf("after set: updated.Environment = %q, want %q", updated.Environment, "development")
	}
	got, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() after set returned unexpected error: %v", err)
	}
	if got.Environment != "development" {
		t.Fatalf("after set: got.Environment = %q, want %q", got.Environment, "development")
	}

	if _, err := repo.UpdateEnvironment(context.Background(), pageID, ""); err != nil {
		t.Fatalf("UpdateEnvironment() clear returned unexpected error: %v", err)
	}
	got, err = repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() after clear returned unexpected error: %v", err)
	}
	if got.Environment != "" {
		t.Errorf("after clear: got.Environment = %q, want empty", got.Environment)
	}
}

// TestStatusPageRepository_UpdateEnvironment_UnknownID_ReturnsErrNotFound
// mirrors ServiceRepository's UpdateEnvironment not-found convention.
func TestStatusPageRepository_UpdateEnvironment_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newAttachDomainTestRepo(t)

	_, err := repo.UpdateEnvironment(context.Background(), "00000000-0000-0000-0000-000000000000", "development")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEnvironment() error = %v, want ErrNotFound", err)
	}
}

// newUnscopedPool returns a migrated pool with NO session-level
// app.tenant_id, unlike newTenantScopedPool. A test that needs to prove
// something about how a repository handles the caller's transaction cannot
// use the tenant-scoped pool: presetting the setting on every connection
// would make an independently pooled transaction resolve the tenant_id
// DEFAULT just fine, hiding the very failure under test.
func newUnscopedPool(t *testing.T) *Pool {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestStatusPageRepository_Create_InsideExistingTenantTx_ReusesCallerTransaction
// is the regression guard on Create opening its own pooled transaction: the
// tenant_id DEFAULT reads current_setting('app.tenant_id'), which is SET
// LOCAL on the caller's transaction only, so a second transaction resolved
// it to NULL and the insert died on the NOT NULL constraint. Asserts three
// things at once: the call succeeds, the row lands on the caller's tenant,
// and it is genuinely part of the caller's transaction (rolling that
// transaction back removes it, which a self-committing Create could not
// honour).
func TestStatusPageRepository_Create_InsideExistingTenantTx_ReusesCallerTransaction(t *testing.T) {
	pool := newUnscopedPool(t)
	tenantID := seedPlainTenant(t, pool)
	ctx := context.Background()

	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := WithTenantTx(ctx, tx)

	repo := NewStatusPageRepository(pool)
	statusPage := &StatusPage{Name: fmt.Sprintf("tenant-tx-page-%d", time.Now().UnixNano())}
	if err := repo.Create(txCtx, statusPage, nil); err != nil {
		t.Fatalf("Create() inside an existing tenant transaction returned unexpected error: %v", err)
	}
	if statusPage.ID == "" {
		t.Fatal("Create() left StatusPage.ID empty")
	}

	var gotTenantID string
	if err := pool.QueryRow(txCtx, "SELECT tenant_id FROM status_pages WHERE id = $1", statusPage.ID).Scan(&gotTenantID); err != nil {
		t.Fatalf("reading back tenant_id returned unexpected error: %v", err)
	}
	if gotTenantID != tenantID {
		t.Errorf("status_pages.tenant_id = %q, want %q", gotTenantID, tenantID)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM status_pages WHERE id = $1", statusPage.ID).Scan(&count); err != nil {
		t.Fatalf("counting rows after rollback returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("status page rows surviving the caller's rollback = %d, want 0 (Create committed its own transaction)", count)
	}
}

// TestStatusPageRepository_List_MixOfDomainedAndDomainless_CorrectNullability
// asserts SPD-01/SPD-05: List returns both a domain-less and a domained
// row with correct nullability on each.
func TestStatusPageRepository_List_MixOfDomainedAndDomainless_CorrectNullability(t *testing.T) {
	pool, _ := newTenantScopedPool(t)

	repo := NewStatusPageRepository(pool)

	noDomainName := fmt.Sprintf("list-no-domain-page-%d", time.Now().UnixNano())
	noDomainPage := &StatusPage{Name: noDomainName}
	if err := repo.Create(context.Background(), noDomainPage, nil); err != nil {
		t.Fatalf("Create() (no domain) returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", noDomainPage.ID)
	})

	hostname := fmt.Sprintf("status-page-repo-list-test-%d.example.com", time.Now().UnixNano())
	domains := NewDomainRepository(pool)
	domain := &Domain{Hostname: hostname}
	if err := domains.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup domain Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	subdomain := "status"
	withDomainName := fmt.Sprintf("list-with-domain-page-%d", time.Now().UnixNano())
	withDomainPage := &StatusPage{Name: withDomainName, Subdomain: &subdomain, DomainID: &domain.ID}
	if err := repo.Create(context.Background(), withDomainPage, nil); err != nil {
		t.Fatalf("Create() (with domain) returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", withDomainPage.ID)
	})

	// ListPaginated (PAG-08) replaced List(ctx) - page through every page
	// since the shared dev DB this suite runs against can hold more than
	// one page's worth of status pages across many tests.
	var foundNoDomain, foundWithDomain bool
	for page := 1; ; page++ {
		statusPages, total, err := repo.ListPaginated(context.Background(), page, 20)
		if err != nil {
			t.Fatalf("ListPaginated() returned unexpected error: %v", err)
		}
		for _, sp := range statusPages {
			switch sp.Name {
			case noDomainName:
				foundNoDomain = true
				if sp.DomainID != nil {
					t.Errorf("no-domain page DomainID = %v, want nil", *sp.DomainID)
				}
				if sp.Subdomain != nil {
					t.Errorf("no-domain page Subdomain = %v, want nil", *sp.Subdomain)
				}
			case withDomainName:
				foundWithDomain = true
				if sp.DomainID == nil || *sp.DomainID != domain.ID {
					t.Errorf("with-domain page DomainID = %v, want %q", sp.DomainID, domain.ID)
				}
				if sp.Subdomain == nil || *sp.Subdomain != subdomain {
					t.Errorf("with-domain page Subdomain = %v, want %q", sp.Subdomain, subdomain)
				}
			}
		}
		if len(statusPages) == 0 || page*20 >= total {
			break
		}
	}
	if !foundNoDomain {
		t.Error("ListPaginated() did not include the domain-less status page across any page")
	}
	if !foundWithDomain {
		t.Error("ListPaginated() did not include the with-domain status page across any page")
	}
}

// createAttachTestDomain inserts a domain fixture and registers its
// cleanup, returning the generated ID.
func createAttachTestDomain(t *testing.T, pool *Pool) string {
	t.Helper()
	hostname := fmt.Sprintf("status-page-attach-test-%d.example.com", time.Now().UnixNano())
	domains := NewDomainRepository(pool)
	domain := &Domain{Hostname: hostname}
	if err := domains.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup domain Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })
	return domain.ID
}

// createDomainlessStatusPage inserts a domain-less status page fixture and
// registers its cleanup, returning the generated ID.
func createDomainlessStatusPage(t *testing.T, repo *StatusPageRepository, pool *Pool, namePrefix string) string {
	t.Helper()
	statusPage := &StatusPage{Name: fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixNano())}
	if err := repo.Create(context.Background(), statusPage, nil); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})
	return statusPage.ID
}

func newAttachDomainTestRepo(t *testing.T) (*StatusPageRepository, *Pool) {
	t.Helper()
	pool, _ := newTenantScopedPool(t)
	return NewStatusPageRepository(pool), pool
}

// testTenantID reads back the app.tenant_id GUC newTenantScopedPool pinned
// on every connection in pool (via dbtest.TenantScopedDSN's "-c
// app.tenant_id=..." connection option) - used by UpdateGroup/DeleteGroup
// call sites that now require an explicit tenantID argument, without
// threading a third return value through newAttachDomainTestRepo's ~40
// existing call sites.
func testTenantID(t *testing.T, pool *Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(context.Background(), "SELECT current_setting('app.tenant_id', true)").Scan(&tenantID); err != nil {
		t.Fatalf("reading app.tenant_id returned unexpected error: %v", err)
	}
	return tenantID
}

// TestAttachDomain_DomainlessPage_SucceedsAndMovesToPendingTLS asserts
// SPD-06: attaching a domain to a domain-less page sets both fields and
// moves state to "pending_tls". This used to assert state stayed "draft"
// ("the existing on-demand TLS mechanism takes over from here") - that
// was the bug: tls.HostPolicy rejects ACME issuance for any hostname
// whose state is still "draft", so a page that never left "draft" could
// never reach the on-demand TLS mechanism this comment assumed would run.
// See migration 0020 and internal/tls/manager.go's draftState doc.
func TestAttachDomain_DomainlessPage_SucceedsAndMovesToPendingTLS(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "attach-success-page")

	updated, err := repo.AttachDomain(context.Background(), pageID, domainID, "status")
	if err != nil {
		t.Fatalf("AttachDomain() returned unexpected error: %v", err)
	}

	if updated.DomainID == nil || *updated.DomainID != domainID {
		t.Errorf("DomainID = %v, want %q", updated.DomainID, domainID)
	}
	if updated.Subdomain == nil || *updated.Subdomain != "status" {
		t.Errorf("Subdomain = %v, want %q", updated.Subdomain, "status")
	}
	if updated.State != "pending_tls" {
		t.Errorf("State = %q, want %q", updated.State, "pending_tls")
	}
}

func TestPublicHostnameByID_DomainAttached_ReturnsFullHostname(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "hostname-lookup-page")
	if _, err := repo.AttachDomain(context.Background(), pageID, domainID, "status"); err != nil {
		t.Fatalf("AttachDomain() returned unexpected error: %v", err)
	}

	var domainHostname string
	if err := pool.QueryRow(context.Background(), "SELECT hostname FROM domains WHERE id = $1", domainID).Scan(&domainHostname); err != nil {
		t.Fatalf("failed to read domain hostname fixture: %v", err)
	}

	got, err := repo.PublicHostnameByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("PublicHostnameByID() returned unexpected error: %v", err)
	}
	want := "status." + domainHostname
	if got != want {
		t.Errorf("PublicHostnameByID() = %q, want %q", got, want)
	}
}

func TestPublicHostnameByID_NoDomainAttached_ErrNoDomainAttached(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "hostname-lookup-no-domain-page")

	_, err := repo.PublicHostnameByID(context.Background(), pageID)
	if !errors.Is(err, ErrNoDomainAttached) {
		t.Fatalf("PublicHostnameByID() error = %v, want ErrNoDomainAttached", err)
	}
}

func TestPublicHostnameByID_UnknownID_ErrNotFound(t *testing.T) {
	repo, _ := newAttachDomainTestRepo(t)

	_, err := repo.PublicHostnameByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("PublicHostnameByID() error = %v, want ErrNotFound", err)
	}
}

// TestAttachDomain_NonexistentPage_ErrNotFound asserts the "page doesn't
// exist" outcome AttachDomain must distinguish from "already attached"
// (design.md Tech Decisions: the reason for SELECT ... FOR UPDATE over a
// conditional UPDATE).
func TestAttachDomain_NonexistentPage_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)

	_, err := repo.AttachDomain(context.Background(), "00000000-0000-0000-0000-000000000000", domainID, "status")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("AttachDomain() error = %v, want ErrNotFound", err)
	}
}

// TestAttachDomain_AlreadyAttachedPage_ErrDomainAlreadyAttachedRowUnmodified
// asserts SPD-07: attaching to a page that already has a domain fails and
// leaves the row's existing domain_id/subdomain untouched.
func TestAttachDomain_AlreadyAttachedPage_ErrDomainAlreadyAttachedRowUnmodified(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	firstDomainID := createAttachTestDomain(t, pool)
	secondDomainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "attach-already-attached-page")

	if _, err := repo.AttachDomain(context.Background(), pageID, firstDomainID, "status"); err != nil {
		t.Fatalf("setup AttachDomain() returned unexpected error: %v", err)
	}

	_, err := repo.AttachDomain(context.Background(), pageID, secondDomainID, "other")
	if !errors.Is(err, ErrDomainAlreadyAttached) {
		t.Fatalf("AttachDomain() error = %v, want ErrDomainAlreadyAttached", err)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if page.DomainID == nil || *page.DomainID != firstDomainID {
		t.Errorf("DomainID after failed re-attach = %v, want unchanged %q", page.DomainID, firstDomainID)
	}
	if page.Subdomain == nil || *page.Subdomain != "status" {
		t.Errorf("Subdomain after failed re-attach = %v, want unchanged %q", page.Subdomain, "status")
	}
}

// TestAttachDomain_InvalidDomainID_ErrInvalidDomainRowUnmodified asserts
// SPD-07: a domain_id that doesn't reference an existing Domain fails with
// ErrInvalidDomain, and the target page stays domain-less.
func TestAttachDomain_InvalidDomainID_ErrInvalidDomainRowUnmodified(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "attach-invalid-domain-page")

	_, err := repo.AttachDomain(context.Background(), pageID, "00000000-0000-0000-0000-000000000000", "status")
	if !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("AttachDomain() error = %v, want ErrInvalidDomain", err)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if page.DomainID != nil {
		t.Errorf("DomainID after failed attach = %v, want nil (unmodified)", *page.DomainID)
	}
}

// TestAttachDomain_DuplicateDomainSubdomainPair_ErrDuplicateDomainSubdomainRowUnmodified
// asserts SPD-09: attaching a (domain_id, subdomain) pair already used by
// another status page fails, and the target page stays domain-less.
func TestAttachDomain_DuplicateDomainSubdomainPair_ErrDuplicateDomainSubdomainRowUnmodified(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)

	takenPageID := createDomainlessStatusPage(t, repo, pool, "attach-duplicate-taken-page")
	if _, err := repo.AttachDomain(context.Background(), takenPageID, domainID, "status"); err != nil {
		t.Fatalf("setup AttachDomain() returned unexpected error: %v", err)
	}

	pageID := createDomainlessStatusPage(t, repo, pool, "attach-duplicate-colliding-page")
	_, err := repo.AttachDomain(context.Background(), pageID, domainID, "status")
	if !errors.Is(err, ErrDuplicateDomainSubdomain) {
		t.Fatalf("AttachDomain() error = %v, want ErrDuplicateDomainSubdomain", err)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if page.DomainID != nil {
		t.Errorf("DomainID after failed attach = %v, want nil (unmodified)", *page.DomainID)
	}
}

// TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins is the edge
// case from spec.md: two concurrent AttachDomain calls targeting the same
// domain-less page must let exactly one succeed and the other fail with
// ErrDomainAlreadyAttached - no double-attach, no lost update.
//
// This does NOT launch two bare goroutines and hope they interleave: in
// practice the first transaction commits before the second even issues its
// SELECT, so the row is never actually contended and the test passes even
// with SELECT ... FOR UPDATE removed from AttachDomain (proven in review).
// Instead it drives the contention deterministically: an explicit "holder"
// transaction takes the same SELECT ... FOR UPDATE lock AttachDomain would
// take, performs the attach itself, and stays open (uncommitted) while a
// real AttachDomain call runs concurrently in a goroutine. Because the
// holder transaction genuinely holds the row lock in Postgres, this proves
// AttachDomain's second call cannot complete until the holder releases it,
// and that on release it observes the now-non-null domain_id and returns
// ErrDomainAlreadyAttached - not a lost update overwriting the holder's
// domain_id/subdomain.
func TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	firstDomainID := createAttachTestDomain(t, pool)
	secondDomainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "attach-concurrent-page")

	ctx := context.Background()

	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	var lockedDomainID *string
	holderRow := holderTx.QueryRow(ctx, "SELECT domain_id FROM status_pages WHERE id = $1 FOR UPDATE", pageID)
	if err := holderRow.Scan(&lockedDomainID); err != nil {
		t.Fatalf("holder SELECT ... FOR UPDATE failed: %v", err)
	}
	if lockedDomainID != nil {
		t.Fatalf("holder locked domain_id = %v, want nil before either attach", lockedDomainID)
	}
	if _, err := holderTx.Exec(ctx,
		"UPDATE status_pages SET domain_id = $1, subdomain = $2 WHERE id = $3",
		firstDomainID, "status-a", pageID,
	); err != nil {
		t.Fatalf("holder UPDATE failed: %v", err)
	}

	// Run the real AttachDomain call while holderTx is still open and
	// uncommitted. With the production SELECT ... FOR UPDATE in place,
	// this second call cannot observe a result until holderTx releases
	// the row lock below.
	done := make(chan error, 1)
	go func() {
		_, err := repo.AttachDomain(context.Background(), pageID, secondDomainID, "status-b")
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("AttachDomain() returned (err=%v) while the holder transaction was still open - the row lock did not block it", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked behind the holder's uncommitted row lock.
	}

	if err := holderTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit holder transaction: %v", err)
	}

	var attachErr error
	select {
	case attachErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AttachDomain() did not return after the holder transaction committed")
	}

	if !errors.Is(attachErr, ErrDomainAlreadyAttached) {
		t.Fatalf("AttachDomain() error = %v, want ErrDomainAlreadyAttached", attachErr)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if page.DomainID == nil || *page.DomainID != firstDomainID {
		t.Errorf("DomainID after contended attach = %v, want holder's %q (no lost update)", page.DomainID, firstDomainID)
	}
	if page.Subdomain == nil || *page.Subdomain != "status-a" {
		t.Errorf("Subdomain after contended attach = %v, want holder's %q (no lost update)", page.Subdomain, "status-a")
	}
}

// createSetServicesTestService inserts a minimal service fixture (no SLO
// integration needed - SetServices only cares about the service's id) and
// registers its cleanup.
func createSetServicesTestService(t *testing.T, pool *Pool, namePrefix string) string {
	t.Helper()
	services := NewServiceRepository(pool)
	service := &Service{Name: fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixNano()), SLOID: "slo-fixture-id"}
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := services.Create(ctx, service); err != nil {
			t.Fatalf("setup service Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID)
	})
	return service.ID
}

// TestStatusPageRepository_SetServices_ReplacesFullSet asserts SetServices
// fully replaces the linked-service set: an existing link not present in
// the new call is dropped, and a new one is added - not merged.
func TestStatusPageRepository_SetServices_ReplacesFullSet(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-page")
	serviceA := createSetServicesTestService(t, pool, "set-services-svc-a")
	serviceB := createSetServicesTestService(t, pool, "set-services-svc-b")

	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{{ServiceID: serviceA}}); err != nil {
		t.Fatalf("SetServices() first call returned unexpected error: %v", err)
	}
	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if len(page.ServiceIDs) != 1 || page.ServiceIDs[0] != serviceA {
		t.Fatalf("ServiceIDs after first SetServices() = %v, want [%q]", page.ServiceIDs, serviceA)
	}

	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{{ServiceID: serviceB}}); err != nil {
		t.Fatalf("SetServices() second call returned unexpected error: %v", err)
	}
	page, err = repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if len(page.ServiceIDs) != 1 || page.ServiceIDs[0] != serviceB {
		t.Fatalf("ServiceIDs after second SetServices() = %v, want [%q] (serviceA should have been dropped)", page.ServiceIDs, serviceB)
	}
}

// TestStatusPageRepository_SetServices_EmptySlice_UnlinksAll asserts an
// empty (non-nil) slice is a valid way to unlink every service.
func TestStatusPageRepository_SetServices_EmptySlice_UnlinksAll(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-unlink-page")
	serviceA := createSetServicesTestService(t, pool, "set-services-unlink-svc")

	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{{ServiceID: serviceA}}); err != nil {
		t.Fatalf("SetServices() (link) returned unexpected error: %v", err)
	}
	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{}); err != nil {
		t.Fatalf("SetServices() (unlink) returned unexpected error: %v", err)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if len(page.ServiceIDs) != 0 {
		t.Errorf("ServiceIDs after unlink = %v, want empty", page.ServiceIDs)
	}
}

// TestStatusPageRepository_SetServices_NonexistentPage_ErrNotFound asserts
// SetServices distinguishes "page doesn't exist" rather than silently
// succeeding on a no-op delete+insert.
func TestStatusPageRepository_SetServices_NonexistentPage_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	serviceA := createSetServicesTestService(t, pool, "set-services-missing-page-svc")

	err := repo.SetServices(context.Background(), "00000000-0000-0000-0000-000000000000", []ServicePlacement{{ServiceID: serviceA}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetServices() error = %v, want ErrNotFound", err)
	}
}

// readServiceLink reads back a single status_page_services row's
// group_id, parent_service_id and position directly, bypassing GetByID
// (which only surfaces ServiceIDs) - used to assert T4's new columns and
// to prove a rejected SetServices() call left the DB untouched.
func readServiceLink(t *testing.T, pool *Pool, statusPageID, serviceID string) (groupID, parentServiceID *string, position int, found bool) {
	t.Helper()
	row := pool.QueryRow(context.Background(),
		"SELECT group_id, parent_service_id, position FROM status_page_services WHERE status_page_id = $1 AND service_id = $2",
		statusPageID, serviceID,
	)
	if err := row.Scan(&groupID, &parentServiceID, &position); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, 0, false
		}
		t.Fatalf("reading back service link returned unexpected error: %v", err)
	}
	return groupID, parentServiceID, position, true
}

// countServiceLinks returns the number of status_page_services rows for
// statusPageID - used to assert a rejected SetServices() call performed no
// DELETE either.
func countServiceLinks(t *testing.T, pool *Pool, statusPageID string) int {
	t.Helper()
	var count int
	row := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM status_page_services WHERE status_page_id = $1", statusPageID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("counting service links returned unexpected error: %v", err)
	}
	return count
}

// TestStatusPageRepository_SetServices_PersistsGroupParentPosition asserts
// a full-replace SetServices() call persists all 3 new placement columns
// (group_id, parent_service_id, position) - not just service_id (T4).
func TestStatusPageRepository_SetServices_PersistsGroupParentPosition(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-placement-page")
	parent := createSetServicesTestService(t, pool, "set-services-placement-parent")
	child := createSetServicesTestService(t, pool, "set-services-placement-child")

	group, err := repo.CreateGroup(context.Background(), pageID, "Placement Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	placements := []ServicePlacement{
		{ServiceID: parent, GroupID: &group.ID, Position: 0},
		{ServiceID: child, GroupID: &group.ID, ParentServiceID: &parent, Position: 1},
	}
	if err := repo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("SetServices() returned unexpected error: %v", err)
	}

	gotGroupID, gotParentID, gotPosition, found := readServiceLink(t, pool, pageID, parent)
	if !found {
		t.Fatalf("service link for parent not found after SetServices()")
	}
	if gotGroupID == nil || *gotGroupID != group.ID {
		t.Errorf("parent link group_id = %v, want %q", gotGroupID, group.ID)
	}
	if gotParentID != nil {
		t.Errorf("parent link parent_service_id = %v, want nil", *gotParentID)
	}
	if gotPosition != 0 {
		t.Errorf("parent link position = %d, want 0", gotPosition)
	}

	gotGroupID, gotParentID, gotPosition, found = readServiceLink(t, pool, pageID, child)
	if !found {
		t.Fatalf("service link for child not found after SetServices()")
	}
	if gotGroupID == nil || *gotGroupID != group.ID {
		t.Errorf("child link group_id = %v, want %q", gotGroupID, group.ID)
	}
	if gotParentID == nil || *gotParentID != parent {
		t.Errorf("child link parent_service_id = %v, want %q", gotParentID, parent)
	}
	if gotPosition != 1 {
		t.Errorf("child link position = %d, want 1", gotPosition)
	}
}

// TestStatusPageRepository_SetServices_OmittingPlacementClearsIt asserts
// replace-all semantics for the new columns specifically: a second
// SetServices() call that omits a previously-set group_id/parent_service_id
// for a service clears it rather than preserving it (no merge - this is
// the easy place for a subtle bug per T4's brief).
func TestStatusPageRepository_SetServices_OmittingPlacementClearsIt(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-clear-page")
	parent := createSetServicesTestService(t, pool, "set-services-clear-parent")
	child := createSetServicesTestService(t, pool, "set-services-clear-child")

	group, err := repo.CreateGroup(context.Background(), pageID, "Clear Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	first := []ServicePlacement{
		{ServiceID: parent, GroupID: &group.ID, Position: 0},
		{ServiceID: child, GroupID: &group.ID, ParentServiceID: &parent, Position: 1},
	}
	if err := repo.SetServices(context.Background(), pageID, first); err != nil {
		t.Fatalf("SetServices() (first) returned unexpected error: %v", err)
	}

	// Second call re-submits both services but drops the group/parent
	// assignment entirely - replace-all must clear it, not merge with the
	// first call's values.
	second := []ServicePlacement{
		{ServiceID: parent, Position: 0},
		{ServiceID: child, Position: 1},
	}
	if err := repo.SetServices(context.Background(), pageID, second); err != nil {
		t.Fatalf("SetServices() (second) returned unexpected error: %v", err)
	}

	gotGroupID, gotParentID, _, found := readServiceLink(t, pool, pageID, child)
	if !found {
		t.Fatalf("service link for child not found after second SetServices()")
	}
	if gotGroupID != nil {
		t.Errorf("child link group_id after clearing call = %v, want nil (replace-all, not merge)", *gotGroupID)
	}
	if gotParentID != nil {
		t.Errorf("child link parent_service_id after clearing call = %v, want nil (replace-all, not merge)", *gotParentID)
	}
}

// TestStatusPageRepository_SetServices_ValidationErrors_DBUntouched asserts
// each of validateServicePlacements' 4 sentinel errors is surfaced by
// SetServices() without any DB mutation - the pre-call state (including
// zero rows, for the "no existing links" case) is read back unchanged
// after the rejected call.
func TestStatusPageRepository_SetServices_ValidationErrors_DBUntouched(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)

	otherPageID := createDomainlessStatusPage(t, repo, pool, "set-services-other-page")
	otherGroup, err := repo.CreateGroup(context.Background(), otherPageID, "Other Page Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() (other page) returned unexpected error: %v", err)
	}

	svcA := createSetServicesTestService(t, pool, "set-services-err-a")
	svcB := createSetServicesTestService(t, pool, "set-services-err-b")
	svcC := createSetServicesTestService(t, pool, "set-services-err-c")

	tests := []struct {
		name       string
		placements []ServicePlacement
		wantErr    error
	}{
		{
			name:       "group cross page",
			placements: []ServicePlacement{{ServiceID: svcA, GroupID: &otherGroup.ID}},
			wantErr:    ErrGroupCrossPage,
		},
		{
			name:       "self reference",
			placements: []ServicePlacement{{ServiceID: svcA, ParentServiceID: &svcA}},
			wantErr:    ErrSelfReference,
		},
		{
			name:       "parent not in set",
			placements: []ServicePlacement{{ServiceID: svcA, ParentServiceID: &svcB}},
			wantErr:    ErrParentNotInSet,
		},
		{
			name: "max depth exceeded",
			placements: []ServicePlacement{
				{ServiceID: svcA},
				{ServiceID: svcB, ParentServiceID: &svcA},
				{ServiceID: svcC, ParentServiceID: &svcB},
			},
			wantErr: ErrMaxDepthExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pageID := createDomainlessStatusPage(t, repo, pool, "set-services-validation-page")
			preCount := countServiceLinks(t, pool, pageID)

			err := repo.SetServices(context.Background(), pageID, tt.placements)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetServices() error = %v, want %v", err, tt.wantErr)
			}

			postCount := countServiceLinks(t, pool, pageID)
			if postCount != preCount {
				t.Errorf("service link count after rejected SetServices() = %d, want unchanged %d", postCount, preCount)
			}
		})
	}
}

// TestStatusPageRepository_SetServices_RemovingParentFromPage_ChildBecomesParentless
// covers SGD-13 (P2-AC7, spec.md's own Independent Test: "Remove A from the
// page, verify B renders parentless"): a parent linked to the page by one
// SetServices() call, then omitted from a second SetServices() call on the
// same page whose placements still include its former child, must not
// reject the second call with ErrParentNotInSet - the child's
// parent_service_id is instead cleared (SGD-13), same as if the parent
// service row had been deleted outright (migration 0046's ON DELETE SET
// NULL, exercised by the sibling DeleteParentService test above).
func TestStatusPageRepository_SetServices_RemovingParentFromPage_ChildBecomesParentless(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-sgd13-page")
	parent := createSetServicesTestService(t, pool, "set-services-sgd13-parent")
	child := createSetServicesTestService(t, pool, "set-services-sgd13-child")

	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{
		{ServiceID: parent},
		{ServiceID: child, ParentServiceID: &parent},
	}); err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
	}

	// Second call removes the parent from the page's placement set
	// entirely, while the child - still referencing it - stays.
	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{
		{ServiceID: child, ParentServiceID: &parent},
	}); err != nil {
		t.Fatalf("SetServices() removing the parent returned unexpected error: %v, want nil (SGD-13, child should become parentless)", err)
	}

	_, gotParentID, _, found := readServiceLink(t, pool, pageID, child)
	if !found {
		t.Fatalf("child service link not found after removing its parent from the page")
	}
	if gotParentID != nil {
		t.Errorf("child link parent_service_id after removing parent from page = %v, want nil (SGD-13)", *gotParentID)
	}
}

// TestStatusPageRepository_SetServices_ParentNeverOnPage_StillRejected
// guards the SGD-13 fix above against over-relaxing SGD-11: a
// ParentServiceID that was never linked to this status page at all (not
// just-removed by this call) must still be rejected with
// ErrParentNotInSet, same as before the SGD-13 fix.
func TestStatusPageRepository_SetServices_ParentNeverOnPage_StillRejected(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-sgd11-page")
	neverOnPage := createSetServicesTestService(t, pool, "set-services-sgd11-never-on-page")
	child := createSetServicesTestService(t, pool, "set-services-sgd11-child")

	err := repo.SetServices(context.Background(), pageID, []ServicePlacement{
		{ServiceID: child, ParentServiceID: &neverOnPage},
	})
	if !errors.Is(err, ErrParentNotInSet) {
		t.Fatalf("SetServices() error = %v, want ErrParentNotInSet (parent never linked to this page)", err)
	}
}

// TestStatusPageRepository_SetServices_DeleteGroup_SetsGroupIDNull asserts
// the group_id FK's ON DELETE SET NULL (migration 0046) is exercised by a
// real DeleteGroup() after SetServices() has linked a service to that
// group - the row survives, with group_id now NULL.
func TestStatusPageRepository_SetServices_DeleteGroup_SetsGroupIDNull(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-fk-group-page")
	svc := createSetServicesTestService(t, pool, "set-services-fk-group-svc")

	group, err := repo.CreateGroup(context.Background(), pageID, "FK Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}
	if err := repo.SetServices(context.Background(), pageID, []ServicePlacement{{ServiceID: svc, GroupID: &group.ID}}); err != nil {
		t.Fatalf("SetServices() returned unexpected error: %v", err)
	}

	if err := repo.DeleteGroup(context.Background(), testTenantID(t, pool), pageID, group.ID); err != nil {
		t.Fatalf("DeleteGroup() returned unexpected error: %v", err)
	}

	gotGroupID, _, _, found := readServiceLink(t, pool, pageID, svc)
	if !found {
		t.Fatalf("service link not found after DeleteGroup() - row should survive, not cascade-delete")
	}
	if gotGroupID != nil {
		t.Errorf("service link group_id after DeleteGroup() = %v, want nil (ON DELETE SET NULL)", *gotGroupID)
	}
}

// TestStatusPageRepository_SetServices_DeleteParentService_SetsParentIDNull
// asserts the parent_service_id FK's ON DELETE SET NULL (migration 0046)
// is exercised by a real delete of the parent service's services row after
// SetServices() has linked a child to it as parent - the child's link
// survives, with parent_service_id now NULL.
//
// validateAndSanitizeServicePlacements requires the parent to appear as its
// own placement in the same SetServices() call, or to have at least been
// linked to the page before this call (SGD-13, ErrParentNotInSet
// otherwise), which links it to the page via status_page_services.service_id
// too - a separate FK (status_page_services_service_id_fkey) with no ON
// DELETE SET NULL of its own, unrelated to T4's new columns. That row is
// removed directly first so the real DELETE FROM services below exercises
// only the parent_service_id FK this test targets, not that other one.
func TestStatusPageRepository_SetServices_DeleteParentService_SetsParentIDNull(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "set-services-fk-parent-page")
	parent := createSetServicesTestService(t, pool, "set-services-fk-parent-parent")
	child := createSetServicesTestService(t, pool, "set-services-fk-parent-child")

	placements := []ServicePlacement{
		{ServiceID: parent},
		{ServiceID: child, ParentServiceID: &parent},
	}
	if err := repo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("SetServices() returned unexpected error: %v", err)
	}

	if _, err := pool.Exec(context.Background(),
		"DELETE FROM status_page_services WHERE status_page_id = $1 AND service_id = $2", pageID, parent,
	); err != nil {
		t.Fatalf("removing parent's own membership row returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", parent); err != nil {
		t.Fatalf("deleting parent service returned unexpected error: %v", err)
	}

	gotGroupID, gotParentID, _, found := readServiceLink(t, pool, pageID, child)
	_ = gotGroupID
	if !found {
		t.Fatalf("child service link not found after parent service delete - row should survive, not cascade-delete")
	}
	if gotParentID != nil {
		t.Errorf("child link parent_service_id after parent service delete = %v, want nil (ON DELETE SET NULL)", *gotParentID)
	}
}

// TestStatusPageRepository_Create_PopulatesServiceIDs asserts Create fills
// in ServiceIDs on the returned StatusPage from exactly the ids it was
// given, so callers (the admin API's create response) don't need a
// separate reload to show what got linked.
func TestStatusPageRepository_Create_PopulatesServiceIDs(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	serviceA := createSetServicesTestService(t, pool, "create-populates-svc")

	statusPage := &StatusPage{Name: fmt.Sprintf("create-populates-page-%d", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), statusPage, []string{serviceA}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	if len(statusPage.ServiceIDs) != 1 || statusPage.ServiceIDs[0] != serviceA {
		t.Errorf("ServiceIDs = %v, want [%q]", statusPage.ServiceIDs, serviceA)
	}
}

// seedStatusPageFixtures creates n domain-less status pages named
// prefix-0..prefix-(n-1) and registers their cleanup.
func seedStatusPageFixtures(t *testing.T, repo *StatusPageRepository, pool *Pool, prefix string, n int) []*StatusPage {
	t.Helper()
	pages := make([]*StatusPage, n)
	for i := 0; i < n; i++ {
		sp := &StatusPage{Name: fmt.Sprintf("%s-%d", prefix, i)}
		if err := repo.Create(context.Background(), sp, nil); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", sp.ID)
		})
		pages[i] = sp
	}
	return pages
}

func TestStatusPageRepository_ListPaginated_Page1_ReturnsExactlyPageSizeAndCorrectTotal(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	prefix := fmt.Sprintf("list-paginated-p1-%d", time.Now().UnixNano())
	seedStatusPageFixtures(t, repo, pool, prefix, 22)

	items, total, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	if len(items) != 20 {
		t.Errorf("len(items) = %d, want 20 (page_size)", len(items))
	}
	if total < 22 {
		t.Errorf("total = %d, want >= 22", total)
	}
}

func TestStatusPageRepository_ListPaginated_Page2_ReturnsRemainder(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	prefix := fmt.Sprintf("list-paginated-p2-%d", time.Now().UnixNano())
	seedStatusPageFixtures(t, repo, pool, prefix, 22)

	page1, total1, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated(page=1) returned unexpected error: %v", err)
	}
	page2, total2, err := repo.ListPaginated(context.Background(), 2, 20)
	if err != nil {
		t.Fatalf("ListPaginated(page=2) returned unexpected error: %v", err)
	}

	if total1 != total2 {
		t.Errorf("total differs between page 1 (%d) and page 2 (%d), want equal", total1, total2)
	}

	wantPage2Len := total1 - 20
	if wantPage2Len > 20 {
		wantPage2Len = 20
	}
	if len(page2) != wantPage2Len {
		t.Errorf("len(page2 items) = %d, want %d (min(page_size, total-page_size))", len(page2), wantPage2Len)
	}

	seen := map[string]bool{}
	for _, sp := range page1 {
		seen[sp.ID] = true
	}
	for _, sp := range page2 {
		if seen[sp.ID] {
			t.Errorf("status page %s appeared on both page 1 and page 2", sp.ID)
		}
	}
}

func TestStatusPageRepository_ListPaginated_PageBeyondLast_EmptyItemsCorrectTotal(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	prefix := fmt.Sprintf("list-paginated-beyond-%d", time.Now().UnixNano())
	seedStatusPageFixtures(t, repo, pool, prefix, 3)

	_, totalFirst, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated(page=1) returned unexpected error: %v", err)
	}

	items, total, err := repo.ListPaginated(context.Background(), 999, 20)
	if err != nil {
		t.Fatalf("ListPaginated(page=999) returned unexpected error: %v", err)
	}

	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0 for a page beyond the last", len(items))
	}
	if total != totalFirst {
		t.Errorf("total for out-of-range page = %d, want %d (same as page 1's total, via the zero-row fallback)", total, totalFirst)
	}
}

// TestStatusPageRepository_ListPaginated_PopulatesServiceIDs asserts the
// serviceIDsByStatusPage batch lookup still attaches ServiceIDs correctly
// when scoped to just the paged subset of IDs (design.md: this now runs
// against only the paged IDs, not every status page in the table).
// TestAttachedNamesByDomainIDs_EmptyInput_ReturnsEmptyMapNoQuery asserts
// an empty domainIDs slice short-circuits to an empty map without error
// (domains-status-pages-page T1: DomainsHandler.List can call this
// unconditionally even on an empty page).
func TestAttachedNamesByDomainIDs_EmptyInput_ReturnsEmptyMapNoQuery(t *testing.T) {
	repo, _ := newAttachDomainTestRepo(t)

	got, err := repo.AttachedNamesByDomainIDs(context.Background(), []string{})
	if err != nil {
		t.Fatalf("AttachedNamesByDomainIDs() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("AttachedNamesByDomainIDs() = %v, want empty map", got)
	}
}

// TestAttachedNamesByDomainIDs_ZeroAttached_KeyAbsentFromMap asserts a
// domain with no attached status pages simply has no key in the result
// map (DSP-02's zero-attached case at the repository layer).
func TestAttachedNamesByDomainIDs_ZeroAttached_KeyAbsentFromMap(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)

	got, err := repo.AttachedNamesByDomainIDs(context.Background(), []string{domainID})
	if err != nil {
		t.Fatalf("AttachedNamesByDomainIDs() returned unexpected error: %v", err)
	}
	if _, ok := got[domainID]; ok {
		t.Errorf("AttachedNamesByDomainIDs()[%q] = %v, want key absent (zero attached)", domainID, got[domainID])
	}
}

// TestAttachedNamesByDomainIDs_OneAttached_ReturnsSingleName asserts
// DSP-03: a domain with exactly one attached page returns that page's
// name.
func TestAttachedNamesByDomainIDs_OneAttached_ReturnsSingleName(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "attached-names-one")
	if _, err := repo.AttachDomain(context.Background(), pageID, domainID, "status"); err != nil {
		t.Fatalf("setup AttachDomain() returned unexpected error: %v", err)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}

	got, err := repo.AttachedNamesByDomainIDs(context.Background(), []string{domainID})
	if err != nil {
		t.Fatalf("AttachedNamesByDomainIDs() returned unexpected error: %v", err)
	}
	if len(got[domainID]) != 1 || got[domainID][0] != page.Name {
		t.Errorf("AttachedNamesByDomainIDs()[%q] = %v, want [%q]", domainID, got[domainID], page.Name)
	}
}

// TestAttachedNamesByDomainIDs_TwoOrMoreAttached_OrderedByCreatedAt
// asserts DSP-04: a domain with 2+ attached pages returns every name,
// ordered created_at ASC so the caller can treat the first as "primary".
// AttachDomain can only be called once per page (SPD-06), so this fixture
// creates two status pages already carrying their domain_id at insert
// time (bypassing AttachDomain), sequencing their created_at explicitly.
func TestAttachedNamesByDomainIDs_TwoOrMoreAttached_OrderedByCreatedAt(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	domainID := createAttachTestDomain(t, pool)

	subdomainA := "status-a"
	subdomainB := "status-b"
	pageA := &StatusPage{Name: fmt.Sprintf("attached-names-multi-a-%d", time.Now().UnixNano()), Subdomain: &subdomainA, DomainID: &domainID}
	if err := repo.Create(context.Background(), pageA, nil); err != nil {
		t.Fatalf("setup Create() (page A) returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", pageA.ID) })

	pageB := &StatusPage{Name: fmt.Sprintf("attached-names-multi-b-%d", time.Now().UnixNano()), Subdomain: &subdomainB, DomainID: &domainID}
	if err := repo.Create(context.Background(), pageB, nil); err != nil {
		t.Fatalf("setup Create() (page B) returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", pageB.ID) })

	// Force a deterministic created_at ordering rather than relying on two
	// inserts issued microseconds apart.
	if _, err := pool.Exec(context.Background(), "UPDATE status_pages SET created_at = $1 WHERE id = $2",
		time.Now().Add(-1*time.Hour), pageA.ID); err != nil {
		t.Fatalf("failed to backdate page A created_at: %v", err)
	}

	got, err := repo.AttachedNamesByDomainIDs(context.Background(), []string{domainID})
	if err != nil {
		t.Fatalf("AttachedNamesByDomainIDs() returned unexpected error: %v", err)
	}
	want := []string{pageA.Name, pageB.Name}
	if len(got[domainID]) != 2 || got[domainID][0] != want[0] || got[domainID][1] != want[1] {
		t.Errorf("AttachedNamesByDomainIDs()[%q] = %v, want %v (ordered created_at ASC)", domainID, got[domainID], want)
	}
}

func TestStatusPageRepository_ListPaginated_PopulatesServiceIDs(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	serviceA := createSetServicesTestService(t, pool, "list-paginated-svc-ids")

	statusPage := &StatusPage{Name: fmt.Sprintf("list-paginated-svc-ids-page-%d", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), statusPage, []string{serviceA}); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	found := false
	for page := 1; ; page++ {
		items, total, err := repo.ListPaginated(context.Background(), page, 20)
		if err != nil {
			t.Fatalf("ListPaginated() returned unexpected error: %v", err)
		}
		for _, sp := range items {
			if sp.ID == statusPage.ID {
				found = true
				if len(sp.ServiceIDs) != 1 || sp.ServiceIDs[0] != serviceA {
					t.Errorf("ServiceIDs = %v, want [%q]", sp.ServiceIDs, serviceA)
				}
			}
		}
		if found || len(items) == 0 || page*20 >= total {
			break
		}
	}
	if !found {
		t.Fatalf("status page %s not found across any page of ListPaginated()", statusPage.ID)
	}
}

// TestStatusPageRepository_CreateGroup_AppendsAtEndPosition asserts
// CreateGroup sets Position to one past the highest position already used
// on the page, so repeated calls append in order rather than colliding at 0.
func TestStatusPageRepository_CreateGroup_AppendsAtEndPosition(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "create-group-page")

	first, err := repo.CreateGroup(context.Background(), pageID, "First Group")
	if err != nil {
		t.Fatalf("CreateGroup() first call returned unexpected error: %v", err)
	}
	if first.Position != 0 {
		t.Errorf("first group Position = %d, want 0", first.Position)
	}
	if first.StatusPageID != pageID {
		t.Errorf("first group StatusPageID = %q, want %q", first.StatusPageID, pageID)
	}
	if first.Name != "First Group" {
		t.Errorf("first group Name = %q, want %q", first.Name, "First Group")
	}
	if first.ID == "" {
		t.Error("first group ID is empty, want generated id")
	}

	second, err := repo.CreateGroup(context.Background(), pageID, "Second Group")
	if err != nil {
		t.Fatalf("CreateGroup() second call returned unexpected error: %v", err)
	}
	if second.Position != 1 {
		t.Errorf("second group Position = %d, want 1 (appended after first)", second.Position)
	}
}

// TestStatusPageRepository_CreateGroup_AfterDeletingMiddleGroup_NoDuplicatePosition
// is the regression test for the COUNT(*) bug this fix replaces: create
// G0/G1/G2 (positions 0/1/2), delete G0 (positions 1/2 remain), then create
// G3 - COUNT(*) would return 2 (only 2 rows exist), colliding with G2's own
// position 2 and leaving their display order ambiguous. The
// COALESCE(MAX(position), -1)+1 fix must give G3 position 3 instead,
// regardless of how many rows currently exist.
func TestStatusPageRepository_CreateGroup_AfterDeletingMiddleGroup_NoDuplicatePosition(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "create-group-after-delete-page")
	tenantID := testTenantID(t, pool)

	g0, err := repo.CreateGroup(context.Background(), pageID, "G0")
	if err != nil {
		t.Fatalf("CreateGroup() G0 returned unexpected error: %v", err)
	}
	g1, err := repo.CreateGroup(context.Background(), pageID, "G1")
	if err != nil {
		t.Fatalf("CreateGroup() G1 returned unexpected error: %v", err)
	}
	g2, err := repo.CreateGroup(context.Background(), pageID, "G2")
	if err != nil {
		t.Fatalf("CreateGroup() G2 returned unexpected error: %v", err)
	}
	if g1.Position != 1 || g2.Position != 2 {
		t.Fatalf("setup positions = G1:%d G2:%d, want G1:1 G2:2", g1.Position, g2.Position)
	}

	if err := repo.DeleteGroup(context.Background(), tenantID, pageID, g0.ID); err != nil {
		t.Fatalf("DeleteGroup() G0 returned unexpected error: %v", err)
	}

	g3, err := repo.CreateGroup(context.Background(), pageID, "G3")
	if err != nil {
		t.Fatalf("CreateGroup() G3 returned unexpected error: %v", err)
	}
	if g3.Position != 3 {
		t.Errorf("G3 Position = %d, want 3 (one past G2's position, not COUNT(*)'s 2 which would collide with G2)", g3.Position)
	}
	if g3.Position == g2.Position {
		t.Errorf("G3 Position = %d collides with G2 Position = %d", g3.Position, g2.Position)
	}
}

// TestStatusPageRepository_UpdateGroup_RenamesAndRepositions asserts
// UpdateGroup can change both Name and Position on an existing group.
func TestStatusPageRepository_UpdateGroup_RenamesAndRepositions(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "update-group-page")

	group, err := repo.CreateGroup(context.Background(), pageID, "Original Name")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	updated, err := repo.UpdateGroup(context.Background(), testTenantID(t, pool), pageID, group.ID, "Renamed", 5)
	if err != nil {
		t.Fatalf("UpdateGroup() returned unexpected error: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("updated group Name = %q, want %q", updated.Name, "Renamed")
	}
	if updated.Position != 5 {
		t.Errorf("updated group Position = %d, want 5", updated.Position)
	}
	if updated.ID != group.ID {
		t.Errorf("updated group ID = %q, want %q (unchanged)", updated.ID, group.ID)
	}
}

// TestStatusPageRepository_UpdateGroup_NonexistentID_ErrNotFound asserts
// UpdateGroup distinguishes "group doesn't exist" rather than silently
// succeeding on a no-op UPDATE.
func TestStatusPageRepository_UpdateGroup_NonexistentID_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "update-group-missing-page")

	_, err := repo.UpdateGroup(context.Background(), testTenantID(t, pool), pageID, "00000000-0000-0000-0000-000000000000", "Name", 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateGroup() error = %v, want ErrNotFound", err)
	}
}

// TestStatusPageRepository_UpdateGroup_GroupBelongsToDifferentStatusPage_ErrNotFound
// asserts UpdateGroup's status_page_id filter rejects a group id that
// exists but belongs to a different status page, even one in the same
// tenant - RLS (migration 0049) only stops cross-tenant access, so this
// filter is the only thing stopping cross-page mutation within one tenant
// (security review finding).
func TestStatusPageRepository_UpdateGroup_GroupBelongsToDifferentStatusPage_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	ownPageID := createDomainlessStatusPage(t, repo, pool, "update-group-cross-page-own")
	otherPageID := createDomainlessStatusPage(t, repo, pool, "update-group-cross-page-other")

	group, err := repo.CreateGroup(context.Background(), otherPageID, "Belongs To Other Page")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	_, err = repo.UpdateGroup(context.Background(), testTenantID(t, pool), ownPageID, group.ID, "Hijacked", 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateGroup() error = %v, want ErrNotFound when groupId belongs to a different status page", err)
	}

	unchanged, err := repo.ListGroups(context.Background(), otherPageID)
	if err != nil {
		t.Fatalf("ListGroups() returned unexpected error: %v", err)
	}
	if len(unchanged) != 1 || unchanged[0].Name != "Belongs To Other Page" {
		t.Fatalf("group on otherPageID changed after cross-page UpdateGroup attempt: %+v", unchanged)
	}
}

// TestStatusPageRepository_DeleteGroup_RemovesGroupAndLeavesServiceGroupIDNull
// asserts DeleteGroup removes the group row itself, and that a previously
// assigned service's group_id becomes NULL (via the FK's ON DELETE SET
// NULL from migration 0046) rather than the service row being deleted too.
func TestStatusPageRepository_DeleteGroup_RemovesGroupAndLeavesServiceGroupIDNull(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "delete-group-page")
	serviceID := createSetServicesTestService(t, pool, "delete-group-svc")

	group, err := repo.CreateGroup(context.Background(), pageID, "To Delete")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_page_services (status_page_id, service_id, group_id) VALUES ($1, $2, $3)",
		pageID, serviceID, group.ID,
	); err != nil {
		t.Fatalf("setup linking service to group returned unexpected error: %v", err)
	}

	if err := repo.DeleteGroup(context.Background(), testTenantID(t, pool), pageID, group.ID); err != nil {
		t.Fatalf("DeleteGroup() returned unexpected error: %v", err)
	}

	groups, err := repo.ListGroups(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListGroups() after delete returned unexpected error: %v", err)
	}
	for _, g := range groups {
		if g.ID == group.ID {
			t.Fatalf("group %s still present after DeleteGroup()", group.ID)
		}
	}

	var groupID *string
	row := pool.QueryRow(context.Background(),
		"SELECT group_id FROM status_page_services WHERE status_page_id = $1 AND service_id = $2", pageID, serviceID,
	)
	if err := row.Scan(&groupID); err != nil {
		t.Fatalf("reading back service link's group_id returned unexpected error: %v", err)
	}
	if groupID != nil {
		t.Errorf("service link group_id after DeleteGroup() = %v, want nil (SET NULL via FK)", *groupID)
	}
}

// TestStatusPageRepository_DeleteGroup_NonexistentID_ErrNotFound asserts
// DeleteGroup distinguishes "group doesn't exist" rather than silently
// succeeding on a no-op DELETE.
func TestStatusPageRepository_DeleteGroup_NonexistentID_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "delete-group-missing-page")

	err := repo.DeleteGroup(context.Background(), testTenantID(t, pool), pageID, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteGroup() error = %v, want ErrNotFound", err)
	}
}

// TestStatusPageRepository_DeleteGroup_GroupBelongsToDifferentStatusPage_ErrNotFound
// asserts DeleteGroup's status_page_id filter rejects a group id that
// exists but belongs to a different status page, even one in the same
// tenant - mirrors UpdateGroup's equivalent test (security review finding).
func TestStatusPageRepository_DeleteGroup_GroupBelongsToDifferentStatusPage_ErrNotFound(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	ownPageID := createDomainlessStatusPage(t, repo, pool, "delete-group-cross-page-own")
	otherPageID := createDomainlessStatusPage(t, repo, pool, "delete-group-cross-page-other")

	group, err := repo.CreateGroup(context.Background(), otherPageID, "Belongs To Other Page")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	err = repo.DeleteGroup(context.Background(), testTenantID(t, pool), ownPageID, group.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteGroup() error = %v, want ErrNotFound when groupId belongs to a different status page", err)
	}

	stillThere, err := repo.ListGroups(context.Background(), otherPageID)
	if err != nil {
		t.Fatalf("ListGroups() returned unexpected error: %v", err)
	}
	if len(stillThere) != 1 || stillThere[0].ID != group.ID {
		t.Fatalf("group on otherPageID was deleted by cross-page DeleteGroup attempt: %+v", stillThere)
	}
}

// TestStatusPageRepository_ListGroups_OrderedByPosition asserts ListGroups
// returns a page's groups sorted by position, not insertion/created_at
// order.
func TestStatusPageRepository_ListGroups_OrderedByPosition(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "list-groups-page")

	groupA, err := repo.CreateGroup(context.Background(), pageID, "Group A")
	if err != nil {
		t.Fatalf("setup CreateGroup() (A) returned unexpected error: %v", err)
	}
	groupB, err := repo.CreateGroup(context.Background(), pageID, "Group B")
	if err != nil {
		t.Fatalf("setup CreateGroup() (B) returned unexpected error: %v", err)
	}
	// Reposition so B now sorts before A, proving ListGroups orders by the
	// position column rather than created_at/insertion order.
	tenantID := testTenantID(t, pool)
	if _, err := repo.UpdateGroup(context.Background(), tenantID, pageID, groupA.ID, groupA.Name, 1); err != nil {
		t.Fatalf("setup UpdateGroup() (A) returned unexpected error: %v", err)
	}
	if _, err := repo.UpdateGroup(context.Background(), tenantID, pageID, groupB.ID, groupB.Name, 0); err != nil {
		t.Fatalf("setup UpdateGroup() (B) returned unexpected error: %v", err)
	}

	groups, err := repo.ListGroups(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListGroups() returned unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups[0].ID != groupB.ID || groups[1].ID != groupA.ID {
		t.Fatalf("groups order = [%s, %s], want [%s, %s] (position-ordered)",
			groups[0].ID, groups[1].ID, groupB.ID, groupA.ID)
	}
}

// TestStatusPageRepository_Delete_AlsoRemovesServiceGroups asserts Delete's
// manual-cleanup transaction also clears status_page_service_groups for the
// deleted page, since that table has no cascade from status_pages either
// (same reasoning as the existing status_page_services cleanup).
func TestStatusPageRepository_Delete_AlsoRemovesServiceGroups(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	pageID := createDomainlessStatusPage(t, repo, pool, "delete-page-groups")

	group, err := repo.CreateGroup(context.Background(), pageID, "Orphaned Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	if err := repo.Delete(context.Background(), pageID); err != nil {
		t.Fatalf("Delete() returned unexpected error: %v", err)
	}

	var count int
	row := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM status_page_service_groups WHERE id = $1", group.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("counting leftover group rows returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("status_page_service_groups rows remaining after Delete() = %d, want 0", count)
	}
}
