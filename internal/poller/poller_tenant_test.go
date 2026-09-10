package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeTenantLister is a tenantLister double returning a fixed list of
// tenants, or an error if tenantsErr is set.
type fakeTenantLister struct {
	tenants    []db.Tenant
	tenantsErr error
}

func (f *fakeTenantLister) List(ctx context.Context) ([]db.Tenant, error) {
	if f.tenantsErr != nil {
		return nil, f.tenantsErr
	}
	return f.tenants, nil
}

// fakeTenantTxKey is the context key fakeTenantTx uses to mark which tenant
// a context was scoped to - fakeServiceListerByTenant reads it back to
// return that tenant's own services, proving pollOnce actually ran under
// the tenant-scoped context pollCycle built, not some ambient one.
type fakeTenantTxKey struct{}

// fakeTenantTx builds a TenantTxFunc double: it never touches a real
// database, just tags the returned context with tenantID (via
// fakeTenantTxKey) and records every commit/rollback call it receives, so
// tests can assert pollCycle opened, closed, and used exactly the
// transactions it should have per tenant.
type fakeTenantTx struct {
	beginErrForTenant  map[string]error
	commitErrForTenant map[string]error

	beginCalls    []string
	commitCalls   []string
	rollbackCalls []string
}

func (f *fakeTenantTx) fn(ctx context.Context, tenantID string) (context.Context, func(context.Context) error, func(context.Context), error) {
	f.beginCalls = append(f.beginCalls, tenantID)
	if err := f.beginErrForTenant[tenantID]; err != nil {
		return ctx, nil, nil, err
	}

	tenantCtx := context.WithValue(ctx, fakeTenantTxKey{}, tenantID)
	commit := func(context.Context) error {
		f.commitCalls = append(f.commitCalls, tenantID)
		return f.commitErrForTenant[tenantID]
	}
	rollback := func(context.Context) {
		f.rollbackCalls = append(f.rollbackCalls, tenantID)
	}
	return tenantCtx, commit, rollback, nil
}

// fakeServiceListerByTenant returns a different fixed list of services per
// tenant, keyed by the tenant id fakeTenantTx tagged the context with -
// this is what proves pollOnce for tenant A never sees tenant B's services.
type fakeServiceListerByTenant struct {
	servicesByTenant map[string][]db.Service
}

func (f *fakeServiceListerByTenant) List(ctx context.Context) ([]db.Service, error) {
	tenantID, _ := ctx.Value(fakeTenantTxKey{}).(string)
	return f.servicesByTenant[tenantID], nil
}

// fakeIntegrationUpdater is an integrationStatusUpdater double recording
// how many times each method was called.
type fakeIntegrationUpdater struct {
	invalidCalls int
	checkedCalls int
}

func (f *fakeIntegrationUpdater) MarkDatadogInvalid(ctx context.Context, lastError string) error {
	f.invalidCalls++
	return nil
}

func (f *fakeIntegrationUpdater) MarkDatadogChecked(ctx context.Context) error {
	f.checkedCalls++
	return nil
}

func newTenantIterationPoller(t *testing.T, tenants tenantLister, tenantTx TenantTxFunc, services serviceLister, statuses serviceStatusUpdater, intervals statusIntervalWriter, integrations integrationStatusUpdater) *Poller {
	t.Helper()
	analyzer := NewSLOAnalyzer(&fakeIncidentStore{openIncidents: map[string]string{}}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second, zap.NewNop())
	p := &Poller{
		services:        services,
		statuses:        statuses,
		statusIntervals: intervals,
		integrations:    integrations,
		provider:        &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", RequestCount: 10}},
		interval:        time.Hour,
		analyzer:        analyzer,
		logger:          zap.NewNop(),
		breachStreak:    make(map[string]int),
	}
	p.EnableTenantIteration(tenants, tenantTx)
	return p
}

// TestPollCycle_TenantIterationEnabled_ProcessesEachTenantInItsOwnContext
// is T15's core assertion: pollCycle enumerates every tenant tenants.List
// returns and calls pollOnce once per tenant, each inside that tenant's own
// tenant-scoped context (never a shared/ambient one) - proven here by each
// tenant's service list being visible only under its own tagged context.
func TestPollCycle_TenantIterationEnabled_ProcessesEachTenantInItsOwnContext(t *testing.T) {
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-a"}, {ID: "tenant-b"}}}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	services := &fakeServiceListerByTenant{servicesByTenant: map[string][]db.Service{
		"tenant-a": {{ID: "svc-a1", SLOID: "slo-a1", CurrentStatus: "not_configured"}},
		"tenant-b": {{ID: "svc-b1", SLOID: "slo-b1", CurrentStatus: "not_configured"}},
	}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	integrations := &fakeIntegrationUpdater{}

	p := newTenantIterationPoller(t, tenants, tx.fn, services, statuses, intervals, integrations)
	p.pollCycle(context.Background())

	if len(tx.beginCalls) != 2 || tx.beginCalls[0] != "tenant-a" || tx.beginCalls[1] != "tenant-b" {
		t.Fatalf("beginCalls = %v, want [tenant-a tenant-b]", tx.beginCalls)
	}
	if len(tx.commitCalls) != 2 {
		t.Fatalf("commitCalls = %v, want one commit per tenant", tx.commitCalls)
	}
	if len(tx.rollbackCalls) != 0 {
		t.Errorf("rollbackCalls = %v, want none (both commits succeeded)", tx.rollbackCalls)
	}

	if len(statuses.calls) != 2 {
		t.Fatalf("UpdateStatus calls = %+v, want exactly 2 (one per tenant's own service)", statuses.calls)
	}
	gotServiceIDs := map[string]bool{statuses.calls[0].serviceID: true, statuses.calls[1].serviceID: true}
	if !gotServiceIDs["svc-a1"] || !gotServiceIDs["svc-b1"] {
		t.Errorf("UpdateStatus calls = %+v, want svc-a1 and svc-b1 (each tenant's own service, not the other's)", statuses.calls)
	}
}

// TestPollCycle_TenantWithZeroServices_SkippedWithoutError proves a tenant
// with no configured services doesn't error the cycle - pollOnce's own
// empty-list loop is a no-op, and the tenant's transaction still opens and
// commits cleanly.
func TestPollCycle_TenantWithZeroServices_SkippedWithoutError(t *testing.T) {
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-empty"}}}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	services := &fakeServiceListerByTenant{servicesByTenant: map[string][]db.Service{}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	integrations := &fakeIntegrationUpdater{}

	p := newTenantIterationPoller(t, tenants, tx.fn, services, statuses, intervals, integrations)
	p.pollCycle(context.Background())

	if len(tx.commitCalls) != 1 {
		t.Fatalf("commitCalls = %v, want exactly 1 (the empty tenant's transaction still commits)", tx.commitCalls)
	}
	if len(statuses.calls) != 0 {
		t.Errorf("UpdateStatus calls = %+v, want none for a tenant with zero configured services", statuses.calls)
	}
	if integrations.invalidCalls != 0 || integrations.checkedCalls != 0 {
		t.Errorf("integration status calls (invalid=%d, checked=%d), want none for a tenant with zero services", integrations.invalidCalls, integrations.checkedCalls)
	}
}

// TestPollCycle_TenantListError_NoTenantProcessed proves a failure listing
// tenants aborts the whole cycle cleanly (logged, not panicking), touching
// no tenant transaction at all.
func TestPollCycle_TenantListError_NoTenantProcessed(t *testing.T) {
	tenants := &fakeTenantLister{tenantsErr: errors.New("list tenants: boom")}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	services := &fakeServiceListerByTenant{servicesByTenant: map[string][]db.Service{}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	integrations := &fakeIntegrationUpdater{}

	p := newTenantIterationPoller(t, tenants, tx.fn, services, statuses, intervals, integrations)
	p.pollCycle(context.Background())

	if len(tx.beginCalls) != 0 {
		t.Errorf("beginCalls = %v, want none when listing tenants fails", tx.beginCalls)
	}
}

// TestPollCycle_OneTenantBeginTxFails_OtherTenantsStillProcessed proves a
// single tenant's transaction failure doesn't abort the whole cycle - the
// remaining tenants still get processed.
func TestPollCycle_OneTenantBeginTxFails_OtherTenantsStillProcessed(t *testing.T) {
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-broken"}, {ID: "tenant-ok"}}}
	tx := &fakeTenantTx{
		beginErrForTenant:  map[string]error{"tenant-broken": errors.New("begin tx: boom")},
		commitErrForTenant: map[string]error{},
	}
	services := &fakeServiceListerByTenant{servicesByTenant: map[string][]db.Service{
		"tenant-ok": {{ID: "svc-ok", SLOID: "slo-ok", CurrentStatus: "not_configured"}},
	}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	integrations := &fakeIntegrationUpdater{}

	p := newTenantIterationPoller(t, tenants, tx.fn, services, statuses, intervals, integrations)
	p.pollCycle(context.Background())

	if len(tx.commitCalls) != 1 || tx.commitCalls[0] != "tenant-ok" {
		t.Fatalf("commitCalls = %v, want exactly [tenant-ok]", tx.commitCalls)
	}
	if len(statuses.calls) != 1 || statuses.calls[0].serviceID != "svc-ok" {
		t.Errorf("UpdateStatus calls = %+v, want exactly svc-ok (tenant-broken's own failure must not block tenant-ok)", statuses.calls)
	}
}

// TestPollCycle_TenantIterationDisabled_FallsBackToAmbientPollOnce proves
// backward compatibility: a Poller built without EnableTenantIteration
// (every existing NewPoller caller) behaves exactly as before - a single
// pollOnce call against the context pollCycle was given, no tenant
// enumeration at all.
func TestPollCycle_TenantIterationDisabled_FallsBackToAmbientPollOnce(t *testing.T) {
	services := &fakeServiceListerByTenant{servicesByTenant: map[string][]db.Service{
		"": {{ID: "svc-ambient", SLOID: "slo-ambient", CurrentStatus: "not_configured"}},
	}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	integrations := &fakeIntegrationUpdater{}
	analyzer := NewSLOAnalyzer(&fakeIncidentStore{openIncidents: map[string]string{}}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second, zap.NewNop())

	p := NewPoller(services, statuses, intervals, integrations,
		&fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok", RequestCount: 10}},
		time.Hour, analyzer, zap.NewNop())

	p.pollCycle(context.Background())

	if len(statuses.calls) != 1 || statuses.calls[0].serviceID != "svc-ambient" {
		t.Errorf("UpdateStatus calls = %+v, want exactly svc-ambient (unchanged ambient-context behavior)", statuses.calls)
	}
}
