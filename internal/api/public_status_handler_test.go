//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

	pool, _ := newAPITenantScopedPool(t)

	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	companySettings := db.NewTenantRepository(pool)
	statusPages := db.NewStatusPageRepository(pool)
	domains := db.NewDomainRepository(pool)
	handler := NewPublicStatusHandler(services, intervals, incidents, companySettings, statusPages, domains, zap.NewNop())

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
	tenantID := seedTestTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		if err := services.Create(txCtx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	if err := services.UpdateStatus(ctx, service.ID, status); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	intervals := db.NewStatusIntervalRepository(pool)
	if _, err := intervals.OpenOrExtend(ctx, service.ID, status, 0.5, lastSeenAt); err != nil {
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
	statusPageID, _ := createPublicStatusPageFixtureWithDomainID(t, pool, serviceIDs...)
	return statusPageID
}

// createPublicStatusPageFixtureWithDomainID is createPublicStatusPageFixture,
// additionally returning the attached domain's ID so
// public-status-domain-health (PSDH-01..04) tests can mutate its health
// columns via db.DomainRepository.SetHealthCheckResult - the same
// AD-039-populated columns resolveDomainHealth reads, never a new write
// path of its own.
func createPublicStatusPageFixtureWithDomainID(t *testing.T, pool *db.Pool, serviceIDs ...string) (statusPageID, domainID string) {
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

	return statusPage.ID, domain.ID
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
	if len(found.History) != 24 {
		t.Errorf("len(HourlyHistory) = %d, want %d", len(found.History), 24)
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
	if len(found.History) != 24 {
		t.Fatalf("len(HourlyHistory) = %d, want %d", len(found.History), 24)
	}

	var outageBuckets int
	for _, bucket := range found.History {
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
	tenantID := seedTestTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		if err := services.Create(txCtx, service); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
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
	if len(found.History) != 24 {
		t.Fatalf("len(HourlyHistory) = %d, want %d", len(found.History), 24)
	}
	for i, bucket := range found.History {
		if bucket.Status != "no_data" {
			t.Errorf("History[%d].Status = %q, want %q", i, bucket.Status, "no_data")
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
	if _, err := db.NewStatusIntervalRepository(pool).OpenOrExtend(context.Background(), serviceID, "operational", 0.5, time.Now()); err != nil {
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

	// Truncated to microsecond precision: Postgres TIMESTAMPTZ stores at
	// most microsecond resolution, so the value read back after
	// OpenOrExtend below loses time.Now()'s nanosecond digits - comparing
	// against an untruncated secondSeenAt would fail on Equal() nearly
	// every run (rounding to the microsecond changes the value with high
	// probability).
	secondSeenAt := time.Now().Add(-1 * time.Hour).Truncate(time.Microsecond)
	intervals := db.NewStatusIntervalRepository(pool)
	if _, err := intervals.OpenOrExtend(context.Background(), serviceID, "operational", 0.5, secondSeenAt); err != nil {
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
	if _, err := intervals.OpenOrExtend(context.Background(), serviceID, "operational", 0.5, staleAt); err != nil {
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

	lastBucket := found.History[len(found.History)-1]
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
	tenantID := seedTestTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		if err := services.Create(txCtx, notConfigured); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
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
	if found := findPublicIncident(body.Incidents.Resolved.Items, incidentID); found != nil {
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

	if found := findPublicIncident(body.Incidents.Resolved.Items, incidentID); found == nil {
		t.Fatalf("incident %s resolved 10 days ago not present in resolved history, want it within the 90-day window", incidentID)
	}
}

// resetCompanySettingsForPublicStatusTest resets the fixture tenant's
// singleton row to a known state, registering the same reset as cleanup -
// mirrors newCompanySettingsRouter's reset in company_settings_handler_test.go,
// since the row is shared across every test in this package.
func resetCompanySettingsForPublicStatusTest(t *testing.T, pool *db.Pool) {
	t.Helper()

	// This reset races internal/db's and internal/cli's own
	// company-profile tests across the separate concurrent processes
	// `go test ./...` runs them as, so take the shared advisory lock for
	// the duration of this test - see LockTenantsTable' doc comment.
	dbtest.LockTenantsTable(t, context.Background(), testDatabaseURL(t))

	reset := func() {
		_, _ = pool.Exec(context.Background(),
			"UPDATE tenants SET name = '', contact_email = '', logo_data = NULL, logo_content_type = NULL WHERE id = $1", apiTestTenantID(t))
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

	companySettings := db.NewTenantRepository(pool)
	if _, err := companySettings.Update(context.Background(), apiTestTenantID(t), db.TenantUpdate{Name: ptr("Acme Status"), ContactEmail: ptr("contato@acme.example")}); err != nil {
		t.Fatalf("setup Update() returned unexpected error: %v", err)
	}
	if _, err := companySettings.UpdateLogo(context.Background(), apiTestTenantID(t), "image/png", []byte("fake-png-bytes")); err != nil {
		t.Fatalf("setup UpdateLogo() returned unexpected error: %v", err)
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
	if body.Company.LogoURL == nil || *body.Company.LogoURL != "/uploads/logo" {
		t.Errorf("Company.LogoURL = %v, want %q", body.Company.LogoURL, "/uploads/logo")
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

	if found := findPublicIncident(body.Incidents.Resolved.Items, incidentID); found != nil {
		t.Errorf("incident %s resolved 91 days ago present in resolved history, want it hidden past the 90-day window", incidentID)
	}
	if found := findPublicIncident(body.Incidents.Active, incidentID); found != nil {
		t.Errorf("incident %s present in active incidents, want it hidden entirely", incidentID)
	}
}

// createResolvedPublicIncidentFixture creates an incident linked to
// serviceID and marks it resolved well within the retention window,
// registering cleanup. Used by the resolved-incidents pagination tests
// below to seed more than one page's worth of history.
func createResolvedPublicIncidentFixture(t *testing.T, pool *db.Pool, serviceID, title string) string {
	t.Helper()
	ctx := context.Background()

	incidents := db.NewIncidentRepository(pool)
	incident := &db.Incident{Title: title}
	if err := incidents.Create(ctx, incident, []string{serviceID}); err != nil {
		t.Fatalf("setup incident Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	if _, err := pool.Exec(ctx,
		"UPDATE incidents SET status = 'resolved', resolved_at = now() - interval '1 hour' WHERE id = $1",
		incident.ID,
	); err != nil {
		t.Fatalf("setup resolved_at update returned unexpected error: %v", err)
	}

	return incident.ID
}

// TestPublicStatusGet_ResolvedIncidents_Page1ExactlyPageSizeCorrectTotal
// covers PAG-12/PAG-13: page 1 of more than page_size (10) resolved
// incidents within retention returns exactly 10 items and the correct
// total, never the whole unbounded history.
func TestPublicStatusGet_ResolvedIncidents_Page1ExactlyPageSizeCorrectTotal(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID := createIncidentTestService(t, pool)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	for i := 0; i < 11; i++ {
		createResolvedPublicIncidentFixture(t, pool, serviceID, fmt.Sprintf("paginated resolved incident %d", i))
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

	if len(body.Incidents.Resolved.Items) != 10 {
		t.Errorf("len(Resolved.Items) = %d, want 10 (page_size)", len(body.Incidents.Resolved.Items))
	}
	if body.Incidents.Resolved.Total != 11 {
		t.Errorf("Resolved.Total = %d, want 11", body.Incidents.Resolved.Total)
	}
	if body.Incidents.Resolved.Page != 1 {
		t.Errorf("Resolved.Page = %d, want 1 (default)", body.Incidents.Resolved.Page)
	}
	if body.Incidents.Resolved.PageSize != 10 {
		t.Errorf("Resolved.PageSize = %d, want 10", body.Incidents.Resolved.PageSize)
	}
}

// TestPublicStatusGet_ResolvedIncidents_Page2ReturnsRemainder covers
// PAG-13/14: page 2 of an 11-incident seed (page_size 10) returns exactly
// the 1 remaining incident, with the same total as page 1.
func TestPublicStatusGet_ResolvedIncidents_Page2ReturnsRemainder(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID := createIncidentTestService(t, pool)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	for i := 0; i < 11; i++ {
		createResolvedPublicIncidentFixture(t, pool, serviceID, fmt.Sprintf("paginated resolved incident page2 %d", i))
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?page=2", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if len(body.Incidents.Resolved.Items) != 1 {
		t.Errorf("len(Resolved.Items) = %d, want 1 (11-incident seed remainder)", len(body.Incidents.Resolved.Items))
	}
	if body.Incidents.Resolved.Total != 11 {
		t.Errorf("Resolved.Total = %d, want 11", body.Incidents.Resolved.Total)
	}
	if body.Incidents.Resolved.Page != 2 {
		t.Errorf("Resolved.Page = %d, want 2", body.Incidents.Resolved.Page)
	}
}

// seedPublicStatusRangeFixture creates a service with a long-open,
// continuously-confirmed "operational" interval plus a real, known outage
// inside the 24h window - used by the range-tier tests below so both
// "no outage in this narrow slice" and "outage visible in this wider slice"
// assertions can be made off the same seed data (TRS-01..04).
func seedPublicStatusRangeFixture(t *testing.T, pool *db.Pool) (serviceID, statusPageID string) {
	t.Helper()

	openedAt := time.Now().Add(-95 * 24 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", openedAt)
	t.Cleanup(cleanup)
	statusPageID = createPublicStatusPageFixture(t, pool, serviceID)

	// A real outage inside the last 24h - present in every tier's window
	// (24h through 90d) so uptime_percent should reflect it in all of them,
	// but its share of the denominator (and therefore the computed
	// percentage) differs by tier (TRS-04).
	outageStart := time.Now().Add(-6 * time.Hour)
	insertStatusInterval(t, pool, serviceID, "outage", outageStart, outageStart.Add(3*time.Hour))

	// Keep the open interval's last_seen_at fresh so the H7 asOf clamp
	// doesn't clip the window before every tier's own windowStart.
	if _, err := db.NewStatusIntervalRepository(pool).OpenOrExtend(context.Background(), serviceID, "operational", 0.5, time.Now()); err != nil {
		t.Fatalf("setup post-outage OpenOrExtend() returned unexpected error: %v", err)
	}

	return serviceID, statusPageID
}

// TestPublicStatusGet_NoRangeParam_Behaves24hDefault covers TRS-01: a
// request with no range param at all behaves identically to range=24h -
// 24 buckets, 1h wide.
func TestPublicStatusGet_NoRangeParam_Behaves24hDefault(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)

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
	if len(found.History) != 24 {
		t.Errorf("len(History) = %d, want 24 (no range param defaults to 24h)", len(found.History))
	}
}

// TestPublicStatusGet_Range24h_Behaves24hDefault covers TRS-01: an explicit
// range=24h behaves identically to no range param at all.
func TestPublicStatusGet_Range24h_Behaves24hDefault(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=24h", nil), statusPageID)
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
	if len(found.History) != 24 {
		t.Errorf("len(History) = %d, want 24", len(found.History))
	}
}

// TestPublicStatusGet_Range7d_Returns28BucketsSixHoursWide covers TRS-02/03.
func TestPublicStatusGet_Range7d_Returns28BucketsSixHoursWide(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=7d", nil), statusPageID)
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
	if len(found.History) != 28 {
		t.Fatalf("len(History) = %d, want 28", len(found.History))
	}
	if got := found.History[1].Start.Sub(found.History[0].Start); got != 6*time.Hour {
		t.Errorf("bucket width = %v, want 6h", got)
	}
}

// TestPublicStatusGet_Range30d_Returns30BucketsOneDayWide covers TRS-02/03.
func TestPublicStatusGet_Range30d_Returns30BucketsOneDayWide(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=30d", nil), statusPageID)
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
	if len(found.History) != 30 {
		t.Fatalf("len(History) = %d, want 30", len(found.History))
	}
	if got := found.History[1].Start.Sub(found.History[0].Start); got != 24*time.Hour {
		t.Errorf("bucket width = %v, want 24h", got)
	}
}

// TestPublicStatusGet_Range90d_Returns90BucketsOneDayWideBeyondOldRetention
// covers TRS-02/03/04 and confirms history beyond the old 35-day retention
// window is actually retrievable now that the retention constant has (per
// T4) been raised - the fixture's own outage sits inside all tiers, but its
// long-open "operational" interval predates the old 35-day retention
// entirely, proving the 90d window isn't silently truncated at the old
// boundary.
func TestPublicStatusGet_Range90d_Returns90BucketsOneDayWideBeyondOldRetention(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=90d", nil), statusPageID)
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
	if len(found.History) != 90 {
		t.Fatalf("len(History) = %d, want 90", len(found.History))
	}
	if got := found.History[1].Start.Sub(found.History[0].Start); got != 24*time.Hour {
		t.Errorf("bucket width = %v, want 24h", got)
	}
	if found.UptimePercent == nil {
		t.Fatalf("UptimePercent = nil, want a defined value (interval predating the old 35-day retention is retrievable)")
	}
}

// TestPublicStatusGet_UptimePercent_DiffersBetween24hAnd90dRanges covers
// TRS-04: the same underlying data must produce a different uptime_percent
// depending on the selected range, proving the figure isn't pinned to 24h.
func TestPublicStatusGet_UptimePercent_DiffersBetween24hAnd90dRanges(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, statusPageID := seedPublicStatusRangeFixture(t, pool)
	allServices, err := db.NewServiceRepository(pool).List(context.Background())
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}

	req24h := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=24h", nil), statusPageID)
	rec24h := httptest.NewRecorder()
	r.ServeHTTP(rec24h, req24h)
	if rec24h.Code != http.StatusOK {
		t.Fatalf("24h status = %d, want %d, body = %s", rec24h.Code, http.StatusOK, rec24h.Body.String())
	}
	var body24h publicStatusResponse
	if err := json.Unmarshal(rec24h.Body.Bytes(), &body24h); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	found24h := findPublicService(body24h.Services, serviceID, allServices)
	if found24h == nil || found24h.UptimePercent == nil {
		t.Fatalf("24h uptime_percent missing")
	}

	req90d := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=90d", nil), statusPageID)
	rec90d := httptest.NewRecorder()
	r.ServeHTTP(rec90d, req90d)
	if rec90d.Code != http.StatusOK {
		t.Fatalf("90d status = %d, want %d, body = %s", rec90d.Code, http.StatusOK, rec90d.Body.String())
	}
	var body90d publicStatusResponse
	if err := json.Unmarshal(rec90d.Body.Bytes(), &body90d); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	found90d := findPublicService(body90d.Services, serviceID, allServices)
	if found90d == nil || found90d.UptimePercent == nil {
		t.Fatalf("90d uptime_percent missing")
	}

	if *found24h.UptimePercent == *found90d.UptimePercent {
		t.Errorf("uptime_percent identical between 24h (%v) and 90d (%v) ranges for the same 3h outage, want different (90d's larger denominator dilutes the same outage far less)", *found24h.UptimePercent, *found90d.UptimePercent)
	}
}

// countingServiceLister wraps a real serviceLister and counts calls to
// ListForStatusPage - used to assert an invalid range never triggers any
// DB work (design.md's Error Handling Strategy: "no partial work").
type countingServiceLister struct {
	inner serviceLister
	calls int
}

func (c *countingServiceLister) ListForStatusPage(ctx context.Context, statusPageID string) ([]db.Service, error) {
	c.calls++
	return c.inner.ListForStatusPage(ctx, statusPageID)
}

// TestPublicStatusGet_InvalidRange_422NoDBQueryIssued covers TRS-07: an
// invalid range value is rejected with 422 before composeResponse (and
// therefore before any services/intervals DB query) ever runs.
func TestPublicStatusGet_InvalidRange_422NoDBQueryIssued(t *testing.T) {
	pool, _ := newAPITenantScopedPool(t)

	spy := &countingServiceLister{inner: db.NewServiceRepository(pool)}
	intervals := db.NewStatusIntervalRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	companySettings := db.NewTenantRepository(pool)
	statusPages := db.NewStatusPageRepository(pool)
	domains := db.NewDomainRepository(pool)
	handler := NewPublicStatusHandler(spy, intervals, incidents, companySettings, statusPages, domains, zap.NewNop())

	r := chi.NewRouter()
	r.Get("/", handler.Get)

	statusPageID := createPublicStatusPageFixture(t, pool)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=5d", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if want := "range must be one of 24h, 7d, 30d, 90d"; body["error"] != want {
		t.Errorf("error = %q, want %q", body["error"], want)
	}
	if spy.calls != 0 {
		t.Errorf("ListForStatusPage calls = %d, want 0 (invalid range must reject before any DB work)", spy.calls)
	}
}

// TestPublicStatusGet_DegradedServiceWithAnalysis_IncludesStatusAnalysis
// covers AI-16: a degraded service with a finished status_analysis exposes
// it verbatim in the public response.
func TestPublicStatusGet_DegradedServiceWithAnalysis_IncludesStatusAnalysis(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "degraded", time.Now())
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	analysis := "Elevated latency on the checkout path, error budget burning faster than usual."
	if err := db.NewServiceRepository(pool).UpdateStatusAnalysis(context.Background(), serviceID, &analysis); err != nil {
		t.Fatalf("setup UpdateStatusAnalysis() returned unexpected error: %v", err)
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
	if found.StatusAnalysis == nil || *found.StatusAnalysis != analysis {
		t.Errorf("StatusAnalysis = %v, want %q", found.StatusAnalysis, analysis)
	}
}

// TestPublicStatusGet_DegradedServiceAnalysisPending_OmitsStatusAnalysis
// covers AI-16's pending case: a degraded service whose analysis hasn't
// finished generating yet (status_analysis still NULL) omits the field
// entirely, not a null.
func TestPublicStatusGet_DegradedServiceAnalysisPending_OmitsStatusAnalysis(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "degraded", time.Now())
	t.Cleanup(cleanup)
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
	if found.StatusAnalysis != nil {
		t.Errorf("StatusAnalysis = %q, want nil while still pending", *found.StatusAnalysis)
	}
}

// TestPublicStatusGet_NonDegradedServiceWithStaleAnalysis_OmitsStatusAnalysis
// covers AI-15/AI-17's defense-in-depth re-check at the DTO boundary: even
// if status_analysis somehow still carried a stale value for a
// non-degraded service, the public response must never surface it.
func TestPublicStatusGet_NonDegradedServiceWithStaleAnalysis_OmitsStatusAnalysis(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	staleAnalysis := "stale degraded-tooltip text that should never have survived a transition"
	if err := db.NewServiceRepository(pool).UpdateStatusAnalysis(context.Background(), serviceID, &staleAnalysis); err != nil {
		t.Fatalf("setup UpdateStatusAnalysis() returned unexpected error: %v", err)
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
	if found.StatusAnalysis != nil {
		t.Errorf("StatusAnalysis = %q, want nil for an operational service even with a stale stored value", *found.StatusAnalysis)
	}
	if strings.Contains(rec.Body.String(), "stale degraded-tooltip") {
		t.Errorf("response body leaks the stale status_analysis text: %s", rec.Body.String())
	}
}

// TestPublicStatusGet_IncidentWithDescription_IncludesDescription covers
// AI-18: an incident with a description set exposes it on the public
// response.
func TestPublicStatusGet_IncidentWithDescription_IncludesDescription(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "incident with description public test")
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	if err := db.NewIncidentRepository(pool).SetDescription(context.Background(), incidentID, "root cause: upstream provider outage"); err != nil {
		t.Fatalf("setup SetDescription() returned unexpected error: %v", err)
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
	found := findPublicIncident(body.Incidents.Active, incidentID)
	if found == nil {
		t.Fatalf("incident %s not present in active incidents", incidentID)
	}
	if found.Description == nil || *found.Description != "root cause: upstream provider outage" {
		t.Errorf("Description = %v, want the set description", found.Description)
	}
}

// TestPublicStatusGet_IncidentWithoutDescription_OmitsDescription covers a
// manually created incident with no description: the field is omitted, not
// a null.
func TestPublicStatusGet_IncidentWithoutDescription_OmitsDescription(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "incident without description public test")
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
	found := findPublicIncident(body.Incidents.Active, incidentID)
	if found == nil {
		t.Fatalf("incident %s not present in active incidents", incidentID)
	}
	if found.Description != nil {
		t.Errorf("Description = %q, want nil for an incident with no description set", *found.Description)
	}
}

// TestPublicStatusGet_NeverExposesPendingCloseComment is a negative test for
// AI-19/AI-20's admin-only field: even with a pending closing-comment
// proposal set, the public response body must never contain it or the
// pending_close_comment key at all.
func TestPublicStatusGet_NeverExposesPendingCloseComment(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	incidentID, serviceID := createPublicIncidentFixture(t, pool, "pending close comment public test")
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	if err := db.NewIncidentRepository(pool).SetPendingCloseComment(context.Background(), incidentID, "service recovered, this must never leak publicly"); err != nil {
		t.Fatalf("setup SetPendingCloseComment() returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "pending_close_comment") || strings.Contains(rec.Body.String(), "this must never leak publicly") {
		t.Errorf("response body leaks pending_close_comment: %s", rec.Body.String())
	}
}

// TestPublicStatusGet_Range90d_PartialCoverageLeadingBucketsNoData covers
// TRS-06 at the handler level for a wide range: a service whose real data
// only covers the most recent slice of a 90d window must still render
// leading buckets as no_data, not error.
func TestPublicStatusGet_Range90d_PartialCoverageLeadingBucketsNoData(t *testing.T) {
	r, pool := newPublicStatusRouter(t)

	// Only 2 days of real data - the remaining ~88 days of the 90d window
	// predate this service's very first interval.
	openedAt := time.Now().Add(-2 * 24 * time.Hour)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", openedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	// Keep the interval's last_seen_at fresh so the H7 asOf clamp doesn't
	// clip the trailing (most recent) bucket to no_data too - this test is
	// only exercising the leading, out-of-coverage buckets.
	if _, err := db.NewStatusIntervalRepository(pool).OpenOrExtend(context.Background(), serviceID, "operational", 0.5, time.Now()); err != nil {
		t.Fatalf("setup OpenOrExtend() returned unexpected error: %v", err)
	}

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/?range=90d", nil), statusPageID)
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
	if len(found.History) != 90 {
		t.Fatalf("len(History) = %d, want 90", len(found.History))
	}
	if found.History[0].Status != "no_data" {
		t.Errorf("leading bucket Status = %q, want %q (predates the service's first interval)", found.History[0].Status, "no_data")
	}
	if found.History[len(found.History)-1].Status == "no_data" {
		t.Errorf("trailing (most recent) bucket Status = %q, want real data, not no_data", found.History[len(found.History)-1].Status)
	}
}

// TestPublicStatusGet_DegradedBucket_IncludesEpisodes covers DEGINT-11: a
// bucket overlapping a degraded interval with a generated analysis text
// exposes it in the response's episodes array.
func TestPublicStatusGet_DegradedBucket_IncludesEpisodes(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	openedAt := time.Now().Add(-5 * time.Minute)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "degraded", openedAt)
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	// A second poll confirming the same "degraded" status a few minutes
	// later extends last_seen_at without touching starts_at, giving the
	// interval real (non-zero) duration - a freshly-opened interval whose
	// starts_at equals its own last_seen_at/asOf is deliberately skipped by
	// history.BuildBuckets's zero-duration guard.
	intervals := db.NewStatusIntervalRepository(pool)
	intervalID, err := intervals.OpenOrExtend(context.Background(), serviceID, "degraded", 0.5, time.Now())
	if err != nil {
		t.Fatalf("setup second OpenOrExtend() returned unexpected error: %v", err)
	}
	if err := intervals.SetIntervalAnalysis(context.Background(), intervalID, "Latência elevada no checkout."); err != nil {
		t.Fatalf("setup SetIntervalAnalysis() returned unexpected error: %v", err)
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

	last := found.History[len(found.History)-1]
	if len(last.Episodes) != 1 {
		t.Fatalf("len(episodes) = %d, want 1", len(last.Episodes))
	}
	if last.Episodes[0].Analysis == nil || *last.Episodes[0].Analysis != "Latência elevada no checkout." {
		t.Errorf("episode Analysis = %v, want %q", last.Episodes[0].Analysis, "Latência elevada no checkout.")
	}
}

// TestPublicStatusGet_OperationalBucket_OmitsEpisodesField covers DEGINT-11:
// a bucket with no degraded episode omits the episodes field (not null,
// not an empty array) - json.Marshal's omitempty on a nil slice omits the
// key entirely, asserted here via the raw response body.
func TestPublicStatusGet_OperationalBucket_OmitsEpisodesField(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID := createPublicStatusPageFixture(t, pool, serviceID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"episodes"`) {
		t.Errorf("response body contains an \"episodes\" key for a bucket with no degraded episode: %s", rec.Body.String())
	}
}

// findPublicServiceByID looks up a publicServiceResponse by its own ID
// field (T9 adds ID to the DTO, unlike findPublicService above which
// predates it and matches by name).
func findPublicServiceByID(services []publicServiceResponse, id string) *publicServiceResponse {
	for i := range services {
		if services[i].ID == id {
			return &services[i]
		}
	}
	return nil
}

// TestPublicStatusGet_GroupedUngroupedParentChild_PopulatesPlacementFields
// covers SGD-02, SGD-03, SGD-06, SGD-09: a status page mixing an ungrouped
// service, a grouped parent, and a grouped child (ParentServiceID pointing
// at the parent) exposes group_id/group_name/parent_service_id/
// parent_service_name correctly, and groups (including one with zero
// services, SGD-09) appear in the top-level groups array. It also asserts
// each service's independently-computed Status/LastUpdatedAt/
// StatusAnalysis are untouched by grouping (SGD-09's "grouping never
// affects status computation" non-goal) - the same assertions T9's
// pre-existing single-service tests already make, now proving the new
// placement fields are purely additive.
func TestPublicStatusGet_GroupedUngroupedParentChild_PopulatesPlacementFields(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()

	fetchedAt := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	ungroupedID, cleanupUngrouped := createPublicStatusServiceFixture(t, pool, "operational", fetchedAt)
	t.Cleanup(cleanupUngrouped)
	parentID, cleanupParent := createPublicStatusServiceFixture(t, pool, "operational", fetchedAt)
	t.Cleanup(cleanupParent)
	childID, cleanupChild := createPublicStatusServiceFixture(t, pool, "degraded", fetchedAt)
	t.Cleanup(cleanupChild)

	analysis := "Downstream dependency degraded, propagating to this child service."
	if err := db.NewServiceRepository(pool).UpdateStatusAnalysis(ctx, childID, &analysis); err != nil {
		t.Fatalf("setup UpdateStatusAnalysis() returned unexpected error: %v", err)
	}

	statusPageID := createPublicStatusPageFixture(t, pool, ungroupedID, parentID, childID)

	statusPages := db.NewStatusPageRepository(pool)
	group, err := statusPages.CreateGroup(ctx, statusPageID, "Infra")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}
	// An empty group with zero services placed in it - must still appear
	// in the public groups array (SGD-09).
	emptyGroup, err := statusPages.CreateGroup(ctx, statusPageID, "Empty")
	if err != nil {
		t.Fatalf("setup CreateGroup() returned unexpected error: %v", err)
	}

	err = statusPages.SetServices(ctx, statusPageID, []db.ServicePlacement{
		{ServiceID: ungroupedID, GroupID: nil, ParentServiceID: nil, Position: 0},
		{ServiceID: parentID, GroupID: &group.ID, ParentServiceID: nil, Position: 1},
		{ServiceID: childID, GroupID: &group.ID, ParentServiceID: &parentID, Position: 2},
	})
	if err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
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

	// Groups array: both groups present, including the empty one, each
	// with its real name/position - unfiltered against which services use
	// them.
	if len(body.Groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2, body = %+v", len(body.Groups), body.Groups)
	}
	var foundGroup, foundEmptyGroup bool
	for _, g := range body.Groups {
		switch g.ID {
		case group.ID:
			foundGroup = true
			if g.Name != "Infra" || g.Position != 0 {
				t.Errorf("group = %+v, want Name=Infra Position=0", g)
			}
		case emptyGroup.ID:
			foundEmptyGroup = true
			if g.Name != "Empty" || g.Position != 1 {
				t.Errorf("empty group = %+v, want Name=Empty Position=1", g)
			}
		}
	}
	if !foundGroup {
		t.Errorf("groups %+v missing populated group %s", body.Groups, group.ID)
	}
	if !foundEmptyGroup {
		t.Errorf("groups %+v missing empty group %s", body.Groups, emptyGroup.ID)
	}

	// Ungrouped, parentless service.
	ungrouped := findPublicServiceByID(body.Services, ungroupedID)
	if ungrouped == nil {
		t.Fatalf("ungrouped service %s not present in response", ungroupedID)
	}
	if ungrouped.GroupID != nil || ungrouped.GroupName != nil {
		t.Errorf("ungrouped service GroupID/GroupName = %v/%v, want nil/nil", ungrouped.GroupID, ungrouped.GroupName)
	}
	if ungrouped.ParentServiceID != nil || ungrouped.ParentServiceName != nil {
		t.Errorf("ungrouped service ParentServiceID/ParentServiceName = %v/%v, want nil/nil", ungrouped.ParentServiceID, ungrouped.ParentServiceName)
	}
	// Status computation is untouched by grouping (SGD-09 non-goal).
	if ungrouped.Status != "operational" {
		t.Errorf("ungrouped service Status = %q, want %q", ungrouped.Status, "operational")
	}
	if !ungrouped.LastUpdatedAt.Equal(fetchedAt) {
		t.Errorf("ungrouped service LastUpdatedAt = %v, want %v", ungrouped.LastUpdatedAt, fetchedAt)
	}

	// Grouped parent, no parent of its own.
	parent := findPublicServiceByID(body.Services, parentID)
	if parent == nil {
		t.Fatalf("parent service %s not present in response", parentID)
	}
	if parent.GroupID == nil || *parent.GroupID != group.ID {
		t.Errorf("parent service GroupID = %v, want %q", parent.GroupID, group.ID)
	}
	if parent.GroupName == nil || *parent.GroupName != "Infra" {
		t.Errorf("parent service GroupName = %v, want %q", parent.GroupName, "Infra")
	}
	if parent.ParentServiceID != nil {
		t.Errorf("parent service ParentServiceID = %v, want nil", parent.ParentServiceID)
	}
	if parent.Status != "operational" {
		t.Errorf("parent service Status = %q, want %q", parent.Status, "operational")
	}

	// Grouped child, with a parent reference resolved to the parent's name.
	child := findPublicServiceByID(body.Services, childID)
	if child == nil {
		t.Fatalf("child service %s not present in response", childID)
	}
	if child.GroupID == nil || *child.GroupID != group.ID {
		t.Errorf("child service GroupID = %v, want %q", child.GroupID, group.ID)
	}
	if child.ParentServiceID == nil || *child.ParentServiceID != parentID {
		t.Errorf("child service ParentServiceID = %v, want %q", child.ParentServiceID, parentID)
	}
	if child.ParentServiceName == nil || *child.ParentServiceName != parent.Name {
		t.Errorf("child service ParentServiceName = %v, want %q", child.ParentServiceName, parent.Name)
	}
	// Each service's own status/analysis is unaffected by its placement -
	// the child is independently degraded with its own analysis, exactly
	// as TestPublicStatusGet_DegradedServiceWithAnalysis_IncludesStatusAnalysis
	// asserts for an ungrouped service.
	if child.Status != "degraded" {
		t.Errorf("child service Status = %q, want %q", child.Status, "degraded")
	}
	if child.StatusAnalysis == nil || *child.StatusAnalysis != analysis {
		t.Errorf("child service StatusAnalysis = %v, want %q", child.StatusAnalysis, analysis)
	}
}

// TestPublicStatusGet_NotConfiguredParent_ChildStillCarriesParentIDAndName
// covers the edge case T9 identifies but design.md leaves unresolved: a
// child's ParentServiceID can reference a service that itself got filtered
// out of the public response (status "not_configured", see
// composeResponse's shownServices filter). The decision implemented here is
// (a) ParentServiceID is still exposed (a client can know "this depends on
// something" even if that something isn't separately listed) and
// ParentServiceName is resolved from the *unfiltered* service list so the
// name is never silently dropped just because the parent isn't publicly
// shown.
func TestPublicStatusGet_NotConfiguredParent_ChildStillCarriesParentIDAndName(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()

	childID, cleanupChild := createPublicStatusServiceFixture(t, pool, "operational", time.Now().UTC())
	t.Cleanup(cleanupChild)

	services := db.NewServiceRepository(pool)
	parentName := uniqueServiceName(t)
	notConfiguredParent := &db.Service{Name: parentName, SLOID: "slo-not-configured-parent-test"}
	tenantID := seedTestTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		if err := services.Create(txCtx, notConfiguredParent); err != nil {
			t.Fatalf("setup Create() returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", notConfiguredParent.ID)
	})
	if notConfiguredParent.CurrentStatus != "not_configured" {
		t.Fatalf("setup: fresh service CurrentStatus = %q, want %q", notConfiguredParent.CurrentStatus, "not_configured")
	}

	statusPageID := createPublicStatusPageFixture(t, pool, childID, notConfiguredParent.ID)

	statusPages := db.NewStatusPageRepository(pool)
	parentIDCopy := notConfiguredParent.ID
	err := statusPages.SetServices(ctx, statusPageID, []db.ServicePlacement{
		{ServiceID: childID, GroupID: nil, ParentServiceID: &parentIDCopy, Position: 0},
		{ServiceID: notConfiguredParent.ID, GroupID: nil, ParentServiceID: nil, Position: 1},
	})
	if err != nil {
		t.Fatalf("setup SetServices() returned unexpected error: %v", err)
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

	for _, s := range body.Services {
		if s.ID == notConfiguredParent.ID {
			t.Fatalf("not_configured parent %s present in public response, want hidden", notConfiguredParent.ID)
		}
	}

	child := findPublicServiceByID(body.Services, childID)
	if child == nil {
		t.Fatalf("child service %s not present in response", childID)
	}
	if child.ParentServiceID == nil || *child.ParentServiceID != notConfiguredParent.ID {
		t.Errorf("child ParentServiceID = %v, want %q (kept even though parent is hidden)", child.ParentServiceID, notConfiguredParent.ID)
	}
	if child.ParentServiceName == nil || *child.ParentServiceName != parentName {
		t.Errorf("child ParentServiceName = %v, want %q (resolved from unfiltered service list)", child.ParentServiceName, parentName)
	}
}

// TestPublicStatusGet_DomainHealthy_DomainHealthOK covers PSDH-01/02: a
// status page whose attached domain has had a successful RDAP check, no
// NS drift, and an expiration far in the future reports domain_health.status
// "ok".
func TestPublicStatusGet_DomainHealthy_DomainHealthOK(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID, domainID := createPublicStatusPageFixtureWithDomainID(t, pool, serviceID)

	expiresAt := time.Now().Add(365 * 24 * time.Hour)
	registrar := "Example Registrar"
	domains := db.NewDomainRepository(pool)
	if err := domains.SetHealthCheckResult(context.Background(), domainID, &expiresAt, &registrar, []string{"ns1.example.com"}, false, nil); err != nil {
		t.Fatalf("setup SetHealthCheckResult() returned unexpected error: %v", err)
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
	if body.DomainHealth == nil {
		t.Fatalf("DomainHealth = nil, want non-nil")
	}
	if body.DomainHealth.Status != "ok" {
		t.Errorf("DomainHealth.Status = %q, want %q", body.DomainHealth.Status, "ok")
	}
	if strings.Contains(rec.Body.String(), "registrar") || strings.Contains(rec.Body.String(), "expires_at") || strings.Contains(rec.Body.String(), "ns1.example.com") {
		t.Errorf("response body leaks raw domain health data, want only derived status: %s", rec.Body.String())
	}
}

// TestPublicStatusGet_DomainNSDrift_DomainHealthAttention covers PSDH-02:
// NS drift alone (expiration far away) is enough to report "attention".
func TestPublicStatusGet_DomainNSDrift_DomainHealthAttention(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID, domainID := createPublicStatusPageFixtureWithDomainID(t, pool, serviceID)

	expiresAt := time.Now().Add(365 * 24 * time.Hour)
	registrar := "Example Registrar"
	domains := db.NewDomainRepository(pool)
	if err := domains.SetHealthCheckResult(context.Background(), domainID, &expiresAt, &registrar, []string{"ns1.example.com"}, true, nil); err != nil {
		t.Fatalf("setup SetHealthCheckResult() returned unexpected error: %v", err)
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
	if body.DomainHealth == nil {
		t.Fatalf("DomainHealth = nil, want non-nil")
	}
	if body.DomainHealth.Status != "attention" {
		t.Errorf("DomainHealth.Status = %q, want %q (NS drift)", body.DomainHealth.Status, "attention")
	}
}

// TestPublicStatusGet_DomainExpiringSoon_DomainHealthAttention covers
// PSDH-02: expiration within 30 days alone (no NS drift) is enough to
// report "attention" - reuses AD-039's first alert band, no new threshold.
func TestPublicStatusGet_DomainExpiringSoon_DomainHealthAttention(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID, domainID := createPublicStatusPageFixtureWithDomainID(t, pool, serviceID)

	expiresAt := time.Now().Add(10 * 24 * time.Hour)
	registrar := "Example Registrar"
	domains := db.NewDomainRepository(pool)
	if err := domains.SetHealthCheckResult(context.Background(), domainID, &expiresAt, &registrar, []string{"ns1.example.com"}, false, nil); err != nil {
		t.Fatalf("setup SetHealthCheckResult() returned unexpected error: %v", err)
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
	if body.DomainHealth == nil {
		t.Fatalf("DomainHealth = nil, want non-nil")
	}
	if body.DomainHealth.Status != "attention" {
		t.Errorf("DomainHealth.Status = %q, want %q (expires in 10 days)", body.DomainHealth.Status, "attention")
	}
}

// TestPublicStatusGet_DomainNeverRDAPChecked_DomainHealthOmitted covers
// PSDH-03: a domain that has never had a successful RDAP check (the
// fixture's default state right after Create - SetHealthCheckResult never
// called) must not surface domain_health at all, not even an optimistic
// "ok".
func TestPublicStatusGet_DomainNeverRDAPChecked_DomainHealthOmitted(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)
	statusPageID, _ := createPublicStatusPageFixtureWithDomainID(t, pool, serviceID)

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPageID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "domain_health") {
		t.Errorf("response contains domain_health field, want omitted entirely (never RDAP-checked): %s", rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.DomainHealth != nil {
		t.Errorf("DomainHealth = %+v, want nil", body.DomainHealth)
	}
}

// TestPublicStatusGet_NoDomainAttached_DomainHealthOmitted covers PSDH-04:
// a status page with no domain attached at all never has domain_health in
// its response - unchanged current behavior.
func TestPublicStatusGet_NoDomainAttached_DomainHealthOmitted(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "public-status-test-no-domain"}
	if err := statusPages.Create(ctx, statusPage, []string{serviceID}); err != nil {
		t.Fatalf("setup status page Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPage.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPage.ID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "domain_health") {
		t.Errorf("response contains domain_health field, want omitted entirely (no domain attached): %s", rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.DomainHealth != nil {
		t.Errorf("DomainHealth = %+v, want nil", body.DomainHealth)
	}
}

// TestPublicStatusGet_EnvironmentSet_IncludedInResponse covers SPEB-04: a
// status page with a non-empty environment tag surfaces it in the public
// response.
func TestPublicStatusGet_EnvironmentSet_IncludedInResponse(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "public-status-test-env", Environment: "staging"}
	if err := statusPages.Create(ctx, statusPage, []string{serviceID}); err != nil {
		t.Fatalf("setup status page Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPage.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPage.ID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", body.Environment, "staging")
	}
}

// TestPublicStatusGet_EnvironmentEmpty_OmittedFromResponse covers SPEB-04:
// a status page with no environment tag ("" - the overwhelming majority)
// never serializes the field at all, not even as an empty string.
func TestPublicStatusGet_EnvironmentEmpty_OmittedFromResponse(t *testing.T) {
	r, pool := newPublicStatusRouter(t)
	ctx := context.Background()
	serviceID, cleanup := createPublicStatusServiceFixture(t, pool, "operational", time.Now())
	t.Cleanup(cleanup)

	statusPages := db.NewStatusPageRepository(pool)
	statusPage := &db.StatusPage{Name: "public-status-test-no-env"}
	if err := statusPages.Create(ctx, statusPage, []string{serviceID}); err != nil {
		t.Fatalf("setup status page Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPage.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM status_pages WHERE id = $1", statusPage.ID)
	})

	req := withStatusPageContext(httptest.NewRequest(http.MethodGet, "/", nil), statusPage.ID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"environment"`) {
		t.Errorf("response contains environment field, want omitted entirely (no tag set): %s", rec.Body.String())
	}

	var body publicStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Environment != "" {
		t.Errorf("Environment = %q, want empty", body.Environment)
	}
}
