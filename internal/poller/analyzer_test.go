package poller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// fakeIncidentStore is a no-network, no-DB fake of incidentStore that
// records every call so tests can assert exactly which methods fired.
// Mutex-protected and channel-notified because dispatchOutageEnrichment/
// dispatchClosingCommentEnrichment call SetDescription/
// SetPendingCloseComment from a goroutine (T14) - tests must synchronize
// on that write rather than racing it or sleep-polling for it.
type fakeIncidentStore struct {
	mu            sync.Mutex
	openIncidents map[string]string // serviceID -> incidentID, present means "open"

	createCalls      []*db.Incident
	createServiceIDs [][]string
	createErr        error
	hasOpenErr       error

	setDescriptionCalls []string // incidentID
	setDescriptionTexts []string // description, parallel to setDescriptionCalls
	setDescriptionErr   error
	setDescriptionDone  chan struct{} // signaled once per SetDescription call, if non-nil

	setPendingCommentCalls []string // incidentID
	setPendingCommentErr   error
	setPendingCommentDone  chan struct{} // signaled once per SetPendingCloseComment call, if non-nil
}

func (f *fakeIncidentStore) Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.createCalls = append(f.createCalls, incident)
	f.createServiceIDs = append(f.createServiceIDs, serviceIDs)
	incident.ID = "new-incident-id"
	return nil
}

func (f *fakeIncidentStore) HasOpenIncidentForService(ctx context.Context, serviceID string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasOpenErr != nil {
		return "", false, f.hasOpenErr
	}
	id, ok := f.openIncidents[serviceID]
	return id, ok, nil
}

func (f *fakeIncidentStore) SetDescription(ctx context.Context, incidentID, description string) error {
	f.mu.Lock()
	err := f.setDescriptionErr
	if err == nil {
		f.setDescriptionCalls = append(f.setDescriptionCalls, incidentID)
		f.setDescriptionTexts = append(f.setDescriptionTexts, description)
	}
	done := f.setDescriptionDone
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeIncidentStore) SetPendingCloseComment(ctx context.Context, incidentID, comment string) error {
	f.mu.Lock()
	err := f.setPendingCommentErr
	if err == nil {
		f.setPendingCommentCalls = append(f.setPendingCommentCalls, incidentID)
	}
	done := f.setPendingCommentDone
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeIncidentStore) snapshot() (createCalls int, setDescriptionCalls, setPendingCommentCalls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.createCalls), append([]string(nil), f.setDescriptionCalls...), append([]string(nil), f.setPendingCommentCalls...)
}

// snapshotDescriptionTexts returns the description text passed to each
// SetDescription call, parallel to setDescriptionCalls.
func (f *fakeIncidentStore) snapshotDescriptionTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setDescriptionTexts...)
}

// fakeStatusAnalysisWriter is a no-DB fake of statusAnalysisWriter.
// Mutex-protected and channel-notified for the same reason as
// fakeIncidentStore - dispatchDegradedEnrichment calls
// UpdateStatusAnalysis from a goroutine.
type fakeStatusAnalysisWriter struct {
	mu    sync.Mutex
	calls []*string // nil entry means UpdateStatusAnalysis(ctx, id, nil)
	err   error
	done  chan struct{} // signaled once per call, if non-nil
}

func (f *fakeStatusAnalysisWriter) UpdateStatusAnalysis(ctx context.Context, serviceID string, analysis *string) error {
	f.mu.Lock()
	err := f.err
	if err == nil {
		f.calls = append(f.calls, analysis)
	}
	done := f.done
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeStatusAnalysisWriter) snapshot() []*string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*string(nil), f.calls...)
}

// fakeLLMGenerator is a no-network fake of llmGenerator with a
// per-method controllable result/error/delay, so T14's tests can exercise
// success, failure, and "still running past the caller's return" without
// any real network call.
type fakeLLMGenerator struct {
	degradedResult, outageResult, closingResult string
	degradedErr, outageErr, closingErr          error
	degradedDelay, outageDelay, closingDelay    time.Duration
	// block, when non-nil, makes every Generate* method wait on ctx.Done()
	// instead of returning - used by the timing test to simulate a
	// provider call that never completes on its own.
	block bool
}

func (f *fakeLLMGenerator) GenerateDegradedAnalysis(ctx context.Context, in llm.AnalysisInput) (string, error) {
	return f.run(ctx, f.degradedDelay, f.degradedResult, f.degradedErr)
}

func (f *fakeLLMGenerator) GenerateOutageDescription(ctx context.Context, in llm.AnalysisInput) (string, error) {
	return f.run(ctx, f.outageDelay, f.outageResult, f.outageErr)
}

func (f *fakeLLMGenerator) GenerateClosingComment(ctx context.Context, in llm.AnalysisInput) (string, error) {
	return f.run(ctx, f.closingDelay, f.closingResult, f.closingErr)
}

func (f *fakeLLMGenerator) run(ctx context.Context, delay time.Duration, result string, err error) (string, error) {
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return result, err
}

func newTestAnalyzer(incidents *fakeIncidentStore, services *fakeStatusAnalysisWriter, llmSvc *fakeLLMGenerator, timeout time.Duration) *SLOAnalyzer {
	return NewSLOAnalyzer(incidents, services, llmSvc, timeout, zap.NewNop())
}

// waitOrTimeout waits up to 2s for ch to receive, failing the test on
// timeout - the standard "wait for the async goroutine to finish" pattern
// used throughout this file instead of a fixed sleep.
func waitOrTimeout(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async goroutine to finish")
	}
}

func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "operational", datadog.SLOStatus{})

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
	if len(services.snapshot()) != 0 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 0", len(services.snapshot()))
	}
}

// TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing and
// its outage counterpart below are the defense-in-depth regression tests for
// AI-13/AI-18: unlike the operational->operational no-op case above (inert
// only because no incident exists to duplicate), these fixtures are set up
// so that, if the previousStatus == newStatus guard were ever removed,
// HandleTransition would fall through into dispatchDegradedEnrichment /
// handleOutageTransition and register a call the assertions below would
// catch (verified by temporarily removing the guard: both tests fail
// without it and pass with it restored).
func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "degraded", datadog.SLOStatus{})

	if len(services.snapshot()) != 0 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 0 (no-op guard must prevent re-clearing/re-dispatching for an unchanged degraded status)", len(services.snapshot()))
	}
	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
}

// TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Outage_CallsNothing seeds
// no open incident (openIncidents empty) - without the no-op guard,
// outage->outage would fall through to handleOutageTransition, find no
// existing incident, and call Create, which the assertion below would catch.
func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Outage_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "outage", datadog.SLOStatus{})

	createCalls, setDescriptionCalls, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (no-op guard must prevent creating a duplicate incident for an unchanged outage status)", createCalls)
	}
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0", len(setDescriptionCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_OutageNoExistingIncident_CreatesAutoIncident(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, setDescriptionDone: make(chan struct{}, 1)}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: "A real outage description."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 1 {
		t.Fatalf("Create called %d times, want 1", createCalls)
	}
	created := incidents.createCalls[0]
	if !created.AutoCreated {
		t.Error("AutoCreated = false, want true")
	}
	if created.Description == nil || *created.Description == "" {
		t.Error("Description is nil/empty, want a generic fallback description")
	}
	if len(incidents.createServiceIDs[0]) != 1 || incidents.createServiceIDs[0][0] != "svc-1" {
		t.Errorf("serviceIDs = %v, want [svc-1]", incidents.createServiceIDs[0])
	}

	waitOrTimeout(t, incidents.setDescriptionDone)
	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 1 || setDescriptionCalls[0] != "new-incident-id" {
		t.Errorf("SetDescription calls = %v, want [new-incident-id] (async enrichment must overwrite the generic description)", setDescriptionCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OutageAlreadyOpenIncident_CreatesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (an incident is already open)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_IntoDegraded_ClearsStatusAnalysisSynchronously(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{degradedDelay: 50 * time.Millisecond, degradedResult: "tooltip text"}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})

	// The synchronous clear must be visible immediately, before the async
	// enrichment (delayed 50ms above) has had a chance to run.
	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times synchronously, want 1", len(calls))
	}
	if calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil (synchronous clear)", *calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_AwayFromDegraded_ClearsStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{})

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1", len(calls))
	}
	if calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil", *calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_DegradedToOutage_ClearsStatusAnalysisAndCreatesIncidentOnce(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "outage", datadog.SLOStatus{})

	if len(services.snapshot()) != 1 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 1 (leaving degraded)", len(services.snapshot()))
	}
	createCalls, _, _ := incidents.snapshot()
	if createCalls != 1 {
		t.Errorf("Create called %d times, want 1 (entering outage)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithOpenIncident_DispatchesClosingCommentAsync(t *testing.T) {
	incidents := &fakeIncidentStore{
		openIncidents:         map[string]string{"svc-1": "existing-incident"},
		setPendingCommentDone: make(chan struct{}, 1),
	}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingResult: "Service has recovered."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{})

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (operational transition never creates an incident)", createCalls)
	}

	waitOrTimeout(t, incidents.setPendingCommentDone)
	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 1 || pendingCalls[0] != "existing-incident" {
		t.Errorf("SetPendingCloseComment calls = %v, want [existing-incident]", pendingCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithNoOpenIncident_DoesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{})

	createCalls, _, pendingCalls := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0", len(pendingCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_HasOpenIncidentError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{hasOpenErr: errors.New("db unreachable")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (HasOpenIncidentForService failed)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_CreateError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, createErr: errors.New("insert failed")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	// Must not panic.
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})
}

// --- T14: async enrichment dispatch ---

func TestSLOAnalyzer_DegradedEnrichment_Success_UpdatesStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedResult: "SLO is approaching its error budget limit."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})

	// Two calls: the synchronous clear, then the async write.
	waitOrTimeout(t, services.done)
	waitOrTimeout(t, services.done)

	calls := services.snapshot()
	if len(calls) != 2 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 2 (sync clear + async write)", len(calls))
	}
	if calls[1] == nil || *calls[1] != "SLO is approaching its error budget limit." {
		t.Errorf("second UpdateStatusAnalysis call = %v, want the generated analysis text", calls[1])
	}
}

func TestSLOAnalyzer_DegradedEnrichment_Failure_LeavesStatusAnalysisNull(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})

	// Only the synchronous clear fires - GenerateDegradedAnalysis fails, so
	// the goroutine returns before calling UpdateStatusAnalysis a second
	// time.
	waitOrTimeout(t, services.done)
	time.Sleep(20 * time.Millisecond) // let the goroutine's early return (if any second call were coming) settle

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1 (only the synchronous clear; the failed enrichment writes nothing)", len(calls))
	}
}

// TestSLOAnalyzer_DegradedEnrichment_EmptyResult_LeavesStatusAnalysisNull
// covers spec.md's edge case: "IF the LLM returns an empty or clearly
// malformed response THEN the system SHALL treat it the same as a failed
// call... never publish empty or garbled text."
// internal/connectors/openai.Client.Complete can legally return ("", nil).
func TestSLOAnalyzer_DegradedEnrichment_EmptyResult_LeavesStatusAnalysisNull(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedResult: "   "}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})

	// Only the synchronous clear fires - the goroutine must discard the
	// whitespace-only result instead of writing it.
	waitOrTimeout(t, services.done)
	time.Sleep(20 * time.Millisecond) // let a would-be second call (if the empty guard were missing) settle

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1 (only the synchronous clear; an empty analysis result must not be written)", len(calls))
	}
}

func TestSLOAnalyzer_OutageEnrichment_EmptyResult_LeavesGenericDescriptionInPlace(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: ""}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	time.Sleep(50 * time.Millisecond) // let the goroutine finish

	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0 (empty result must not overwrite the generic description)", len(setDescriptionCalls))
	}
}

func TestSLOAnalyzer_ClosingCommentEnrichment_EmptyResult_LeavesIncidentOpenWithNoProposal(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingResult: "  \n "}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{})

	time.Sleep(50 * time.Millisecond) // let the goroutine finish

	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (empty result must leave no proposal)", len(pendingCalls))
	}
}

func TestSLOAnalyzer_OutageEnrichment_Success_OverwritesGenericDescription(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, setDescriptionDone: make(chan struct{}, 1)}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: "The payments service is returning 5xx errors for most requests."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	waitOrTimeout(t, incidents.setDescriptionDone)
	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 1 {
		t.Fatalf("SetDescription called %d times, want 1", len(setDescriptionCalls))
	}
	texts := incidents.snapshotDescriptionTexts()
	if len(texts) != 1 || texts[0] != "The payments service is returning 5xx errors for most requests." {
		t.Errorf("SetDescription text = %v, want [%q] (the generated description, not just a call)", texts, "The payments service is returning 5xx errors for most requests.")
	}
}

func TestSLOAnalyzer_OutageEnrichment_Failure_LeavesGenericDescriptionInPlace(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	time.Sleep(50 * time.Millisecond) // let the failed goroutine finish

	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0 (failed enrichment must not overwrite the generic description)", len(setDescriptionCalls))
	}
	created := incidents.createCalls[0]
	if created.Description == nil || *created.Description == "" {
		t.Error("original generic Description was cleared, want it left in place")
	}
}

func TestSLOAnalyzer_ClosingCommentEnrichment_Failure_LeavesIncidentOpenWithNoProposal(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{})

	time.Sleep(50 * time.Millisecond) // let the failed goroutine finish

	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (failed enrichment must leave no proposal)", len(pendingCalls))
	}
}

// TestSLOAnalyzer_HandleTransition_ReturnsWellBeforeBlockedLLMCallUnblocks is
// the T14-mandated proof that a hung provider call cannot delay
// HandleTransition's own return - the actual "must not delay pollOnce's
// remaining services" guarantee is proven end-to-end by T15's poller-level
// timing test; this is the analyzer-level half of that guarantee.
func TestSLOAnalyzer_HandleTransition_ReturnsWellBeforeBlockedLLMCallUnblocks(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 1)}
	llmSvc := &fakeLLMGenerator{block: true}
	// A long a.timeout - the goroutine is bounded by it eventually, but
	// this test only cares that HandleTransition itself returns almost
	// immediately, long before that bound is reached.
	a := newTestAnalyzer(incidents, services, llmSvc, 5*time.Second)

	start := time.Now()
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("HandleTransition() took %v, want well under the 5s timeout (blocked LLM call must not delay the caller)", elapsed)
	}
}
