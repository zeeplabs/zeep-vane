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
	rows           map[string]*ProviderRecord
	activeProvider string
	upsertErr      error
	getErr         error
	listErr        error
	getActiveErr   error
	setActiveErr   error
	updateModelErr error
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

// fakeProvider is a Provider double recording whether it was asked to
// validate, and what to return.
type fakeProvider struct {
	validateErr error
}

func (f *fakeProvider) ValidateCredentials(context.Context) error { return f.validateErr }
func (f *fakeProvider) Complete(context.Context, string, string) (string, error) {
	return "", nil
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
