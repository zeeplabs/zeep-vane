//go:build integration

package db

import (
	"context"
	"errors"
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

// strPtr returns a pointer to s, for building polling-mode Service fixtures
// whose PollType/PollTarget fields are *string.
func strPtr(s string) *string { return &s }

// intPtr returns a pointer to i, for building polling-mode Service fixtures
// whose PollIntervalSeconds field is *int.
func intPtr(i int) *int { return &i }

// TestServiceRepository_Create_PollingMode_PersistsAllFourFieldsAndNilSLOID
// asserts MP-01/MP-02/MP-06: Create persists a polling-mode service's
// MonitorMode/PollType/PollTarget/PollIntervalSeconds, and slo_id stays NULL
// in storage (read back as "" via Get's COALESCE).
func TestServiceRepository_Create_PollingMode_PersistsAllFourFieldsAndNilSLOID(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-polling-%d", time.Now().UnixNano())
	service := &Service{
		Name:                name,
		MonitorMode:         "polling",
		PollType:            strPtr("http"),
		PollTarget:          strPtr("https://example.test/health"),
		PollIntervalSeconds: intPtr(30),
	}

	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if service.MonitorMode != "polling" {
		t.Errorf("service.MonitorMode = %q, want %q", service.MonitorMode, "polling")
	}

	var monitorMode string
	var sloID *string
	var pollType, pollTarget string
	var pollIntervalSeconds int
	row := pool.QueryRow(context.Background(),
		"SELECT monitor_mode, slo_id, poll_type, poll_target, poll_interval_seconds FROM services WHERE id = $1",
		service.ID)
	if err := row.Scan(&monitorMode, &sloID, &pollType, &pollTarget, &pollIntervalSeconds); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}

	if monitorMode != "polling" {
		t.Errorf("stored monitor_mode = %q, want %q", monitorMode, "polling")
	}
	if sloID != nil {
		t.Errorf("stored slo_id = %v, want nil", sloID)
	}
	if pollType != "http" {
		t.Errorf("stored poll_type = %q, want %q", pollType, "http")
	}
	if pollTarget != "https://example.test/health" {
		t.Errorf("stored poll_target = %q, want %q", pollTarget, "https://example.test/health")
	}
	if pollIntervalSeconds != 30 {
		t.Errorf("stored poll_interval_seconds = %d, want %d", pollIntervalSeconds, 30)
	}
}

// TestServiceRepository_Create_SLOModeUnchanged asserts MP-01/MP-05: an
// slo-mode Create (MonitorMode left at its zero value, exactly as every
// caller before this feature) still persists slo_id/slo_name unchanged -
// existing behavior, not regressed by the new branching.
func TestServiceRepository_Create_SLOModeUnchanged(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-slo-unchanged-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-unchanged-1", SLOName: "Unchanged SLO"}

	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if service.MonitorMode != "slo" {
		t.Errorf("service.MonitorMode = %q, want %q", service.MonitorMode, "slo")
	}

	var monitorMode, sloID, sloName string
	var pollType *string
	row := pool.QueryRow(context.Background(),
		"SELECT monitor_mode, slo_id, slo_name, poll_type FROM services WHERE id = $1", service.ID)
	if err := row.Scan(&monitorMode, &sloID, &sloName, &pollType); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if monitorMode != "slo" {
		t.Errorf("stored monitor_mode = %q, want %q", monitorMode, "slo")
	}
	if sloID != "slo-unchanged-1" {
		t.Errorf("stored slo_id = %q, want %q", sloID, "slo-unchanged-1")
	}
	if sloName != "Unchanged SLO" {
		t.Errorf("stored slo_name = %q, want %q", sloName, "Unchanged SLO")
	}
	if pollType != nil {
		t.Errorf("stored poll_type = %v, want nil", pollType)
	}
}

// TestServiceRepository_Create_PollingModeMissingRequiredField_SurfacesConstraintViolation
// asserts T2's "Done when": a Create call that would violate the DB's
// mode/field-combination CHECK constraint (here: monitor_mode="polling" but
// PollTarget/PollIntervalSeconds left unset, a plausible caller bug) returns
// an error rather than silently partial-inserting the row.
func TestServiceRepository_Create_PollingModeMissingRequiredField_SurfacesConstraintViolation(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-polling-invalid-%d", time.Now().UnixNano())
	service := &Service{
		Name:        name,
		MonitorMode: "polling",
		PollType:    strPtr("http"),
		// PollTarget/PollIntervalSeconds deliberately left nil.
	}

	tx, err := pool.BeginTenantTx(context.Background(), "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	txCtx := WithTenantTx(context.Background(), tx)

	createErr := repo.Create(txCtx, service)
	if createErr == nil {
		t.Fatalf("Create() succeeded, want a CHECK constraint violation error")
	}
	_ = tx.Rollback(context.Background())

	var count int
	row := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM services WHERE name = $1", name)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("services rows named %q after failed Create() = %d, want 0 (no partial insert)", name, count)
	}
}

// TestServiceRepository_ListPollingManual_ReturnsOnlyPollingRows asserts
// MP-06: ListPollingManual returns only monitor_mode='polling' rows,
// excluding an slo-mode row created alongside it.
func TestServiceRepository_ListPollingManual_ReturnsOnlyPollingRows(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	prefix := fmt.Sprintf("list-polling-only-%d", time.Now().UnixNano())

	pollingSvc := &Service{
		Name:                prefix + "-polling",
		MonitorMode:         "polling",
		PollType:            strPtr("tcp"),
		PollTarget:          strPtr("db.example.test:5432"),
		PollIntervalSeconds: intPtr(60),
	}
	sloSvc := &Service{Name: prefix + "-slo", SLOID: "slo-list-polling-only"}

	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, pollingSvc); err != nil {
			t.Fatalf("Create(pollingSvc) returned unexpected error: %v", err)
		}
		if err := repo.Create(ctx, sloSvc); err != nil {
			t.Fatalf("Create(sloSvc) returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id IN ($1, $2)", pollingSvc.ID, sloSvc.ID)
	})

	items, err := repo.ListPollingManual(context.Background())
	if err != nil {
		t.Fatalf("ListPollingManual() returned unexpected error: %v", err)
	}

	var foundPolling, foundSLO bool
	for _, item := range items {
		if item.ID == pollingSvc.ID {
			foundPolling = true
			if item.MonitorMode != "polling" {
				t.Errorf("item.MonitorMode = %q, want %q", item.MonitorMode, "polling")
			}
			if item.PollType == nil || *item.PollType != "tcp" {
				t.Errorf("item.PollType = %v, want %q", item.PollType, "tcp")
			}
			if item.PollTarget == nil || *item.PollTarget != "db.example.test:5432" {
				t.Errorf("item.PollTarget = %v, want %q", item.PollTarget, "db.example.test:5432")
			}
			if item.PollIntervalSeconds == nil || *item.PollIntervalSeconds != 60 {
				t.Errorf("item.PollIntervalSeconds = %v, want %d", item.PollIntervalSeconds, 60)
			}
		}
		if item.ID == sloSvc.ID {
			foundSLO = true
		}
	}

	if !foundPolling {
		t.Errorf("ListPollingManual() did not return the polling-mode service %s", pollingSvc.ID)
	}
	if foundSLO {
		t.Errorf("ListPollingManual() returned the slo-mode service %s, want it excluded", sloSvc.ID)
	}
}

// TestServiceRepository_ListPollingManual_EmptyWhenNoneExist asserts T2's
// "Done when": ListPollingManual returns an empty (non-nil) slice, not a
// nil-panic, when no polling-manual service exists in the current result
// set.
func TestServiceRepository_ListPollingManual_EmptyWhenNoneExist(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("list-polling-empty-slo-only-%d", time.Now().UnixNano())
	sloSvc := &Service{Name: name, SLOID: "slo-list-polling-empty"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, sloSvc); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", sloSvc.ID) })

	items, err := repo.ListPollingManual(context.Background())
	if err != nil {
		t.Fatalf("ListPollingManual() returned unexpected error: %v", err)
	}
	if items == nil {
		t.Errorf("ListPollingManual() = nil, want a non-nil (possibly empty) slice")
	}
	for _, item := range items {
		if item.ID == sloSvc.ID {
			t.Errorf("ListPollingManual() returned the slo-mode service %s", sloSvc.ID)
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

// TestServiceRepository_Update_RenamesAndLeavesEverythingElseUnchanged
// covers service-edit spec.md SVCEDIT-01: only name changes, every other
// column (monitor_mode, slo_id, slo_name, current_status) stays byte-for-
// byte identical.
func TestServiceRepository_Update_RenamesAndLeavesEverythingElseUnchanged(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	oldName := fmt.Sprintf("update-old-%d", time.Now().UnixNano())
	service := &Service{Name: oldName, SLOID: "slo-update-1", SLOName: "Checkout latency SLO"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	newName := fmt.Sprintf("update-new-%d", time.Now().UnixNano())
	if err := repo.Update(context.Background(), service.ID, newName); err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.Name != newName {
		t.Errorf("got.Name = %q, want %q", got.Name, newName)
	}
	if got.SLOID != "slo-update-1" {
		t.Errorf("got.SLOID = %q, want unchanged %q", got.SLOID, "slo-update-1")
	}
	if got.SLOName != "Checkout latency SLO" {
		t.Errorf("got.SLOName = %q, want unchanged %q", got.SLOName, "Checkout latency SLO")
	}
	if got.CurrentStatus != "not_configured" {
		t.Errorf("got.CurrentStatus = %q, want unchanged %q", got.CurrentStatus, "not_configured")
	}
}

// TestServiceRepository_Update_UnknownID_ReturnsErrNotFound covers SVCEDIT-04:
// an unknown ID returns ErrNotFound, not a silent no-op success.
func TestServiceRepository_Update_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	err := repo.Update(context.Background(), "00000000-0000-0000-0000-000000000000", "anything")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

// TestServiceRepository_Update_SameName_IsIdempotentNoError covers the
// spec.md Edge Case: resubmitting the same name is a no-op success, not an
// error.
func TestServiceRepository_Update_SameName_IsIdempotentNoError(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("update-idempotent-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-update-2"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.Update(context.Background(), service.ID, name); err != nil {
		t.Errorf("Update() with unchanged name returned unexpected error: %v", err)
	}
}

// TestServiceRepository_SoftDelete_SetsDeletedAtAndHidesFromReads covers
// service-delete SVCDEL-01/02: deleted_at is set, and the service
// disappears from Get/List/ListPaginated/ListPollingManual afterward.
func TestServiceRepository_SoftDelete_SetsDeletedAtAndHidesFromReads(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("softdelete-%d", time.Now().UnixNano())
	pollType, pollTarget, interval := "http", "https://example.com", 60
	service := &Service{
		Name: name, MonitorMode: "polling",
		PollType: &pollType, PollTarget: &pollTarget, PollIntervalSeconds: &interval,
	}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.SoftDelete(context.Background(), service.ID); err != nil {
		t.Fatalf("SoftDelete() returned unexpected error: %v", err)
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT deleted_at FROM services WHERE id = $1", service.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("verify deleted_at query returned unexpected error: %v", err)
	}
	if deletedAt == nil {
		t.Errorf("deleted_at = nil, want a timestamp after SoftDelete")
	}

	if _, found, err := repo.Get(context.Background(), service.ID); err != nil || found {
		t.Errorf("Get() after SoftDelete = (found=%v, err=%v), want (found=false, err=nil)", found, err)
	}

	all, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	for _, s := range all {
		if s.ID == service.ID {
			t.Errorf("List() still returned soft-deleted service %s", service.ID)
		}
	}

	polling, err := repo.ListPollingManual(context.Background())
	if err != nil {
		t.Fatalf("ListPollingManual() returned unexpected error: %v", err)
	}
	for _, s := range polling {
		if s.ID == service.ID {
			t.Errorf("ListPollingManual() still returned soft-deleted service %s", service.ID)
		}
	}
}

// TestServiceRepository_SoftDelete_AttachedToStatusPage_ReturnsErrServiceInUse
// covers SVCDEL-03: a service still referenced by status_page_services is
// not deleted.
func TestServiceRepository_SoftDelete_AttachedToStatusPage_ReturnsErrServiceInUse(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("softdelete-inuse-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-inuse"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	var statusPageID string
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO status_pages (name, tenant_id) VALUES ($1, $2) RETURNING id",
		"softdelete-inuse-page", tenantID,
	).Scan(&statusPageID); err != nil {
		t.Fatalf("setup status_pages INSERT returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPageID) })
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)", statusPageID, service.ID,
	); err != nil {
		t.Fatalf("setup status_page_services INSERT returned unexpected error: %v", err)
	}

	err := repo.SoftDelete(context.Background(), service.ID)
	if !errors.Is(err, ErrServiceInUse) {
		t.Fatalf("SoftDelete() error = %v, want ErrServiceInUse", err)
	}

	got, found, getErr := repo.Get(context.Background(), service.ID)
	if getErr != nil || !found {
		t.Fatalf("Get() after blocked SoftDelete = (found=%v, err=%v), want (found=true, err=nil)", found, getErr)
	}
	if got.Name != name {
		t.Errorf("got.Name = %q, want unchanged %q", got.Name, name)
	}
}

// TestServiceRepository_SoftDelete_UnknownID_ReturnsErrNotFound covers
// SVCDEL-04.
func TestServiceRepository_SoftDelete_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	err := repo.SoftDelete(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SoftDelete() error = %v, want ErrNotFound", err)
	}
}

// TestServiceRepository_SoftDelete_AlreadyDeleted_ReturnsErrNotFound covers
// the spec.md Assumption: a second delete call is idempotent-as-404, not a
// distinct success/error path.
func TestServiceRepository_SoftDelete_AlreadyDeleted_ReturnsErrNotFound(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("softdelete-twice-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-twice"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.SoftDelete(context.Background(), service.ID); err != nil {
		t.Fatalf("first SoftDelete() returned unexpected error: %v", err)
	}
	if err := repo.SoftDelete(context.Background(), service.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDelete() error = %v, want ErrNotFound", err)
	}
}
