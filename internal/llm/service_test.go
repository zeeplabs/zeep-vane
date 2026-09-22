package llm

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

const testMasterKey = "test-master-key"

// fakeStore is an in-memory LLMProviderStore double - no real DB.
type fakeStore struct {
	rows                    map[string]*ProviderRecord
	activeProvider          string
	upsertErr               error
	getErr                  error
	listErr                 error
	getActiveErr            error
	setActiveErr            error
	updateModelErr          error
	markInvalidErr          error
	markCheckedErr          error
	markTransientFailureErr error
	deleteErr               error
	setRootCauseErr         error
	getRootCauseErr         error

	markInvalidCalls          []string // provider
	markCheckedCalls          []string // provider
	markTransientFailureCalls []string // provider
	deleteCalls               []string // provider

	rootCauseEnrichmentEnabled bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string]*ProviderRecord{}}
}

func (f *fakeStore) UpsertProvider(_ context.Context, provider string, encryptedAPIKey []byte, model string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.rows[provider] = &ProviderRecord{
		Provider:        provider,
		EncryptedAPIKey: encryptedAPIKey,
		Model:           model,
		Status:          "connected",
	}
	return nil
}

func (f *fakeStore) Get(_ context.Context, provider string) (*ProviderRecord, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.rows[provider]
	if !ok {
		return nil, ErrProviderRecordNotFound
	}
	return row, nil
}

func (f *fakeStore) ListPaginated(_ context.Context, page, pageSize int) ([]ProviderRecord, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	rows := make([]ProviderRecord, 0, len(f.rows))
	for _, row := range f.rows {
		rows = append(rows, *row)
	}
	total := len(rows)

	start := (page - 1) * pageSize
	if start >= total {
		return []ProviderRecord{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return rows[start:end], total, nil
}

func (f *fakeStore) GetActiveProvider(_ context.Context) (string, error) {
	if f.getActiveErr != nil {
		return "", f.getActiveErr
	}
	return f.activeProvider, nil
}

func (f *fakeStore) SetActiveProvider(_ context.Context, provider string) error {
	if f.setActiveErr != nil {
		return f.setActiveErr
	}
	f.activeProvider = provider
	return nil
}

func (f *fakeStore) UpdateModel(_ context.Context, provider, model string) error {
	if f.updateModelErr != nil {
		return f.updateModelErr
	}
	row, ok := f.rows[provider]
	if !ok {
		return ErrProviderRecordNotFound
	}
	row.Model = model
	return nil
}

func (f *fakeStore) MarkInvalid(_ context.Context, provider, lastError string) error {
	if f.markInvalidErr != nil {
		return f.markInvalidErr
	}
	f.markInvalidCalls = append(f.markInvalidCalls, provider)
	if row, ok := f.rows[provider]; ok {
		row.Status = "invalid"
		row.LastError = &lastError
	}
	return nil
}

func (f *fakeStore) MarkChecked(_ context.Context, provider string) error {
	if f.markCheckedErr != nil {
		return f.markCheckedErr
	}
	f.markCheckedCalls = append(f.markCheckedCalls, provider)
	if row, ok := f.rows[provider]; ok {
		row.Status = "connected"
		row.LastError = nil
	}
	return nil
}

func (f *fakeStore) MarkTransientFailure(_ context.Context, provider, lastError string) error {
	if f.markTransientFailureErr != nil {
		return f.markTransientFailureErr
	}
	f.markTransientFailureCalls = append(f.markTransientFailureCalls, provider)
	if row, ok := f.rows[provider]; ok {
		row.LastError = &lastError
	}
	return nil
}

// DeleteProvider mirrors the real repository's idempotent delete, including
// the FK's ON DELETE SET NULL behavior on llm_settings.active_provider - so
// a test asserting Disconnect clears the active provider through this fake
// exercises the same observable behavior as the real database constraint.
func (f *fakeStore) DeleteProvider(_ context.Context, provider string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls = append(f.deleteCalls, provider)
	delete(f.rows, provider)
	if f.activeProvider == provider {
		f.activeProvider = ""
	}
	return nil
}

// SetRootCauseEnrichmentEnabled records the last value set, mirroring the
// real repository's insert-or-update shape (RCA-07/RCA-09).
func (f *fakeStore) SetRootCauseEnrichmentEnabled(_ context.Context, enabled bool) error {
	if f.setRootCauseErr != nil {
		return f.setRootCauseErr
	}
	f.rootCauseEnrichmentEnabled = enabled
	return nil
}

// RootCauseEnrichmentEnabled returns the last value SetRootCauseEnrichmentEnabled
// recorded (RCA-08), or getRootCauseErr when set.
func (f *fakeStore) RootCauseEnrichmentEnabled(_ context.Context) (bool, error) {
	if f.getRootCauseErr != nil {
		return false, f.getRootCauseErr
	}
	return f.rootCauseEnrichmentEnabled, nil
}

// fakeProvider is a Provider double recording whether it was asked to
// validate, and what to return.
type fakeProvider struct {
	validateErr error
	completeErr error
	completeOut string
}

func (f *fakeProvider) ValidateCredentials(context.Context) error { return f.validateErr }
func (f *fakeProvider) Complete(context.Context, string, string) (string, error) {
	return f.completeOut, f.completeErr
}

func newTestService(store LLMProviderStore, factory ProviderFactory) *Service {
	return NewService(store, factory, testMasterKey, zap.NewNop())
}

func TestConnect_ValidKey_EncryptsAndPersistsWithDefaultModel(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) {
		return &fakeProvider{}, nil
	}
	svc := newTestService(store, factory)

	err := svc.Connect(t.Context(), "openai", "real-api-key", "")
	if err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	row, ok := store.rows["openai"]
	if !ok {
		t.Fatal("Connect() did not persist a row for openai")
	}
	if row.Status != "connected" {
		t.Errorf("Status = %q, want %q", row.Status, "connected")
	}
	if row.Model != defaultModel {
		t.Errorf("Model = %q, want default %q", row.Model, defaultModel)
	}

	decrypted, err := crypto.Decrypt(testMasterKey, row.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("Decrypt() returned unexpected error: %v", err)
	}
	if string(decrypted) != "real-api-key" {
		t.Errorf("decrypted stored key = %q, want %q (never plaintext, but must round-trip)", decrypted, "real-api-key")
	}
}

func TestConnect_ExplicitModel_Persisted(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) {
		return &fakeProvider{}, nil
	}
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	row := store.rows["openai"]
	if row.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", row.Model, "gpt-4o")
	}
}

// TestConnect_UnknownModel_ReturnsErrUnknownModel_NeverCallsFactory covers
// the post-review fix: Connect must reject a model outside the allowlist
// before ever calling the factory/ValidateCredentials, same as SetModel -
// previously only SetModel validated the model, so a caller could persist
// an off-allowlist model straight through Connect, silently breaking every
// subsequent Generate* call.
func TestConnect_UnknownModel_ReturnsErrUnknownModel_NeverCallsFactory(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey, model string) (Provider, error) {
		factoryCalled = true
		return &fakeProvider{}, nil
	}
	svc := newTestService(store, factory)

	err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-3.5-turbo-instruct")
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("Connect() error = %v, want ErrUnknownModel", err)
	}
	if factoryCalled {
		t.Error("Connect() called the factory despite an off-allowlist model")
	}
	if _, ok := store.rows["openai"]; ok {
		t.Fatal("Connect() persisted a row despite an off-allowlist model")
	}
}

func TestConnect_InvalidCredentials_ReturnsErrValidationFailed_PersistsNothing(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) {
		return &fakeProvider{validateErr: ErrUnauthorized}, nil
	}
	svc := newTestService(store, factory)

	err := svc.Connect(t.Context(), "openai", "bad-key", "")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("Connect() error = %v, want ErrValidationFailed", err)
	}
	if _, ok := store.rows["openai"]; ok {
		t.Fatal("Connect() persisted a row despite validation failure")
	}
}

func TestConnect_MissingAPIKey_ReturnsErrInvalidInput_NeverCallsFactory(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey, model string) (Provider, error) {
		factoryCalled = true
		return &fakeProvider{}, nil
	}
	svc := newTestService(store, factory)

	err := svc.Connect(t.Context(), "openai", "", "")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Connect() error = %v, want ErrInvalidInput", err)
	}
	if factoryCalled {
		t.Error("Connect() called the provider factory despite missing api key")
	}
}

func TestSetModel_UnknownModel_RejectedWithoutTouchingRow(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	err := svc.SetModel(t.Context(), "openai", "gpt-3.5-turbo")
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("SetModel() error = %v, want ErrUnknownModel", err)
	}
	if store.rows["openai"].Model != "gpt-4o" {
		t.Errorf("Model = %q after rejected SetModel, want unchanged %q", store.rows["openai"].Model, "gpt-4o")
	}
}

func TestSetModel_KnownModel_Persisted(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", "gpt-4o-mini"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	if err := svc.SetModel(t.Context(), "openai", "gpt-4.1"); err != nil {
		t.Fatalf("SetModel() returned unexpected error: %v", err)
	}
	if store.rows["openai"].Model != "gpt-4.1" {
		t.Errorf("Model = %q, want %q", store.rows["openai"].Model, "gpt-4.1")
	}
}

func TestSetModel_UnconnectedProvider_ReturnsErrProviderNotConnected(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	err := svc.SetModel(t.Context(), "openai", "gpt-4o")
	if !errors.Is(err, ErrProviderNotConnected) {
		t.Fatalf("SetModel() error = %v, want ErrProviderNotConnected", err)
	}
}

func TestActivate_UnconnectedProvider_ReturnsErrProviderNotConnected(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	err := svc.Activate(t.Context(), "openai")
	if !errors.Is(err, ErrProviderNotConnected) {
		t.Fatalf("Activate() error = %v, want ErrProviderNotConnected", err)
	}
}

func TestActivate_ConnectedProvider_SetsActive(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", ""); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}
	if store.activeProvider != "openai" {
		t.Errorf("activeProvider = %q, want %q", store.activeProvider, "openai")
	}
}

func TestList_NeverLeaksKeyMaterial(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "super-secret-key", "gpt-4o"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	result, err := svc.List(t.Context(), 1, 10)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(result.Providers))
	}
	if result.Providers[0].Provider != "openai" || result.Providers[0].Model != "gpt-4o" {
		t.Errorf("Providers[0] = %+v, want provider=openai model=gpt-4o", result.Providers[0])
	}
}

// TestDisconnect_ConnectedProvider_RemovesRow covers provider-disconnect
// PROVDISC-04 AC1: Disconnect deletes the provider's row.
func TestDisconnect_ConnectedProvider_RemovesRow(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", ""); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	if err := svc.Disconnect(t.Context(), "openai"); err != nil {
		t.Fatalf("Disconnect() returned unexpected error: %v", err)
	}
	if _, ok := store.rows["openai"]; ok {
		t.Error("Disconnect() did not remove the provider row")
	}
	if len(store.deleteCalls) != 1 || store.deleteCalls[0] != "openai" {
		t.Errorf("deleteCalls = %v, want [\"openai\"] (Disconnect must delegate to the repository)", store.deleteCalls)
	}
}

// TestDisconnect_ActiveProvider_ClearsActiveProvider covers PROVDISC-04
// AC2: disconnecting the active provider leaves active_provider cleared -
// via the repository, not a manual Service-level clear.
func TestDisconnect_ActiveProvider_ClearsActiveProvider(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Connect(t.Context(), "openai", "real-api-key", ""); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "openai"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	if err := svc.Disconnect(t.Context(), "openai"); err != nil {
		t.Fatalf("Disconnect() returned unexpected error: %v", err)
	}
	if store.activeProvider != "" {
		t.Errorf("activeProvider = %q, want \"\" after disconnecting the active provider", store.activeProvider)
	}
}

// TestDisconnect_NeverConnected_NoError covers PROVDISC-05 AC4: disconnecting
// a provider with no row is a no-op success, not an error.
func TestDisconnect_NeverConnected_NoError(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.Disconnect(t.Context(), "openai"); err != nil {
		t.Fatalf("Disconnect() on never-connected provider returned unexpected error: %v, want nil (idempotent)", err)
	}
}

// TestSetRootCauseEnrichmentEnabled_DelegatesToRepository covers RCA-07:
// Service.SetRootCauseEnrichmentEnabled is a thin wrapper over the
// repository's method - no provider-connected precondition, unlike
// Activate/SetModel.
func TestSetRootCauseEnrichmentEnabled_DelegatesToRepository(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	if err := svc.SetRootCauseEnrichmentEnabled(t.Context(), true); err != nil {
		t.Fatalf("SetRootCauseEnrichmentEnabled(true) returned unexpected error: %v", err)
	}
	if !store.rootCauseEnrichmentEnabled {
		t.Errorf("store.rootCauseEnrichmentEnabled = false, want true")
	}

	if err := svc.SetRootCauseEnrichmentEnabled(t.Context(), false); err != nil {
		t.Fatalf("SetRootCauseEnrichmentEnabled(false) returned unexpected error: %v", err)
	}
	if store.rootCauseEnrichmentEnabled {
		t.Errorf("store.rootCauseEnrichmentEnabled = true, want false")
	}
}

// TestSetRootCauseEnrichmentEnabled_RepositoryError_Wrapped covers the
// error-wrapping convention every other Service method uses (e.g.
// Disconnect's "llm: failed to disconnect provider" wrap) - the repository
// error is not returned raw.
func TestSetRootCauseEnrichmentEnabled_RepositoryError_Wrapped(t *testing.T) {
	store := newFakeStore()
	store.setRootCauseErr = errors.New("db unavailable")
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	err := svc.SetRootCauseEnrichmentEnabled(t.Context(), true)
	if err == nil {
		t.Fatal("SetRootCauseEnrichmentEnabled() returned nil error, want a wrapped error")
	}
	if !errors.Is(err, store.setRootCauseErr) {
		t.Errorf("SetRootCauseEnrichmentEnabled() error = %v, want it to wrap %v", err, store.setRootCauseErr)
	}
}

// TestRootCauseEnrichmentEnabled_DelegatesToRepository covers RCA-08: the
// settings UI needs to read the toggle's current state, not just write it.
func TestRootCauseEnrichmentEnabled_DelegatesToRepository(t *testing.T) {
	store := newFakeStore()
	store.rootCauseEnrichmentEnabled = true
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	enabled, err := svc.RootCauseEnrichmentEnabled(t.Context())
	if err != nil {
		t.Fatalf("RootCauseEnrichmentEnabled() returned unexpected error: %v", err)
	}
	if !enabled {
		t.Errorf("RootCauseEnrichmentEnabled() = false, want true")
	}
}

// TestRootCauseEnrichmentEnabled_RepositoryError_Wrapped mirrors
// TestSetRootCauseEnrichmentEnabled_RepositoryError_Wrapped for the read
// path.
func TestRootCauseEnrichmentEnabled_RepositoryError_Wrapped(t *testing.T) {
	store := newFakeStore()
	store.getRootCauseErr = errors.New("db unavailable")
	factory := func(provider, apiKey, model string) (Provider, error) { return &fakeProvider{}, nil }
	svc := newTestService(store, factory)

	_, err := svc.RootCauseEnrichmentEnabled(t.Context())
	if err == nil {
		t.Fatal("RootCauseEnrichmentEnabled() returned nil error, want a wrapped error")
	}
	if !errors.Is(err, store.getRootCauseErr) {
		t.Errorf("RootCauseEnrichmentEnabled() error = %v, want it to wrap %v", err, store.getRootCauseErr)
	}
}
