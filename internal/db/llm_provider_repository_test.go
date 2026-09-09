//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// resetLLMProviders clears both llm_providers rows and the llm_settings
// singleton's active_provider, leaving the schema in the same clean state
// a fresh migration would - so each test in this file starts from a known
// baseline regardless of what a previous test left behind.
func resetLLMProviders(t *testing.T, ctx context.Context, pool *Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "UPDATE llm_settings SET active_provider = NULL WHERE id = 1"); err != nil {
		t.Fatalf("failed to reset llm_settings.active_provider: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM llm_providers"); err != nil {
		t.Fatalf("failed to clear llm_providers: %v", err)
	}
}

func newLLMProviderRepoForTest(t *testing.T) (*LLMProviderRepository, *Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	resetLLMProviders(t, context.Background(), pool)
	t.Cleanup(func() { resetLLMProviders(t, context.Background(), pool) })

	return NewLLMProviderRepository(pool), pool
}

func TestLLMProviderRepository_UpsertProvider_FirstConnect_RoundTripsWithModel(t *testing.T) {
	repo, pool := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher-1"), "gpt-4o-mini"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}

	lp, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if lp.Status != "connected" {
		t.Errorf("Status = %q, want %q", lp.Status, "connected")
	}
	if lp.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want %q", lp.Model, "gpt-4o-mini")
	}
	if string(lp.EncryptedAPIKey) != "cipher-1" {
		t.Errorf("EncryptedAPIKey = %q, want %q", lp.EncryptedAPIKey, "cipher-1")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM llm_providers WHERE provider = 'openai'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for openai = %d, want exactly 1", count)
	}
}

func TestLLMProviderRepository_UpsertProvider_Reconnect_OverwritesSameRow(t *testing.T) {
	repo, pool := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher-old"), "gpt-4o-mini"); err != nil {
		t.Fatalf("first UpsertProvider() returned unexpected error: %v", err)
	}
	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher-new"), "gpt-4o"); err != nil {
		t.Fatalf("second UpsertProvider() returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM llm_providers WHERE provider = 'openai'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for openai = %d, want exactly 1 (reconnect must overwrite, not insert a second row)", count)
	}

	lp, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if string(lp.EncryptedAPIKey) != "cipher-new" {
		t.Errorf("EncryptedAPIKey = %q, want %q", lp.EncryptedAPIKey, "cipher-new")
	}
	if lp.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", lp.Model, "gpt-4o")
	}
}

func TestLLMProviderRepository_Get_NeverConnected_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	_, err := repo.Get(ctx, "openai")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestLLMProviderRepository_List_NoneConnected_ReturnsEmptySlice(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	providers, total, err := repo.ListPaginated(ctx, 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("ListPaginated() returned %d providers, want 0", len(providers))
	}
	if total != 0 {
		t.Fatalf("ListPaginated() total = %d, want 0", total)
	}
}

func TestLLMProviderRepository_GetActiveProvider_EmptySettingsRow_ReturnsEmptyString(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	active, err := repo.GetActiveProvider(ctx)
	if err != nil {
		t.Fatalf("GetActiveProvider() returned unexpected error: %v", err)
	}
	if active != "" {
		t.Errorf("GetActiveProvider() = %q, want \"\"", active)
	}
}

func TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher"), "gpt-4o-mini"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}

	if err := repo.SetActiveProvider(ctx, "openai"); err != nil {
		t.Fatalf("SetActiveProvider() returned unexpected error: %v", err)
	}

	active, err := repo.GetActiveProvider(ctx)
	if err != nil {
		t.Fatalf("GetActiveProvider() returned unexpected error: %v", err)
	}
	if active != "openai" {
		t.Fatalf("GetActiveProvider() = %q, want %q", active, "openai")
	}
}

func TestLLMProviderRepository_UpdateModel_ChangesOnlyModelColumn(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher"), "gpt-4o-mini"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}
	before, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() before UpdateModel() returned unexpected error: %v", err)
	}

	if err := repo.UpdateModel(ctx, "openai", "gpt-4.1"); err != nil {
		t.Fatalf("UpdateModel() returned unexpected error: %v", err)
	}

	after, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() after UpdateModel() returned unexpected error: %v", err)
	}
	if after.Model != "gpt-4.1" {
		t.Errorf("Model = %q, want %q", after.Model, "gpt-4.1")
	}
	if after.Status != before.Status {
		t.Errorf("Status changed to %q, want unchanged %q", after.Status, before.Status)
	}
	if string(after.EncryptedAPIKey) != string(before.EncryptedAPIKey) {
		t.Errorf("EncryptedAPIKey changed, want unchanged")
	}
}

func TestLLMProviderRepository_MarkInvalid_SetsStatusAndLastError(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher"), "gpt-4o-mini"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}

	if err := repo.MarkInvalid(ctx, "openai", "invalid api key"); err != nil {
		t.Fatalf("MarkInvalid() returned unexpected error: %v", err)
	}

	lp, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if lp.Status != "invalid" {
		t.Errorf("Status = %q, want %q", lp.Status, "invalid")
	}
	if lp.LastError == nil || *lp.LastError != "invalid api key" {
		t.Errorf("LastError = %v, want %q", lp.LastError, "invalid api key")
	}
	if lp.LastCheckedAt == nil {
		t.Error("LastCheckedAt is nil, want a timestamp set by MarkInvalid")
	}
}

func TestLLMProviderRepository_MarkChecked_ClearsInvalidState(t *testing.T) {
	repo, _ := newLLMProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "openai", []byte("cipher"), "gpt-4o-mini"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}
	if err := repo.MarkInvalid(ctx, "openai", "seeded failure"); err != nil {
		t.Fatalf("MarkInvalid() returned unexpected error: %v", err)
	}

	if err := repo.MarkChecked(ctx, "openai"); err != nil {
		t.Fatalf("MarkChecked() returned unexpected error: %v", err)
	}

	lp, err := repo.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if lp.Status != "connected" {
		t.Errorf("Status = %q, want %q", lp.Status, "connected")
	}
	if lp.LastError != nil {
		t.Errorf("LastError = %q, want nil (must clear the seeded failure)", *lp.LastError)
	}
}
