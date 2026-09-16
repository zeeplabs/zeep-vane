//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
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
// ssl_status=pending, verified_at=NULL.
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
	if domain.SSLStatus != "pending" {
		t.Errorf("SSLStatus = %q, want %q", domain.SSLStatus, "pending")
	}
	if domain.VerifiedAt != nil {
		t.Errorf("VerifiedAt = %v, want nil", domain.VerifiedAt)
	}
	if domain.LastError != nil {
		t.Errorf("LastError = %v, want nil", domain.LastError)
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
	updated, err := repo.SetVerificationResult(context.Background(), domain.ID, "verified", "active", nil, checkedAt)
	if err != nil {
		t.Fatalf("SetVerificationResult() returned unexpected error: %v", err)
	}
	if updated.Status != "verified" {
		t.Errorf("Status = %q, want %q", updated.Status, "verified")
	}
	if updated.SSLStatus != "active" {
		t.Errorf("SSLStatus = %q, want %q", updated.SSLStatus, "active")
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

	errMsg := "DNS not resolved"
	updated, err := repo.SetVerificationResult(context.Background(), domain.ID, "error", "error", &errMsg, time.Now())
	if err != nil {
		t.Fatalf("SetVerificationResult() returned unexpected error: %v", err)
	}
	if updated.Status != "error" {
		t.Errorf("Status = %q, want %q", updated.Status, "error")
	}
	if updated.SSLStatus != "error" {
		t.Errorf("SSLStatus = %q, want %q", updated.SSLStatus, "error")
	}
	if updated.LastError == nil || *updated.LastError != errMsg {
		t.Errorf("LastError = %v, want %q", updated.LastError, errMsg)
	}
}

// TestDomainRepository_SetVerificationResult_Unknown_ErrNotFound covers the
// not-found path.
func TestDomainRepository_SetVerificationResult_Unknown_ErrNotFound(t *testing.T) {
	repo, _ := newDomainRepoTestPool(t)

	_, err := repo.SetVerificationResult(context.Background(), "00000000-0000-0000-0000-000000000000", "verified", "active", nil, time.Now())
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
	if _, err := repo.SetVerificationResult(ctx, verifiedA.ID, "verified", "active", nil, time.Now()); err != nil {
		t.Fatalf("SetVerificationResult(verifiedA) returned unexpected error: %v", err)
	}
	if _, err := repo.SetVerificationResult(ctx, verifiedB.ID, "verified", "active", nil, time.Now()); err != nil {
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
	if _, err := repo.SetVerificationResult(ctx, verifiedA.ID, "verified", "active", nil, time.Now()); err != nil {
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
