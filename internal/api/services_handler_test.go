//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newServicesRouter(t *testing.T) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()

	pool, _ := newAPITenantScopedPool(t)

	repo := db.NewServiceRepository(pool)
	admins := db.NewUserRepository(pool)
	handler := NewServicesHandler(repo, db.NewStatusIntervalRepository(pool), db.NewIncidentRepository(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		// Mirrors buildAdminRouter: TenantContext runs right after
		// RequireAuth and is what resolves the caller's role in the active
		// tenant for RequireRole (multi-tenancy-core, AD-022).
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		// TenantContext must run (services now carries tenant_id + RLS,
		// 0024) for Create/List to see anything at all - matches
		// production wiring in routes.go.
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Post("/api/services", handler.Create)
		protected.Get("/api/services", handler.List)
		protected.Get("/api/services/{id}", handler.Get)
	})

	return r, pool, admins
}

func uniqueServiceName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("services-handler-test-%d", time.Now().UnixNano())
}

// issueTestSessionTokenWithTenant is issueTestSessionToken plus a tenant +
// owner membership for the new admin, and issues the token with that
// tenant active - the shape TenantContext needs to let Create/List
// actually see rows (0024's RLS policy on services). Kept local to this
// file rather than folded into the shared issueTestSessionToken (used by
// ~10 other handler test files whose tables aren't tenant-scoped in this
// batch).
func issueTestSessionTokenWithTenant(t *testing.T, admins *db.UserRepository, pool *db.Pool) string {
	t.Helper()
	_ = pool
	return seedSessionForRole(t, admins, db.RoleOwner)
}

func postCreateService(t *testing.T, r http.Handler, token, name, sloID string) *httptest.ResponseRecorder {
	t.Helper()
	return postCreateServiceWithSLOName(t, r, token, name, sloID, "")
}

func postCreateServiceWithSLOName(t *testing.T, r http.Handler, token, name, sloID, sloName string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(createServiceRequest{Name: name, SLOID: sloID, SLOName: sloName})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// postCreateServiceRaw posts an arbitrary raw JSON body to POST
// /api/services, for validation tests that need to omit a field entirely
// rather than send it as "".
func postCreateServiceRaw(t *testing.T, r http.Handler, token, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// postCreateServicePolling posts a monitor_mode="polling" create request.
func postCreateServicePolling(t *testing.T, r http.Handler, token, name, pollType, pollTarget string, pollIntervalSeconds int) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(createServiceRequest{
		Name:                name,
		MonitorMode:         "polling",
		PollType:            pollType,
		PollTarget:          pollTarget,
		PollIntervalSeconds: pollIntervalSeconds,
	})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/services", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// countServicesNamed returns how many services rows currently exist with
// name - used to assert a rejected create persisted nothing.
func countServicesNamed(t *testing.T, pool *db.Pool, name string) int {
	t.Helper()
	var count int
	row := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM services WHERE name = $1", name)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	return count
}

func getServices(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getServicesPage(t *testing.T, r http.Handler, token string, page int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/services?page=%d", page), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getServiceDetail(t *testing.T, r http.Handler, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/services/"+id, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// findServiceAcrossPages pages through every page of GET /api/services
// looking for name - the shared dev DB this integration suite runs against
// can accumulate services across many tests, so page 1 alone is not
// guaranteed to include one created just now (same reasoning as domains
// handler's findDomainAcrossPages, PAG-08).
func findServiceAcrossPages(t *testing.T, r http.Handler, token, name string) *serviceResponse {
	t.Helper()
	for page := 1; ; page++ {
		rec := getServicesPage(t, r, token, page)
		if rec.Code != http.StatusOK {
			t.Fatalf("page=%d status = %d, want %d, body = %s", page, rec.Code, http.StatusOK, rec.Body.String())
		}
		var got Page[serviceResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
		}
		for i := range got.Items {
			if got.Items[i].Name == name {
				return &got.Items[i]
			}
		}
		if len(got.Items) == 0 || page*got.PageSize >= got.Total {
			return nil
		}
	}
}

func TestCreateService_ValidRequest_201SavesSLOLink(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateService(t, r, token, name, "slo-abc-123")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created serviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.SLOID != "slo-abc-123" {
		t.Errorf("response SLOID = %q, want %q", created.SLOID, "slo-abc-123")
	}

	var storedSLOID string
	row := pool.QueryRow(context.Background(), "SELECT slo_id FROM services WHERE name = $1", name)
	if err := row.Scan(&storedSLOID); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if storedSLOID != "slo-abc-123" {
		t.Errorf("stored slo_id = %q, want %q", storedSLOID, "slo-abc-123")
	}
}

func TestListServices_ReturnsAllWithCurrentStatus(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	createRec := postCreateService(t, r, token, name, "slo-xyz-789")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	rec := getServices(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (default)", page.Page)
	}
	if page.PageSize != 20 {
		t.Errorf("page.PageSize = %d, want 20", page.PageSize)
	}

	found := findServiceAcrossPages(t, r, token, name)
	if found == nil {
		t.Fatalf("created service %q not present across any page of GET /api/services", name)
	}
	if found.CurrentStatus != "not_configured" {
		t.Errorf("CurrentStatus = %q, want %q", found.CurrentStatus, "not_configured")
	}
}

func TestListServices_InvalidPage_ClampsToPage1(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getServicesPage(t, r, token, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if page.Page != 1 {
		t.Errorf("page.Page = %d, want 1 (clamped from invalid ?page=0)", page.Page)
	}
}

func TestListServices_PageBeyondLast_EmptyItems200(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getServicesPage(t, r, token, 999999)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var page Page[serviceResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("len(page.Items) = %d, want 0 for a page far beyond the last", len(page.Items))
	}
}

// TestCreateService_WithSLOName_PersistsAndReturnsIt asserts T4/SVC-01:
// Create accepts slo_name in the request body, persists it, and returns it
// in the response.
func TestCreateService_WithSLOName_PersistsAndReturnsIt(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateServiceWithSLOName(t, r, token, name, "slo-name-test-1", "Checkout latency SLO")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created serviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if created.SLOName != "Checkout latency SLO" {
		t.Errorf("response SLOName = %q, want %q", created.SLOName, "Checkout latency SLO")
	}

	var storedSLOName string
	row := pool.QueryRow(context.Background(), "SELECT slo_name FROM services WHERE name = $1", name)
	if err := row.Scan(&storedSLOName); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if storedSLOName != "Checkout latency SLO" {
		t.Errorf("stored slo_name = %q, want %q", storedSLOName, "Checkout latency SLO")
	}
}

// TestCreateService_MissingName_422 asserts T4's "no relaxation of existing
// validation" criterion: an omitted name still 422s exactly as before,
// unaffected by slo_name becoming an accepted field.
func TestCreateService_MissingName_422(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateServiceRaw(t, r, token, `{"slo_id":"slo-missing-name"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestCreateService_MissingSLOID_422 asserts T4's "no relaxation of
// existing validation" criterion: an omitted slo_id still 422s exactly as
// before.
func TestCreateService_MissingSLOID_422(t *testing.T) {
	r, _, admins := newServicesRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := postCreateServiceRaw(t, r, token, `{"name":"missing-slo-id"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestCreateService_OmittedMonitorMode_DefaultsToSLO_UnchangedBehavior
// asserts T5's "Done when": omitting monitor_mode behaves exactly as
// before - the created row's monitor_mode column is "slo".
func TestCreateService_OmittedMonitorMode_DefaultsToSLO_UnchangedBehavior(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateService(t, r, token, name, "slo-omitted-mode")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var monitorMode string
	row := pool.QueryRow(context.Background(), "SELECT monitor_mode FROM services WHERE name = $1", name)
	if err := row.Scan(&monitorMode); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if monitorMode != "slo" {
		t.Errorf("stored monitor_mode = %q, want %q", monitorMode, "slo")
	}
}

// TestCreateService_PollingMode_HTTP_201PersistsFields asserts MP-01/MP-02:
// a valid polling-mode HTTP(S) request creates the service with the right
// fields persisted, and slo_id stays unset.
func TestCreateService_PollingMode_HTTP_201PersistsFields(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-http"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateServicePolling(t, r, token, name, "http", "https://polling-http-test.invalid/health", 30)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var monitorMode, pollType, pollTarget string
	var pollIntervalSeconds int
	var sloID *string
	row := pool.QueryRow(context.Background(),
		"SELECT monitor_mode, slo_id, poll_type, poll_target, poll_interval_seconds FROM services WHERE name = $1", name)
	if err := row.Scan(&monitorMode, &sloID, &pollType, &pollTarget, &pollIntervalSeconds); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if monitorMode != "polling" {
		t.Errorf("stored monitor_mode = %q, want %q", monitorMode, "polling")
	}
	if sloID != nil {
		t.Errorf("stored slo_id = %v, want nil", sloID)
	}
	if pollType != "http" {
		t.Errorf("stored poll_type = %q, want %q", pollType, "http")
	}
	if pollTarget != "https://polling-http-test.invalid/health" {
		t.Errorf("stored poll_target = %q, want %q", pollTarget, "https://polling-http-test.invalid/health")
	}
	if pollIntervalSeconds != 30 {
		t.Errorf("stored poll_interval_seconds = %d, want %d", pollIntervalSeconds, 30)
	}
}

// TestCreateService_PollingMode_TCP_201PersistsFields asserts MP-01/MP-02
// for the TCP check type.
func TestCreateService_PollingMode_TCP_201PersistsFields(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-tcp"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateServicePolling(t, r, token, name, "tcp", "polling-tcp-test.invalid:5432", 60)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var monitorMode, pollType, pollTarget string
	row := pool.QueryRow(context.Background(),
		"SELECT monitor_mode, poll_type, poll_target FROM services WHERE name = $1", name)
	if err := row.Scan(&monitorMode, &pollType, &pollTarget); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if monitorMode != "polling" || pollType != "tcp" || pollTarget != "polling-tcp-test.invalid:5432" {
		t.Errorf("stored (monitor_mode, poll_type, poll_target) = (%q, %q, %q), want (%q, %q, %q)",
			monitorMode, pollType, pollTarget, "polling", "tcp", "polling-tcp-test.invalid:5432")
	}
}

// TestCreateService_PollingMode_Ping_201PersistsFields asserts MP-01/MP-02
// for the Ping check type.
func TestCreateService_PollingMode_Ping_201PersistsFields(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-ping"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	rec := postCreateServicePolling(t, r, token, name, "ping", "polling-ping-test.invalid", 300)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var monitorMode, pollType, pollTarget string
	var pollIntervalSeconds int
	row := pool.QueryRow(context.Background(),
		"SELECT monitor_mode, poll_type, poll_target, poll_interval_seconds FROM services WHERE name = $1", name)
	if err := row.Scan(&monitorMode, &pollType, &pollTarget, &pollIntervalSeconds); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if monitorMode != "polling" || pollType != "ping" || pollTarget != "polling-ping-test.invalid" || pollIntervalSeconds != 300 {
		t.Errorf("stored row = (%q, %q, %q, %d), want (%q, %q, %q, %d)",
			monitorMode, pollType, pollTarget, pollIntervalSeconds, "polling", "ping", "polling-ping-test.invalid", 300)
	}
}

// TestCreateService_PollingMode_SSRFBlockedTarget_422NoServiceCreated
// asserts MP-03: a polling-mode target resolving to a blocked IP range
// (a literal loopback address here) is rejected with 422 and nothing is
// persisted.
func TestCreateService_PollingMode_SSRFBlockedTarget_422NoServiceCreated(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-ssrf-blocked"

	rec := postCreateServicePolling(t, r, token, name, "http", "http://127.0.0.1/health", 30)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != invalidPollTargetBody {
		t.Errorf("body = %s, want the fixed generic %s (never a raw resolver error)", rec.Body.String(), invalidPollTargetBody)
	}

	if count := countServicesNamed(t, pool, name); count != 0 {
		t.Errorf("services rows named %q after a rejected create = %d, want 0", name, count)
	}
}

// TestCreateService_PollingMode_MalformedTarget_422NoServiceCreated asserts
// MP-04: a target whose format doesn't match its poll_type (a bare host for
// HTTP(S), which requires a full URL) is rejected with 422 and nothing is
// persisted.
func TestCreateService_PollingMode_MalformedTarget_422NoServiceCreated(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-malformed-target"

	rec := postCreateServicePolling(t, r, token, name, "http", "api.acme.health", 30)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != invalidPollTargetBody {
		t.Errorf("body = %s, want the fixed generic %s (never a raw parse error)", rec.Body.String(), invalidPollTargetBody)
	}

	if count := countServicesNamed(t, pool, name); count != 0 {
		t.Errorf("services rows named %q after a rejected create = %d, want 0", name, count)
	}
}

// TestCreateService_PollingModeWithSLOID_422NoServiceCreated asserts MP-05:
// a polling-mode request that also includes slo_id is rejected (422), and
// nothing is persisted.
func TestCreateService_PollingModeWithSLOID_422NoServiceCreated(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-polling-with-slo-id"

	body, err := json.Marshal(createServiceRequest{
		Name: name, SLOID: "slo-should-not-be-allowed",
		MonitorMode: "polling", PollType: "http", PollTarget: "https://polling-mixed-test.invalid/", PollIntervalSeconds: 30,
	})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	rec := postCreateServiceRaw(t, r, token, string(body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != mixedModeFieldsBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), mixedModeFieldsBody)
	}

	if count := countServicesNamed(t, pool, name); count != 0 {
		t.Errorf("services rows named %q after a rejected create = %d, want 0", name, count)
	}
}

// TestCreateService_SLOModeWithPollFields_422NoServiceCreated asserts
// MP-05: an slo-mode request that also includes poll_type/poll_target/
// poll_interval_seconds is rejected (422), and nothing is persisted.
func TestCreateService_SLOModeWithPollFields_422NoServiceCreated(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t) + "-slo-with-poll-fields"

	body, err := json.Marshal(createServiceRequest{
		Name: name, SLOID: "slo-mixed-1",
		PollType: "http", PollTarget: "https://polling-mixed-test.invalid/", PollIntervalSeconds: 30,
	})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	rec := postCreateServiceRaw(t, r, token, string(body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Body.String() != mixedModeFieldsBody {
		t.Errorf("body = %s, want %s", rec.Body.String(), mixedModeFieldsBody)
	}

	if count := countServicesNamed(t, pool, name); count != 0 {
		t.Errorf("services rows named %q after a rejected create = %d, want 0", name, count)
	}
}

// TestListServices_NeverPolled_UptimeAndLastSeenNil asserts SVC-01/SVC-06:
// a freshly created service with zero StatusInterval rows gets
// uptime_30d/last_seen_at nil in the list response, and its slo_name
// round-trips.
func TestListServices_NeverPolled_UptimeAndLastSeenNil(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", name) })

	createRec := postCreateServiceWithSLOName(t, r, token, name, "slo-never-polled", "Never Polled SLO")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d", createRec.Code, http.StatusCreated)
	}

	found := findServiceAcrossPages(t, r, token, name)
	if found == nil {
		t.Fatalf("created service %q not present across any page of GET /api/services", name)
	}
	if found.SLOName != "Never Polled SLO" {
		t.Errorf("SLOName = %q, want %q", found.SLOName, "Never Polled SLO")
	}
	if found.Uptime30d != nil {
		t.Errorf("Uptime30d = %v, want nil for a service with no StatusInterval rows", *found.Uptime30d)
	}
	if found.LastSeenAt != nil {
		t.Errorf("LastSeenAt = %v, want nil for a service with no StatusInterval rows", *found.LastSeenAt)
	}
}

// TestListServices_MixedNotConfiguredAndPolled_SamePage asserts the
// design.md Risks & Concerns row 3 regression: a not_configured service
// (zero StatusInterval rows) and a polled service (an open interval with
// real uptime data) in the SAME paginated response must each get their own
// correct uptime_30d/last_seen_at - not both nil, and not the polled
// service's data leaking onto the not_configured one.
func TestListServices_MixedNotConfiguredAndPolled_SamePage(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)

	notConfiguredName := uniqueServiceName(t) + "-not-configured"
	polledName := uniqueServiceName(t) + "-polled"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name IN ($1, $2)", notConfiguredName, polledName)
	})

	if rec := postCreateService(t, r, token, notConfiguredName, "slo-not-configured"); rec.Code != http.StatusCreated {
		t.Fatalf("setup create (not_configured) status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	polledRec := postCreateService(t, r, token, polledName, "slo-polled")
	if polledRec.Code != http.StatusCreated {
		t.Fatalf("setup create (polled) status = %d, want %d, body = %s", polledRec.Code, http.StatusCreated, polledRec.Body.String())
	}
	var polledCreated serviceResponse
	if err := json.Unmarshal(polledRec.Body.Bytes(), &polledCreated); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	intervals := db.NewStatusIntervalRepository(pool)
	polledAt := time.Now().Add(-time.Hour)
	if err := intervals.OpenOrExtend(context.Background(), polledCreated.ID, "operational", 100, polledAt); err != nil {
		t.Fatalf("setup OpenOrExtend() returned unexpected error: %v", err)
	}

	gotNotConfigured := findServiceAcrossPages(t, r, token, notConfiguredName)
	if gotNotConfigured == nil {
		t.Fatalf("not_configured service %q not present across any page of GET /api/services", notConfiguredName)
	}
	if gotNotConfigured.Uptime30d != nil {
		t.Errorf("not_configured Uptime30d = %v, want nil", *gotNotConfigured.Uptime30d)
	}
	if gotNotConfigured.LastSeenAt != nil {
		t.Errorf("not_configured LastSeenAt = %v, want nil", *gotNotConfigured.LastSeenAt)
	}

	gotPolled := findServiceAcrossPages(t, r, token, polledName)
	if gotPolled == nil {
		t.Fatalf("polled service %q not present across any page of GET /api/services", polledName)
	}
	if gotPolled.Uptime30d == nil {
		t.Fatalf("polled Uptime30d = nil, want a real percentage")
	}
	if *gotPolled.Uptime30d != 100 {
		t.Errorf("polled Uptime30d = %v, want 100 (fully operational open interval)", *gotPolled.Uptime30d)
	}
	if gotPolled.LastSeenAt == nil {
		t.Fatalf("polled LastSeenAt = nil, want the open interval's LastSeenAt")
	}
	// Allow for sub-second precision loss across the Postgres round-trip
	// and JSON (de)serialization rather than requiring bit-exact equality.
	if diff := gotPolled.LastSeenAt.Sub(polledAt); diff < -time.Second || diff > time.Second {
		t.Errorf("polled LastSeenAt = %v, want ~%v (within 1s)", *gotPolled.LastSeenAt, polledAt)
	}
}

func TestServicesRoutes_NoAuth_401(t *testing.T) {
	r, _, _ := newServicesRouter(t)

	createRec := postCreateService(t, r, "", "any-name", "any-slo")
	if createRec.Code != http.StatusUnauthorized {
		t.Errorf("POST status = %d, want %d", createRec.Code, http.StatusUnauthorized)
	}

	listRec := getServices(t, r, "")
	if listRec.Code != http.StatusUnauthorized {
		t.Errorf("GET status = %d, want %d", listRec.Code, http.StatusUnauthorized)
	}

	// T5/SVC-14..19: the new detail route requires authentication exactly
	// like List/Create, same anyRole posture.
	detailRec := getServiceDetail(t, r, "", "any-id")
	if detailRec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/services/{id} status = %d, want %d", detailRec.Code, http.StatusUnauthorized)
	}
}

// createServiceForDetail creates a service via POST /api/services and
// returns its response for detail-endpoint tests.
func createServiceForDetail(t *testing.T, r http.Handler, pool *db.Pool, token, name, sloID, sloName string) serviceResponse {
	t.Helper()
	rec := postCreateServiceWithSLOName(t, r, token, name, sloID, sloName)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var created serviceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", created.ID) })
	return created
}

// TestGetService_Found_ReturnsFullDetailDTO asserts SVC-14/SVC-17: an
// existing, never-polled service returns the full detail DTO with
// uptime_30d/last_seen_at nil, incidents_30d = 0, and exactly 24
// hourly_buckets.
func TestGetService_Found_ReturnsFullDetailDTO(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	created := createServiceForDetail(t, r, pool, token, name, "slo-detail-found", "Detail Found SLO")

	rec := getServiceDetail(t, r, token, created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var detail serviceDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if detail.ID != created.ID {
		t.Errorf("detail.ID = %q, want %q", detail.ID, created.ID)
	}
	if detail.SLOName != "Detail Found SLO" {
		t.Errorf("detail.SLOName = %q, want %q", detail.SLOName, "Detail Found SLO")
	}
	if detail.Uptime30d != nil {
		t.Errorf("detail.Uptime30d = %v, want nil for a never-polled service", *detail.Uptime30d)
	}
	if detail.LastSeenAt != nil {
		t.Errorf("detail.LastSeenAt = %v, want nil for a never-polled service", *detail.LastSeenAt)
	}
	if detail.Incidents30d != 0 {
		t.Errorf("detail.Incidents30d = %d, want 0", detail.Incidents30d)
	}
	if len(detail.HourlyBuckets) != 24 {
		t.Errorf("len(detail.HourlyBuckets) = %d, want 24", len(detail.HourlyBuckets))
	}
	if detail.StatusAnalysis != nil {
		t.Errorf("detail.StatusAnalysis = %v, want nil for a not_configured service", *detail.StatusAnalysis)
	}
}

// TestGetService_UnknownID_404FixedBody asserts SVC-14: an unknown ID
// returns 404 with the fixed generic body, never a leaked err.Error().
func TestGetService_UnknownID_404FixedBody(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)

	rec := getServiceDetail(t, r, token, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if rec.Body.String() != serviceNotFoundBody {
		t.Errorf("body = %q, want fixed generic body %q (no err.Error() leak)", rec.Body.String(), serviceNotFoundBody)
	}
}

// TestGetService_ZeroHistory_Exactly24BucketsAllNoData asserts SVC-17:
// hourly_buckets always has exactly 24 entries, even for a service with
// zero StatusInterval rows - each bucket reporting "no_data".
func TestGetService_ZeroHistory_Exactly24BucketsAllNoData(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	created := createServiceForDetail(t, r, pool, token, name, "slo-detail-zero-history", "")

	rec := getServiceDetail(t, r, token, created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var detail serviceDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if len(detail.HourlyBuckets) != 24 {
		t.Fatalf("len(detail.HourlyBuckets) = %d, want 24", len(detail.HourlyBuckets))
	}
	for i, bucket := range detail.HourlyBuckets {
		if bucket.Status != "no_data" {
			t.Errorf("HourlyBuckets[%d].Status = %q, want %q", i, bucket.Status, "no_data")
		}
	}
}

// TestGetService_Degraded_WithStatusAnalysis_ReturnsNote asserts SVC-15/16
// (L-048): status_analysis is present when current_status is "degraded"
// and an analysis has been stored.
func TestGetService_Degraded_WithStatusAnalysis_ReturnsNote(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	created := createServiceForDetail(t, r, pool, token, name, "slo-detail-degraded", "")

	analysis := "SLI dropped below target"
	if _, err := pool.Exec(context.Background(), "UPDATE services SET current_status = 'degraded', status_analysis = $1 WHERE id = $2", analysis, created.ID); err != nil {
		t.Fatalf("setup UPDATE returned unexpected error: %v", err)
	}

	rec := getServiceDetail(t, r, token, created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var detail serviceDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if detail.CurrentStatus != "degraded" {
		t.Fatalf("detail.CurrentStatus = %q, want %q", detail.CurrentStatus, "degraded")
	}
	if detail.StatusAnalysis == nil || *detail.StatusAnalysis != analysis {
		t.Errorf("detail.StatusAnalysis = %v, want %q", detail.StatusAnalysis, analysis)
	}
}

// TestGetService_NotDegraded_StatusAnalysisNull asserts SVC-15/16 (L-048)'s
// other branch: an operational service's status_analysis is null, even if
// a stale value were somehow still stored (db.Service's own invariant is
// that UpdateStatusAnalysis clears it on leaving "degraded", so this also
// guards against the handler failing to pass that invariant through).
func TestGetService_NotDegraded_StatusAnalysisNull(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	created := createServiceForDetail(t, r, pool, token, name, "slo-detail-not-degraded", "")

	rec := getServiceDetail(t, r, token, created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var detail serviceDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if detail.CurrentStatus == "degraded" {
		t.Fatalf("detail.CurrentStatus = %q, want not degraded (a fresh service is not_configured)", detail.CurrentStatus)
	}
	if detail.StatusAnalysis != nil {
		t.Errorf("detail.StatusAnalysis = %v, want nil for a non-degraded service", *detail.StatusAnalysis)
	}
}

// TestGetService_Incidents30d_ReflectsCount asserts SVC-14: incidents_30d
// reflects IncidentRepository.CountByServiceSince's count for this service.
func TestGetService_Incidents30d_ReflectsCount(t *testing.T) {
	r, pool, admins := newServicesRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	name := uniqueServiceName(t)
	created := createServiceForDetail(t, r, pool, token, name, "slo-detail-incidents", "")

	incidents := db.NewIncidentRepository(pool)
	for i := 0; i < 2; i++ {
		incident := &db.Incident{Title: fmt.Sprintf("%s-incident-%d", name, i)}
		if err := incidents.Create(context.Background(), incident, []string{created.ID}); err != nil {
			t.Fatalf("setup incident Create() returned unexpected error: %v", err)
		}
		t.Cleanup(func(id string) func() {
			return func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", id) }
		}(incident.ID))
	}

	rec := getServiceDetail(t, r, token, created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var detail serviceDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if detail.Incidents30d != 2 {
		t.Errorf("detail.Incidents30d = %d, want 2", detail.Incidents30d)
	}
}
