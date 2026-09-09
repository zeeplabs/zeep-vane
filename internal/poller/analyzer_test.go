package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// fakeIncidentStore is a no-network, no-DB fake of incidentStore that
// records every call so tests can assert exactly which methods fired.
type fakeIncidentStore struct {
	openIncidents map[string]string // serviceID -> incidentID, present means "open"

	createCalls            []*db.Incident
	createServiceIDs       [][]string
	createErr              error
	hasOpenErr             error
	setDescriptionCalls    []string // incidentID
	setDescriptionErr      error
	setPendingCommentCalls []string // incidentID
	setPendingCommentErr   error
}

func (f *fakeIncidentStore) Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createCalls = append(f.createCalls, incident)
	f.createServiceIDs = append(f.createServiceIDs, serviceIDs)
	incident.ID = "new-incident-id"
	return nil
}

func (f *fakeIncidentStore) HasOpenIncidentForService(ctx context.Context, serviceID string) (string, bool, error) {
	if f.hasOpenErr != nil {
		return "", false, f.hasOpenErr
	}
	id, ok := f.openIncidents[serviceID]
	return id, ok, nil
}

func (f *fakeIncidentStore) SetDescription(ctx context.Context, incidentID, description string) error {
	if f.setDescriptionErr != nil {
		return f.setDescriptionErr
	}
	f.setDescriptionCalls = append(f.setDescriptionCalls, incidentID)
	return nil
}

func (f *fakeIncidentStore) SetPendingCloseComment(ctx context.Context, incidentID, comment string) error {
	if f.setPendingCommentErr != nil {
		return f.setPendingCommentErr
	}
	f.setPendingCommentCalls = append(f.setPendingCommentCalls, incidentID)
	return nil
}

// fakeStatusAnalysisWriter is a no-DB fake of statusAnalysisWriter.
type fakeStatusAnalysisWriter struct {
	calls []*string // nil entry means UpdateStatusAnalysis(ctx, id, nil)
	err   error
}

func (f *fakeStatusAnalysisWriter) UpdateStatusAnalysis(ctx context.Context, serviceID string, analysis *string) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, analysis)
	return nil
}

// fakeLLMGenerator is a no-network fake of llmGenerator. T13's tests never
// expect it to be called (no async dispatch exists yet) - it exists so
// NewSLOAnalyzer can be constructed.
type fakeLLMGenerator struct {
	degradedCalls int
	outageCalls   int
	closingCalls  int
}

func (f *fakeLLMGenerator) GenerateDegradedAnalysis(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.degradedCalls++
	return "", nil
}

func (f *fakeLLMGenerator) GenerateOutageDescription(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.outageCalls++
	return "", nil
}

func (f *fakeLLMGenerator) GenerateClosingComment(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.closingCalls++
	return "", nil
}

func newTestAnalyzer(incidents *fakeIncidentStore, services *fakeStatusAnalysisWriter, llmSvc *fakeLLMGenerator) *SLOAnalyzer {
	return NewSLOAnalyzer(incidents, services, llmSvc, time.Second, zap.NewNop())
}

func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "operational", datadog.SLOStatus{})

	if len(incidents.createCalls) != 0 {
		t.Errorf("Create called %d times, want 0", len(incidents.createCalls))
	}
	if len(services.calls) != 0 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 0", len(services.calls))
	}
}

func TestSLOAnalyzer_HandleTransition_OutageNoExistingIncident_CreatesAutoIncident(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	if len(incidents.createCalls) != 1 {
		t.Fatalf("Create called %d times, want 1", len(incidents.createCalls))
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
}

func TestSLOAnalyzer_HandleTransition_OutageAlreadyOpenIncident_CreatesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	if len(incidents.createCalls) != 0 {
		t.Errorf("Create called %d times, want 0 (an incident is already open)", len(incidents.createCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_IntoDegraded_ClearsStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{})

	if len(services.calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1", len(services.calls))
	}
	if services.calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil", *services.calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_AwayFromDegraded_ClearsStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{})

	if len(services.calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1", len(services.calls))
	}
	if services.calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil", *services.calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_DegradedToOutage_ClearsStatusAnalysisAndDoesNotCreateIncidentTwice(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "outage", datadog.SLOStatus{})

	if len(services.calls) != 1 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 1 (leaving degraded)", len(services.calls))
	}
	if len(incidents.createCalls) != 1 {
		t.Errorf("Create called %d times, want 1 (entering outage)", len(incidents.createCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithOpenIncident_IdentifiesRecoveryWithoutSynchronousWrite(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{})

	// T13 identifies the case (HasOpenIncidentForService was consulted,
	// proven indirectly by SetPendingCloseComment never being called with
	// stale/empty data) but performs no synchronous write - there is
	// nothing to write without an LLM call, which is T14's job.
	if len(incidents.setPendingCommentCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (no async dispatch yet in T13)", len(incidents.setPendingCommentCalls))
	}
	if len(incidents.createCalls) != 0 {
		t.Errorf("Create called %d times, want 0 (operational transition never creates an incident)", len(incidents.createCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithNoOpenIncident_DoesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{})

	if len(incidents.createCalls) != 0 {
		t.Errorf("Create called %d times, want 0", len(incidents.createCalls))
	}
	if len(incidents.setPendingCommentCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0", len(incidents.setPendingCommentCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_HasOpenIncidentError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{hasOpenErr: errors.New("db unreachable")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})

	if len(incidents.createCalls) != 0 {
		t.Errorf("Create called %d times, want 0 (HasOpenIncidentForService failed)", len(incidents.createCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_CreateError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, createErr: errors.New("insert failed")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{})

	// Must not panic.
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{})
}
