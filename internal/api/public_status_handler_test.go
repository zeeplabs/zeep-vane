//go:build integration

package api

import (
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
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

func newPublicStatusRouter(t *testing.T) (http.Handler, *db.Pool) {
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

	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	companySettings := db.NewCompanySettingsRepository(pool)
	handler := NewPublicStatusHandler(services, intervals, incidents, companySettings, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/", handler.Get)

	return r, pool
}

// createPublicStatusServiceFixture creates a service, sets its current
// status, and opens a status interval with a fixed, past lastSeenAt
// (rather than "now") so tests can assert the exact timestamp the public
// endpoint exposes, not just that some recent-looking value came back.
func createPublicStatusServiceFixture(t *testing.T, pool *db.Pool, status string, lastSeenAt time.Time) (serviceID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	name := uniqueServiceName(t)

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: name, SLOID: "slo-public-test"}
	if err := services.Create(ctx, service); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(ctx, service.ID, status); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	intervals := db.NewStatusIntervalRepository(pool)
	if err := intervals.OpenOrExtend(ctx, service.ID, status, 0.5, lastSeenAt); err != nil {
		t.Fatalf("setup OpenOrExtend() returned unexpected error: %v", err)
	}

	return service.ID, func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) }
}

// insertStatusInterval records a closed status interval for serviceID
// spanning [startsAt, endsAt), on top of whatever
// createPublicStatusServiceFixture already opened - used to control exactly
// which hourly bucket an interval lands in (UPT-01..04) without disturbing
// the fixture's own open interval (and therefore LastUpdatedAt).
func insertStatusInterval(t *testing.T, pool *db.Pool, serviceID, status string, startsAt, endsAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at, ends_at) VALUES ($1, $2, $3, $4, $4, $5)",
		serviceID, status, 0.5, startsAt, endsAt,
	); err != nil {
		t.Fatalf("setup interval insert returned unexpected error: %v", err)
	}
}

// createPublicStatusPageFixture creates a domain and a status page linked
// to serviceIDs, registering cleanup for both. It returns the status
// page's ID, which tests attach to the request context via
// withStatusPageContext - the same StatusPage.ID router.HostRouter would
// resolve and attach for a real request against this status page's
// hostname (SP-15 scoping).
func createPublicStatusPageFixture(t *testing.T, pool *db.Pool, serviceIDs ...string) string {
	t.Helper()
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootHostname := fmt.Sprintf("public-status-test-%s.example.com", suffix)

	domains := db.NewDomainRepository(pool)
	domain := &db.Domain{Hostname: rootHostname}
	if err := domains.Create(ctx, domain); err != nil {
		t.Fatalf("setup domain Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM domains WHERE id = $1", domain.ID) })

	statusPages := db.NewStatusPageRepository(pool)
	subdomain := "status"
	statusPage := &db.StatusPage{Name: "public-status-test-page", Subdomain: &subdomain, DomainID: &domain.ID}
	if err := statusPages.Create(ctx, statusPage, serviceIDs); err != nil {
		t.Fatalf("setup status page Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPage.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	return statusPage.ID
}

// withStatusPageContext returns a shallow copy of req carrying
// statusPageID on its context, exactly as router.HostRouter attaches it
// before dispatching to the public handler in the real server.
func withStatusPageContext(req *http.Request, statusPageID string) *http.Request {
	return req.WithContext(router.WithStatusPageID(req.Context(), statusPageID))
}

func findPublicService(services []publicServiceResponse, serviceID string, allServices []db.Service) *publicServiceResponse {
	var name string
	for _, s := range allServices {
		if s.ID == serviceID {
			name = s.Name
			break
		}
	}
	for i := range services {
		if services[i].Name == name {
			return &services[i]
		}
	}
	return nil
}

func TestPublicStatusGet_NoAuthHeader_200WithServiceStatus(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	fetchedAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", fetchedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	// Deliberately no Authorization header at all - the public endpoint
	// must be reachable by an anonymous visitor (SP-10).
	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if found.Status != "operational" {
		t.Errorf("Status = %q, want %q", found.Status, "operational")
	}
	if !found.LastUpdatedAt.Equal(fetchedAt) {
		t.Errorf("LastUpdatedAt = %v, want %v (the open interval's last_seen_at)", found.LastUpdatedAt, fetchedAt)
	}
	// UPT-01: the HourlyHistory field is a new addition, but every existing
	// field above must remain unaffected by it.
	if len(found.HourlyHistory) != 24 {
		t.Errorf("len(HourlyHistory) = %d, want %d", len(found.HourlyHistory), 24)
	}
}

// TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket
// covers UPT-01/02/03/04: an interval recorded a known number of hours ago
// must appear as exactly one hourly bucket, colored by its status, at the
// correct America/Sao_Paulo local hour.
func TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket(t *testing.T) {
	r, pool := newPublicStatusRouter(t)

	// The fixture's own open interval starts well outside the 24h window
	// (but stays open, so it still underlies every in-window bucket as
	// "operational" - the outage interval below then wins those hours it
	// overlaps, per worst-status-wins).
	staleFetchedAt := time.Now().Add(-72 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", staleFetchedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	knownFetchedAt := time.Now().Add(-3 * time.Hour)
	insertStatusInterval(t, pool, serviceID, "outage", knownFetchedAt, knownFetchedAt.Add(1*time.Minute))

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation() returned unexpected error: %v", err)
	}
	localKnown := knownFetchedAt.In(loc)
	wantStart := time.Date(localKnown.Year(), localKnown.Month(), localKnown.Day(), localKnown.Hour(), 0, 0, 0, loc)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if len(found.HourlyHistory) != 24 {
		t.Fatalf("len(HourlyHistory) = %d, want %d", len(found.HourlyHistory), 24)
	}

	var outageBuckets int
	for _, bucket := range found.HourlyHistory {
		if bucket.Status != "outage" {
			continue
		}
		outageBuckets++
		if !bucket.Start.Equal(wantStart) {
			t.Errorf("outage bucket Start = %v, want %v", bucket.Start, wantStart)
		}
	}
	if outageBuckets != 1 {
		t.Fatalf("outage buckets = %d, want exactly 1", outageBuckets)
	}
}

// TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData
// covers UPT-06: a service the poller has never reached (zero
// status_intervals rows ever, not just outside the window) must still
// render all 24 bars as no_data, never an omitted or fabricated history.
func TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()

	services := db.NewServiceRepository(pool)
	service := &db.Service{Name: uniqueServiceName(t), SLOID: "slo-no-snapshot-test"}
	if err := services.Create(ctx, service); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(ctx, service.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", service.ID) })
	statusPageID := createPublicStatusPageFixture(t, pool, service.ID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, service.ID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", service.ID)
	}
	if len(found.HourlyHistory) != 24 {
		t.Fatalf("len(HourlyHistory) = %d, want %d", len(found.HourlyHistory), 24)
	}
	for i, bucket := range found.HourlyHistory {
		if bucket.Status != "no_data" {
			t.Errorf("HourlyHistory[%d].Status = %q, want %q", i, bucket.Status, "no_data")
		}
	}
	// SHU-15: zero recorded intervals ever means uptime % is undefined
	// (null), never a fabricated 0 or 100.
	if found.UptimePercent != nil {
		t.Errorf("UptimePercent = %v, want nil (no recorded intervals)", *found.UptimePercent)
	}
}

// TestPublicStatusGet_UptimePercent_OutageWindowComputesExpectedValue
// covers SHU-10..14 end-to-end through the handler: a service with a real
// 6h outage inside an otherwise operational 24h window must render
// uptime_percent close to 75.0, not a value computed off a stale snapshot
// model. "Close to" rather than exactly 75.0 because of the H7 fix
// (public_status_handler.go's asOf): the denominator now ends at the last
// time the fixture's open interval was actually confirmed, which - like a
// real poller - is captured a moment before the request, not at the
// request's own wall-clock now. That gap is milliseconds here (imperceptible
// against a 24h window) but means the floored percentage lands at 74.9, not
// exactly 75.0 - asserting the exact value would be asserting a race that
// happens to lose by a hair, every run.
func TestPublicStatusGet_UptimePercent_OutageWindowComputesExpectedValue(t *testing.T) {
	r, pool := newPublicStatusRouter(t)

	// Opens an operational interval well before the window so the entire
	// 24h denominator is used (SHU-11).
	openedAt := time.Now().Add(-48 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", openedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	outageStart := time.Now().Add(-10 * time.Hour)
	outageEnd := outageStart.Add(6 * time.Hour)
	insertStatusInterval(t, pool, serviceID, "outage", outageStart, outageEnd)

	// A real poller keeps confirming operational after the outage clears,
	// continuously bumping the open interval's last_seen_at - without this,
	// the H7 asOf clamp (public_status_handler.go) would see the fixture's
	// still-open interval as last confirmed at openedAt (48h ago) and clip
	// the uptime denominator there, which this test isn't exercising.
	if err := db.NewStatusIntervalRepository(pool).OpenOrExtend(context.Background(), serviceID, "operational", 0.5, time.Now()); err != nil {
		t.Fatalf("setup post-outage OpenOrExtend() returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if found.UptimePercent == nil {
		t.Fatalf("UptimePercent = nil, want ~75.0")
	}
	if *found.UptimePercent != 75.0 && *found.UptimePercent != 74.9 {
		t.Errorf("UptimePercent = %v, want 74.9 or 75.0", *found.UptimePercent)
	}
}

// TestPublicStatusGet_LastUpdatedAt_AdvancesOnRepeatedSameStatusPoll is a
// regression guard for the design's stated risk: without last_seen_at
// bumped on every confirming poll, LastUpdatedAt would freeze at the
// moment a status interval opened and never move again while the status
// stays unchanged.
func TestPublicStatusGet_LastUpdatedAt_AdvancesOnRepeatedSameStatusPoll(t *testing.T) {
	r, pool := newPublicStatusRouter(t)

	firstSeenAt := time.Now().Add(-2 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", firstSeenAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	secondSeenAt := time.Now().Add(-1 * time.Hour)
	intervals := db.NewStatusIntervalRepository(pool)
	if err := intervals.OpenOrExtend(context.Background(), serviceID, "operational", 0.5, secondSeenAt); err != nil {
		t.Fatalf("setup second OpenOrExtend() returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if !found.LastUpdatedAt.Equal(secondSeenAt) {
		t.Errorf("LastUpdatedAt = %v, want %v (advanced past the interval's original starts_at %v)", found.LastUpdatedAt, secondSeenAt, firstSeenAt)
	}
}

// TestPublicStatusGet_StalledPoller_CurrentHourNotFabricatedOperational is
// the H7 regression guard: a service's open interval was last confirmed
// hours ago (the poller died and stopped ticking) - the current hourly
// bucket must render no_data, not the stale open interval's status
// extrapolated forward to wall-clock now, which would fabricate confidence
// the public page never actually had (SP-08, SP-09).
func TestPublicStatusGet_StalledPoller_CurrentHourNotFabricatedOperational(t *testing.T) {
	r, pool := newPublicStatusRouter(t)

	openedAt := time.Now().Add(-48 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", openedAt)
	t.Cleanup(cleanup)

	// The poller's last real confirmation was 10h ago - well before the
	// current hourly bucket - and nothing has extended the interval since
	// (simulating a dead poller, not a reconnect).
	staleAt := time.Now().Add(-10 * time.Hour)
	intervals := db.NewStatusIntervalRepository(pool)
	if err := intervals.OpenOrExtend(context.Background(), serviceID, "operational", 0.5, staleAt); err != nil {
		t.Fatalf("setup second OpenOrExtend() returned unexpected error: %v", err)
	}

	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}

	lastBucket := found.HourlyHistory[len(found.HourlyHistory)-1]
	if lastBucket.Status != "no_data" {
		t.Errorf("current hour bucket = %q, want %q (poller stalled 10h ago - must not fabricate a status this recent)", lastBucket.Status, "no_data")
	}
}

// TestPublicStatusGet_IntegrationInvalid_StillServesLastSnapshot simulates
// the Datadog connection failing (Integration marked "invalid" after the
// poller exhausts its retries, T24) and confirms the public endpoint keeps
// serving the last valid cached status instead of erroring out (SP-08,
// SP-09).
func TestPublicStatusGet_IntegrationInvalid_StillServesLastSnapshot(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	fetchedAt := time.Date(2026, 2, 1, 8, 30, 0, 0, time.UTC)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "degraded", fetchedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	dsn := testDatabaseURL(t)
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	integrations := db.NewIntegrationRepository(pool)
	if err := integrations.UpsertDatadog(context.Background(), []byte("encrypted-key"), []byte("encrypted-app-key")); err != nil {
		t.Fatalf("setup UpsertDatadog() returned unexpected error: %v", err)
	}
	if err := integrations.MarkDatadogInvalid(context.Background(), "connection failure: exhausted retries"); err != nil {
		t.Fatalf("setup MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	found := findPublicService(body.Services, serviceID, allServices)
	if found == nil {
		t.Fatalf("service %s not present in public response", serviceID)
	}
	if found.Status != "degraded" {
		t.Errorf("Status = %q, want %q (last cached status, unaffected by integration failure)", found.Status, "degraded")
	}
	if !found.LastUpdatedAt.Equal(fetchedAt) {
		t.Errorf("LastUpdatedAt = %v, want %v (real last-success timestamp, never a fabricated now)", found.LastUpdatedAt, fetchedAt)
	}
}

// TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown covers
// the spec.md edge case: a service with no SLO linked yet stays
// "not_configured" on the admin side and must never appear in the public
// response, while a service with a real status appears normally alongside
// it.
func TestPublicStatusGet_NotConfiguredService_HiddenValidServiceShown(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()

	configuredID, cleanupConfigured := createPublicStatusServiceFixture(t, pool, "operational", time.Now().UTC())
	t.Cleanup(cleanupConfigured)

	services := db.NewServiceRepository(pool)
	notConfiguredName := uniqueServiceName(t)
	notConfigured := &db.Service{Name: notConfiguredName, SLOID: "slo-not-configured-test"}
	if err := services.Create(ctx, notConfigured); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", notConfigured.ID) })
	if notConfigured.CurrentStatus != "not_configured" {
		t.Fatalf("setup: fresh service CurrentStatus = %q, want %q", notConfigured.CurrentStatus, "not_configured")
	}
	statusPageID := createPublicStatusPageFixture(t, pool, configuredID, notConfigured.ID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	for _, s := range body.Services {
		if s.Name == notConfiguredName {
			t.Fatalf("not_configured service %q present in public response, want hidden", notConfiguredName)
		}
	}

	allServices, err := services.List(ctx)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if found := findPublicService(body.Services, configuredID, allServices); found == nil {
		t.Fatalf("configured service %s not present in public response, want shown", configuredID)
	}
}

// createPublicIncidentFixture creates an incident linked to a fresh service
// and returns the incident's ID and that service's ID, registering
// cleanup. The service ID is needed by callers so they can link a status
// page to it via createPublicStatusPageFixture - an incident only appears
// on a status page's public response if it's linked (via incident_services)
// to a service that status page also publishes (SP-15).
func createPublicIncidentFixture(t *testing.T, pool *db.Pool, title string) (incidentID, serviceID string) {
	t.Helper()
	ctx := context.Background()

	serviceID = createIncidentTestService(t, pool)
	incidents := db.NewIncidentRepository(pool)
	incident := &db.Incident{Title: title}
	if err := incidents.Create(ctx, incident, []string{serviceID}); err != nil {
		t.Fatalf("setup incident Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	return incident.ID, serviceID
}

func findPublicIncident(incidents []publicIncidentResponse, incidentID string) *publicIncidentResponse {
	for i := range incidents {
		if incidents[i].ID == incidentID {
			return &incidents[i]
		}
	}
	return nil
}

// TestPublicStatusGet_UnresolvedIncident_AppearsInActive covers SP-18: an
// incident not yet resolved must appear in destaque (the "active" group) on
// the public status page.
func TestPublicStatusGet_UnresolvedIncident_AppearsInActive(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "unresolved incident public test")
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if found := findPublicIncident(body.Incidents.Active, incidentID); found == nil {
		t.Fatalf("unresolved incident %s not present in active incidents, want it featured", incidentID)
	}
	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found != nil {
		t.Errorf("unresolved incident %s present in resolved incidents, want it absent", incidentID)
	}
}

// TestPublicStatusGet_ResolvedIncidentWithinRetention_AppearsInHistory
// covers SP-18/retention: an incident resolved less than 90 days ago must
// still appear in the public history.
func TestPublicStatusGet_ResolvedIncidentWithinRetention_AppearsInHistory(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "recently resolved incident public test")
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	if _, err := pool.Exec(context.Background(),
		"UPDATE incidents SET status = 'resolved', resolved_at = now() - interval '10 days' WHERE id = $1",
		incidentID,
	); err != nil {
		t.Fatalf("setup resolved_at update returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found == nil {
		t.Fatalf("incident %s resolved 10 days ago not present in resolved history, want it within the 90-day window", incidentID)
	}
}

// resetCompanySettingsForPublicStatusTest resets the company_settings
// singleton row to a known state, registering the same reset as cleanup -
// mirrors newCompanySettingsRouterWithUploadsDir's reset in
// company_settings_handler_test.go, since the row is shared across every
// test in this package.
func resetCompanySettingsForPublicStatusTest(t *testing.T, pool *db.Pool) {
	t.Helper()

	// This reset races internal/db's and internal/cli's own
	// company_settings tests across the separate concurrent processes
	// `go test ./...` runs them as, so take the shared advisory lock for
	// the duration of this test - see LockCompanySettings' doc comment.
	dbtest.LockCompanySettings(t, context.Background(), testDatabaseURL(t))

	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)
}

// TestPublicStatusGet_CompanySettingsSet_IncludesNameAndLogo covers SET-15:
// the public response must include the real, persisted company name and
// logo URL, not a mocked placeholder.
func TestPublicStatusGet_CompanySettingsSet_IncludesNameAndLogo(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	resetCompanySettingsForPublicStatusTest(t, pool)

	companySettings := db.NewCompanySettingsRepository(pool)
	if _, err := companySettings.Update(context.Background(), "Acme Status", "contato@acme.example"); err != nil {
		t.Fatalf("setup Update() returned unexpected error: %v", err)
	}
	if _, err := companySettings.UpdateLogoURL(context.Background(), "/uploads/logo.png"); err != nil {
		t.Fatalf("setup UpdateLogoURL() returned unexpected error: %v", err)
	}

	statusPageID := createPublicStatusPageFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if body.Company.Name != "Acme Status" {
		t.Errorf("Company.Name = %q, want %q", body.Company.Name, "Acme Status")
	}
	if body.Company.LogoURL == nil || *body.Company.LogoURL != "/uploads/logo.png" {
		t.Errorf("Company.LogoURL = %v, want %q", body.Company.LogoURL, "/uploads/logo.png")
	}
}

// TestPublicStatusGet_NoLogoUploaded_CompanyLogoURLNull covers SET-16: while
// no logo has ever been uploaded, the public response must carry
// logo_url: null rather than a fabricated placeholder path.
func TestPublicStatusGet_NoLogoUploaded_CompanyLogoURLNull(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	resetCompanySettingsForPublicStatusTest(t, pool)

	statusPageID := createPublicStatusPageFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if body.Company.LogoURL != nil {
		t.Errorf("Company.LogoURL = %v, want nil (no logo ever uploaded)", *body.Company.LogoURL)
	}
}

// TestPublicStatusGet_ResolvedIncidentBeyondRetention_Hidden covers the
// spec.md 90-day retention assumption: an incident resolved more than 90
// days ago must never appear on the public status page.
func TestPublicStatusGet_ResolvedIncidentBeyondRetention_Hidden(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "long-resolved incident public test")
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	if _, err := pool.Exec(context.Background(),
		"UPDATE incidents SET status = 'resolved', resolved_at = now() - interval '91 days' WHERE id = $1",
		incidentID,
	); err != nil {
		t.Fatalf("setup resolved_at update returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if found := findPublicIncident(body.Incidents.Resolved, incidentID); found != nil {
		t.Errorf("incident %s resolved 91 days ago present in resolved history, want it hidden past the 90-day window", incidentID)
	}
	if found := findPublicIncident(body.Incidents.Active, incidentID); found != nil {
		t.Errorf("incident %s present in active incidents, want it hidden entirely", incidentID)
	}
}
