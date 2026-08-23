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
