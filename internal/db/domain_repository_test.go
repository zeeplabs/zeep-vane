//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// newDomainRepoTestPool boots a migrated pool and a fresh *DomainRepository
// backed by it.
func newDomainRepoTestPool(t *testing.T) (*DomainRepository, *Pool) {
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
