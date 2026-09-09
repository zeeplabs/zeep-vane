package llm

import (
	"context"
	"errors"
	"testing"
)

// completingProvider is a Provider double for the Generate* tests: it
// records the prompts it was given and returns a fixed output/error.
type completingProvider struct {
	output           string
	completeErr      error
	lastSystemPrompt string
	lastUserPrompt   string
}

func (f *completingProvider) ValidateCredentials(context.Context) error { return nil }
func (f *completingProvider) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	f.lastSystemPrompt = systemPrompt
	f.lastUserPrompt = userPrompt
	return f.output, f.completeErr
}

func connectedServiceWithActiveProvider(t *testing.T) (*Service, *completingProvider) {
	t.Helper()
	store := newFakeStore()
	provider := &completingProvider{output: "generated analysis text"}
	factory := func(providerName, apiKey, model string) (Provider, error) {
		return provider, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o-mini"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}
	return svc, provider
}

func TestGenerateDegradedAnalysis_NoActiveProvider_ReturnsErrNoActiveProvider_NoFactoryCall(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey, model string) (Provider, error) {
		factoryCalled = true
		return &completingProvider{}, nil
	}
	svc := newTestService(store, factory)

	_, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput())
	if !errors.Is(err, ErrNoActiveProvider) {
		t.Fatalf("GenerateDegradedAnalysis() error = %v, want ErrNoActiveProvider", err)
	}
	if factoryCalled {
		t.Error("GenerateDegradedAnalysis() called the provider factory despite no active provider")
	}
}

func TestGenerateOutageDescription_NoActiveProvider_ReturnsErrNoActiveProvider_NoFactoryCall(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey, model string) (Provider, error) {
		factoryCalled = true
		return &completingProvider{}, nil
	}
	svc := newTestService(store, factory)

	_, err := svc.GenerateOutageDescription(t.Context(), testAnalysisInput())
	if !errors.Is(err, ErrNoActiveProvider) {
		t.Fatalf("GenerateOutageDescription() error = %v, want ErrNoActiveProvider", err)
	}
	if factoryCalled {
		t.Error("GenerateOutageDescription() called the provider factory despite no active provider")
	}
}

func TestGenerateClosingComment_NoActiveProvider_ReturnsErrNoActiveProvider_NoFactoryCall(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey, model string) (Provider, error) {
		factoryCalled = true
		return &completingProvider{}, nil
	}
	svc := newTestService(store, factory)

	_, err := svc.GenerateClosingComment(t.Context(), testAnalysisInput())
	if !errors.Is(err, ErrNoActiveProvider) {
		t.Fatalf("GenerateClosingComment() error = %v, want ErrNoActiveProvider", err)
	}
	if factoryCalled {
		t.Error("GenerateClosingComment() called the provider factory despite no active provider")
	}
}

func TestGenerateDegradedAnalysis_ActiveProvider_ReturnsCompletion(t *testing.T) {
	svc, provider := connectedServiceWithActiveProvider(t)
	provider.output = "degraded tooltip text"

	out, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput())
	if err != nil {
		t.Fatalf("GenerateDegradedAnalysis() returned unexpected error: %v", err)
	}
	if out != "degraded tooltip text" {
		t.Errorf("GenerateDegradedAnalysis() = %q, want %q", out, "degraded tooltip text")
	}
	if provider.lastSystemPrompt == "" || provider.lastUserPrompt == "" {
		t.Error("GenerateDegradedAnalysis() did not pass a system/user prompt to Provider.Complete")
	}
}

// TestGenerate_DecryptsStoredKeyAndPassesStoredModelToFactory confirms the
// generate helper correctly decrypts the persisted key and forwards the
// persisted model to ProviderFactory, rather than some default/empty
// value.
func TestGenerate_DecryptsStoredKeyAndPassesStoredModelToFactory(t *testing.T) {
	store := newFakeStore()
	provider := &completingProvider{output: "ok"}
	var gotAPIKey, gotModel string
	factory := func(providerName, apiKey, model string) (Provider, error) {
		gotAPIKey = apiKey
		gotModel = model
		return provider, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "the-real-api-key", "gpt-4o"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	if _, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput()); err != nil {
		t.Fatalf("GenerateDegradedAnalysis() returned unexpected error: %v", err)
	}

	if gotAPIKey != "the-real-api-key" {
		t.Errorf("factory received apiKey = %q, want decrypted %q", gotAPIKey, "the-real-api-key")
	}
	if gotModel != "gpt-4o" {
		t.Errorf("factory received model = %q, want stored model %q", gotModel, "gpt-4o")
	}
}

func TestGenerateOutageDescription_ActiveProvider_ReturnsCompletion(t *testing.T) {
	svc, provider := connectedServiceWithActiveProvider(t)
	provider.output = "outage description text"

	out, err := svc.GenerateOutageDescription(t.Context(), testAnalysisInput())
	if err != nil {
		t.Fatalf("GenerateOutageDescription() returned unexpected error: %v", err)
	}
	if out != "outage description text" {
		t.Errorf("GenerateOutageDescription() = %q, want %q", out, "outage description text")
	}
}

func TestGenerateClosingComment_ActiveProvider_ReturnsCompletion(t *testing.T) {
	svc, provider := connectedServiceWithActiveProvider(t)
	provider.output = "closing comment text"

	out, err := svc.GenerateClosingComment(t.Context(), testAnalysisInput())
	if err != nil {
		t.Fatalf("GenerateClosingComment() returned unexpected error: %v", err)
	}
	if out != "closing comment text" {
		t.Errorf("GenerateClosingComment() = %q, want %q", out, "closing comment text")
	}
}

func TestGenerateDegradedAnalysis_ProviderError_Propagated(t *testing.T) {
	svc, provider := connectedServiceWithActiveProvider(t)
	provider.completeErr = errors.New("boom")

	_, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput())
	if err == nil || err.Error() != "boom" {
		t.Errorf("GenerateDegradedAnalysis() error = %v, want propagated %q", err, "boom")
	}
}

// TestGenerateDegradedAnalysis_UnauthorizedError_MarksProviderInvalid covers
// the edge case where a previously-connected provider's credentials are
// revoked/expired: Complete returning ErrUnauthorized must mark the
// provider row invalid so an operator has a visible signal, rather than
// failing silently forever.
func TestGenerateDegradedAnalysis_UnauthorizedError_MarksProviderInvalid(t *testing.T) {
	store := newFakeStore()
	provider := &completingProvider{completeErr: ErrUnauthorized}
	factory := func(providerName, apiKey, model string) (Provider, error) {
		return provider, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o-mini"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	_, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("GenerateDegradedAnalysis() error = %v, want ErrUnauthorized", err)
	}
	if len(store.markInvalidCalls) != 1 || store.markInvalidCalls[0] != "openai" {
		t.Errorf("markInvalidCalls = %v, want [openai]", store.markInvalidCalls)
	}
	if len(store.markCheckedCalls) != 0 {
		t.Errorf("markCheckedCalls = %v, want none (unauthorized must not also mark checked)", store.markCheckedCalls)
	}
	if store.rows["openai"].Status != "invalid" {
		t.Errorf("provider status = %q, want %q", store.rows["openai"].Status, "invalid")
	}
}

// TestGenerateDegradedAnalysis_TransientError_MarksTransientFailure_NeverChecked
// covers the post-review fix: a non-authorization failure (timeout, 5xx)
// must never call MarkChecked, since that would silently clear a
// previously recorded 'invalid' status the moment any unrelated transient
// error occurred - it is not evidence the credentials are valid again.
func TestGenerateDegradedAnalysis_TransientError_MarksTransientFailure_NeverChecked(t *testing.T) {
	store := newFakeStore()
	provider := &completingProvider{completeErr: errors.New("service unavailable")}
	factory := func(providerName, apiKey, model string) (Provider, error) {
		return provider, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o-mini"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}
	// Simulate a key already flagged invalid by a prior call.
	store.rows["openai"].Status = "invalid"

	if _, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput()); err == nil {
		t.Fatal("GenerateDegradedAnalysis() error = nil, want the transient error")
	}

	if len(store.markCheckedCalls) != 0 {
		t.Errorf("markCheckedCalls = %v, want none (transient failure must never mark checked)", store.markCheckedCalls)
	}
	if len(store.markTransientFailureCalls) != 1 || store.markTransientFailureCalls[0] != "openai" {
		t.Errorf("markTransientFailureCalls = %v, want [openai]", store.markTransientFailureCalls)
	}
	if store.rows["openai"].Status != "invalid" {
		t.Errorf("provider status = %q, want unchanged %q (transient failure must not resurrect it)", store.rows["openai"].Status, "invalid")
	}
}

// TestGenerateDegradedAnalysis_Success_MarksProviderChecked confirms a
// successful Generate* call also stamps last_checked_at (via MarkChecked),
// clearing any prior invalid status.
func TestGenerateDegradedAnalysis_Success_MarksProviderChecked(t *testing.T) {
	store := newFakeStore()
	provider := &completingProvider{output: "tooltip text"}
	factory := func(providerName, apiKey, model string) (Provider, error) {
		return provider, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o-mini"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	if _, err := svc.GenerateDegradedAnalysis(t.Context(), testAnalysisInput()); err != nil {
		t.Fatalf("GenerateDegradedAnalysis() returned unexpected error: %v", err)
	}

	if len(store.markCheckedCalls) != 1 || store.markCheckedCalls[0] != "openai" {
		t.Errorf("markCheckedCalls = %v, want [openai]", store.markCheckedCalls)
	}
	if len(store.markInvalidCalls) != 0 {
		t.Errorf("markInvalidCalls = %v, want none", store.markInvalidCalls)
	}
}
