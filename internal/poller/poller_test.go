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
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newTestPool(t *testing.T) (*db.Pool, string) {
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

	return pool, dsn
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

// createTestIntegration seeds an active Datadog integration row so
// MarkDatadogInvalid has a row to update, and cleans it up afterwards.
func createTestIntegration(t *testing.T, pool *db.Pool, dsn string, integrations *db.IntegrationRepository) {
	t.Helper()
	ctx := context.Background()

	dbtest.LockDatadogIntegration(t, ctx, dsn)

	if err := integrations.UpsertDatadog(ctx, []byte("encrypted-key"), []byte("encrypted-app-key")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM integrations WHERE provider = 'datadog'") })
}

// TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot exercises one full
// iteration against real repositories with Datadog mocked (T23 Done-when):
// the cycle updates current_status per the fetched error budget/state and
// persists a status_snapshots row.
func TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	svc := createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 91.2},
	}

	p := NewPoller(services, services, snapshots, integrations, provider, time.Hour, zap.NewNop())
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

// TestPoller_PollOnce_ConnectionFailure_MarksIntegrationInvalidAndKeepsLastStatus
// covers T24: once retries are exhausted, the Datadog integration is marked
// invalid with the failure reason (SP-09) for the admin to see, and the
// service's last known valid current_status is left untouched - a
// connection failure must never overwrite it (spec.md P1 AC4).
func TestPoller_PollOnce_ConnectionFailure_MarksIntegrationInvalidAndKeepsLastStatus(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	svc := createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	// Seed a known-good status via one successful poll, so the failure that
	// follows has a last-known-valid status to preserve.
	okProvider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 99}}
	seedingPoller := NewPoller(services, services, snapshots, integrations, okProvider, time.Hour, zap.NewNop())
	seedingPoller.pollOnce(ctx)

	backoffBase = time.Millisecond
	failingProvider := &fakeProvider{errs: []error{datadog.ErrTimeout, datadog.ErrTimeout, datadog.ErrTimeout}}
	p := NewPoller(services, services, snapshots, integrations, failingProvider, time.Hour, zap.NewNop())
	p.pollOnce(ctx)

	integration, err := integrations.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "invalid" {
		t.Errorf("Status = %q, want %q", integration.Status, "invalid")
	}
	if integration.LastError == nil || *integration.LastError == "" {
		t.Error("LastError is empty, want the connection failure reason recorded")
	}

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
		t.Errorf("CurrentStatus = %q after connection failure, want unchanged %q", found.CurrentStatus, "operational")
	}
}

// TestPoller_Run_StopsOnContextCancel confirms the ticker loop exits
// cleanly when its context is canceled, so cmd/vane serve (T25) can shut it
// down without a goroutine leak.
func TestPoller_Run_StopsOnContextCancel(t *testing.T) {
	pool, _ := newTestPool(t)
	services := db.NewServiceRepository(pool)
	snapshots := db.NewStatusSnapshotRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok"}}

	p := NewPoller(services, services, snapshots, integrations, provider, time.Hour, zap.NewNop())

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
