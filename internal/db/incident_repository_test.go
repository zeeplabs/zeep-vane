//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// newIncidentRepoTestPool boots a migrated pool and a fresh
// *IncidentRepository backed by it.
func newIncidentRepoTestPool(t *testing.T) (*IncidentRepository, *Pool) {
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
	return NewIncidentRepository(pool), pool
}

// createIncidentFixture inserts an incident with the given title (no linked
// services needed for pagination tests) and registers its cleanup.
func createIncidentFixture(t *testing.T, repo *IncidentRepository, pool *Pool, title string) *Incident {
	t.Helper()
	incident := &Incident{Title: title}
	if err := repo.Create(context.Background(), incident, nil); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID)
	})
	return incident
}

// seedIncidents creates n incidents named prefix-0..prefix-(n-1), sleeping
// briefly between inserts so created_at ordering (ORDER BY created_at DESC)
// is deterministic even at low timestamp resolution.
func seedIncidents(t *testing.T, repo *IncidentRepository, pool *Pool, prefix string, n int) []*Incident {
	t.Helper()
	incidents := make([]*Incident, n)
	for i := 0; i < n; i++ {
		incidents[i] = createIncidentFixture(t, repo, pool, fmt.Sprintf("%s-%d", prefix, i))
		time.Sleep(time.Millisecond)
	}
	return incidents
}

func TestIncidentRepository_ListPaginated_Page1_ReturnsExactlyPageSizeAndCorrectTotal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p1-%d", time.Now().UnixNano())
	seedIncidents(t, repo, pool, prefix, 27)

	items, total, err := repo.ListPaginated(context.Background(), 1, 25)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	if len(items) != 25 {
		t.Errorf("len(items) = %d, want 25 (page_size)", len(items))
	}
	if total < 27 {
		t.Errorf("total = %d, want >= 27", total)
	}
}

func TestIncidentRepository_ListPaginated_Page2_ReturnsRemainder(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-p2-%d", time.Now().UnixNano())
	seedIncidents(t, repo, pool, prefix, 27)

	page1, total1, err := repo.ListPaginated(context.Background(), 1, 25)
	if err != nil {
		t.Fatalf("ListPaginated(page=1) returned unexpected error: %v", err)
	}
	page2, total2, err := repo.ListPaginated(context.Background(), 2, 25)
	if err != nil {
		t.Fatalf("ListPaginated(page=2) returned unexpected error: %v", err)
	}

	if total1 != total2 {
		t.Errorf("total differs between page 1 (%d) and page 2 (%d), want equal", total1, total2)
	}
	if total1 < 27 {
		t.Errorf("total = %d, want >= 27 (seeded count)", total1)
	}
	// The table may hold rows beyond this test's own 27 fixtures (other
	// tests' fixtures may not have finished cleanup yet), so page 2's
	// length is the exact remainder only when total - 25 is smaller than
	// one page; otherwise it's a full page.
	wantPage2Len := total1 - 25
	if wantPage2Len > 25 {
		wantPage2Len = 25
	}
	if len(page2) != wantPage2Len {
		t.Errorf("len(page2 items) = %d, want %d (min(page_size, total-page_size))", len(page2), wantPage2Len)
	}

	// The two pages must not overlap.
	seen := map[string]bool{}
	for _, inc := range page1 {
		seen[inc.ID] = true
	}
	for _, inc := range page2 {
		if seen[inc.ID] {
			t.Errorf("incident %s appeared on both page 1 and page 2", inc.ID)
		}
	}
}

func TestIncidentRepository_ListPaginated_PageBeyondLast_EmptyItemsCorrectTotal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-beyond-%d", time.Now().UnixNano())
	seedIncidents(t, repo, pool, prefix, 3)

	_, totalFirst, err := repo.ListPaginated(context.Background(), 1, 25)
	if err != nil {
		t.Fatalf("ListPaginated(page=1) returned unexpected error: %v", err)
	}

	items, total, err := repo.ListPaginated(context.Background(), 999, 25)
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

func TestIncidentRepository_ListPaginated_OrderByCreatedAtDescUnchanged(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	prefix := fmt.Sprintf("list-paginated-order-%d", time.Now().UnixNano())
	seeded := seedIncidents(t, repo, pool, prefix, 5)

	items, _, err := repo.ListPaginated(context.Background(), 1, 25)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	// Filter to just this test's own fixtures, preserving the returned order.
	var ours []Incident
	for _, inc := range items {
		for _, s := range seeded {
			if inc.ID == s.ID {
				ours = append(ours, inc)
			}
		}
	}
	if len(ours) != len(seeded) {
		t.Fatalf("found %d of this test's incidents in ListPaginated(), want %d", len(ours), len(seeded))
	}
	// seeded[len-1] was created last, so it must come first (DESC).
	for i, inc := range ours {
		wantID := seeded[len(seeded)-1-i].ID
		if inc.ID != wantID {
			t.Errorf("ours[%d].ID = %q, want %q (ORDER BY created_at DESC)", i, inc.ID, wantID)
		}
	}
}

// TestIncidentRepository_ListPaginated_NoRowsForThisIncident_ItemsEmptyNotNil
// covers the "zero-row table" edge from the Test Coverage Matrix at a scope
// this suite can isolate deterministically: ListUpdatesPaginated for a
// freshly created incident with zero updates yet. A literal empty
// `incidents` table can't be asserted safely in this shared-DB suite
// (other packages' integration tests run against the same database), but
// this exercises the identical zero-row-returned -> fallback COUNT(*) code
// path (countIncidentUpdates), which is what the edge case is actually
// about (see design.md's COUNT(*) OVER() zero-row fallback decision).
func TestIncidentRepository_ListUpdatesPaginated_NoUpdatesYet_EmptyItemsZeroTotal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, fmt.Sprintf("updates-empty-%d", time.Now().UnixNano()))

	items, total, err := repo.ListUpdatesPaginated(context.Background(), incident.ID, 1, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated() returned unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0 for an incident with no updates yet", len(items))
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
}

func TestIncidentRepository_ListPaginated_PopulatesServiceIDs(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	services := NewServiceRepository(pool)
	service := &Service{Name: fmt.Sprintf("incident-list-svc-%d", time.Now().UnixNano()), SLOID: "slo-fixture-id"}
	if err := services.Create(context.Background(), service); err != nil {
		t.Fatalf("setup service Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	incident := &Incident{Title: fmt.Sprintf("list-service-ids-%d", time.Now().UnixNano())}
	if err := repo.Create(context.Background(), incident, []string{service.ID}); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	items, _, err := repo.ListPaginated(context.Background(), 1, 25)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}

	var found *Incident
	for i := range items {
		if items[i].ID == incident.ID {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatal("ListPaginated() did not include the seeded incident")
	}
	if len(found.ServiceIDs) != 1 || found.ServiceIDs[0] != service.ID {
		t.Errorf("ServiceIDs = %v, want [%q]", found.ServiceIDs, service.ID)
	}
}

func TestIncidentRepository_ListUpdatesPaginated_Page1And2_CorrectSlicing(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, fmt.Sprintf("updates-paginated-%d", time.Now().UnixNano()))

	const seedCount = 27
	for i := 0; i < seedCount; i++ {
		if _, err := repo.AddUpdate(context.Background(), incident.ID, fmt.Sprintf("update-%d", i)); err != nil {
			t.Fatalf("setup AddUpdate() returned unexpected error: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	page1, total1, err := repo.ListUpdatesPaginated(context.Background(), incident.ID, 1, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated(page=1) returned unexpected error: %v", err)
	}
	if len(page1) != 25 {
		t.Errorf("len(page1) = %d, want 25 (page_size)", len(page1))
	}
	if total1 != seedCount {
		t.Errorf("total = %d, want %d", total1, seedCount)
	}

	page2, total2, err := repo.ListUpdatesPaginated(context.Background(), incident.ID, 2, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated(page=2) returned unexpected error: %v", err)
	}
	if len(page2) != seedCount-25 {
		t.Errorf("len(page2) = %d, want %d", len(page2), seedCount-25)
	}
	if total2 != seedCount {
		t.Errorf("total (page 2) = %d, want %d", total2, seedCount)
	}
}

func TestIncidentRepository_ListUpdatesPaginated_PageBeyondLast_EmptyItemsCorrectTotal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, fmt.Sprintf("updates-paginated-beyond-%d", time.Now().UnixNano()))

	if _, err := repo.AddUpdate(context.Background(), incident.ID, "only update"); err != nil {
		t.Fatalf("setup AddUpdate() returned unexpected error: %v", err)
	}

	items, total, err := repo.ListUpdatesPaginated(context.Background(), incident.ID, 999, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated() returned unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0 for a page beyond the last", len(items))
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestIncidentRepository_ListUpdatesPaginated_ScopedToOneIncident(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incidentA := createIncidentFixture(t, repo, pool, fmt.Sprintf("updates-scoped-a-%d", time.Now().UnixNano()))
	incidentB := createIncidentFixture(t, repo, pool, fmt.Sprintf("updates-scoped-b-%d", time.Now().UnixNano()))

	if _, err := repo.AddUpdate(context.Background(), incidentA.ID, "a-update-1"); err != nil {
		t.Fatalf("setup AddUpdate() (A) returned unexpected error: %v", err)
	}
	if _, err := repo.AddUpdate(context.Background(), incidentB.ID, "b-update-1"); err != nil {
		t.Fatalf("setup AddUpdate() (B) returned unexpected error: %v", err)
	}
	if _, err := repo.AddUpdate(context.Background(), incidentB.ID, "b-update-2"); err != nil {
		t.Fatalf("setup AddUpdate() (B) returned unexpected error: %v", err)
	}

	itemsA, totalA, err := repo.ListUpdatesPaginated(context.Background(), incidentA.ID, 1, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated(A) returned unexpected error: %v", err)
	}
	if totalA != 1 || len(itemsA) != 1 {
		t.Fatalf("incident A: len=%d total=%d, want len=1 total=1", len(itemsA), totalA)
	}
	if itemsA[0].IncidentID != incidentA.ID {
		t.Errorf("itemsA[0].IncidentID = %q, want %q", itemsA[0].IncidentID, incidentA.ID)
	}

	itemsB, totalB, err := repo.ListUpdatesPaginated(context.Background(), incidentB.ID, 1, 25)
	if err != nil {
		t.Fatalf("ListUpdatesPaginated(B) returned unexpected error: %v", err)
	}
	if totalB != 2 || len(itemsB) != 2 {
		t.Fatalf("incident B: len=%d total=%d, want len=2 total=2", len(itemsB), totalB)
	}
}

func TestIncidentRepository_ListUpdatesPaginated_UnknownIncident_ErrNotFound(t *testing.T) {
	repo, _ := newIncidentRepoTestPool(t)

	_, _, err := repo.ListUpdatesPaginated(context.Background(), "00000000-0000-0000-0000-000000000000", 1, 25)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListUpdatesPaginated() error = %v, want ErrNotFound", err)
	}
}
