//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

func newCompanySettingsTestRepo(t *testing.T) (*CompanySettingsRepository, *Pool) {
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

	// Reset the singleton row to a known state before each test - other
	// tests in this suite/package mutate the same row (there is only one).
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1")
	})
	if _, err := pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1"); err != nil {
		t.Fatalf("failed to reset company_settings fixture: %v", err)
	}

	return NewCompanySettingsRepository(pool), pool
}

// TestCompanySettingsRepository_Get_ReturnsSingletonRow_NoNotFoundBranch
// asserts SET-03/SET-06: Get is a plain SELECT with no "not found" path -
// the seeded row is always there.
func TestCompanySettingsRepository_Get_ReturnsSingletonRow_NoNotFoundBranch(t *testing.T) {
	repo, _ := newCompanySettingsTestRepo(t)

	settings, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if settings.Name != "" {
		t.Errorf("Name = %q, want \"\"", settings.Name)
	}
	if settings.ContactEmail != "" {
		t.Errorf("ContactEmail = %q, want \"\"", settings.ContactEmail)
	}
	if settings.LogoURL != nil {
		t.Errorf("LogoURL = %q, want nil", *settings.LogoURL)
	}
}

// TestCompanySettingsRepository_Update_PersistsNameAndContactEmail asserts
// SET-01: Update persists name/contact_email and returns the updated row.
func TestCompanySettingsRepository_Update_PersistsNameAndContactEmail(t *testing.T) {
	repo, _ := newCompanySettingsTestRepo(t)

	updated, err := repo.Update(context.Background(), "Acme Inc.", "owner@acme.example.com")
	if err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}
	if updated.Name != "Acme Inc." {
		t.Errorf("Name = %q, want %q", updated.Name, "Acme Inc.")
	}
	if updated.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail = %q, want %q", updated.ContactEmail, "owner@acme.example.com")
	}

	// Confirms persistence via a fresh Get, not just the returned value.
	fetched, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if fetched.Name != "Acme Inc." {
		t.Errorf("persisted Name = %q, want %q", fetched.Name, "Acme Inc.")
	}
	if fetched.ContactEmail != "owner@acme.example.com" {
		t.Errorf("persisted ContactEmail = %q, want %q", fetched.ContactEmail, "owner@acme.example.com")
	}
}

// TestCompanySettingsRepository_UpdateLogoURL_PersistsIndependentlyOfUpdate
// asserts design.md's UpdateLogoURL contract: it persists logo_url without
// touching name/contact_email, and vice versa - the two mutations are
// independent (SET-07 groundwork).
func TestCompanySettingsRepository_UpdateLogoURL_PersistsIndependentlyOfUpdate(t *testing.T) {
	repo, _ := newCompanySettingsTestRepo(t)

	if _, err := repo.Update(context.Background(), "Acme Inc.", "owner@acme.example.com"); err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}

	updated, err := repo.UpdateLogoURL(context.Background(), "/uploads/logo.png")
	if err != nil {
		t.Fatalf("UpdateLogoURL() returned unexpected error: %v", err)
	}
	if updated.LogoURL == nil || *updated.LogoURL != "/uploads/logo.png" {
		t.Fatalf("LogoURL = %v, want %q", updated.LogoURL, "/uploads/logo.png")
	}
	// name/contact_email untouched by UpdateLogoURL.
	if updated.Name != "Acme Inc." {
		t.Errorf("Name = %q, want unchanged %q", updated.Name, "Acme Inc.")
	}
	if updated.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail = %q, want unchanged %q", updated.ContactEmail, "owner@acme.example.com")
	}

	fetched, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if fetched.LogoURL == nil || *fetched.LogoURL != "/uploads/logo.png" {
		t.Fatalf("persisted LogoURL = %v, want %q", fetched.LogoURL, "/uploads/logo.png")
	}
}
