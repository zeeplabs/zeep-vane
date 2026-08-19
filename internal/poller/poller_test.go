//go:build integration

package poller

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func createTestService(t *testing.T, pool *db.Pool, services *db.ServiceRepository) db.Service {
	t.Helper()
	ctx := context.Background()

	svc := &db.Service{
		Name:  fmt.Sprintf("poller-test-%d", time.Now().UnixNano()),
		SLOID: "slo-poll-1",
	}
	if err := services.Create(ctx, svc); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM services WHERE id = $1", svc.ID) })

	return *svc
}

// TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot exercises one full
// iteration against real repositories with Datadog mocked (T23 Done-when):
// the cycle updates current_status per the fetched error budget/state and
// persists a status_snapshots row.
func TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	svc := createTestService(t, pool, services)

	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 91.2},
	}

	p := NewPoller(services, services, snapshots, provider, time.Hour, zap.NewNop())
	p.pollOnce(ctx)

	all, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}

	var found *db.Service
	for i := range all {
		if all[i].ID == svc.ID {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("service %s not found after pollOnce", svc.ID)
	}
	if found.CurrentStatus != "operational" {
		t.Errorf("CurrentStatus = %q, want %q", found.CurrentStatus, "operational")
	}

	var snapshotCount int
	row := pool.QueryRow(ctx,
		"SELECT count(*) FROM status_snapshots WHERE service_id = $1 AND status = $2 AND error_budget_remaining = $3",
		svc.ID, "operational", 91.2,
	)
	if err := row.Scan(&snapshotCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if snapshotCount != 1 {
		t.Errorf("status_snapshots rows for service = %d, want 1", snapshotCount)
	}
}

// TestPoller_Run_StopsOnContextCancel confirms the ticker loop exits
// cleanly when its context is canceled, so cmd/vane serve (T25) can shut it
// down without a goroutine leak.
func TestPoller_Run_StopsOnContextCancel(t *testing.T) {
	pool := newTestPool(t)
	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok"}}

	p := NewPoller(services, services, snapshots, provider, time.Hour, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
