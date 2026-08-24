//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func newPollerStatusRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	// Use context.Background() here, not the bounded ctx above: this lock
	// may need to wait for another concurrently-run package's test to
	// release the same advisory key, and a 5s setup deadline is
	// unrelated to (and shorter than) how long that wait can legitimately
	// take. Mirrors the pattern already used by
	// public_status_handler_test.go and poller_test.go's own
	// LockDatadogIntegration calls.
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	admins := db.NewAdminRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	handler := NewPollerStatusHandler(integrations, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.With(RequireRole(db.RoleOwner, db.RoleOperator, db.RoleViewer)).Get("/api/poller/status", handler.List)
	})

	return r, pool, admins
}

func getPollerStatus(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/poller/status", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func findPollerStatus(list []pollerIntegrationStatus, provider string) (pollerIntegrationStatus, bool) {
	for _, item := range list {
		if item.Provider == provider {
			return item, true
		}
	}
	return pollerIntegrationStatus{}, false
}

func TestPollerStatus_SuccessfulIntegration_ReflectsPersistedState(t *testing.T) {
	r, pool, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	integrations := db.NewIntegrationRepository(pool)
	if err := integrations.UpsertDatadog(context.Background(), []byte("enc-api-key"), []byte("enc-app-key")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}

	rec := getPollerStatus(t, r, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var list []pollerIntegrationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	got, ok := findPollerStatus(list, "datadog")
	if !ok {
		t.Fatalf("response missing datadog integration: %s", rec.Body.String())
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if got.LastError != nil {
		t.Errorf("LastError = %v, want nil (no failure recorded)", got.LastError)
	}
}

func TestPollerStatus_PersistedFailure_ReflectsInvalidStatusAndError(t *testing.T) {
	r, pool, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	integrations := db.NewIntegrationRepository(pool)
	if err := integrations.UpsertDatadog(context.Background(), []byte("enc-api-key"), []byte("enc-app-key")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}
	const failureReason = "datadog: unauthorized"
	if err := integrations.MarkDatadogInvalid(context.Background(), failureReason); err != nil {
		t.Fatalf("MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	rec := getPollerStatus(t, r, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var list []pollerIntegrationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	got, ok := findPollerStatus(list, "datadog")
	if !ok {
		t.Fatalf("response missing datadog integration: %s", rec.Body.String())
	}
	if got.Status != "invalid" {
		t.Errorf("Status = %q, want %q", got.Status, "invalid")
	}
	if got.LastError == nil || *got.LastError != failureReason {
		t.Errorf("LastError = %v, want %q", got.LastError, failureReason)
	}
	if got.LastCheckedAt == nil {
		t.Error("LastCheckedAt = nil, want a timestamp (poller recorded the failed attempt)")
	}
}

func TestPollerStatus_Owner_200(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleOwner)

	rec := getPollerStatus(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPollerStatus_Operator_200(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleOperator)

	rec := getPollerStatus(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPollerStatus_Viewer_200(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionTokenWithRole(t, admins, db.RoleViewer)

	rec := getPollerStatus(t, r, token)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
