//go:build integration

package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// assertNoReading fails the test if any of svc's latest-reading fields is
// non-nil.
func assertNoReading(t *testing.T, svc Service) {
	t.Helper()
	if svc.LastBurnRateShort != nil || svc.LastBurnRateLong != nil || svc.LastWindowErrors != nil ||
		svc.LastWindowRequests != nil || svc.LastReadingAt != nil || svc.LastErrorAt != nil {
		t.Errorf("reading fields = (%v, %v, %v, %v, %v, %v), want all nil",
			svc.LastBurnRateShort, svc.LastBurnRateLong, svc.LastWindowErrors,
			svc.LastWindowRequests, svc.LastReadingAt, svc.LastErrorAt)
	}
}

// assertReading fails the test unless svc's latest-reading fields match rd.
func assertReading(t *testing.T, svc Service, rd ServiceReading) {
	t.Helper()
	if svc.LastBurnRateShort == nil || *svc.LastBurnRateShort != rd.BurnShort {
		t.Errorf("LastBurnRateShort = %v, want %v", svc.LastBurnRateShort, rd.BurnShort)
	}
	if svc.LastBurnRateLong == nil || *svc.LastBurnRateLong != rd.BurnLong {
		t.Errorf("LastBurnRateLong = %v, want %v", svc.LastBurnRateLong, rd.BurnLong)
	}
	if svc.LastWindowErrors == nil || *svc.LastWindowErrors != rd.ShortErrors {
		t.Errorf("LastWindowErrors = %v, want %v", svc.LastWindowErrors, rd.ShortErrors)
	}
	if svc.LastWindowRequests == nil || *svc.LastWindowRequests != rd.ShortRequests {
		t.Errorf("LastWindowRequests = %v, want %v", svc.LastWindowRequests, rd.ShortRequests)
	}
	if svc.LastReadingAt == nil || !svc.LastReadingAt.Equal(rd.ReadAt) {
		t.Errorf("LastReadingAt = %v, want %v", svc.LastReadingAt, rd.ReadAt)
	}
	switch {
	case rd.LastErrorAt == nil && svc.LastErrorAt != nil:
		t.Errorf("LastErrorAt = %v, want nil", *svc.LastErrorAt)
	case rd.LastErrorAt != nil && (svc.LastErrorAt == nil || !svc.LastErrorAt.Equal(*rd.LastErrorAt)):
		t.Errorf("LastErrorAt = %v, want %v", svc.LastErrorAt, *rd.LastErrorAt)
	}
}

// pgNow returns the current time truncated to Postgres timestamptz
// precision, so a value round-tripped through the database compares equal.
func pgNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func lookupService(services []Service, id string) (Service, bool) {
	for _, s := range services {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

func findService(t *testing.T, services []Service, id string) Service {
	t.Helper()
	s, ok := lookupService(services, id)
	if !ok {
		t.Fatalf("service %s not found in list of %d", id, len(services))
	}
	return s
}

func TestServiceRepository_NewService_ReadingLoadsAsNil(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("reading-nil-%d", time.Now().UnixNano())
	svc := seedServiceFixtures(t, repo, pool, prefix, 1)[0]

	got, found, err := repo.Get(context.Background(), svc.ID)
	if err != nil || !found {
		t.Fatalf("Get() = (found=%v, err=%v), want found and no error", found, err)
	}
	assertNoReading(t, *got)

	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	assertNoReading(t, findService(t, list, svc.ID))
}

func TestServiceRepository_UpdateStatusAndReading_PersistsReadingOnAllReadPaths(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("reading-persist-%d", time.Now().UnixNano())
	svc := seedServiceFixtures(t, repo, pool, prefix, 1)[0]

	errAt := pgNow().Add(-2 * time.Minute)
	rd := ServiceReading{BurnShort: 14.4, BurnLong: 6.25, ShortErrors: 37, ShortRequests: 1200, ReadAt: pgNow(), LastErrorAt: &errAt}
	if err := repo.UpdateStatusAndReading(context.Background(), svc.ID, "degraded", rd); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}

	got, found, err := repo.Get(context.Background(), svc.ID)
	if err != nil || !found {
		t.Fatalf("Get() = (found=%v, err=%v), want found and no error", found, err)
	}
	if got.CurrentStatus != "degraded" {
		t.Errorf("CurrentStatus = %q, want %q", got.CurrentStatus, "degraded")
	}
	assertReading(t, *got, rd)

	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	assertReading(t, findService(t, list, svc.ID), rd)

	// The fixture's name prefix is unique, but ListPaginated is not
	// filtered by it - walk pages until the service shows up.
	for page := 1; ; page++ {
		items, total, err := repo.ListPaginated(context.Background(), page, 100)
		if err != nil {
			t.Fatalf("ListPaginated() returned unexpected error: %v", err)
		}
		if s, ok := lookupService(items, svc.ID); ok {
			assertReading(t, s, rd)
			break
		}
		if page*100 >= total {
			t.Fatalf("service %s not found by ListPaginated", svc.ID)
		}
	}
}

func TestServiceRepository_UpdateStatusAndReading_StatusChangeSemanticsMatchUpdateStatus(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("reading-status-%d", time.Now().UnixNano())
	svc := seedServiceFixtures(t, repo, pool, prefix, 1)[0]
	ctx := context.Background()

	if err := repo.UpdateStatusAndReading(ctx, svc.ID, "operational", ServiceReading{ReadAt: pgNow()}); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}
	first, _, err := repo.Get(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	// Same status again: the reading changes, last_status_change_at must
	// not move.
	rd := ServiceReading{BurnShort: 0.5, BurnLong: 0.2, ShortErrors: 1, ShortRequests: 500, ReadAt: pgNow()}
	if err := repo.UpdateStatusAndReading(ctx, svc.ID, "operational", rd); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}
	same, _, err := repo.Get(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !same.LastStatusChangeAt.Equal(first.LastStatusChangeAt) {
		t.Errorf("LastStatusChangeAt moved on an unchanged status: %v -> %v", first.LastStatusChangeAt, same.LastStatusChangeAt)
	}
	assertReading(t, *same, rd)

	// A different status bumps last_status_change_at. now() is the
	// transaction start time, so sleep past the previous one to make the
	// bump observable.
	time.Sleep(5 * time.Millisecond)
	if err := repo.UpdateStatusAndReading(ctx, svc.ID, "major_outage", rd); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}
	changed, _, err := repo.Get(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if changed.CurrentStatus != "major_outage" {
		t.Errorf("CurrentStatus = %q, want %q", changed.CurrentStatus, "major_outage")
	}
	if !changed.LastStatusChangeAt.After(same.LastStatusChangeAt) {
		t.Errorf("LastStatusChangeAt = %v, want after %v on a status change", changed.LastStatusChangeAt, same.LastStatusChangeAt)
	}
}

func TestServiceRepository_UpdateStatusAndReading_NilLastErrorAtClearsColumn(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	prefix := fmt.Sprintf("reading-clear-%d", time.Now().UnixNano())
	svc := seedServiceFixtures(t, repo, pool, prefix, 1)[0]
	ctx := context.Background()

	errAt := pgNow()
	if err := repo.UpdateStatusAndReading(ctx, svc.ID, "degraded", ServiceReading{ShortErrors: 3, ShortRequests: 10, ReadAt: pgNow(), LastErrorAt: &errAt}); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}
	rd := ServiceReading{ShortRequests: 10, ReadAt: pgNow()}
	if err := repo.UpdateStatusAndReading(ctx, svc.ID, "operational", rd); err != nil {
		t.Fatalf("UpdateStatusAndReading() returned unexpected error: %v", err)
	}

	got, _, err := repo.Get(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	assertReading(t, *got, rd)
}

// rlsServiceID returns the id of the single service seedTenantWithService
// created for tenantID, read through that tenant's own RLS session.
func rlsServiceID(t *testing.T, pool *Pool, tenantID string) string {
	t.Helper()
	tx, ctx := beginRLSTx(t, pool, "", tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()

	var id string
	if err := pool.QueryRow(ctx, "SELECT id FROM services").Scan(&id); err != nil {
		t.Fatalf("selecting tenant's service returned unexpected error: %v", err)
	}
	return id
}

// TestServiceRepository_UpdateStatusAndReading_TenantIsolation runs as the
// non-superuser rlsTestRole (see rls_test.go) so RLS genuinely applies:
// tenant A writes and reads its own reading, tenant B can neither see A's
// service nor overwrite its reading.
func TestServiceRepository_UpdateStatusAndReading_TenantIsolation(t *testing.T) {
	pool := newRLSTestPool(t)
	repo := NewServiceRepository(pool)
	tenantA := seedTenantWithService(t, pool, fmt.Sprintf("reading-rls-a-%d", tUniqueSuffix()))
	tenantB := seedTenantWithService(t, pool, fmt.Sprintf("reading-rls-b-%d", tUniqueSuffix()))
	serviceA := rlsServiceID(t, pool, tenantA)

	rd := ServiceReading{BurnShort: 20, BurnLong: 3, ShortErrors: 50, ShortRequests: 400, ReadAt: pgNow()}
	txA, ctxA := beginRLSTx(t, pool, "", tenantA)
	if err := repo.UpdateStatusAndReading(ctxA, serviceA, "major_outage", rd); err != nil {
		_ = txA.Rollback(context.Background())
		t.Fatalf("UpdateStatusAndReading() as tenant A returned unexpected error: %v", err)
	}
	if err := txA.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	// Tenant B: A's service is invisible, and an UPDATE aimed at it
	// matches zero rows (no error, same as an unknown id).
	txB, ctxB := beginRLSTx(t, pool, "", tenantB)
	if _, found, err := repo.Get(ctxB, serviceA); err != nil || found {
		_ = txB.Rollback(context.Background())
		t.Fatalf("Get() of tenant A's service as tenant B = (found=%v, err=%v), want not found", found, err)
	}
	foreign := ServiceReading{BurnShort: 0, BurnLong: 0, ShortErrors: 0, ShortRequests: 1, ReadAt: pgNow()}
	if err := repo.UpdateStatusAndReading(ctxB, serviceA, "operational", foreign); err != nil {
		_ = txB.Rollback(context.Background())
		t.Fatalf("UpdateStatusAndReading() as tenant B returned unexpected error: %v", err)
	}
	if err := txB.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	// Tenant A still sees its own, untouched reading.
	txA2, ctxA2 := beginRLSTx(t, pool, "", tenantA)
	defer func() { _ = txA2.Rollback(context.Background()) }()
	got, found, err := repo.Get(ctxA2, serviceA)
	if err != nil || !found {
		t.Fatalf("Get() as tenant A = (found=%v, err=%v), want found and no error", found, err)
	}
	if got.CurrentStatus != "major_outage" {
		t.Errorf("CurrentStatus = %q, want %q (tenant B must not overwrite it)", got.CurrentStatus, "major_outage")
	}
	assertReading(t, *got, rd)
}

// TestServiceLatestReadingMigration_DownReversesCleanly asserts 0052's
// down.sql drops the columns without error and the schema can be migrated
// back up afterwards.
func TestServiceLatestReadingMigration_DownReversesCleanly(t *testing.T) {
	// Own scratch database: stepping a migration down on the shared
	// TEST_DATABASE_URL would break every other test using it.
	dsn := newScratchDatabase(t)

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		t.Fatalf("pgxmigrate.WithInstance() returned unexpected error: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://migrations", "pgx5", driver)
	if err != nil {
		t.Fatalf("migrate.NewWithDatabaseInstance() returned unexpected error: %v", err)
	}

	const serviceLatestReadingVersion = 52
	if err := m.Migrate(serviceLatestReadingVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", serviceLatestReadingVersion, err)
	}

	columnCount := func() int {
		t.Helper()
		var n int
		if err := sqlDB.QueryRow(
			`SELECT COUNT(*) FROM information_schema.columns
			 WHERE table_name = 'services' AND column_name IN
			   ('last_burn_rate_short', 'last_burn_rate_long', 'last_window_errors',
			    'last_window_requests', 'last_reading_at', 'last_error_at')`,
		).Scan(&n); err != nil {
			t.Fatalf("counting reading columns returned unexpected error: %v", err)
		}
		return n
	}
	if n := columnCount(); n != 6 {
		t.Fatalf("reading columns after up = %d, want 6", n)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if n := columnCount(); n != 0 {
		t.Fatalf("reading columns after down = %d, want 0", n)
	}

	if err := m.Migrate(serviceLatestReadingVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", serviceLatestReadingVersion, err)
	}
}
