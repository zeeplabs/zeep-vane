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
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func newPollerStatusRouter(t *testing.T) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()
	dsn := testDatabaseURL(t)

	pool, _ := newAPITenantScopedPool(t)
	// Use context.Background() here, not the bounded ctx above: this lock
	// may need to wait for another concurrently-run package's test to
	// release the same advisory key, and a 5s setup deadline is
	// unrelated to (and shorter than) how long that wait can legitimately
	// take. Mirrors the pattern already used by
	// public_status_handler_test.go and poller_test.go's own
	// LockDatadogIntegration calls.
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	admins := db.NewUserRepository(pool)
	integrations := db.NewIntegrationRepository(pool)
	handler := NewPollerStatusHandler(integrations, db.NewPollerLeadershipRepository(pool), db.NewStatusIntervalRepository(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		// Mirrors buildAdminRouter: TenantContext runs right after
		// RequireAuth and is what resolves the caller's role in the active
		// tenant for RequireRole (multi-tenancy-core, AD-022).
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
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

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	got, ok := findPollerStatus(resp.Items, "datadog")
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

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	got, ok := findPollerStatus(resp.Items, "datadog")
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

func TestPollerStatus_InvalidPage_ClampsToPage1(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	req := httptest.NewRequest(http.MethodGet, "/api/poller/status?page=abc", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Page != 1 {
		t.Errorf("resp.Page = %d, want 1 (clamped from invalid ?page=abc)", resp.Page)
	}
	if resp.PageSize != 20 {
		t.Errorf("resp.PageSize = %d, want 20", resp.PageSize)
	}
}

func TestPollerStatus_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	req := httptest.NewRequest(http.MethodGet, "/api/poller/status?page=999999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Errorf("len(resp.Items) = %d, want 0 for a page far beyond the last", len(resp.Items))
	}
}

// acquirePollerLeaderLock holds db.PollerLeaderLockKey on a dedicated
// connection with the given application_name, simulating a replica that
// currently leads the poller (POLLST-01). Released via t.Cleanup.
func acquirePollerLeaderLock(t *testing.T, dsn, applicationName string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgx.ParseConfig() returned unexpected error: %v", err)
	}
	cfg.RuntimeParams["application_name"] = applicationName

	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgx.ConnectConfig() returned unexpected error: %v", err)
	}

	var acquired bool
	if err := conn.QueryRow(context.Background(), "SELECT pg_try_advisory_lock($1)", db.PollerLeaderLockKey).Scan(&acquired); err != nil {
		t.Fatalf("pg_try_advisory_lock() returned unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("pg_try_advisory_lock() = false, want true (nothing else holds this key)")
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", db.PollerLeaderLockKey)
		_ = conn.Close(context.Background())
	})
}

// TestPollerStatus_NoLeader_ReportsFalseFalseNilReplica covers POLLST-04:
// when nobody holds the leadership lock, the response reports
// leader_elected: false, poller_running: false, replica: null.
func TestPollerStatus_NoLeader_ReportsFalseFalseNilReplica(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getPollerStatus(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.LeaderElected {
		t.Error("LeaderElected = true, want false")
	}
	if resp.PollerRunning {
		t.Error("PollerRunning = true, want false")
	}
	if resp.Replica != nil {
		t.Errorf("Replica = %+v, want nil", resp.Replica)
	}
}

// TestPollerStatus_LeaderElectedNoIntegration_PollerRunningFalse covers
// POLLST-03: a held lock with no stored Datadog integration reports
// leader_elected: true, poller_running: false, and the replica's identity.
func TestPollerStatus_LeaderElectedNoIntegration_PollerRunningFalse(t *testing.T) {
	r, _, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)
	acquirePollerLeaderLock(t, testDatabaseURL(t), "poller-status-test-replica-no-integration")

	rec := getPollerStatus(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if !resp.LeaderElected {
		t.Error("LeaderElected = false, want true")
	}
	if resp.PollerRunning {
		t.Error("PollerRunning = true, want false (no stored Datadog integration)")
	}
	if resp.Replica == nil || resp.Replica.ApplicationName != "poller-status-test-replica-no-integration" {
		t.Errorf("Replica = %+v, want application_name %q", resp.Replica, "poller-status-test-replica-no-integration")
	}
	if resp.Replica != nil && resp.Replica.BackendStart.IsZero() {
		t.Error("Replica.BackendStart is zero, want a real timestamp")
	}
}

// TestPollerStatus_LeaderElectedWithIntegration_PollerRunningTrue covers
// POLLST-02: a held lock plus a stored, connected Datadog integration
// reports poller_running: true.
func TestPollerStatus_LeaderElectedWithIntegration_PollerRunningTrue(t *testing.T) {
	r, pool, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)
	acquirePollerLeaderLock(t, testDatabaseURL(t), "poller-status-test-replica-with-integration")

	integrations := db.NewIntegrationRepository(pool)
	if err := integrations.UpsertDatadog(context.Background(), []byte("enc-api-key"), []byte("enc-app-key")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}

	rec := getPollerStatus(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp pollerStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if !resp.LeaderElected {
		t.Error("LeaderElected = false, want true")
	}
	if !resp.PollerRunning {
		t.Error("PollerRunning = false, want true (leader elected and integration stored)")
	}
}

// TestPollerStatus_ChecksLastMinute_ReflectsRecentStatusIntervalActivity
// covers POLLST-05/06: checks_last_minute increases when a status_intervals
// row is written inside the last-minute window. Compares before/after
// rather than an absolute count, since this is a table-wide (tenant-scoped)
// count, not scoped to one service.
func TestPollerStatus_ChecksLastMinute_ReflectsRecentStatusIntervalActivity(t *testing.T) {
	r, pool, admins := newPollerStatusRouter(t)
	token := issueTestSessionToken(t, admins)

	before := getPollerStatus(t, r, token)
	if before.Code != http.StatusOK {
		t.Fatalf("status (before) = %d, want %d, body = %s", before.Code, http.StatusOK, before.Body.String())
	}
	var beforeResp pollerStatusResponse
	if err := json.Unmarshal(before.Body.Bytes(), &beforeResp); err != nil {
		t.Fatalf("json.Unmarshal() (before) returned unexpected error: %v", err)
	}

	tenantID := seedTestTenant(t, pool)
	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: uniqueServiceName(t), SLOID: "slo-poller-status-checks-test"}
	withTenantTx(t, pool, tenantID, func(ctx context.Context) {
		if err := services.Create(ctx, service); err != nil {
			t.Fatalf("services.Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })

	intervals := db.NewStatusIntervalRepository(pool)
	if err := intervals.OpenOrExtend(context.Background(), service.ID, "operational", 95.0, time.Now()); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}

	after := getPollerStatus(t, r, token)
	if after.Code != http.StatusOK {
		t.Fatalf("status (after) = %d, want %d, body = %s", after.Code, http.StatusOK, after.Body.String())
	}
	var afterResp pollerStatusResponse
	if err := json.Unmarshal(after.Body.Bytes(), &afterResp); err != nil {
		t.Fatalf("json.Unmarshal() (after) returned unexpected error: %v", err)
	}

	if afterResp.ChecksLastMinute != beforeResp.ChecksLastMinute+1 {
		t.Errorf("ChecksLastMinute went from %d to %d, want +1", beforeResp.ChecksLastMinute, afterResp.ChecksLastMinute)
	}
}
