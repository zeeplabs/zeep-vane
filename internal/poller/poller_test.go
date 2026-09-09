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

// newTestSLOAnalyzer builds an SLOAnalyzer backed by real repositories
// (so a first-poll transition like "not_configured" -> "operational"
// exercises the real HasOpenIncidentForService/UpdateStatusAnalysis
// queries, same as production) and a no-op fake LLM generator - sufficient
// for tests in this file that don't themselves exercise SLOAnalyzer's
// behavior (that's analyzer_test.go's job and this file's own
// TestPoller_PollOnce_HungLLMEnrichment... test below), but still need
// pollService's now-required analyzer field to be non-nil.
func newTestSLOAnalyzer(pool *db.Pool, services *db.ServiceRepository) *SLOAnalyzer {
	incidents := db.NewIncidentRepository(pool)
	return NewSLOAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second, zap.NewNop())
}

// seedTestTenant creates a throwaway tenant so fixtures (services, ...) can
// satisfy the tenant_id NOT NULL constraint 0024 added, and creates svc
// inside a transaction with app.tenant_id set to it (the DEFAULT
// current_setting expression on services.tenant_id only resolves outside
// that transaction's session setting - see internal/db/pool.go's
// BeginTenantTx).
func seedTestTenant(t *testing.T, pool *db.Pool) string {
	t.Helper()
	ctx := context.Background()

	var tenantID string
	if err := pool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "poller-test-tenant").Scan(&tenantID); err != nil {
		t.Fatalf("seeding test tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	return tenantID
}

func createTestService(t *testing.T, pool *db.Pool, services *db.ServiceRepository) db.Service {
	t.Helper()
	ctx := context.Background()
	tenantID := seedTestTenant(t, pool)

	svc := &db.Service{
		Name:  fmt.Sprintf("poller-test-%d", time.Now().UnixNano()),
		SLOID: "slo-poll-1",
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
// iteration against real repositories with Datadog mocked: the cycle
// updates current_status per the fetched error budget/state and opens a
// status interval for the service.
func TestPoller_PollOnce_UpdatesStatusAndPersistsSnapshot(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	svc := createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 91.2, RequestCount: 10},
	}

	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
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

	var intervalCount int
	row := pool.QueryRow(ctx,
		"SELECT count(*) FROM status_intervals WHERE service_id = $1 AND status = $2 AND error_budget_remaining = $3 AND ends_at IS NULL",
		svc.ID, "operational", 91.2,
	)
	if err := row.Scan(&intervalCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if intervalCount != 1 {
		t.Errorf("open status_intervals rows for service = %d, want 1", intervalCount)
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
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	svc := createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	// Seed a known-good status via one successful poll, so the failure that
	// follows has a last-known-valid status to preserve.
	okProvider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 99, RequestCount: 10}}
	seedingPoller := NewPoller(services, services, statusIntervals, integrations, okProvider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
	seedingPoller.pollOnce(ctx)

	backoffBase = time.Millisecond
	failingProvider := &fakeProvider{errs: []error{datadog.ErrTimeout, datadog.ErrTimeout, datadog.ErrTimeout}}
	p := NewPoller(services, services, statusIntervals, integrations, failingProvider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
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

// sloKeyedFakeProvider returns a fixed result or error per SLO ID,
// independent across services - unlike fakeProvider, whose single
// call-indexed sequence can't represent one service succeeding while
// another fails within the same pollOnce cycle.
type sloKeyedFakeProvider struct {
	statuses map[string]datadog.SLOStatus
	errs     map[string]error
}

func (f *sloKeyedFakeProvider) FetchSLOStatus(ctx context.Context, sloID string, from, to time.Time) (datadog.SLOStatus, error) {
	if err, ok := f.errs[sloID]; ok {
		return datadog.SLOStatus{}, err
	}
	return f.statuses[sloID], nil
}

func createTestServiceWithSLO(t *testing.T, pool *db.Pool, services *db.ServiceRepository, sloID string) db.Service {
	t.Helper()
	ctx := context.Background()
	tenantID := seedTestTenant(t, pool)

	svc := &db.Service{
		Name:  fmt.Sprintf("poller-test-%d", time.Now().UnixNano()),
		SLOID: sloID,
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
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM services WHERE id = $1", svc.ID) })

	return *svc
}

// TestPoller_PollOnce_Success_MarksIntegrationChecked is the H5 regression
// guard: a successful cycle must record that success on the Datadog
// integration - previously nothing ever did, so an integration once marked
// invalid stayed invalid forever, even after the poller recovered.
func TestPoller_PollOnce_Success_MarksIntegrationChecked(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 100}}
	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
	p.pollOnce(ctx)

	integration, err := integrations.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "active" {
		t.Errorf("Status = %q, want %q", integration.Status, "active")
	}
	if integration.LastCheckedAt == nil {
		t.Error("LastCheckedAt is nil, want a timestamp set by a successful poll")
	}
}

// TestPoller_PollOnce_RecoversFromInvalid_AfterSubsequentSuccess is the
// other half of H5: a cycle that succeeds after a prior cycle failed must
// clear the invalid state and the recorded error, not leave the admin
// looking at a stale failure the poller has already recovered from.
func TestPoller_PollOnce_RecoversFromInvalid_AfterSubsequentSuccess(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	if err := integrations.MarkDatadogInvalid(ctx, "seeded failure"); err != nil {
		t.Fatalf("setup MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 100}}
	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
	p.pollOnce(ctx)

	integration, err := integrations.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "active" {
		t.Errorf("Status = %q, want %q (must clear the seeded invalid state)", integration.Status, "active")
	}
	if integration.LastError != nil {
		t.Errorf("LastError = %q, want nil (must clear the seeded failure reason)", *integration.LastError)
	}
}

// TestPoller_PollOnce_OneOfTwoServicesFails_IntegrationStaysActive is the H6
// regression guard: a single misconfigured SLO among several reachable
// services must not mark the whole Datadog integration invalid - that would
// hide every other service's real status behind one bad SLO ID.
func TestPoller_PollOnce_OneOfTwoServicesFails_IntegrationStaysActive(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	createTestIntegration(t, pool, dsn, integrations)

	okSvc := createTestServiceWithSLO(t, pool, services, "slo-ok")
	badSvc := createTestServiceWithSLO(t, pool, services, "slo-not-found")

	backoffBase = time.Millisecond
	provider := &sloKeyedFakeProvider{
		statuses: map[string]datadog.SLOStatus{"slo-ok": {State: "ok", ErrorBudgetRemaining: 100, RequestCount: 10}},
		errs:     map[string]error{"slo-not-found": datadog.ErrTimeout},
	}
	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())
	p.pollOnce(ctx)

	integration, err := integrations.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "active" {
		t.Errorf("Status = %q, want %q (one bad SLO must not invalidate the whole integration)", integration.Status, "active")
	}

	all, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	var foundOK, foundBad *db.Service
	for i := range all {
		switch all[i].ID {
		case okSvc.ID:
			foundOK = &all[i]
		case badSvc.ID:
			foundBad = &all[i]
		}
	}
	if foundOK == nil {
		t.Fatalf("service %s not found after pollOnce", okSvc.ID)
	}
	if foundOK.CurrentStatus != "operational" {
		t.Errorf("ok service CurrentStatus = %q, want %q", foundOK.CurrentStatus, "operational")
	}
	if foundBad == nil {
		t.Fatalf("service %s not found after pollOnce", badSvc.ID)
	}
	if foundBad.CurrentStatus == "operational" {
		t.Errorf("bad service CurrentStatus = %q, want unchanged from its default (never successfully fetched)", foundBad.CurrentStatus)
	}
}

// TestPoller_Run_StopsOnContextCancel confirms the ticker loop exits
// cleanly when its context is canceled, so cmd/vane serve (T25) can shut it
// down without a goroutine leak.
func TestPoller_Run_StopsOnContextCancel(t *testing.T) {
	pool, _ := newTestPool(t)
	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok"}}

	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())

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

// TestPoller_Run_PollsImmediatelyBeforeFirstTick is the M17 regression
// guard: Run must not wait a full interval for time.NewTicker's first tick
// before fetching status - an admin who just connected Datadog watches the
// dashboard update immediately, not after a silent up-to-interval gap.
func TestPoller_Run_PollsImmediatelyBeforeFirstTick(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	svc := createTestService(t, pool, services)
	createTestIntegration(t, pool, dsn, integrations)

	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", ErrorBudgetRemaining: 100, RequestCount: 10}}
	// A long interval - if Run waited for the ticker's first tick instead
	// of polling immediately, this test would time out waiting for a
	// status that shouldn't take an hour to appear.
	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, newTestSLOAnalyzer(pool, services), zap.NewNop())

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		p.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		all, err := services.List(ctx)
		if err != nil {
			t.Fatalf("List() returned unexpected error: %v", err)
		}
		for i := range all {
			if all[i].ID == svc.ID && all[i].CurrentStatus == "operational" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("service CurrentStatus never reached operational within 2s of Run() starting - want an immediate poll, not a wait for the 1h ticker's first tick")
}

// TestPoller_PollOnce_HungLLMEnrichment_DoesNotDelayRemainingServices is
// the design.md Risks-section-mandated regression guard: SLOAnalyzer's
// async LLM enrichment must never be able to delay pollOnce's synchronous
// loop over the remaining services in the same cycle, no matter how long
// the underlying provider call hangs. One service (degradingSvc)
// transitions into "degraded" this cycle, which dispatches an async
// enrichment goroutine against a fake LLM generator that blocks until its
// context is canceled (fakeLLMGenerator{block: true}, from
// analyzer_test.go) - simulating a provider call that never completes on
// its own. A second service (okSvc) transitions to "operational", which
// makes no LLM call at all. pollOnce must still finish processing both
// well within a bound far tighter than the analyzer's own timeout.
func TestPoller_PollOnce_HungLLMEnrichment_DoesNotDelayRemainingServices(t *testing.T) {
	pool, dsn := newTestPool(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	statusIntervals := db.NewStatusIntervalRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	createTestIntegration(t, pool, dsn, integrations)

	degradingSvc := createTestServiceWithSLO(t, pool, services, fmt.Sprintf("hung-llm-degraded-%d", time.Now().UnixNano()))
	okSvc := createTestServiceWithSLO(t, pool, services, fmt.Sprintf("hung-llm-ok-%d", time.Now().UnixNano()))

	provider := &sloKeyedFakeProvider{
		statuses: map[string]datadog.SLOStatus{
			degradingSvc.SLOID: {State: "warning", ErrorBudgetRemaining: 5, RequestCount: 10},
			okSvc.SLOID:        {State: "ok", ErrorBudgetRemaining: 100, RequestCount: 10},
		},
	}

	// A long analyzer timeout (well beyond this test's own assertion bound
	// below) - if the hang somehow did propagate into pollOnce, this test
	// would time out waiting for pollOnce to return, not just fail an
	// assertion.
	analyzer := NewSLOAnalyzer(incidents, services, &fakeLLMGenerator{block: true}, 10*time.Second, zap.NewNop())
	p := NewPoller(services, services, statusIntervals, integrations, provider, time.Hour, analyzer, zap.NewNop())

	start := time.Now()
	p.pollOnce(ctx)
	elapsed := time.Since(start)

	const bound = 2 * time.Second
	if elapsed > bound {
		t.Fatalf("pollOnce() took %v, want under %v (a hung LLM enrichment call must not delay the poll cycle)", elapsed, bound)
	}

	all, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	var foundDegrading, foundOK *db.Service
	for i := range all {
		switch all[i].ID {
		case degradingSvc.ID:
			foundDegrading = &all[i]
		case okSvc.ID:
			foundOK = &all[i]
		}
	}
	if foundDegrading == nil {
		t.Fatalf("service %s not found after pollOnce", degradingSvc.ID)
	}
	if foundDegrading.CurrentStatus != "degraded" {
		t.Errorf("degrading service CurrentStatus = %q, want %q (must still complete despite the hung enrichment it triggered)", foundDegrading.CurrentStatus, "degraded")
	}
	if foundOK == nil {
		t.Fatalf("service %s not found after pollOnce", okSvc.ID)
	}
	if foundOK.CurrentStatus != "operational" {
		t.Errorf("ok service CurrentStatus = %q, want %q (must not be delayed by the other service's hung enrichment)", foundOK.CurrentStatus, "operational")
	}
}
