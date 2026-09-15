package poller

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakePollingServiceLister is a pollingServiceLister double, keyed by
// tenant like fakeServiceListerByTenant (poller_tenant_test.go) - it reads
// the tenant id fakeTenantTx tagged the context with, proving reconcile
// only ever sees each tenant's own polling-manual services.
type fakePollingServiceLister struct {
	servicesByTenant map[string][]db.Service
	err              error
}

func (f *fakePollingServiceLister) ListPollingManual(ctx context.Context) ([]db.Service, error) {
	if f.err != nil {
		return nil, f.err
	}
	tenantID, _ := ctx.Value(fakeTenantTxKey{}).(string)
	return f.servicesByTenant[tenantID], nil
}

func newTestManualScheduler(services pollingServiceLister, statuses serviceStatusUpdater, intervals statusIntervalWriter, tenants tenantLister, tenantTx TenantTxFunc) *ManualScheduler {
	return NewManualScheduler(services, statuses, intervals, tenants, tenantTx, zap.NewNop())
}

// newLocalListener opens a raw TCP listener on IPv6 loopback ([::1]) - not
// 127.0.0.1, which internal/checks' SSRF-safe dialer blocks (127.0.0.0/8),
// so a "real, reachable" target in these tests must use an address the
// dialer does not block. A dedicated test below specifically exercises the
// blocked-range case using a literal 127.0.0.1 target.
func newLocalListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("could not listen on [::1] in this environment: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

// acceptAndClose runs an Accept loop that immediately closes every
// connection it receives, until the listener itself is closed.
func acceptAndClose(listener net.Listener) {
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
}

// TestManualScheduler_RunCheck_SuccessWritesOperationalAndResetsStreak
// asserts MP-06/MP-12: a successful check writes "operational" via
// UpdateStatus, opens/extends a status_intervals row (errorBudgetRemaining
// 0, same two calls the Datadog poller's own pollService makes), and
// resets the failure streak to 0.
func TestManualScheduler_RunCheck_SuccessWritesOperationalAndResetsStreak(t *testing.T) {
	listener := newLocalListener(t)
	acceptAndClose(listener)

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	s := newTestManualScheduler(&fakePollingServiceLister{}, statuses, intervals, &fakeTenantLister{}, tx.fn)

	failureStreak := 3 // simulate a prior streak - success must reset it
	got := s.runCheck(context.Background(), "tenant-a", "svc-1", "tcp", listener.Addr().String(), "outage", &failureStreak)

	if got != "operational" {
		t.Errorf("runCheck() = %q, want %q", got, "operational")
	}
	if failureStreak != 0 {
		t.Errorf("failureStreak after success = %d, want 0", failureStreak)
	}
	if len(statuses.calls) != 1 || statuses.calls[0].serviceID != "svc-1" || statuses.calls[0].status != "operational" {
		t.Fatalf("UpdateStatus calls = %+v, want exactly one (svc-1, operational)", statuses.calls)
	}
	if len(intervals.calls) != 1 || intervals.calls[0].serviceID != "svc-1" || intervals.calls[0].status != "operational" || intervals.calls[0].errorBudget != 0 {
		t.Fatalf("OpenOrExtend calls = %+v, want exactly one (svc-1, operational, errorBudget=0)", intervals.calls)
	}
}

// TestManualScheduler_RunCheck_OneFailureCarriesStatusForward asserts
// MP-10: a single failed check (failureStreak below manualFailureThreshold)
// does not change the recorded status - the previous status is carried
// forward and persisted as-is.
func TestManualScheduler_RunCheck_OneFailureCarriesStatusForward(t *testing.T) {
	listener := newLocalListener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() returned unexpected error: %v", err)
	}

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	s := newTestManualScheduler(&fakePollingServiceLister{}, statuses, intervals, &fakeTenantLister{}, tx.fn)

	failureStreak := 0
	got := s.runCheck(context.Background(), "tenant-a", "svc-1", "tcp", addr, "operational", &failureStreak)

	if got != "operational" {
		t.Errorf("runCheck() after 1 failure = %q, want %q (carried forward)", got, "operational")
	}
	if failureStreak != 1 {
		t.Errorf("failureStreak after 1 failure = %d, want 1", failureStreak)
	}
	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Fatalf("UpdateStatus calls = %+v, want the carried-forward status persisted (not skipped)", statuses.calls)
	}
}

// TestManualScheduler_RunCheck_SecondConsecutiveFailureFlipsToOutage
// asserts MP-06/MP-11: the 2nd consecutive failed check (reaching
// manualFailureThreshold) flips the recorded status to "outage", and a
// subsequent success resets the streak and returns to "operational" -
// spec.md Edge Cases: the failure reason (here, two different failing
// targets) never resets or affects the streak.
func TestManualScheduler_RunCheck_SecondConsecutiveFailureFlipsToOutage(t *testing.T) {
	closedListener := newLocalListener(t)
	closedAddr := closedListener.Addr().String()
	if err := closedListener.Close(); err != nil {
		t.Fatalf("listener.Close() returned unexpected error: %v", err)
	}

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	s := newTestManualScheduler(&fakePollingServiceLister{}, statuses, intervals, &fakeTenantLister{}, tx.fn)

	failureStreak := 1 // simulate the first failure already having happened
	got := s.runCheck(context.Background(), "tenant-a", "svc-1", "tcp", closedAddr, "operational", &failureStreak)

	if got != "outage" {
		t.Errorf("runCheck() at 2nd consecutive failure = %q, want %q", got, "outage")
	}
	if failureStreak != 2 {
		t.Errorf("failureStreak = %d, want 2", failureStreak)
	}

	// A success afterwards resets the streak and returns to operational.
	openListener := newLocalListener(t)
	acceptAndClose(openListener)
	got = s.runCheck(context.Background(), "tenant-a", "svc-1", "tcp", openListener.Addr().String(), got, &failureStreak)
	if got != "operational" {
		t.Errorf("runCheck() after recovery = %q, want %q", got, "operational")
	}
	if failureStreak != 0 {
		t.Errorf("failureStreak after recovery = %d, want 0", failureStreak)
	}

	if len(statuses.calls) != 2 || statuses.calls[0].status != "outage" || statuses.calls[1].status != "operational" {
		t.Fatalf("UpdateStatus calls = %+v, want [outage, operational] in order", statuses.calls)
	}
}

// TestManualScheduler_RunCheck_BlockedRangeTarget_TreatedAsFailedCheck
// asserts MP-15/T6's "Done when": a literal blocked-range target (the
// SSRF-safe dialer's own Control hook rejecting it, proven independently
// in internal/checks) is treated as an ordinary failed check by
// ManualScheduler - no panic, no special-cased status, contributing to the
// consecutive-failure count exactly like any other failure reason.
func TestManualScheduler_RunCheck_BlockedRangeTarget_TreatedAsFailedCheck(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("runCheck panicked on a blocked-range target: %v", r)
		}
	}()

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	s := newTestManualScheduler(&fakePollingServiceLister{}, statuses, intervals, &fakeTenantLister{}, tx.fn)

	failureStreak := 0
	got := s.runCheck(context.Background(), "tenant-a", "svc-1", "tcp", "127.0.0.1:1", "operational", &failureStreak)

	if got != "operational" {
		t.Errorf("runCheck() after 1 blocked-range failure = %q, want %q (carried forward, not a panic/success)", got, "operational")
	}
	if failureStreak != 1 {
		t.Errorf("failureStreak = %d, want 1", failureStreak)
	}
}

// TestManualScheduler_Run_ZeroServices_StartsAndReturnsCleanly asserts
// MP-09: with zero polling-manual services configured, Run starts and
// returns cleanly on ctx cancellation without error (a no-op, not an
// error) - and touches neither UpdateStatus nor OpenOrExtend.
func TestManualScheduler_Run_ZeroServices_StartsAndReturnsCleanly(t *testing.T) {
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-empty"}}}
	services := &fakePollingServiceLister{servicesByTenant: map[string][]db.Service{}}

	s := newTestManualScheduler(services, statuses, intervals, tenants, tx.fn)
	s.discoveryInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of ctx cancellation")
	}

	if len(statuses.calls) != 0 {
		t.Errorf("UpdateStatus calls = %+v, want none with zero polling-manual services", statuses.calls)
	}
	if len(intervals.calls) != 0 {
		t.Errorf("OpenOrExtend calls = %+v, want none with zero polling-manual services", intervals.calls)
	}
}

// TestManualScheduler_Run_DiscoversNewServiceAndChecksItImmediately
// asserts MP-06/MP-07: a polling-manual service present at Run's first
// reconciliation pass gets its first check immediately (the same
// immediate-first-pass-then-tick shape as Poller.Run), well within one
// reconciliation cycle - proven here by a successful UpdateStatus call
// arriving shortly after Run starts, without waiting anywhere near a real
// poll_interval_seconds.
func TestManualScheduler_Run_DiscoversNewServiceAndChecksItImmediately(t *testing.T) {
	listener := newLocalListener(t)
	acceptAndClose(listener)
	addr := listener.Addr().String()

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-a"}}}
	pollType, pollTarget, pollInterval := "tcp", addr, 30
	services := &fakePollingServiceLister{servicesByTenant: map[string][]db.Service{
		"tenant-a": {{
			ID: "svc-discovered", CurrentStatus: "not_configured",
			MonitorMode: "polling", PollType: &pollType, PollTarget: &pollTarget, PollIntervalSeconds: &pollInterval,
		}},
	}}

	s := newTestManualScheduler(services, statuses, intervals, tenants, tx.fn)
	s.discoveryInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	found := false
	for !found {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("no UpdateStatus(svc-discovered, operational) call observed within 2s")
		case <-time.After(10 * time.Millisecond):
			for _, call := range statuses.calls {
				if call.serviceID == "svc-discovered" && call.status == "operational" {
					found = true
					break
				}
			}
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of ctx cancellation")
	}
}

// TestManualScheduler_Run_GoroutineCountReturnsToBaselineAfterCtxCancel
// asserts the design.md Risks & Concerns goroutine-leak guard: after Run's
// ctx is canceled and Run has returned, every per-service goroutine it
// spawned has exited - runtime.NumGoroutine() returns to (approximately)
// its pre-Run baseline. A bounded retry absorbs ordinary scheduler/GC
// noise from unrelated goroutines rather than asserting exact equality.
func TestManualScheduler_Run_GoroutineCountReturnsToBaselineAfterCtxCancel(t *testing.T) {
	listeners := make([]net.Listener, 3)
	servicesByTenant := []db.Service{}
	pollTypeTCP := "tcp"
	pollInterval := 30
	for i := range listeners {
		listeners[i] = newLocalListener(t)
		acceptAndClose(listeners[i])
		addr := listeners[i].Addr().String()
		servicesByTenant = append(servicesByTenant, db.Service{
			ID: "svc-leak-guard-" + addr, CurrentStatus: "not_configured",
			MonitorMode: "polling", PollType: &pollTypeTCP, PollTarget: &addr, PollIntervalSeconds: &pollInterval,
		})
	}

	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	tenants := &fakeTenantLister{tenants: []db.Tenant{{ID: "tenant-a"}}}
	services := &fakePollingServiceLister{servicesByTenant: map[string][]db.Service{"tenant-a": servicesByTenant}}

	s := newTestManualScheduler(services, statuses, intervals, tenants, tx.fn)
	s.discoveryInterval = 10 * time.Millisecond

	runtime.GC()
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	// Let discovery run and spawn goroutines for all 3 services.
	time.Sleep(100 * time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of ctx cancellation")
	}

	var after int
	ok := false
	for i := 0; i < 20; i++ {
		runtime.GC()
		after = runtime.NumGoroutine()
		if after <= baseline+1 { // +1 slack for GC/runtime noise
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		t.Errorf("NumGoroutine() after Run returned = %d, want back near baseline %d (leak suspected)", after, baseline)
	}
}

// TestManualScheduler_Reconcile_TenantListError_LoggedNoPanic asserts
// reconcile's own failure posture mirrors Poller.pollCycle: a tenant-list
// error is logged and the pass is skipped, never a panic.
func TestManualScheduler_Reconcile_TenantListError_LoggedNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("reconcile panicked on a tenant-list error: %v", r)
		}
	}()

	tenants := &fakeTenantLister{tenantsErr: errors.New("list tenants: boom")}
	tx := &fakeTenantTx{beginErrForTenant: map[string]error{}, commitErrForTenant: map[string]error{}}
	services := &fakePollingServiceLister{servicesByTenant: map[string][]db.Service{}}
	statuses := &fakeStatusUpdater{}
	intervals := &fakeIntervalWriter{}

	s := newTestManualScheduler(services, statuses, intervals, tenants, tx.fn)
	var wg sync.WaitGroup
	known := make(map[string]bool)
	s.reconcile(context.Background(), known, &wg)
	wg.Wait()

	if len(tx.beginCalls) != 0 {
		t.Errorf("beginCalls = %v, want none when listing tenants fails", tx.beginCalls)
	}
}
