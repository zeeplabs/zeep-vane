//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// TestStatusPageRepository_Create_NoDomain_ReturnsNullDomainAndSubdomain
// asserts SPD-01: a status page can be created with DomainID and Subdomain
// both nil, and the returned row reflects both as nil (no domain forced on
// creation).
func TestStatusPageRepository_Create_NoDomain_ReturnsNullDomainAndSubdomain(t *testing.T) {
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
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

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

// TestStatusPageRepository_List_MixOfDomainedAndDomainless_CorrectNullability
// asserts SPD-01/SPD-05: List returns both a domain-less and a domained
// row with correct nullability on each.
func TestStatusPageRepository_List_MixOfDomainedAndDomainless_CorrectNullability(t *testing.T) {
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

	statusPages, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}

	var foundNoDomain, foundWithDomain bool
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
	if !foundNoDomain {
		t.Error("List() did not include the domain-less status page")
	}
	if !foundWithDomain {
		t.Error("List() did not include the with-domain status page")
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
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewStatusPageRepository(pool), pool
}

// TestAttachDomain_DomainlessPage_SucceedsWithStateUnchanged asserts
// SPD-06: attaching a domain to a domain-less page sets both fields and
// leaves state as "draft" (the existing on-demand TLS mechanism takes
// over from here, unmodified by this call).
func TestAttachDomain_DomainlessPage_SucceedsWithStateUnchanged(t *testing.T) {
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
	if updated.State != "draft" {
		t.Errorf("State = %q, want %q", updated.State, "draft")
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
// ErrDomainAlreadyAttached - no double-attach, no lost update. This runs
// both calls in real goroutines against the real database (not mocked) so
// the SELECT ... FOR UPDATE row lock is actually exercised: the second
// goroutine's SELECT blocks until the first transaction commits or rolls
// back, at which point it observes the now-non-null domain_id.
func TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins(t *testing.T) {
	repo, pool := newAttachDomainTestRepo(t)
	firstDomainID := createAttachTestDomain(t, pool)
	secondDomainID := createAttachTestDomain(t, pool)
	pageID := createDomainlessStatusPage(t, repo, pool, "attach-concurrent-page")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = repo.AttachDomain(context.Background(), pageID, firstDomainID, "status-a")
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = repo.AttachDomain(context.Background(), pageID, secondDomainID, "status-b")
	}()
	wg.Wait()

	successes := 0
	alreadyAttached := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrDomainAlreadyAttached):
			alreadyAttached++
		default:
			t.Fatalf("unexpected error from concurrent AttachDomain: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("successes = %d, want 1", successes)
	}
	if alreadyAttached != 1 {
		t.Errorf("alreadyAttached = %d, want 1", alreadyAttached)
	}

	page, err := repo.GetByID(context.Background(), pageID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if page.DomainID == nil {
		t.Fatal("DomainID after concurrent attaches = nil, want exactly one winner's domain set")
	}
	if *page.DomainID != firstDomainID && *page.DomainID != secondDomainID {
		t.Errorf("DomainID = %q, want one of %q or %q", *page.DomainID, firstDomainID, secondDomainID)
	}
}
