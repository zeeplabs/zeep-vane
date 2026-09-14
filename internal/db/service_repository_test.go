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
	tenantID := seedPlainTenant(t, pool)
	services := make([]*Service, n)
	for i := 0; i < n; i++ {
		s := &Service{Name: fmt.Sprintf("%s-%d", prefix, i), SLOID: fmt.Sprintf("slo-%s-%d", prefix, i)}
		withTenantTx(t, pool, tenantID, func(ctx context.Context) {
			if err := repo.Create(ctx, s); err != nil {
				t.Fatalf("setup Create() returned unexpected error: %v", err)
			}
		})
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

// TestServiceRepository_Create_PersistsAndReturnsSLOName asserts SVC-01:
// Create persists SLOName and it round-trips on the returned *Service.
func TestServiceRepository_Create_PersistsAndReturnsSLOName(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-slo-name-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-create-1", SLOName: "Checkout latency SLO"}

	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if service.SLOName != "Checkout latency SLO" {
		t.Errorf("service.SLOName = %q, want %q", service.SLOName, "Checkout latency SLO")
	}

	var storedSLOName string
	row := pool.QueryRow(context.Background(), "SELECT slo_name FROM services WHERE id = $1", service.ID)
	if err := row.Scan(&storedSLOName); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if storedSLOName != "Checkout latency SLO" {
		t.Errorf("stored slo_name = %q, want %q", storedSLOName, "Checkout latency SLO")
	}
}

// TestServiceRepository_ListPaginated_ReturnsSLONameForMixOfPreAndPostMigrationRows
// asserts SVC-01: ListPaginated returns SLOName for every row, including a
// row created with an empty SLOName (simulating a pre-migration row never
// re-saved) alongside one created with a real SLOName - neither errors, and
// the empty one comes back as "" rather than some other zero value.
func TestServiceRepository_ListPaginated_ReturnsSLONameForMixOfPreAndPostMigrationRows(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	prefix := fmt.Sprintf("mixed-slo-name-%d", time.Now().UnixNano())

	withSLOName := &Service{Name: prefix + "-with-name", SLOID: "slo-mixed-1", SLOName: "Checkout latency SLO"}
	withoutSLOName := &Service{Name: prefix + "-without-name", SLOID: "slo-mixed-2", SLOName: ""}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, withSLOName); err != nil {
			t.Fatalf("Create(withSLOName) returned unexpected error: %v", err)
		}
		if err := repo.Create(ctx, withoutSLOName); err != nil {
			t.Fatalf("Create(withoutSLOName) returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id IN ($1, $2)", withSLOName.ID, withoutSLOName.ID)
	})

	items, _, err := repo.ListPaginated(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	var gotWithName, gotWithoutName *Service
	for i := range items {
		switch items[i].ID {
		case withSLOName.ID:
			gotWithName = &items[i]
		case withoutSLOName.ID:
			gotWithoutName = &items[i]
		}
	}

	if gotWithName == nil {
		t.Fatalf("service %s not found in ListPaginated()", withSLOName.ID)
	}
	if gotWithName.SLOName != "Checkout latency SLO" {
		t.Errorf("gotWithName.SLOName = %q, want %q", gotWithName.SLOName, "Checkout latency SLO")
	}

	if gotWithoutName == nil {
		t.Fatalf("service %s not found in ListPaginated()", withoutSLOName.ID)
	}
	if gotWithoutName.SLOName != "" {
		t.Errorf("gotWithoutName.SLOName = %q, want %q (empty, not an error)", gotWithoutName.SLOName, "")
	}
}

// TestServiceRepository_Get_Found_ReturnsFullRow asserts SVC-01/SVC-14:
// Get returns the full row - including SLOName, CurrentStatus, and
// StatusAnalysis - for an existing service.
func TestServiceRepository_Get_Found_ReturnsFullRow(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("get-found-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-get-1", SLOName: "Checkout latency SLO"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	analysis := "SLI dropped below target"
	if _, err := pool.Exec(context.Background(), "UPDATE services SET current_status = 'degraded', status_analysis = $1 WHERE id = $2", analysis, service.ID); err != nil {
		t.Fatalf("setup UPDATE returned unexpected error: %v", err)
	}

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.SLOName != "Checkout latency SLO" {
		t.Errorf("got.SLOName = %q, want %q", got.SLOName, "Checkout latency SLO")
	}
	if got.CurrentStatus != "degraded" {
		t.Errorf("got.CurrentStatus = %q, want %q", got.CurrentStatus, "degraded")
	}
	if got.StatusAnalysis == nil || *got.StatusAnalysis != analysis {
		t.Errorf("got.StatusAnalysis = %v, want %q", got.StatusAnalysis, analysis)
	}
}

// TestServiceRepository_Get_NotFound_ReturnsFalseNoError asserts SVC-14:
// an unknown ID returns found=false with a nil error, not ErrNotFound or a
// scan error.
func TestServiceRepository_Get_NotFound_ReturnsFalseNoError(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	got, found, err := repo.Get(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v, want nil", err)
	}
	if found {
		t.Errorf("found = true, want false for an unknown ID")
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
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
