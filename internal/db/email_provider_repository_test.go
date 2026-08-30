//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// resetEmailProviders clears both email_providers rows and the
// email_settings singleton's active_provider, leaving the schema in the
// same clean state a fresh migration would - so each test in this file
// starts from a known baseline regardless of what a previous test left
// behind.
func resetEmailProviders(t *testing.T, ctx context.Context, pool *Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "UPDATE email_settings SET active_provider = NULL WHERE id = 1"); err != nil {
		t.Fatalf("failed to reset email_settings.active_provider: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM email_providers"); err != nil {
		t.Fatalf("failed to clear email_providers: %v", err)
	}
}

func newEmailProviderRepoForTest(t *testing.T) (*EmailProviderRepository, *Pool) {
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

	resetEmailProviders(t, context.Background(), pool)
	t.Cleanup(func() { resetEmailProviders(t, context.Background(), pool) })

	return NewEmailProviderRepository(pool), pool
}

func TestEmailProviderRepository_UpsertProvider_FirstConnect_Inserts(t *testing.T) {
	repo, pool := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "sendgrid", []byte("cipher-1"), "a@example.com", "Alice"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}

	ep, err := repo.Get(ctx, "sendgrid")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if ep.Status != "connected" {
		t.Errorf("Status = %q, want %q", ep.Status, "connected")
	}
	if ep.FromEmail != "a@example.com" || ep.FromName != "Alice" {
		t.Errorf("FromEmail/FromName = %q/%q, want %q/%q", ep.FromEmail, ep.FromName, "a@example.com", "Alice")
	}
	if string(ep.EncryptedAPIKey) != "cipher-1" {
		t.Errorf("EncryptedAPIKey = %q, want %q", ep.EncryptedAPIKey, "cipher-1")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_providers WHERE provider = 'sendgrid'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for sendgrid = %d, want exactly 1", count)
	}
}

func TestEmailProviderRepository_UpsertProvider_Reconnect_OverwritesSameRow(t *testing.T) {
	repo, pool := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "resend", []byte("cipher-old"), "old@example.com", "Old Name"); err != nil {
		t.Fatalf("first UpsertProvider() returned unexpected error: %v", err)
	}
	if err := repo.UpsertProvider(ctx, "resend", []byte("cipher-new"), "new@example.com", "New Name"); err != nil {
		t.Fatalf("second UpsertProvider() returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_providers WHERE provider = 'resend'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for resend = %d, want exactly 1 (reconnect must overwrite, not insert a second row)", count)
	}

	ep, err := repo.Get(ctx, "resend")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if string(ep.EncryptedAPIKey) != "cipher-new" {
		t.Errorf("EncryptedAPIKey = %q, want %q (overwritten value)", ep.EncryptedAPIKey, "cipher-new")
	}
	if ep.FromEmail != "new@example.com" || ep.FromName != "New Name" {
		t.Errorf("FromEmail/FromName = %q/%q, want %q/%q", ep.FromEmail, ep.FromName, "new@example.com", "New Name")
	}
}

func TestEmailProviderRepository_Get_NeverConnected_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	_, err := repo.Get(ctx, "sendgrid")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestEmailProviderRepository_List_NoneConnected_ReturnsEmptySlice(t *testing.T) {
	repo, _ := newEmailProviderRepoForTest(t)
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

func TestEmailProviderRepository_List_MultipleConnected_ReturnsAllOrderedByProvider(t *testing.T) {
	repo, _ := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "resend", []byte("cipher-resend"), "r@example.com", "Resend Sender"); err != nil {
		t.Fatalf("UpsertProvider(resend) returned unexpected error: %v", err)
	}
	if err := repo.UpsertProvider(ctx, "sendgrid", []byte("cipher-sendgrid"), "s@example.com", "SendGrid Sender"); err != nil {
		t.Fatalf("UpsertProvider(sendgrid) returned unexpected error: %v", err)
	}

	providers, total, err := repo.ListPaginated(ctx, 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("ListPaginated() returned %d providers, want 2", len(providers))
	}
	if total != 2 {
		t.Fatalf("ListPaginated() total = %d, want 2", total)
	}
	if providers[0].Provider != "resend" || providers[1].Provider != "sendgrid" {
		t.Fatalf("ListPaginated() order = [%q, %q], want [resend, sendgrid] (ordered by provider)", providers[0].Provider, providers[1].Provider)
	}
}

func TestEmailProviderRepository_GetActiveProvider_NeverActivated_ReturnsEmptyString(t *testing.T) {
	repo, _ := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	active, err := repo.GetActiveProvider(ctx)
	if err != nil {
		t.Fatalf("GetActiveProvider() returned unexpected error: %v", err)
	}
	if active != "" {
		t.Errorf("GetActiveProvider() = %q, want \"\"", active)
	}
}

func TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow(t *testing.T) {
	repo, _ := newEmailProviderRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertProvider(ctx, "sendgrid", []byte("cipher"), "s@example.com", "SendGrid Sender"); err != nil {
		t.Fatalf("UpsertProvider() returned unexpected error: %v", err)
	}

	if err := repo.SetActiveProvider(ctx, "sendgrid"); err != nil {
		t.Fatalf("SetActiveProvider() returned unexpected error: %v", err)
	}

	active, err := repo.GetActiveProvider(ctx)
	if err != nil {
		t.Fatalf("GetActiveProvider() returned unexpected error: %v", err)
	}
	if active != "sendgrid" {
		t.Fatalf("GetActiveProvider() = %q, want %q", active, "sendgrid")
	}

	// Switching to a second provider must flip the singleton, not add to it.
	if err := repo.UpsertProvider(ctx, "resend", []byte("cipher"), "r@example.com", "Resend Sender"); err != nil {
		t.Fatalf("UpsertProvider(resend) returned unexpected error: %v", err)
	}
	if err := repo.SetActiveProvider(ctx, "resend"); err != nil {
		t.Fatalf("SetActiveProvider(resend) returned unexpected error: %v", err)
	}
	active, err = repo.GetActiveProvider(ctx)
	if err != nil {
		t.Fatalf("GetActiveProvider() returned unexpected error: %v", err)
	}
	if active != "resend" {
		t.Fatalf("GetActiveProvider() after switch = %q, want %q", active, "resend")
	}
}
