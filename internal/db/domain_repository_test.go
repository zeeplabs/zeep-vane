//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// newDomainRepoTestPool boots a migrated pool and a fresh *DomainRepository
// backed by it.
func newDomainRepoTestPool(t *testing.T) (*DomainRepository, *Pool) {
	t.Helper()
	pool, _ := newTenantScopedPool(t)
	return NewDomainRepository(pool), pool
}

// seedDomainFixtures creates n domains named prefix-0.example.com..
// prefix-(n-1).example.com and registers their cleanup.
func seedDomainFixtures(t *testing.T, repo *DomainRepository, pool *Pool, prefix string, n int) []*Domain {
	t.Helper()
	domains := make([]*Domain, n)
	for i := 0; i < n; i++ {
		d := &Domain{Hostname: fmt.Sprintf("%s-%d.example.com", prefix, i)}
		if err := repo.Create(context.Background(), d); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", d.ID)
		})
		domains[i] = d
	}
	return domains
}

func TestDomainRepository_ListPaginated_Page1_ReturnsExactlyPageSizeAndCorrectTotal(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p1-%d", time.Now().UnixNano())
	seedDomainFixtures(t, repo, pool, prefix, 22)

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

func TestDomainRepository_ListPaginated_Page2_ReturnsRemainder(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p2-%d", time.Now().UnixNano())
	seedDomainFixtures(t, repo, pool, prefix, 22)

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
	for _, d := range page1 {
		seen[d.ID] = true
	}
	for _, d := range page2 {
		if seen[d.ID] {
			t.Errorf("domain %s appeared on both page 1 and page 2", d.ID)
		}
	}
}

func TestDomainRepository_ListPaginated_PageBeyondLast_EmptyItemsCorrectTotal(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-beyond-%d", time.Now().UnixNano())
	seedDomainFixtures(t, repo, pool, prefix, 3)

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

func TestDomainRepository_ListPaginated_OrderByHostnameUnchanged(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	// A prefix guarantees these fixtures sort together and in a known
	// relative order (hostname ASC), regardless of other domains already
	// present in this shared-DB suite.
	prefix := fmt.Sprintf("aaa-order-%d", time.Now().UnixNano())
	seeded := seedDomainFixtures(t, repo, pool, prefix, 5)

	items, _, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	var ours []Domain
	for _, d := range items {
		for _, s := range seeded {
			if d.ID == s.ID {
				ours = append(ours, d)
			}
		}
	}
	if len(ours) != len(seeded) {
		t.Fatalf("found %d of this test's domains in ListPaginated(), want %d", len(ours), len(seeded))
	}
	for i, d := range ours {
		if d.ID != seeded[i].ID {
			t.Errorf("ours[%d].ID = %q, want %q (ORDER BY hostname)", i, d.ID, seeded[i].ID)
		}
	}
}

// TestDomainRepository_Create_DefaultsToPendingCustom covers DOMVER-02: a
// newly created domain starts as domain_type=custom, status=pending,
// verified_at=NULL.
func TestDomainRepository_Create_DefaultsToPendingCustom(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("create-defaults-%d.example.com", time.Now().UnixNano())}

	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	if domain.DomainType != "custom" {
		t.Errorf("DomainType = %q, want %q", domain.DomainType, "custom")
	}
	if domain.Status != "pending" {
		t.Errorf("Status = %q, want %q", domain.Status, "pending")
	}
	if domain.VerifiedAt != nil {
		t.Errorf("VerifiedAt = %v, want nil", domain.VerifiedAt)
	}
	if domain.LastError != nil {
		t.Errorf("LastError = %v, want nil", domain.LastError)
	}
}

// TestDomainRepository_Create_PersistsNonEmptyVerificationToken covers
// DATV-01: Create generates and persists a non-empty verification token.
func TestDomainRepository_Create_PersistsNonEmptyVerificationToken(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("verify-token-%d.example.com", time.Now().UnixNano())}

	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	if domain.VerificationToken == "" {
		t.Error("VerificationToken = \"\", want a non-empty generated token")
	}
	if len(domain.VerificationToken) != 32 {
		t.Errorf("len(VerificationToken) = %d, want 32 (16 random bytes, hex-encoded)", len(domain.VerificationToken))
	}
}

// TestDomainRepository_Create_TwoDomains_DistinctVerificationTokens covers
// the spec's "unique verification token" requirement: two domains never
// share a token.
func TestDomainRepository_Create_TwoDomains_DistinctVerificationTokens(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	ctx := context.Background()
	a := &Domain{Hostname: fmt.Sprintf("verify-token-a-%d.example.com", time.Now().UnixNano())}
	b := &Domain{Hostname: fmt.Sprintf("verify-token-b-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create(a) returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM domains WHERE id = $1", a.ID) })
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("Create(b) returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM domains WHERE id = $1", b.ID) })

	if a.VerificationToken == b.VerificationToken {
		t.Errorf("both domains got the same VerificationToken %q, want distinct tokens", a.VerificationToken)
	}
}

// TestDomainRepository_GetByID_ReturnsPersistedVerificationToken covers T2's
// Done-when: GetByID's scan includes verification_token.
func TestDomainRepository_GetByID_ReturnsPersistedVerificationToken(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("verify-token-getbyid-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	got, err := repo.GetByID(context.Background(), domain.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.VerificationToken == "" {
		t.Error("GetByID() VerificationToken = \"\", want the persisted token")
	}
	if got.VerificationToken != domain.VerificationToken {
		t.Errorf("GetByID() VerificationToken = %q, want %q", got.VerificationToken, domain.VerificationToken)
	}
}

// TestDomainRepository_ListPaginated_IncludesVerificationToken covers T2's
// Done-when: ListPaginated's scan includes verification_token.
func TestDomainRepository_ListPaginated_IncludesVerificationToken(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("verify-token-list-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	items, _, err := repo.ListPaginated(context.Background(), 1, 1000)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	var found *Domain
	for i := range items {
		if items[i].ID == domain.ID {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("created domain %s not found in ListPaginated()", domain.ID)
	}
	if found.VerificationToken != domain.VerificationToken {
		t.Errorf("ListPaginated() VerificationToken = %q, want %q", found.VerificationToken, domain.VerificationToken)
	}
}

// TestDomainRepository_GetByID_Existing_ReturnsDomain covers a basic lookup.
func TestDomainRepository_GetByID_Existing_ReturnsDomain(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("get-by-id-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	got, err := repo.GetByID(context.Background(), domain.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.Hostname != domain.Hostname {
		t.Errorf("Hostname = %q, want %q", got.Hostname, domain.Hostname)
	}
}

// TestDomainRepository_GetByID_Unknown_ErrNotFound covers the not-found
// path.
func TestDomainRepository_GetByID_Unknown_ErrNotFound(t *testing.T) {
	repo, _ := newDomainRepoTestPool(t)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByID() error = %v, want ErrNotFound", err)
	}
}

// TestDomainRepository_SetVerificationResult_Success_UpdatesAllFields
// covers DOMVER-04/07: a successful check's result is persisted in full.
func TestDomainRepository_SetVerificationResult_Success_UpdatesAllFields(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("verify-success-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	checkedAt := time.Now().UTC().Truncate(time.Millisecond)
	updated, err := repo.SetVerificationResult(context.Background(), domain.ID, "verified", nil, checkedAt)
	if err != nil {
		t.Fatalf("SetVerificationResult() returned unexpected error: %v", err)
	}
	if updated.Status != "verified" {
		t.Errorf("Status = %q, want %q", updated.Status, "verified")
	}
	if updated.LastError != nil {
		t.Errorf("LastError = %v, want nil", updated.LastError)
	}
	if updated.VerifiedAt == nil || !updated.VerifiedAt.Equal(checkedAt) {
		t.Errorf("VerifiedAt = %v, want %v", updated.VerifiedAt, checkedAt)
	}
}

// TestDomainRepository_SetVerificationResult_Failure_PersistsLastError
// covers DOMVER-08: a failed check's error message is persisted.
func TestDomainRepository_SetVerificationResult_Failure_PersistsLastError(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("verify-failure-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	errMsg := "TXT record not found"
	updated, err := repo.SetVerificationResult(context.Background(), domain.ID, "error", &errMsg, time.Now())
	if err != nil {
		t.Fatalf("SetVerificationResult() returned unexpected error: %v", err)
	}
	if updated.Status != "error" {
		t.Errorf("Status = %q, want %q", updated.Status, "error")
	}
	if updated.LastError == nil || *updated.LastError != errMsg {
		t.Errorf("LastError = %v, want %q", updated.LastError, errMsg)
	}
}

// TestDomainRepository_SetVerificationResult_Unknown_ErrNotFound covers the
// not-found path.
func TestDomainRepository_SetVerificationResult_Unknown_ErrNotFound(t *testing.T) {
	repo, _ := newDomainRepoTestPool(t)

	_, err := repo.SetVerificationResult(context.Background(), "00000000-0000-0000-0000-000000000000", "verified", nil, time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetVerificationResult() error = %v, want ErrNotFound", err)
	}
}

// newDomainRepoScratchPool returns a DomainRepository backed by a pool on a
// fresh scratch database, so an unfiltered COUNT is deterministic instead of
// racing other suites that share TEST_DATABASE_URL.
func newDomainRepoScratchPool(t *testing.T) (*DomainRepository, *Pool) {
	t.Helper()
	dsn := newScratchDatabase(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	admin, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() (admin) returned unexpected error: %v", err)
	}

	var tenantID string
	if err := admin.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "countverified-fixture-tenant").Scan(&tenantID); err != nil {
		admin.Close()
		t.Fatalf("seeding fixture tenant returned unexpected error: %v", err)
	}
	admin.Close()

	pool, err := NewPool(ctx, dbtest.TenantScopedDSN(dsn, tenantID))
	if err != nil {
		t.Fatalf("NewPool() (tenant-scoped) returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewDomainRepository(pool), pool
}

func TestDomainRepository_CountVerified_MixedStatuses_CountsOnlyVerified(t *testing.T) {
	repo, _ := newDomainRepoScratchPool(t)
	ctx := context.Background()

	baseline, err := repo.CountVerified(ctx)
	if err != nil {
		t.Fatalf("CountVerified() (baseline) returned unexpected error: %v", err)
	}
	if baseline != 0 {
		t.Fatalf("baseline = %d, want 0 on a fresh scratch database", baseline)
	}

	verifiedA := &Domain{Hostname: "countverified-a.example.com"}
	if err := repo.Create(ctx, verifiedA); err != nil {
		t.Fatalf("Create(verifiedA) returned unexpected error: %v", err)
	}
	verifiedB := &Domain{Hostname: "countverified-b.example.com"}
	if err := repo.Create(ctx, verifiedB); err != nil {
		t.Fatalf("Create(verifiedB) returned unexpected error: %v", err)
	}
	pendingC := &Domain{Hostname: "countverified-c.example.com"}
	if err := repo.Create(ctx, pendingC); err != nil {
		t.Fatalf("Create(pendingC) returned unexpected error: %v", err)
	}
	if _, err := repo.SetVerificationResult(ctx, verifiedA.ID, "verified", nil, time.Now()); err != nil {
		t.Fatalf("SetVerificationResult(verifiedA) returned unexpected error: %v", err)
	}
	if _, err := repo.SetVerificationResult(ctx, verifiedB.ID, "verified", nil, time.Now()); err != nil {
		t.Fatalf("SetVerificationResult(verifiedB) returned unexpected error: %v", err)
	}

	got, err := repo.CountVerified(ctx)
	if err != nil {
		t.Fatalf("CountVerified() returned unexpected error: %v", err)
	}
	if got != 2 {
		t.Errorf("CountVerified() = %d, want 2 (one still pending)", got)
	}
}

func TestDomainRepository_CountVerified_NoDomains_ReturnsZero(t *testing.T) {
	repo, _ := newDomainRepoScratchPool(t)

	got, err := repo.CountVerified(context.Background())
	if err != nil {
		t.Fatalf("CountVerified() returned unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("CountVerified() = %d, want 0 for a tenant with no domains", got)
	}
}

// TestDomainRepository_CountAll_MixedStatuses_CountsRegardlessOfStatus covers
// OVW-20 (Overview's "verified/total domains" denominator): unlike
// CountVerified, CountAll must not filter by verification status.
func TestDomainRepository_CountAll_MixedStatuses_CountsRegardlessOfStatus(t *testing.T) {
	repo, _ := newDomainRepoScratchPool(t)
	ctx := context.Background()

	baseline, err := repo.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll() (baseline) returned unexpected error: %v", err)
	}
	if baseline != 0 {
		t.Fatalf("baseline = %d, want 0 on a fresh scratch database", baseline)
	}

	verifiedA := &Domain{Hostname: "countall-a.example.com"}
	if err := repo.Create(ctx, verifiedA); err != nil {
		t.Fatalf("Create(verifiedA) returned unexpected error: %v", err)
	}
	pendingB := &Domain{Hostname: "countall-b.example.com"}
	if err := repo.Create(ctx, pendingB); err != nil {
		t.Fatalf("Create(pendingB) returned unexpected error: %v", err)
	}
	if _, err := repo.SetVerificationResult(ctx, verifiedA.ID, "verified", nil, time.Now()); err != nil {
		t.Fatalf("SetVerificationResult(verifiedA) returned unexpected error: %v", err)
	}

	got, err := repo.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll() returned unexpected error: %v", err)
	}
	if got != 2 {
		t.Errorf("CountAll() = %d, want 2 (one verified, one still pending - both counted)", got)
	}
}

func TestDomainRepository_CountAll_NoDomains_ReturnsZero(t *testing.T) {
	repo, _ := newDomainRepoScratchPool(t)

	got, err := repo.CountAll(context.Background())
	if err != nil {
		t.Fatalf("CountAll() returned unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("CountAll() = %d, want 0 for a tenant with no domains", got)
	}
}

// TestDomainRepository_SetHealthCheckResult_AllFields_WritesSevenColumns
// covers DHM-01/DHM-05: one health-check cycle's result is persisted in
// full, and the NS baseline is learned from the first successful check.
func TestDomainRepository_SetHealthCheckResult_AllFields_WritesSevenColumns(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("health-all-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	ctx := context.Background()
	expires := time.Now().UTC().Add(20 * 24 * time.Hour).Truncate(time.Millisecond)
	registrar := "GoDaddy"
	ns := []string{"ns1.example.net", "ns2.example.net"}
	if err := repo.SetHealthCheckResult(ctx, domain.ID, &expires, &registrar, ns, false, nil); err != nil {
		t.Fatalf("SetHealthCheckResult() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
	if got.Registrar == nil || *got.Registrar != registrar {
		t.Errorf("Registrar = %v, want %q", got.Registrar, registrar)
	}
	if !reflect.DeepEqual(got.ExpectedNS, ns) {
		t.Errorf("ExpectedNS = %v, want %v (baseline learned on first check)", got.ExpectedNS, ns)
	}
	if !reflect.DeepEqual(got.CurrentNS, ns) {
		t.Errorf("CurrentNS = %v, want %v", got.CurrentNS, ns)
	}
	if got.NSDriftDetected {
		t.Error("NSDriftDetected = true, want false (no drift on the first check)")
	}
	if got.LastRDAPCheckAt == nil {
		t.Error("LastRDAPCheckAt = nil, want the check's timestamp")
	}
	if got.RDAPLastError != nil {
		t.Errorf("RDAPLastError = %v, want nil", got.RDAPLastError)
	}
}

// TestDomainRepository_SetHealthCheckResult_NSFailure_PreservesBaseline
// covers DHM-07: an NS lookup failure (nil currentNS) never overwrites or
// clears the learned ExpectedNS/CurrentNS.
func TestDomainRepository_SetHealthCheckResult_NSFailure_PreservesBaseline(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("health-nsfail-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	ctx := context.Background()
	baseline := []string{"ns1.example.net", "ns2.example.net"}
	if err := repo.SetHealthCheckResult(ctx, domain.ID, nil, nil, baseline, false, nil); err != nil {
		t.Fatalf("first SetHealthCheckResult() returned unexpected error: %v", err)
	}
	// Second cycle: DNS lookup failed entirely (nil NS set).
	if err := repo.SetHealthCheckResult(ctx, domain.ID, nil, nil, nil, false, nil); err != nil {
		t.Fatalf("second SetHealthCheckResult() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got.ExpectedNS, baseline) {
		t.Errorf("ExpectedNS = %v, want %v (baseline must survive a DNS lookup failure)", got.ExpectedNS, baseline)
	}
	if !reflect.DeepEqual(got.CurrentNS, baseline) {
		t.Errorf("CurrentNS = %v, want %v (must not be cleared by a DNS lookup failure)", got.CurrentNS, baseline)
	}
}

// TestDomainRepository_SetHealthCheckResult_NSFailure_PreservesDriftFlag
// covers a bug where a DNS lookup failure (nil currentNS, so the scheduler
// always passes driftDetected=false) silently cleared a previously detected
// ns_drift_detected back to false - making a real, still-unresolved drift
// disappear from the UI, and re-triggering the drift notification on the
// next cycle that could resolve the NS lookup again. ns_drift_detected must
// be preserved exactly like current_ns/expected_ns are.
func TestDomainRepository_SetHealthCheckResult_NSFailure_PreservesDriftFlag(t *testing.T) {
	repo, pool := newDomainRepoTestPool(t)
	domain := &Domain{Hostname: fmt.Sprintf("health-driftpreserve-%d.example.com", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), domain); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	ctx := context.Background()
	// First cycle learns the baseline.
	if err := repo.SetHealthCheckResult(ctx, domain.ID, nil, nil, []string{"ns1.example.net"}, false, nil); err != nil {
		t.Fatalf("baseline SetHealthCheckResult() returned unexpected error: %v", err)
	}
	// Second cycle: NS actually changed, drift detected.
	if err := repo.SetHealthCheckResult(ctx, domain.ID, nil, nil, []string{"ns2.example.net"}, true, nil); err != nil {
		t.Fatalf("drift SetHealthCheckResult() returned unexpected error: %v", err)
	}
	// Third cycle: transient DNS failure (nil currentNS); the scheduler
	// always passes driftDetected=false in this path since it has no fresh
	// NS set to compare.
	if err := repo.SetHealthCheckResult(ctx, domain.ID, nil, nil, nil, false, nil); err != nil {
		t.Fatalf("DNS-failure SetHealthCheckResult() returned unexpected error: %v", err)
	}

	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID() returned unexpected error: %v", err)
	}
	if !got.NSDriftDetected {
		t.Error("NSDriftDetected = false, want true - a DNS lookup failure must not clear a previously detected drift")
	}
}

// TestDomainRepository_SetHealthCheckResult_Unknown_ErrNotFound covers the
// not-found path (T3 done-when: a clear not-found-shaped error, not a silent
// no-op).
func TestDomainRepository_SetHealthCheckResult_Unknown_ErrNotFound(t *testing.T) {
	repo, _ := newDomainRepoTestPool(t)

	err := repo.SetHealthCheckResult(context.Background(), "00000000-0000-0000-0000-000000000000", nil, nil, nil, false, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetHealthCheckResult() error = %v, want ErrNotFound", err)
	}
}

// domainRLSTestPool boots the non-superuser RLS pool and grants its role DML
// on domains - the fixed GRANT list in newRLSTestPool predates this suite's
// use of that table. status_pages needs SELECT because the domains
// public_published_read policy (0025) subqueries it, and INSERT ... RETURNING
// re-reads the new row through every SELECT policy.
func domainRLSTestPool(t *testing.T) *Pool {
	t.Helper()
	pool := newRLSTestPool(t)
	if _, err := pool.Exec(context.Background(),
		"GRANT SELECT, INSERT, UPDATE, DELETE ON domains TO "+rlsTestRole); err != nil {
		t.Fatalf("granting DML on domains to %s returned unexpected error: %v", rlsTestRole, err)
	}
	if _, err := pool.Exec(context.Background(),
		"GRANT SELECT ON status_pages TO "+rlsTestRole); err != nil {
		t.Fatalf("granting SELECT on status_pages to %s returned unexpected error: %v", rlsTestRole, err)
	}
	return pool
}

// seedTenantWithDomain creates and commits one tenant plus one domain it
// owns, mirroring rls_test.go's seedTenantWithService.
func seedTenantWithDomain(t *testing.T, pool *Pool, prefix string) (tenantID, domainID string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		t.Fatalf("generating tenant id returned unexpected error: %v", err)
	}

	tx, txCtx := beginRLSTx(t, pool, "", tenantID)
	if _, err := pool.Exec(txCtx,
		"INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, prefix+"-tenant",
	); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seed tenant insert returned unexpected error: %v", err)
	}
	if err := pool.QueryRow(txCtx,
		"INSERT INTO domains (hostname, verification_token) VALUES ($1, $2) RETURNING id",
		prefix+"-domain.example.com", "rls-fixture-verification-token",
	).Scan(&domainID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seed domain insert returned unexpected error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	t.Cleanup(func() {
		cleanupTx, cleanupCtx := beginRLSTx(t, pool, "", tenantID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM domains WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM tenants WHERE id = $1", tenantID)
		_ = cleanupTx.Commit(context.Background())
	})

	return tenantID, domainID
}

// TestDomainRepository_SetHealthCheckResult_CrossTenant_RejectedByRLS is the
// load-bearing isolation proof for T3: a write attempted with a transaction
// scoped to a different tenant than the domain's owner must not reach the
// row. RLS's USING clause filters it out, so the UPDATE affects zero rows and
// surfaces as ErrNotFound - and the owner's row must be verifiably unchanged
// afterward, not merely "the write didn't error".
func TestDomainRepository_SetHealthCheckResult_CrossTenant_RejectedByRLS(t *testing.T) {
	pool := domainRLSTestPool(t)
	tenantA, domainA := seedTenantWithDomain(t, pool, fmt.Sprintf("rls-dhm-a-%d", tUniqueSuffix()))
	tenantB, _ := seedTenantWithDomain(t, pool, fmt.Sprintf("rls-dhm-b-%d", tUniqueSuffix()))
	repo := NewDomainRepository(pool)

	registrar := "evil-registrar"
	expires := time.Now().UTC().Add(24 * time.Hour)

	txB, ctxB := beginRLSTx(t, pool, "", tenantB)
	err := repo.SetHealthCheckResult(ctxB, domainA, &expires, &registrar, []string{"ns.evil.example"}, true, nil)
	_ = txB.Rollback(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetHealthCheckResult() from a foreign tenant error = %v, want ErrNotFound (RLS must filter the row out)", err)
	}

	txA, ctxA := beginRLSTx(t, pool, "", tenantA)
	defer func() { _ = txA.Rollback(context.Background()) }()

	got, err := repo.GetByID(ctxA, domainA)
	if err != nil {
		t.Fatalf("GetByID() for the owner returned unexpected error: %v", err)
	}
	if got.ExpiresAt != nil || got.Registrar != nil || got.ExpectedNS != nil || got.CurrentNS != nil ||
		got.NSDriftDetected || got.LastRDAPCheckAt != nil || got.RDAPLastError != nil {
		t.Errorf("owner's domain health fields changed after a cross-tenant write attempt: %+v", got)
	}
}
