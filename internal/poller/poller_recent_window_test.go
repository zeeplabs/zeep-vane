package poller

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeIntervalWriter records every OpenOrExtend call it receives.
type fakeIntervalWriter struct {
	calls []struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}
}

func (f *fakeIntervalWriter) OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) error {
	f.calls = append(f.calls, struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}{serviceID, status, errorBudgetRemaining, at})
	return nil
}

// fakeStatusUpdater records every UpdateStatus call it receives.
type fakeStatusUpdater struct {
	calls []struct {
		serviceID, status string
	}
}

func (f *fakeStatusUpdater) UpdateStatus(ctx context.Context, serviceID, status string) error {
	f.calls = append(f.calls, struct{ serviceID, status string }{serviceID, status})
	return nil
}

func newTestPoller(provider datadog.SLOProvider, interval time.Duration, intervals *fakeIntervalWriter, statuses *fakeStatusUpdater) *Poller {
	return &Poller{
		statuses:        statuses,
		statusIntervals: intervals,
		provider:        provider,
		interval:        interval,
		logger:          zap.NewNop(),
		breachStreak:    make(map[string]int),
	}
}

func TestPollService_WindowPassedToProvider_FixedWidthLaggedBehindNow(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 10},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	// interval is deliberately not 5m/1m here: the window's width and lag
	// come from recentWindowWidth/recentWindowLag, independent of
	// p.interval (AD-019 addendum) - a mismatched interval must not change
	// the window bounds.
	p := newTestPoller(provider, 2*time.Minute, intervals, statuses)

	before := time.Now()
	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}
	after := time.Now()

	if len(provider.gotFrom) != 1 {
		t.Fatalf("provider received %d calls, want 1", len(provider.gotFrom))
	}
	gotFrom, gotTo := provider.gotFrom[0], provider.gotTo[0]

	wantToMin, wantToMax := before.Add(-recentWindowLag), after.Add(-recentWindowLag)
	if gotTo.Before(wantToMin) || gotTo.After(wantToMax) {
		t.Errorf("to = %v, want within [%v, %v] (now - recentWindowLag)", gotTo, wantToMin, wantToMax)
	}
	wantFrom := gotTo.Add(-recentWindowWidth)
	if !gotFrom.Equal(wantFrom) {
		t.Errorf("from = %v, want %v (to - recentWindowWidth)", gotFrom, wantFrom)
	}
}

func TestPollService_SufficientVolume_RecomputesFromRecoveredState(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 10},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "outage"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want a single call with status %q", statuses.calls, "operational")
	}
	if len(intervals.calls) != 1 || intervals.calls[0].status != "operational" {
		t.Errorf("OpenOrExtend calls = %+v, want a single call with status %q", intervals.calls, "operational")
	}
}

func TestPollService_LowVolumeNoisyBreach_CarriesForwardPreviousStatus(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "breached", RequestCount: 3},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want carry-forward of %q", statuses.calls, "operational")
	}
	if len(intervals.calls) != 1 || intervals.calls[0].status != "operational" {
		t.Errorf("OpenOrExtend calls = %+v, want carry-forward of %q", intervals.calls, "operational")
	}
}

func TestPollService_LowVolumeCarryForward_StillInvokesWriters(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 0},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "degraded"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(intervals.calls) != 1 {
		t.Fatalf("OpenOrExtend calls = %d, want 1 (carry-forward still writes the interval)", len(intervals.calls))
	}
	if len(statuses.calls) != 1 {
		t.Fatalf("UpdateStatus calls = %d, want 1 (carry-forward still updates cached status)", len(statuses.calls))
	}
}

func TestPollService_SingleBreachedWindow_CarriesForwardInsteadOfOutage(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "breached", RequestCount: 5000},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want a single breached window to carry forward %q, not flip to outage", statuses.calls, "operational")
	}
}

func TestPollService_TwoConsecutiveBreachedWindows_FlipsToOutage(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil, nil},
		status: datadog.SLOStatus{State: "breached", RequestCount: 5000},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)
	svc := db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}

	if err := p.pollService(t.Context(), svc); err != nil {
		t.Fatalf("pollService() call 1 returned unexpected error: %v", err)
	}
	if err := p.pollService(t.Context(), svc); err != nil {
		t.Fatalf("pollService() call 2 returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 2 || statuses.calls[1].status != "outage" {
		t.Errorf("UpdateStatus calls = %+v, want the 2nd consecutive breached cycle to flip to %q", statuses.calls, "outage")
	}
}

func TestPollService_BreachStreak_ResetByAnIntermediateOKWindow(t *testing.T) {
	provider := &fakeProvider{
		errs: []error{nil, nil, nil},
		statuses: []datadog.SLOStatus{
			{State: "breached", RequestCount: 5000},
			{State: "ok", RequestCount: 5000},
			{State: "breached", RequestCount: 5000},
		},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)
	svc := db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}

	for i := 0; i < 3; i++ {
		if err := p.pollService(t.Context(), svc); err != nil {
			t.Fatalf("pollService() call %d returned unexpected error: %v", i+1, err)
		}
	}

	if len(statuses.calls) != 3 || statuses.calls[2].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want breach streak reset by the intermediate ok window, 3rd call still carrying forward %q", statuses.calls, "operational")
	}
}

func TestPollService_FirstPollLowVolume_StaysNotConfigured(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 1},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "not_configured" {
		t.Errorf("UpdateStatus calls = %+v, want carry-forward of %q, no special-cased default", statuses.calls, "not_configured")
	}
}
