//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
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

	// This reset races internal/api's and internal/cli's own
	// company_settings tests across the separate concurrent processes
	// `go test ./...` runs them as, so take the shared advisory lock for
	// the duration of this test - see LockCompanySettings' doc comment.
	// Deliberately context.Background(), not the bounded `ctx` above,
	// which is canceled by the deferred cancel() as soon as this
	// function returns.
	dbtest.LockCompanySettings(t, context.Background(), dsn)

	// Reset the singleton row to a known state before each test - other
	// tests in this suite/package mutate the same row (there is only one).
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_data = NULL, logo_content_type = NULL WHERE id = 1")
	})
	if _, err := pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_data = NULL, logo_content_type = NULL WHERE id = 1"); err != nil {
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
	if settings.LogoContentType != nil {
		t.Errorf("LogoContentType = %q, want nil", *settings.LogoContentType)
	}
	if settings.LogoServedURL() != nil {
		t.Errorf("LogoServedURL() = %v, want nil", *settings.LogoServedURL())
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

// TestCompanySettingsRepository_UpdateLogo_PersistsIndependentlyOfUpdate
// asserts design.md's UpdateLogo contract: it persists the logo without
// touching name/contact_email, and vice versa - the two mutations are
// independent (SET-07 groundwork).
func TestCompanySettingsRepository_UpdateLogo_PersistsIndependentlyOfUpdate(t *testing.T) {
	repo, _ := newCompanySettingsTestRepo(t)

	if _, err := repo.Update(context.Background(), "Acme Inc.", "owner@acme.example.com"); err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}

	updated, err := repo.UpdateLogo(context.Background(), "image/png", []byte("fake-png-bytes"))
	if err != nil {
		t.Fatalf("UpdateLogo() returned unexpected error: %v", err)
	}
	if updated.LogoServedURL() == nil || *updated.LogoServedURL() != "/uploads/logo" {
		t.Fatalf("LogoServedURL() = %v, want %q", updated.LogoServedURL(), "/uploads/logo")
	}
	// name/contact_email untouched by UpdateLogo.
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
	if fetched.LogoServedURL() == nil || *fetched.LogoServedURL() != "/uploads/logo" {
		t.Fatalf("persisted LogoServedURL() = %v, want %q", fetched.LogoServedURL(), "/uploads/logo")
	}

	// The bytes/content type themselves round-trip via GetLogo (Get above
	// deliberately never fetches them, so this is the one path that does).
	contentType, data, found, err := repo.GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatal("GetLogo() found = false, want true")
	}
	if contentType != "image/png" {
		t.Errorf("GetLogo() contentType = %q, want %q", contentType, "image/png")
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("GetLogo() data = %q, want %q", data, "fake-png-bytes")
	}
}

// TestCompanySettingsRepository_GetLogo_NeverUploaded_NotFound asserts the
// fresh-install state: no logo has ever been uploaded, so GetLogo reports
// found=false rather than a zero-length byte slice a caller could
// mistakenly serve as a 200.
func TestCompanySettingsRepository_GetLogo_NeverUploaded_NotFound(t *testing.T) {
	repo, _ := newCompanySettingsTestRepo(t)

	_, _, found, err := repo.GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if found {
		t.Error("GetLogo() found = true, want false (no logo ever uploaded)")
	}
}
