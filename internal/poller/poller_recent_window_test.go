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
	analyzer := NewSLOAnalyzer(&fakeIncidentStore{openIncidents: map[string]string{}}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second, zap.NewNop())
	return &Poller{
		statuses:        statuses,
		statusIntervals: intervals,
		provider:        provider,
		interval:        interval,
		analyzer:        analyzer,
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

// TestPollService_FirstPollLowVolume_ClassifiesInsteadOfStayingNotConfigured
// is the SLOTRAF-01 root-fix test: a service still on the "not_configured"
// seed gets a real first classification even when RequestCount is far below
// minRecentWindowRequests, since any real reading beats a placeholder
// backed by zero data. Target is left at its zero value here so the
// pre-existing Target<=0 branch (classifyByState) is what actually
// classifies - that branch already ignored the traffic floor before this
// fix (spec.md Assumptions), so this test's outcome changes only because
// the low-volume case above it no longer intercepts a not_configured
// service first.
func TestPollService_FirstPollLowVolume_ClassifiesInsteadOfStayingNotConfigured(t *testing.T) {
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

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want %q (first-ever classification, no more carry-forward of the not_configured seed)", statuses.calls, "operational")
	}
}

// TestPollService_FirstPollLowVolume_UsableTarget_ClassifiesViaBreachBound
// covers the same SLOTRAF-01 scenario through the breachBound branch
// instead of classifyByState: a usable Target means the window-rescaled
// comparison runs, not the Target<=0 fallback.
func TestPollService_FirstPollLowVolume_UsableTarget_ClassifiesViaBreachBound(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", SLI: 99.9, Target: 99.5, RequestCount: 3},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want %q (usable target, healthy SLI, first-ever classification despite low RequestCount)", statuses.calls, "operational")
	}
}

// TestPollService_LowVolumeAfterFirstClassification_StillCarriesForward is
// the SLOTRAF-02 regression guard: once a service has left not_configured,
// the traffic floor applies exactly as before - a low-volume cycle carries
// the already-classified status forward, it does not reclassify.
func TestPollService_LowVolumeAfterFirstClassification_StillCarriesForward(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "breached", RequestCount: 1},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want carry-forward of %q, no reclassification once already classified", statuses.calls, "operational")
	}
}

// TestPollService_HighVolumeBreachedStateButSLIWithinBand_DegradesNotOutage
// is the root-fix test (spec BTR-03): Datadog reports "breached" against the
// SLO's 30-day target, but the window's SLI (99.4) sits above the
// window-rescaled bound (~99.29), so it must be "degraded", never "outage".
func TestPollService_HighVolumeBreachedStateButSLIWithinBand_DegradesNotOutage(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "breached", SLI: 99.4, Target: 99.5, RequestCount: 10000},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "degraded" {
		t.Errorf("UpdateStatus calls = %+v, want %q (below target but inside the sampling band)", statuses.calls, "degraded")
	}
}

// TestPollService_HighVolumeOKStateButSLIBelowTarget_Degrades covers the
// SLI<Target half of the below-target-within-band branch (spec BTR-03) even
// when Datadog's own state says "ok".
func TestPollService_HighVolumeOKStateButSLIBelowTarget_Degrades(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", SLI: 99.4, Target: 99.5, RequestCount: 10000},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "degraded" {
		t.Errorf("UpdateStatus calls = %+v, want %q (SLI below the SLO target)", statuses.calls, "degraded")
	}
}

// TestPollService_HighVolumeHealthyWindow_StaysOperational covers the
// bound-path operational case (spec BTR-04): SLI at/above target, state ok.
func TestPollService_HighVolumeHealthyWindow_StaysOperational(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", SLI: 99.9, Target: 99.5, RequestCount: 10000},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "outage"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want %q", statuses.calls, "operational")
	}
}

// TestPollService_HighVolumeRealBreach_HysteresisThenOutage covers the
// bound-driven breach feeding the retained hysteresis (spec BTR-02, BTR-06,
// BTR-07): SLI far below the bound carries forward once, then flips.
func TestPollService_HighVolumeRealBreach_HysteresisThenOutage(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil, nil},
		status: datadog.SLOStatus{State: "breached", SLI: 50, Target: 99.5, RequestCount: 10000},
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

	if len(statuses.calls) != 2 || statuses.calls[0].status != "operational" || statuses.calls[1].status != "outage" {
		t.Errorf("UpdateStatus calls = %+v, want first cycle carry-forward %q then %q", statuses.calls, "operational", "outage")
	}
}

// TestPollService_WithinBandWindowResetsBreachStreak covers spec BTR-08: a
// window that is below target but inside the band must reset the streak, so
// a later breach does not flip on its own.
func TestPollService_WithinBandWindowResetsBreachStreak(t *testing.T) {
	provider := &fakeProvider{
		errs: []error{nil, nil, nil},
		statuses: []datadog.SLOStatus{
			{State: "breached", SLI: 50, Target: 99.5, RequestCount: 10000},
			{State: "breached", SLI: 99.4, Target: 99.5, RequestCount: 10000},
			{State: "breached", SLI: 50, Target: 99.5, RequestCount: 10000},
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

	if len(statuses.calls) != 3 || statuses.calls[1].status != "degraded" || statuses.calls[2].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want call 2 %q and call 3 carry-forward %q (streak reset)", statuses.calls, "degraded", "operational")
	}
}

// TestPollService_NoTarget_FallsBackToStateClassification covers spec
// BTR-09: with no usable target the old state-based classification is
// preserved, including its hysteresis.
func TestPollService_NoTarget_FallsBackToStateClassification(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil, nil},
		status: datadog.SLOStatus{State: "breached", SLI: 50, Target: 0, RequestCount: 5000},
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

	if len(statuses.calls) != 2 || statuses.calls[0].status != "operational" || statuses.calls[1].status != "outage" {
		t.Errorf("UpdateStatus calls = %+v, want fallback carry-forward %q then %q", statuses.calls, "operational", "outage")
	}
}
