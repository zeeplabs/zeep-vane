package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// TestHealthcheckCmd_HealthyListener_Succeeds and
// TestHealthcheckCmd_Non200_Fails exercise RunE directly against a real
// httptest listener bound to the port config.Load() will read from PORT -
// httptest.NewServer picks its own port, so these bind an
// httptest.Server-backed listener via a fixed local port derived from the
// test server's own address, keeping the whole thing self-contained without
// starting a real `vane serve`.
func TestHealthcheckCmd_HealthyListener_Succeeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := testServerPort(t, srv.URL)
	setHealthcheckRequiredEnv(t, port)

	cmd := NewHealthcheckCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("RunE() returned unexpected error: %v", err)
	}
}

func TestHealthcheckCmd_Non200_Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	port := testServerPort(t, srv.URL)
	setHealthcheckRequiredEnv(t, port)

	cmd := NewHealthcheckCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("RunE() returned nil error for a non-200 /healthz response, want an error")
	}
}

func TestHealthcheckCmd_NothingListening_Fails(t *testing.T) {
	setHealthcheckRequiredEnv(t, 1) // port 1 - nothing listens there

	cmd := NewHealthcheckCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("RunE() returned nil error with nothing listening, want a connection error")
	}
}

func testServerPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse() returned unexpected error: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("strconv.Atoi() returned unexpected error: %v", err)
	}
	return port
}

// setHealthcheckRequiredEnv sets every env var config.Load() requires, so
// RunE's call to it succeeds - only PORT actually matters to this command.
func setHealthcheckRequiredEnv(t *testing.T, port int) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/vane")
	t.Setenv("VANE_MASTER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("VANE_SESSION_SECRET", "test-session-secret-at-least-32-bytes!!")
	t.Setenv("PORT", strconv.Itoa(port))
	t.Setenv("POLL_INTERVAL_SECONDS", "60")
}
