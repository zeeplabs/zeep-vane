package config

import "testing"

func TestLoad_EmailAssetsBaseURLUnset_DefaultsToAdminBaseURLEmailPath(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_ADMIN_BASE_URL", "https://admin.example.com/")
	t.Setenv("VANE_EMAIL_ASSETS_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.EmailAssetsBaseURL != "https://admin.example.com/email" {
		t.Errorf("EmailAssetsBaseURL = %q, want the admin host's /email path", cfg.EmailAssetsBaseURL)
	}
}

func TestLoad_EmailAssetsBaseURLAndAdminBaseURLUnset_Empty(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_ADMIN_BASE_URL", "")
	t.Setenv("VANE_EMAIL_ASSETS_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.EmailAssetsBaseURL != "" {
		t.Errorf("EmailAssetsBaseURL = %q, want empty (logo omitted) with no base URL configured", cfg.EmailAssetsBaseURL)
	}
}

func TestLoad_EmailVarsSet_UsesGivenValues(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_ADMIN_BASE_URL", "https://admin.example.com")
	t.Setenv("VANE_EMAIL_ASSETS_BASE_URL", "https://cdn.example.com/vane/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.EmailAssetsBaseURL != "https://cdn.example.com/vane" {
		t.Errorf("EmailAssetsBaseURL = %q, want the override with its trailing slash trimmed", cfg.EmailAssetsBaseURL)
	}
}

func TestLoad_EmailURLsInvalid_ReturnError(t *testing.T) {
	for _, name := range []string{"VANE_EMAIL_ASSETS_BASE_URL"} {
		for _, value := range []string{"cdn.example.com", "https://", "javascript:alert(1)", "https://x.example.com?a=b"} {
			t.Run(name+"="+value, func(t *testing.T) {
				setAllRequiredEnv(t)
				t.Setenv(name, value)
				if _, err := Load(); err == nil {
					t.Errorf("Load() with %s=%q returned nil error, want a boot failure", name, value)
				}
			})
		}
	}
}
