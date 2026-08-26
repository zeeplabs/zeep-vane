package config

import "testing"

func setAllRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/vane")
	t.Setenv("VANE_MASTER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("VANE_SESSION_SECRET", "test-session-secret-at-least-32-bytes!!")
	t.Setenv("PORT", "8080")
	t.Setenv("POLL_INTERVAL_SECONDS", "120")
}

func TestLoad_AllVarsPresent_Success(t *testing.T) {
	setAllRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/vane" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@localhost:5432/vane")
	}
	if cfg.MasterKey != "0123456789abcdef0123456789abcdef" {
		t.Errorf("MasterKey = %q, want %q", cfg.MasterKey, "0123456789abcdef0123456789abcdef")
	}
	if cfg.SessionSecret != "test-session-secret-at-least-32-bytes!!" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "test-session-secret-at-least-32-bytes!!")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.PollIntervalSeconds != 120 {
		t.Errorf("PollIntervalSeconds = %d, want 120", cfg.PollIntervalSeconds)
	}
}

// TestLoad_MasterKeyTooShort_Error asserts a VANE_MASTER_KEY shorter than
// 32 characters is rejected instead of silently accepted as a weak key.
func TestLoad_MasterKeyTooShort_Error(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_MASTER_KEY", "too-short")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with a short VANE_MASTER_KEY returned nil error, want error")
	}
}

// TestLoad_SessionSecretTooShort_Error mirrors the master-key case for
// VANE_SESSION_SECRET.
func TestLoad_SessionSecretTooShort_Error(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_SESSION_SECRET", "too-short")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with a short VANE_SESSION_SECRET returned nil error, want error")
	}
}

// TestLoad_KnownPlaceholderSecret_Error asserts the example values shipped
// in this repo's own docker-compose.yml are rejected, even though they are
// individually long enough to pass the length check - a deploy must never
// be able to start with a secret that is public on GitHub.
func TestLoad_KnownPlaceholderSecret_Error(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{"master key placeholder", "VANE_MASTER_KEY", "change-me-master-key"},
		{"session secret placeholder", "VANE_SESSION_SECRET", "change-me-session-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAllRequiredEnv(t)
			t.Setenv(tt.env, tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned nil error, want error", tt.env, tt.value)
			}
		})
	}
}

func TestLoad_MissingRequiredVar_Error(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with missing DATABASE_URL returned nil error, want error")
	}
}

func TestLoad_InvalidPollIntervalFormat_Error(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("POLL_INTERVAL_SECONDS", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with non-numeric POLL_INTERVAL_SECONDS returned nil error, want error")
	}
}

// TestLoad_UploadsDirSet_UsesGivenValue asserts SET-11: UPLOADS_DIR, when
// set, is reflected verbatim in Config.UploadsDir.
func TestLoad_UploadsDirSet_UsesGivenValue(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("UPLOADS_DIR", "/mnt/vane-uploads")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.UploadsDir != "/mnt/vane-uploads" {
		t.Errorf("UploadsDir = %q, want %q", cfg.UploadsDir, "/mnt/vane-uploads")
	}
}

// TestLoad_UploadsDirUnset_DefaultsToDataUploads asserts SET-11's default:
// when UPLOADS_DIR is unset, Config.UploadsDir falls back to
// "./data/uploads" so local dev works without any extra setup.
func TestLoad_UploadsDirUnset_DefaultsToDataUploads(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("UPLOADS_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.UploadsDir != "./data/uploads" {
		t.Errorf("UploadsDir = %q, want %q", cfg.UploadsDir, "./data/uploads")
	}
}

// TestLoad_PublicDNSTargetSet_UsesGivenValue asserts SPD-10: PUBLIC_DNS_TARGET,
// when set, is reflected verbatim in Config.PublicDNSTarget.
func TestLoad_PublicDNSTargetSet_UsesGivenValue(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("PUBLIC_DNS_TARGET", "vane.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.PublicDNSTarget != "vane.example.com" {
		t.Errorf("PublicDNSTarget = %q, want %q", cfg.PublicDNSTarget, "vane.example.com")
	}
}

// TestLoad_HTTPSEnabledUnset_DefaultsToTrue asserts the on-demand-TLS
// listener still starts by default (H8 fix must not change existing
// deploys that rely on custom status-page domains without setting
// anything new).
func TestLoad_HTTPSEnabledUnset_DefaultsToTrue(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_HTTPS_ENABLED", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if !cfg.HTTPSEnabled {
		t.Error("HTTPSEnabled = false, want true when VANE_HTTPS_ENABLED is unset")
	}
}

// TestLoad_HTTPSEnabledFalse_DisablesHTTPS asserts an operator can opt out
// of the HTTPS listener explicitly (H8: no way to bind :443 - or no custom
// domain to serve - must not be fatal).
func TestLoad_HTTPSEnabledFalse_DisablesHTTPS(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("VANE_HTTPS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.HTTPSEnabled {
		t.Error("HTTPSEnabled = true, want false when VANE_HTTPS_ENABLED=false")
	}
}

// TestLoad_PublicDNSTargetUnset_DefaultsToEmptyString asserts SPD-10's
// default: when PUBLIC_DNS_TARGET is unset, Config.PublicDNSTarget is ""
// (the operator hasn't configured this value), not an error.
func TestLoad_PublicDNSTargetUnset_DefaultsToEmptyString(t *testing.T) {
	setAllRequiredEnv(t)
	t.Setenv("PUBLIC_DNS_TARGET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.PublicDNSTarget != "" {
		t.Errorf("PublicDNSTarget = %q, want empty string", cfg.PublicDNSTarget)
	}
}
