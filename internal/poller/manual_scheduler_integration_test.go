//go:build integration

package poller

import (
	"context"
	"net"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// createTestPollingService seeds a real polling-manual service row (via a
// real tenant transaction) and registers its cleanup.
func createTestPollingService(t *testing.T, pool *db.Pool, services *db.ServiceRepository, tenantID, pollType, pollTarget string, pollIntervalSeconds int) db.Service {
	t.Helper()
	ctx := context.Background()

	svc := &db.Service{
		Name:                "manual-scheduler-test-svc",
		MonitorMode:         "polling",
		PollType:            &pollType,
		PollTarget:          &pollTarget,
		PollIntervalSeconds: &pollIntervalSeconds,
	}
	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	if err := services.Create(db.WithTenantTx(ctx, tx), svc); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", svc.ID) })

	return *svc
}

// TestManualScheduler_RunCheck_RealDB_WritesStatusIntervalRowSameShapeAsPoller
// asserts MP-12: a successful polling-manual check writes a status_intervals
// row through the exact same repository (db.StatusIntervalRepository) and
// exact same OpenOrExtend contract the Datadog poller's own pollService
// uses (poller_test.go's
// TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot asserts the
// identical row shape for that path) - proving polling-manual services need
// zero special-casing in internal/history/OverviewHandler's read paths.
func TestManualScheduler_RunCheck_RealDB_WritesStatusIntervalRowSameShapeAsPoller(t *testing.T) {
	pool, _ := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	tenantID := seedTestTenant(t, pool)

	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("could not listen on [::1] in this environment: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	svc := createTestPollingService(t, pool, services, tenantID, "tcp", listener.Addr().String(), 30)

	tenantTx := func(ctx context.Context, tid string) (context.Context, func(context.Context) error, func(context.Context), error) {
		tx, err := pool.BeginTenantTx(ctx, "", tid)
		if err != nil {
			return ctx, nil, nil, err
		}
		txCtx := db.WithTenantTx(ctx, tx)
		commit := func(c context.Context) error { return tx.Commit(c) }
		rollback := func(c context.Context) { _ = tx.Rollback(c) }
		return txCtx, commit, rollback, nil
	}

	s := NewManualScheduler(services, services, statusIntervals, db.NewSystemTenantLister(pool), tenantTx, zap.NewNop())

	failureStreak := 0
	status := s.runCheck(ctx, tenantID, svc.ID, "tcp", *svc.PollTarget, "not_configured", &failureStreak)
	if status != "operational" {
		t.Fatalf("runCheck() = %q, want %q", status, "operational")
	}

	found, ok, err := services.Get(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("service %s not found after runCheck", svc.ID)
	}
	if found.CurrentStatus != "operational" {
		t.Errorf("CurrentStatus = %q, want %q", found.CurrentStatus, "operational")
	}

	var intervalCount int
	row := pool.QueryRow(ctx,
		"SELECT count(*) FROM status_intervals WHERE service_id = $1 AND status = $2 AND error_budget_remaining = $3 AND ends_at IS NULL",
		svc.ID, "operational", float64(0),
	)
	if err := row.Scan(&intervalCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if intervalCount != 1 {
		t.Errorf("open status_intervals rows for service = %d, want 1 (same shape as the Datadog poller's own row)", intervalCount)
	}
}
