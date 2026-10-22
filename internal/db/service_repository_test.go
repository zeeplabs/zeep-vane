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

// TestServiceRepository_Create_PersistsSLOTypeAndDatadogServiceTag covers
// slo-root-cause-enrichment RCA-01: Create persists both new columns and
// Get reads them back unchanged.
func TestServiceRepository_Create_PersistsSLOTypeAndDatadogServiceTag(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-rca-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-rca-1", SLOType: "metric", DatadogServiceTag: "service:checkout-svc"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.SLOType != "metric" {
		t.Errorf("got.SLOType = %q, want %q", got.SLOType, "metric")
	}
	if got.DatadogServiceTag != "service:checkout-svc" {
		t.Errorf("got.DatadogServiceTag = %q, want %q", got.DatadogServiceTag, "service:checkout-svc")
	}
}

// TestServiceRepository_Create_EmptySLOTypeAndTag_StoredAsNullReadAsEmpty
// covers a pre-feature-shaped row (flow-type SLO, or a row saved before
// SearchSLOs decoded these fields): "" in, "" out, never a scan error from
// a NULL column.
func TestServiceRepository_Create_EmptySLOTypeAndTag_StoredAsNullReadAsEmpty(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-rca-empty-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-rca-2"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.SLOType != "" {
		t.Errorf("got.SLOType = %q, want empty", got.SLOType)
	}
	if got.DatadogServiceTag != "" {
		t.Errorf("got.DatadogServiceTag = %q, want empty", got.DatadogServiceTag)
	}
}

// TestServiceRepository_UpdateSLOMetadata_SetsThenClears_RoundTrips covers
// RCA-01's admin-facing round trip: set both fields, read them back, then
// clear both via "" and confirm they read back empty again (not left
// stale).
func TestServiceRepository_UpdateSLOMetadata_SetsThenClears_RoundTrips(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("update-rca-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-rca-3"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.UpdateSLOMetadata(context.Background(), service.ID, "metric", "service:checkout-svc"); err != nil {
		t.Fatalf("UpdateSLOMetadata() set returned unexpected error: %v", err)
	}
	got, _, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() after set returned unexpected error: %v", err)
	}
	if got.SLOType != "metric" || got.DatadogServiceTag != "service:checkout-svc" {
		t.Fatalf("after set: got = {SLOType:%q DatadogServiceTag:%q}, want {metric service:checkout-svc}", got.SLOType, got.DatadogServiceTag)
	}

	if err := repo.UpdateSLOMetadata(context.Background(), service.ID, "", ""); err != nil {
		t.Fatalf("UpdateSLOMetadata() clear returned unexpected error: %v", err)
	}
	got, _, err = repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() after clear returned unexpected error: %v", err)
	}
	if got.SLOType != "" {
		t.Errorf("after clear: got.SLOType = %q, want empty", got.SLOType)
	}
	if got.DatadogServiceTag != "" {
		t.Errorf("after clear: got.DatadogServiceTag = %q, want empty", got.DatadogServiceTag)
	}
}

// TestServiceRepository_UpdateWithMetadata_AppliesNameSLOAndEnvironmentTogether
// covers the item-5 fix: a single UpdateWithMetadata() call applies the
// rename, SLO metadata, and environment tag together (previously 3
// separate non-transactional handler-level calls - Update, then
// UpdateSLOMetadata, then UpdateEnvironment).
func TestServiceRepository_UpdateWithMetadata_AppliesNameSLOAndEnvironmentTogether(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("update-with-metadata-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-wm-1"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	newName := name + "-renamed"
	sloType := "metric"
	datadogServiceTag := "service:checkout-svc"
	environment := "staging"
	if err := repo.UpdateWithMetadata(context.Background(), service.ID, newName, &sloType, &datadogServiceTag, &environment); err != nil {
		t.Fatalf("UpdateWithMetadata() returned unexpected error: %v", err)
	}

	got, _, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if got.Name != newName {
		t.Errorf("Name = %q, want %q", got.Name, newName)
	}
	if got.SLOType != sloType || got.DatadogServiceTag != datadogServiceTag {
		t.Errorf("SLO metadata = {%q %q}, want {%q %q}", got.SLOType, got.DatadogServiceTag, sloType, datadogServiceTag)
	}
	if got.Environment != environment {
		t.Errorf("Environment = %q, want %q", got.Environment, environment)
	}
}

// TestServiceRepository_UpdateWithMetadata_NilSLOAndEnvironment_LeavesThemUnchanged
// asserts a plain rename (sloType/datadogServiceTag/environment all nil)
// never clobbers previously set SLO metadata or environment tag - the same
// guarantee the old 3-separate-calls handler code relied on, now enforced
// inside UpdateWithMetadata itself.
func TestServiceRepository_UpdateWithMetadata_NilSLOAndEnvironment_LeavesThemUnchanged(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("update-with-metadata-nil-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-wm-2"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.UpdateSLOMetadata(context.Background(), service.ID, "metric", "service:checkout-svc"); err != nil {
		t.Fatalf("setup UpdateSLOMetadata() returned unexpected error: %v", err)
	}
	if err := repo.UpdateEnvironment(context.Background(), service.ID, "production"); err != nil {
		t.Fatalf("setup UpdateEnvironment() returned unexpected error: %v", err)
	}

	newName := name + "-renamed-only"
	if err := repo.UpdateWithMetadata(context.Background(), service.ID, newName, nil, nil, nil); err != nil {
		t.Fatalf("UpdateWithMetadata() returned unexpected error: %v", err)
	}

	got, _, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if got.Name != newName {
		t.Errorf("Name = %q, want %q", got.Name, newName)
	}
	if got.SLOType != "metric" || got.DatadogServiceTag != "service:checkout-svc" {
		t.Errorf("SLO metadata changed by a plain rename = {%q %q}, want unchanged {metric service:checkout-svc}", got.SLOType, got.DatadogServiceTag)
	}
	if got.Environment != "production" {
		t.Errorf("Environment changed by a plain rename = %q, want unchanged %q", got.Environment, "production")
	}
}

// TestServiceRepository_UpdateWithMetadata_UnknownID_ReturnsErrNotFound
// mirrors Update's not-found convention.
func TestServiceRepository_UpdateWithMetadata_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	sloType := "metric"
	datadogServiceTag := "service:x"
	err := repo.UpdateWithMetadata(context.Background(), "00000000-0000-0000-0000-000000000000", "name", &sloType, &datadogServiceTag, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateWithMetadata() error = %v, want ErrNotFound", err)
	}
}

// TestServiceRepository_UpdateSLOMetadata_UnknownID_ReturnsErrNotFound
// mirrors Update's not-found convention.
func TestServiceRepository_UpdateSLOMetadata_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	err := repo.UpdateSLOMetadata(context.Background(), "00000000-0000-0000-0000-000000000000", "metric", "service:x")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSLOMetadata() error = %v, want ErrNotFound", err)
	}
}

// TestServiceRepository_List_IncludesSLOTypeAndDatadogServiceTag covers the
// poller's read path (internal/poller/poller.go calls List every cycle and
// feeds each db.Service into SLOAnalyzer.HandleTransition) - RCA-01's
// resolveCauseHint reads svc.SLOType/svc.DatadogServiceTag from exactly
// this call.
func TestServiceRepository_List_IncludesSLOTypeAndDatadogServiceTag(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("list-rca-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-rca-4", SLOType: "metric", DatadogServiceTag: "service:checkout-svc"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	all, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	var found *Service
	for i := range all {
		if all[i].ID == service.ID {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatalf("List() did not return the seeded service")
	}
	if found.SLOType != "metric" {
		t.Errorf("found.SLOType = %q, want %q", found.SLOType, "metric")
	}
	if found.DatadogServiceTag != "service:checkout-svc" {
		t.Errorf("found.DatadogServiceTag = %q, want %q", found.DatadogServiceTag, "service:checkout-svc")
	}
}

// TestServiceRepository_Create_PersistsEnvironment covers SET-01/SET-02:
// Create persists a non-empty Environment value and Get reads it back
// unchanged.
func TestServiceRepository_Create_PersistsEnvironment(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-env-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-env-1", Environment: "staging"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.Environment != "staging" {
		t.Errorf("got.Environment = %q, want %q", got.Environment, "staging")
	}
}

// TestServiceRepository_Create_EmptyEnvironment_DefaultsToEmpty covers the
// ""-means-absent convention (SET-01): a Service with no Environment set
// persists and reads back "" (no tag), same as every pre-feature caller.
func TestServiceRepository_Create_EmptyEnvironment_DefaultsToEmpty(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-env-empty-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-env-2"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.Environment != "" {
		t.Errorf("got.Environment = %q, want empty", got.Environment)
	}
}

// TestServiceRepository_Create_PollingMode_PersistsEnvironment covers that
// Environment is independent of MonitorMode (SET-01): a polling-mode
// service's environment column is set through the same Create branch that
// skips slo_id/slo_name.
func TestServiceRepository_Create_PollingMode_PersistsEnvironment(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("create-env-polling-%d", time.Now().UnixNano())
	service := &Service{
		Name:                name,
		MonitorMode:         "polling",
		PollType:            strPtr("http"),
		PollTarget:          strPtr("https://example.com/health"),
		PollIntervalSeconds: intPtr(60),
		Environment:         "production",
	}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	got, found, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if got.Environment != "production" {
		t.Errorf("got.Environment = %q, want %q", got.Environment, "production")
	}
}

// TestServiceRepository_ListPaginated_IncludesEnvironment covers the admin
// list read path (SET-04): ListPaginated returns Environment alongside the
// other fields List/Get already return.
func TestServiceRepository_ListPaginated_IncludesEnvironment(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("listpaginated-env-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-env-3", Environment: "development"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	items, _, err := repo.ListPaginated(context.Background(), 1, 1000)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	var found *Service
	for i := range items {
		if items[i].ID == service.ID {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatalf("ListPaginated() did not return the seeded service")
	}
	if found.Environment != "development" {
		t.Errorf("found.Environment = %q, want %q", found.Environment, "development")
	}
}

// TestServiceRepository_UpdateEnvironment_SetsThenClears_RoundTrips covers
// SET-03's admin-facing round trip: set the environment, read it back, then
// clear it via "" and confirm it reads back empty again (not left stale) -
// mirrors UpdateSLOMetadata's set/clear round trip above.
func TestServiceRepository_UpdateEnvironment_SetsThenClears_RoundTrips(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("update-env-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-env-4"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if err := repo.UpdateEnvironment(context.Background(), service.ID, "production"); err != nil {
		t.Fatalf("UpdateEnvironment() set returned unexpected error: %v", err)
	}
	got, _, err := repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() after set returned unexpected error: %v", err)
	}
	if got.Environment != "production" {
		t.Fatalf("after set: got.Environment = %q, want %q", got.Environment, "production")
	}

	if err := repo.UpdateEnvironment(context.Background(), service.ID, ""); err != nil {
		t.Fatalf("UpdateEnvironment() clear returned unexpected error: %v", err)
	}
	got, _, err = repo.Get(context.Background(), service.ID)
	if err != nil {
		t.Fatalf("Get() after clear returned unexpected error: %v", err)
	}
	if got.Environment != "" {
		t.Errorf("after clear: got.Environment = %q, want empty", got.Environment)
	}
}

// TestServiceRepository_UpdateEnvironment_UnknownID_ReturnsErrNotFound
// mirrors UpdateSLOMetadata's not-found convention.
func TestServiceRepository_UpdateEnvironment_UnknownID_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	err := repo.UpdateEnvironment(context.Background(), "00000000-0000-0000-0000-000000000000", "production")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEnvironment() error = %v, want ErrNotFound", err)
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

// TestServiceRepository_SoftDelete_ConcurrentAttach_SerializesAndBlocksDelete
// covers the Verifier finding in service-delete validation.md (M2): without
// SoftDelete locking the services row FOR UPDATE before its
// status_page_services check, a concurrent attach could commit between the
// check and the UPDATE, leaving a service both deleted and attached - the
// exact state spec.md's Assumptions table says cannot occur. Mirrors
// TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins's technique:
// an explicit "holder" transaction takes the same row lock SoftDelete would
// take, performs the attach itself, and stays open while a real SoftDelete
// call runs concurrently in a goroutine - proving SoftDelete cannot
// complete until the holder releases the lock, and that on release it
// observes the now-attached service and returns ErrServiceInUse, not a
// delete that raced past the attach.
func TestServiceRepository_SoftDelete_ConcurrentAttach_SerializesAndBlocksDelete(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("softdelete-race-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-race"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	ctx := context.Background()

	holderTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin holder transaction: %v", err)
	}
	defer func() { _ = holderTx.Rollback(context.Background()) }()

	var lockedID string
	if err := holderTx.QueryRow(ctx, "SELECT id FROM services WHERE id = $1 FOR UPDATE", service.ID).Scan(&lockedID); err != nil {
		t.Fatalf("holder SELECT ... FOR UPDATE failed: %v", err)
	}

	var statusPageID string
	if err := holderTx.QueryRow(ctx,
		"INSERT INTO status_pages (name, tenant_id) VALUES ($1, $2) RETURNING id",
		"softdelete-race-page", tenantID,
	).Scan(&statusPageID); err != nil {
		t.Fatalf("holder status_pages INSERT failed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM status_pages WHERE id = $1", statusPageID) })
	if _, err := holderTx.Exec(ctx,
		"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)", statusPageID, service.ID,
	); err != nil {
		t.Fatalf("holder status_page_services INSERT failed: %v", err)
	}

	// Run the real SoftDelete call while holderTx is still open and
	// uncommitted. With the production SELECT ... FOR UPDATE in place, this
	// call cannot observe a result until holderTx releases the row lock.
	done := make(chan error, 1)
	go func() {
		done <- repo.SoftDelete(context.Background(), service.ID)
	}()

	select {
	case err := <-done:
		t.Fatalf("SoftDelete() returned (err=%v) while the holder transaction was still open - the row lock did not block it", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked behind the holder's uncommitted row lock.
	}

	if err := holderTx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit holder transaction: %v", err)
	}

	var softDeleteErr error
	select {
	case softDeleteErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SoftDelete() did not return after the holder transaction committed")
	}

	if !errors.Is(softDeleteErr, ErrServiceInUse) {
		t.Fatalf("SoftDelete() error = %v, want ErrServiceInUse (the attach committed first)", softDeleteErr)
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT deleted_at FROM services WHERE id = $1", service.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("verify deleted_at query returned unexpected error: %v", err)
	}
	if deletedAt != nil {
		t.Errorf("deleted_at = %v, want nil - SoftDelete must not have applied after losing the race", *deletedAt)
	}
}

// TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents
// covers SVCDEL-07: history rows referencing the deleted service's ID are
// never touched (no cascade, no cleanup).
func TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	tenantID := seedPlainTenant(t, pool)
	name := fmt.Sprintf("softdelete-history-%d", time.Now().UnixNano())
	service := &Service{Name: name, SLOID: "slo-history"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := repo.Create(ctx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at) VALUES ($1, 'operational', 0, now(), now())",
		service.ID,
	); err != nil {
		t.Fatalf("setup status_intervals INSERT returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM status_intervals WHERE service_id = $1", service.ID)
	})

	var incidentID string
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO incidents (title, tenant_id) VALUES ($1, $2) RETURNING id", "softdelete-history-incident", tenantID,
	).Scan(&incidentID); err != nil {
		t.Fatalf("setup incidents INSERT returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incidentID) })
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO incident_services (incident_id, service_id) VALUES ($1, $2)", incidentID, service.ID,
	); err != nil {
		t.Fatalf("setup incident_services INSERT returned unexpected error: %v", err)
	}

	if err := repo.SoftDelete(context.Background(), service.ID); err != nil {
		t.Fatalf("SoftDelete() returned unexpected error: %v", err)
	}

	var intervalCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM status_intervals WHERE service_id = $1", service.ID).Scan(&intervalCount); err != nil {
		t.Fatalf("verify status_intervals count query returned unexpected error: %v", err)
	}
	if intervalCount != 1 {
		t.Errorf("status_intervals count after SoftDelete = %d, want 1 (preserved)", intervalCount)
	}

	var incidentServiceCount int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM incident_services WHERE service_id = $1", service.ID).Scan(&incidentServiceCount); err != nil {
		t.Fatalf("verify incident_services count query returned unexpected error: %v", err)
	}
	if incidentServiceCount != 1 {
		t.Errorf("incident_services count after SoftDelete = %d, want 1 (preserved)", incidentServiceCount)
	}
}

// newListForStatusPageTestRepos returns a *ServiceRepository and
// *StatusPageRepository sharing one tenant-scoped pool (T8) - both
// services and status pages/groups need a valid tenant_id to satisfy the
// NOT NULL constraint 0024 added, and a tenant-scoped pool (session-level
// app.tenant_id default, same helper status_page_repository_test.go's own
// suite uses) lets every fixture call below go through plain
// context.Background() instead of an explicit withTenantTx wrapper.
func newListForStatusPageTestRepos(t *testing.T) (*ServiceRepository, *StatusPageRepository, *Pool) {
	t.Helper()
	pool, _ := newTenantScopedPool(t)
	return NewServiceRepository(pool), NewStatusPageRepository(pool), pool
}

// TestServiceRepository_ListForStatusPage_TwoGroups_OrdersByGroupThenPosition
// asserts T8's core ordering contract: services sort by their own group's
// position first (every service in an earlier group precedes every service
// in a later one), then by their own position within that group - not by
// name, the pre-T8 behavior.
func TestServiceRepository_ListForStatusPage_TwoGroups_OrdersByGroupThenPosition(t *testing.T) {
	serviceRepo, pageRepo, pool := newListForStatusPageTestRepos(t)
	pageID := createDomainlessStatusPage(t, pageRepo, pool, "lfsp-two-groups-page")

	// Named so alphabetical order (zzz-later, aaa-first) is the OPPOSITE of
	// the intended group-position order below - a passing test on
	// coincidental name order would be a false positive.
	svcInLaterGroup := createSetServicesTestService(t, pool, "lfsp-aaa-in-later-group")
	svcInEarlierGroup := createSetServicesTestService(t, pool, "lfsp-zzz-in-earlier-group")

	groupEarlier, err := pageRepo.CreateGroup(context.Background(), pageID, "Earlier Group")
	if err != nil {
		t.Fatalf("setup CreateGroup(earlier) returned unexpected error: %v", err)
	}
	groupLater, err := pageRepo.CreateGroup(context.Background(), pageID, "Later Group")
	if err != nil {
		t.Fatalf("setup CreateGroup(later) returned unexpected error: %v", err)
	}

	placements := []ServicePlacement{
		{ServiceID: svcInLaterGroup, GroupID: &groupLater.ID, Position: 0},
		{ServiceID: svcInEarlierGroup, GroupID: &groupEarlier.ID, Position: 0},
	}
	if err := pageRepo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
	}

	got, err := serviceRepo.ListForStatusPage(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListForStatusPage() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForStatusPage() len = %d, want 2", len(got))
	}
	if got[0].ID != svcInEarlierGroup || got[1].ID != svcInLaterGroup {
		t.Fatalf("ListForStatusPage() order = [%s, %s], want [earlier-group service, later-group service]", got[0].Name, got[1].Name)
	}
	if got[0].GroupID == nil || *got[0].GroupID != groupEarlier.ID {
		t.Errorf("got[0].GroupID = %v, want %q", got[0].GroupID, groupEarlier.ID)
	}
	if got[0].GroupName == nil || *got[0].GroupName != "Earlier Group" {
		t.Errorf("got[0].GroupName = %v, want %q", got[0].GroupName, "Earlier Group")
	}
	if got[1].GroupID == nil || *got[1].GroupID != groupLater.ID {
		t.Errorf("got[1].GroupID = %v, want %q", got[1].GroupID, groupLater.ID)
	}
	if got[1].GroupName == nil || *got[1].GroupName != "Later Group" {
		t.Errorf("got[1].GroupName = %v, want %q", got[1].GroupName, "Later Group")
	}
}

// TestServiceRepository_ListForStatusPage_MixedGroupedAndUngrouped_UngroupedLast
// asserts NULLS LAST: a page mixing grouped and ungrouped services returns
// every grouped service (ordered by its group's position) before every
// ungrouped one, regardless of name.
func TestServiceRepository_ListForStatusPage_MixedGroupedAndUngrouped_UngroupedLast(t *testing.T) {
	serviceRepo, pageRepo, pool := newListForStatusPageTestRepos(t)
	pageID := createDomainlessStatusPage(t, pageRepo, pool, "lfsp-mixed-page")

	// Named so the ungrouped service would sort FIRST by name alone - again
	// ruling out a false positive from coincidental name order.
	svcUngrouped := createSetServicesTestService(t, pool, "lfsp-aaa-ungrouped")
	svcGrouped := createSetServicesTestService(t, pool, "lfsp-zzz-grouped")

	group, err := pageRepo.CreateGroup(context.Background(), pageID, "Only Group")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	placements := []ServicePlacement{
		{ServiceID: svcUngrouped, Position: 0},
		{ServiceID: svcGrouped, GroupID: &group.ID, Position: 0},
	}
	if err := pageRepo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
	}

	got, err := serviceRepo.ListForStatusPage(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListForStatusPage() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForStatusPage() len = %d, want 2", len(got))
	}
	if got[0].ID != svcGrouped || got[1].ID != svcUngrouped {
		t.Fatalf("ListForStatusPage() order = [%s, %s], want [grouped service, ungrouped service] (NULLS LAST)", got[0].Name, got[1].Name)
	}
	if got[1].GroupID != nil {
		t.Errorf("got[1] (ungrouped).GroupID = %v, want nil", *got[1].GroupID)
	}
	if got[1].GroupName != nil {
		t.Errorf("got[1] (ungrouped).GroupName = %v, want nil", *got[1].GroupName)
	}
}

// TestServiceRepository_ListForStatusPage_ParentChild_BothReturnedWithCorrectParentServiceID
// asserts a parent/child pair is returned as two independent placement
// rows, position-ordered like any other pair - ParentServiceID carries the
// relationship, but it imposes no extra ordering requirement of its own.
func TestServiceRepository_ListForStatusPage_ParentChild_BothReturnedWithCorrectParentServiceID(t *testing.T) {
	serviceRepo, pageRepo, pool := newListForStatusPageTestRepos(t)
	pageID := createDomainlessStatusPage(t, pageRepo, pool, "lfsp-parent-child-page")
	parent := createSetServicesTestService(t, pool, "lfsp-parent")
	child := createSetServicesTestService(t, pool, "lfsp-child")

	placements := []ServicePlacement{
		{ServiceID: parent, Position: 0},
		{ServiceID: child, ParentServiceID: &parent, Position: 1},
	}
	if err := pageRepo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
	}

	got, err := serviceRepo.ListForStatusPage(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListForStatusPage() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForStatusPage() len = %d, want 2", len(got))
	}

	byID := map[string]Service{got[0].ID: got[0], got[1].ID: got[1]}
	gotParent, ok := byID[parent]
	if !ok {
		t.Fatalf("ListForStatusPage() result missing parent service %s", parent)
	}
	if gotParent.ParentServiceID != nil {
		t.Errorf("parent.ParentServiceID = %v, want nil", *gotParent.ParentServiceID)
	}
	if gotParent.Position != 0 {
		t.Errorf("parent.Position = %d, want 0", gotParent.Position)
	}

	gotChild, ok := byID[child]
	if !ok {
		t.Fatalf("ListForStatusPage() result missing child service %s", child)
	}
	if gotChild.ParentServiceID == nil || *gotChild.ParentServiceID != parent {
		t.Errorf("child.ParentServiceID = %v, want %q", gotChild.ParentServiceID, parent)
	}
	if gotChild.Position != 1 {
		t.Errorf("child.Position = %d, want 1", gotChild.Position)
	}
	if gotChild.GroupID != nil {
		t.Errorf("child.GroupID = %v, want nil (no group assigned in this test)", *gotChild.GroupID)
	}
}

// TestServiceRepository_ListForStatusPage_NoGroups_UnchangedResultNoRegression
// is the pre-T8 regression check (tasks.md's Done-when): a page with no
// groups at all still returns every linked service, in position order,
// with every new field at its nil/zero value, and the pre-existing fields
// (name, current status, status analysis) untouched by this task.
func TestServiceRepository_ListForStatusPage_NoGroups_UnchangedResultNoRegression(t *testing.T) {
	serviceRepo, pageRepo, pool := newListForStatusPageTestRepos(t)
	pageID := createDomainlessStatusPage(t, pageRepo, pool, "lfsp-no-groups-page")
	svcA := createSetServicesTestService(t, pool, "lfsp-nogroups-aaa")
	svcB := createSetServicesTestService(t, pool, "lfsp-nogroups-bbb")

	placements := []ServicePlacement{
		{ServiceID: svcB, Position: 0},
		{ServiceID: svcA, Position: 1},
	}
	if err := pageRepo.SetServices(context.Background(), pageID, placements); err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
	}

	got, err := serviceRepo.ListForStatusPage(context.Background(), pageID)
	if err != nil {
		t.Fatalf("ListForStatusPage() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListForStatusPage() len = %d, want 2", len(got))
	}
	if got[0].ID != svcB || got[1].ID != svcA {
		t.Fatalf("ListForStatusPage() order = [%s, %s], want position order [svcB, svcA], not alphabetical", got[0].Name, got[1].Name)
	}
	for i, service := range got {
		if service.GroupID != nil {
			t.Errorf("got[%d].GroupID = %v, want nil", i, *service.GroupID)
		}
		if service.GroupName != nil {
			t.Errorf("got[%d].GroupName = %v, want nil", i, *service.GroupName)
		}
		if service.ParentServiceID != nil {
			t.Errorf("got[%d].ParentServiceID = %v, want nil", i, *service.ParentServiceID)
		}
		if service.CurrentStatus == "" {
			t.Errorf("got[%d].CurrentStatus = %q, want non-empty (pre-existing field untouched by this task)", i, service.CurrentStatus)
		}
	}
}

// seedModeServices creates one slo-mode and one polling-mode service in a
// fresh tenant and registers their cleanup - fixtures for the monitor_mode
// read-path tests below.
func seedModeServices(t *testing.T, repo *ServiceRepository, pool *Pool, prefix string) (sloSvc, pollingSvc *Service) {
	t.Helper()
	tenantID := seedPlainTenant(t, pool)
	stamp := time.Now().UnixNano()
	sloSvc = &Service{Name: fmt.Sprintf("%s-slo-%d", prefix, stamp), SLOID: fmt.Sprintf("slo-%s-%d", prefix, stamp)}
	pollingSvc = &Service{
		Name:                fmt.Sprintf("%s-polling-%d", prefix, stamp),
		MonitorMode:         "polling",
		PollType:            strPtr("http"),
		PollTarget:          strPtr("https://example.test/health"),
		PollIntervalSeconds: intPtr(60),
	}
	for _, s := range []*Service{sloSvc, pollingSvc} {
		withTenantTx(t, pool, tenantID, func(ctx context.Context) {
			if err := repo.Create(ctx, s); err != nil {
				t.Fatalf("setup Create() returned unexpected error: %v", err)
			}
		})
		id := s.ID
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", id) })
	}
	return sloSvc, pollingSvc
}

// assertSLOMode fails unless got is an slo-mode row with every poll_* nil.
func assertSLOMode(t *testing.T, got Service) {
	t.Helper()
	if got.MonitorMode != "slo" {
		t.Errorf("slo service MonitorMode = %q, want %q", got.MonitorMode, "slo")
	}
	if got.PollType != nil || got.PollTarget != nil || got.PollIntervalSeconds != nil {
		t.Errorf("slo service poll fields = (%v, %v, %v), want all nil", got.PollType, got.PollTarget, got.PollIntervalSeconds)
	}
}

// assertPollingMode fails unless got is the polling row seeded by
// seedModeServices.
func assertPollingMode(t *testing.T, got Service) {
	t.Helper()
	if got.MonitorMode != "polling" {
		t.Errorf("polling service MonitorMode = %q, want %q", got.MonitorMode, "polling")
	}
	if got.PollType == nil || *got.PollType != "http" {
		t.Errorf("polling service PollType = %v, want %q", got.PollType, "http")
	}
	if got.PollTarget == nil || *got.PollTarget != "https://example.test/health" {
		t.Errorf("polling service PollTarget = %v, want %q", got.PollTarget, "https://example.test/health")
	}
	if got.PollIntervalSeconds == nil || *got.PollIntervalSeconds != 60 {
		t.Errorf("polling service PollIntervalSeconds = %v, want 60", got.PollIntervalSeconds)
	}
}

// TestServiceRepository_Get_ReturnsMonitorModeAndPollFields guards the
// detail drawer's latest-reading gate: Get must load monitor_mode (it used
// to come back "" for every service, hiding the block in production) and
// the NULL-safe poll_* columns.
func TestServiceRepository_Get_ReturnsMonitorModeAndPollFields(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	sloSvc, pollingSvc := seedModeServices(t, repo, pool, "get-mode")

	gotSLO, found, err := repo.Get(context.Background(), sloSvc.ID)
	if err != nil || !found {
		t.Fatalf("Get(slo) = (found %v, err %v), want found with no error", found, err)
	}
	assertSLOMode(t, *gotSLO)

	gotPolling, found, err := repo.Get(context.Background(), pollingSvc.ID)
	if err != nil || !found {
		t.Fatalf("Get(polling) = (found %v, err %v), want found with no error", found, err)
	}
	assertPollingMode(t, *gotPolling)
}

// TestServiceRepository_ListPaginated_ReturnsMonitorModeAndPollFields is
// ListPaginated's counterpart: the admin list response mirrors the same
// serviceResponse fields, so it must load them too.
func TestServiceRepository_ListPaginated_ReturnsMonitorModeAndPollFields(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	sloSvc, pollingSvc := seedModeServices(t, repo, pool, "listpaginated-mode")

	items, _, err := repo.ListPaginated(context.Background(), 1, 1000)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	gotSLO, ok := lookupService(items, sloSvc.ID)
	if !ok {
		t.Fatalf("ListPaginated() did not return the seeded slo service")
	}
	assertSLOMode(t, gotSLO)
	gotPolling, ok := lookupService(items, pollingSvc.ID)
	if !ok {
		t.Fatalf("ListPaginated() did not return the seeded polling service")
	}
	assertPollingMode(t, gotPolling)
}

// TestServiceRepository_List_ReturnsMonitorModeAndPollFields keeps List
// (poller) in line with the other full-row read paths.
func TestServiceRepository_List_ReturnsMonitorModeAndPollFields(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	sloSvc, pollingSvc := seedModeServices(t, repo, pool, "list-mode")

	items, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	gotSLO, ok := lookupService(items, sloSvc.ID)
	if !ok {
		t.Fatalf("List() did not return the seeded slo service")
	}
	assertSLOMode(t, gotSLO)
	gotPolling, ok := lookupService(items, pollingSvc.ID)
	if !ok {
		t.Fatalf("List() did not return the seeded polling service")
	}
	assertPollingMode(t, gotPolling)
}
