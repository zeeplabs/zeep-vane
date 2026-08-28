//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// newServiceRepoTestPool boots a migrated pool and a fresh
// *ServiceRepository backed by it.
func newServiceRepoTestPool(t *testing.T) (*ServiceRepository, *Pool) {
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
	return NewServiceRepository(pool), pool
}

// seedServiceFixtures creates n services named prefix-0..prefix-(n-1) and
// registers their cleanup.
func seedServiceFixtures(t *testing.T, repo *ServiceRepository, pool *Pool, prefix string, n int) []*Service {
	t.Helper()
	services := make([]*Service, n)
	for i := 0; i < n; i++ {
		s := &Service{Name: fmt.Sprintf("%s-%d", prefix, i), SLOID: fmt.Sprintf("slo-%s-%d", prefix, i)}
		if err := repo.Create(context.Background(), s); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", s.ID)
		})
		services[i] = s
	}
	return services
}

func TestServiceRepository_ListPaginated_Page1_ReturnsExactlyPageSizeAndCorrectTotal(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p1-%d", time.Now().UnixNano())
	seedServiceFixtures(t, repo, pool, prefix, 22)

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

func TestServiceRepository_ListPaginated_Page2_ReturnsRemainder(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p2-%d", time.Now().UnixNano())
	seedServiceFixtures(t, repo, pool, prefix, 22)

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
	for _, s := range page1 {
		seen[s.ID] = true
	}
	for _, s := range page2 {
		if seen[s.ID] {
			t.Errorf("service %s appeared on both page 1 and page 2", s.ID)
		}
	}
}

func TestServiceRepository_ListPaginated_PageBeyondLast_EmptyItemsCorrectTotal(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-beyond-%d", time.Now().UnixNano())
	seedServiceFixtures(t, repo, pool, prefix, 3)

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

func TestServiceRepository_ListPaginated_OrderByNameUnchanged(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("aaa-order-%d", time.Now().UnixNano())
	seeded := seedServiceFixtures(t, repo, pool, prefix, 5)

	items, _, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	var ours []Service
	for _, s := range items {
		for _, seed := range seeded {
			if s.ID == seed.ID {
				ours = append(ours, s)
			}
		}
	}
	if len(ours) != len(seeded) {
		t.Fatalf("found %d of this test's services in ListPaginated(), want %d", len(ours), len(seeded))
	}
	for i, s := range ours {
		if s.ID != seeded[i].ID {
			t.Errorf("ours[%d].ID = %q, want %q (ORDER BY name)", i, s.ID, seeded[i].ID)
		}
	}
}

// TestServiceRepository_List_StillWorksForPoller confirms ServiceRepository.
// List(ctx) - the poller's own unpaginated caller - is untouched: it must
// keep returning every service, never just one page, so internal/poller
// still sees every service every cycle (design.md Risks & Concerns).
func TestServiceRepository_List_StillWorksForPoller(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("poller-unpaginated-%d", time.Now().UnixNano())
	seeded := seedServiceFixtures(t, repo, pool, prefix, 25)

	all, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}

	found := 0
	for _, s := range all {
		for _, seed := range seeded {
			if s.ID == seed.ID {
				found++
			}
		}
	}
	if found != len(seeded) {
		t.Errorf("List() returned %d of this test's 25 seeded services, want all 25 (unpaginated)", found)
	}
}
